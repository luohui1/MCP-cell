"""Private state, single-writer ownership and compare-before-replace writes."""
from __future__ import annotations
import ctypes
import json
import os
from pathlib import Path
import stat
import tempfile

class Conflict(RuntimeError):
    pass

class InstanceLock:
    def __init__(self, root: Path):
        private_dir(root)
        self.file = open(root / 'manager.lock', 'a+b')
        if os.name == 'nt':
            import msvcrt
            if self.file.seek(0, 2) == 0:
                self.file.write(b'0'); self.file.flush()
            self.file.seek(0)
            try: msvcrt.locking(self.file.fileno(), msvcrt.LK_NBLCK, 1)
            except OSError: self.file.close(); raise Conflict('MCPcell 已在运行') from None
        else:
            import fcntl
            try: fcntl.flock(self.file, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except OSError: self.file.close(); raise Conflict('MCPcell 已在运行') from None
    def close(self):
        if not self.file.closed:
            if os.name == 'nt':
                import msvcrt
                self.file.seek(0); msvcrt.locking(self.file.fileno(), msvcrt.LK_UNLCK, 1)
            self.file.close()

def private_dir(path: Path):
    for p in [path, *path.parents]:
        if p.exists() and (p.is_symlink() or getattr(p.lstat(), 'st_file_attributes', 0) & 0x400):
            raise Conflict('状态目录不能位于符号链接或重解析点内')
    path.mkdir(parents=True, exist_ok=True, mode=0o700)
    if os.name != 'nt':
        if path.stat().st_uid != os.getuid(): raise Conflict('状态目录所有者不匹配')
        path.chmod(0o700)

def crypt(data: bytes, decrypt=False) -> bytes:
    if os.name != 'nt': return data
    # Windows current-user DPAPI. No plaintext fallback on failure.
    from ctypes import wintypes
    class Blob(ctypes.Structure):
        _fields_ = [('cbData', wintypes.DWORD), ('pbData', ctypes.POINTER(ctypes.c_ubyte))]
    buf = (ctypes.c_ubyte * len(data)).from_buffer_copy(data)
    src, dst = Blob(len(data), buf), Blob()
    dll = ctypes.WinDLL('crypt32', use_last_error=True)
    fn = dll.CryptUnprotectData if decrypt else dll.CryptProtectData
    fn.restype = wintypes.BOOL
    fn.argtypes = [ctypes.POINTER(Blob), ctypes.c_void_p, ctypes.c_void_p, ctypes.c_void_p, ctypes.c_void_p, wintypes.DWORD, ctypes.POINTER(Blob)]
    if not fn(ctypes.byref(src), None, None, None, None, 1, ctypes.byref(dst)):
        raise Conflict('无法使用 Windows DPAPI 保护本地状态')
    try: return ctypes.string_at(dst.pbData, dst.cbData)
    finally:
        kernel = ctypes.WinDLL('kernel32', use_last_error=True)
        kernel.LocalFree.argtypes = [ctypes.c_void_p]; kernel.LocalFree(dst.pbData)

def atomic(path: Path, data: bytes, expected: bytes | None = None, mode: int = 0o600):
    if path.is_symlink(): raise Conflict('拒绝替换链接文件')
    if expected is not None and path.read_bytes() != expected:
        raise Conflict('配置已被其他程序修改，请重新检测；没有覆盖新内容')
    fd, name = tempfile.mkstemp(prefix='.mcpcell-', dir=path.parent)
    try:
        with os.fdopen(fd, 'wb') as f:
            if os.name != 'nt': os.fchmod(f.fileno(), mode)
            f.write(data); f.flush(); os.fsync(f.fileno())
        if expected is not None and path.read_bytes() != expected:
            raise Conflict('写入前发现配置变化，已取消')
        if os.name == 'nt' and path.exists():
            # ReplaceFileW preserves the replaced file's ACL. Fail closed.
            from ctypes import wintypes
            fn = ctypes.WinDLL('kernel32', use_last_error=True).ReplaceFileW
            fn.argtypes = [wintypes.LPCWSTR, wintypes.LPCWSTR, wintypes.LPCWSTR, wintypes.DWORD, ctypes.c_void_p, ctypes.c_void_p]
            fn.restype = wintypes.BOOL
            if not fn(str(path), str(name), None, 0, None, None):
                raise OSError(ctypes.get_last_error(), '配置替换失败')
        else:
            os.replace(name, path)
        if os.name != 'nt':
            dfd = os.open(path.parent, os.O_RDONLY)
            try: os.fsync(dfd)
            finally: os.close(dfd)
    finally:
        try: os.unlink(name)
        except FileNotFoundError: pass

class Vault:
    def __init__(self, root: Path):
        private_dir(root); self.root = root
    def put(self, name: str, value: object):
        data = json.dumps(value, ensure_ascii=False).encode()
        if len(data) > 16 * 1024 * 1024: raise Conflict('本地状态超出 16 MiB 限额，操作已取消')
        atomic(self.root / name, crypt(data))
    def get(self, name: str, default=None):
        path = self.root / name
        if not path.exists(): return default
        if path.is_symlink() or path.stat().st_size > 16 * 1024 * 1024:
            raise Conflict('状态文件类型或大小无效')
        return json.loads(crypt(path.read_bytes(), decrypt=True))
