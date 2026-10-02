# Drive synchronization failure — reproduction, root causes and fixes

**Date:** 2026-10-02. **Scope:** the owner's report that LedgeSync "is not
synchronizing files" after alpha.4. **Result:** four independent defects
reproduced with the production HTTP provider, fixed, and covered by regression
tests. Live Google acceptance is a separate step; see
[live acceptance](DRIVE_UPLOAD_ACCEPTANCE.md).

## Evidence boundary

No LedgeSync application was installed in the inspected Mac's application
folders, no crash reports or unified-log entries existed, and the only recorded
desktop folder selection was an earlier automated smoke fixture. The owner's
exact failing machine was therefore not available. The failure was reproduced
instead with the real `internal/providers/drive` HTTP client against a new
Google Drive v3 emulator that models the `drive.file` visibility rules
([`drivetest`](../../internal/providers/drive/drivetest/server.go)). Requests
pass through the same request boundary as the production authorizer
(`driveauth.AllowedDriveRequest`). The emulator never contacts Google.

## Reproduced failures (alpha.4 code)

| Journey | Alpha.4 result | Root cause |
|---|---|---|
| **Use My Drive** | Destination selection failed with `DRIVE_NOT_FOUND` | With `drive.file`, `files.get("root")` returns 404. The pinned rclone v1.75.1 source handles the same 404 explicitly (`backend/drive/drive.go`, root ID lookup). LedgeSync required readable root metadata for selection, for every destination check and for each parent check before creation. |
| **Any developer folder** with a symbolic link anywhere, e.g. `node_modules/.bin/*` or `.venv/bin/python`, even when ignored | Preview failed with `NODE_UNSUPPORTED` | The scanner walked every directory, including ignored ones, and treated any link or special node as a fatal error. |
| **A folder in use** (a log, cache or editor file changing — even an ignored one) | The run ended `needs_review` before or after copying | Approval fingerprints included excluded entries and directory timestamps; every node revalidated the full directory structure of the whole tree, so unrelated churn invalidated the run. |
| **Earlier copy removed or moved in Drive** | Every later preview failed permanently | A verified mapping whose object was missing, trashed, renamed or moved raised an error with no recovery path. |

Additional defects found while fixing: a status query that reported progress
after transient upload failures was counted as another failure (uploads gave up
after three transient errors); a file changed while streaming aborted the whole
run as `SOURCE_UNREADABLE`; Drive requests failed immediately with `AUTH_BUSY`
if a short credential operation (such as a status read) held the lock; and the
first restore implementation created a rejected target inside the source.
Each has a regression test.

Interface diagnostics hid all of this: destination, preview and start failures
were replaced by generic messages, and transfer status had no error code.

## Fixes

- **Policy-aware traversal** ([discovery](../../internal/discovery/discovery.go),
  [app service](../../internal/app/service.go)): directories are listed
  completely before any child is considered; Gitignore rules from ancestors
  decide, under conservative composition, whether a child directory is excluded
  entirely. Such directories are recorded but not read. Links and special nodes
  are recorded, never followed, read or copied, and are reported as
  `Not copied`. A disagreement between the traversal and the complete policy
  fails closed (`SCAN_INCOMPLETE`).
- **Approval binding** ([transfer service](../../internal/transfer/service.go)):
  the fingerprint covers selected entries' paths, kinds, sizes and SHA-256,
  rules and configuration. Excluded churn no longer invalidates a run. Before
  each mutation the executor checks the source root, known rule files and new
  rule files in traversed folders; rule or configuration changes stop the run.
  An approved file whose content changed is skipped and reported per file; the
  run ends `partial`. Unapproved content is never uploaded.
- **My Drive alias** ([client](../../internal/providers/drive/client.go)):
  creation uses the `root` alias; placement is confirmed with a parent query
  that `drive.file` permits, and the canonical root ID is recorded.
- **Replacement generations**: a recorded object that is missing, trashed,
  renamed or moved is never modified or adopted. The item (and, for folders, its
  subtree) is copied again under a new operation identity, shown as `Copy again`
  in the review.
- **Fewer requests**: recorded objects are verified with one listing per folder,
  IDs are reserved in batches, writable-parent checks are cached briefly, and
  the per-file existence read is skipped for freshly reserved IDs.
- **Typed, redacted errors** for every desktop action and transfer status
  ([public errors](../../internal/connections/errors.go)); the interface shows
  the safe message with guidance for the code.

## Regression tests

End-to-end with the real provider and the emulator
([e2e tests](../../internal/transfer/e2e_test.go)):

`TestE2EPickerFolderCopiesCompleteHierarchy`,
`TestE2EMyDriveWorksWithoutReadableRoot`,
`TestE2ESymlinksInsideIgnoredDirectoriesDoNotBlockCopy`,
`TestE2EExcludedFileChurnDuringUploadStillSucceeds`,
`TestE2ERepeatCopiesOnlyNewAndChangedFiles`,
`TestE2EManagedFolderTrashedInDriveIsCopiedAgainWithoutTouchingIt`,
`TestE2EFileDeletedOrRenamedInDriveIsRecreatedIndividually`,
`TestE2ECancelledMultiChunkUploadResumesWithoutDuplicates`,
`TestE2ETransientFailuresRetryWithinBoundsAndLostAcknowledgementsReconcile`,
`TestE2EExpiredSessionNeedsReviewThenResumesSameIdentity`,
`TestE2ERuleChangeDuringRunStopsBeforeFurtherMutation`,
`TestE2EDestinationTrashedAfterPreviewBlocksMutation`.

Before the fixes, the My Drive, symlink and excluded-churn tests failed exactly
as listed in the table above; after the fixes all twelve pass. The desktop
bridge journeys ([journey tests](../../internal/transport/desktop/journey_test.go))
exercise the same calls the interface makes, including saved-pair reopening,
repeat copies, automatic copies and typed errors. Restore is verified by
downloading the copy and comparing SHA-256 values
([restore test](../../internal/restore/restore_test.go)).

## Commands and results (2026-10-02, macOS 27 arm64, Go 1.27.1)

Run from the repository root with isolated caches under `build/claude/`:

```sh
go test ./cmd/... ./internal/...            # all packages pass
go test -race ./cmd/... ./internal/...      # all packages pass
go vet ./cmd/... ./internal/...             # clean
go vet -tags bindings ./cmd/ledgesync-desktop
(cd frontend && npm run build && npx playwright test)   # 53 passed
```

A native macOS arm64 desktop build and CLI were built with the publisher's
Desktop OAuth client (generated source removed afterwards). The desktop app
started, created its private state (`catalog.sqlite`, mode 0600) and exited
cleanly; it also ran with the hardened runtime and no entitlement exceptions.
These are build and startup checks, not live Google acceptance.
