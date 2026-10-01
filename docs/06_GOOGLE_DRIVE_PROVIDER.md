# 06 — Native Google Drive Provider

The application uses the official Drive API through a Go adapter, not the Drive desktop database and not an external rclone command. Provider behavior is subordinate to the safety specification. Sources D01–D08 are cataloged in the source register.

## Authorization and namespace strategy

Start with an app-managed destination created/selected through an explicitly authorized workflow. Prefer `drive.file` when it supports the actual workflow; it limits the app to files made available to it. Do not assume that choosing a parent folder grants access to every pre-existing descendant. Test that exact workflow before enabling an existing-tree feature. Broad Drive access is a separate, explicit mode and may introduce restricted-scope verification requirements. Never silently escalate scopes after a 403. [D01]

Onboarding must explain account identity, scope, selected root, app-managed ownership, and whether existing files are visible/editable. Show the authenticated account and immutable selected folder ID in the final confirmation, not only a display name. Multiple accounts have separate credential references and inventories. Disconnecting stops jobs and clears local credentials; revocation and deletion of cloud files are separate explicit actions.

Use the system browser for OAuth authorization with authorization-code flow, PKCE, unpredictable state, and the supported desktop redirect approach. Validate callback state, bind a loopback listener narrowly, impose a timeout, and close it after completion. Never collect the user's Google password or embed the login in the desktop webview. Keep tokens in an OS credential vault and expose only opaque account references to the UI. A desktop OAuth client secret is not a secure confidential-server secret. [D02]

## Capability declaration

At connection/preflight, report capabilities instead of guessing uniform cloud semantics: paginated listing, regular-binary upload, resumable upload, content hashes, read-back, server-side copy, trash, native cloud documents, shortcuts, shared-drive support, conditional mutation support, and change-feed availability. Mark native-doc upload/conversion, shared-drive broad synchronization, shortcut traversal, and atomic conditional overwrite unsupported until their dedicated gates pass.

The initial adapter handles ordinary local files as binary Drive files. It does not automatically convert Markdown or Office documents into native Google documents. It does not claim to preserve arbitrary POSIX permissions, ownership, hard links, executable flags, resource forks, or extended attributes. Such preservation needs a versioned metadata/archive design.

## Discovery and identity

Use object IDs and parent IDs as identity. Names are display/path components, not unique keys. Page every listing completely and constrain traversal to the selected namespace. Retrieve only fields required for matching, type checks, ownership, capabilities, version observation, sizes, timestamps, and available checksums. A listing error sets inventory completeness to false. [D06, D07]

Build a parent-ID index and detect duplicate sibling names, cycles/unsupported shortcuts, inaccessible nodes, and local case/Unicode collisions. Do not rename colliding files automatically during preview. Store raw provider names separately from normalized logical paths. Reject paths the local restore target cannot represent safely.

Do not traverse Drive shortcuts initially. Native cloud document entries are protected/unsupported unless a later explicit export profile defines representation, naming, MIME conversion, integrity, and conflict semantics. A known object returning 404 may be deleted, moved, or no longer visible; reconcile authorization and parent state before classifying it.

## Operation mapping

| Domain operation | Drive behavior | Safety condition |
|---|---|---|
| Inspect identity | File metadata read by ID | Authorized account and root membership |
| Inventory | Paginated child listing | Exhaust every page; no inferred completeness |
| Create directory | Create folder resource | Only during approved execution, with journaled identity |
| Upload new binary | Create with upload; resumable for chosen threshold | Stable parent and source fingerprint |
| Update managed binary | Update existing object, later gate | Recovery and single-writer/precheck policy |
| Recovery copy | Server-side copy or verified alternative | Capability tested; distinct recovery namespace |
| Trash managed object | Set trash state by known ID | Complete mirror gate; no recursive folder shortcut |
| Verify | Comparable content hash or read-back | Exact uploaded bytes; no metadata-only success |
| Restore | Read media to safe local target | New target directory and path validation |

No API method is treated as a universal atomic compare-and-swap. Recording a remote version is useful for detecting changes but does not by itself prevent a race between precheck and write. P2/P4 must document and test the exact supported preconditions, or keep update/trash capabilities gated. [D06]

## Resumable upload contract

Use the official resumable workflow: initiate the upload, retain the returned session URI securely, send byte ranges, and reconcile the server-reported committed range after interruption. Choose a bounded chunk size compatible with the documented API requirements; pin the precise chunk constraints in adapter tests. Session lifetime and expiry are provider-controlled, not permanent resume guarantees. Restart an expired session through the same idempotency/reconciliation flow. [D03]

The durable checkpoint includes operation ID, source fingerprint/digest progress, expected total size, confirmed remote offset, target object/parent IDs, session-secret reference, retry budget, and timestamp. Never log or put the session URI in frontend events or exported JSON. On resume, revalidate the source identity before sending remaining bytes. If source contents changed, abandon the old source snapshot rather than appending unrelated bytes.

An ambiguous timeout after create/upload is not permission to create another object. Query known object IDs and application metadata markers within the authorized root. If the adapter cannot determine whether an operation completed, mark `UNKNOWN_REMOTE_RESULT` and stop that operation for review.

## Error classification and quotas

Map transport/provider errors into stable domain codes with a safe human message and a redacted provider correlation field. Inspect structured reasons; the same HTTP status may represent different actions. A bounded credential refresh is appropriate for an expired access token, not repeated interactive sign-in loops. Permission/scope/storage failures require user action; rate limits and selected server/transport failures permit bounded backoff. [D04]

Keep request concurrency, transfer concurrency, retry budget, and bandwidth control separately configurable. Avoid hard-coding account quota promises or assuming one universal request allowance. Display provider quota/storage failure reasons and honor documented guidance. Record actual metrics locally; no upload telemetry by default. [D08]

## Incremental change feed

P2 can use full scoped reconciliation for correctness. A later optimization may persist Drive change tokens, process all pages, and commit a new cursor only after corresponding state is durable. Token invalidation, loss of access, or uncertain scope requires a full rescan. Change events are hints for reconciliation, not authority to delete local files. [D05]

## Integration authorization gate

Use mocks/fake provider until an authorized test account and disposable folder are supplied. Do not reuse a user's existing Drive tokens or unrelated project folder automatically. A live test fixture must verify app-created namespaces, insufficient scope, revoked tokens, permissions changed mid-run, duplicate names, upload interruption/resume, lost responses, and restore. Cleanup is restricted to exact test-created IDs; never purge a parent tree broadly.

A successful OAuth handshake is not proof of production readiness, full-folder visibility, complete synchronization, or completed Google app verification.
