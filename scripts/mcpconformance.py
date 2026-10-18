"""Runs the official MCP conformance suite (@modelcontextprotocol/conformance)
against a server natively and through MCPcell, and compares what passes.

The suite tests servers over Streamable HTTP and sends no credentials, so the
MCPcell side goes through a small proxy that adds the bearer token and maps
/mcp to /s/<service>. Loopback Host and Origin values are rewritten to the
daemon's port; anything else passes unchanged, so the DNS-rebinding scenario
still reaches MCPcell's own check.

    python scripts/mcpconformance.py <conformance repo at the suite's tag> <MCPCELL_HOME> <out dir>

The repo's examples/servers/typescript must have its dependencies installed
and an everything-stdio.ts: everything-server.ts up to its HTTP part, logging
to stderr, ending with createMcpServer().connect(new StdioServerTransport()).
"""
import http.client, http.server, json, os, socketserver, subprocess, sys, threading, time, urllib.request

SUITE = '@modelcontextprotocol/conformance@0.1.16'
NATIVE_PORT = 3011
HOP = {'connection', 'keep-alive', 'transfer-encoding', 'te', 'trailer', 'upgrade', 'proxy-authorization', 'content-length'}


def proxy(listen, daemon_port, service, token):
    class Handler(http.server.BaseHTTPRequestHandler):
        protocol_version = 'HTTP/1.0'  # responses end when the connection closes, so SSE streams through

        def log_message(self, *a):
            pass

        def fix(self, v):
            for h in ('127.0.0.1', 'localhost', '[::1]'):
                for pre in ('', 'http://'):
                    if v.lower() == f'{pre}{h}:{listen}':
                        return f'{pre}{h}:{daemon_port}'
            return v

        def forward(self):
            n = int(self.headers.get('Content-Length') or 0)
            body = self.rfile.read(n) if n else None
            headers = {k: (self.fix(v) if k.lower() in ('host', 'origin') else v)
                       for k, v in self.headers.items() if k.lower() not in HOP}
            headers['Authorization'] = 'Bearer ' + token
            path = self.path.replace('/mcp', '/s/' + service, 1)
            conn = http.client.HTTPConnection('127.0.0.1', daemon_port, timeout=600)
            conn.putrequest(self.command, path, skip_host=True, skip_accept_encoding=True)
            for k, v in headers.items():
                conn.putheader(k, v)
            if body is not None:
                conn.putheader('Content-Length', str(len(body)))
            conn.endheaders(body)
            r = conn.getresponse()
            self.send_response(r.status, r.reason)
            for k, v in r.getheaders():
                if k.lower() not in HOP:
                    self.send_header(k, v)
            if r.getheader('Content-Length'):
                self.send_header('Content-Length', r.getheader('Content-Length'))
            self.end_headers()
            while chunk := r.read1(65536):
                self.wfile.write(chunk)
                self.wfile.flush()
            conn.close()

        do_GET = do_POST = do_DELETE = do_OPTIONS = forward

    class Server(socketserver.ThreadingMixIn, http.server.HTTPServer):
        daemon_threads = True

    srv = Server(('127.0.0.1', listen), Handler)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv


def api(base, token, method, path, body=None):
    req = urllib.request.Request(base + path, method=method, data=json.dumps(body).encode() if body else None,
                                 headers={'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json'})
    return urllib.request.urlopen(req, timeout=60).read()


def suite(url, out):
    os.makedirs(out, exist_ok=True)
    p = subprocess.run(['cmd', '/c', 'npx', '-y', SUITE, 'server', '--url', url, '-o', out],
                       capture_output=True, text=True, encoding='utf-8', errors='replace', timeout=1800)
    open(os.path.join(out, 'stdout.txt'), 'w', encoding='utf-8').write(p.stdout + '\n--- stderr\n' + p.stderr)
    return p.stdout


def main():
    sys.stdout.reconfigure(encoding='utf-8', errors='replace')
    repo, home, out = sys.argv[1:4]
    srv_dir = os.path.join(repo, 'examples', 'servers', 'typescript')
    tsx = os.path.join(srv_dir, 'node_modules', 'tsx', 'dist', 'cli.mjs')
    token = open(os.path.join(home, 'token'), encoding='utf-8').read().strip()
    port = json.load(open(os.path.join(home, 'daemon.json'), encoding='utf-8'))['port']
    base = f'http://127.0.0.1:{port}'
    targets = {'native': f'http://localhost:{NATIVE_PORT}/mcp'}
    for i, mode in enumerate(('shared', 'per-session')):
        name = 'conformance-' + mode
        api(base, token, 'PUT', '/api/services/' + name, {'name': name, 'command': 'node', 'args': [tsx, 'everything-stdio.ts'],
                                                    'cwd': srv_dir, 'mode': mode, 'catalog': True, 'lean': True, 'env': {}})
        proxy(NATIVE_PORT + 1 + i, port, name, token)
        targets[mode] = f'http://localhost:{NATIVE_PORT + 1 + i}/mcp'
    native = subprocess.Popen(['node', tsx, 'everything-server.ts'], cwd=srv_dir, env={**os.environ, 'PORT': str(NATIVE_PORT)},
                              stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    time.sleep(8)
    try:
        for name, url in targets.items():
            print('==', name, url, flush=True)
            text = suite(url, os.path.join(out, name))
            print('\n'.join(l for l in text.splitlines() if l.strip())[-3000:], flush=True)
    finally:
        subprocess.run(['taskkill', '/T', '/F', '/PID', str(native.pid)], capture_output=True)


if __name__ == '__main__':
    main()
