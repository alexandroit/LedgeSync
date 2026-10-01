# 05 — Sync Safety, State, and Recovery

**Normative owner of mutation safety.** A filter decision answers whether an entry is selected. It does not authorize a deletion, overwrite, permission change, or local-source modification.

## Modes and supported ownership

`copy` adds selected source files and skips verified unchanged files. It never deletes destination objects because the source lacks them or now ignores them. The default conflict policy is `keep-both`, and the default overwrite policy is `deny`. A name collision is not proof of identity.

`mirror` is a later, separately gated mode. It reconciles only objects explicitly managed by this project, not arbitrary objects in a destination folder. The first implementation must not delete unmanaged files, adopt them silently, or promise that a directory becomes an exact clone when exclusions/protected files exist. Describe it as a **managed mirror**.

The initial supported deployment is a dedicated, single-writer destination namespace. Another computer, Drive desktop, another project, or a human editor is a possible concurrent writer, not protected by a local mutex. Observe remote identity/version immediately before mutation, but do not advertise atomic compare-and-swap unless the exact provider method supports and passes that guarantee. Concurrent editing after a check remains a residual risk. [D06]

## Required invariants

| ID | Invariant |
|---|---|
| S01 | Upload workflows never write to, move, or delete the local source. |
| S02 | Preview performs no remote mutation, including folder creation. |
| S03 | An absent, unmounted, inaccessible, or partially scanned source is not an empty source. |
| S04 | An unreadable or invalid active rule source blocks the job; do not fall back to include-all. |
| S05 | Excluded and unsupported remote objects are protected, not treated as missing source objects. |
| S06 | Only the approved plan's operations can execute. |
| S07 | Account, root identity, configuration, rules, and operation preconditions must still match. |
| S08 | Destructive operations require separate capability, recovery, ownership, and approval checks. |
| S09 | Completion means verified required operations, not bytes queued or HTTP request started. |
| S10 | Ambiguous remote results are reconciled by identity before retrying creation. |
| S11 | An incomplete listing, permission error, missing cursor page, or cancellation forbids deletion. |
| S12 | A source or destination conflict never silently chooses the last writer. |
| S13 | Job state is journaled durably around every externally visible mutation. |
| S14 | Cancellation cannot claim to undo a mutation already acknowledged by the provider. |
| S15 | Recovery material and its verified mapping are retained before an allowed overwrite/trash. |
| S16 | A filter/configuration change invalidates approval and cannot trigger automated cleanup. |

## Run state machine

`CREATED -> VALIDATING -> DISCOVERING -> PLANNED -> AWAITING_APPROVAL -> APPLYING -> VERIFYING -> SUCCEEDED`.

Terminal or intervention states are `FAILED`, `CANCELLED`, `PARTIAL`, `BLOCKED`, and `NEEDS_REVIEW`. `PAUSED` is allowed only with a durable resume checkpoint. A resumed run must revalidate its snapshot and capabilities. It may return to `NEEDS_REVIEW`; it does not bypass authorization because approval existed before a restart.

Every operation has `PENDING -> PRECHECKED -> IN_PROGRESS -> REMOTE_ACKNOWLEDGED -> VERIFIED`. Alternative states are `SKIPPED_VERIFIED`, `FAILED`, `UNKNOWN_REMOTE_RESULT`, and `BLOCKED`. Journal a stable operation ID before the request. Record remote object ID, transferred digest, preconditions, timestamps, and recovery mapping after acknowledgement. The last-successful baseline advances only after all required work for the corresponding entry is verified; keep the previous full-run baseline when a run is partial.

## Planning algorithm

1. Validate strict configuration, all explicit rule sources, credentials reference, account identity, provider capabilities, source real path/volume identity, and destination root identity.
2. Acquire the project/root-pair process lock. Check known overlapping projects and warn/block conflicting writer registrations. Do not acquire a lock inside the uploaded directory.
3. Build a local inventory and rule snapshot. Track scan errors independently from an entry's exclusion status. Read regular files without following links and retain stable identity fingerprints.
4. List the permitted remote namespace to exhaustion. Record IDs/parents, observed versions, types, relevant hashes, and scan completeness. Detect duplicate names and collisions.
5. Apply selection. Keep both selected and protected/excluded ownership records; dropping exclusions from state would make later deletion unsafe.
6. Compare by known object identity and verified fingerprints. Produce explicit create-directory, upload-new, skip, conflict, and, in later gated modes, backup/update/trash operations with dependencies.
7. Compute risks, counts, byte estimates, destination scope, and exact digests. Display all destructive operations individually. A dry run is a plan, not a simulated success report.
8. Approve the exact plan digest. Before each operation, revalidate identities and preconditions. A material change requires a new plan rather than adding work to an approved plan.

