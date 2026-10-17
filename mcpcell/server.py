"""Loopback-only authenticated management API and bounded static workbench."""
from __future__ import annotations

from concurrent.futures import ThreadPoolExecutor
from http.cookies import SimpleCookie, CookieError
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
from urllib.parse import urlsplit
import json
import secrets
import threading
import time

from .discovery import Discovery, plan
from .management import Manager
from .runtime import Runtime
from .storage import InstanceLock, Conflict

WEB = Path(__file__).with_name("web")
ASSETS = {"/": ("index.html", "text/html; charset=utf-8"),
          "/app.js": ("app.js", "text/javascript; charset=utf-8"),
          "/style.css": ("style.css", "text/css; charset=utf-8")}
CSP = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; font-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"


class Workbench(HTTPServer):
    """Fixed worker pool with no unbounded request/thread queue."""
    allow_reuse_address = True
    request_queue_size = 8

    def __init__(self, scanner: Discovery, port: int = 0, state_dir: Path | None = None):
        self.scanner = scanner
        self.token = secrets.token_urlsafe(32)
        self.session = secrets.token_urlsafe(32)
        self.lock = threading.Lock()
        self.state_dir = state_dir or (scanner.home / ".mcpcell")
        self.instance_lock = InstanceLock(self.state_dir)
        self.manager = Manager(scanner, self.state_dir)
        self.runtime = Runtime(self.manager)
        self.manager.runtime = self.runtime
        self.snapshot = self.manager.scan()
        self.last_scan = time.monotonic()
        self.pool = ThreadPoolExecutor(max_workers=8, thread_name_prefix="mcpcell-http")
        self.slots = threading.BoundedSemaphore(8)
        try:
            super().__init__(("127.0.0.1", port), Handler)
        except Exception:
            self.runtime.close(); self.instance_lock.close(); self.pool.shutdown(wait=False)
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
            self.reply(200, snapshot)
        elif path == "/api/runtime":
            self.reply(200, self.server.runtime.status())
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
        if size < 0 or size > max_body:
            self.reply(413, {"error": "body_limit"})
            return
        try:
            body = json.loads(self.rfile.read(size))
            if not isinstance(body, dict): raise ValueError()
            schemas = {"/api/enroll": {"ids", "confirm"}, "/api/toggle": {"bindingId", "enabled"},
                       "/api/restore": {"bindingId"}, "/api/stop": {"bindingId"}}
            if set(body) != schemas.get(path, set()): raise ValueError()
        except (ValueError, UnicodeError):
            self.reply(400, {"error": "请求内容无效"})
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
                    self.reply(200, self.server.snapshot)
            finally:
                self.server.lock.release()
        else:
            self.reply(404, {"error": "not_found"})
