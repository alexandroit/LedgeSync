# Manual Google Drive folder-copy acceptance

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
| Existing connection | Open the configured candidate; identify the intended account. Check the connection and reopen the app. No client JSON or token entry is requested. | Pending |
| Native folder selection | Choose **Choose existing Drive folder**, select the disposable writable parent in Google's browser Picker and return. The application shows the selected parent and the same account. | Pending |
| Selection cancellation | Start selection and cancel. The previous destination remains; no upload starts and a new preview is required. | Pending |
| Account binding | If testing an explicitly authorized second disposable account, select it in the browser. The first saved account and destination must not be silently replaced. | Pending |
| Preview only | Select the fixture and choose **Preview folder upload**. Inspect account, destination ID, root child, hierarchy, empty directory, sizes and exclusions. No remote object is created before approval. | Pending |
| First copy | Choose **Upload folder**. Wait for verified completion, then use **Open destination folder on Google Drive**. The managed fixture root appears inside the parent, with all included entries and no excluded file. | Pending |
| Independent integrity | Inspect the root and empty directory in Drive. Download only the newly created fixture files and compare sizes/SHA-256 with the recorded source values, including empty and multi-chunk files. The unrelated parent marker remains unchanged. | Pending |
| Unchanged repeat | Preview and approve again with unchanged inputs. Entries show existing-copy verification, the same provider IDs remain, and no duplicate hierarchy/files appear. | Pending |
| Local change / keep both | Edit one fixture file yourself, preview and approve. The original remote version remains and the changed version has a stable `.ledgesync-` suffix. A repeated unchanged run does not create another suffix copy. | Pending |
| Cancel and continue | Start a sufficiently large fixture upload and choose **Cancel upload**. Completion is not reported. Already created objects remain. Preview again and continue using reconciled IDs. | Pending |
| Native close | During a transfer, close the window. **Keep Open**, Escape or dialog cancellation keeps the application active. **Stop and Close** stops/drains work before exit. Reopen, select the same source/destination and preview to continue. | Pending |
| Changed source after approval | Produce a preview, then alter a fixture file before starting. The old approval is rejected; no changed bytes are accepted under that plan. | Pending |
| Missing rule after restart | After a preview establishes the fixture rules, close the app and move its `.gitignore` outside the fixture yourself. Reopen and preview the same source. Upload remains blocked until the rule is restored or policy is explicitly reconfigured and newly approved. | Pending |
| Remote alteration | Manually rename/move/change one of the app-created disposable objects, then preview again. The app reports review/failure and does not silently overwrite, adopt, duplicate or delete it. | Pending |
| Connection failure | Interrupt connectivity during a disposable transfer and restore it. Observe bounded recovery or a clear stopped/review state. A fresh preview must reconcile ambiguity rather than duplicate objects. | Pending |
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
