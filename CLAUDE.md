# Claude Code Project Entry

Read [AGENTS.md](AGENTS.md) as the shared implementation policy. Then follow [START_HERE.md](START_HERE.md) and the [bootstrap prompt](CODEX_CLAUDE_BOOTSTRAP.md).

Do not maintain a second, divergent set of engineering rules in this file. The filter and synchronization specifications are authoritative for their domains. Keep all code, documentation, tests, commits, and implementation reports in English.

First complete [the mandatory rclone source analysis](docs/19_RCLONE_SOURCE_ANALYSIS_AND_IMPLEMENTATION.md), including a real clone, pinned baseline, source/test audit, and reuse decision. Then implement the offline policy/filter engine, major VCS adapter foundation, fake provider, and functional local desktop Files explorer; do not stop at documentation. Read `docs/20_DESKTOP_EXPLORER_AND_CODE_POLICY_ADAPTERS.md`. Do not connect the user's real Google account, mutate their Drive, enable startup jobs, or execute destructive commands merely because the implementation task mentions synchronization.

Claude-specific memory or permission settings are not configured by this documentation package. Verify the installed tool's official behavior before relying on automatic file discovery. See sources A02 and A03 in the source register.
