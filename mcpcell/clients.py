"""Bounded, non-executing discovery of well-known MCP configuration files.

A found file proves only that a configuration exists, not that a client is
installed, running, approved, or using this configuration. See docs/clients.md.
"""
from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Mapping
import os
import sys

CLIENTS = {
    "codex": ("Codex", "CX", "TOML"),
    "claude-code": ("Claude Code", "CC", "JSON"),
    "claude-desktop": ("Claude Desktop", "CD", "JSON"),
    "cursor": ("Cursor", "CU", "JSON"),
    "vscode": ("VS Code / Copilot", "VS", "JSONC"),
    "gemini": ("Gemini CLI", "GE", "JSON"),
    "windsurf": ("Windsurf / Cascade", "WS", "JSON"),
    "kiro": ("Kiro", "KI", "JSON"),
    "cline": ("Cline", "CL", "JSON"),
    "roo": ("Roo Code", "RO", "JSON"),
    "opencode": ("OpenCode", "OC", "JSONC"),
}

@dataclass(frozen=True)
class Source:
    client: str
    path: Path
    scope: str = "user"
    workspace: str = ""
    selector: str = "mcpServers"
    format: str = "json"
    note: str = ""


def candidates(home: Path, workspaces: list[Path], *, platform: str | None = None,
               environ: Mapping[str, str] | None = None) -> list[Source]:
    platform = platform or sys.platform
    env = os.environ if environ is None else environ
    result: list[Source] = []
    seen: set[tuple] = set()

    def add(client: str, path: Path, scope: str = "user", workspace: str = "",
            selector: str = "mcpServers", fmt: str = "json", note: str = "") -> None:
        key = (client, str(path), scope, workspace, selector)
        if key not in seen:
            seen.add(key)
            result.append(Source(client, path, scope, workspace, selector, fmt, note))

    xdg = Path(env.get("XDG_CONFIG_HOME", str(home / ".config")))
    appdata = Path(env.get("APPDATA", str(home / "AppData/Roaming")))
    if platform == "win32":
        apps = appdata
    elif platform == "darwin":
        apps = home / "Library/Application Support"
    else:
        apps = xdg
    add("codex", Path(env.get("CODEX_HOME", str(home / ".codex"))) / "config.toml",
        selector="mcp_servers", fmt="toml")
    add("claude-code", home / ".claude.json")
    if env.get("CLAUDE_CONFIG_DIR"):
        add("claude-code", Path(env["CLAUDE_CONFIG_DIR"]) / ".claude.json", note="custom config directory; verify client version")
    if platform in {"win32", "darwin"}:
        add("claude-desktop", apps / "Claude/claude_desktop_config.json")
    add("cursor", home / ".cursor/mcp.json")
    add("vscode", apps / "Code/User/mcp.json", selector="servers", fmt="jsonc")
    add("vscode", apps / "Code - Insiders/User/mcp.json", selector="servers", fmt="jsonc", note="Insiders")
    add("vscode", Path(env.get("COPILOT_HOME", str(home / ".copilot"))) / "mcp-config.json")
    add("gemini", home / ".gemini/settings.json")
    add("windsurf", home / ".codeium/windsurf/mcp_config.json", note="legacy Windsurf location")
    add("windsurf", (appdata / "devin" if platform == "win32" else xdg / "devin") / "mcp_config.json", note="Cascade compatibility; not Devin Local agent")
    add("kiro", home / ".kiro/settings/mcp.json")
    add("cline", Path(env.get("CLINE_MCP_SETTINGS_PATH", str(home / ".cline/data/settings/cline_mcp_settings.json"))))
    for product in ["Code", "Code - Insiders", "Cursor"]:
        storage = apps / product / "User/globalStorage"
        add("cline", storage / "saoudrizwan.claude-dev/settings/cline_mcp_settings.json", note=f"{product} extension compatibility path")
        add("roo", storage / "rooveterinaryinc.roo-cline/settings/mcp_settings.json", note=f"{product} extension compatibility path")
    for suffix in ["json", "jsonc"]:
        add("opencode", xdg / "opencode" / f"opencode.{suffix}", selector="mcp", fmt="jsonc")
    if env.get("OPENCODE_CONFIG"):
        add("opencode", Path(env["OPENCODE_CONFIG"]), selector="mcp", fmt="jsonc", note="explicit environment override")
    # No recursive disk search or scanning arbitrary projects from history.
    for root in workspaces:
        w = str(root)
        for client, rel, selector, fmt in [
            ("codex", ".codex/config.toml", "mcp_servers", "toml"),
            ("claude-code", ".mcp.json", "mcpServers", "json"),
            ("cursor", ".cursor/mcp.json", "mcpServers", "json"),
            ("vscode", ".vscode/mcp.json", "servers", "jsonc"),
            ("vscode", ".mcp.json", "mcpServers", "json"),
            ("gemini", ".gemini/settings.json", "mcpServers", "json"),
            ("kiro", ".kiro/settings/mcp.json", "mcpServers", "json"),
            ("roo", ".roo/mcp.json", "mcpServers", "json"),
            ("opencode", "opencode.json", "mcp", "jsonc"),
            ("opencode", "opencode.jsonc", "mcp", "jsonc"),
        ]:
            add(client, root / rel, "workspace", w, selector, fmt)
        add("claude-code", home / ".claude.json", "local", w, "claude_project")
    return result
