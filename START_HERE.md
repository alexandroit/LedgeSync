# Start Here — LedgeSync

Read [PROJECT_IDENTITY.md](PROJECT_IDENTITY.md), [AGENTS.md](AGENTS.md), then
[HANDOFF.md](HANDOFF.md).

**Current blocking report:** the owner says the application is not synchronizing
files. Source and recorded alpha.4 distribution exist; successful live file
transfer has not been established. The handoff contains the exact diagnosis order,
remaining product work and evidence required to finish.

## Workspace

This directory is the implementation repository. Resolve its root from the active
checkout; use relative file references rather than a previous machine's mount or
user directory. Commands run from this root unless a document explicitly says
otherwise. Markdown links resolve from their containing document.

The moved checkout contains tracked source, specifications, website, notices and
release evidence. Generated dependencies/builds and private OAuth build input are
not tracked. Recreate them with the documented build workflow; do not interpret
their absence as lost application code.

## Reading order

1. [Current continuation](HANDOFF.md) and
   [status/history](docs/17_AGENT_HANDOFF_AND_STATUS.md).
2. [Requirements](docs/01_PRODUCT_REQUIREMENTS.md),
   [architecture](docs/03_ARCHITECTURE.md),
   [filter contract](docs/04_FILTER_ENGINE_SPEC.md),
   [sync safety](docs/05_SYNC_SAFETY_AND_STATE.md),
   [Drive provider](docs/06_GOOGLE_DRIVE_PROVIDER.md).
3. [Desktop/CLI workflows](docs/08_DESKTOP_CLI_AND_AUTOMATION.md),
   [test strategy](docs/10_TEST_STRATEGY_AND_ACCEPTANCE.md),
   [backlog](docs/11_IMPLEMENTATION_BACKLOG.md),
   [ADRs](docs/12_ADR_DECISIONS.md), and
   [explorer/adapter contract](docs/20_DESKTOP_EXPLORER_AND_CODE_POLICY_ADAPTERS.md).
4. [Completed upstream audit](docs/research/RCLONE_SOURCE_AUDIT.md),
   [reuse matrix](docs/research/RCLONE_REUSE_MATRIX.md),
   [build guide](docs/PLATFORMS.md#build-from-source), and
   [live acceptance checklist](docs/research/DRIVE_UPLOAD_ACCEPTANCE.md).

## Current success gate

A user chooses a source and authorized Drive parent, understands the preview,
approves it, and obtains independently verifiable copies with hierarchy, empty
folders and active ignore rules preserved. Repeated unchanged runs avoid duplicate
copies; interruption reconciles durable IDs; invalid or missing rules stop mutation.
The native GUI must demonstrate this behavior, not merely the fake-provider CLI.

The full completion scope, including saved projects, settings/policies/history,
opt-in automation, adapter coverage and release boundaries, is in the handoff.
Do not silently reduce the project to another visual/offline slice or enable
unfinished destructive features to satisfy a checklist.

## Documentation checks

From the repository root:

```sh
python3 -B tools/validate_docs.py
python3 -B tools/verify_licenses.py
```

Schema validation requires `jsonschema`; a missing dependency is a skipped check,
not a pass. These checks do not exercise a real Google account or fix the app.
The handoff and pinned workflows describe actual Go, frontend, native-package and
live acceptance checks. The original `PACKAGE_MANIFEST.json` is historical
provenance, not the current source inventory; do not use `--package` on this checkout.
