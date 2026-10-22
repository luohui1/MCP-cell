"""End-to-end check that the tray icon is only a view: quitting it leaves the
daemon and every wrapped session alone.

    python scripts/mcptray.py <dir with mcpcell.exe, mcpcelld.exe, mcpcell-run.exe, mcpcell-tray.exe> [--window]

Runs its own MCPcell (temporary home and agent config dirs, port 47696,
graphical interface). `mcpcell start` brings up the daemon and the tray; an
agent session goes through mcpcell-run to a tiny MCP server shared by all
sessions. Then the tray quits the way its 退出 does: the icon must be gone,
the daemon still running, and the session's next calls still answered by the
same server process under the daemon (not run directly by the wrapper).
`mcpcell mode cli|gui` must stop and start the tray, `mcpcell stop --help`
must not stop anything, and the tray's private commit is reported. With
--window it also opens the window (briefly, on screen): opening twice keeps
one window, and quitting the tray closes it.
Windows only. Never touches the installed MCPcell or its logon entries.
"""
import ctypes
import ctypes.wintypes as wt
import json
import os
import queue
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request

PORT = 47696
CREATE_NO_WINDOW = 0x08000000
WM_TRAY_QUIT = 0x8000 + 2  # WM_APP + 2, what StopTray and 退出 do

user32 = ctypes.windll.user32
shell32 = ctypes.windll.shell32
kernel32 = ctypes.windll.kernel32


def tiny_server():
    """A stdio MCP server: echo and pid."""
    tools = [{'name': n, 'inputSchema': {'type': 'object'}} for n in ('echo', 'pid')]
    for line in sys.stdin:
        m = json.loads(line)
        if 'id' not in m:
            continue
        if m['method'] == 'initialize':
            res = {'protocolVersion': m['params']['protocolVersion'], 'capabilities': {'tools': {}}, 'serverInfo': {'name': 'tiny', 'version': '1'}}
        elif m['method'] == 'tools/list':
            res = {'tools': tools}
        elif m['method'] == 'tools/call':
            name, args = m['params']['name'], m['params'].get('arguments') or {}
            res = {'content': [{'type': 'text', 'text': args.get('text', '') if name == 'echo' else str(os.getpid())}]}
        else:
            res = {}
        sys.stdout.write(json.dumps({'jsonrpc': '2.0', 'id': m['id'], 'result': res}) + '\n')
        sys.stdout.flush()


def home_id(home):
    """The tray's window property (desktop.homeID): FNV-1a of the lower-cased path, low bit set."""
    h = 0x811C9DC5
    for b in os.path.normpath(home).lower().encode('utf-8'):
        h = ((h ^ b) * 0x01000193) & 0xFFFFFFFF
    return h | 1


EnumProc = ctypes.WINFUNCTYPE(wt.BOOL, wt.HWND, wt.LPARAM)
user32.GetPropW.restype = ctypes.c_void_p
user32.GetPropW.argtypes = [wt.HWND, wt.LPCWSTR]


def find_windows(cls, home):
    want, found = home_id(home), []

    def cb(h, _):
        buf = ctypes.create_unicode_buffer(64)
        user32.GetClassNameW(h, buf, 64)
        if buf.value == cls and (user32.GetPropW(h, 'MCPcellHome') or 0) == want:
            found.append(h)
        return True

    user32.EnumWindows(EnumProc(cb), 0)
    return found


def find_window(cls, home):
    found = find_windows(cls, home)
    return found[0] if found else None


class NOTIFYICONIDENTIFIER(ctypes.Structure):
    _fields_ = [('cbSize', wt.DWORD), ('hWnd', wt.HWND), ('uID', wt.UINT), ('guidItem', ctypes.c_byte * 16)]


def icon_shown(hwnd):
    """Shell_NotifyIconGetRect succeeds only while the icon exists."""
    if not hwnd:
        return False
    ident = NOTIFYICONIDENTIFIER(ctypes.sizeof(NOTIFYICONIDENTIFIER), hwnd, 1)
    r = wt.RECT()
    return shell32.Shell_NotifyIconGetRect(ctypes.byref(ident), ctypes.byref(r)) == 0


