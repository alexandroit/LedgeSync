# Continuation handoff — Finish LedgeSync from the current checkout

**Updated:** 2026-10-02, America/Toronto. **Product:** LedgeSync.
**Priority:** the owner reports that the application is not synchronizing files.
**Status:** the failure was reproduced and fixed. Alpha.5 fixed four defects
found with an emulator. The live Google acceptance then found two upload
defects that alpha.5 still had (Google's `session_crd` session parameter, and
Drive-detected media types). With those fixes, alpha.6 passes all 14 live
steps and is published (release, installers, signed APT, website). The owner
then asked for Google Drive-like behavior. Alpha.7 adds two-way sync with full
Drive access ([ADR-034](docs/12_ADR_DECISIONS.md),
[two-way sync](docs/research/TWO_WAY_SYNC.md)). Publisher signing awaits owner
steps. Current
dispositions, commands and blockers are in
[current status](docs/17_AGENT_HANDOFF_AND_STATUS.md); the original brief below
is retained for context.

This is the single continuation entrypoint. Read [project identity](PROJECT_IDENTITY.md)
and [shared engineering rules](AGENTS.md), then follow this document. Work in the
existing application; do not recreate the project, rename it, replace its architecture,
or deliver another offline demonstration as the finished product.

## 1. Current request and evidence boundary

The owner moved this repository and asks the coding agent to finish the application.
Their latest functional report is: **the application is not synchronizing files**.
That report takes precedence over earlier summaries calling the alpha functional.
Source code for manual copying exists, but its presence, a green CI run, an OAuth
connection, and published installers do not prove that the installed app uploads.

The immediate objective is to reproduce and fix the actual failure, then complete
the required desktop/server workflows and validate them with real, authorized
Google Drive acceptance. Do not assume the cause is an old installed version,
missing approval, OAuth configuration, the repository move, or an expectation of
automatic watching. Those are hypotheses to distinguish with evidence.

This handoff was prepared by editing documentation only. No application fix,
new build, live Google test, credential operation, commit, push or deployment was
performed as part of its preparation.

## 2. Workspace and portable path convention

- **Repository root is `.`**, the directory containing this document, `go.mod`,
  `PROJECT_IDENTITY.md`, `cmd/`, `internal/`, `frontend/` and `docs/`.
- Open the relocated **LedgeSync** folder under the owner's selected `github`
  directory. Resolve its location from the active workspace; do not search old
  Drive project folders or embed a workstation's volume/user path in new files.
- Shell examples below run from the repository root unless stated otherwise.
  Markdown links resolve relative to the Markdown file containing them.
- Use `./build/` for ignored generated output and `../reference-sources/rclone`
  as a suggested external reference checkout. That reference directory is not
  asserted to exist. Verify its pinned identity before using it.
- Some tools require absolute runtime paths. Derive those from the active root,
  for example `LEDGESYNC_ROOT="$(pwd -P)"`; never hardcode where this Mac mounted it.
- Actual OS install destinations and remote server paths in platform/deployment
  runbooks are technical locations, not repository references. Do not change
  them to checkout-relative destinations or move production data with this repo.
- Preserve immutable historical JSON release evidence. Portable command
  transcriptions in Markdown do not mean the recorded tests were rerun here.

At inspection, `main` was at `6f0200dd68af24c915b75e9b3b55ca035a12af91`, with
320 tracked files present and a clean working tree. This handoff and related
Markdown updates are subsequent local changes: inspect and preserve them.
The observed origin is `git@github.com:alexandroit/LedgeSync.git`.
Remote freshness and credentials must be checked separately.

The moved checkout does not contain ignored `.venv/`, `frontend/node_modules/`,
frontend build output, generated Wails bindings, `build/` artifacts, or the generated
publisher OAuth source. These are development inputs/output, not lost tracked code.
Do not mistake missing generated output for a reason to rebuild the product from scratch.

## 3. What exists, and what has not been proved

