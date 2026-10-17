"""Read-only parsers, conservative normalization, redaction, and review plans."""
from __future__ import annotations

import hashlib
import hmac
import json
import math
import os
from pathlib import Path
import re
import secrets
import stat
import time
import tomllib
from urllib.parse import urlsplit
from collections import defaultdict

from .clients import CLIENTS, Source, candidates

MAX_FILE = 1024 * 1024
MAX_SOURCES = 256
MAX_ENTRIES = 1024
SAFE_LAUNCHERS = {"npx", "node", "nodejs", "uv", "uvx", "python", "python3", "python3.11", "python3.12", "python3.13", "docker", "podman", "bun", "deno", "npm", "pnpm", "cmd", "powershell", "pwsh", "sh", "bash", "java", "dotnet"}
KNOWN = {"command", "args", "url", "httpUrl", "serverUrl", "type", "transport", "env", "environment", "headers", "http_headers", "env_http_headers", "env_vars", "cwd", "disabled", "enabled", "autoApprove", "alwaysAllow", "disabledTools", "enabled_tools", "disabled_tools", "timeout", "timeout_ms", "startup_timeout_sec", "tool_timeout_sec", "trust", "oauth", "bearer_token_env_var", "envFile", "env_file", "description"}

class ConfigError(ValueError):
    """An intentionally sanitized configuration error."""


def jsonc(text: str) -> str:
    """Remove comments and trailing commas without modifying string literals."""
    out = list(text)
    i, quoted, escaped = 0, False, False
    while i < len(text):
        c = text[i]
        if quoted:
            if escaped:
                escaped = False
            elif c == "\\":
                escaped = True
            elif c == '"':
                quoted = False
        elif c == '"':
            quoted = True
        elif text[i:i+2] == "//":
            end = text.find("\n", i + 2)
            end = len(text) if end < 0 else end
            out[i:end] = " " * (end - i)
            i = end - 1
        elif text[i:i+2] == "/*":
            end = text.find("*/", i + 2)
            if end < 0:
                raise ConfigError("JSONC 注释未闭合")
            for j in range(i, end + 2):
                if out[j] not in "\n\r":
                    out[j] = " "
            i = end + 1
        i += 1
    clean = "".join(out)
    out = list(clean)
    quoted = escaped = False
    for i, c in enumerate(clean):
        if quoted:
            if escaped:
                escaped = False
            elif c == "\\":
                escaped = True
            elif c == '"':
                quoted = False
        elif c == '"':
            quoted = True
        elif c == ",":
            j = i + 1
            while j < len(clean) and clean[j].isspace():
                j += 1
            if j < len(clean) and clean[j] in "]}":
                out[i] = " "
    return "".join(out)


def unique_object(pairs: list[tuple]) -> dict:
    result = {}
    for key, value in pairs:
        if key in result:
            raise ConfigError("配置包含重复键，拒绝猜测覆盖顺序")
        result[key] = value
    return result


def parse(data: bytes, fmt: str) -> dict:
    try:
        text = data.decode("utf-8-sig")
        obj = tomllib.loads(text) if fmt == "toml" else json.loads(
            jsonc(text) if fmt == "jsonc" else text,
            object_pairs_hook=unique_object,
            parse_constant=lambda _: (_ for _ in ()).throw(ConfigError("非有限数字不受支持")))
        if not isinstance(obj, dict):
            raise ConfigError("配置根节点必须为对象")
        stack = [(obj, 0)]
        visited = 0
        while stack:
            value, depth = stack.pop()
            visited += 1
            if depth > 64 or visited > 20000:
                raise ConfigError("配置嵌套或节点数量超过解析限额")
            if isinstance(value, float) and not math.isfinite(value):
                raise ConfigError("非有限数字不受支持")
            children = value.values() if isinstance(value, dict) else value if isinstance(value, list) else ()
            stack.extend((child, depth + 1) for child in children)
        return obj
    except ConfigError:
        raise
    except (ValueError, UnicodeError, RecursionError) as exc:
        # Parser exception messages may include credential material. Never echo.
        raise ConfigError("配置语法无效；原文件未修改") from None