class PROCESS_MEMORY_COUNTERS_EX(ctypes.Structure):
    _fields_ = [('cb', wt.DWORD), ('PageFaultCount', wt.DWORD)] + [(n, ctypes.c_size_t) for n in (
        'PeakWorkingSetSize', 'WorkingSetSize', 'QuotaPeakPagedPoolUsage', 'QuotaPagedPoolUsage',
        'QuotaPeakNonPagedPoolUsage', 'QuotaNonPagedPoolUsage', 'PagefileUsage', 'PeakPagefileUsage', 'PrivateUsage')]


def private_commit(pid):
    h = kernel32.OpenProcess(0x1000 | 0x0010, False, pid)  # QUERY_LIMITED_INFORMATION | VM_READ
    c = PROCESS_MEMORY_COUNTERS_EX()
    c.cb = ctypes.sizeof(c)
    ctypes.windll.psapi.GetProcessMemoryInfo(h, ctypes.byref(c), c.cb)
    kernel32.CloseHandle(h)
    return c.PrivateUsage, c.WorkingSetSize


def window_pid(hwnd):
    pid = wt.DWORD()
    user32.GetWindowThreadProcessId(hwnd, ctypes.byref(pid))
    return pid.value


class Agent:
    def __init__(self, argv, env):
        self.p = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env, creationflags=CREATE_NO_WINDOW)
        self.replies, self.next, self.stderr = queue.Queue(), 0, []
        threading.Thread(target=lambda: [self.replies.put(json.loads(l)) for l in self.p.stdout], daemon=True).start()
        threading.Thread(target=lambda: [self.stderr.append(l.decode('utf-8', 'replace').strip()) for l in self.p.stderr], daemon=True).start()

    def send(self, method, params=None):
        self.next += 1
        self.p.stdin.write((json.dumps({'jsonrpc': '2.0', 'id': self.next, 'method': method, 'params': params or {}}) + '\n').encode())
        self.p.stdin.flush()
        return self.next

    def reply(self, id, timeout=60):
        deadline = time.time() + timeout
        while time.time() < deadline:
            try:
                m = self.replies.get(timeout=1)
            except queue.Empty:
                continue
            if m.get('id') == id:
                return m
        raise TimeoutError(id)

    def call(self, tool, **args):
        m = self.reply(self.send('tools/call', {'name': tool, 'arguments': args}))
        return m['result']['content'][0]['text'] if 'result' in m else m['error']


