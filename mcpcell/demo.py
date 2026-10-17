"""Synthetic demo fixtures. Never scans the real home directory."""
import json
from pathlib import Path
from tempfile import TemporaryDirectory
from .discovery import Discovery


def demo_discovery():
    temp = TemporaryDirectory(prefix="mcpcell-demo-")
    home = Path(temp.name).resolve()
    root = home / "Projects/atlas"
    root.mkdir(parents=True)
    docs = {"command": "npx", "args": ["-y", "@upstash/context7-mcp@2.0.0"]}
    browser = {"command": "npx", "args": ["-y", "@playwright/mcp@0.0.1"]}
    examples = {
        ".cursor/mcp.json": {"mcpServers": {"context7": docs, "playwright": browser}},
        ".claude.json": {"mcpServers": {"context7": docs, "github": {"type": "http", "url": "https://mcp.example.com/github", "headers": {"Authorization": "Bearer DEMO_NOT_A_CREDENTIAL"}}},
            "projects": {str(root): {"mcpServers": {"filesystem": {"command": "node", "args": ["filesystem.js", "./src"], "cwd": str(root)}}}}},
        ".gemini/settings.json": {"mcpServers": {"context7": docs, "research": {"httpUrl": "https://research.example.com/mcp"}}},
        ".kiro/settings/mcp.json": {"mcpServers": {"aws-docs": {"command": "uvx", "args": ["documentation-server"]}}},
        ".cline/data/settings/cline_mcp_settings.json": {"mcpServers": {"local-shell": {"command": "bash", "args": ["-lc", "echo DEMO_ONLY"], "autoApprove": ["*"]}}},
        ".config/opencode/opencode.json": {"mcp": {"project-index": {"type": "local", "command": ["node", "index.js"], "environment": {"PROJECT_ROOT": "${workspaceFolder}"}}, "archive": {"type": "remote", "url": "http://archive.example.com/mcp", "enabled": False}}},
    }
    for rel, body in examples.items():
        file = home / rel
        file.parent.mkdir(parents=True, exist_ok=True)
        file.write_text(json.dumps(body), encoding="utf-8")
    codex = home / ".codex/config.toml"
    codex.parent.mkdir(parents=True)
    codex.write_text('[mcp_servers.context7]\ncommand = "npx"\nargs = ["-y", "@upstash/context7-mcp@2.0.0"]\n', encoding="utf-8")
    return temp, Discovery(home, [root], platform="linux", environ={}, demo=True)
