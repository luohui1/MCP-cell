"""Owned interactive PTYs with bounded output. Not a filesystem/network sandbox."""
from __future__ import annotations

import base64
import os
from pathlib import Path
import select
import signal
import subprocess
import sys
import threading
import time

from .storage import Conflict

MAX_OUTPUT = 1024 * 1024
MAX_READ = 64 * 1024
MAX_INPUT = 8192


class OutputBuffer:
    def __init__(self, limit=MAX_OUTPUT):
        self.limit = limit
        self.data = bytearray()
        self.start = 0
        self.lock = threading.Lock()

    def append(self, data: bytes):
        with self.lock:
            self.data.extend(data)
            excess = max(0, len(self.data) - self.limit)
            if excess:
                del self.data[:excess]
                self.start += excess

    def read(self, offset: int) -> dict:
        if type(offset) is not int or offset < 0:
            raise Conflict('输出位置无效')
        with self.lock:
            end = self.start + len(self.data)
            if offset > end:
                raise Conflict('输出位置超出会话范围')
            lost = max(0, self.start - offset)
            position = max(offset, self.start)
            data = bytes(self.data[position - self.start:position - self.start + MAX_READ])
            return {'data': base64.b64encode(data).decode('ascii'), 'next': position + len(data),
                    'droppedBytes': lost, 'hasMore': position + len(data) < end}


