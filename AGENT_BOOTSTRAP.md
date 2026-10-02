# Continuation Prompt for the Coding Agent

**Updated:** 2026-10-02. **Project:** LedgeSync. **Specification:** 0.2.1.

Open this repository root, then give the agent this prompt:

> Read PROJECT_IDENTITY.md, AGENTS.md and HANDOFF.md. Continue the
> existing LedgeSync project in this checkout. The owner reports that files do
> not synchronize. Reproduce the installed application's actual failure, preserve
> user data and native credential protections, fix it with regression tests, and
> finish the defined product workflows using the ordered completion and acceptance
> gates in the handoff. Do not start over, stop after another plan/document, or
> call an OAuth connection, mock test, successful build or published installer
> proof of a real upload. Use repository-relative paths and runtime-derived roots.
> Preserve historical release identities and existing user changes. Continue
> authorized local work independently; ask only for missing information or
> external authorization that is genuinely necessary and not already supplied.
> Record actual commands/results, live acceptance, skipped checks and blockers in
> docs/17_AGENT_HANDOFF_AND_STATUS.md. Work in English.

[HANDOFF.md](HANDOFF.md) is the single start document;
[START_HERE.md](START_HERE.md) is a short orientation map.

## Expected first work

Inspect the actual checkout and installed artifact, prepare missing local
build/test inputs, and trace the reported failure through the GUI, shared transfer
service, authorization/provider and journal. Distinguish confirmed behavior from
hypotheses. Produce working changes and acceptance evidence, not a new offline demo.

The mandatory upstream audit was already executed; inspect its
[baseline](docs/research/UPSTREAM_BASELINE.json),
[source analysis](docs/research/RCLONE_SOURCE_AUDIT.md) and
[reuse decisions](docs/research/RCLONE_REUSE_MATRIX.md). Re-verify pinned upstream
source when needed for a change, without throwing away completed application work.
