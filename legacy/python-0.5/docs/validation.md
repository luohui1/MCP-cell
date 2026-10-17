# Validation · 0.5.0a1 · 2026-10-17

## This round actually executed

- `python -m unittest discover -s tests -v`: **130 tests passed**, 29.200 seconds, Linux in this environment. Includes prior real fixture MCP/PTY/HTTP integration tests and two new browser-launch unit tests.
- `node --check mcpcell/web/app.js` and `node --check mcpcell/web/hub.js`: passed.
- `python scripts/ui_smoke.py`: **38 checks passed**, zero browser JavaScript page errors, synthetic offline document.
- Viewports: 1366×900, 1920×1080 and 390×844. Main workspace fills viewport rather than forcing app-window dimensions. Outer radius 0, primary controls 4px. No role=tab or session-strip.
- Reproduced 0.4 bug before the fix: under Cursor-only filter, context7 enrollment confirmation included Codex, Claude Code, Cursor and Gemini CLI (4 bindings). After the fix it includes only Cursor (1 binding). Added single/bulk enrollment, scoped pause/restore and all-sources behavior checks.
- UI also verifies icon library, missing Agent behavior, collection/pin/disable/manual form, independent sessions and picker, rename/stop/close, last-session return to MCP, search, source navigation, zero offline requests and reduced-motion preference.

## Browser-to-running-server attempt

`python scripts/browser_integration.py` starts an isolated demo Workbench and opens its normal authenticated loopback URL. This attempt returned `ERR_BLOCKED_BY_ADMINISTRATOR`. No browser policy was changed or bypassed. Result: **blocked, zero full browser-to-server checks completed**. Do not present separate HTTP and offline UI tests as this end-to-end test passing.

## Not verified / not implemented

Windows/macOS native behavior, production Claude/Pi/model conversations, native window manager behavior and real user installations were not tested. Opening the default browser is unit-tested by mocking its documented Python interface; a real desktop user's browser preference was not exercised here.

No shared instance pool, protocol-aware aggregate gateway, tool-call-only lazy start, hard memory budget, remote MCP relay or complete filesystem/network sandbox was added. These are in `unification-plan.md` as proposals. Source-level limits and process-tree RSS reporting are not equivalent to OOM prevention.

Local source baseline is the supplied 0.4 ZIP. GitHub PR #1 was read and still points to 0.3 commit `889f0a757da5c2492e44be60fedc76233588eb20`; this round does not claim a push or merge. No files or MCP configuration on the user's actual workstation were changed by these container tests.

## Reproduction

```sh
python -m unittest discover -s tests -v
python scripts/build_preview.py
python scripts/ui_smoke.py
python scripts/browser_integration.py
```

Playwright/Chromium are optional development dependencies, not required to run the application. Runtime dependencies remain in requirements.txt. The ZIP retains the preceding round's locally bundled terminal assets and notices; their supply provenance was not changed this round.
