"""Compatibility test: MCP servers run natively vs through MCPcell.

Every server runs the same client script natively and through mcpcell-run,
and the transcripts are compared: the initialize result, every list (all
pages merged), tool call results, the requests the server sends the client
(roots, sampling, elicitation) and the notifications it sends.

    python scripts/mcpcompat.py <corpus.json> <out.json> [--discover] [--only a,b] [--jobs N]

corpus.json: {"wrapper": "<mcpcell-run.exe>", "home": "<MCPCELL_HOME of a
running test daemon>", "dirs": {"W1": ..., "W2": ..., "DATA": ...},
"servers": [{"name", "group", "argv", "env", "calls": [{"name",
"arguments", "meta"}], "pair": {"a": [calls], "b": [calls]}, "reset": [paths]}]}.
"reset" lists files a server keeps state in; they are deleted before every
scenario. {W1}, {W2}, {DATA} (any key of dirs) in any string are replaced by
the dirs. Servers of one group run one
after another (variants of one package), groups run in parallel. The daemon
must not be the user's.

Scenarios per server:
  native       at W1; calls run twice to tell nondeterministic results apart
  native-pair  two overlapping native sessions (pair.a, then pair.b)
  native-old   at W1 asking for an older protocol version (initialize, tools)
  cold         through MCPcell on first use, before anything is cached
  warm         through MCPcell with the catalog cached and the instance stopped
  pair         two overlapping sessions through MCPcell
  other-dir    through MCPcell at W2 with the catalog learned at W1
  old          through MCPcell asking for the older protocol version
--discover runs only "native" without calls and prints the tools.
"""
import argparse, json, os, queue, shutil, subprocess, sys, threading, time, urllib.parse, urllib.request
from concurrent.futures import ThreadPoolExecutor

CREATE_NO_WINDOW = 0x08000000
PROTO, OLD = '2025-11-25', '2025-03-26'
START, CALL = 300, 120
LISTS = (('tools/list', 'tools', 'tools'), ('prompts/list', 'prompts', 'prompts'),
         ('resources/list', 'resources', 'resources'), ('resources/templates/list', 'resourceTemplates', 'resources'))
print_lock = threading.Lock()


def say(*a):
    with print_lock:
        print(*a, flush=True)


def canon(v):
    return json.dumps(v, sort_keys=True, ensure_ascii=False)


def file_uri(path):
    return 'file:///' + urllib.parse.quote(path.replace('\\', '/').lstrip('/'), safe='/:')


class Client:
    """One stdio MCP client that answers what a full-featured agent would."""

    def __init__(self, argv, env, cwd):
        self.cwd = cwd
        self.p = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                  env=env, cwd=cwd, creationflags=CREATE_NO_WINDOW if os.name == 'nt' else 0)
        self.lock = threading.Lock()
        self.waiting, self.next = {}, 0
        self.requests, self.notes, self.stderr, self.nonjson = [], [], [], 0
        self.done = threading.Event()
        threading.Thread(target=self._out, daemon=True).start()
        threading.Thread(target=self._err, daemon=True).start()

    def _send(self, msg):
        try:
            with self.lock:
                self.p.stdin.write((json.dumps(msg) + '\n').encode())
                self.p.stdin.flush()
        except OSError:
            pass

    def _out(self):
        for line in self.p.stdout:
            line = line.strip()
            if not line:
                continue
            try:
                m = json.loads(line)
            except ValueError:
                self.nonjson += 1
                continue
            for m in m if isinstance(m, list) else [m]:
                if 'method' in m and 'id' in m:
                    self._serve(m)
                elif 'method' in m:
                    self.notes.append(m['method'])
                elif (q := self.waiting.get(m.get('id'))) is not None:
                    q.put(m)
        self.done.set()

    def _serve(self, m):
        self.requests.append(m['method'])
        result = {'roots/list': {'roots': [{'uri': file_uri(self.cwd), 'name': 'work'}]},
                  'sampling/createMessage': {'role': 'assistant', 'content': {'type': 'text', 'text': 'sampled'},
                                             'model': 'mcpcompat', 'stopReason': 'endTurn'},
                  'elicitation/create': {'action': 'decline'},
                  'ping': {}}.get(m['method'])
        if result is None:
            self._send({'jsonrpc': '2.0', 'id': m['id'], 'error': {'code': -32601, 'message': 'not supported'}})
        else:
            self._send({'jsonrpc': '2.0', 'id': m['id'], 'result': result})

    def _err(self):
        for line in self.p.stderr:
            if len(self.stderr) < 200:
                self.stderr.append(line.decode('utf-8', 'replace').rstrip())

    def rpc(self, method, params, timeout):
        self.next += 1
        i, q = self.next, queue.Queue()
        self.waiting[i] = q
        self._send({'jsonrpc': '2.0', 'id': i, 'method': method, 'params': params})
        deadline = time.perf_counter() + timeout
        try:
            while time.perf_counter() < deadline:
                try:
                    m = q.get(timeout=0.25)
                    return {'error': m['error']} if 'error' in m else {'result': m.get('result')}
                except queue.Empty:
                    if self.done.is_set() and q.empty():
                        return {'eof': True}
            return {'timeout': timeout}
        finally:
            del self.waiting[i]

    def notify(self, method):
        self._send({'jsonrpc': '2.0', 'method': method})

    def close(self):
        try:
            self.p.stdin.close()
        except OSError:
            pass
        try:
            self.p.wait(timeout=15)
            return 'exited'
        except subprocess.TimeoutExpired:
            subprocess.run(['taskkill', '/T', '/F', '/PID', str(self.p.pid)], capture_output=True)
            return 'killed'


