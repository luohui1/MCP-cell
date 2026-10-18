"""Memory and speed benchmark: MCP servers run natively vs through MCPcell.

Clients are started the way agents start MCP servers (pipes, hidden
console), complete the handshake and list tools; memory is the private
commit of every process involved (each client's whole tree, conhost
included; for MCPcell also the daemon and its instances). No model is
involved, so results repeat.

    python scripts/mcpbench.py <servers.json> <out.json> [--quick | --readiness]

servers.json: {"wrapper": "<mcpcell-run.exe>", "home": "<MCPCELL_HOME of a
test daemon>", "servers": [{"name", "argv", "env", "tool": {"name",
"arguments"} | null, "calls": 30}]}. The daemon must be running already and
must not be the user's: point "home" at a test home.

Experiments:
  scaling   N concurrent idle sessions (connected, tools listed, no calls),
            every server per session: memory, processes, time until ready
  latency   per server with a tool: time from start to the first result
            (native; MCPcell with its instance stopped; MCPcell with it
            running) and the round trip of repeated calls
"""
import json, os, queue, statistics, subprocess, sys, threading, time, urllib.request

CREATE_NO_WINDOW = 0x08000000


class Client:
    """One MCP stdio client, as an agent would run it."""

    def __init__(self, argv, env):
        self.t0 = time.perf_counter()
        self.p = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                                  env=env, creationflags=CREATE_NO_WINDOW if os.name == 'nt' else 0)
        self.q = queue.Queue()
        self.next = 0
        threading.Thread(target=self._read, daemon=True).start()

    def _read(self):
        for line in self.p.stdout:
            try:
                self.q.put(json.loads(line))
            except ValueError:
                pass
        self.q.put(None)

    def rpc(self, method, params, timeout):
        self.next += 1
        msg = {'jsonrpc': '2.0', 'id': self.next, 'method': method, 'params': params}
        self.p.stdin.write((json.dumps(msg) + '\n').encode())
        self.p.stdin.flush()
        deadline = time.perf_counter() + timeout
        while True:
            left = deadline - time.perf_counter()
            if left <= 0:
                raise TimeoutError(method)
            m = self.q.get(timeout=left)
            if m is None:
                raise EOFError(method)
            if m.get('id') == self.next:
                if 'error' in m:
                    raise RuntimeError(json.dumps(m['error'])[:200])
                return m['result']

    def ready(self, timeout=180):
        """Handshake and tools/list; returns seconds since start."""
        self.rpc('initialize', {'protocolVersion': '2025-11-25', 'capabilities': {},
                                'clientInfo': {'name': 'mcpbench', 'version': '1'}}, timeout)
        self.p.stdin.write(b'{"jsonrpc":"2.0","method":"notifications/initialized"}\n')
        self.p.stdin.flush()
        self.tools = len(self.rpc('tools/list', {}, timeout)['tools'])
        return time.perf_counter() - self.t0

    def call(self, tool, timeout=180):
        t = time.perf_counter()
        r = self.rpc('tools/call', {'name': tool['name'], 'arguments': tool.get('arguments', {})}, timeout)
        if r.get('isError'):
            raise RuntimeError('tool error: ' + json.dumps(r)[:200])
        return time.perf_counter() - t

    def close(self):
        try:
            self.p.stdin.close()
            self.p.wait(timeout=10)
        except Exception:
            self.p.kill()


def processes():
    """Every process: pid -> (parent, name, private bytes, created)."""
    out = subprocess.run(['powershell', '-NoProfile', '-Command',
                          "Get-CimInstance Win32_Process | Select-Object ProcessId,ParentProcessId,Name,PrivatePageCount,"
                          "@{n='C';e={$_.CreationDate.ToFileTimeUtc()}} | ConvertTo-Json -Compress"],
                         capture_output=True, text=True).stdout
    return {r['ProcessId']: (r['ParentProcessId'], r['Name'], r['PrivatePageCount'], r['C']) for r in json.loads(out)}


