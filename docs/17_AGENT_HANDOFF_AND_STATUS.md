# 17 — Agent Handoff and Current Status

**Updated:** 2026-10-02. **Product:** LedgeSync. **Specification:** 0.2.1.

## Alpha.6 — live Google acceptance found and fixed two upload defects

**Alpha.5 cannot upload files to real Google Drive.** With the owner's consent,
the live acceptance ran against real Google Drive on 2026-10-02. The official
alpha.5 CLI created the managed folder and then failed every file upload.

A redacted diagnostic build found two defects that the emulator did not model:
- **Google's session parameter.** Google's resumable session URI carries a
  `session_crd` parameter, and the client's session check rejected it, so no
  file was ever sent.
- **Detected media types.** Drive stores the media type it detects
  (`README.md` → `text/markdown`), and verification required the uploaded type.

Both are fixed (`1eb3e71`, `a8c7a5d`; [ADR-033](12_ADR_DECISIONS.md)), and the
emulator now models them. With the fixes, the live acceptance passed all 14
steps: first copy, independent SHA-256 restore, unchanged repeat, keep-both,
`kill -9` and resume, network loss and continuation, and automatic copy with a
pause on rule change. Details are in the
[failure analysis](research/DRIVE_SYNC_FAILURE_ANALYSIS.md#live-acceptance-findings-alpha6)
and the [live acceptance](research/DRIVE_UPLOAD_ACCEPTANCE.md).

Alpha.5 assets stay immutable, and alpha.6 replaces them. Publication identities
are recorded below once complete. Still pending from the owner: the desktop
Picker pass and publisher signing material.

## Alpha.5 published — superseded by alpha.6 (uploads fail against real Drive)

**Owner report:** the application was not synchronizing files. **Outcome:** four
independent defects were reproduced with the production HTTP provider against a
new `drive.file` emulator, fixed and regression-tested: My Drive selection
failed under `drive.file`; any symbolic link (for example in an ignored
`node_modules` or `.venv`) aborted the scan; churn in ignored files turned runs
into `needs_review`; and earlier copies missing or moved in Drive blocked every
later run. The owner's own failing machine/build was not available (no installed
app, logs or crash reports on the inspected Mac); see the
[failure analysis](research/DRIVE_SYNC_FAILURE_ANALYSIS.md) for evidence, fixes
and test names. The work was merged to `main` through
[pull request 2](https://github.com/alexandroit/LedgeSync/pull/2) and published as
pre-release [v0.1.0-alpha.5](https://github.com/alexandroit/LedgeSync/releases/tag/v0.1.0-alpha.5)
from `64cf420` (37 assets). Installers passed on clean Windows and Ubuntu runners,
the signed APT snapshot `20261002-alpha5-64cf420` is active with public
installation verified on amd64/arm64, and ledgesync.com and Pages serve the
alpha.5 downloads. Exact identities and results:
[alpha.5 evidence](research/DRIVE_SYNC_ALPHA5_RELEASE.json) and
[platform gates](PLATFORMS.md). Alpha.4 tags, assets and APT pool bytes are
unchanged.

**Live Google acceptance is pending the owner.** The configured CLI opened
Google's consent page; nobody completed it (`AUTH_TIMEOUT`), so no account was
connected and nothing was written to Drive. Run
[live_acceptance.py](../tools/live_acceptance.py) after `ledgesync auth connect`
as described in the [acceptance checklist](research/DRIVE_UPLOAD_ACCEPTANCE.md).

**Publisher signing is blocked on owner material.** The build Mac has only an
*Apple Development* identity (not notarizable) and no notarization credentials;
the repository has no Windows signing configuration. The signing pipeline is
implemented and fails closed; [Publisher signing](PLATFORMS.md#publisher-signing)
lists exactly what to provide. Ubuntu APT metadata is already signed with the
existing server key.

### Completion dispositions (handoff A–H)

| Item | Disposition | Evidence |
|---|---|---|
| A — first/incremental copy failure, safe diagnostics | **Implemented**; live acceptance pending owner consent | `internal/transfer/e2e_test.go` (12 end-to-end tests), `internal/transport/desktop/journey_test.go`, `internal/connections/errors_test.go`, Playwright `pairs.spec.ts` |
| B — configuration semantics | **Implemented:** `conflictPolicy: pause` pauses changed files (no upload, listed in the review); `maxRetries` bounds retries (`drive.Client.WithRetries`, default 6); `maxTransfers` is an upper bound honored by serial execution. **Deferred:** parallel transfers (the credential service serializes requests by design; concurrency needs a reviewed redesign) | `internal/transfer/service.go` (`ActionPaused`), `internal/providers/drive/client.go` |
| C — durable projects and primary GUI | **Implemented:** saved pairs (private catalog), reopen without stored approval, policy editor (rule groups, dialects, composition, conflict policy, retries), Activity, History & Recovery and Settings on saved data | `internal/projects`, `internal/transferstate/catalog.go`, `internal/transport/desktop/projects.go`, Playwright `pairs.spec.ts` |
| D — repeat, recovery, diagnostics | **Implemented:** unchanged repeats skip; changed files keep both; cancellation, process loss, lost acknowledgements, expired sessions and network loss reconcile reserved IDs; per-file issues and bounded run history | e2e tests above; `TestE2ECancelledMultiChunkUploadResumesWithoutDuplicates`, `TestE2ETransientFailures…`, `TestE2EExpiredSession…` |
| E — opt-in automatic copy | **Implemented:** explicit authorization bound to a reviewed preview; interval and change checks while the app runs; `automatic run`/`watch` for servers; pause on any bound change or missing earlier copies; wait when offline, busy or the source is unmounted; global pause; nothing installed | `internal/projects/scheduler.go`, `projects_test.go`, `TestDesktopJourneyPickerAutomationRunsAndPausesOnRuleChange`, `TestCLIPairsCopyAutomaticAndRestoreShareServices` |
| F — VCS/code-tool policy adapters | **Deferred, fail closed:** Git and rclone dialects remain the implemented adapters; Mercurial, SVN (properties), Perforce, CVS, Bazaar/Breezy, Fossil, docker/npm/prettier/helm stay *Not implemented* in capabilities and block configurations that enable them. No dialect is silently mapped to Gitignore | `internal/policy/policy.go` capabilities; `app` `CAPABILITY_UNSUPPORTED` path |
| G — native/server reliability and release security | **Implemented in source:** GUI and CLI share every service; native vaults and permissions unchanged; Drive requests wait briefly for short credential operations; signing pipeline for macOS/Windows; APT signing unchanged. **Blocked:** publisher signing material; live SSH consent; release builds on all targets need CI | `tools/sign_macos.py`, `tools/sign_windows.ps1`, `.github/workflows/*.yml` |
| H — remaining recovery scope | **Implemented:** restore to a new empty folder with MD5/SHA-256 verification (desktop and CLI). **Explicitly disabled (ADR-031, owner confirmation pending):** managed overwrite, mirror and any deletion | `internal/restore`, `restore_test.go`, Playwright restore test |
| Mobile | **Not claimed.** macOS builds are not iPhone/iPad deliverables; iOS scope still needs the owner's decision | — |

### Commands and results (2026-10-02, macOS 27 arm64)

From the repository root, with Go caches under `build/claude/`:

- `go test ./cmd/... ./internal/...` — all packages pass.
- `go test -race ./cmd/... ./internal/...` — all packages pass.
- `go vet ./cmd/... ./internal/...` and `go vet -tags bindings ./cmd/ledgesync-desktop` — clean.
- `cd frontend && npm ci && npm run build && npx playwright test` — build passes, **53 passed**.
- `.venv/bin/python tools/test_*.py` — OAuth client 9, CLI packaging 6, DMG 4, Windows packaging 4, Debian 11, APT builder 4: all pass.
- `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7` on the edited workflows — only false positives for runner labels the linter predates (`macos-15-intel`, `windows-11-arm`), which earlier CI runs used successfully.
- Native macOS arm64 desktop (Wails v2.14.0) and CLI built with the publisher client; generated client source removed. The app started and exited cleanly, created private state, and ran with the hardened runtime and no entitlement exceptions (`flags=0x10002(adhoc,runtime)`).

Native runs on clean machines (all through GitHub-hosted runners): core tests on
Ubuntu 24.04 x64/ARM, macOS 15 ARM/Intel, Windows Server 2022/2025 and
Windows 11 ARM; desktop builds on six targets; native vault lifecycle on six
targets; Windows installers on Server 2022 x64 and Windows 11 ARM64; Ubuntu
packages and public APT on amd64/arm64.

**Not run:** live Google consent/upload (owner step; the attempt timed out
unattended), Picker and desktop window clicks against real Drive, publisher
signing and notarization (no material), Windows 11 x64 installer (no hosted
runner), SSH return.

### Next safe steps

1. Owner: `ledgesync auth connect` with the test account, then
   `tools/live_acceptance.py` (see the acceptance checklist), plus one manual
   Picker/desktop pass with the alpha.5 app.
2. Owner: provide signing material as listed in
   [Publisher signing](PLATFORMS.md#publisher-signing); then rebuild from `main`
   and publish a signed version (a new version number; alpha.5 stays immutable).
3. Owner: disable Cloudflare Web Analytics injection for ledgesync.com or
   disclose it in the privacy policy ([website note](WEBSITE.md#verified-alpha5-deployment)).
4. Owner decisions still open: iPhone/iPad scope, managed overwrite/mirror
   (ADR-031), and VCS adapters beyond Git/rclone (item F).

---

The sections below are the historical record before this change.

**Historical blocking owner report:** the application is not synchronizing files.
The release/CI records below describe historical distribution and synthetic/native
checks. Use [CLAUDE_CODE_HANDOFF.md](../CLAUDE_CODE_HANDOFF.md) as the continuation
entrypoint; the current status is above.

**Implementation:** 0.1.0-alpha.4 implements explicitly approved Drive folder
uploads. All 37 release assets are published and publicly verified. The alpha.4
APT snapshot is activated and public installation passed on both architectures.
The canonical Ubuntu website and secondary Pages copy serve the updated release.

**Source follow-up:** OAuth hardening, native destination Picker, durable copy
journal, resumable binary uploads, desktop approval/progress and interactive
server CLI copies are implemented.
Public alpha.3 assets remain immutable.

Read [PROJECT_IDENTITY.md](../PROJECT_IDENTITY.md) first. The authoritative name
is LedgeSync, command `ledgesync`, primary domain `ledgesync.com`. The owner
requested public Apache-2.0 source, a website and desktop/server platform builds.
After initially deferring DNS activation, the owner explicitly requested
publication on the existing Ubuntu server hosting HiperMusicas. The canonical
site is now live there. The owner subsequently requested Google Drive token
authorization, then selected a bundled Desktop client with one-click browser
consent. The owner then confirmed a working connected account and enabled Picker
API, and requested real whole-folder uploads inside an existing Drive folder.

## Approved Drive folder uploads — alpha.4

### Cross-platform release follow-up

The owner requested the same security standard on every supported system. The
implementation uses each platform's own credential vault and verifies local
journal access separately. Linux user-bus connections now check `SO_PEERCRED`
before any D-Bus authentication bytes. macOS rejects extended ACL grants that
POSIX mode bits cannot express. Windows creates protected owner-only inheritable
DACLs and inspects native ownership, ACLs, reparse points and hardlinks.
Unavailable native protection blocks the operation; no plaintext fallback was
introduced. iPhone/iPad scope is awaiting clarification; the existing Darwin
builds target macOS, not iOS.

Build run `36960353039` at `5179dbeb1458caa4169b703a25fdc37e06524184`
compiled all six native desktop targets but failed core tests on macOS/Windows.
The follow-up fixes canonicalize trusted macOS `/var` and `/tmp` aliases and
Windows short names before source/state containment checks, correct a Windows
SQLite test URI, and revalidate actual directory membership when timestamps do
not reveal a newly added ignore file. Journal recovery now rejects malformed
operation status, kind, IDs, paths and checksums. New regression tests accompany
these fixes. The same commit's native vault run `36960353021` passed all six
targets; it predates the additional Linux peer-identity check.

After these fixes, local full `go test -race ./...` and `go vet ./...` passed,
as did `.venv/bin/python tools/validate_docs.py` with zero failures/skips and
the 63 notice hash checks. At that checkpoint, new native CI and immutable installers were still
required before alpha.4 publication. The final build/vault results are recorded
below; no live Google acceptance was inferred from the local checks.

The security follow-up commit `daf8b1047cbbb8642fd081824e3a61c3a5d3ed2f`
subsequently passed all 16 jobs in build run `36962530879` and all six native
vault jobs in `36962530784`. This includes actual native ACL/permission tests
and the Linux peer-identity check. Those intermediate binaries are not the
final release inputs: the subsequent CLI/shared OAuth lock changes received
their own exact-commit build and vault runs, recorded below.

The CLI follow-up adds native publisher-configured builds on all six targets
(macOS CGO/Keychain, Linux and Windows native vaults without CGO), interactive
preview/digest approval, browser/SSH loopback consent, and cancellation draining.
A separate native process lock protects the shared OAuth lifecycle, including
authorized response bodies. External contention produces a safe `busy` state
without stale account controls. Shared source selection rejects application
settings and their parents before creating lock files. A targeted race test
exposed and now guards nil-record status publication during concurrent revocation.

Local checks before final native CI: full Go race suite and vet passed; 46 frontend interaction
tests and its production build passed; 9 OAuth helper, 6 CLI packaging, 11 Debian
and 4 Windows packaging guard tests passed. Documentation validated 52 Markdown
files and 179 local links with zero failures/skips; 63 notice hashes passed.
No personal account, real cloud write or live SSH consent was used by these tests.

### Final alpha.4 validation and publication

Application/tag source: `fcd578488d07f627372e7f5dd2221e162634bf05`.
[build run 36963525743](https://github.com/alexandroit/LedgeSync/actions/runs/36963525743) passed **16/16 jobs**, including the seven native core targets,
six native desktop/CLI packaging targets, contracts, frontend tests/build and
the pinned rclone differential. Every desktop job completed publisher-client
injection, the native app/CLI builds and generated-source cleanup. Both macOS
DMGs passed the packaging/verification step. Race tests ran on supported core
runners; Windows ARM64 explicitly skips the race detector.

[native-vault run 36963524884](https://github.com/alexandroit/LedgeSync/actions/runs/36963524884) passed **6/6 native vault jobs** at that same SHA.
This release includes the Linux peer-identity check, native journal/lock
ACL protections, interactive CLI copy and shared OAuth process lock. The earlier
failed/intermediate runs above are preserved as history and are not release
inputs. Automated tests use synthetic credentials/data and disposable native
vault entries; no personal Google authorization or SSH consent was exercised.

[Installer run 36964667348](https://github.com/alexandroit/LedgeSync/actions/runs/36964667348)
passed all five required jobs at packaging source
`13342144685824daa38c774cb3ef7bdf14e315d3`: Windows x64/ARM64 wizard and lifecycle,
Ubuntu amd64/arm64 package/startup/lifecycle, and local signed APT installation.
[Installer evidence](research/DRIVE_COPY_INSTALLERS_RELEASE.json) records exact
inputs and native results. [Public asset verification](research/DRIVE_COPY_PUBLIC_ASSETS.json)
confirms anonymous SHA-256/size checks for all 37 assets and the exact source tag;
all 106 earlier assets and their release/tag identities remain unchanged.

APT snapshot `20261002-alpha4-fcd5784` is activated with the existing signing key.
[Public APT run 36965018881](https://github.com/alexandroit/LedgeSync/actions/runs/36965018881)
passed on native Ubuntu amd64 and arm64 at packaging source `13342144685824daa38c774cb3ef7bdf14e315d3`: pinned HTTPS key/source,
metadata signature, tamper rejection, by-hash acquisition, desktop and headless
CLI install/remove, and preservation of a synthetic user fixture. Installed
Debian version: `0.1.0~alpha.4-1`. No application was installed on the production
web server. The previous 36 APT pool/by-hash files and earlier snapshot remain
unchanged.

Website/Pages source: `6c8f1d19f8c2aa398d98d0a5b39ba5a1999772d1`.
[Pages run 36965146423](https://github.com/alexandroit/LedgeSync/actions/runs/36965146423)
passed; seven files matched exact source bytes at public HTTPS, origin TLS and
Pages (21 checks). Seventeen shared configuration hashes and the HiperMusicas
service PID remained unchanged; public/origin health checks passed. No Nginx
reload was required. Previous site and APT directories are retained for rollback.
The public repository identifies Apache-2.0 and release notes match the 37 assets.
See [application evidence](research/DRIVE_COPY_RELEASE.json), the installer and
public-asset records above, and [APT/site deployment verification](research/DRIVE_COPY_DEPLOYMENT_VERIFICATION.json).
All distribution results are collected in [platform release results](PLATFORMS.md#alpha4-native-validation-and-publication-gates).
No trusted Apple publisher/notarization or Windows Authenticode signing has been
supplied. Live Google Picker, whole-folder copy, recovery and SSH return remain
separate acceptance work. No iOS acceptance or package is claimed.

### Copy behavior

The Files screen now exposes a real destination, selected through Google's native
browser Picker or My Drive, followed by a fresh preview and explicit approval.
The entire included source hierarchy, including empty folders, is copied inside
a new app-managed child folder on the first run. Active ignore rules apply.
Later runs reuse that managed hierarchy, verify and skip unchanged content, and
create a separate suffixed copy for changed files. No overwrite, deletion,
automatic watcher, background scheduler or shared-drive support was added.
The CLI now invokes the same services for native browser authorization and
interactive approved copies. It prints the full preview and requires its exact
digest in the same process. Headless consent uses an explicitly requested
loopback SSH tunnel and the server user's available native vault. There is no
unattended apply command, credential-file fallback or independently implemented
CLI provider. See [CLI usage](CLI.md).

The backend binds approvals to source/rules/configuration, account, destination,
remote observations and journal state; unstarted previews expire after 15 minutes.
SQLite records generated IDs and operation markers before remote creation,
acknowledgements before final verification and durable terminal run events before
the UI reports success. Interrupted copies require a fresh preview and reconcile
their recorded IDs. A kernel process lock serializes overlapping local writers.
Known rule-source disappearance remains blocked after restart and destination
changes. Journals contain identifiers, paths and checksums, never tokens or
resumable session URLs. Transfer cancellation drains before account cleanup.

Source decisions and dependencies: [Drive provider/executor review](research/DRIVE_PROVIDER_REVIEW.md),
[native Picker/auth request review](research/NATIVE_PICKER_REVIEW.md), and
[SQLite runtime review](research/TRANSFER_STATE_REVIEW.md). The original pinned
rclone source was inspected; its update/delete/live-sync loop was not imported.

Initial local upload-only validation, before the CLI/process-lock follow-up:

- Go unit/integration run: 484 passing test/subtest events, 3 explicit skips
  (native vault opt-in, case-alias test on the case-sensitive SSD and external
  rclone reference). The separate pinned-rclone differential run passed.
- Full Go race and vet checks passed. `govulncheck v1.8.0 -tags desktop` found
  no known vulnerabilities in the checked application graph.
- Frontend: 45 interaction tests passed; the production bundle's 900 by 620
  approval/cancel flow passed keyboard checks and screenshot inspection.
- Nine OAuth build-helper tests passed. Runtime notices cover 29 modules and
  63 verified license-file hashes. Windows AMD64 SQLite tests cross-compiled;
  that check does not establish Windows runtime behavior.
- A native macOS ARM64 graphical build with the already-authorized publisher
  Desktop client succeeded. Generated client source was removed afterward.
  Packaging and public release were still pending at this initial checkpoint;
  their final native and public verification is recorded above.

Changed application areas: `internal/{transfer,transferstate,discovery,driveauth}`,
`internal/providers/drive`, desktop transport/main, frontend Files/Connections,
package descriptions, dependency notices and the linked guides. Prior uncommitted
OAuth hardening and read-only CLI status work were retained in the released source.
Tests use synthetic contents, local fake endpoints and disposable journals.
No personal vault was read or real Google upload performed by these checks.
The owner reported production consent and successful connection; native Picker,
upload and restart acceptance remain pending the [manual fixture checklist](research/DRIVE_UPLOAD_ACCEPTANCE.md).

Implementation limits: uploads are serial, file metadata/content and ignore
sources are rechecked, and success includes a final full scan. Uncooperative
network/FUSE filesystem reads can still delay cancellation. Power-loss durability
depends on the operating system/filesystem honoring SQLite flushes. The first
copy schema embeds approved configuration/rule/inventory identities in the run
record and stores operation/object mappings together; destructive recovery and
future schema migrations remain separate work. Do not call this production-ready
or claim real Google acceptance from fake-provider tests.

## Historical initial privacy, terms and branding publication

Published `https://ledgesync.com/privacy-policy` and
`https://ledgesync.com/public-term` on the existing Ubuntu origin, with homepage
footer links and the owner-supplied public contact `alex@alexandro.net`.
Website source: `43f7ab6ada2711795ca0b14df6bc333f6344e131`. The existing app logo
is available at `https://ledgesync.com/assets/ledgesync-logo.png` (512 × 512,
4,072 bytes), with an unchanged SVG companion. No logo redesign was performed.

Validation: static HTML/title/heading/local-reference checks passed (51 local
references); original PNG byte identity and dimensions passed; `nginx -t`
passed before graceful reloads. Public and direct-origin HTTPS content matched
all seven source files; 18 origin/public checks and three secondary Pages checks
passed. Pages workflow 36957112425 succeeded. An initial CDN email transformation
was fixed with scoped `no-transform` headers. HiperMusicas retained its PID and
configuration hashes, and APT metadata was unchanged. No browser visual QA was
requested or performed. See [deployment evidence](research/LEGAL_SITE_DEPLOYMENT_VERIFICATION.json),
[website operations](WEBSITE.md) and [branding fields](BRANDING.md).

This website publication does not publish the preceding local OAuth code edits
or change application installers, DNS or shared Google Cloud settings. The
existing private Sites preview was not republished; the requested production
domain and secondary GitHub Pages copy were updated. Next owner step: enter the
published URLs and PNG into Google's branding form, reviewing shared-project
impact before saving project-wide changes.

## OAuth protection verification and hardening follow-up

The following records the hardening checkpoint before alpha.4 publication.
Those changes are included in the release described above.

**Latest local follow-up:** [OAuth local review](research/OAUTH_LOCAL_REVIEW.md)
records the new request, retained protections, code changes, exact checks and
remaining integration limits. The build helper now excludes unused development
metadata, explicit API scope loss requires reconnection, and `auth status` uses
the shared authentication service without browser/network/token mutations.
These working-tree changes have not been published in installers or deployed.

Local follow-up checks: 333 Go test/subtest pass events, 2 explicit integration
skips; race/vet checks, 9 synthetic build-helper tests and 30 frontend tests
passed. Native macOS GUI/CLI and Linux/Windows AMD64/ARM64 CLI compilation passed.
The first Windows ARM64 build failed for system-volume disk exhaustion; the
SSD-isolated retry passed. Documentation validation passed with zero failures.
No live Google or personal-vault validation was performed. Exact commands,
changed files, limitations and authorization boundaries are in the linked report.
Next safe step: review the local diff; plan an isolated, explicitly authorized
native-vault/Google acceptance run before preparing a new immutable release.

The owner requested verification of existing protections and code fixes for
missing controls. Read [the requirement-by-requirement evidence and limits](research/OAUTH_SECURITY_HARDENING.md).
Existing PKCE/state generation, system browser, native vaults, backend-only
tokens, exact `drive.file`, bounded refresh, account/client binding and publisher
build configuration were retained after inspection.

Corrections: callback attempt context and closure before exchange; canonical
route/parameter checks; duplicate/case-aliased provider JSON rejection; cancel and
drain before local credential removal; separate confirmed remote revocation
bound to the reviewed account; explicit shared-project warning, default Cancel,
and local-cleanup recovery after confirmed revocation. CLI previews remain
offline; the subsequent local follow-up adds read-only account status. No
alternate CLI credential store or authorization flow was introduced.

Changed files: `internal/driveauth/{oauth,service,http,types}.go`, new callback,
lifecycle and diagnostic tests, two callback test constructor calls, desktop
bridge/tests, frontend Connections/types/styles/tests, CLI help and this
documentation. Tests use local fake endpoints and synthetic credentials. No
real grant was authorized/revoked, no personal vault entry was read, no cloud
files were uploaded and no shared Google Cloud settings were changed.

Validation completed locally:

- `go test -json ./...`: passed, with 315 test/subtest pass events. The native
  vault opt-in and external rclone differential tests explicitly skipped;
  three packages had no tests. Synthetic OAuth and credential-boundary tests ran.
- `go test -race ./...` and `go vet ./...`: passed.
- Callback-specific race regressions also passed five bounded repetitions,
  covering concurrent replay and listener closure timing.
- `python3 tools/test_configure_oauth_client.py`: all nine synthetic guards passed.
- Frontend `npm run check`, `npm test`, `npm run build`: passed, including all
  30 Playwright tests. An initial UI test exposed a closed-dialog cleanup timing
  issue; dismissal now removes it synchronously and all tests passed afterward.
- `go test ./internal/transport/desktop` and `go vet ./internal/transport/desktop`:
  passed. Native macOS ARM64 compilation passed with `go build -tags desktop`
  and macOS 13 CGO minimum flags; this compilation did not import a real client
  or authorize an account.
- Compiled production UI was inspected at the 900×620 minimum window using a
  synthetic account. The impact warning and buttons fit, Cancel was focused,
  and the test dialog was canceled. Owned preview processes were closed.
- `.venv/bin/python tools/validate_docs.py`: 42 Markdown files, 140 local links,
  35 JSON files, two schemas and five examples; zero failures/skips.
  `git diff --check` passed.

[CI run 36955479712](https://github.com/alexandroit/LedgeSync/actions/runs/36955479712)
passed all 16 jobs at source `ad941e345b2fc4a62cc7dce85f6a87479f8bc859`, including
all six desktop builds, seven native core targets, contracts, all 30 frontend
cases and the external rclone differential check. The subsequent UI copy-only
follow-up makes the session-local cleanup warning explicit; its existing
Playwright recovery test passed with two new assertions, and TypeScript plus
production frontend build passed again. No backend behavior changed afterward.
See [machine-readable verification](research/OAUTH_HARDENING_VERIFICATION.json).

Native-vault lifecycle evidence for alpha.3 remains historical; the owner's
personal native-vault integration opt-in remains disabled. Cross-process
credential serialization, multi-account support and cloud transfer jobs are
still separate implementation gates. A failed local cleanup after confirmed
revocation is remembered only in the running process; finish cleanup before
closing the app.

## Historical one-click authorization follow-up (alpha.3)

The owner selected direct computer-to-Google authorization, with no end-user
JSON import and no server token store. The supplied path still contained a Web
client; a sibling downloaded Desktop `installed` client was found and verified
to belong to the same Google project. No credential values or user tokens were
printed. The Desktop client is authorized as a build input; the Web client is
not used. Actual browser consent remains for the owner to complete.

Changed files: core `internal/driveauth/**`, `internal/connections/**`, native
bridge/main, frontend Connections and tests, build-time injector/CI, packaging
copy/version and authorization documentation. The native app uses immutable
bundled client configuration, zero-write initial status, direct PKCE browser
consent, and a native window activation request only after successful connection.
The JSON-import and Cloud-setup bindings no longer exist. Existing different
client grants remain intact and unusable until an explicit disconnect succeeds.

Validation completed locally:

- `go test ./...`, `go test -race ./...`, `go vet ./...`: passed.
- Frontend `npm run check`, `npm test`, `npm run build`: passed; 22 Playwright
  tests (18 authorization and four explorer). Production screenshot reviewed.
- `python3 tools/test_configure_oauth_client.py`: nine synthetic guards passed,
  including strict parser/redaction, atomic file publication, generated Go
  compilation and absence of fixture client values in Go build metadata.
- `python3 tools/test_package_dmg.py`: four passed;
  `python3 tools/verify_licenses.py`: 44 recorded hashes verified.
- `.venv/bin/python tools/validate_docs.py`: zero failures/skips.
- Native macOS ARM64 Wails build passed with macOS 13 minimum. Actual WebView
  displayed **Not connected**, **Ready to request access in your system browser**,
  and **Connect Google Drive**, with no import/setup controls. No browser consent
  was started; no user token written. The smoke app was closed afterward.

The correct Desktop client was configured as the canonical repository Actions
secret via stdin, with no credential output; only trusted main native builds
receive it. Its generated source is ignored and desktop cache export disabled.
Desktop client metadata remains extractable from distributed binaries by OAuth
public-client design. User tokens are never CI inputs.

Release validation and publication:

- Application/tag source is `4da377c311b78a99b1a9fde1127d77e21e6e05ec`.
  [Build run 36953803971](https://github.com/alexandroit/LedgeSync/actions/runs/36953803971)
  passed all 16 jobs, including 22 frontend tests. Each of the six desktop jobs
  passed client injection, native build and generated-source cleanup. Separately,
  a private byte comparison matched the supplied Desktop configuration in all
  six released graphical binaries and confirmed its absence from all six CLI
  binaries. Client values were not printed; the report contains only results.
  No user tokens were build inputs and no live Google consent was performed.
- [Vault run 36953805903](https://github.com/alexandroit/LedgeSync/actions/runs/36953805903)
  passed synthetic native credential lifecycle checks on all six runners.
- [Installer run 36954256173](https://github.com/alexandroit/LedgeSync/actions/runs/36954256173)
  passed all five required jobs at packaging source
  `0ec2f3fb66624522638b69f3b7a71ef517cf5259`: both Windows wizards and
  install/reinstall/remove flows, both Ubuntu package checks and signed local
  APT installation. These retain exact released application bytes; they do not
  establish cross-version migration or full Windows/Linux GUI acceptance.
- Public APT snapshot `20261002-alpha3-0ec2f3f` is active with the existing key
  unchanged and all 24 prior pool/by-hash files preserved.
  [Public run 36954499569](https://github.com/alexandroit/LedgeSync/actions/runs/36954499569)
  passed on native Ubuntu 24.04 amd64 and arm64: pinned HTTPS key/signatures,
  tamper rejection, forced by-hash, desktop with GNOME Keyring, separate CLI and
  removal preserving synthetic data. No application or OS package was installed
  on the production server.
- All 37 public alpha.3 assets passed anonymous download, size and SHA-256
  verification. The 24 initial alpha.3 assets and all 69 alpha.1/alpha.2 assets
  retain their original identities and bytes.
- The canonical Ubuntu website and secondary Pages serve source
  `5c702076b39a8170f2065c3262f5060d7b65698f`, with matching public HTML/CSS.
  [Pages run 36954520715](https://github.com/alexandroit/LedgeSync/actions/runs/36954520715)
  passed. HiperMusicas public/origin remain HTTP 200, PID 1521428 unchanged;
  shared Nginx configuration hashes are unchanged.

Evidence: [application archives/DMGs](research/OAUTH_ONECLICK_RELEASE.json),
[installers](research/OAUTH_ONECLICK_INSTALLERS_RELEASE.json),
[public assets](research/OAUTH_ONECLICK_PUBLIC_ASSETS.json), and
[APT/site deployment](research/OAUTH_ONECLICK_DEPLOYMENT_VERIFICATION.json).
Final documentation checks passed: 41 Markdown files, 132 local links, 35 JSON
files, two schemas and five examples, with zero failures/skips; `git diff --check`
passed. The owned local website preview and production upload staging files were
removed; active and rollback content/APT snapshots were retained. Generated
Desktop client source was removed from the working tree after the build.

Actual Google consent, refresh/revocation and owner-controlled project audience
remain unverified. Cloud file transfers, trusted publisher signing and
notarization remain outside this release's acceptance. Alpha.2 evidence below
is historical to that release.

## Historical alpha.3 implemented behavior

This section records the earlier authorization/offline baseline. The alpha.4
copy provider, journal, locks and CLI follow-up are documented at the top.

The Go application service is shared by CLI and Wails desktop. It loads strict
v1.1 configuration, rejects duplicate/unknown fields and unsupported configured
capabilities, takes source/rule snapshots, evaluates separate Gitignore and
rclone dialects, composes decisions with provenance, and produces a bounded
copy plan against an explicitly simulated empty destination. No plan executor
exists. Source files are read-only; exported plans must be new files outside
the source, checked by directory identity as well as path spelling.

The desktop provides native folder/configuration pickers, list/grid navigation,
breadcrumbs, search, excluded-file visibility, a policy inspector, preview
operations and adapter capability information. It consumes actual Go output;
there is no independent TypeScript policy engine. Browser interaction tests
stub the Wails transport only, using inventories/plans generated by the CLI.

The policy registry advertises implemented and unavailable profiles honestly.
Git support is patterns-only, independent of tracked-file status. There is no
external Git/rclone runtime dependency. The native Drive provider, additional
VCS adapters, durable SQLite journal/migrations, persistent rule baseline,
root-pair locking, execution, scheduling, mirror and recovery remain unfinished.
Empty-directory transfer and broad grammar/performance compatibility are not
claimed. No entire P1/P2/P3/P4 milestone is marked complete.

## Historical task coverage before the alpha.4 copy follow-up

- P0-00A/B/C: actual rclone v1.75.1 source clone, full SHA pin, selected source
  review and isolated upstream tests completed before application code. Audit,
  reuse matrix, license texts and baseline are in `docs/research` and ADR-021.
- P0-01/02/03: pinned toolchains, typed domain, shared service and executable
  surfaces implemented. Go 1.27.1, Wails 2.14.0, Node 24.20.0; lockfiles retained.
- P0-04: pinned GitHub Actions matrix implemented; remote outcomes must be
  checked below rather than inferred from workflow YAML.
- P1-01/02/02A/03/04/05/06/08/09: bounded offline portions implemented and
  tested. Migration, persistence, additional adapters and full acceptance
  requirements remain open; these task IDs are not wholesale completion claims.
- P2-01: desktop OAuth and native vault implemented under the explicit follow-up
  request. Synthetic protocol/UI tests pass; live consent acceptance with the
  bundled publisher client remains pending. P2-02 account identity read exists, but file listing
  and provider capabilities remain deferred. Neither task is fully accepted.
- P1-03A/P1-07 and remaining P2 tasks: deferred. Unsupported required sources fail
  closed. Cloud file writes and destructive capabilities remain absent.

The canonical Drive documentation was reread after the owner's update. All six
root Markdown files and twenty technical documents were fetched and compared.
See [the reread evidence and its limits](research/SPEC_REVIEW_0.2.1.md).

## Historical alpha.2 — Google Drive authorization follow-up

The owner explicitly asked for the missing access request, then confirmed no
Google OAuth Desktop client exists. No personal Google authorization was
performed. The correct LedgeSync Drive specs (06 and 09) were reread through
the connected Drive; the older supplied folder resolves to an unrelated project
and was not used as LedgeSync requirements.

Changed implementation: `internal/driveauth/**`, `internal/credentialvault/**`,
`internal/connections/**`, `internal/systembrowser/**`, desktop bridge/main and
frontend Connections UI. The core is an independent implementation after the
pinned upstream OAuth source review. Native vault dependencies are pinned and
reviewed; distribution notices cover 21 runtime modules and 44 license hashes.

Implemented: native Desktop-client JSON picker/import; system-browser OAuth
with PKCE S256/state; bounded loopback callback, cancel/timeout and fixed HTTPS
endpoints; exact `drive.file` scope; read-only account identity; native vault
refresh-token persistence; process-only access tokens; bounded renewal;
account mismatch/revocation handling; explicit local disconnect. Frontend DTOs
contain only safe status and identity. Import buffers are cleared after use.
Launcher output/errors are discarded and OS browser handoff is bounded.

The service serializes operations within one application process. It does not
claim a cross-process credential transaction or multi-instance refresh lock;
use one running app instance. Root-pair locks for future transfers remain open.
No cloud file operations or credential access in headless CLI were added.

Local checks observed on macOS ARM64:

- `go test ./...`, `go test -race ./...`, `go vet ./...`: passed. Synthetic OAuth
  covers PKCE/state, denial, timeout/cancel, response validation/redaction,
  refresh rotation, account mismatch and vault failures. Native vault lifecycle
  is opt-in only on disposable CI users; ordinary tests access no personal vault.
- `npm run build` and Playwright: **23 passed**, including 19 new authorization
  cases and four existing explorer tests. Wails errors are simulated as rejected
  promises; failed operations reconcile persisted safe state, not stale badges.
- Native Wails `darwin/arm64` build with macOS 13 flags: passed. Real native
  WebView displayed Connections and Setup required, opened the native client
  picker and returned unchanged after Cancel. No JSON imported or vault item
  written; no Google browser authorization started. Test app closed afterward.
- `python3 tools/collect_licenses.py`, `python3 tools/verify_licenses.py`: passed
  after new dependency pins. Four DMG integrity/no-overwrite tests passed.
- `.venv/bin/python tools/validate_docs.py`: zero failures and zero skipped
  checks, including both schemas and all five examples. The system interpreter
  lacked jsonschema; the existing project virtual environment supplies it.

[Build run 36949133758](https://github.com/alexandroit/LedgeSync/actions/runs/36949133758)
passed all 16 jobs at application source
`42474b558d3b1557318f9cb0ae714748869f90b3`.
[Vault run 36949135946](https://github.com/alexandroit/LedgeSync/actions/runs/36949135946)
passed actual synthetic create/read/update/delete on all six native OS/architecture
runners. The public alpha.2 tag and 24 initial asset digests were verified;
[OAUTH_ALPHA_RELEASE.json](research/OAUTH_ALPHA_RELEASE.json) records all twelve
archives and two DMGs, their architecture/Go metadata, 44 license hashes and
exact native bundle matching. DMG packaging commit
`b4e3d7261776309648f203c29119bf859ca49695` changes installation copy only;
application bytes remain those of the successful native builds. Installer and
public APT verification evidence is recorded below. Alpha.1 artifacts
remain immutable. Setup instructions are
in [GOOGLE_DRIVE_AUTH.md](GOOGLE_DRIVE_AUTH.md), protocol/source decisions in
[OAUTH_SOURCE_REVIEW.md](research/OAUTH_SOURCE_REVIEW.md) and vault review in
[OAUTH_VAULT_REVIEW.md](research/OAUTH_VAULT_REVIEW.md).

The first alpha.2 installer run, 36949766324, passed both Windows installer
lifecycles and both native Ubuntu package tests, but its local APT job failed
because the repository builder still required alpha.1. The builder now requires
an explicit Debian version and checks all four package identities before signing;
four regression tests cover this boundary. Fix commit:
`ad2cddfbea493590609d490b0dee10435a9be2d0`. The failed run is not treated as
an all-platform success; a new full installer run validates the correction.

Final packaging/deployment evidence:

- [Installer run 36950139047](https://github.com/alexandroit/LedgeSync/actions/runs/36950139047)
  passed all five required jobs at `ad2cddfbea493590609d490b0dee10435a9be2d0`.
  Both Windows wizards/install/reinstall/remove flows and both Ubuntu native
  package checks passed; local signed APT installation passed. Four repository
  input-version regression tests passed, alongside ten Debian and four Windows
  packaging guards. See [installer evidence](research/OAUTH_INSTALLERS_RELEASE.json).
- [Public APT run 36950554723](https://github.com/alexandroit/LedgeSync/actions/runs/36950554723)
  passed on Ubuntu 24.04 amd64 and arm64. Active snapshot is
  `20261002-alpha2-ad2cddf`, Debian version `0.1.0~alpha.2-1`, with the existing
  signing key unchanged. All twelve prior pool/by-hash files are retained.
  No application or OS package was installed on the production server.
- Public release contains 37 verified assets; all 24 initial alpha.2 assets and
  all 32 alpha.1 assets retain their original IDs, bytes, sizes and timestamps.
  [Public asset evidence](research/OAUTH_PUBLIC_ASSETS.json) records the hashes.
- Canonical Ubuntu website and secondary Pages both serve source
  `82b2f6bf05e49d7e8f25c9f5e06e2db105d25636`. Public and origin HTML/CSS hashes
  match tracked content. [Pages run 36950632081](https://github.com/alexandroit/LedgeSync/actions/runs/36950632081)
  passed. HiperMusicas public/origin remain HTTP 200, PID 1521428 unchanged;
  shared Nginx hashes match the pre-deployment baseline. No Nginx reload was
  required for these content switches. See [deployment evidence](research/OAUTH_DEPLOYMENT_VERIFICATION.json).

Unrun external acceptance: actual Google consent, renewal/revocation against a
real account, publisher signing/notarization, and cloud file transfers. No
client ID, secret, token or private account fixture is committed or released.

## Historical alpha.1 commands and observed local results

Executed on macOS ARM64 with Go 1.27.1:

- `LEDGESYNC_GIT_EXPECT_VERSION='git version 2.55.0' LEDGESYNC_RCLONE_REFERENCE=<absolute-reference-binary> go test -race -json ./...`: **184 passing test/subtest events**, eight tested packages, zero failures and no skipped test cases. The policy/fake packages have no standalone tests and are exercised by integration suites.
- Product fixture corpus: **28 Git + 12 rclone + 10 composition** cases passed. Actual Git 2.55.0 differential: **35 cases**. Actual pinned rclone v1.75.1 local-only reference: **18 file/directory cases**. These are finite tested cases, not universal grammar compatibility.
- `go vet ./...`: passed.
- Four parser/path fuzz targets, five seconds each: Git **22,011**, rclone **11,929**, config **967**, path **314,342** executions, all passed. This is bounded fuzzing, not exhaustive proof.
- CLI validation/browse/explain/plan/inspect on synthetic fixtures with an empty runtime PATH: all exit 0; source contents unchanged; emitted plan validated against the JSON schema. No installed Git/rclone was needed by the product.
- `go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...` and the same command with `-tags desktop`: no reachable known vulnerabilities reported at the run date.
- `npm ci`, `npm run build`, `npm test` in frontend: TypeScript/Vite build passed; four interaction tests passed. Tests cover actual core selection, provenance, navigation, keyboard entry, list/grid, search, preview filters, stale-view clearing, refresh after inventory shrink and untrusted filename rendering.
- Native macOS ARM64 Wails build and actual WebView smoke passed: native picker, nine-entry synthetic fixture, excluded visibility and provenance, paired preview with nine operations/88 bytes/zero deletions. Bundle identity `com.ledgesync.app`, version 0.1.0 and macOS 13 minimum were verified in plist and Mach-O. This is an ad-hoc-signed developer build without a trusted publisher certificate or notarization. Other platform results are recorded separately after CI.
- `python3 tools/package_cli.py --version 0.1.0-alpha.1`: six portable CLI archives built; all archive structures, SHA-256 values and required notice files verified. The extracted macOS ARM64 CLI reported the correct version. Cross-compilation is not execution on the other systems.
- `python3 tools/collect_licenses.py` and `python3 tools/verify_licenses.py`: six target import graphs inspected; 19 runtime Go modules and 42 recorded notice hashes verified. This proves license inventory loading/integrity; the native build results are recorded below.
- Documentation/schema/fixture structure validation passed with zero failures and zero skipped checks. These structural checks are separate from product tests.

The isolated development reference can be rebuilt using
`tools/build_rclone_reference.py` and a clean external checkout at the audited
full SHA. Its binary and temporary credentials-free fixture environment are
never included in product archives. Core differential tests explicitly skip
this oracle if the environment variable is omitted; the dedicated CI reference
job supplies it.

## Review findings resolved

Regression fixes cover Git character classes crossing `/`, source volume/file
identity, duplicate destination names/operation IDs, planner cancellation,
strict plan decoding, and source-output case aliases/parent identity races.
The scan and plan are read-only. There is no basis for claiming that future
upload, overwrite, recovery or deletion paths have been tested.

## Historical alpha.1 — Public delivery and platform evidence

The public repository is <https://github.com/alexandroit/LedgeSync>, licensed
Apache-2.0 with retained third-party notices. The canonical public site is
<https://ledgesync.com/> on the owner's Ubuntu server. It returns HTTP 200 with
the matching stylesheet and valid HTTPS. GitHub Pages remains a secondary copy
at <https://alexandroit.github.io/LedgeSync/>. See [platforms](PLATFORMS.md) and
[website deployment](WEBSITE.md). The separate Sites preview remains private.

[Release v0.1.0-alpha.1](https://github.com/alexandroit/LedgeSync/releases/tag/v0.1.0-alpha.1)
is public and marked prerelease. Its immutable source commit is
`89a9121a279c843f77b2d72f8b6e93dc332cb03b`.
[CI run 36942481310](https://github.com/alexandroit/LedgeSync/actions/runs/36942481310)
passed **all 16 jobs**: seven native core runners, six native desktop builds,
frontend interactions, contracts/license integrity and pinned rclone reference.
Windows ARM64 omits the unavailable race detector; platform-specific filename,
permission, case-sensitivity and symlink-privilege cases have explicit skips.

All twelve CI archives were downloaded and checked for Mach-O/ELF/PE
architecture, Go 1.27.1 metadata/module identity, required licenses and recorded
notice hashes. Their checksums matched the CI package manifests. Release tag,
all fourteen uploaded asset digests/sizes, and anonymous HTTP 200 checksum
download were then verified through GitHub. See
[release evidence](research/OFFLINE_ALPHA_RELEASE.json).

Only macOS ARM64 received a real desktop GUI smoke test in this session.
Other native desktop compilation and core test results do not establish clean
installation, accessibility or GUI runtime acceptance on those systems.

Source audit and documentation-only historical results remain in document 18.
They must not overwrite the actual implementation status above.

## Historical alpha.1 — Website deployment follow-up

The initial website source deployed to Ubuntu was
`92eb75b51d978565e1dd8ef939d6cf6320da2299`. Changed files: `dist/index.html`
(canonical URL), `deploy/nginx/ledgesync.conf` (isolated static vhost), `README.md`,
`docs/WEBSITE.md`, `docs/12_ADR_DECISIONS.md`,
`docs/15_RISKS_AND_OPEN_DECISIONS.md` and this handoff. The GitHub repository
homepage now points to the canonical website. No application or alpha release
artifact was changed.

Verified commands and outcomes:

- `sudo nginx -t`: passed before each graceful reload.
- `curl --silent --show-error --dump-header - https://ledgesync.com/` and the
  equivalent stylesheet request: HTTP 200; bodies match tracked SHA-256 hashes.
- Direct origin `curl --resolve ledgesync.com:443:127.0.0.1 https://ledgesync.com/`
  on the server: HTTP 200 with certificate validation enabled.
- HTTP and www requests: redirect to canonical HTTPS; missing path 404 and
  `.git/config` 403. Direct origin www redirect preserves path/query.
- `openssl x509 -in /etc/letsencrypt/live/ledgesync.com/cert.pem -noout -dates
  -ext subjectAltName`: both names covered; expires December 30, 2026.
- `systemctl is-active nginx certbot.timer`: both active. Existing HiperMusicas
  public and origin requests return 200, its Supervisor PID is unchanged, and
  both shared Nginx configuration hashes match the pre-deployment baseline.
- `sudo certbot renew --cert-name ledgesync.com --dry-run --non-interactive
  --run-deploy-hooks`: simulated renewal passed, including Nginx validation
  and reload. This did not replace the live certificate.

The first immediate HTTP probe after the bootstrap reload returned an empty
reply; the subsequent origin probe and all final HTTPS probes succeeded.
Browser visual inspection was unavailable because the computer-use connector
reported no browser. Website-only changes did not rerun application CI; the
immutable alpha retains the existing 16-job validation above. Deployment,
renewal, update and rollback procedures are in [WEBSITE.md](WEBSITE.md).

## Historical alpha.1 — Graphical app and macOS disk-image delivery

The owner requested the missing graphical download and `.dmg`. Two disk images
were added to the existing `v0.1.0-alpha.1` release: Apple Silicon ARM64 and Intel
x64. They contain the exact apps extracted from the original, checksum-verified
release archives; all 14 original release assets, including their IDs, digests,
sizes and timestamps, were verified unchanged. Each DMG has a separate checksum
and the additional `DMG_RELEASE.json` records its provenance. See
[the packaging evidence](research/MACOS_DMG_RELEASE.json).

Packaging commit: `06ca1e2749e4b7ceda5fa3703495051557e7173a`. Added
`tools/package_dmg.py` and `tools/test_package_dmg.py`; macOS CI now creates and
verifies DMGs alongside its archives. Updated README, platform instructions,
release notes and website downloads. At this stage the Ubuntu site served
`188ba5de5c76b5562b6b7c6afa8e14f4a20ad45f`, with matching HTML/versioned CSS
and both direct DMG links returning HTTP 200. The previous static release is
retained for rollback; HiperMusicas remains available.

Observed checks:

- `python3 tools/test_package_dmg.py`: four integrity/no-overwrite tests passed.
- `python3 tools/package_dmg.py --app <verified-release>/LedgeSync.app --arch
  <arm64|amd64> --version 0.1.0-alpha.1 --output build/dmg-release/packages
  --notices-root <verified-release>`: both packages passed. This verifies the
  disk image, read-only mount, full app bytes/modes/symlinks, Mach-O architecture,
  strict ad-hoc signature, Applications shortcut, instructions and notices.
- The first verification mount under the external synced SSD was denied by
  macOS. Moving only the temporary mount to the local `/tmp` filesystem resolved
  it; image publication remains atomic and refuses existing output names.
- Anonymous downloads of all five new assets matched the local bytes and GitHub
  digests. No `--clobber`, tag replacement or original manifest rewrite was used.
- The ARM64 graphical app from the public archive was opened through the native
  application connector and its WebView rendered the explorer start screen.
  The folder picker opened, but additional picker automation did not complete;
  the test app was closed. Earlier native GUI behavior evidence still applies.
- Local documentation validation: zero failures, one optional JSON Schema check
  skipped because that interpreter lacks `jsonschema`. License verification
  passed for all 42 recorded notice hashes.
- [CI run 36944925521](https://github.com/alexandroit/LedgeSync/actions/runs/36944925521)
  passed all 16 jobs at packaging commit `06ca1e2749e4b7ceda5fa3703495051557e7173a`,
  including creation and mounted-content verification of DMGs on native macOS
  ARM64 and Intel runners. The contracts job includes the new integrity guards.
- [Pages run 36945114075](https://github.com/alexandroit/LedgeSync/actions/runs/36945114075)
  passed for website commit `188ba5de5c76b5562b6b7c6afa8e14f4a20ad45f`;
  public secondary HTML and CSS match the same tracked source.

Both DMGs preserve the alpha's existing ad-hoc signatures. Developer ID signing
and notarization are absent; downloaded-app Gatekeeper acceptance and a clean
Intel installation were not established. The app remains an offline developer
alpha. Packaging does not implement Drive connections, transfers or scheduling.

## Historical next work after alpha.1

Finish owner-run live OAuth acceptance, then prioritize persistent
state/migration/locking and remaining policy/provider contracts before transfers.
Use fake providers and temporary roots for development. Real accounts and
mutations require the corresponding explicit authorization. Keep unsupported
or ambiguous capabilities disabled.

At the next handoff record changed files, exact commands/results, failed or
unrun checks, remaining limits and the next safe step. Never infer successful
installation, signing, GUI runtime, live transfer or full milestone completion
from a build or simulated destination.

## Historical alpha.1 — Windows installers and Ubuntu APT delivery

The owner required the Windows EXE to open an installation wizard and Ubuntu
to support `apt-get install ledgesync`. This packaging follow-up is complete;
it does not change the offline application or enable cloud synchronization.

- Windows x64 and ARM64 setup EXEs are published in the existing alpha release.
  They use pinned Inno Setup 7.1.0, retain the exact verified original desktop
  payload/notices, install for the current user, create a Start menu shortcut,
  optionally create a desktop shortcut and register an uninstaller. Missing
  WebView2 is detected with Microsoft's official prerequisite address. No
  automatic prerequisite download, service or startup task was added.
- Ubuntu 24.04 x64 and ARM64 have `ledgesync` graphical and `ledgesync-cli`
  headless packages at Debian version `0.1.0~alpha.1-1`. They retain the original
  app/CLI bytes and notices. Native `dpkg-shlibdeps` derives graphical runtime
  dependencies; the static CLI has no graphical dependencies.
- The signed repository at `https://ledgesync.com/apt` is live on the existing
  Ubuntu origin. Users register its scoped key/source once, then use APT.
  The key fingerprint is `11B35F4E066806C33AA8653A51AD694F4729F5AB`. Its private
  key remains root-only on the server; only the public key is committed.
  Initial snapshot: `20261002-alpha1-e796bc5`. See [APT operations](APT_REPOSITORY.md).
- The GitHub release now has 32 assets. All 19 previously published assets retain
  the same IDs, digests, sizes and timestamps; tag/source remains
  `89a9121a279c843f77b2d72f8b6e93dc332cb03b`. All 13 new anonymous downloads match
  their GitHub/local digests. GitHub normalized Debian filename tildes to dots;
  the new checksum/manifest metadata was corrected to match those names before
  final verification. No installer or Debian payload was replaced.

Evidence and limits:

- [Ubuntu package run 36946272426](https://github.com/alexandroit/LedgeSync/actions/runs/36946272426):
  both native Ubuntu jobs and the local signed APT lifecycle job passed. Overall
  initial run failed due to Windows packaging; do not call it an all-platform pass.
  Ubuntu checks cover byte/notice integrity, real installation/removal and eight
  seconds of GUI process startup under Xvfb, not interactive GUI acceptance.
- [Windows run 36946941074](https://github.com/alexandroit/LedgeSync/actions/runs/36946941074)
  at `8bcb6966030047df483762cc29fbba7c94a4e92a`: both native jobs passed.
  x64 used Windows Server 2022; ARM64 used Windows 11. Both exercised actual
  wizard welcome/Next/cancel, silent install, exact payload, registration,
  default/optional shortcuts, same-version reinstall and uninstall preserving
  synthetic user data. Full app interaction, x64 Windows 11 execution,
  cross-version migration and SmartScreen acceptance are not claimed.
- [Public APT run 36946856179](https://github.com/alexandroit/LedgeSync/actions/runs/36946856179)
  at `55bc82685eaf42c357be26609fbf11569a9abec2`: both native Ubuntu jobs passed.
  Verified the pinned key/source, signed metadata, rejection of tampered metadata,
  by-hash acquisition, `apt-get install ledgesync`, CLI-only installation and
  removal preserving synthetic user data. Public package bytes independently
  match all four verified CI packages. The native curl/APT clients work without
  changing Cloudflare protections; the original urllib probe received error 1010.
- Packaging guards: four Windows and eight Debian checks passed. PowerShell
  syntax passed. Documentation validation reports zero failures; its optional
  JSON Schema dependency is absent locally, so that separate check is skipped.
- [Installer provenance](research/INSTALLERS_RELEASE.json) and
  [public verification](research/INSTALLERS_PUBLIC_VERIFICATION.json) retain
  source/archive/package identities and the bounded test results.

The live website now serves source
`2890e90f028c6ee038739b09423b372668e20a00`, with Windows setup links and Ubuntu
commands. Public HTML/versioned CSS/key/source bytes match tracked files; origin
HTTPS and public/origin HiperMusicas return 200. HiperMusicas still has PID 1521428
and shared Nginx configuration hashes are unchanged. The previous website
release remains available for rollback. No app was installed on the production
server as a lifecycle test. Main workflows now offer Windows/Ubuntu targets and
separate public APT verification; failed Windows runs retain diagnostics.

At that alpha.1 checkpoint, product work and signing limitations remained:
offline preview only, no Drive account/transfer, no cross-version installer migration claim,
no trusted Windows publisher signature, no macOS notarization, and no full
Windows/Linux graphical application acceptance.

The secondary GitHub Pages copy also matches the new HTML and CSS after
[Pages run 36947337099](https://github.com/alexandroit/LedgeSync/actions/runs/36947337099)
passed. Production remains the owner's Ubuntu origin. Temporary server upload
directories were removed; live/rollback releases and signed APT snapshots remain.

## Current handoff

**Immediate next work:** reproduce and fix the owner's non-working file transfer
in the installed native GUI, following [the Claude Code handoff](../CLAUDE_CODE_HANDOFF.md).
Distinguish manual upload, the separate offline simulation and unimplemented
watch/schedule behavior without assuming which explains the report. Add typed,
redacted diagnostics and a regression for the actual failure before expanding
features. Finish with independent real Drive acceptance, not another build claim.

The previous publication checkpoint follows for provenance, not as a resolution
of this newly reported failure.


The alpha.4 application/tag source is `fcd578488d07f627372e7f5dd2221e162634bf05`:
all 16 jobs in build `36963525743` and all six native vault jobs in `36963524884`
passed. Desktop and interactive native CLI now implement explicitly approved
Google Drive folder copies using one shared engine. Platform-native vaults,
journal access checks and cross-process credential locks are implemented and
covered by the final release checks described above.

**Application and installer publication is verified:** all 37 assets passed
anonymous byte checks. Installer run `36964667348` passed all five required jobs
at packaging source `13342144685824daa38c774cb3ef7bdf14e315d3`. Prior release
identities remain unchanged. APT snapshot `20261002-alpha4-fcd5784` is activated,
and public installation run `36965018881` passed on both architectures. Website
and Pages source `6c8f1d19f8c2aa398d98d0a5b39ba5a1999772d1` passed 21 exact-byte
checks; Pages run `36965146423` succeeded. Shared server configuration/service
checks passed, with no Nginx reload and prior rollback directories retained.
[Platform release results](PLATFORMS.md#alpha4-native-validation-and-publication-gates)
link the four durable application, installer, public-asset and deployment records.

The owner reports successful account connection and Production OAuth/Picker
configuration. Independent live Google Picker/copy/recovery and SSH consent
acceptance remain unverified. Remote revocation needs its own deliberate approval
because it can affect other clients in the same Google Cloud project. Trusted
Apple publisher signing/notarization and Windows Authenticode remain unavailable;
no iOS package or acceptance is claimed. No watcher, schedule, unattended apply,
remote overwrite/deletion or source modification is enabled.

## Documentation continuation after repository relocation — 2026-10-02

Task DOC-CONTINUE-01 records the owner's request for a Claude Code completion
handoff. This documentation task does not implement or claim an application fix.
The repository root is now resolved as `.` in the active LedgeSync checkout;
no workstation-specific path is required. The inspected baseline was
`6f0200dd68af24c915b75e9b3b55ca035a12af91`, with all 320 tracked files present.
Generated local dependencies, builds and private OAuth build input were absent;
they must be prepared through the existing workflow, not copied into source.

[The single handoff](../CLAUDE_CODE_HANDOFF.md) now carries the failure report,
current implementation/evidence, portable workspace rules, reproduction order,
shared service map, known configuration/GUI/automation/adapter gaps, native
security constraints, permission/release boundaries and explicit completion gates.
`CLAUDE.md`, `START_HERE.md`, `CODEX_CLAUDE_BOOTSTRAP.md` and `AGENTS.md` route
agents to that current work. Current README/auth/workflow/decision text no longer
presents obsolete offline-only status as the active implementation.

Markdown workstation/cache/example paths were normalized to relative references
or runtime-derived roots. Historical command spellings are marked as portable
transcriptions; recorded outcomes, source identities and immutable JSON evidence
are retained. Real OS/server installation destinations remain explicit technical
locations, not misplaced references to this checkout.

Only Markdown was changed. No runtime source, credentials, private journals,
release assets, server state or Google data were changed. No application build,
live authorization/copy, watcher, commit, push or deployment was performed.
Validation for this documentation task:

- `python3 -B tools/validate_docs.py`: 53 Markdown files, 279 local links,
  40 JSON files, 50 filter fixtures and 32 safety scenarios checked; zero failures.
  JSON Schema validation was skipped because `jsonschema` is not installed
  (one skip). This is not proof that schemas passed.
- `python3 -B tools/verify_licenses.py`: all 63 recorded notice hashes passed.
- `git diff --check`: passed.
- Markdown scans for old workstation roots and absolute checkout/example paths:
  no remaining matches. Actual OS/server installation destinations are retained
  under the path convention above.

Runtime tests, application builds and live provider checks were not run for this
documentation-only change. The next coding agent must establish its own test
and real provider evidence for the fix and subsequent completion work.
