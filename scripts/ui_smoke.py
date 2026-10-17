"""Multi-page/agent/session UI smoke checks using explicitly synthetic data."""
import json
import os
import sys
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT))
from scripts.build_preview import build_preview
from playwright.sync_api import sync_playwright

preview=build_preview(ROOT/'dist/agent-workspace-preview.html')
checks=[];errors=[]
with sync_playwright() as p:
    browser=p.chromium.launch(executable_path=os.environ.get('CHROMIUM_PATH','/usr/bin/chromium'),headless=True,args=['--no-sandbox'])
    page=browser.new_page(viewport={'width':1100,'height':790},device_scale_factor=1)
    page.on('pageerror',lambda e:errors.append(str(e)))
    page.set_content(preview.read_text(encoding='utf-8'),wait_until='load')
    page.wait_for_selector('body[data-ready="true"]')
    assert page.locator('.window').evaluate('(e)=>Math.round(e.getBoundingClientRect().width)')==1000
    assert page.locator('.window').evaluate('(e)=>Math.round(e.getBoundingClientRect().height)')==710
    checks.append('1000x710 compact multi-page workspace')
    assert page.locator('.primary-tabs [role=tab]').count()==3
    assert page.locator('#page-detection').is_visible()
    checks.append('dedicated detection page is the initial window')
    assert page.locator('.detected-row').count()==10
    assert '演示' in page.locator('#connection').inner_text()
    checks.append('10 agent-family discovery rows and visible demo provenance')
    page.screenshot(path=str(ROOT/'dist/detection-window.png'))
    page.locator('#detection-search').fill('pi')
    assert any('Pi' in t for t in page.locator('.detected-row').all_inner_texts())
    checks.append('agent discovery search works')
    page.locator('#detection-search').fill('')
    page.locator('[data-detect-kind="mcp"]').click()
    assert page.locator('.detected-row').count()==12
    checks.append('detection separates 12 MCP source bindings from agent executables')
    page.locator('[data-detect-kind="agents"]').click()
    page.locator('[data-collect="gemini"]').click()
    assert '只保存启动入口' in page.locator('#dialog').inner_text()
    page.locator('[data-hub-action="enroll"]').click()
    assert page.locator('#page-agents').is_visible()
    assert page.locator('.agent-card').count()==4
    checks.append('collecting an Agent needs confirmation and opens its library')
    page.locator('#page-agents [data-agent-info="pi"]').click()
    assert '没有内置权限审批' in page.locator('#dialog').inner_text()
    page.locator('[data-hub-action="pin"]').click()
    page.locator('[data-agent-filter="pinned"]').click()
    assert page.locator('.agent-card').count()==1
    checks.append('per-agent details and pin filtering')
    page.locator('[data-agent-filter="all"]').click()
    page.locator('#page-agents [data-agent-info="gemini"]').click()
    page.locator('[data-hub-action="enable"]').click()
    assert page.locator('[data-launch-agent="gemini"]').is_disabled()
    checks.append('disable prevents new sessions without deleting configuration')
    page.locator('#register-agent').click()
    assert page.locator('#custom-exe').is_visible()
    assert page.locator('#custom-args').input_value()=='[]'
    page.keyboard.press('Escape')
    checks.append('manual registration form uses executable plus structured argv')
    page.locator('[data-launch-agent="claude-code"]').click()
    assert page.locator('#launch-cwd').is_visible()
    page.locator('#launch-cwd').fill('D:\\work\\CableSimPro')
    page.locator('[data-hub-action="launch"]').click()
    assert page.locator('.session-chip').count()==1
    assert page.locator('.xterm').count()==1
    checks.append('first Agent session opens a genuine terminal emulator in its own tab')
    page.locator('#new-session').click()
    page.locator('#launch-agent').select_option('pi')
    page.locator('#launch-cwd').fill('D:\\work\\MCP-cell')
    page.locator('[data-hub-action="launch"]').click()
    assert page.locator('.session-chip').count()==2
    checks.append('Claude Code and Pi have independent simultaneous session tabs')
    page.locator('.session-panel:visible .xterm-helper-textarea').focus()
    page.keyboard.type('explain project')
    page.keyboard.press('Enter')
    page.wait_for_timeout(150)
    assert '未执行' in page.locator('.session-panel:visible .xterm-rows').inner_text()
    checks.append('demo terminal accepts input but clearly does not execute')
    page.screenshot(path=str(ROOT/'dist/session-window.png'))
    page.locator('#nav-agents').click()
    assert page.locator('.session-chip').count()==2
    page.locator('#toast').evaluate('(e)=>e.classList.remove("visible")')
    page.screenshot(path=str(ROOT/'dist/agents-window.png'))
    first=page.locator('[data-session-tab]').first
    first.click()
    assert page.locator('.session-panel:visible .terminal-title strong').inner_text().startswith('Claude Code')
    assert page.locator('.session-panel').count()==2
    checks.append('switching main/session pages does not create another terminal')
    page.locator('.session-panel:visible [data-session-rename]').click()
    page.locator('#session-title').fill('CC · 电缆项目')
    page.locator('[data-hub-action="rename"]').click()
    assert 'CC · 电缆项目' in page.locator('#session-tabs').inner_text()
    checks.append('session tabs can be renamed')
    page.locator('[data-session-close]').first.click()
    assert '其他页签不受影响' in page.locator('#dialog').inner_text()
    page.locator('[data-hub-action="close"]').click()
    assert page.locator('.session-chip').count()==1
    assert 'Pi' in page.locator('.session-chip').inner_text()
    checks.append('confirmed close removes only the selected session')
    page.locator('.session-panel:visible [data-session-stop]').click()
    page.locator('[data-hub-action="stop"]').click()
    assert '已结束' in page.locator('.session-panel:visible [data-terminal-state]').inner_text()
    checks.append('stopping preserves the tab and its output')
    page.locator('#nav-mcp').click()
    assert page.locator('#page-mcp').is_visible()
    assert page.locator('#list .row').count()==9
    page.locator('[data-enroll]').first.click()
    page.locator('#confirm-enroll').click()
    page.locator('#tab-managed').click()
    assert page.locator('#list .row').count()==1
    checks.append('existing MCP grouping and enrollment flow still works')
    page.locator('[data-toggle]').first.click()
    assert '已暂停' in page.locator('#list').inner_text()
    page.locator('[data-restore]').first.click()
    page.locator('#confirm-restore').click()
    assert '还没有接入' in page.locator('#list').inner_text()
    checks.append('MCP pause and restore remain functional')
    page.locator('#nav-detection').click()
    page.locator('#add-scope').click()
    assert page.locator('#scan-workspace').is_visible()
    page.keyboard.press('Escape')
    checks.append('explicit project-directory detection dialog')
    page.keyboard.press('Alt+2')
    assert page.locator('#page-agents').is_visible()
    page.keyboard.press('Control+k')
    assert page.locator('#agent-search').evaluate('(e)=>document.activeElement===e')
    checks.append('page shortcuts and contextual search')
    page.set_viewport_size({'width':390,'height':844})
    assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
    page.screenshot(path=str(ROOT/'dist/agents-mobile.png'))
    checks.append('390px narrow viewport has no horizontal page overflow')
    page.emulate_media(reduced_motion='reduce')
    assert page.evaluate('matchMedia("(prefers-reduced-motion: reduce)").matches')
    checks.append('reduced-motion preference supported')
    browser.close()
assert not errors, errors
(ROOT/'dist/ui-results.json').write_text(json.dumps({'checks':checks,'errors':errors},ensure_ascii=False,indent=2),encoding='utf-8')
print(json.dumps({'passed':len(checks),'errors':errors},ensure_ascii=False))
