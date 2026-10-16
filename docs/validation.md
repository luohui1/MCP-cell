# Validation record · 2026-10-16

## Executed in this development environment

- Linux, Python 3.13.5: `python -m unittest discover -s tests -v` — **55 tests passed**.
- `node --check mcpcell/web/app.js` — syntax check passed. Node is a development check only, not an application runtime dependency.
- Chromium + Playwright: **17 offline UI smoke checks passed**, no JavaScript page errors. Desktop viewport 1440×1100; narrow viewport 390×844. Cards stay within the narrow viewport; data tables scroll inside their own containers.
- Demo mode runs only synthetic configurations in an isolated temporary directory: 7 client families with found configs, 12 bindings, 1 equal-configuration candidate group. These are sample counts, not measurements from the user's computer.

## Coverage

Unit/API tests cover JSON/JSONC/TOML, comments and escaped strings, repeated keys, invalid/non-finite/deep input, file/node/binding limits, symbolic links and FIFO handling, secret-value omission, source immutability, account/config separation, same-physical-file handling, explicit workspace scopes, no command execution, and non-applicable review plans.

API tests use a real loopback HTTP server through Python's HTTP client: authentication, cookies, Host/Origin checks, cross-site rejection, invalid request shapes, body limits, rescan throttling, static-file allowlist and absent execution endpoints.

UI checks use a self-contained demo document: service search, disabled/client filters, selection, detail dialogs, Escape, plan download, 11 client cards, source details, review findings, CLI/capability boundary labels, Ctrl+K, narrow-screen card bounds and reduced-motion preference. The reduced-motion test confirms the preference can be supplied; it is not a full accessibility certification.

## Explicitly not verified

The browser environment blocked navigation to loopback with `ERR_BLOCKED_BY_ADMINISTRATOR`. No attempt was made to disable that policy. Browser rendering and interactions were therefore tested with the offline document, while HTTP integration was tested separately. A full browser-to-running-server integration test remains outstanding.

Windows/macOS native execution and real installed AI-client configurations were not tested. Platform path fixtures are not a substitute for native tests. No live MCP/CLI execution, migration, sandbox, process reuse, cgroups/Job Objects, OOM prevention or measured memory savings are claimed.

## Reproduce

```sh
python -m unittest discover -s tests -v
python scripts/build_preview.py
```

Optional UI development test (Playwright is not required to run MCP-cell):

```sh
python -m pip install playwright
python -m playwright install chromium
python scripts/ui_smoke.py
```

To use an already installed Chromium, set `CHROMIUM_PATH` to its executable. The script does not remove administrator browser policies. Generated previews, screenshots and test reports go to ignored `dist/`.
