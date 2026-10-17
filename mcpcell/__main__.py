"""Run with Python 3.11+: python -m mcpcell [serve|scan|plan]."""
import argparse
import json
import os
from pathlib import Path
import sys
import webbrowser

from .clients import CLIENTS, Source
from .discovery import Discovery, plan
from .demo import demo_discovery
from .server import Workbench
from .storage import Vault, Conflict
from .window import open_window


def main():
    parser = argparse.ArgumentParser(description="MCPcell：检测、接入、暂停和还原本机 MCP。接入前会请求确认。")
    parser.add_argument("action", nargs="?", choices=["serve", "scan", "plan"], default="serve")
    parser.add_argument("--demo", action="store_true", help="使用隔离的演示配置，不读取本机配置")
    parser.add_argument("--home", type=Path, help="显式指定配置根目录；不会读取真实环境中的路径覆盖")
    parser.add_argument("--workspace", type=Path, action="append", default=[], help="显式授权扫描的工作区，可重复，最多 16 个")
    parser.add_argument("--config", action="append", default=[], metavar="CLIENT=PATH", help="显式选择自定义配置文件；不执行")
    parser.add_argument("--port", type=int, default=43123)
    parser.add_argument("--state-dir", type=Path, help="本机管理状态目录；不可删除仍在使用的接管记录")
    parser.add_argument("--open", action="store_true", help="在系统浏览器打开本地控制台")
    args = parser.parse_args()
    if not 0 <= args.port <= 65535:
        parser.error("端口必须在 0–65535 范围内")
    if args.demo and (args.home or args.workspace or args.config):
        parser.error("演示模式不能与本机扫描路径混用")
    if len(args.config) > 32:
        parser.error("最多 32 个自定义配置文件")
    temp = None
    try:
        if args.demo:
            temp, scanner = demo_discovery()
        else:
            home = (args.home or Path.home()).expanduser().absolute()
            roots = list(dict.fromkeys(p.expanduser().absolute() for p in args.workspace))
            extra = []
            for spec in args.config:
                client, sep, path = spec.partition("=")
                if not sep or client not in CLIENTS or not path:
                    parser.error("自定义配置需使用 CLIENT=PATH；CLIENT: " + ", ".join(CLIENTS))
                selector = "mcp_servers" if client == "codex" else "servers" if client == "vscode" else "mcp" if client == "opencode" else "mcpServers"
                fmt = "toml" if client == "codex" else "jsonc" if client in {"vscode", "opencode"} else "json"
                extra.append(Source(client, Path(path).expanduser().absolute(), "custom", selector=selector, format=fmt))
            scanner = Discovery(home, roots, environ={} if args.home else None, extra=extra)
        if args.action in {"scan", "plan"}:
            snapshot = scanner.scan()
            print(json.dumps(plan(snapshot) if args.action == "plan" else snapshot, ensure_ascii=False, indent=2))
            return
        state_dir=(args.state_dir or scanner.home / ".mcpcell").expanduser().absolute()
        try:
            server=Workbench(scanner,args.port,state_dir)
        except Conflict:
            endpoint=Vault(state_dir).get("endpoint.bin",{})
            if not endpoint.get("url"): raise
            print("MCPcell 已在运行。请勿分享此临时访问链接：\n"+endpoint["url"])
            if args.open:open_window(endpoint["url"])
            return
        with server:
            url = f"{server.origin}/#token={server.token}"
            print("MCPcell 0.2 · 本机 MCP 管理器", flush=True)
            print("此链接含临时访问凭据，请勿分享：\n" + url, flush=True)
            print("接入前不会修改配置。按 Ctrl+C 停止管理器及其托管进程。", flush=True)
            if args.open:
                open_window(url)
            try:
                server.serve_forever(poll_interval=0.25)
            except KeyboardInterrupt:
                pass
    except (OSError, ValueError, Conflict) as exc:
        print(f"无法启动：{type(exc).__name__}。请检查端口、目录与参数。", file=sys.stderr)
        raise SystemExit(1) from None
    finally:
        if temp:
            temp.cleanup()

if __name__ == "__main__":
    main()