A plan expiry of 15 minutes is a proposed initial UI default, not a substitute for per-operation checks. Configure it explicitly. A long approved transfer can continue while its own preconditions remain satisfied; expiration prevents starting an unstarted stale plan and is not a wall-clock termination mid-upload.

## Object matching and collisions

Persist `(projectId, relativePath, providerObjectId, parentId, lastVerifiedDigest, observedVersion, ownershipState)` as identity evidence. Matching by filename alone is insufficient. A remote duplicate-name set is `AMBIGUOUS_DESTINATION`; require an explicit resolution. Do not use a dedupe/purge command as an automatic remedy.

For new uploads, persist an idempotency marker in supported application metadata and the journal. After a timeout, search/reconcile within the permitted root by that marker and known IDs before retrying. Markers reduce duplicate creation; they are not a distributed transaction or server-enforced uniqueness guarantee. If results remain ambiguous, stop.

`keep-both` creates a unique sibling name including a short stable operation suffix and records the mapping, never overwrites an unknown object. Repeated retries of the same operation must locate the same sibling rather than producing many copies. A future intentional update to a managed object requires `recover-managed`, a verified recovery copy, no detected concurrent change, and explicit single-writer acknowledgement.

## Managed-mirror deletion gate

A candidate can be trashed only when all of these predicates hold:

- The object ID was created/adopted explicitly by this project, has a verified baseline, remains within the exact destination root, and is not a recovery/control object.
- The source and destination inventories are complete; the source volume/root identity is unchanged; there were no traversal, permission, cancellation, or decoding errors.
- The path is genuinely absent under the unchanged policy, not excluded, newly unreachable, unsupported, filtered by age/size, or hidden by a missing rule file.
- Config/rule digests match the last approved deletion baseline. A changed policy requires a fresh non-destructive reconciliation and separate review; do not turn ignore edits into deletion suggestions automatically.
- The object has not changed unexpectedly since the verified baseline/precheck, and a recovery copy or other proven retained recovery mechanism exists.
- Count and percentage caps both permit the operation, and a human approved the exact destructive set. Defaults of zero prohibit all deletions. The denominator is the previously managed, in-scope, eligible file set, never all account files.

Trash individual file IDs. Do not trash a folder containing excluded, unmanaged, unknown, concurrently created, or protected descendants. Initially leave empty directory cleanup disabled; a future implementation must prove descendant safety again before cleanup. Permanent deletion, empty-trash, mass deduplication, and purge are outside the initial product.

## Source changes and upload integrity

Capture a non-following file identity, length, modification time, and streamed digest. Recheck identity before and after reading. If the file changes, discard the operation's success claim even when the provider accepted bytes; record what happened and reconcile. Never delete a prior good destination version to hide a failed replacement.

The default verification requirement is a provider-supported content hash of uploaded binary data compared to the locally streamed digest. When no reliable comparable hash exists, download-and-hash verification is required for the same assurance, or block that assurance level. Metadata-only checks must be visibly labeled weaker, never reported as content verification.

Ordinary reads do not produce a point-in-time snapshot of a changing database or VM image. Detect active writes and recommend an application-consistent export; filesystem/application snapshot integration is future work.

## Crash, cancellation, and retry

Cancel cooperatively, stop admitting new work, persist checkpoints, and query ambiguous requests before cleanup. On startup recover `IN_PROGRESS` and `UNKNOWN_REMOTE_RESULT` entries through read-only reconciliation. Do not infer that a missing local acknowledgement means the upload did not happen.

Retry transient transport/quota failures with bounded exponential backoff and jitter. Invalid rules, permission denial, unsupported data types, identity mismatches, and conflicts are not blindly retryable. Retrying a destructive operation requires unchanged approval/preconditions and known remote state. Scheduler restart never grants those conditions.

## Recovery and restore

Recovery objects live in a separate app-managed recovery namespace and are not eligible for ordinary synchronization/deletion. Retention is explicit, bounded, visible, and never silently shortened. Metadata includes original path/object ID, run/operation ID, digest, time, retention deadline, and relationship to the replacement.

Default restore downloads to a newly chosen, empty local directory outside the active source and checks all paths again. Never overwrite source files automatically or follow shortcuts/symlinks during restore. Verify restored contents and preserve a report. Synchronization plus limited recovery is not an immutable backup or full historical snapshot system.

## Storage model

Required tables/entities: `projects`, `config_revisions`, `account_refs`, `runs`, `rule_snapshots`, `inventories`, `inventory_entries`, `plans`, `operations`, `object_mappings`, `recovery_entries`, and `run_events`. Use unique project/operation identities, foreign keys, explicit transaction boundaries, and migrations. Store large traces separately with retention. Encrypt secret-bearing resumable session records; do not put plaintext credentials in SQLite.

Tests in `tests/safety-scenarios.json` are release gates. The package does not yet contain the executor that would satisfy them.
