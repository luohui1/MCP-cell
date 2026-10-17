# 0.2 validation · 2026-10-17

## Executed

- Linux, Python 3.13.5: **87 unit / integration / HTTP tests passed**.
- Node `--check mcpcell/web/app.js`: passed (development-only syntax check).
- Chromium / Playwright: **16 offline UI checks passed**, no JavaScript page errors.
- Read-back and byte-exact restore tests use isolated temporary source files, never the user's real AI configurations.

New tests actually execute a harmless local fixture MCP through the broker and through the installed-style bridge command. Coverage includes JSON-RPC initialization/list/call, unchanged IDs, separate processes for concurrent clients, environment filtering, authorization rejection, delayed process creation, EOF cleanup, pause, persistence, prepare/recovery, same-file batch enrollment, reverse and forward restore order, source conflicts, JSONC comments, Codex TOML, OpenCode arrays, and the authenticated enrollment/pause/restore HTTP endpoints.

Dependencies present during execution: tomlkit 0.13.3, psutil 7.2.2. The application accepts psutil 7.x; other patch versions were not separately tested.

## UI

1100×790 screenshot contains a 960×660 tool window. Also checked 390×844 with no page-width overflow. Tests cover detection-source grouping, search, client filter, selection counts, explicit confirmation, managed tab, pause/enable/restore, source detail, Escape, settings, keyboard search and reduced-motion preference. Offline mutation tests are UI simulations, not substitutes for the actual backend tests above.

## Environment restriction

The actual browser-to-loopback test was attempted, but Chromium returned `ERR_BLOCKED_BY_ADMINISTRATOR`. Browser policy was not altered. The full browser → running server path therefore remains unverified in this environment; backend HTTP tests and offline UI checks were performed separately. Temporary access tokens are not included in this report.

## Not claimed

No Windows/macOS native execution, production AI-client session, remote MCP proxy, cross-session shared process, memory-savings benchmark, cgroups, hard memory limit, full security sandbox, CLI execution or Windows executable packaging has been verified or delivered. Linux process-group cleanup is not containment for hostile processes that deliberately escape the group. Windows suspended-start + Job Object implementation remains subject to native validation.
