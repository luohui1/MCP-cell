> 0.5 补充：本轮 UI 已改为正常浏览器工作区、按需辅助视图和会话选择器。运行能力边界以 README 与 unification-plan.md 为准；下文为先前版本设计记录。

# MCPcell execution contract · 0.2 status

This is a project-specific design, not an official MCP standard. The root README and validation record describe the delivered capabilities.

## Implemented now

ClientAdapter discovery is separate from execution. Discovery retains the original client, file, scope, workspace, format and source version. It does not execute a configured program.

For eligible local stdio entries, explicit user confirmation creates a private backup and durable prepared record, then atomically replaces only the selected launch command/arguments with a bridge. JSON/JSONC and TOML use format-aware patches; other client fields remain unchanged. A subsequent client connection is authenticated to the manager. The manager checks the current source still matches the approved bridge entry before starting the original command.

Each connection receives an independent process. JSON-RPC bytes are forwarded without request-ID rewriting, capability fabrication or tool renaming. Startup occurs after the first protocol frame, not only after a tools/call request. There is no shared-upstream multiplexing in this version.

The manager caps accepted bridge connections, protocol frame length and selected operations. PID and process-tree RSS are observations, not enforced per-instance memory limits. Pause closes connections and terminates the owned process group. Cancellation, disconnect or timeout does not imply business rollback; ongoing calls are not automatically replayed.

Restore compares the target against the written bridge entry. If the entire file is unchanged, restore the original bytes. Otherwise restore only the unchanged managed entry while preserving unrelated edits. If the entry itself changed, report conflict. Batch enrollment is per-entry, not an all-files transaction. Check-before-replace cannot eliminate races with non-cooperating external writers.

## Security scope

The local management API has separate credentials from bridge tickets, Host/Origin checks and bounded request handling. Credentials and original configuration values stay out of browser snapshots. Windows uses current-user DPAPI for stored state; POSIX uses private directory and file permissions. Neither provides a defense against a fully compromised user account.

Windows process ownership is established using a suspended child and Job Object before resuming its thread. Native Windows validation remains outstanding. POSIX process groups support cleanup but are not a hostile-process containment boundary. Full sandbox, file/network permission enforcement and hard memory budgets remain future work.

## Next execution adapters

Remote MCP HTTP/SSE remains discovery-only. CLI will use explicitly registered executable artifacts and argv schemas rather than arbitrary shell text. MCP and CLI will share identity, workspace, authorization, budget, lifecycle and audit concepts without conflating an AI-client configuration adapter with a protocol execution adapter.

Safe sharing is opt-in only after validating a pinned artifact, configuration, identity/credential domain, workspace/mount boundary, policy version and state model. Equal configuration fingerprints are not sharing approval, and permissions must not be unioned to improve reuse. Request-level hard memory isolation cannot be promised inside a single shared process.

## 0.3 implementation addendum

The compact UI now separates detection, Agent collection and MCP management and adds independent interactive Agent tabs. AgentHub explicitly registers local CLI Agent entrypoints, then uses POSIX PTY or a preview ConPTY backend to launch them. This is a supported subset of CLI execution, not a general shell or parameterized job scheduler. MCP runtime remains per-connection exclusive; Agent integration does not automatically inject MCP tools or inherit a sandbox. See agents.md and validation.md for actual limits and platform verification.