def list_all(c, method, key):
    items, cursor, pages = [], None, 0
    while True:
        r = c.rpc(method, {'cursor': cursor} if cursor else {}, CALL)
        if 'result' not in r:
            return r if not pages else {'result': items, 'pages': pages, 'broken': r}
        items += r['result'].get(key) or []
        pages += 1
        cursor = r['result'].get('nextCursor')
        if not cursor or pages >= 100:
            return {'result': items, 'pages': pages}


def call_params(x):
    p = {'name': x['name'], 'arguments': x.get('arguments', {})}
    if 'meta' in x:
        p['_meta'] = x['meta']
    return p


def open_session(c, proto, calls, lists=True, repeat=False):
    """Everything up to closing: initialize, lists, calls."""
    rec, t0 = {}, time.perf_counter()
    rec['init'] = c.rpc('initialize', {'protocolVersion': proto, 'clientInfo': {'name': 'mcpcompat', 'version': '1'},
                                       'capabilities': {'roots': {'listChanged': True}, 'sampling': {}, 'elicitation': {}}},
                        START)
    rec['ready'] = round(time.perf_counter() - t0, 3)
    if 'result' not in rec['init']:
        return rec
    c.notify('notifications/initialized')
    caps = rec['init']['result'].get('capabilities') or {}
    rec['lists'] = {m: list_all(c, m, key) for m, key, cap in LISTS if lists and (cap in caps or m == 'tools/list')}
    rec['calls'] = [c.rpc('tools/call', call_params(x), CALL) for x in calls]
    if repeat:
        rec['calls2'] = [c.rpc('tools/call', call_params(x), CALL) for x in calls]
    return rec


def close_session(c, rec):
    time.sleep(1)  # trailing notifications and server requests
    rec['requests'] = sorted(set(c.requests))
    rec['notifications'] = sorted(set(c.notes))
    rec['via'] = any(l.startswith('[mcpcell]') for l in c.stderr)
    rec['stderr'] = [l for l in c.stderr if not l.startswith('[mcpcell]')][:40]
    rec['nonjson'] = c.nonjson
    rec['exit'] = c.close()
    return rec


def fresh(paths):
    for p in paths:
        if os.path.isdir(p):
            shutil.rmtree(p, ignore_errors=True)
        elif os.path.exists(p):
            try:
                os.remove(p)
            except OSError as e:
                say('  cannot reset', p, e)


def _run_one(argv, env, cwd, proto, calls, lists=True, repeat=False):
    c = Client(argv, env, cwd)
    return close_session(c, open_session(c, proto, calls, lists, repeat))


def _run_pair(argv, env, cwd, a, b):
    ca = Client(argv, env, cwd)
    ra = open_session(ca, PROTO, a)
    cb = Client(argv, env, cwd)
    rb = open_session(cb, PROTO, b)
    return {'a': close_session(ca, ra), 'b': close_session(cb, rb)}