| Area | Observed source or recorded evidence | Remaining boundary |
|---|---|---|
| Desktop/CLI | Go core, TypeScript/Wails GUI, shared interactive Drive-copy service | Owner reports files do not synchronize; reproduce installed behavior |
| Authorization | Browser OAuth, PKCE/state, bundled Desktop client build path, native folder Picker | Real current return/upload journey still needs acceptance |
| Copy | Included hierarchy/empty folders, preview/digest approval, ID journal, checksums, keep-both, cancellation/reconciliation | Independent live integrity, repeat and interruption tests remain pending |
| Credentials | Keychain, Credential Manager, Secret Service; no plaintext fallback | Native user-session availability and real server consent require testing |
| Source/state protection | Read-only sources, strict paths, journal/lock ACL checks, Linux bus peer UID, process locks | Preserve these guarantees while fixing functionality |
| Distribution | Recorded alpha.4 DMGs, Windows installers, Ubuntu packages/APT, CLI archives and site | These are historical distribution results, not proof of the reported workflow |
| Automatic operation | No active watcher, scheduler, service or automatic restart/resumption | Implement only through the explicit opt-in job contract |
| Mobile | Existing Apple builds target macOS | iPhone/iPad scope is unresolved; do not claim iOS support |

Recorded application/tag source: `fcd578488d07f627372e7f5dd2221e162634bf05`.
Packaging source: `13342144685824daa38c774cb3ef7bdf14e315d3`.
Website source: `6c8f1d19f8c2aa398d98d0a5b39ba5a1999772d1`.
Recorded native jobs: build/test `36963525743`, vault `36963524884`, installers
`36964667348`, public APT `36965018881`, Pages `36965146423`.
Inspect the [application](docs/research/DRIVE_COPY_RELEASE.json),
[installer](docs/research/DRIVE_COPY_INSTALLERS_RELEASE.json),
[public assets](docs/research/DRIVE_COPY_PUBLIC_ASSETS.json), and
[deployment](docs/research/DRIVE_COPY_DEPLOYMENT_VERIFICATION.json) records.
Do not turn those historical results into a claim that this checkout was rebuilt
or that the owner-visible failure has been resolved.

## 4. First session: inspect, prepare, reproduce

1. Confirm this checkout, branch, diff and existing changes. Read the current
   [status](docs/17_AGENT_HANDOFF_AND_STATUS.md), [requirements](docs/01_PRODUCT_REQUIREMENTS.md),
   [filter rules](docs/04_FILTER_ENGINE_SPEC.md), [sync safety](docs/05_SYNC_SAFETY_AND_STATE.md),
   [provider contract](docs/06_GOOGLE_DRIVE_PROVIDER.md), and
   [backlog](docs/11_IMPLEMENTATION_BACKLOG.md). Use implemented code as evidence,
   not unmarked legacy descriptions of an offline-only product.
