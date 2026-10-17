"""Bounded authenticated stdio relay. One upstream per connection, never shared."""
from __future__ import annotations
import json
import os
from pathlib import Path
import secrets
import socket
import socketserver
import threading
import time

from .processes import OwnedProcess
from .storage import Conflict

MAX_FRAME=8*1024*1024
BASE_ENV={'PATH','HOME','USERPROFILE','APPDATA','LOCALAPPDATA','SYSTEMROOT','SystemRoot','WINDIR',
          'TEMP','TMP','TMPDIR','USER','USERNAME','LANG','LC_ALL','PATHEXT','COMSPEC',
          'XDG_CONFIG_HOME','XDG_CACHE_HOME','VIRTUAL_ENV','CONDA_PREFIX','SSL_CERT_FILE',
          'SSL_CERT_DIR','HTTP_PROXY','HTTPS_PROXY','NO_PROXY','http_proxy','https_proxy','no_proxy'}

class Runtime(socketserver.ThreadingTCPServer):
    allow_reuse_address=True
    daemon_threads=True
    request_queue_size=8
    def __init__(self,manager):
        self.manager=manager;self.lock=threading.RLock();self.instances={}
        self.slots=threading.BoundedSemaphore(8)
        self.stopping=False
        super().__init__(('127.0.0.1',0),Relay)
        self.thread=threading.Thread(target=self.serve_forever,daemon=True);self.thread.start()
    def process_request(self,request,addr):
        if self.stopping or not self.slots.acquire(blocking=False):
            self.shutdown_request(request);return
        try:super().process_request(request,addr)
        except Exception:self.slots.release();self.shutdown_request(request)
    def process_request_thread(self,request,addr):
        try:super().process_request_thread(request,addr)
        finally:self.slots.release()
    def stop(self,bid):
        with self.lock:
            for r in list(self.instances.values()):
                if r['bindingId']==bid:
                    r['cancel'].set()
                    try:r['socket'].shutdown(socket.SHUT_RDWR)
                    except OSError:pass
    def status(self):
        try:import psutil
        except ImportError:psutil=None
        rows=[]
        with self.lock:
            for sid,r in self.instances.items():
                p=r['owned'].process
                rss=None;count=None
                if psutil and p.poll() is None:
                    try:
                        procs=[psutil.Process(p.pid)];procs+=procs[0].children(recursive=True)
                        rss=sum(x.memory_info().rss for x in procs);count=len(procs)
                    except psutil.Error:pass
                rows.append({'id':sid,'bindingId':r['bindingId'],'pid':p.pid,'rssBytes':rss,
                    'processCount':count,'uptimeSeconds':round(time.monotonic()-r['started']),
                    'status':'stopping' if r['cancel'].is_set() else 'running'})
        return {'instances':rows,'running':len(rows),'rssBytes':sum(r['rssBytes'] for r in rows) if rows and all(r['rssBytes'] is not None for r in rows) else (0 if not rows else None),
                'measurement':'sum-process-rss-not-deduplicated','sharing':'exclusive'}
    def close(self):
        self.stopping=True
        with self.lock:bids=[r['bindingId'] for r in self.instances.values()]
        for bid in bids:self.stop(bid)
        self.shutdown();self.server_close();self.thread.join(timeout=2)
        deadline=time.monotonic()+5
        while self.instances and time.monotonic()<deadline:time.sleep(.05)

class Relay(socketserver.StreamRequestHandler):
    def handle(self):
        runtime=self.server;owned=None;sid=None;cancel=threading.Event()
        try:
            self.request.settimeout(5)
            line=self.rfile.readline(65537)
            if not line.endswith(b'\n') or len(line)>65536: return
            hello=json.loads(line)
            bid=hello.get('bindingId');token=hello.get('token','')
            if not isinstance(bid,str) or not isinstance(token,str):return
            with runtime.manager.lock:
                saved=runtime.manager.bindings.get(bid)
                if not saved or not secrets.compare_digest(token.encode(),saved['token'].encode()):return
                record=runtime.manager.verify(bid)
            self.wfile.write(b'{"ok":true}\n');self.wfile.flush()
            # Await the client's first protocol frame; do not pre-spawn a server farm.
            self.request.settimeout(60)
            first=self.rfile.readline(MAX_FRAME+1)
            if not first or len(first)>MAX_FRAME or not first.endswith(b'\n'):return
            obj=json.loads(first)
            if not isinstance(obj,dict):return
            cwd=hello.get('cwd')
            if not isinstance(cwd,str) or not os.path.isabs(cwd) or not Path(cwd).is_dir():raise Conflict('工作目录无效')
            supplied=hello.get('env',{})
            if not isinstance(supplied,dict) or len(supplied)>256 or any(not isinstance(k,str) or not isinstance(v,str) for k,v in supplied.items()):return
            cfg=record['original']
            names=set(cfg.get('env',cfg.get('environment',{})))|set(cfg.get('env_vars',[]))
            env={k:v for k,v in supplied.items() if k in BASE_ENV or k in names}
            # Never inherit all of the manager's environment/credentials.
            for key in ['PATH','SYSTEMROOT','SystemRoot','WINDIR']:
                if key not in env and key in os.environ:env[key]=os.environ[key]
            if cfg.get('cwd'):cwd=cfg['cwd']
            argv=cfg['command'] if isinstance(cfg['command'],list) else [cfg['command']]+cfg.get('args',[])
            with runtime.manager.lock:
                runtime.manager.verify(bid) # Check again after waiting for first frame.
                owned=OwnedProcess(argv,cwd,env)
                sid=secrets.token_hex(8)
                with runtime.lock:
                    runtime.instances[sid]={'bindingId':bid,'owned':owned,'socket':self.request,
                                             'cancel':cancel,'started':time.monotonic()}
            self.request.settimeout(None)
            p=owned.process
            def upstream():
                try:
                    p.stdin.write(first);p.stdin.flush()
                    while not cancel.is_set():
                        data=self.rfile.readline(MAX_FRAME+1)
                        if not data or len(data)>MAX_FRAME or not data.endswith(b'\n'):break
                        p.stdin.write(data);p.stdin.flush()
                except (OSError,ValueError):pass
                finally:cancel.set()
            def downstream():
                try:
                    while not cancel.is_set():
                        data=p.stdout.readline(MAX_FRAME+1)
                        if not data or len(data)>MAX_FRAME or not data.endswith(b'\n'):break
                        self.wfile.write(data);self.wfile.flush()
                except (OSError,ValueError):pass
                finally:cancel.set()
            def stderr():
                try:
                    while p.stderr.read(4096):pass # Bounded drain, never persist possible secrets.
                except (OSError,ValueError):pass
            threads=[threading.Thread(target=fn,daemon=True) for fn in [upstream,downstream,stderr]]
            for t in threads:t.start()
            while not cancel.wait(.15):
                if p.poll() is not None:break
        except (OSError,ValueError,TypeError,KeyError,RuntimeError):
            # No command/credential data in diagnostics; no automatic replay.
            try:
                error={'jsonrpc':'2.0','id':obj.get('id') if 'obj' in locals() else None,
                       'error':{'code':-32001,'message':'MCPcell 无法启动或转发此服务；请检查管理器状态。未自动重试。'}}
                self.wfile.write(json.dumps(error).encode()+b'\n');self.wfile.flush()
            except (OSError,ValueError):pass
        finally:
            cancel.set()
            try:self.request.shutdown(socket.SHUT_RDWR)
            except OSError:pass
            if owned:owned.stop()
            if sid:
                with runtime.lock:runtime.instances.pop(sid,None)
