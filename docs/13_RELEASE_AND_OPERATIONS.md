# 13 — Release, CI, and Operations

## Repository and CI

Establish real build/test commands during P0 and pin versions in the repository. Do not pretend a nonexistent `go test ./...` result belongs to this documentation delivery. Product CI must run formatting, static checks, Go unit/race tests, parser fuzz seeds, schema/contract checks, differential suites against pinned references, frontend type checks/tests, and dependency/license/secret scanning.

Run untrusted pull requests without production cloud credentials or write-capable release tokens. Pin workflow actions to reviewed revisions, minimize permissions, and separate publish jobs from ordinary tests. Live Drive tests are opt-in for an authorized disposable account/root and never run automatically against user files.

## Packaging

Start with the user's macOS desktop acceptance, preserving portability in core design. Verify the chosen stable Wails/Go/Node compatibility, native build prerequisites, Intel/Apple Silicon strategy, and vault integration in the packaging spike. Linux/Windows support is a target until built/tested there, not an inherited guarantee from a framework list. [A01]

Package only required components. The end user must not need rclone or Git for normal operation; Git/rclone are development reference tools. Installer behavior must not silently enable startup/background scheduling. Signed/notarized public distribution, developer accounts, and app update trust are explicit owner/release gates.

## Releases and updates

Use semantic application versions and separately version config, plan, event and state schemas. Generate an SBOM and third-party notices from the actual dependency graph. Publish checksums and signatures through the approved release channel. The updater must verify authenticity and cannot fetch/execute arbitrary scripts.

Back up local state before schema migration. New versions detect incompatible configs/plans, preserve original files, and require review for safety-changing migrations. Rollback cannot assume an older binary understands a newer state DB; define export/restore or supported downgrade procedures. A migration must never broaden filters or enable deletion silently.

## Runbooks

**Authentication or permission failure:** pause jobs, classify the reason, show the account/scope, reconnect explicitly when necessary, and rescan after access restoration. Never expand scope as an invisible retry.

**Source volume missing:** mark the project unavailable, retain last-good inventory, perform no mirror cleanup, and require stable identity when the volume returns.

**Filter parse error or missing previously active rule:** block execution, show relative file/line/group, retain old approved snapshots only for diagnosis, and create a selection diff after correction.

**Interrupted upload:** use the journal and provider-reported state; revalidate source; resume or reconcile safely. Unknown remote outcome stays unresolved until identity checks succeed.

**Integrity failure:** report the operation as failed, preserve recovery/previous baseline, investigate content/source changes and provider hash capabilities. Do not retry destructive replacement blindly.

**Conflict or suspected concurrent writer:** pause managed updates/mirror, preserve both, identify actual object IDs, and reestablish the supported writer model before replanning.

**State DB corruption:** stop mutations. Preserve a forensic copy, inspect recovery catalog and remote ownership markers, and use explicit reconstruction/adoption procedures. A missing local DB does not authorize treating an entire remote folder as owned.

**Deletion incident:** stop further jobs, preserve logs/journal, enumerate exact affected IDs, restore from verified recovery into a new location, and investigate violated preconditions. Do not empty trash or overwrite sources while investigating.

## Privacy and diagnostics

Keep bounded local logs and run histories. Export a redacted support bundle only after showing what is included; avoid credentials, session URLs, private content and absolute source paths. Provide a documented way to disconnect credentials and clear local data without implying remote copies were deleted.

## Completion gates

A copy alpha can ship privately without mirror/mount/bisync. Public release requires completed signing/privacy/license decisions and honest supported-capability documentation. Full initial release includes the P4 recovery/managed-mirror gates or explicitly disables unfinished destructive features. There is no requirement to claim parity with every rclone command before delivering useful safe copy.
