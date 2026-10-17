"""Loopback-only authenticated management API and bounded static workbench."""
from __future__ import annotations

from concurrent.futures import ThreadPoolExecutor
from http.cookies import SimpleCookie, CookieError
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
from urllib.parse import urlsplit, parse_qs
import json
import secrets
import threading
import time

from .discovery import Discovery, plan
from .management import Manager
from .runtime import Runtime
from .storage import InstanceLock, Conflict
from .agents import AgentHub
from .clients import candidates

WEB = Path(__file__).with_name("web")
ASSETS = {"/hub.css": ("hub.css", "text/css; charset=utf-8"), "/hub.js": ("hub.js", "text/javascript; charset=utf-8"), "/": ("index.html", "text/html; charset=utf-8"),
          "/app.js": ("app.js", "text/javascript; charset=utf-8"),
          "/style.css": ("style.css", "text/css; charset=utf-8"),
          "/vendor/terminal.js": ("vendor/terminal.js", "text/javascript; charset=utf-8"),
          "/vendor/terminal.css": ("vendor/terminal.css", "text/css; charset=utf-8")}
CSP = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"


class Workbench(HTTPServer):
    """Fixed worker pool with no unbounded request/thread queue."""
    allow_reuse_address = True
    request_queue_size = 8

    def __init__(self, scanner: Discovery, port: int = 0, state_dir: Path | None = None, agent_environ=None):
        self.scanner = scanner
        self.token = secrets.token_urlsafe(32)
        self.session = secrets.token_urlsafe(32)
        self.lock = threading.Lock()
        self.state_dir = state_dir or (scanner.home / ".mcpcell")
        self.instance_lock = InstanceLock(self.state_dir)
        self.manager = Manager(scanner, self.state_dir)
        self.runtime = Runtime(self.manager)
        self.manager.runtime = self.runtime
        self.agent_hub = AgentHub(scanner.home, self.manager.vault, demo=scanner.demo, environ=agent_environ)
        self.snapshot = self.manager.scan()
        self.last_scan = time.monotonic()
        self.pool = ThreadPoolExecutor(max_workers=8, thread_name_prefix="mcpcell-http")
        self.slots = threading.BoundedSemaphore(8)
        try:
            super().__init__(("127.0.0.1", port), Handler)
        except Exception:
            self.agent_hub.close(); self.runtime.close(); self.instance_lock.close(); self.pool.shutdown(wait=False)
            raise
        self.authority = f"127.0.0.1:{self.server_port}"
        self.origin = f"http://{self.authority}"
        self.cookie_name = f"mcpcell_{self.server_port}"
        self.manager.vault.put("endpoint.bin", {"brokerPort": self.runtime.server_address[1], "url": self.origin + "/#token=" + self.token})

    def process_request(self, request, client_address):
        if not self.slots.acquire(blocking=False):
            self.shutdown_request(request)
            return
        request.settimeout(5)
        try:
            self.pool.submit(self.process_bounded, request, client_address)
        except RuntimeError:
            self.slots.release()
            self.shutdown_request(request)

    def process_bounded(self, request, address):
        try:
            self.finish_request(request, address)
        except (OSError, ValueError, RuntimeError):
            pass
        finally:
            self.shutdown_request(request)
            self.slots.release()

    def server_close(self):
        super().server_close()
        self.pool.shutdown(wait=True, cancel_futures=True)
        self.agent_hub.close()
        self.runtime.close()
        self.instance_lock.close()


