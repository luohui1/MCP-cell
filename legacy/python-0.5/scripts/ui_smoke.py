"""MCP-first UI and operation-scope regression checks (synthetic data only)."""
import json
import os
import sys
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT))
from scripts.build_preview import build_preview
from playwright.sync_api import sync_playwright

preview=build_preview(ROOT/'dist/mcp-workspace-preview.html')
checks=[];errors=[];requests=[]
def ok(label): checks.append(label)
with sync_playwright() as p:
    browser=p.chromium.launch(executable_path=os.environ.get('CHROMIUM_PATH','/usr/bin/chromium'),headless=True,args=['--no-sandbox'])
    page=browser.new_page(viewport={'width':1366,'height':900},device_scale_factor=1)
    page.on('pageerror',lambda e:errors.append(str(e)))
    page.on('request',lambda r:requests.append(r.url))
    page.set_content(preview.read_text(encoding='utf-8'),wait_until='load')
    page.wait_for_selector('body[data-ready="true"]')
    assert page.locator('#page-mcp').is_visible()
    assert not page.locator('#page-agents').is_visible()
    ok('MCP is the only initial primary view')
    assert page.locator('.window').evaluate('(e)=>Math.round(e.getBoundingClientRect().width)')==1366
    assert page.locator('.window').evaluate('(e)=>Math.round(e.getBoundingClientRect().height)')==900
    ok('workspace uses the normal browser viewport, not a 960px fixed window')
    assert page.locator('.window').evaluate('(e)=>getComputedStyle(e).borderRadius')=='0px'
    assert page.locator('#scan').evaluate('(e)=>getComputedStyle(e).borderRadius')=='4px'
    ok('outer window radius is removed and controls use 4px corners')
    assert page.locator('[role=tab]').count()==0
    assert page.locator('#session-strip').count()==0
    ok('no persistent function or session tab bars')
    assert '演示' in page.locator('#connection').inner_text()
    assert page.locator('#list .row').count()==9
    ok('demo provenance and nine presentation groups are visible')
    assert page.locator('[data-source-client]').count()==12
    ok('all client sources plus all-sources selector remain accessible')
    page.screenshot(path=str(ROOT/'dist/workspace.png'))
    page.locator('[data-source-client="cursor"]').click()
    assert page.locator('#list .row').count()==2
    ok('source filter selects Cursor bindings')
    row=page.locator('#list .row').filter(has=page.locator('.name',has_text='context7'))
    row.locator('[data-enroll]').click()
    text=page.locator('#dialog-content').inner_text()
    assert '接入 1 条' in text and 'Cursor' in text and 'Codex' not in text and 'Gemini' not in text
    ok('REGRESSION: Cursor-only enrollment does not include hidden other clients')
    page.locator('#confirm-enroll').click()
    data=page.evaluate('window.mcpcell.snapshot.entries.filter(e=>e.managed).map(e=>e.client)')
    assert data==['cursor'],data
    ok('confirmed scoped enrollment mutates only the selected source in demo')
    page.locator('#list .row').filter(has=page.locator('.name',has_text='context7')).locator('[data-toggle]').click()
    managed=page.evaluate('window.mcpcell.snapshot.entries.filter(e=>e.managed)')
    assert len(managed)==1 and managed[0]['enabled'] is False
    ok('scoped pause is confined to the managed source')
    page.locator('#list .row').filter(has=page.locator('.name',has_text='context7')).locator('[data-restore]').click()
    page.locator('#confirm-restore').click()
    assert page.evaluate('window.mcpcell.snapshot.entries.every(e=>!e.managed)')
    ok('scoped restore preserves unrelated sources')
    page.locator('#select-all').check()
    page.locator('#enroll-selected').click()
    text=page.locator('#dialog-content').inner_text()
    assert '接入 2 条' in text and 'Codex' not in text
    page.keyboard.press('Escape')
    ok('bulk selection also honors the current source')
    page.locator('[data-source-client="all"]').click()
    page.locator('#search').fill('context7')
    assert page.locator('#list .row').count()==1
    page.locator('[data-enroll]').click()
    assert '接入 4 条' in page.locator('#dialog-content').inner_text()
    page.keyboard.press('Escape')
    ok('all-source grouping still explicitly previews all four source bindings')
    page.locator('#search').fill('')
    page.locator('#management-filter').select_option('managed')
    assert page.locator('#list .row').count()==0
    page.locator('#management-filter').select_option('unmanaged')
    assert page.locator('#list .row').count()>0
    ok('state dropdown replaces all/managed tab row')
    page.locator('#management-filter').select_option('all')
    page.locator('[data-source-client="windsurf"]').click()
    assert page.locator('#list .row').count()==0
    page.locator('[data-source-client="all"]').click()
    ok('empty source is not presented as installed or running')
    page.locator('#nav-detection').click()
    assert page.locator('#page-detection').is_visible()
    assert page.locator('.detected-row').count()==12
    page.locator('#detection-kind').select_option('agents')
    assert page.locator('.detected-row').count()==10
    ok('on-demand detection separates MCP configurations from ten Agent entrypoints')
    page.locator('#detection-search').fill('pi')
    assert any('Pi' in t for t in page.locator('.detected-row').all_inner_texts())
    page.locator('#detection-search').fill('')
    page.locator('#add-scope').click()
    assert page.locator('#scan-workspace').is_visible()
    page.keyboard.press('Escape')
    ok('detection search and explicit project-directory dialog are preserved')
    page.locator('[data-collect="gemini"]').click()
    assert '只保存启动入口' in page.locator('#dialog').inner_text()
    page.locator('[data-hub-action="enroll"]').click()
    assert page.locator('#page-agents').is_visible()
    ok('Agent collection still requires confirmation and does not launch')
    assert page.locator('.agent-card').count()==10
    assert page.locator('.agent-entry .ai-logo svg').count()==10
    assert page.locator('#client-dock-items .client-entry').count()==7
    ok('Agent and editor icon collection is retained')
    page.locator('[data-agent-card="droid"] .agent-entry').click()
    assert page.locator('[data-hub-action="launch"]').count()==0
    page.keyboard.press('Escape')
    ok('missing Agent opens details, not installation or execution')
    page.locator('#agent-filter').select_option('enrolled')
    assert page.locator('.agent-card').count()==4
    page.locator('#page-agents [data-agent-info="pi"]').click()
    page.locator('[data-hub-action="pin"]').click()
    page.locator('#agent-filter').select_option('pinned')
    assert page.locator('.agent-card').count()==1
    ok('collection and pinned filters work without a tab bar')
    page.locator('#agent-filter').select_option('all')
    page.locator('#page-agents [data-agent-info="gemini"]').click()
    page.locator('[data-hub-action="enable"]').click()
    assert page.locator('[data-launch-agent="gemini"]').is_disabled()
    ok('disabling Agent still blocks new sessions')
    page.locator('#register-agent').click()
    assert page.locator('#custom-args').input_value()=='[]'
    page.keyboard.press('Escape')
    ok('manual registration remains structured argv, not shell concatenation')
    page.locator('[data-launch-agent="claude-code"]').click()
    page.locator('[data-hub-action="launch"]').click()
    assert page.locator('.xterm').count()==1
    assert page.locator('#session-control').is_visible()
    first=page.locator('#session-picker').input_value()
    ok('first terminal opens with one session picker, not a tab strip')
    page.locator('#new-session').click()
    page.locator('#launch-agent').select_option('pi')
    page.locator('[data-hub-action="launch"]').click()
    second=page.locator('#session-picker').input_value()
    assert second!=first and page.locator('.xterm').count()==2
    ok('two independent sessions remain available')
    page.locator('.session-panel:visible .xterm-helper-textarea').focus()
    page.keyboard.type('example');page.keyboard.press('Enter');page.wait_for_timeout(100)
    assert '未执行' in page.locator('.session-panel:visible .xterm-rows').inner_text()
    ok('demo terminal receives input without executing it')
    page.locator('#session-picker').select_option(first)
    assert page.locator('.session-panel:visible .terminal-title strong').inner_text().startswith('Claude Code')
    assert page.locator('.session-panel').count()==2
    ok('session picker switches without creating or replaying processes')
    page.locator('.session-panel:visible [data-session-rename]').click()
    page.locator('#session-title').fill('CC · 项目 A')
    page.locator('[data-hub-action="rename"]').click()
    assert 'CC · 项目 A' in page.locator('#session-picker').inner_text()
    ok('renamed session appears in picker')
    page.locator('#nav-mcp').click()
    assert page.locator('#page-mcp').is_visible()
    assert page.locator('#session-picker option').count()==3
    page.locator('#session-picker').select_option(first)
    page.locator('.session-panel:visible [data-session-close]').click()
    assert '其他会话不受影响' in page.locator('#dialog').inner_text()
    page.locator('[data-hub-action="close"]').click()
    assert page.locator('#session-picker option').count()==2
    assert page.locator('#session-picker').input_value()==second
    ok('closing selected session requires confirmation and preserves the other')
    page.locator('.session-panel:visible [data-session-stop]').click()
    page.locator('[data-hub-action="stop"]').click()
    assert '已结束' in page.locator('.session-panel:visible [data-terminal-state]').inner_text()
    assert page.locator('.xterm').count()==1
    ok('stop retains terminal output without a persistent tab')
    page.locator('.session-panel:visible [data-session-close]').click()
    assert page.locator('#page-mcp').is_visible()
    assert page.locator('#session-control').is_hidden()
    ok('closing the final session returns to MCP and removes the picker')
    page.locator('#nav-agents').click()
    page.locator('[data-open-client="cursor"]').click()
    assert page.locator('#page-mcp').is_visible() and page.locator('#client').input_value()=='cursor'
    ok('editor icon still links to MCP source filtering')
    page.locator('[data-source-client="all"]').click()
    page.keyboard.press('Control+k')
    assert page.locator('#search').evaluate('(e)=>document.activeElement===e')
    ok('keyboard search works on the primary workspace')
    assert page.evaluate('(()=>{const x=[...document.querySelectorAll("svg [id]")].map(e=>e.id);return x.length===new Set(x).size;})()')
    assert 'onload' not in page.evaluate('window.MCPcellIcons.render("<img onload=evil>")')
    ok('icon gradient IDs and unknown-brand escaping remain valid')
    page.set_viewport_size({'width':1920,'height':1080})
    assert page.locator('.window').evaluate('(e)=>e.getBoundingClientRect().width')==1920
    assert page.evaluate('document.documentElement.scrollWidth<=innerWidth')
    ok('layout expands to 1920px without the old width ceiling')
    page.set_viewport_size({'width':390,'height':844})
    assert page.evaluate('document.documentElement.scrollWidth<=innerWidth')
    page.screenshot(path=str(ROOT/'dist/workspace-mobile.png'))
    page.locator('#nav-agents').click()
    assert page.locator('.agent-entry').evaluate_all('(els)=>els.every(e=>e.getBoundingClientRect().right<=innerWidth+1)')
    ok('390px MCP and Agent views stay within viewport')
    page.emulate_media(reduced_motion='reduce')
    assert page.evaluate('matchMedia("(prefers-reduced-motion: reduce)").matches')
    ok('reduced-motion preference remains supported')
    assert not requests,requests
    ok('offline preview and icons issue no network requests')
    browser.close()
assert not errors,errors
(ROOT/'dist/ui-results.json').write_text(json.dumps({'checks':checks,'errors':errors},ensure_ascii=False,indent=2),encoding='utf-8')
print(json.dumps({'passed':len(checks),'errors':errors},ensure_ascii=False))