# --- comparison ---

def shape(r):
    """What must match even when the content varies between runs."""
    if 'result' not in r:
        return ('fail', next(iter(r)), (r.get('error') or {}).get('code'))
    res = r['result'] or {}
    if isinstance(res, list):  # a merged list
        return ('list', len(res))
    return ('ok', bool(res.get('isError')), tuple(x.get('type') for x in res.get('content') or []))


def names(items):
    return [x.get('name') or x.get('uri') or x.get('uriTemplate') for x in items]


def compare(scenario, nat, mc, nondet=()):
    """Differences of an MCPcell record from the native one: (scenario, item, severity, detail)."""
    out = []

    def d(item, sev, detail):
        out.append({'scenario': scenario, 'item': item, 'severity': sev, 'detail': detail})

    if 'result' not in mc.get('init', {}):
        d('initialize', 'break', f'MCPcell 失败：{canon(mc.get("init"))[:200]}')
        return out
    ni, mi = nat['init']['result'], mc['init']['result']
    for k in sorted(set(ni) | set(mi)):
        if canon(ni.get(k)) != canon(mi.get(k)):
            d('initialize.' + k, 'break', f'原生 {canon(ni.get(k))[:160]} / MCPcell {canon(mi.get(k))[:160]}')
    for m in sorted(set(nat.get('lists', {})) | set(mc.get('lists', {}))):
        a, b = nat.get('lists', {}).get(m, {}), mc.get('lists', {}).get(m, {})
        if 'result' not in a or 'result' not in b:
            if canon(shape(a)) != canon(shape(b)):
                d(m, 'break', f'原生 {canon(a)[:120]} / MCPcell {canon(b)[:120]}')
            continue
        if canon(a['result']) == canon(b['result']):
            continue
        if sorted(map(canon, a['result'])) == sorted(map(canon, b['result'])):
            d(m, 'note', '内容相同，顺序不同')
            continue
        na, nb = names(a['result']), names(b['result'])
        if na == nb:
            d(m, 'break', '名字相同，定义不同：' + ', '.join(str(n) for x, y, n in zip(a['result'], b['result'], na) if canon(x) != canon(y))[:200])
        else:
            d(m, 'break', f'原生 {len(na)} 个 / MCPcell {len(nb)} 个；只在原生：{sorted(set(map(str, na)) - set(map(str, nb)))[:8]}；'
                          f'只在 MCPcell：{sorted(set(map(str, nb)) - set(map(str, na)))[:8]}')
    for i, (a, b) in enumerate(zip(nat.get('calls', []), mc.get('calls', []))):
        if canon(a) == canon(b):
            continue
        same_shape = canon(shape(a)) == canon(shape(b))
        if i in nondet and same_shape:
            continue
        d(f'call#{i}', 'note' if same_shape else 'break',
          ('内容不同（同样的结构）：' if same_shape else '') + f'原生 {canon(a)[:150]} / MCPcell {canon(b)[:150]}')
    for key, sev in (('requests', 'break'), ('notifications', 'note')):
        if set(nat.get(key, [])) != set(mc.get(key, [])):
            d(key, sev, f'原生 {nat.get(key)} / MCPcell {mc.get(key)}')
    if nat.get('nonjson', 0) != mc.get('nonjson', 0):
        d('stdout', 'note', f'非 JSON 行：原生 {nat.get("nonjson")} / MCPcell {mc.get("nonjson")}')
    if mc.get('exit') != 'exited':
        d('exit', 'break', '关闭 stdin 后 15 秒内没有退出')
    return out


def nondeterministic(rec):
    return {i for i, (a, b) in enumerate(zip(rec.get('calls', []), rec.get('calls2', []))) if canon(a) != canon(b)}


# --- daemon ---