def read_bounded(path: Path) -> bytes:
    # Reject links/reparse points, FIFOs and devices. Never wait on a named pipe.
    for p in [path, *path.parents]:
        s = p.lstat()
        if stat.S_ISLNK(s.st_mode) or getattr(s, "st_file_attributes", 0) & 0x400:
            raise ConfigError("跳过符号链接或重解析点")
    flags = os.O_RDONLY | getattr(os, "O_NONBLOCK", 0) | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_BINARY", 0)
    fd = os.open(path, flags)
    with os.fdopen(fd, "rb") as stream:
        st = os.fstat(stream.fileno())
        if not stat.S_ISREG(st.st_mode):
            raise ConfigError("只读取常规配置文件")
        if st.st_size > MAX_FILE:
            raise ConfigError("配置超过 1 MiB 限额，已跳过")
        data = stream.read(MAX_FILE + 1)
        if len(data) > MAX_FILE:
            raise ConfigError("配置超过 1 MiB 限额，已跳过")
        after = os.fstat(stream.fileno())
        if (st.st_size, st.st_mtime_ns) != (after.st_size, after.st_mtime_ns):
            raise ConfigError("读取期间配置变化，请重新扫描")
        return data


def label(value: object, limit: int = 160) -> str:
    return "".join(c for c in str(value) if c.isprintable())[:limit]


def endpoint_summary(value: str) -> str:
    # Hide userinfo, path, query, fragment (any can contain a bearer secret).
    try:
        u = urlsplit(value)
        if u.scheme not in {"http", "https", "ws", "wss"} or not u.hostname:
            return "远程地址已隐藏"
        return f"{u.scheme}://{u.hostname}" + (f":{u.port}" if u.port else "") + "/…"
    except ValueError:
        return "远程地址格式待核对"