class TerminalSession:
    def __init__(self, sid: str, agent_id: str, title: str, argv: list[str], cwd: str,
                 env: dict[str, str], cols: int = 100, rows: int = 28):
        self.id, self.agent_id, self.title = sid, agent_id, title
        self.cwd, self.argv = cwd, list(argv)
        self.created = time.time()
        self.output = OutputBuffer()
        self.lock = threading.RLock()
        self.stopping = threading.Event()
        self.closed = False
        self.exit_code = None
        self.error = None
        self.state = 'starting'
        self.master = None
        self.pty = None
        self.process = None
        self.cleanup = 'pending'
        self.cols, self.rows = cols, rows
        self._validate_size(cols, rows)
        if os.name == 'nt':
            try:
                from winpty import PTY
            except ImportError:
                raise Conflict('缺少 Windows 终端组件：请安装 requirements.txt 中的 pywinpty') from None
            # Direct ConPTY binding: no additional local socket listener.
            self.pty = PTY(cols, rows, backend=1)
            block = '\0'.join(f'{k}={v}' for k, v in env.items()) + '\0'
            self.pty.spawn(argv[0], cwd=cwd, env=block,
                           cmdline=' ' + subprocess.list2cmdline(argv[1:]))
            self.pid = self.pty.pid
        else:
            import pty
            import fcntl
            import struct
            import termios
            master, slave = pty.openpty()
            try:
                fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack('HHHH', rows, cols, 0, 0))
                self.process = subprocess.Popen(
                    [sys.executable, '-I', str(Path(__file__).with_name('terminal_child.py')), *argv],
                    stdin=slave, stdout=slave, stderr=slave, cwd=cwd, env=env,
                    start_new_session=True, close_fds=True)
                self.pid = self.process.pid
                self.master = master
                os.set_blocking(master, False)
            except Exception:
                os.close(master)
                raise
            finally:
                os.close(slave)
        try:
            import psutil
            self.identity = psutil.Process(self.pid).create_time()
        except Exception:
            self.identity = None
        self.state = 'running'
        self.reader = threading.Thread(target=self._read, name='agent-pty-' + sid[:8], daemon=True)
        self.reader.start()

    @staticmethod
    def _validate_size(cols, rows):
        if type(cols) is not int or type(rows) is not int or not 20 <= cols <= 300 or not 5 <= rows <= 100:
            raise Conflict('终端尺寸必须在 20–300 列、5–100 行之间')

    def _alive(self):
        if self.process is not None:
            if hasattr(os, 'waitid') and hasattr(os, 'WNOWAIT'):
                # Keep the leader PID reserved until owned-group cleanup.
                result = os.waitid(os.P_PID, self.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT)
                return result is None
            return self.process.poll() is None
        try:
            return bool(self.pty.isalive())
        except Exception:
            return False

    def _read(self):
        try:
            while not self.stopping.is_set():
                if self.master is not None:
                    ready, _, _ = select.select([self.master], [], [], .2)
                    if ready:
                        try:
                            data = os.read(self.master, MAX_READ)
                        except BlockingIOError:
                            continue
                        except OSError:
                            break
                        if not data:
                            break
                        self.output.append(data)
                    elif not self._alive():
                        break
                else:
                    data = self.pty.read(blocking=False)
                    if data:
                        self.output.append(data.encode('utf-8', errors='replace'))
                    elif not self._alive():
                        break
                    else:
                        time.sleep(.03)
        except Exception:
            if not self.stopping.is_set():
                self.error = '终端输出已断开；未重放输入'
        finally:
            self._finish()

    def _finish(self):
        with self.lock:
            if self.closed:
                return
            self.stopping.set()
            if self.process is not None:
                self.exit_code = self.process.returncode
                # This is the process group created for this session only.
                try:
                    os.killpg(self.pid, signal.SIGTERM)
                except ProcessLookupError:
                    pass
                try:
                    self.process.wait(timeout=.5)
                except subprocess.TimeoutExpired:
                    pass
                try:
                    os.killpg(self.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                try:
                    self.process.wait(timeout=2)
                    self.cleanup = 'process-group-stopped'
                except subprocess.TimeoutExpired:
                    self.cleanup = 'unconfirmed'
                if self.exit_code is None:
                    self.exit_code = self.process.returncode
                if self.master is not None:
                    os.close(self.master)
                    self.master = None
            else:
                # ConPTY tree cleanup is identity-checked best effort, not a Job boundary.
                self.cleanup = 'best-effort'
                try:
                    import psutil
                    root = psutil.Process(self.pid)
                    if self.identity is not None and root.create_time() == self.identity:
                        family = root.children(recursive=True) + [root]
                        for p in reversed(family):
                            try:
                                p.terminate()
                            except psutil.Error:
                                pass
                        _, alive = psutil.wait_procs(family, timeout=.5)
                        for p in alive:
                            try:
                                p.kill()
                            except psutil.Error:
                                pass
                    self.exit_code = self.pty.get_exitstatus()
                except Exception:
                    self.cleanup = 'unconfirmed'
                try:
                    self.pty.cancel_io()
                except Exception:
                    pass
            self.closed = True
            self.state = 'exited'

    def stop(self):
        self.stopping.set()
        self._finish()
        if threading.current_thread() != self.reader:
            self.reader.join(timeout=2)

    def write(self, text: str):
        if not isinstance(text, str) or not text or len(text.encode('utf-8')) > MAX_INPUT:
            raise Conflict('单次终端输入须为 1–8192 字节')
        with self.lock:
            if self.closed or not self._alive():
                raise Conflict('会话已经结束，输入未发送')
            if self.master is None:
                self.pty.write(text)
            else:
                data = text.encode('utf-8')
                deadline = time.monotonic() + 1
                while data:
                    if time.monotonic() > deadline:
                        raise Conflict('终端输入超时；可能部分已发送，请勿自动重试')
                    _, writable, _ = select.select([], [self.master], [], .05)
                    if writable:
                        try:
                            n = os.write(self.master, data)
                            data = data[n:]
                        except BlockingIOError:
                            continue

    def resize(self, cols, rows):
        self._validate_size(cols, rows)
        with self.lock:
            if self.closed:
                return
            if self.master is not None:
                import fcntl
                import struct
                import termios
                fcntl.ioctl(self.master, termios.TIOCSWINSZ, struct.pack('HHHH', rows, cols, 0, 0))
            else:
                self.pty.set_size(cols, rows)
            self.cols, self.rows = cols, rows

    def snapshot(self):
        rss = None
        if not self.closed:
            try:
                import psutil
                p = psutil.Process(self.pid)
                if self.identity is not None and p.create_time() == self.identity:
                    rss = sum(x.memory_info().rss for x in [p, *p.children(recursive=True)])
            except Exception:
                pass
        return {'id': self.id, 'agentId': self.agent_id, 'title': self.title, 'cwd': self.cwd,
                'pid': self.pid, 'state': self.state, 'exitCode': self.exit_code,
                'createdAt': self.created, 'rssBytes': rss, 'error': self.error,
                'cols': self.cols, 'rows': self.rows, 'cleanup': self.cleanup,
                'backend': 'conpty-preview' if os.name == 'nt' else 'posix-pty'}
