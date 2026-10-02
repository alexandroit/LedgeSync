# 17 — Agent Handoff and Current Status

**Updated:** 2026-10-02. **Product:** LedgeSync. **Specification:** 0.2.1.
**Implementation:** 0.1.0-alpha.3, published one-click authorization developer alpha.

**Source follow-up:** OAuth protection hardening is implemented after alpha.3;
the published installers, APT packages, website and release tag are unchanged.

Read [PROJECT_IDENTITY.md](../PROJECT_IDENTITY.md) first. The authoritative name
is LedgeSync, command `ledgesync`, primary domain `ledgesync.com`. The owner
requested public Apache-2.0 source, a website and desktop/server platform builds.
After initially deferring DNS activation, the owner explicitly requested
publication on the existing Ubuntu server hosting HiperMusicas. The canonical
site is now live there. The owner subsequently requested Google Drive token
authorization, then selected a bundled Desktop client with one-click browser
consent. The current change does not enable cloud transfers.

## Public privacy, terms and branding publication

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

## OAuth protection verification and hardening (unreleased source)

The owner requested verification of existing protections and code fixes for
missing controls. Read [the requirement-by-requirement evidence and limits](research/OAUTH_SECURITY_HARDENING.md).
Existing PKCE/state generation, system browser, native vaults, backend-only
tokens, exact `drive.file`, bounded refresh, account/client binding and publisher
build configuration were retained after inspection.

Corrections: callback attempt context and closure before exchange; canonical
route/parameter checks; duplicate/case-aliased provider JSON rejection; cancel and
drain before local credential removal; separate confirmed remote revocation
bound to the reviewed account; explicit shared-project warning, default Cancel,
and local-cleanup recovery after confirmed revocation. The CLI remains offline
and its help now correctly describes the bundled client. No alternate CLI
credential store or authorization flow was introduced.

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

## One-click authorization follow-up (alpha.3)

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

## Implemented behavior

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

## Task coverage and source gate

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

## Next dependency-ready work

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

Remaining product work and signing limitations are unchanged: offline preview
only, no Drive account/transfer, no cross-version installer migration claim,
no trusted Windows publisher signature, no macOS notarization, and no full
Windows/Linux graphical application acceptance.

The secondary GitHub Pages copy also matches the new HTML and CSS after
[Pages run 36947337099](https://github.com/alexandroit/LedgeSync/actions/runs/36947337099)
passed. Production remains the owner's Ubuntu origin. Temporary server upload
directories were removed; live/rollback releases and signed APT snapshots remain.

## Current handoff

The subsequent OAuth protection changes are in source and covered by the
hardening section above. They have not replaced the published alpha.3 artifacts.
Complete live Google acceptance with an authorized account before claiming
provider interoperability; remote revocation is a separate deliberate action
with possible impact on other apps in the same Google Cloud project.

Alpha.3 is published with the bundled Desktop OAuth client, native credential
storage, DMGs, Windows installers and signed Ubuntu packages. Release and
publication evidence is recorded in the one-click authorization section above.
Both public APT architectures passed; the website and secondary Pages are current.
The owner can now open **Connections → Connect Google Drive** and complete
browser consent using [the connection guide](GOOGLE_DRIVE_AUTH.md); no end-user
client creation or JSON import is required. Actual Google account acceptance
remains pending. Cloud file transfers remain unimplemented.