2. Verify local tool versions against `go.mod`, `frontend/package-lock.json`,
   [build instructions](docs/PLATFORMS.md#build-from-source) and the pinned
   [CI workflow](.github/workflows/ci.yml). Recreate local dependencies in isolated
   project directories; do not upgrade the toolchain/dependencies speculatively.
3. Reuse the completed [rclone audit](docs/research/RCLONE_SOURCE_AUDIT.md),
   [baseline](docs/research/UPSTREAM_BASELINE.json), and
   [reuse matrix](docs/research/RCLONE_REUSE_MATRIX.md). Recheck the actual upstream
   source when changing reused code; do not repeat P0 or create a competing app.
4. Identify the **installed executable/package** the owner is testing: OS, CPU,
   application location, version, package/binary SHA-256 and launch method.
   The displayed alpha.4 string alone is insufficient; multiple local candidates
   may share it. Compare with the relevant release manifest when applicable.
5. Capture the actual steps and sanitized failure: connection state, local source
   availability, destination choice, preview result, approval, transfer state and
   safe error code. Determine whether the problem is initial copy, a new file
   added after a completed run, restart recovery, or expected automatic watching.
   Obtain only missing facts; do not ask the owner to restate this document.
6. Reproduce with a disposable fixture, first through the GUI and then through
   the same shared service/CLI where useful. A real Drive mutation needs the
   intended account/destination and the corresponding owner authorization.
   Continue local/fake-provider diagnosis while any real-account input is pending.

Missing publisher configuration must not produce a new end-user JSON-import
flow. Follow [maintainer OAuth setup](docs/OAUTH_BUILD.md); locate the previously
authorized Desktop configuration privately or verify the existing CI secret
name `GOOGLE_DESKTOP_CLIENT_JSON` without printing its value. Never embed an
owner's tokens, copy client JSON into tracked docs, weaken scopes, or expose
credentials in browser/assistant output. A source-only build can deliberately
report authorization unavailable; distinguish it from an official configured build.

## 5. Diagnose the upload before adding features

The intended manual sequence is:

**Choose local folder → Connect Google Drive → choose My Drive/existing parent →
Preview folder upload → review → Upload folder → verified result.**

Connecting an account or choosing a local folder does not currently start an
upload. A file added later needs another preview/approval. This explains current
design boundaries; it does not establish the cause of the owner's failure.

| Boundary | Inspect |
|---|---|
| UI events and real versus simulated preview | [frontend/src/main.ts](frontend/src/main.ts): `chooseDestination`, `previewDriveUpload`, `renderPreview`, `StartDriveUpload` call |
| Typed GUI/Go transport | [drive_transfer.go](internal/transport/desktop/drive_transfer.go), [google_drive.go](internal/transport/desktop/google_drive.go), [app.go](internal/transport/desktop/app.go) |
| Account/client/native vault | [connections](internal/connections/google.go), [driveauth service](internal/driveauth/service.go), [credentialvault](internal/credentialvault/vault.go) |
| Preview, plan identity and start | [transfer/service.go](internal/transfer/service.go): `Preview`, `Start`, `SetDestination` |
| Execution and source/destination revalidation | [transfer/execute.go](internal/transfer/execute.go): `execute`, `guard`, `verify` |
| Native API/resumable transport | [drive/client.go](internal/providers/drive/client.go), [drive/upload.go](internal/providers/drive/upload.go) |
| Durable mappings and run outcomes | [transferstate/store.go](internal/transferstate/store.go) |
| Server/CLI parity | [cmd/ledgesync/online.go](cmd/ledgesync/online.go) |

The UI still has a separate **simulated empty-destination preview** when a real
upload plan is absent. Inspect the screen actually shown and make the distinction
clear; do not remove safety approval or claim an empty simulation is a real remote scan.

Current destination/preview/start catches replace backend distinctions with generic
messages. Transfer status lacks a typed error code, and failed previews return to
a generic idle state. Add actionable, sanitized error DTOs and consistent run/UI
states as needed to diagnose the failure. Test redaction. Do not surface raw HTTP
bodies, OAuth callback data, tokens, upload-session URLs or personal absolute paths.

Write a regression for the reproduced failing layer, fix the smallest coherent
root cause, then prove the complete GUI journey. Do not close the issue merely
because a fake provider, CLI-only test, connection status or process launch passes.

## 6. Ordered completion work

| Order | Work | Acceptance before calling it done |
|---|---|---|
| A | Fix the reported first-copy/incremental-copy failure and safe diagnostics | Authorized real parent contains the included root, hierarchy and empty folders; independently downloaded fixture hashes match; source unchanged; excluded files absent |
| B | Honor configuration semantics | `conflictPolicy: pause` really pauses or is explicitly rejected before mutation; `maxRetries` controls bounded retries; configured transfer limits are honored; unsupported settings never silently acquire different semantics |
| C | Durable project setup and primary GUI | Saved local/remote/account bindings, clear reopen workflow, configurable policy editor, authorized cloud/paired view, useful Activity/History/Settings driven by shared services and journal data |
| D | Incremental repeat, recovery and diagnostics | Unchanged repeat makes no extra copies; changed files follow chosen policy; cancellation/restart/network ambiguity reconcile IDs; bounded, redacted per-file/run results are inspectable |
| E | Opt-in automatic copy | Saved jobs, explicit preauthorization and visible pause/disable; watch/interval jobs bind account/source/destination/config/policies and respect sleep/offline/missing volumes; overflow or changed rules force reconciliation/reapproval |
| F | Required policy adapters | Major VCS/code-tool scope mapped to U10/F23/F24, with real per-adapter semantics and differential fixtures; optional missing capabilities fail closed, including actual SVN properties |
| G | Native/server reliability and release security | GUI and CLI share behavior; native vault/permissions remain enforced; real SSH return/session tests; tested macOS/Windows/Ubuntu artifacts and required publisher signing |
| H | Remaining initial-release recovery scope | Restore-to-new-location and any managed overwrite/mirror satisfy P4 recovery/deletion gates; otherwise keep them explicitly disabled with an accepted, documented scope decision |

Confirmed code gaps to address in B/C/E:

- [Config validation](internal/config/config.go) accepts `pause`, but the
  [transfer planner](internal/transfer/service.go) unconditionally plans keep-both
  for changed known paths. Do not confuse the preview's explicit approval with
  support for the configured conflict policy.
- `maxRetries` is accepted but the Drive provider uses a fixed `maxAttempts = 4`.
  `maxTransfers` is accepted but execution is serial. Serial execution does not
  exceed the upper bound; configurable concurrency is nevertheless unimplemented.
- The selected GUI source, destination and pending approval live in memory.
  The durable journal is not a saved-project catalog. Restart currently requires
  source/destination reselection and a new approval; never persist reusable stale approval.
- Activity, History & Recovery and Settings are disabled placeholders. Policies
  currently presents capabilities rather than a full editor.
- [Policy capabilities](internal/policy/policy.go) implement only Git/rclone.
  Do not silently map Mercurial, SVN, Perforce, CVS, Bazaar/Breezy, Fossil or other
  dialects to Gitignore, or label configuration-schema support as runtime support.
- No scheduler/watcher is implemented. Build automation only after manual copy
  and durable projects are correct; never enable startup jobs by installation.

[Product requirements](docs/01_PRODUCT_REQUIREMENTS.md) describe P4 as the full
initial release; [release gates](docs/13_RELEASE_AND_OPERATIONS.md) allow unfinished
destructive features to remain explicitly disabled. Resolve this scope boundary
in the ADR/status record. Do not silently call the whole roadmap complete or add
multi-cloud, bidirectional sync, crypt, mount, serve/API, billing or an AI service.
If the owner meant iPhone/iPad by “iOS,” obtain that platform decision while
continuing desktop work; a macOS DMG is not an iOS deliverable.

## 7. Non-negotiable safety and permission boundaries

- Preserve read-only local sources, limited `drive.file`, native browser OAuth,
  PKCE/state, account binding, expiring exact approvals and journal-before-mutation.
- Preserve Keychain, Credential Manager and Secret Service. Unavailable/locked
  vaults must block online use; do not add plaintext credentials, empty-password
  keyrings, permissive ACLs or a central token database to make a test pass.
- Preserve native journal/lock protection, Linux peer-UID checks, source containment,
  missing-rule detection and cross-process OAuth/transfer coordination.
- Never disable OS security, broaden Google scopes, revoke grants, delete remote
  files, clear the user's journal or replace source data as a troubleshooting shortcut.
- Routine local implementation/fake tests can continue without repeated permission
  requests. Real account/fixture access, cloud writes, automation and publication
  must remain within the corresponding authorization already given by the owner.
  This handoff creates no new authorization for unrelated accounts, data or services.
- Existing GitHub/site/APT publishing and the Ubuntu host were owner-authorized
  previously. Verify current connectivity and applicable scope before new external
  writes; do not ask again merely because a routine authorized step is reversible.
- Keep old tags, release assets and APT pool bytes immutable. A changed binary needs
  a new version. Never use the old alpha.4 name to disguise a rebuilt candidate.
- Website hosting shares an Ubuntu server with HiperMusicas. Follow the existing
  [site](docs/WEBSITE.md) and [APT](docs/APT_REPOSITORY.md) runbooks; do not modify
  shared services/DNS or deploy speculative fixes while diagnosing the desktop.

## 8. Build, test and acceptance evidence

Use the existing pinned workflows as the authoritative native build matrix.
The following are preparation/validation instructions for the coding agent; they were not
executed to produce new application results during this documentation handoff.

From the repository root, on a compatible Unix development host:

```sh
python3 -m venv .venv
.venv/bin/python -m pip install jsonschema==4.26.0
.venv/bin/python tools/validate_docs.py
.venv/bin/python tools/verify_licenses.py
```

For isolated Go build/test output (Go requires absolute cache/temp paths, derived
here from the current checkout):

```sh
LEDGESYNC_ROOT="$(pwd -P)"
mkdir -p ./build/cache/go-cache ./build/cache/go-mod ./build/cache/tmp
export GOCACHE="$LEDGESYNC_ROOT/build/cache/go-cache"
export GOMODCACHE="$LEDGESYNC_ROOT/build/cache/go-mod"
export GOTMPDIR="$LEDGESYNC_ROOT/build/cache/tmp"
export TMPDIR="$LEDGESYNC_ROOT/build/cache/tmp"
go test ./...
go test -race ./...
go vet ./...
```

Do not run the race detector on an unsupported target or mislabel cross-compilation
as native execution. Follow platform-specific build/OAuth instructions and
[native vault CI](.github/workflows/oauth-vault.yml), using synthetic vault entries.
Do not opt into tests against the developer's personal stored connection.

From `frontend/`:

```sh
npm ci
npm run build
npx playwright install chromium
npm test
```

For native desktop packaging, use the [platform guide](docs/PLATFORMS.md#build-from-source),
[OAuth build guide](docs/OAUTH_BUILD.md), and existing `tools/package_*` helpers.
Do not bypass required frontend generation, license inventories or bundled-client checks.

Relevant regressions already exist in
[upload_integration_test.go](internal/transfer/upload_integration_test.go),
[drive-upload.spec.ts](frontend/tests/drive-upload.spec.ts),
[Drive provider tests](internal/providers/drive/upload_test.go),
[OAuth tests](internal/driveauth/process_lock_test.go) and
[state tests](internal/transferstate/store_test.go). Keep tests for lost write
acknowledgements, unchanged repeats, missing rules across restart, and durable
run-finalization failure. Add tests for the actual bug and new configuration/UX behavior.

Complete the [live acceptance checklist](docs/research/DRIVE_UPLOAD_ACCEPTANCE.md)
using a disposable authorized fixture: nested/empty folders, ignored files,
zero-byte files, Unicode/spaces and a multi-chunk file. Record exact artifact
identity and independently compare downloaded content hashes. Exercise unchanged
repeat, changed content, cancellation/close/restart, network loss, changed rules,
remote alteration and CLI/SSH behavior. Never upload unrelated personal files to
prove a feature or publish unredacted fixture/account details.

## 9. Completion gates and handback

Do not stop at a plan, another MD, a mockup, a green build, or a new installer.
Deliver working code and evidence for the defined scope:

1. The owner's reported failure is reproduced/classified, fixed, regression-tested,
   and verified through the native GUI against an authorized real destination.
2. Repeated and interrupted operations preserve source/remote safety, integrity and
   rule semantics, with clear actionable errors and inspectable run outcomes.
3. The GUI/server/configuration and adapter requirements above have an explicit
   implemented/deferred/blocked disposition linked to tests, not vague checkmarks.
4. Each advertised OS/architecture has native runtime and installer acceptance.
   Server vault/session prerequisites are real; mobile is never inferred from Darwin.
5. Signatures/notarization, SBOM/notices, immutable release identity and approved
   distribution updates meet [release requirements](docs/13_RELEASE_AND_OPERATIONS.md).
   APT signing is not a substitute for Apple/Windows publisher signing. If owner
   certificates are unavailable, report that concrete external dependency.
6. New public downloads, APT installation and site links are independently verified
   when publishing is authorized. Recorded CI/package success must not substitute
   for the real provider journey.
7. Update [current status](docs/17_AGENT_HANDOFF_AND_STATUS.md), requirements/backlog
   dispositions and user guides. Report changed files, commands/results, skipped
   checks, real acceptance evidence and remaining owner-dependent items truthfully.

Start with sections 4 and 5 now. Preserve the existing implementation and solve
the reported non-working file transfer before treating release packaging as progress.
