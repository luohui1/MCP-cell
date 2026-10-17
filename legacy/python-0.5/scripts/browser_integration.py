"""Normal browser-to-loopback smoke test, isolated demo only. No policy bypass."""
import json, os, sys, threading, time
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1];sys.path.insert(0,str(ROOT))
from mcpcell.demo import demo_discovery
from mcpcell.server import Workbench
from playwright.sync_api import sync_playwright

def main():
    temp,scanner=demo_discovery()
    server=Workbench(scanner,0,state_dir=scanner.home/'.mcpcell',agent_environ={})
    worker=threading.Thread(target=server.serve_forever,daemon=True);worker.start()
    result={'status':'not_run','checks':[]}
    try:
        with sync_playwright() as p:
            b=p.chromium.launch(executable_path=os.environ.get('CHROMIUM_PATH','/usr/bin/chromium'),headless=True,args=['--no-sandbox'])
            pg=b.new_page(viewport={'width':1366,'height':900})
            errors=[];pg.on('pageerror',lambda e:errors.append(str(e)))
            try:
                pg.goto(server.origin+'/#token='+server.token,wait_until='networkidle',timeout=15000)
                pg.wait_for_selector('body[data-ready="true"]',timeout=8000)
                assert pg.locator('#page-mcp').is_visible()
                assert pg.locator('#list .row').count()==9
                assert '#' not in pg.url
                result['checks']+=['authenticated launch to normal loopback URL','token removed from URL','real snapshot renders MCP workspace']
                pg.locator('[data-source-client="cursor"]').click()
                assert pg.locator('#list .row').count()==2
                result['checks'].append('source filter on live HTTP-delivered UI')
                pg.locator('#nav-agents').click()
                assert pg.locator('.agent-card').count()==10
                result['checks'].append('live Agent view and local assets')
                assert not errors,errors
                result.update(status='passed',pageErrors=errors)
            except Exception as e:
                msg=str(e).replace(server.token,'[REDACTED]')
                result.update(status='blocked' if 'ERR_BLOCKED_BY_ADMINISTRATOR' in msg else 'failed',error=msg[:1000])
            finally:b.close()
    finally:
        server.shutdown();worker.join(timeout=5);server.server_close();temp.cleanup()
    (ROOT/'dist').mkdir(exist_ok=True)
    (ROOT/'dist/browser-integration.json').write_text(json.dumps(result,ensure_ascii=False,indent=2))
    print(json.dumps(result,ensure_ascii=False))
    return 0 if result['status'] in {'passed','blocked'} else 1
if __name__=='__main__':raise SystemExit(main())