def main():
    if sys.argv[1:] == ['--server']:
        return tiny_server()
    sys.stdout.reconfigure(encoding='utf-8', errors='replace')
    bins = os.path.abspath(sys.argv[1])  # --window may follow
    tmp = tempfile.mkdtemp(prefix='mcpcell-tray-')
    home = os.path.join(tmp, 'home')
    os.makedirs(home)
    json.dump({'port': PORT, 'interface': 'gui', 'services': {}}, open(os.path.join(home, 'config.json'), 'w'))
    env = dict(os.environ, MCPCELL_HOME=home, CODEX_HOME=os.path.join(tmp, 'codex'), CLAUDE_CONFIG_DIR=os.path.join(tmp, 'claude'),
               USERPROFILE=os.path.join(tmp, 'user'))
    os.makedirs(env['USERPROFILE'])
    checks = []

    def check(name, ok, detail=''):
        checks.append(ok)
        print(('OK  ' if ok else 'FAIL') + f' {name} {detail}', flush=True)

    def cli(*args):
        r = subprocess.run([os.path.join(bins, 'mcpcell.exe'), *args], env=env, capture_output=True, timeout=60)
        return r.returncode, (r.stdout + r.stderr).decode('utf-8', 'replace')

    def api(path, method='GET'):
        token = open(os.path.join(home, 'token')).read().strip()
        req = urllib.request.Request(f'http://127.0.0.1:{PORT}{path}', method=method, headers={'Authorization': 'Bearer ' + token})
        return json.loads(urllib.request.urlopen(req, timeout=10).read())

    def daemon_up():
        try:
            api('/api/status')
            return True
        except OSError:
            return False

    def wait(cond, secs=10):
        deadline = time.time() + secs
        while time.time() < deadline:
            if cond():
                return True
            time.sleep(0.2)
        return cond()

    try:
        code, out = cli('start')
        tray = None
        check('start brings up the daemon and the tray', code == 0 and daemon_up() and wait(lambda: find_window('MCPcellTray', home)), out.strip().replace('\n', ' | '))
        tray = find_window('MCPcellTray', home)
        check('tray icon shown', wait(lambda: icon_shown(tray), 5))
        daemon_pid = api('/api/status')['pid']

        time.sleep(2)  # settled
        priv, ws = private_commit(window_pid(tray))
        print(f'     tray process: private commit {priv / 2**20:.1f} MB, working set {ws / 2**20:.1f} MB')
        dpriv, _ = private_commit(daemon_pid)
        print(f'     daemon: private commit {dpriv / 2**20:.1f} MB')

        wrap = [os.path.join(bins, 'mcpcell-run.exe'), '--shared', '--', sys.executable, os.path.abspath(__file__), '--server']
        a = Agent(wrap, env)
        init = a.reply(a.send('initialize', {'protocolVersion': '2025-11-25', 'capabilities': {}, 'clientInfo': {'name': 'agent', 'version': '1'}}))
        a.p.stdin.write(b'{"jsonrpc":"2.0","method":"notifications/initialized"}\n')
        a.p.stdin.flush()
        check('session through MCPcell', 'result' in init and a.call('echo', text='a') == 'a')
        server = a.call('pid')
        st = api('/api/status')
        check('the daemon runs the server', any((s.get('instance') or {}).get('pid') for s in st['services']) and len(st['sessions']) == 1)

        # 退出 from the tray: icon and window go, nothing else.
        user32.PostMessageW(tray, WM_TRAY_QUIT, 0, 0)
        check('tray quits', wait(lambda: not find_window('MCPcellTray', home), 5))
        check('tray icon gone', not icon_shown(tray))
        check('daemon still running', daemon_up() and api('/api/status')['pid'] == daemon_pid)
        check('session still answered', a.call('echo', text='b') == 'b')
        check('by the same server under the daemon', a.call('pid') == server, f'pid {server}')
        st = api('/api/status')
        check('session still with the daemon', len(st['sessions']) == 1)
        check('wrapper never ran the server directly', not any('直接运行' in l for l in a.stderr), ' | '.join(a.stderr)[-200:])

        # The interface choice starts and stops the tray, nothing else.
        code, out = cli('mode', 'gui')
        check('mode gui starts the tray', code == 0 and wait(lambda: find_window('MCPcellTray', home), 5), out.strip().splitlines()[0] if out.strip() else '')
        code, out = cli('mode', 'cli')
        check('mode cli stops the tray', code == 0 and wait(lambda: not find_window('MCPcellTray', home), 5))
        check('session unaffected by mode switches', a.call('pid') == server and api('/api/status')['pid'] == daemon_pid)

        # Asking for help acts on nothing.
        code, out = cli('stop', '--help')
        check('stop --help only prints usage', code == 0 and '用法' in out and daemon_up())
        code, out = cli('stop', 'now')
        check('stop with a stray argument is refused', code == 2 and daemon_up())

        if '--window' in sys.argv:
            # Opens a real window for a few seconds.
            cli('mode', 'gui')
            code, out = cli('open')
            check('open shows the window', code == 0 and wait(lambda: find_window('MCPcellWindow', home), 15), out.strip()[:80])
            cli('open')
            time.sleep(3)
            check('opening again keeps one window', len(find_windows('MCPcellWindow', home)) == 1)
            tray = find_window('MCPcellTray', home)
            user32.PostMessageW(tray, WM_TRAY_QUIT, 0, 0)
            check('quitting the tray closes the window', wait(lambda: not find_window('MCPcellWindow', home), 10))
            check('and leaves the daemon and session', daemon_up() and a.call('pid') == server)

        a.p.stdin.close()
        a.p.wait(10)
    finally:
        h = find_window('MCPcellTray', home)
        if h:
            user32.PostMessageW(h, WM_TRAY_QUIT, 0, 0)
        if daemon_up():
            cli('stop')
    print(f'\n{sum(checks)}/{len(checks)} OK')
    sys.exit(0 if all(checks) else 1)


if __name__ == '__main__':
    main()
