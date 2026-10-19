"""End-to-end check that a wrapped session survives the daemon going away.

    python scripts/mcpreconnect.py <dir with mcpcell.exe, mcpcelld.exe, mcpcell-run.exe>

Runs its own MCPcell (temporary home, port 47697, temporary agent config
dirs) and one agent session through mcpcell-run, served by a tiny MCP server
in this file. Then: the daemon restarts as `mcpcell install` does it (a call
in flight must come back as result unknown, the next call must work); the
daemon is killed (the wrapper must start it again); MCPcell is stopped (the
wrapper must run the server directly); the agent closes stdin (the wrapper
must exit). The agent never initializes again.
"""
import json, os, queue, subprocess, sys, tempfile, threading, time, urllib.request

PORT = 47697
CREATE_NO_WINDOW = 0x08000000


def tiny_server():
    """A stdio MCP server: echo, slow (sleeps), pid."""
    tools = [{'name': n, 'inputSchema': {'type': 'object'}} for n in ('echo', 'slow', 'pid')]
    for line in sys.stdin:
        m = json.loads(line)
        if 'id' not in m:
            continue
        if m['method'] == 'initialize':
            res = {'protocolVersion': m['params']['protocolVersion'], 'capabilities': {'tools': {}},
                   'serverInfo': {'name': 'tiny', 'version': '1'}}
        elif m['method'] == 'tools/list':
            res = {'tools': tools}
        elif m['method'] == 'tools/call':
            name, args = m['params']['name'], m['params'].get('arguments', {})
            if name == 'slow':
                time.sleep(args.get('seconds', 3))
            text = {'echo': args.get('text', ''), 'slow': 'done', 'pid': str(os.getpid())}[name]
            res = {'content': [{'type': 'text', 'text': text}]}
        else:
            res = {}
        sys.stdout.write(json.dumps({'jsonrpc': '2.0', 'id': m['id'], 'result': res}) + '\n')
        sys.stdout.flush()


class Agent:
    def __init__(self, argv, env):
        self.p = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                  env=env, creationflags=CREATE_NO_WINDOW)
        self.replies, self.next, self.stderr = queue.Queue(), 0, []
        threading.Thread(target=self._out, daemon=True).start()
        threading.Thread(target=self._err, daemon=True).start()

    def _out(self):
        for line in self.p.stdout:
            self.replies.put(json.loads(line))

    def _err(self):
        for line in self.p.stderr:
            self.stderr.append(line.decode('utf-8', 'replace').strip())

    def send(self, method, params=None):
        self.next += 1
        self.p.stdin.write((json.dumps({'jsonrpc': '2.0', 'id': self.next, 'method': method, 'params': params or {}}) + '\n').encode())
        self.p.stdin.flush()
        return self.next

    def reply(self, id, timeout=90):
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
    bins = sys.argv[1]
    tmp = tempfile.mkdtemp(prefix='mcpcell-reconnect-')
    home = os.path.join(tmp, 'home')
    os.makedirs(home)
    json.dump({'port': PORT, 'services': {}}, open(os.path.join(home, 'config.json'), 'w'))
    env = dict(os.environ, MCPCELL_HOME=home, CODEX_HOME=os.path.join(tmp, 'codex'), CLAUDE_CONFIG_DIR=os.path.join(tmp, 'claude'))
    daemons = []

    def start_daemon():
        d = subprocess.Popen([os.path.join(bins, 'mcpcell.exe'), 'daemon', '--no-tray'], env=env,
                             stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, creationflags=CREATE_NO_WINDOW)
        daemons.append(d)
        for _ in range(50):
            try:
                urllib.request.urlopen(f'http://127.0.0.1:{PORT}/api/status', timeout=1)
            except urllib.error.HTTPError:
                return d  # up (401 without a token)
            except OSError:
                time.sleep(0.2)
        raise RuntimeError('daemon did not start')

    def api(path):
        token = open(os.path.join(home, 'token')).read().strip()
        req = urllib.request.Request(f'http://127.0.0.1:{PORT}{path}', method='POST', headers={'Authorization': 'Bearer ' + token})
        urllib.request.urlopen(req, timeout=10)

    def daemon_pid():
        return json.load(open(os.path.join(home, 'daemon.json')))['pid']

    checks = []

    def check(name, ok, detail=''):
        checks.append(ok)
        print(('OK  ' if ok else 'FAIL') + f' {name} {detail}', flush=True)

    start_daemon()
    a = Agent([os.path.join(bins, 'mcpcell-run.exe'), '--', sys.executable, os.path.abspath(__file__), '--server'], env)
    init = a.reply(a.send('initialize', {'protocolVersion': '2025-11-25', 'capabilities': {}, 'clientInfo': {'name': 'agent', 'version': '1'}}))
    a.p.stdin.write(b'{"jsonrpc":"2.0","method":"notifications/initialized"}\n')
    a.p.stdin.flush()
    check('session through MCPcell', 'result' in init and a.call('echo', text='a') == 'a')

    # 1. Restart, as install does it, with a call in flight.
    slow = a.send('tools/call', {'name': 'slow', 'arguments': {'seconds': 5}})
    time.sleep(1)
    api('/api/shutdown?restart=1')
    daemons[-1].wait(15)
    time.sleep(1)
    start_daemon()
    lost = a.reply(slow)
    check('call in flight answered as result unknown', lost.get('error', {}).get('code') == -32002, json.dumps(lost)[:120])
    check('next call after restart', a.call('echo', text='b') == 'b')
    check('wrapper waited for the new daemon', any('正在更新' in l for l in a.stderr) and any('已重新连上' in l for l in a.stderr))

    # 2. Crash: the wrapper starts the daemon again (mcpcelld.exe next to it).
    subprocess.run(['taskkill', '/F', '/PID', str(daemon_pid())], capture_output=True)
    time.sleep(0.5)
    check('next call after a crash', a.call('echo', text='c') == 'c')
    restarted = daemon_pid()
    check('wrapper restarted the daemon', restarted not in [d.pid for d in daemons], f'pid {restarted}')

    # 3. Stop: the session goes on with the server run directly.
    api('/api/shutdown')
    time.sleep(1)
    check('next call after MCPcell stopped', a.call('echo', text='d') == 'd')
    pid = a.call('pid')
    out = subprocess.run(['powershell', '-NoProfile', '-Command', f'(Get-CimInstance Win32_Process -Filter "ProcessId={pid}").ParentProcessId'],
                         capture_output=True, text=True).stdout.strip()
    check('server now runs under the wrapper', out == str(a.p.pid), f'parent {out}, wrapper {a.p.pid}')

    # 4. The agent is done.
    a.p.stdin.close()
    try:
        code = a.p.wait(10)
    except subprocess.TimeoutExpired:
        code = None
    check('wrapper exits when the agent closes stdin', code == 0, f'exit {code}')
    check('agent never saw a second initialize reply', a.replies.empty())
    print('\n'.join('    stderr: ' + l for l in a.stderr))
    for d in daemons:
        d.kill()
    sys.exit(0 if all(checks) else 1)


if __name__ == '__main__':
    main()
