"""Own a subprocess group; use a Windows Job before resuming its first thread."""
from __future__ import annotations
import os
from pathlib import Path
import shutil
import signal
import subprocess
import time


def resolve_argv(argv, env, cwd):
    command=argv[0]
    if os.path.isabs(command): exe=command
    elif '/' in command or '\\' in command: exe=os.path.abspath(os.path.join(cwd,command))
    else: exe=shutil.which(command,path=env.get('PATH'))
    if not exe: raise RuntimeError('原 MCP 的启动程序未找到')
    if os.name=='nt' and Path(exe).suffix.lower() in {'.cmd','.bat'}:
        # Avoid cmd.exe quoting/injection. Resolve standard npm/npx shims only.
        stem=Path(exe).stem.lower()
        if stem not in {'npm','npx'}: raise RuntimeError('此批处理入口暂不支持安全转发')
        script=Path(exe).parent/'node_modules'/'npm'/'bin'/(stem+'-cli.js')
        node=shutil.which('node',path=env.get('PATH'))
        if not node or not script.is_file(): raise RuntimeError('无法解析 npm/npx 的 Node 启动入口')
        return [node,str(script)]+argv[1:]
    return [exe]+argv[1:]


class OwnedProcess:
    def __init__(self,argv,cwd,env):
        self.job=None
        kwargs=dict(stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,
                    cwd=cwd,env=env,bufsize=0)
        if os.name=='nt': kwargs['creationflags']=0x00000004|0x08000000 # suspended, no window
        else: kwargs['start_new_session']=True
        self.process=subprocess.Popen(resolve_argv(argv,env,cwd),**kwargs)
        if os.name=='nt':
            try: self._job_before_resume()
            except Exception:
                self.process.kill();self.process.wait(timeout=3)
                if self.job:self._kernel.CloseHandle(self.job)
                raise RuntimeError('Windows 进程归属建立失败，未运行 MCP') from None

    def _job_before_resume(self):
        import ctypes as C
        from ctypes import wintypes as W
        k=C.WinDLL('kernel32',use_last_error=True);self._kernel=k
        H=W.HANDLE
        class Basic(C.Structure):
            _fields_=[('ProcessTime',C.c_longlong),('JobTime',C.c_longlong),('LimitFlags',W.DWORD),
                ('MinWorking',C.c_size_t),('MaxWorking',C.c_size_t),('ActiveProcesses',W.DWORD),
                ('Affinity',C.c_size_t),('Priority',W.DWORD),('Scheduling',W.DWORD)]
        class IO(C.Structure):
            _fields_=[(name,C.c_ulonglong) for name in ['ReadOps','WriteOps','OtherOps','ReadBytes','WriteBytes','OtherBytes']]
        class Extended(C.Structure):
            _fields_=[('Basic',Basic),('IO',IO),('ProcessMemory',C.c_size_t),('JobMemory',C.c_size_t),
                      ('PeakProcess',C.c_size_t),('PeakJob',C.c_size_t)]
        class ThreadEntry(C.Structure):
            _fields_=[('size',W.DWORD),('usage',W.DWORD),('tid',W.DWORD),('pid',W.DWORD),
                      ('priority',W.LONG),('delta',W.LONG),('flags',W.DWORD)]
        k.CreateJobObjectW.argtypes=[C.c_void_p,W.LPCWSTR];k.CreateJobObjectW.restype=H
        k.SetInformationJobObject.argtypes=[H,C.c_int,C.c_void_p,W.DWORD];k.SetInformationJobObject.restype=W.BOOL
        k.AssignProcessToJobObject.argtypes=[H,H];k.AssignProcessToJobObject.restype=W.BOOL
        k.CloseHandle.argtypes=[H];k.CloseHandle.restype=W.BOOL
        k.CreateToolhelp32Snapshot.argtypes=[W.DWORD,W.DWORD];k.CreateToolhelp32Snapshot.restype=H
        k.Thread32First.argtypes=[H,C.POINTER(ThreadEntry)];k.Thread32First.restype=W.BOOL
        k.Thread32Next.argtypes=[H,C.POINTER(ThreadEntry)];k.Thread32Next.restype=W.BOOL
        k.OpenThread.argtypes=[W.DWORD,W.BOOL,W.DWORD];k.OpenThread.restype=H
        k.ResumeThread.argtypes=[H];k.ResumeThread.restype=W.DWORD
        k.TerminateJobObject.argtypes=[H,W.UINT];k.TerminateJobObject.restype=W.BOOL
        self.job=k.CreateJobObjectW(None,None)
        info=Extended();info.Basic.LimitFlags=0x2000 # KILL_ON_JOB_CLOSE, no breakaway
        if not self.job or not k.SetInformationJobObject(self.job,9,C.byref(info),C.sizeof(info)):
            raise OSError('job setup')
        if not k.AssignProcessToJobObject(self.job,H(int(self.process._handle))): raise OSError('job assignment')
        snap=k.CreateToolhelp32Snapshot(4,0)
        if snap==C.c_void_p(-1).value: raise OSError('thread snapshot')
        resumed=False
        try:
            t=ThreadEntry();t.size=C.sizeof(t);ok=k.Thread32First(snap,C.byref(t))
            while ok:
                if t.pid==self.process.pid:
                    handle=k.OpenThread(2,False,t.tid)
                    if handle:
                        try: resumed=k.ResumeThread(handle)!=0xffffffff
                        finally:k.CloseHandle(handle)
                    if resumed:break
                ok=k.Thread32Next(snap,C.byref(t))
        finally:k.CloseHandle(snap)
        if not resumed:raise OSError('resume failed')

    def stop(self):
        p=self.process
        if os.name=='nt':
            if self.job:
                self._kernel.TerminateJobObject(self.job,1)
                self._kernel.CloseHandle(self.job);self.job=None
        else:
            try:os.killpg(p.pid,signal.SIGTERM)
            except ProcessLookupError:pass
            try:p.wait(timeout=1)
            except subprocess.TimeoutExpired:pass
            # Kill residual children in the owned group even when the leader exited.
            try:os.killpg(p.pid,signal.SIGKILL)
            except ProcessLookupError:pass
        try:p.wait(timeout=3)
        except subprocess.TimeoutExpired:return False
        for f in [p.stdin,p.stdout,p.stderr]:
            try:f.close()
            except (OSError,ValueError):pass
        return True
