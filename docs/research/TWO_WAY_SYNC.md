# Two-way sync (alpha.7)

**Decision:** [ADR-034](../12_ADR_DECISIONS.md). **Code:**
[engine](../../internal/syncer/), [state](../../internal/transferstate/syncstate.go),
[Drive operations](../../internal/providers/drive/sync_ops.go),
[desktop bridge](../../internal/transport/desktop/sync.go),
[CLI](../../cmd/ledgesync/sync.go).

The owner asked for Google Drive-like behavior: once a folder is chosen, it
starts sending and receiving. A synced folder is a local folder and a Drive
folder of the same name inside a chosen location (My Drive by default). If
that location already holds exactly one folder with the name, that folder is
reused, so the same folder added on another computer joins it.

## How a pass decides

Each pass lists the local folder (with the same ignore-rule engine as
previews), lists the Drive folder, and compares both with the **base**: the
last state both sides agreed on, stored per path in the private `sync.sqlite`.

| Situation | Action |
|---|---|
| New on one side | Copy to the other side (folders are created first) |
| New on both, same content | Record as synced |
| New on both, different content | Conflict: keep both |
| Edited on one side | Upload as a new Drive revision, or download and replace |
| Edited on both, different content | Conflict: the Drive version keeps the name; the local version becomes `name (conflict <time>).ext` and is uploaded |
| Deleted on one side, unchanged on the other | Move the other side's item to the Drive trash or to `.ledgesync-trash` |
| Deleted on one side, edited on the other | Keep the edit; copy it back |
| Folder deleted on one side, new content inside it on the other | Keep the folder and the new content; unchanged items are deleted |
| Deleted on both | Forget |

A local file counts as changed when its size or modification time differs from
the base and its MD5 differs. A Drive file counts as changed when its ID or
version differs and its MD5 differs.

## What is not synced

The following are reported in the folder's "items not synced" list and left
untouched:
- symbolic links and special files;
- Google Docs, Sheets, Slides and shortcuts;
- several Drive items with the same name in one folder, including names that
  differ only in letter case on macOS and Windows;
- names the local system cannot store;
- items matched by the ignore rules.

These are never synced: `.ledgesync-trash`, LedgeSync temporary files,
`.DS_Store`, `._*`, `Thumbs.db`, `desktop.ini`, `Icon\r`, `~$*` and `.~lock.*#`.

## Safety

- **Revisions and trash.** Drive edits are stored as revisions of the same
  file. Drive deletions use the trash (30 days). Local deletions move items to
  `.ledgesync-trash/<time>/<path>` inside the synced folder, kept for 30 days.
- **Mass-deletion guard.** A pass waits when it would delete at least 20 files
  and more than 30% of the synced files on one side, or when it would empty
  one side. **Delete on the other side too** confirms exactly that deletion
  set (identified by a digest). **Restore the files instead** forgets those
  items, so the next pass copies them back.
- **Unavailable sides never delete.** A missing local folder pauses as
  *waiting*. A trashed or missing Drive folder pauses the pair.
- **Creations are idempotent.** Each creation journals a reserved Drive ID
  first. A lost response is reconciled by reading that ID, never by creating a
  duplicate.
- **Local writes are checked.** Every local operation runs through `os.Root`
  inside the synced folder. Downloads go to a temporary file whose size and MD5
  are verified, and replace the target only if it still matches the scan. A
  local file that changes during a download or deletion is compared again in
  the next pass.
- **Concurrent changes are rechecked.** Before an upload replaces or trashes a
  Drive file, its current version is checked again.

## When it runs

The background manager:
- checks each folder's file names, sizes and times every 10 s;
- reads the Drive change feed every 30 s, ignoring changes outside synced
  folders;
- lists the whole Drive folder when relevant changes arrive, and at least every
  10 minutes.

Sync runs while the desktop app is open (closing the window offers **Minimize**
to keep syncing) or while `ledgesync sync watch` runs. Nothing is installed or
enabled at login.

## Known limitations

- A rename or move is synced as a deletion plus a new copy (the old item goes
  to the trash).
- Shared drives are not supported.
- Background start at login is not available yet.
- Names in different Unicode normalization forms are not merged. The target
  check prevents overwrites, and the item is reported.

## Tests (2026-10-02, macOS 27 arm64)

- `internal/syncer`: 19 tests, passing with `-race`:
  - every three-way decision;
  - folder containment;
  - the deletion guard;
  - conflict names;
  - built-in exclusions;
  - end-to-end through the real Drive client and the emulator: first-pass
    merge with existing Drive content, local edits and deletions, Drive edits,
    deletions and **files added on the website**, conflicts, edit-versus-delete,
    the guard with confirm and restore, ignore rules, unsyncable items, a
    missing local folder, a trashed Drive folder, a lost upload acknowledgement,
    a lost result across passes, and the background loop syncing both ways
    without commands.
- `cmd/ledgesync`: `TestCLISyncAddRunGuardAndControls` covers add, run, list,
  the guard (exit 5), a refused confirmation with the wrong count, the
  confirmed deletion, pause, resume, activity and remove.
- `internal/driveauth`: the extended request boundary allows only the new
  update, trash and change-feed endpoints. Callback and token responses still
  reject any scope other than the requested one.
- Playwright `tests/sync.spec.ts`: 5 tests (58 in total).

## Live acceptance (2026-10-02/03, owner's account, full Drive access)

[live_sync_acceptance.py](../../tools/live_sync_acceptance.py) uses two local
folders with the same name, standing in for two computers on one new Drive
folder. It passed all 10 steps:
1. Connected with full Drive access.
2. The first sync uploaded 6 files and 4 folders.
3. The second computer joined and downloaded everything.
4. An edit on A reached B.
5. A new file on B reached A.
6. A deletion on A moved B's copy to its local trash.
7. Edits on both sides kept both versions everywhere.
8. Deleting 25 files at once waited for confirmation.
9. **Restore the files instead** brought them back.
10. `sync watch` sent and received in 6 seconds without commands.

Re-adding a folder to an existing identical Drive folder made no changes.
Pending: a file uploaded through the Drive website, which awaits the owner's
upload.
