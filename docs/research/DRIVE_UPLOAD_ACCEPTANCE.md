# Manual Google Drive folder-copy acceptance

## Alpha.6 — automated and live acceptance status (2026-10-02)

**Live Google acceptance: passed, after two fixes.** The owner connected their
Google account with the restricted `drive.file` scope on the build Mac
(macOS 27 arm64). [live_acceptance.py](../../tools/live_acceptance.py) uses only
a synthetic fixture and a new `LedgeSync acceptance <timestamp>` pair in My
Drive, and writes a redacted JSON report.

1. The **official alpha.5 CLI** created the managed folder and then failed the
   first file upload with `UNKNOWN_REMOTE_RESULT`; no file was uploaded. The
   cause and both fixes are in the
   [failure analysis](DRIVE_SYNC_FAILURE_ANALYSIS.md#live-acceptance-findings-alpha6).
2. **LedgeSync 0.1.0-alpha.6** built from `c1e00b1` (the `main` commit with both
   fixes, publisher Desktop client) passed **all 14 steps** in about two
   minutes. An earlier build of the same fixes passed the same steps. The test
   builds were signed with the developer's Apple Development certificate only
   so that the owner's Keychain approval held across rebuilds; this is not a
   distribution signature.

| Step | Result |
|---|---|
| Connected account | `drive.file` only |
| Pair saved for My Drive | passed |
| First copy verified by Drive | 7 files, 20,975,937 bytes. Covers a 20 MiB multi-chunk file, a zero-byte file, Unicode and space names, and nested and empty folders. 3 ignored items were not uploaded |
| Independent download matches source SHA-256 | 7 files, 6 folders |
| Unchanged repeat | only `skip`; no bytes sent |
| Changed file | `keep-both`; succeeded |
| `kill -9` during a 64 MiB multi-chunk upload | killed after more than 9 MiB sent |
| Restart | `resume` of the reserved identity; succeeded |
| Network cut mid-transfer by a local proxy | stopped as `failed` with `DRIVE_NETWORK`; no success claimed |
| Copy after the network returns | succeeded |
| Final tree after interruptions and changes | matches; `docs/guide.txt` keeps its first content and the change is one `.ledgesync-` version |
| Automatic copies authorized from a reviewed preview | passed |
| Authorized automatic run | new file copied; succeeded |
| Ignore-rule change | automatic copies paused with `AUTOMATION_REVIEW_REQUIRED` |

The report `build/live-acceptance/acceptance-20261002-172209.json` stays local
and is not committed. Earlier attempts left more acceptance folders in the
owner's My Drive. One holds only the empty managed folder from the failed
alpha.5 run. Another completed when retried with the fixed CLI; its uncertain
upload was reconciled without duplicates. LedgeSync never deletes Drive files,
so the owner removes the `LedgeSync acceptance …` folders manually.

**Still pending, owner:** one manual desktop pass with the native Picker:
1. Choose an existing folder.
2. Preview and upload.
3. Trash the managed folder in Drive.
4. Confirm that the next preview offers **Copy again**.

**Emulator acceptance: passed.** The real HTTP provider, executor, journal and
desktop bridge were exercised against the `drive.file` emulator. It now also
models Google's `session_crd` session parameter and Drive-detected media types.
The cases covered:
- Destinations: My Drive and Picker.
- Source content: nested and empty folders; ignored files; zero-byte, Unicode
  and space-containing names; multi-chunk files; links in ignored folders;
  churn in ignored files.
- Repeat copies: unchanged repeats (no new uploads); changed files (keep-both);
  added files.
- Interruption and failure: cancellation mid-file and resume of the same
  identity; lost acknowledgements; 5xx; 429 with `Retry-After`; dropped
  connections; expired upload sessions.
- Changes during or after a run: rule changes during a run; destination
  trashed after preview; managed folder trashed, or a file deleted or renamed in
  Drive (copied again without touching the existing item).
- Automatic copies that run, then pause when rules change.
- Restore, with an independent SHA-256 comparison.

See the [failure analysis](DRIVE_SYNC_FAILURE_ANALYSIS.md) for the test names
and commands.

## Status and evidence boundary

Release **0.1.0-alpha.4** implements manual Google Drive folder copies. Its exact
application commit, native CI identities, artifact ZIP digests and package
hashes are recorded in [release evidence](DRIVE_COPY_RELEASE.json) and the
[platform guide](../PLATFORMS.md). Published bytes and installation checks are
separate from the live Google acceptance described here. Earlier alpha.3
artifacts remain immutable and do not upload files.

The owner reports that OAuth is in Production, Google Picker API is enabled and
the existing application connected successfully. Those are owner reports, not
independent Cloud inspection. Acceptance of the new native Picker, folder upload
and recovery flow is **pending**. This checklist does not execute those actions
or authorize tests against unrelated user files/accounts.

Implementation and automated results belong in [the current handoff](../17_AGENT_HANDOFF_AND_STATUS.md).
The OAuth/Picker protocol and bridge checks are recorded in
[the native Picker review](NATIVE_PICKER_REVIEW.md). Read
[the connection guide](../GOOGLE_DRIVE_AUTH.md) and
[platform installation boundaries](../PLATFORMS.md) before native testing.

## Implemented behavior

The desktop uses **Connect Google Drive → choose destination → Preview folder
upload → Upload folder**. The same shared services handle local policy preview,
account authorization, native provider calls and approved execution. The native
CLI shares those services through `auth connect`, `auth status`, `auth disconnect`
and interactive `copy`, with an exact in-process preview confirmation. The
offline `plan` output remains a separate inspection surface and cannot be applied.
See [CLI usage and headless prerequisites](../CLI.md).

The local root becomes one app-managed child folder within My Drive or a chosen
existing My Drive parent. It is not flattened into the parent. Included regular
files and empty directories retain their hierarchy; ignore rules remain active.
The `drive.file` scope is unchanged. Selecting a parent does not authorize every
pre-existing descendant or make the app a complete Drive browser. Shared drives,
shortcuts, cloud-native document conversion, restore/download, overwrite,
deletion, mirroring and bidirectional synchronization are not enabled.

File content copies are not a metadata archive: POSIX permissions, ownership,
extended attributes and macOS resource forks are not preserved by this workflow.

The executor records intended object IDs before creation, verifies identity,
parentage, size and content checksums, and revalidates the approved source and
rules before relevant operations. Later previews reuse verified unchanged
copies. A changed local file gets a separate name with a stable `.ledgesync-`
suffix; both versions remain. Unexpected remote changes stop for review instead
of granting permission to overwrite or recreate a previously verified object.

Transfer state resides in a private per-user SQLite journal outside upload
roots. It stores identifiers, plans, checksums and observed rule sources, not
OAuth tokens, upload-session URLs or uploaded file payloads. Its process lock protects
the transfer journal. A separate protected kernel lock serializes OAuth vault
reads, refresh, browser authorization, authorized request bodies and credential
cleanup across GUI/CLI processes. Contention requires retry and never assumes
that a cached account is still current.
Do not export the database casually: paths, filenames and account references
can still be private.

An observed rule source that disappears blocks subsequent uploads, including
after restart and when selecting another destination. Restore that source or
explicitly change the project configuration and approve a new preview; never
delete the journal to bypass this protection. Cancellation preserves completed
Drive files. Continuing requires a fresh preview that reconciles recorded IDs.
Session URLs remain in memory, so an unfinished file may restart after process
exit. There is no automatic watcher, schedule or startup resumption.

## Maintainer preflight

Use an explicitly authorized test account and a disposable local source plus a
disposable Drive parent. Never point recovery or cancellation tests at personal
projects, existing production trees or another application's files.

Record the exact candidate commit, binary/package SHA-256, version, OS and
architecture. Verify that the candidate was built with the existing publisher
Desktop client and retained notices. Do not record client JSON, tokens, callback
URLs, browser storage, vault values or upload-session URLs. Keep account emails,
personal filenames and screenshots out of public reports unless deliberately
redacted and approved.

Use one application instance and an available native vault: macOS Keychain,
Windows Credential Manager, or Ubuntu's unlocked Secret Service collection in
the graphical session. Do not use a plaintext fallback or modify the shared
Google project as part of this checklist. If Google blocks Picker or permission,
stop and report the safe error; do not broaden scopes or create another client.

Create a fixture resembling:

```text
LedgeSync acceptance/
├── .gitignore           # contains: ignored.log
├── notes.txt
├── empty.bin            # zero bytes
├── ignored.log          # must not be uploaded
├── empty directory/
├── nested/
│   └── example with spaces.txt
└── large.bin            # non-sensitive generated data larger than 16 MiB
```

Record local file sizes and SHA-256 values before starting. The local fixture
must remain byte-for-byte unchanged by LedgeSync. The rule file itself may be
included unless another explicit rule excludes it. Maintain a separate unrelated
marker file in the disposable Drive parent to prove it is left untouched.

## Live workflow checklist

Every row starts **pending**. Record observed results, sanitized evidence and the
exact tested artifact; do not substitute synthetic results for live evidence.

| Check | Action and expected result | Status |
|---|---|---|
| Existing connection | Open the configured candidate; identify the intended account. Check the connection and reopen the app. No client JSON or token entry is requested. | CLI passed live (`c1e00b1`, 2026-10-02); desktop pending |
| Native folder selection | Choose **Choose existing Drive folder**, select the disposable writable parent in Google's browser Picker and return. The application shows the selected parent and the same account. | Pending |
| Selection cancellation | Start selection and cancel. The previous destination remains; no upload starts and a new preview is required. | Pending |
| Account binding | If testing an explicitly authorized second disposable account, select it in the browser. The first saved account and destination must not be silently replaced. | Pending |
| Preview only | Select the fixture and choose **Preview folder upload**. Inspect account, destination ID, root child, hierarchy, empty directory, sizes and exclusions. No remote object is created before approval. | Pending |
| First copy | Choose **Upload folder**. Wait for verified completion, then use **Open destination folder on Google Drive**. The managed fixture root appears inside the parent, with all included entries and no excluded file. | CLI passed live (`c1e00b1`, 2026-10-02); desktop pending |
| Independent integrity | Inspect the root and empty directory in Drive. Download only the newly created fixture files and compare sizes/SHA-256 with the recorded source values, including empty and multi-chunk files. The unrelated parent marker remains unchanged. | CLI passed live (`c1e00b1`, 2026-10-02); desktop pending |
| Unchanged repeat | Preview and approve again with unchanged inputs. Entries show existing-copy verification, the same provider IDs remain, and no duplicate hierarchy/files appear. | CLI passed live (`c1e00b1`, 2026-10-02); desktop pending |
| Local change / keep both | Edit one fixture file yourself, preview and approve. The original remote version remains and the changed version has a stable `.ledgesync-` suffix. A repeated unchanged run does not create another suffix copy. | CLI passed live (`c1e00b1`, 2026-10-02); desktop pending |
| Cancel and continue | Start a sufficiently large fixture upload and choose **Cancel upload**. Completion is not reported. Already created objects remain. Preview again and continue using reconciled IDs. | CLI passed live (`c1e00b1`, 2026-10-02); desktop pending |
| Native close | During a transfer, close the window. **Keep Open**, Escape or dialog cancellation keeps the application active. **Stop and Close** stops/drains work before exit. Reopen, select the same source/destination and preview to continue. | Pending |
| Changed source after approval | Produce a preview, then alter a fixture file before starting. The old approval is rejected; no changed bytes are accepted under that plan. | Pending |
| Missing rule after restart | After a preview establishes the fixture rules, close the app and move its `.gitignore` outside the fixture yourself. Reopen and preview the same source. Upload remains blocked until the rule is restored or policy is explicitly reconfigured and newly approved. | Pending |
| Remote alteration | Manually rename/move/change one of the app-created disposable objects, then preview again. The app reports review/failure and does not silently overwrite, adopt, duplicate or delete it. | Pending |
| Connection failure | Interrupt connectivity during a disposable transfer and restore it. Observe bounded recovery or a clear stopped/review state. A fresh preview must reconcile ambiguity rather than duplicate objects. | CLI passed live (`c1e00b1`, 2026-10-02); desktop pending |
| Local disconnect | During an active fixture transfer, disconnect locally. Work stops before credential cleanup; completed Drive files remain. Reconnection requires the normal explicit flow. | Pending |

Remote token revocation is intentionally not part of routine acceptance. In this
shared Google project it may affect other applications for the same account.
Test it only with separate explicit authorization and an appropriate disposable
account. Canceling its confirmation must not cancel a running transfer.

## Platform and release gates

Repeat the relevant native journey on each advertised desktop OS/architecture;
a compile, Xvfb startup, mocked Wails bridge or browser-only screenshot is not
equivalent to a native journey. Check installed-package behavior, vault access,
Picker return, Unicode/space names, cancellation and the native close dialog.
Windows Server/headless packages remain CLI-only for this candidate workflow.

For CLI acceptance, repeat preview, first copy, unchanged repeat, keep-both and
interruption/recovery from an interactive terminal. Verify exact digest approval,
Ctrl-C/SIGTERM draining and refusal of piped/unattended approval. On an explicitly
authorized SSH test host, verify the printed loopback tunnel and browser return
without exposing a public callback listener. An unavailable native vault must
block online operations. Start another GUI/CLI process during authorization or
an active authorized response and verify safe contention, then fresh status after
release. Automated fake tests do not establish these real server/session results.

Before publication, record the passing build/test/packaging runs and immutable
artifact identities, then verify the actual new download/installation path.
Keep all existing alpha.3 and earlier release bytes, tags, APT snapshots and
checksums intact. macOS notarization and Windows publisher signatures are
separate release gates; this candidate must not claim them from a successful
build or APT repository signature.

Fixture cleanup is manual and restricted to exact test-created IDs after review.
Do not recursively purge the chosen parent. LedgeSync does not implement remote
deletion in this workflow.
