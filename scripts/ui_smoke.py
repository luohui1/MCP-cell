"""Small-window UI interactions. Real HTTP integration is attempted separately."""
import json
import os
import sys
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT))
from scripts.build_preview import build_preview
from playwright.sync_api import sync_playwright

preview=build_preview(ROOT/'dist/compact-preview.html')
checks=[];errors=[]
with sync_playwright() as p:
    browser=p.chromium.launch(executable_path=os.environ.get('CHROMIUM_PATH','/usr/bin/chromium'),headless=True,args=['--no-sandbox'])
    page=browser.new_page(viewport={'width':1100,'height':790},device_scale_factor=1)
    page.on('pageerror',lambda e:errors.append(str(e)))
    page.set_content(preview.read_text(encoding='utf-8'),wait_until='load')
    page.wait_for_selector('body[data-ready="true"]')
    assert page.locator('.window').evaluate('(e)=>Math.round(e.getBoundingClientRect().width)')==960
    assert page.locator('.window').evaluate('(e)=>Math.round(e.getBoundingClientRect().height)')==660
    checks.append('960x660 compact window, no dashboard sidebar')
    assert page.locator('.row').count()==9
    checks.append('12 source bindings grouped into 9 service rows')
    assert '演示' in page.locator('#connection').inner_text()
    checks.append('demo provenance visible')
    page.locator('#search').fill('context7')
    assert page.locator('.row').count()==1
    checks.append('live search')
    page.locator('#search').fill('')
    page.locator('#client').select_option('cursor')
    assert page.locator('.row').count()==2
    checks.append('source filter')
    page.locator('#client').select_option('all')
    page.locator('[data-select]').nth(0).check();page.locator('[data-select]').nth(2).check()
    assert '5' in page.locator('#selection').inner_text()
    checks.append('selection counts source bindings, not false process savings')
    page.screenshot(path=str(ROOT/'dist/compact-window.png'))
    page.locator('#enroll-selected').click()
    assert page.locator('#dialog').is_visible()
    assert '备份' in page.locator('#dialog').inner_text()
    checks.append('enrollment requires explicit confirmation')
    page.screenshot(path=str(ROOT/'dist/enroll-confirm.png'))
    page.locator('#confirm-enroll').click()
    assert not page.locator('#dialog').is_visible()
    assert page.locator('#tab-managed span').inner_text()=='2'
    checks.append('demo enrollment updates managed state')
    page.locator('#tab-managed').click()
    assert page.locator('.row').count()==2
    page.locator('[data-toggle]').first.click()
    assert '已暂停' in page.locator('.row').first.inner_text()
    checks.append('pause control updates managed state')
    page.locator('[data-toggle]').first.click()
    assert '已接入' in page.locator('.row').first.inner_text()
    checks.append('resume control')
    page.locator('[data-restore]').first.click()
    page.locator('#confirm-restore').click()
    assert page.locator('.row').count()==1
    checks.append('restore removes managed bindings')
    page.locator('.name').first.click()
    assert '独占' in page.locator('#dialog').inner_text()
    page.keyboard.press('Escape')
    assert not page.locator('#dialog').is_visible()
    checks.append('source detail and Escape close')
    page.locator('#settings').click()
    assert '尚未提供' in page.locator('#dialog').inner_text()
    page.keyboard.press('Escape')
    checks.append('limits in settings, not dashboard clutter')
    page.keyboard.press('Control+k')
    assert page.locator('#search').evaluate('(e)=>document.activeElement===e')
    checks.append('keyboard search shortcut')
    page.locator('#tab-all').click();page.set_viewport_size({'width':390,'height':844})
    page.locator('#toast').evaluate('(e)=>e.classList.remove("visible")')
    page.screenshot(path=str(ROOT/'dist/compact-mobile.png'))
    assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
    checks.append('390px width without page overflow')
    page.emulate_media(reduced_motion='reduce')
    assert page.evaluate('matchMedia("(prefers-reduced-motion: reduce)").matches')
    checks.append('reduced-motion preference')
    browser.close()
assert not errors,errors
(ROOT/'dist/ui-results.json').write_text(json.dumps({'checks':checks,'errors':errors},ensure_ascii=False,indent=2),encoding='utf-8')
print(json.dumps({'passed':len(checks),'errors':errors},ensure_ascii=False))