def tree(table, root):
    """root and its descendants (children created after their parent, so a
    reused pid is not mistaken for a child)."""
    if root not in table:
        return []
    kids = {}
    for pid, (ppid, *_rest) in table.items():
        kids.setdefault(ppid, []).append(pid)
    out, stack = [], [root]
    while stack:
        x = stack.pop()
        out.append(x)
        stack += [c for c in kids.get(x, []) if table[c][3] >= table[x][3]]
    return out


def measure(clients, daemon_pid=None):
    table = processes()
    pids = [pid for c in clients for pid in tree(table, c.p.pid)]
    if daemon_pid:
        pids += tree(table, daemon_pid)
    names = {}
    for pid in pids:
        names[table[pid][1]] = names.get(table[pid][1], 0) + 1
    return {'mb': sum(table[p][2] for p in pids) / 2**20, 'processes': len(pids), 'names': names}


def free_commit_gb():
    out = subprocess.run(['powershell', '-NoProfile', '-Command', '(Get-CimInstance Win32_OperatingSystem).FreeVirtualMemory'],
                         capture_output=True, text=True).stdout
    return int(out.strip()) / 2**20


class Bench:
    def __init__(self, cfg):
        self.cfg = cfg
        self.home = cfg['home']
        self.token = open(os.path.join(self.home, 'token'), encoding='utf-8').read().strip()
        self.port = json.load(open(os.path.join(self.home, 'daemon.json'), encoding='utf-8'))['port']
        self.daemon_pid = json.load(open(os.path.join(self.home, 'daemon.json'), encoding='utf-8'))['pid']

    def env(self, server, mode):
        env = dict(os.environ)
        for k in ('CLAUDE_CONFIG_DIR', 'CODEX_HOME'):
            env.pop(k, None)
        env.update(server.get('env', {}))
        env['MCPCELL_HOME'] = self.home
        return env

    def argv(self, server, mode):
        return server['argv'] if mode == 'native' else [self.cfg['wrapper'], '--', *server['argv']]

    def api(self, method, path):
        req = urllib.request.Request(f'http://127.0.0.1:{self.port}{path}', method=method,
                                     headers={'Authorization': 'Bearer ' + self.token})
        return json.load(urllib.request.urlopen(req, timeout=60))

    def stop_all(self):
        """Stops every instance, so each run starts from the same state."""
        for s in self.api('GET', '/api/status')['services']:
            if s.get('instance') or s.get('instances'):
                self.api('POST', f'/api/services/{s["name"]}/stop')

    def shared_and_running(self, server):
        """Whether the daemon runs server as one shared instance right now."""
        parts = [a for a in server['argv'] if a.lower() not in ('cmd', '/c')]
        for s in self.api('GET', '/api/status')['services']:
            if all(a in s['command'] for a in parts):
                return s['mode'] == 'shared' and bool(s.get('instance'))
        return False

    def session(self, mode):
        """Starts every server for one session; returns (clients, ready times)."""
        clients = [Client(self.argv(s, mode), self.env(s, mode)) for s in self.cfg['servers']]
        return clients

    def scaling(self, ns, reps):
        rows = []
        for n in ns:
            for rep in range(reps):
                for mode in (('native', 'mcpcell') if rep % 2 == 0 else ('mcpcell', 'native')):
                    if mode == 'native' and free_commit_gb() < 8:
                        print(f'  skip native n={n}: free commit below 8 GB', flush=True)
                        continue
                    self.stop_all()
                    servers = [s for _ in range(n) for s in self.cfg['servers']]
                    clients, ready, errors = [None] * len(servers), [None] * len(servers), []

                    # Each client starts and handshakes on its own thread, as
                    # each agent session would, so one client's time does not
                    # include starting the others.
                    def go(i, server):
                        try:
                            clients[i] = Client(self.argv(server, mode), self.env(server, mode))
                            ready[i] = clients[i].ready()
                        except Exception as e:
                            errors.append(f'{server["name"]}: {e}')
                    t0 = time.perf_counter()
                    ts = [threading.Thread(target=go, args=(i, s)) for i, s in enumerate(servers)]
                    [t.start() for t in ts]
                    [t.join() for t in ts]
                    all_ready = time.perf_counter() - t0
                    clients = [c for c in clients if c]
                    time.sleep(3)  # let memory settle
                    m = measure(clients, self.daemon_pid if mode == 'mcpcell' else None)
                    [c.close() for c in clients]
                    per_server = {}
                    for i, r in enumerate(ready):
                        per_server.setdefault(self.cfg['servers'][i % len(self.cfg['servers'])]['name'], []).append(r)
                    rows.append({'n': n, 'rep': rep, 'mode': mode, 'mb': m['mb'], 'processes': m['processes'], 'names': m['names'],
                                 'allReady': all_ready, 'ready': per_server, 'errors': errors})
                    print(f'  scaling n={n} rep={rep} {mode:8} {m["mb"]:7.0f} MB {m["processes"]:3d} procs '
                          f'all ready {all_ready:5.1f}s errors={len(errors)}', flush=True)
                    time.sleep(2)
        return rows

    def latency(self, reps):
        rows = []
        for server in self.cfg['servers']:
            tool = server.get('tool')
            if not tool:
                continue
            calls = server.get('calls', 30)
            for variant in ('native', 'mcpcell-cold', 'mcpcell-warm'):
                mode = 'native' if variant == 'native' else 'mcpcell'
                keep = None
                if variant == 'mcpcell-warm':
                    # Another session keeps the instance running (shared
                    # services only; a per-session instance is never warm
                    # for a new session).
                    keep = Client(self.argv(server, mode), self.env(server, mode))
                    keep.ready()
                    keep.call(tool)
                    if not self.shared_and_running(server):
                        keep.close()
                        continue
                firsts, rtts, err = [], [], ''
                for rep in range(reps):
                    if variant == 'mcpcell-cold':
                        self.stop_all()
                    c = Client(self.argv(server, mode), self.env(server, mode))
                    try:
                        c.ready()
                        c.call(tool)
                        firsts.append(time.perf_counter() - c.t0)
                        if rep == 0:
                            rtts = [c.call(tool) for _ in range(calls)]
                    except Exception as e:
                        err = str(e)
                    finally:
                        c.close()
                if keep:
                    keep.close()
                rows.append({'server': server['name'], 'variant': variant, 'first': firsts, 'rtt': rtts, 'error': err})
                med = statistics.median(rtts) * 1000 if rtts else float('nan')
                print(f'  latency {server["name"]:20} {variant:13} first {statistics.mean(firsts) if firsts else float("nan"):6.2f}s '
                      f'rtt median {med:7.1f} ms {err[:60]}', flush=True)
        return rows


def main():
    cfg = json.load(open(sys.argv[1], encoding='utf-8'))
    out_path = sys.argv[2]
    quick = '--quick' in sys.argv
    b = Bench(cfg)
    print('warming up: npm caches for the native runs, catalogs for MCPcell', flush=True)
    for mode in ('native', 'mcpcell'):
        cs = b.session(mode)
        for c in cs:
            try:
                c.ready()
            except Exception as e:
                print('  warm-up', mode, e, flush=True)
        [c.close() for c in cs]
    b.stop_all()
    result = {'started': time.strftime('%Y-%m-%d %H:%M:%S'), 'servers': [s['name'] for s in cfg['servers']]}
    if '--readiness' in sys.argv:  # one session, more repetitions
        result['readiness'] = b.scaling([1], 5)
    else:
        result['scaling'] = b.scaling([1, 2] if quick else [1, 2, 3, 4], 1 if quick else 3)
        result['latency'] = b.latency(2 if quick else 5)
    json.dump(result, open(out_path, 'w', encoding='utf-8'), ensure_ascii=False, indent=1)
    print('wrote', out_path)


if __name__ == '__main__':
    main()
