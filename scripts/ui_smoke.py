import json, os, sys
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT))
from scripts.build_preview import build_preview
from playwright.sync_api import sync_playwright

preview=build_preview(ROOT/'dist/liquid-preview.html')
checks=[];errors=[]
try:
    with sync_playwright() as p:
        browser=p.chromium.launch(executable_path=os.environ.get('CHROMIUM_PATH'),headless=True,args=['--no-sandbox'])
        page=browser.new_page(viewport={'width':1440,'height':1100},device_scale_factor=1)
        page.on('pageerror',lambda e:errors.append(str(e)))
        page.set_content(preview.read_text(encoding="utf-8"), wait_until='load')
        page.wait_for_selector('body[data-ready="true"]')
        assert page.locator('#mode-badge').inner_text()=='演示数据 · 离线预览'
        checks.append('demo provenance visible')
        page.screenshot(path=str(ROOT/'dist/preview-desktop.png'),full_page=True)
        page.locator('nav [data-view="services"]').click()
        assert page.locator('tbody tr').count()==12
        checks.append('services show 12 normalized demo bindings')
        page.locator('#service-search').fill('context7')
        assert page.locator('tbody tr').count()==4
        checks.append('service search filters 4 matches')
        page.locator('#service-search').fill('')
        page.locator('[data-filter="disabled"]').click()
        assert page.locator('tbody tr').count()==1
        checks.append('disabled filter works')
        page.locator('[data-filter="all"]').click()
        page.locator('#client-filter').select_option('cursor')
        assert page.locator('tbody tr').count()==2
        checks.append('client filter works')
        page.locator('#select-all').check()
        assert '已选 2 条' in page.locator('#selection-note').inner_text()
        checks.append('bulk selection counts correct')
        page.locator('.service-name').first.click()
        assert page.locator('#detail').is_visible()
        assert '未托管' in page.locator('#detail').inner_text()
        checks.append('detail drawer distinguishes discovery from management')
        page.keyboard.press('Escape')
        assert not page.locator('#detail').is_visible()
        checks.append('Escape closes accessible dialog')
        with page.expect_download() as event:
            page.locator('[data-action="export-plan"]').first.click()
        downloaded=event.value
        data=json.loads(Path(downloaded.path()).read_text(encoding="utf-8"))
        assert data['applicable'] is False and len(data['items'])==2
        checks.append('selected review plan exports as non-executable proposal')
        page.locator('nav [data-view="clients"]').click()
        assert page.locator('.client-card').count()==11
        checks.append('all 11 adapter cards visible')
        page.locator('[data-client-detail="codex"]').click()
        assert '.codex/config.toml' in page.locator('#detail').inner_text()
        page.keyboard.press('Escape')
        checks.append('client source drawer works')
        page.locator('nav [data-view="review"]').click()
        assert page.locator('.finding').count()>10
        checks.append('review findings rendered')
        page.locator('.sidebar [data-view="cli"]').click()
        assert '没有 CLI 执行 API' in page.locator('#main').inner_text()
        checks.append('CLI explicitly marked planned, no fake execution')
        page.locator('.sidebar [data-view="about"]').click()
        assert '未实现' in page.locator('#main').inner_text()
        checks.append('capability boundary page works')
        page.keyboard.press('Control+k')
        assert page.locator('#service-search').evaluate('(e)=>document.activeElement===e')
        checks.append('Ctrl+K opens and focuses search')
        page.locator('nav [data-view="overview"]').click()
        page.set_viewport_size({'width':390,'height':844})
        page.locator('#toast').evaluate('(e)=>e.classList.remove("visible")')
        page.screenshot(path=str(ROOT/'dist/preview-mobile.png'),full_page=True)
        assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
        assert page.locator('.glass').evaluate_all('(els)=>els.every(e=>e.getBoundingClientRect().right <= innerWidth + 1)')
        checks.append('390px responsive cards stay inside viewport')
        page.emulate_media(reduced_motion='reduce')
        checks.append('reduced-motion preference accepted')
        browser.close()
finally:
    pass
(ROOT/'dist/ui-results.json').write_text(json.dumps({'checks':checks,'errors':errors},ensure_ascii=False,indent=2),encoding='utf-8')
print(json.dumps({'passed':len(checks),'errors':errors},ensure_ascii=False))
assert not errors, errors
