# Claude Code Project Entry

Read [PROJECT_IDENTITY.md](PROJECT_IDENTITY.md) first: **LedgeSync**, `ledgesync`,
`ledgesync.com`. Then read [AGENTS.md](AGENTS.md) and
[CLAUDE_CODE_HANDOFF.md](CLAUDE_CODE_HANDOFF.md), the current continuation entrypoint.

The owner reports that the application is not synchronizing files. Diagnose and
fix that actual workflow before claiming the product works or preparing another
release. Preserve the existing implementation and recorded evidence; do not
restart the old offline bootstrap or invent a root cause from the report alone.

The handoff defines the current checkout, relative-path convention, reproduction
sequence, completion scope, native security requirements, permission boundaries
and acceptance gates. Keep implementation, documentation and tests in English.
Shared engineering rules remain in AGENTS.md; this file does not create a second
policy, configure permissions or authorize unrelated external actions.