class Daemon:
    def __init__(self, home):
        self.token = open(os.path.join(home, 'token'), encoding='utf-8').read().strip()
        info = json.load(open(os.path.join(home, 'daemon.json'), encoding='utf-8'))
        self.base = f'http://127.0.0.1:{info["port"]}'

    def api(self, method, path):
        req = urllib.request.Request(self.base + path, method=method, headers={'Authorization': 'Bearer ' + self.token})
        return json.load(urllib.request.urlopen(req, timeout=60))

    def find(self, argv):
        """The run- service the wrapper created for argv: (name, mode)."""
        for s in self.api('GET', '/api/status')['services']:
            parts = s['command'].split(' ')
            if s['name'].startswith('run-') and len(parts) == len(argv) and all(
                    p == a or p.endswith('***') for p, a in zip(parts, argv)):
                return s['name'], s['mode']
        return None, None

    def stop(self, name):
        if name:
            try:
                self.api('POST', f'/api/services/{urllib.parse.quote(name)}/stop')
            except Exception:
                pass
            time.sleep(0.5)


# --- driver ---

def expand(v, dirs):
    if isinstance(v, str):
        for k, p in dirs.items():
            v = v.replace('{' + k + '}', p)
        return v
    if isinstance(v, list):
        return [expand(x, dirs) for x in v]
    if isinstance(v, dict):
        return {k: expand(x, dirs) for k, x in v.items()}
    return v


def trim(v, n=2000):
    """Keeps records readable: long strings cut, compared before trimming."""
    if isinstance(v, str):
        return v if len(v) <= n else v[:n] + f'...[{len(v)} chars]'
    if isinstance(v, list):
        return [trim(x, n) for x in v]
    if isinstance(v, dict):
        return {k: trim(x, n) for k, x in v.items()}
    return v


class Runner:
    def __init__(self, cfg, discover):
        self.cfg, self.discover = cfg, discover
        self.dirs = cfg['dirs']
        self.daemon = None if discover else Daemon(cfg['home'])

    def env(self, server, wrapped):
        env = dict(os.environ)
        for k in ('CLAUDE_CONFIG_DIR', 'CODEX_HOME', 'MCPCELL_HOME'):
            env.pop(k, None)
        env.update(expand(server.get('env', {}), self.dirs))
        if wrapped:
            env['MCPCELL_HOME'] = self.cfg['home']
        return env

    def server(self, s):
        argv = expand(s['argv'], self.dirs)
        native = [shutil.which(argv[0]) or argv[0], *argv[1:]]  # agents resolve PATHEXT themselves
        wrapped = [self.cfg['wrapper'], '--', *argv]
        calls = expand(s.get('calls', []), self.dirs)
        pair = expand(s.get('pair') or {'a': calls, 'b': calls}, self.dirs)
        ne, we = self.env(s, False), self.env(s, True)
        w1, w2 = self.dirs['W1'], self.dirs['W2']
        reset = expand(s.get('reset', []), self.dirs)
        out = {'name': s['name'], 'group': s.get('group', s['name']), 'argv': s['argv'], 'kind': s.get('kind', ''),
               'scenarios': {}, 'diffs': []}
        sc, t0 = out['scenarios'], time.perf_counter()

        def run_one(*a, **k):
            fresh(reset)
            return _run_one(*a, **k)

        def run_pair(*a):
            fresh(reset)
            return _run_pair(*a)

        sc['native'] = run_one(native, ne, w1, PROTO, [] if self.discover else calls, repeat=True)
        if 'result' not in sc['native'].get('init', {}):
            out['verdict'] = 'native-fail'
            out['why'] = canon(sc['native'].get('init'))[:200] + ' | ' + ' / '.join(sc['native'].get('stderr', [])[-3:])[:300]
            return out
        if self.discover:
            out['verdict'] = 'discovered'
            return out
        nondet = nondeterministic(sc['native'])
        sc['native-pair'] = run_pair(native, ne, w1, pair['a'], pair['b'])
        sc['native-old'] = run_one(native, ne, w1, OLD, [])
        sc['cold'] = run_one(wrapped, we, w1, PROTO, calls)
        name, mode = self.daemon.find(argv)
        out['service'], out['mode'] = name, mode
        self.daemon.stop(name)
        sc['warm'] = run_one(wrapped, we, w1, PROTO, calls)
        self.daemon.stop(name)
        sc['pair'] = run_pair(wrapped, we, w1, pair['a'], pair['b'])
        self.daemon.stop(name)
        sc['other-dir'] = run_one(wrapped, we, w2, PROTO, calls)
        self.daemon.stop(name)
        sc['old'] = run_one(wrapped, we, w1, OLD, [])
        self.daemon.stop(name)

        diffs = out['diffs']
        diffs += compare('cold', sc['native'], sc['cold'], nondet)
        diffs += compare('warm', sc['native'], sc['warm'], nondet)
        diffs += compare('pair.a', sc['native-pair']['a'], sc['pair']['a'], nondet)
        diffs += compare('pair.b', sc['native-pair']['b'], sc['pair']['b'], nondet)
        other = compare('other-dir', sc['native'], sc['other-dir'], nondet)
        if any(x['severity'] == 'break' for x in other):
            # Maybe the server itself behaves differently there.
            sc['native-other-dir'] = run_one(native, ne, w2, PROTO, calls)
            other = compare('other-dir', sc['native-other-dir'], sc['other-dir'], nondet)
        diffs += other
        diffs += compare('old', sc['native-old'], sc['old'])
        out['fallback'] = not sc['cold'].get('via')
        out['verdict'] = 'diff' if any(x['severity'] == 'break' for x in diffs) else 'same'
        out['seconds'] = round(time.perf_counter() - t0, 1)
        return out

    def group(self, servers):
        results = []
        for s in servers:
            try:
                r = self.server(s)
            except Exception as e:
                r = {'name': s['name'], 'verdict': 'harness-error', 'why': repr(e)[:300]}
            results.append(r)
            report(r)
        return results


