# MCP-cell

A local-first control plane for discovering and governing MCP tools across AI clients, with a shared execution contract for future CLI adapters.

## Development status

The discovery and liquid-minimal console prototype is being developed on a feature branch. No managed runtime, process sharing, automatic client migration, sandbox, or memory savings have been verified yet.

## Principles

- Discover known client configuration locations without executing configured commands.
- Preserve client-specific scope, authorization, workspace, and unknown configuration fields.
- Treat duplicate definitions as review candidates, never proof of safe process sharing.
- Keep secrets out of browser responses and exported reports.
- Make every resource and isolation guarantee explicit and testable.