class Discovery:
    def __init__(self, home: Path, workspaces: list[Path] | None = None, *,
                 platform: str | None = None, environ: dict | None = None,
                 extra: list[Source] | None = None, demo: bool = False):
        self.home = home
        self.workspaces = workspaces or []
        if len(self.workspaces) > 16:
            raise ValueError("最多选择 16 个工作区")
        self.sources = candidates(home, self.workspaces, platform=platform, environ=environ) + (extra or [])
        self.key = secrets.token_bytes(32)
        self.demo = demo
        self.refs = {}

    def digest(self, obj: object) -> str:
        raw = json.dumps(obj, sort_keys=True, ensure_ascii=False, default=str, separators=(",", ":")).encode()
        return hmac.new(self.key, raw, hashlib.sha256).hexdigest()[:24]

    def display_path(self, path: Path | str) -> str:
        path = Path(path)
        try:
            return "~/" + path.relative_to(self.home).as_posix()
        except ValueError:
            return label(path, 320)

    def normalize(self, name: str, cfg: dict, source: Source, source_id: str) -> dict:
        issues = []
        def issue(code: str, title: str, severity: str = "review"):
            issues.append({"code": code, "title": title, "severity": severity})
        command = cfg.get("command")
        args = cfg.get("args", [])
        if isinstance(command, list):
            args = command[1:]
            command = command[0] if command else None
        if command is not None and (not isinstance(command, str) or not command.strip()):
            raise ConfigError("启动命令格式无效")
        if not isinstance(args, list) or any(not isinstance(a, str) for a in args):
            raise ConfigError("args 必须为字符串数组")
        urls = [cfg.get(k) for k in ["url", "httpUrl", "serverUrl"] if cfg.get(k) is not None]
        if len(urls) > 1:
            issue("multiple-endpoints", "存在多个地址字段，需要核对客户端语义")
        url = urls[0] if urls else None
        if url is not None and not isinstance(url, str):
            raise ConfigError("地址字段必须为字符串")
        disabled = cfg.get("disabled") is True or cfg.get("enabled") is False
        if not command and not url and disabled:
            issue("disable-override", "只有禁用声明，需要保留客户端覆盖语义")
        elif bool(command) == bool(url):
            issue("ambiguous-launch", "无法唯一确定本地命令或远程地址", "high")
        transport = "stdio" if command and not url else "remote" if url and not command else "unknown"
        declared = cfg.get("type", cfg.get("transport", ""))
        if isinstance(declared, str) and declared not in {"", "stdio", "local", "remote", "http", "sse", "ws", "streamable-http", "streamableHttp"}:
            issue("unsupported-transport", "存在尚未适配的传输类型")
        if url and not declared:
            issue("transport-unresolved", "传输类型未显式声明；不按 URL 猜测协议")
        env = cfg.get("env", cfg.get("environment", {}))
        headers = cfg.get("headers", cfg.get("http_headers", {}))
        for value, field in [(env, "env"), (headers, "headers")]:
            if not isinstance(value, dict):
                raise ConfigError(f"{field} 必须为对象")
        credential_fields = any(k in cfg for k in ["oauth", "bearer_token_env_var", "env_vars", "env_http_headers", "envFile", "env_file"])
        if env or headers or credential_fields:
            issue("credential-boundary", "存在环境或凭据上下文，须单独核对授权")
        if headers or any(re.search(r"(?i)(token|secret|password|key|credential)", str(k)) for k in env):
            issue("secret-config", "含凭据相关配置；浏览器与导出不显示值")
        lower = str(command or "").replace("\\", "/").split("/")[-1].lower()
        base = re.sub(r"\.(exe|cmd|bat)$", "", lower)
        if base in {"sh", "bash", "cmd", "powershell", "pwsh"}:
            issue("shell-launch", "通过命令解释器启动，需要人工审查", "high")
        if base in {"npx", "uvx", "npm", "pnpm", "bun"}:
            issue("artifact-unverified", "包管理器启动；尚未验证制品锁定")
        auto = cfg.get("autoApprove", cfg.get("alwaysAllow", []))
        if auto or cfg.get("trust") is True:
            issue("auto-approval", "存在自动批准设置；迁移不能扩大权限", "high")
        if url:
            try:
                parsed = urlsplit(url)
                if parsed.scheme in {"http", "ws"} and parsed.hostname not in {"localhost", "127.0.0.1", "::1"}:
                    issue("unencrypted-remote", "远程地址未启用加密传输", "high")
                if parsed.username or parsed.password or parsed.query:
                    issue("url-secret", "地址含认证或查询信息，默认隐藏", "review")
            except ValueError:
                issue("invalid-url", "远程地址格式待核对", "high")
        unknown = len(set(cfg) - KNOWN)
        if unknown:
            issue("unknown-fields", f"存在 {unknown} 个扩展字段；不自动转换")
        raw_text = json.dumps(cfg, ensure_ascii=False)
        if "${" in raw_text or "{env:" in raw_text or "{file:" in raw_text:
            issue("unresolved-variables", "保留变量与输入引用，不读取引用文件或展开")
        # The full original entry participates in a process-local keyed hash.
        # Equal hashes mean configuration candidates only, NOT sharing approval.
        disabled = cfg.get("disabled") is True or cfg.get("enabled") is False
        return {
            "id": self.digest([source.client, str(source.path), source.scope, source.workspace, name]),
            "name": label(name), "client": source.client, "sourceId": source_id,
            "source": self.display_path(source.path), "scope": source.scope,
            "workspace": self.display_path(source.workspace) if source.workspace else "未绑定；运行时需明确",
            "transport": transport, "declaredTransport": label(declared),
            "configuredState": "disabled" if disabled else "declared",
            "runtimeState": "not_observed", "managed": False,
            "endpoint": endpoint_summary(url) if url else (base if base in SAFE_LAUNCHERS else "自定义可执行文件（路径隐藏）"),
            "argumentCount": len(args), "environmentCount": len(env), "headerCount": len(headers),
            "extraFieldCount": unknown, "issues": issues,
            "severity": "high" if any(i["severity"] == "high" for i in issues) else "review" if issues else "unverified",
            "candidateKey": self.digest(cfg), "candidateCount": 1,
            "originKey": self.digest([str(source.path), source.selector, source.workspace, name]),
            "sharing": "exclusive", "sharingEvidence": "none",
        }

    def scan(self) -> dict:
        started = time.monotonic()
        entries, sources, errors = [], [], []
        self.refs = {}
        for s in self.sources[:MAX_SOURCES]:
            sid = self.digest([s.client, str(s.path), s.scope, s.workspace])
            record = {"id": sid, "client": s.client, "path": self.display_path(s.path), "scope": s.scope,
                      "format": s.format, "status": "missing", "entries": 0, "note": s.note}
            try:
                data = read_bounded(s.path)
                obj = parse(data, s.format)
                if s.selector == "claude_project":
                    selected = obj.get("projects", {}).get(s.workspace, {})
                    mapping = selected.get("mcpServers", {}) if isinstance(selected, dict) else {}
                else:
                    mapping = obj.get(s.selector, {})
                if not isinstance(mapping, dict):
                    raise ConfigError("MCP 服务集合必须为对象")
                record.update(status="found", version=self.digest(data.hex()))
                for name, cfg in mapping.items():
                    if len(entries) >= MAX_ENTRIES:
                        raise ConfigError("已达到 1024 条配置限额，扫描不完整")
                    if not isinstance(cfg, dict):
                        errors.append({"source": record["path"], "message": "跳过非对象服务配置"})
                        continue
                    try:
                        entry = self.normalize(name, cfg, s, sid)
                        keys = ["projects", s.workspace, "mcpServers", name] if s.selector == "claude_project" else [s.selector, name]
                        self.refs[entry["id"]] = {"source": s, "keys": keys, "raw": data, "cfg": cfg, "entry": entry}
                        entries.append(entry)
                        record["entries"] += 1
                    except ConfigError as exc:
                        errors.append({"source": record["path"], "message": str(exc)})
            except FileNotFoundError:
                pass
            except ConfigError as exc:
                record["status"] = "blocked"
                errors.append({"source": record["path"], "message": str(exc)})
            except (OSError, ValueError, TypeError, AttributeError, RecursionError):
                record["status"] = "error"
                errors.append({"source": record["path"], "message": "无法安全读取此配置；原文件未修改"})
            sources.append(record)
        if len(self.sources) > MAX_SOURCES:
            errors.append({"source": "scanner", "message": "配置位置超过 256 个，扫描不完整"})
        origins = defaultdict(set)
        for e in entries:
            origins[e["candidateKey"]].add(e["originKey"])
        counts = {key: len(value) for key, value in origins.items()}
        for e in entries:
            e["candidateCount"] = counts[e["candidateKey"]]
        clients = []
        for key, (name, monogram, fmt) in CLIENTS.items():
            src = [s for s in sources if s["client"] == key]
            found = sum(s["status"] == "found" for s in src)
            clients.append({"id": key, "name": name, "monogram": monogram, "format": fmt,
                "found": found, "entryCount": sum(e["client"] == key for e in entries),
                "status": "config_found" if found else "not_found", "sources": src,
                "capability": "discovery_only"})
        return {"schemaVersion": "mcpcell.inventory/v0alpha1", "version": "0.1.0a1",
                "mode": "demo" if self.demo else "local", "scannedAt": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
                "durationMs": round((time.monotonic() - started) * 1000),
                "clients": clients, "entries": entries, "errors": errors,
                "stats": {"clientsFound": sum(c["found"] > 0 for c in clients), "definitions": len(entries),
                    "candidateGroups": sum(n > 1 for n in counts.values()),
                    "reviewCount": sum(bool(e["issues"]) for e in entries), "managedInstances": 0,
                    "memorySavedBytes": None, "processCount": None},
                "capabilities": {"discover": True, "reviewPlan": True, "configWrite": False, "executeMcp": False, "executeCli": False, "resourceEnforcement": False},
                "limitations": ["只扫描已知位置及显式选择的工作区，不等于安装或运行状态检测。",
                    "不执行 MCP/CLI、不连接远程地址、不改写客户端配置。",
                    "未解析插件注入、企业策略、运行时配置或所有配置覆盖优先级。",
                    "配置相同只是候选，不构成进程安全复用的证据。"]}


def plan(snapshot: dict) -> dict:
    return {"schemaVersion": "mcpcell.review-plan/v0alpha1", "mode": "proposal_only", "applicable": False,
        "reason": "此导出只是审查提案，接入必须通过管理 API 确认。不能用于替换客户端配置。",
        "createdAt": snapshot["scannedAt"], "demo": snapshot["mode"] == "demo",
        "items": [{"definitionId": e["id"], "client": e["client"], "name": e["name"],
            "source": e["source"], "scope": e["scope"], "operation": "review-before-enrollment",
            "proposedSharing": "exclusive", "credentialAction": "keep-in-source",
            "sourceVersion": next((s.get("version") for c in snapshot["clients"] for s in c["sources"] if s["id"] == e["sourceId"]), None),
            "blockers": ["client-semantics-unverified", "operator-approval-required"],
            "checks": [i["code"] for i in e["issues"]]} for e in snapshot["entries"]]}