def report(r):
    v = r['verdict']
    if v == 'discovered':
        tools = r['scenarios']['native']['lists'].get('tools/list', {}).get('result', [])
        init = r['scenarios']['native']['init']['result']
        say(f'== {r["name"]}: {init.get("protocolVersion")} {canon(init.get("serverInfo"))} caps={sorted(init.get("capabilities") or {})} '
            f'{len(tools)} tools, ready {r["scenarios"]["native"]["ready"]}s')
        for t in tools[:40]:
            req = (t.get('inputSchema') or {}).get('required') or []
            say(f'     {t["name"]}({", ".join(req)})')
        return
    if v in ('native-fail', 'harness-error'):
        say(f'-- {r["name"]}: {v}: {r.get("why")}')
        return
    breaks = [x for x in r['diffs'] if x['severity'] == 'break']
    notes = [x for x in r['diffs'] if x['severity'] == 'note']
    tools = len(r['scenarios']['native']['lists'].get('tools/list', {}).get('result', []))
    say(f'{"OK" if v == "same" else "!!"} {r["name"]}: {v} mode={r.get("mode")} fallback={r.get("fallback")} tools={tools} '
        f'breaks={len(breaks)} notes={len(notes)} {r.get("seconds")}s')
    for x in breaks[:12]:
        say(f'     [{x["scenario"]}] {x["item"]}: {x["detail"][:260]}')


def main():
    sys.stdout.reconfigure(encoding='utf-8', errors='replace')
    ap = argparse.ArgumentParser()
    ap.add_argument('corpus')
    ap.add_argument('out')
    ap.add_argument('--discover', action='store_true')
    ap.add_argument('--only')
    ap.add_argument('--jobs', type=int, default=3)
    a = ap.parse_args()
    cfg = json.load(open(a.corpus, encoding='utf-8'))
    servers = cfg['servers']
    if a.only:
        keep = set(a.only.split(','))
        servers = [s for s in servers if s['name'] in keep]
    groups = {}
    for s in servers:
        groups.setdefault(s.get('group', s['name']), []).append(s)
    runner = Runner(cfg, a.discover)
    started = time.strftime('%Y-%m-%d %H:%M:%S')
    with ThreadPoolExecutor(a.jobs) as ex:
        results = [r for rs in ex.map(runner.group, groups.values()) for r in rs]
    counts = {}
    for r in results:
        counts[r['verdict']] = counts.get(r['verdict'], 0) + 1
    json.dump({'started': started, 'proto': PROTO, 'old': OLD, 'counts': counts, 'results': trim(results)},
              open(a.out, 'w', encoding='utf-8'), ensure_ascii=False, indent=1)
    say('counts:', counts, '->', a.out)


if __name__ == '__main__':
    main()
