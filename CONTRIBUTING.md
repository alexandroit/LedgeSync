# Contributing to LedgeSync

LedgeSync is an early, GUI-first file synchronization project. Start with
[the current implementation status](docs/17_AGENT_HANDOFF_AND_STATUS.md),
[architecture](docs/03_ARCHITECTURE.md), and [safety invariants](docs/05_SYNC_SAFETY_AND_STATE.md).

Open an issue for a focused change, use a feature branch, and send a pull request.
Keep code, comments, documentation, and commits in English. Contributions are
licensed under Apache-2.0 unless explicitly stated otherwise.

Run `go test ./...`, `go vet ./...`, and `go test -race ./...` for core changes.
Run `npm ci` and `npm run build` inside `frontend` for desktop frontend changes.
Run `python3 tools/validate_docs.py` for documentation and contract changes.

Use temporary fixture roots and fake providers. Never attach real account tokens,
private file contents, personal source trees, or upload-session URLs to an issue.
Every filter change needs a conformance regression. Every mutation feature needs
its safety gates and recovery tests before it can be enabled.

Do not report a platform as supported merely because it cross-compiles. Record
the exact runner, architecture, build result, and any installation/runtime gaps.
