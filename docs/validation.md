# 0.3 validation · 2026-10-17

## Executed on the delivered working source

- Linux / Python 3.13.5: **125 unit, integration and HTTP tests passed**.
- `node --check` for app.js, hub.js and the locally supplied terminal bundle: passed. Node is a development-only syntax check, not an application server.
- Chromium + Playwright: **22 offline UI checks passed**, no JavaScript page errors.
- Approx. 1000×710 app window in an 1100×790 screenshot; 390px responsive layout.

## Real execution, not only a UI mock

The suite retains 0.2's actual MCP fixture, stdio bridge, configuration backup/recovery, pause/restore and conflict tests. New tests actually run a harmless Python Agent fixture in a POSIX PTY. They verify isatty, interactive Unicode input/output, distinct processes/workspaces, environment filtering, idempotent launch, resize notifications, stop, natural exit, cleanup, and closing one session without stopping another.

`tests/test_agent_live_http.py` exercises authenticated loopback HTTP registration → launch → real PTY → fixture response → resize → close. The fixture only echoes input; it never executes commands or calls a model. Real user configurations and credentials were not used.

## UI checks

Dedicated detection page; separate Agent/MCP detection; 10 built-in Agent entries; search; explicit collection without starting; collection cards; pin/disable; custom entry dialog; two independent Claude/Pi demo terminal tabs; input; switch without recreating terminals; rename; close one; stop retaining output; original MCP enrollment/pause/restore UI; explicit workspace selector; keyboard navigation; narrow screen; reduced motion.

The UI test uses a self-contained synthetic-data page. Its mutations are simulations and are not proof that production Claude Code, Pi, or other installed Agent applications have been exercised.

## Browser integration restriction

A normal browser navigation to the authenticated running loopback workbench was attempted again. Chromium returned `net::ERR_BLOCKED_BY_ADMINISTRATOR`. No policy was changed or bypassed. Full browser → live workbench integration therefore remains unverified; real HTTP/PTY tests and offline browser tests were performed separately.

## Terminal asset provenance

This source ZIP includes a working local terminal bundle sourced from the environment's installed Playwright trace viewer (1.57.0-beta-1764944708000), exposing its bundled Terminal and FitAddon through a small wrapper. Its xterm patch version was not independently established. SHA-256 receipts and MIT/Apache license and modification notices accompany it under mcpcell/web/vendor. No font files are redistributed.

Generated assets are not committed to Git. scripts/setup_terminal.py prepares fixed official xterm 5.5.0 and addon-fit 0.10.0 files by extracting verified npm tarball content without running package scripts. This alternative network build was not executed successfully in the isolated environment, so its browser behavior is not claimed as separately tested. ZIP assets are the ones used for the reported UI tests.

## Not verified / not delivered

Windows/macOS native operation, ConPTY interaction with real Agents, real Claude/Pi login/model conversations, automatic installation/update, credential switching, historical conversation import, hard memory limits, a security sandbox, shared MCP processes, remote MCP forwarding, and measured memory savings are not claimed. Existing MCP Job Object code and the new Agent ConPTY best-effort cleanup have different boundaries. Native-platform validation is required before treating this alpha as production-ready.

## Reproduce

```sh
python -m unittest discover -s tests -v
python scripts/build_preview.py
# optional developer browser checks
python -m pip install playwright
python -m playwright install chromium
python scripts/ui_smoke.py
```

Set CHROMIUM_PATH only to an existing browser executable when needed. Generated reports and screenshots go to ignored dist/. No real API keys or terminal access tokens belong in test artifacts.