class Handler(BaseHTTPRequestHandler):
    server_version = "MCPcell"
    sys_version = ""
    protocol_version = "HTTP/1.0"

    def log_message(self, fmt, *args):
        # No request URL, authorization header or config content in stdout.
        return

    def send_payload(self, status: int, payload: bytes, mime="application/json; charset=utf-8", cookie=False):
        self.send_response(status)
        self.send_header("Content-Type", mime)
        self.send_header("Content-Length", str(len(payload)))
        self.send_header("Cache-Control", "no-store")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.send_header("Referrer-Policy", "no-referrer")
        self.send_header("Content-Security-Policy", CSP)
        self.send_header("X-Frame-Options", "DENY")
        self.send_header("Cross-Origin-Resource-Policy", "same-origin")
        if cookie:
            self.send_header("Set-Cookie", f"{self.server.cookie_name}={self.server.session}; HttpOnly; SameSite=Strict; Path=/")
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(payload)

    def reply(self, status: int, value: object, **kwargs):
        self.send_payload(status, json.dumps(value, ensure_ascii=False, allow_nan=False).encode(), **kwargs)

    def boundary(self) -> bool:
        hosts = self.headers.get_all("Host", [])
        if hosts != [self.server.authority]:
            self.reply(403, {"error": "host_rejected"})
            return False
        origins = self.headers.get_all("Origin", [])
        if origins and origins != [self.server.origin]:
            self.reply(403, {"error": "origin_rejected"})
            return False
        if self.headers.get("Sec-Fetch-Site") == "cross-site":
            self.reply(403, {"error": "cross_site_rejected"})
            return False
        return True

    def authenticated(self) -> bool:
        auth = self.headers.get("Authorization", "")
        if secrets.compare_digest(auth.encode("utf-8"), f"Bearer {self.server.token}".encode()):
            return True
        try:
            cookie = SimpleCookie(self.headers.get("Cookie", ""))
            value = cookie.get(self.server.cookie_name)
            return value is not None and secrets.compare_digest(value.value.encode("utf-8"), self.server.session.encode())
        except (CookieError, TypeError):
            return False

    def do_GET(self):
        if not self.boundary():
            return
        path = urlsplit(self.path).path
        if path in ASSETS:
            file, mime = ASSETS[path]
            if not (WEB / file).is_file():
                self.reply(404, {"error":"terminal_assets_missing"}); return
            self.send_payload(200, (WEB / file).read_bytes(), mime)
            return
        if not path.startswith("/api/"):
            self.reply(404, {"error": "not_found"})
            return
        if not self.authenticated():
            self.reply(401, {"error": "open_authenticated_launch_url"})
            return
        if path == "/api/snapshot":
            snapshot = dict(self.server.snapshot)
            snapshot["runtime"] = self.server.runtime.status()
            snapshot["hub"] = self.server.agent_hub.snapshot()
            self.reply(200, snapshot)
        elif path == "/api/runtime":
            self.reply(200, self.server.runtime.status())
        elif path == "/api/agents":
            self.reply(200, self.server.agent_hub.snapshot())
        elif path == "/api/agents/output":
            try:
                query = parse_qs(urlsplit(self.path).query, max_num_fields=4)
                if set(query) != {'sessionId','offset'} or any(len(v)!=1 for v in query.values()):
                    raise Conflict('输出请求无效')
                session = self.server.agent_hub.get_session(query['sessionId'][0])
                self.reply(200, {**session.output.read(int(query['offset'][0])), 'session':session.snapshot()})
            except (Conflict, ValueError) as exc:
                self.reply(409, {'error':str(exc) if isinstance(exc,Conflict) else '输出位置无效'})
        elif path == "/api/plan":
            self.reply(200, plan(self.server.snapshot))
        else:
            self.reply(404, {"error": "not_found"})

    def do_POST(self):
        if not self.boundary():
            return
        if not self.authenticated():
            self.reply(401, {"error": "authentication_required"})
            return
        if self.headers.get("Content-Type", "").split(";")[0] != "application/json":
            self.reply(415, {"error": "json_required"})
            return
        if self.headers.get("Transfer-Encoding"):
            self.reply(400, {"error": "transfer_encoding_not_supported"})
            return
        lengths = self.headers.get_all("Content-Length", [])
        try:
            size = int(lengths[0]) if len(lengths) == 1 else -1
        except ValueError:
            size = -1
        path = urlsplit(self.path).path
        max_body = 8192 if path in {"/api/enroll", "/api/toggle", "/api/restore", "/api/stop"} else 1024
        if path.startswith("/api/agents/"): max_body = 16384
        if size < 0 or size > max_body:
            self.reply(413, {"error": "body_limit"})
            return
        try:
            body = json.loads(self.rfile.read(size))
            if not isinstance(body, dict): raise ValueError()
            schemas = {"/api/enroll": {"ids", "confirm"}, "/api/toggle": {"bindingId", "enabled"},
                       "/api/restore": {"bindingId"}, "/api/stop": {"bindingId"}}
            schemas.update({
                "/api/agents/enroll":{"ids","confirm"},
                "/api/agents/register":{"name","executable","args","confirm"},
                "/api/agents/update":{"agentId","enabled","pinned"},
                "/api/agents/remove":{"agentId","confirm"},
                "/api/agents/launch":{"agentId","cwd","requestId","confirm"},
                "/api/agents/input":{"sessionId","data"},
                "/api/agents/resize":{"sessionId","cols","rows"},
                "/api/agents/stop":{"sessionId","confirm"},
                "/api/agents/close":{"sessionId","confirm"},
                "/api/agents/rename":{"sessionId","title"},
                "/api/detection/scope":{"workspace","confirm"},
            })
            if set(body) != schemas.get(path, set()): raise ValueError()
        except (ValueError, UnicodeError):
            self.reply(400, {"error": "请求内容无效"})
            return
        if path.startswith('/api/agents/'):
            try:
                if 'confirm' in body and body['confirm'] is not True:
                    raise Conflict('请确认此操作')
                hub=self.server.agent_hub
                if path == '/api/agents/detect': result=hub.detect()
                elif path == '/api/agents/enroll': result=hub.enroll(body['ids'])
                elif path == '/api/agents/register': result=hub.register(body['name'],body['executable'],body['args'])
                elif path == '/api/agents/update': result=hub.update(body['agentId'],body['enabled'],body['pinned'])
                elif path == '/api/agents/remove': result=hub.remove(body['agentId'])
                elif path == '/api/agents/launch': result={'session':hub.launch(body['agentId'],body['cwd'],body['requestId']), 'hub':hub.snapshot()}
                elif path == '/api/agents/input':
                    hub.get_session(body['sessionId']).write(body['data']); result={'ok':True}
                elif path == '/api/agents/resize':
                    hub.get_session(body['sessionId']).resize(body['cols'],body['rows']); result={'ok':True}
                elif path == '/api/agents/stop':
                    hub.get_session(body['sessionId']).stop(); result=hub.snapshot()
                elif path == '/api/agents/close':
                    hub.close_session(body['sessionId']); result=hub.snapshot()
                elif path == '/api/agents/rename':
                    title=body['title']
                    if not isinstance(title,str) or not title.strip() or len(title)>80 or not title.isprintable():
                        raise Conflict('会话名称需为 1–80 个可见字符')
                    hub.get_session(body['sessionId']).title=title.strip(); result=hub.snapshot()
                else:
                    self.reply(404,{'error':'not_found'}); return
                self.reply(200,result)
            except Conflict as exc: self.reply(409,{'error':str(exc)})
            except (OSError,ValueError,TypeError,KeyError): self.reply(400,{'error':'Agent 操作未完成，请核对参数和文件权限'})
            return
        if path == '/api/detection/scope':
            try:
                workspace=body['workspace']
                if body['confirm'] is not True or not isinstance(workspace,str) or not Path(workspace).is_absolute() or not Path(workspace).is_dir():
                    raise Conflict('请确认一个存在的绝对工作目录')
                with self.server.manager.lock:
                    scanner=self.server.scanner
                    if Path(workspace) not in scanner.workspaces:
                        if len(scanner.workspaces)>=16:raise Conflict('最多选择 16 个工作目录')
                        scanner.workspaces.append(Path(workspace))
                        additions=candidates(scanner.home,[Path(workspace)],environ={})
                        known={(s.client,str(s.path),s.scope,s.workspace,s.selector) for s in scanner.sources}
                        scanner.sources.extend(s for s in additions if s.scope!='user' and (s.client,str(s.path),s.scope,s.workspace,s.selector) not in known)
                    self.server.snapshot=self.server.manager.scan()
                self.reply(200,self.server.snapshot)
            except Conflict as exc:self.reply(409,{'error':str(exc)})
            return
        if path in {"/api/enroll", "/api/toggle", "/api/restore", "/api/stop"}:
            try:
                if path == "/api/enroll":
                    ids=body["ids"]
                    if body["confirm"] is not True or not isinstance(ids,list) or not 1 <= len(ids) <= 32 or any(not isinstance(x,str) for x in ids):
                        raise Conflict("请选择最多 32 项并确认接入")
                    result=self.server.manager.enroll(ids)
                else:
                    bid=body["bindingId"]
                    if not isinstance(bid,str) or len(bid)!=32: raise Conflict("接入标识无效")
                    if path == "/api/toggle":
                        if not isinstance(body["enabled"],bool): raise Conflict("启用状态无效")
                        result=self.server.manager.toggle(bid,body["enabled"])
                    elif path == "/api/restore":result=self.server.manager.restore(bid)
                    else:
                        self.server.runtime.stop(bid);result={"ok":True}
                self.server.snapshot=self.server.manager.scan()
                self.reply(200, {**result,"snapshot":self.server.snapshot})
            except Conflict as exc:
                self.reply(409,{"error":str(exc)})
            except (OSError,ValueError,KeyError,TypeError):
                self.reply(500,{"error":"操作未完成；请检查文件权限。备份保留在本机。"})
            return
        if path == "/api/session":
            self.reply(200, {"ok": True}, cookie=True)
        elif path == "/api/rescan":
            if not self.server.lock.acquire(blocking=False):
                self.reply(409, {"error": "scan_in_progress"})
                return
            try:
                if time.monotonic() - self.server.last_scan < 2:
                    self.reply(429, {"error": "rescan_cooldown"})
                else:
                    self.server.snapshot = self.server.manager.scan()
                    self.server.last_scan = time.monotonic()
                    self.server.snapshot["hub"] = self.server.agent_hub.detect()
                    self.reply(200, self.server.snapshot)
            finally:
                self.server.lock.release()
        else:
            self.reply(404, {"error": "not_found"})
