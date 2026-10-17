"""Self-contained synthetic UI preview. No real home scan or agent execution."""
import argparse
import json
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
from mcpcell.demo import demo_discovery
from mcpcell.management import Manager
from mcpcell.agents import AgentHub


def build_preview(output: Path) -> Path:
    web = ROOT / 'mcpcell/web'
    temp, scanner = demo_discovery()
    try:
        manager = Manager(scanner, scanner.home / '.mcpcell')
        snapshot = manager.scan()
        hub = AgentHub(scanner.home, manager.vault, demo=True, environ={})
        snapshot['hub'] = hub.snapshot()
        snapshot['hub']['defaultCwd'] = 'D:\\work\\MCP-cell'
        for a in snapshot['hub']['agents']:
            if a['found']:
                a['executable'] = 'C:\\Users\\demo\\.local\\bin\\' + a['command']
            a['lastCwd'] = 'D:\\work\\CableSimPro' if a['id'] == 'claude-code' else 'D:\\work\\MCP-cell'
        hub.close()
    finally:
        temp.cleanup()
    html = (web / 'index.html').read_text(encoding='utf-8')
    for name in ['style.css', 'hub.css', 'workspace.css', 'vendor/terminal.css']:
        path = web / name
        css = path.read_text(encoding='utf-8') if path.exists() else ''
        html = html.replace(f'<link rel="stylesheet" href="/{name}">', '<style>\n' + css + '\n</style>')
    for name in ['icons.js', 'app.js', 'hub.js', 'vendor/terminal.js']:
        html = html.replace(f'<script defer src="/{name}"></script>', '')
    data = json.dumps(snapshot, ensure_ascii=False).replace('<', '\\u003c')
    script = '<script>window.__MCPCELL_PREVIEW__=' + data + ';</script>\n'
    for name in ['icons.js', 'vendor/terminal.js', 'app.js', 'hub.js']:
        path = web / name
        if path.exists():
            text = path.read_text(encoding='utf-8').replace('</script', '<\\/script')
            script += '<script>\n' + text + '\n</script>\n'
    html = html.replace('</body>', script + '</body>')
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(html, encoding='utf-8')
    return output

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--output', type=Path, default=ROOT / 'dist/agent-workspace-preview.html')
    print(build_preview(parser.parse_args().output))
