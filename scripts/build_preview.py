"""Build a self-contained synthetic-data UI preview; no network or real scan."""
import argparse
import json
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
from mcpcell.demo import demo_discovery


def build_preview(output: Path) -> Path:
    web = ROOT / "mcpcell/web"
    temp, scanner = demo_discovery()
    try:
        snapshot = scanner.scan()
    finally:
        temp.cleanup()
    html = (web / "index.html").read_text(encoding="utf-8")
    html = html.replace('<link rel="stylesheet" href="/style.css">',
        '<style>\n' + (web / "style.css").read_text(encoding="utf-8") + '\n</style>')
    html = html.replace('<script defer src="/app.js"></script>', '')
    data = json.dumps(snapshot, ensure_ascii=False).replace('<', '\\u003c')
    script = (web / "app.js").read_text(encoding="utf-8")
    html = html.replace('</body>', '<script>window.__MCPCELL_PREVIEW__=' + data +
        ';</script>\n<script>\n' + script + '\n</script>\n</body>')
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(html, encoding="utf-8")
    return output

if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument('--output', type=Path, default=ROOT / 'dist/liquid-preview.html')
    print(build_preview(parser.parse_args().output))
