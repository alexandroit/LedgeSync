# Native Drive create-only provider review

Reviewed and implemented on 2026-10-01. This bounded follow-up opens the Drive
provider slice deferred in [the original reuse matrix](RCLONE_REUSE_MATRIX.md).
It does not change OAuth scopes or authorize an unattended cloud integration test.

## Source decision

The existing reference checkout was re-read at rclone v1.75.1, full commit
`687d264b689b8c49a67e2e52a8a5e0caa01c04ce` (verified with `git rev-parse HEAD`).

| Source inspected | Observed mechanism | Decision for this slice |
|---|---|---|
| [upload.go, lines 53–238](https://github.com/rclone/rclone/blob/687d264b689b8c49a67e2e52a8a5e0caa01c04ce/backend/drive/upload.go#L53-L238) | Session initialization, replayable buffers, per-chunk pacing; chunk offset advances by submitted size in the inspected loop | Reimplement the narrow protocol using standard-library HTTP. Query the committed offset after interruption, obey partial acknowledgements, and reconcile the journaled preallocated object ID before another creation attempt. No rclone upload source is copied. |
| [drive.go, lines 1021–1200](https://github.com/rclone/rclone/blob/687d264b689b8c49a67e2e52a8a5e0caa01c04ce/backend/drive/drive.go#L1021-L1200) | Parent-ID query, explicit page cursor; incomplete-search logging; optional shortcut resolution | Preserve duplicate names, return an error on incomplete search, expose explicit page boundaries, reject shortcut destinations, and keep shared-drive support gated. |
| [drive_test.go, lines 1–48](https://github.com/rclone/rclone/blob/687d264b689b8c49a67e2e52a8a5e0caa01c04ce/backend/drive/drive_test.go#L1-L48) | Generic remote integration suite using `TestDrive:` | Read only. Do not execute it with a user's configuration. Use an isolated fake HTTP server for this provider. |

The concrete reason for new upload code is the approved-intent boundary: rclone's
inspected implementation carries filesystem/global pacing dependencies, permits
whole initialization retry, supports updates, and does not itself provide this
application's journal/identity checks. Importing it would add a substantially
larger runtime surface. This adapter adds no dependency beyond Go's standard
library and existing domain/authentication packages. No rclone or Google sample source was
copied into the new provider, so no additional copied-code notice is required.

Inspected source SHA-256 values:

```text
backend/drive/upload.go 2eb0130c8a62e18ec5e14726ebb4a48f36996837beb26363b49df6bb9a21889d
backend/drive/drive.go b29d8dd733425cb3e2e54e460586d8a966ccc1b88cedfe48b9decb49049ce41a
backend/drive/drive_test.go 09f60844f11d4fe0abe48717b02ea393d078b740064839e8f72f212ff7512ea3
```

## Executor source-selection addendum

The same pinned checkout was reviewed again for the approved planner/executor
boundary. Its local reference path during this review was
`/Users/alexandro/dev/reference-sources/rclone-ledgesync`, outside the LedgeSync
checkout and selected upload roots. No upstream file was changed or executed.

| Inspected seam at the pinned SHA | Observed behavior | LedgeSync decision |
|---|---|---|
| [copy.go, `updateOrPut`, lines 213–242](https://github.com/rclone/rclone/blob/687d264b689b8c49a67e2e52a8a5e0caa01c04ce/fs/operations/copy.go#L213-L242) | Chooses destination update or provider put based on the current destination and partial/in-place policy. | Retain the narrow create-only provider port; an existing destination ID is reconciliation evidence, never permission to update. |
| [copy.go, `copy`, lines 307–374](https://github.com/rclone/rclone/blob/687d264b689b8c49a67e2e52a8a5e0caa01c04ce/fs/operations/copy.go#L307-L374) | Retries server-side/manual copy using low-level retry policy, resets accounting, verifies afterwards, removes a failed copy and may rename a partial copy. Retry-After handling in this seam sleeps directly. | Do not reuse this whole loop. Journal a preallocated ID and marker before mutation, reconcile uncertain results by that ID, keep failed/partial remote IDs for review, and use cancellable waits. No cleanup deletion is authorized by upload failure. |
| [copy.go, `verify` and `Copy`, lines 286–301 and 390–425](https://github.com/rclone/rclone/blob/687d264b689b8c49a67e2e52a8a5e0caa01c04ce/fs/operations/copy.go#L286-L425) | Size/common-hash verification, configuration-bound accounting and a dry-run/destructive-operation decision at execution time. | Keep size/hash verification as a requirement, but require the expected digest, stable parent/object identity and explicit approved plan before execution. Do not equate a live dry-run flag with approval of an immutable plan. |
| [sync.go, `pairChecker`, lines 371–475](https://github.com/rclone/rclone/blob/687d264b689b8c49a67e2e52a8a5e0caa01c04ce/fs/sync/sync.go#L371-L475) and [`pairCopyOrMove`, lines 500–526](https://github.com/rclone/rclone/blob/687d264b689b8c49a67e2e52a8a5e0caa01c04ce/fs/sync/sync.go#L500-L526) | Evaluates transfer need while consuming live object pairs; optional immutable checks, backup moves, case correction, copy/move dispatch and source removal share the execution pipeline. | Use the existing LedgeSync policy scan to create a frozen plan, then a separate original executor that revalidates account/root/config/rules/content and only admits its approved operations. Source removal, rename and overwrite are absent from this port. |
| [sync.go, `deleteFiles`, lines 627–666](https://github.com/rclone/rclone/blob/687d264b689b8c49a67e2e52a8a5e0caa01c04ce/fs/sync/sync.go#L627-L666) | Checks accumulated errors and configuration before processing unmatched destination objects for deletion. | Do not import the deletion path. The first folder-copy workflow never infers a deletion from absence. |

**Decision:** original, narrowly typed approval/execution and journal layers,
reusing the existing LedgeSync discovery/policy implementation and the reviewed
SQLite driver. No copy/sync execution source is adapted. The inspected upstream
seams do not supply this application's durable approved-plan and preallocated-ID
contract; this is a finding about those seams, not a claim that no upstream
subsystem has any checkpointing or recovery feature. Importing an entire copy or
sync pipeline would also expose mutation capabilities outside this milestone.

Associated upstream fixtures read: [`TestCopyWithDryRun` and `TestCopy`](https://github.com/rclone/rclone/blob/687d264b689b8c49a67e2e52a8a5e0caa01c04ce/fs/sync/sync_test.go#L49-L84),
and [`TestSyncAfterRemovingAFileAndAddingAFileDryRun` plus the mutation case](https://github.com/rclone/rclone/blob/687d264b689b8c49a67e2e52a8a5e0caa01c04ce/fs/sync/sync_test.go#L1155-L1195).
They exercise the upstream dry-run/live-result distinction through `fstest`
remotes. They were inspected, not run with an inherited remote configuration.
LedgeSync's synthetic executor/journal tests remain the evidence for its distinct
approval, replay, identity and recovery behavior; see the main handoff for runs.

```text
fs/operations/copy.go 57e4a678282a91197c5d66e475cafe0219b329dce7cb9955300867622dd28767
fs/sync/sync.go 0740a05a8dfa29a3d481b1e3a975e4e99b08fd0563575bde2ba329031fb67d94
fs/sync/sync_test.go c25400e5669bb35da472617e70bde587b443d14f8b12ca539b4e58c23b6ac1c9
```

## Current API evidence

Official references checked live on the review date:

- [Upload file data](https://developers.google.com/workspace/drive/api/guides/manage-uploads): resumable creation uses a session URI and PUT ranges, with non-final chunks in multiples of 256 KiB. Interrupted requests require status reconciliation; pre-generated IDs prevent a retry from creating a second ID.
- [Generate IDs](https://developers.google.com/workspace/drive/api/reference/rest/v3/files/generateIds): obtain IDs before creating files/folders and persist them with the operation intent.
- [Resolve errors](https://developers.google.com/workspace/drive/api/guides/handle-errors): inspect structured reasons to distinguish permissions, storage, application quota and temporary rate limits.

## Implemented boundary

`internal/providers/drive` exposes a typed Authorizer port. Credentials never enter
provider DTOs or its storage. The authentication service owns account validation,
token refresh, credential removal and redirect rejection. Calls use fixed Google
HTTPS endpoints, bounded response bodies, cancellable timeouts and safe domain
errors. Provider response bodies and session URLs are never included in errors.
The provider maps the existing authentication service's sentinel errors to domain
intervention codes; revoked permissions, changed accounts, cancellation, busy
authorization and unavailable vaults are not retried as network failures. The
same mapping applies while reading the authentication-owned response body.

- `GetFolder` resolves the root alias and checks availability/type; mutation checks
  `canAddChildren` before proceeding. Shared-drive destinations remain unsupported.
- `ListFolders` returns a cursor explicitly, accepts all-authorized-folders or a
  parent-ID view, retains duplicate display names, and fails incomplete/invalid
  pages. The caller must exhaust cursors and detect cross-page cycles before
  declaring complete discovery. The result covers the current `drive.file`
  namespace; it is not a full-account inventory.
- `GenerateIDs` validates count, uniqueness and safe opaque IDs.
- `CreateFolder` and `Upload` require a preallocated ID and an operation marker
  (`appProperties.ledgesyncOperation`). A matching existing object is reconciled;
  a different object at that ID blocks mutation. No update, overwrite, delete,
  permission modification or conversion API is implemented.
- Binary uploads buffer at most an 8 MiB chunk, including empty-file support.
  Acknowledged ranges are checked for regressions and impossible offsets. A lost
  response triggers a status query before any data replay. Temporary failures
  have a bounded retry budget and cancellable exponential backoff with jitter.
  Read requests have at most four attempts. Creation POSTs are not blindly retried.
- Every success requires a separate metadata read verifying object ID, parent ID,
  name, binary MIME type, operation marker, untrashed status, size and MD5.
  Folder success verifies the corresponding identity and MIME fields.
- Session URIs exist only in backend memory, are never journaled, and accept only
  the exact Google upload endpoint and a single resumable upload identifier.
  After process restart, the executor reconciles the persisted object ID; no
  plaintext session checkpoint is recovered. An expired session returns an
  explicit resumable-operation error after read-only ID reconciliation.

## Executed verification

The first test run could not write Go's temporary `testlog.txt` because the Mac's
internal volume was full. It failed for that environmental reason. Retesting used
task-owned temporary/cache directories on the SSD without deleting user files:

```sh
export GOMODCACHE=/Volumes/SSD/storage/data/go/pkg/mod
export GOCACHE=/Volumes/SSD/storage/drive/Projects/LedgeSync/build/drive-provider-test/cache
export GOTMPDIR=/Volumes/SSD/storage/drive/Projects/LedgeSync/build/drive-provider-test/tmp
export TMPDIR="$GOTMPDIR"
go test ./internal/providers/drive
go test -race -count=1 ./internal/providers/drive
go vet ./internal/providers/drive
```

All three commands passed. Tests use synthetic content and a local HTTP server
through a fake Authorizer, never real credentials or Drive writes. Regressions
cover duplicate names, incomplete listing, cursor failure, scope/storage failures,
bounded GET retry/cancellation, invalid/preallocated IDs, denied parents, foreign
operation identity, lost create acknowledgements, partial chunk acknowledgement,
lost intermediate/final responses, empty files, checksum/size/parent/marker
mismatch, session URL rejection/redaction, expired sessions, source size change
and account mismatch. No live cloud or cross-platform runtime result is claimed
by these provider tests. Full executor, journal and GUI validation is tracked in
the implementation handoff.

A subsequent integration review added wrapped real-authentication-sentinel tests
for both request and response-body failures, checking safe classification,
redaction and no retry. The provider race suite passed again after this change.

## Limits

The caller owns approved plans, durable intents, source identity checks before
and after reading, process locks and safe retry/resume decisions. The production
reader now uses Unix `O_NONBLOCK | O_NOFOLLOW` at file/directory open plus identity
checks, preventing a concurrent final-component FIFO/symlink replacement from
silently entering the regular-file path. Context is checked between reads. An
already blocked filesystem call, including a stalled network/FUSE filesystem,
still cannot be forcibly interrupted; cancellation is not time-bounded for every
possible filesystem provider. A Google
acknowledgement is not rollback: cancellation or later verification failure can
leave the new object remotely present, and its journaled ID must be retained.
No automatic overwrite, trash, native-document conversion, shortcut traversal,
shared-drive synchronization, full-account listing or background scheduling is
enabled by this provider. Live Drive acceptance remains a separate check.
