# OAuth local audit follow-up

Scope: the owner's second seven-part audit request. Baseline commit:
`4452150e02498d59839e2c135e14baebe9b7fed8`. Changes described here are local,
unreleased source changes, not a security certification or a live integration test.
Published alpha.3 installers, APT packages, tags, website and Cloud settings were
not changed. The working tree was clean before this work.

## Existing protections verified

Read project identity, AGENTS, provider/desktop/security/test specifications and
the current handoff. Inspected actual OAuth, vault, browser, desktop bridge,
build-helper and CLI implementation, then ran the existing regressions.

| Protection | Code and test evidence retained |
| --- | --- |
| PKCE S256 and independent random state | `internal/driveauth/oauth.go`: independent 32-byte `crypto/rand` draws, standard SHA-256/base64url. `service_test.go` checks exchange parameters; `callback_hardening_test.go` covers fresh attempts, expiry, incorrect/missing state and concurrent replay. |
| System browser and private callback | `internal/systembrowser` fixed-origin native launcher; `oauth.go` binds `127.0.0.1:0`, validates canonical route/host/parameters, closes before exchange and on cancellation. Synthetic timeout/denial/cleanup tests pass. |
| Backend-only credentials | `internal/credentialvault` uses native Keychain/Credential Manager/Secret Service adapters, with no plaintext fallback. Access tokens are excluded from JSON. Vault failure, DTO, subprocess diagnostic and browser tests pass using fakes. No real vault operation was performed. |
| Account/client binding | `bundled_test.go` and lifecycle tests verify changed clients and changed accounts cannot silently reuse/replace the saved grant. One active account/client record is supported, not a multi-account catalog. |
| Least privilege | `drive.file` is the only requested scope; initial and refresh responses are checked. The UI describes the simulated destination and limited scope. No cloud file browser or picker is falsely claimed to work. |
| Refresh and lifecycle | Omitted refresh tokens are preserved; refresh has a one-attempt budget. Invalid grants stop repeated checks. Local disconnect drains in-process operations. Remote revocation requires explicit confirmation and the reviewed account reference. |
| Revocation warning | Existing UI/backend tests cover default Cancel, shared-project impact, stale account rejection, no automatic remote retry and failed local cleanup after Google success. |

## Missing behavior corrected

1. **Excess build metadata:** validation previously serialized the entire accepted
   Desktop JSON. Generation now retains only the five fields required by the
   runtime parser. Development project ID and certificate URL are omitted.
   Tests check both generated source and synthetic compiled binaries. Validation
   still rejects token-bearing inputs, unknown fields, duplicates, unsafe files
   and non-Google endpoints. No existing client was replaced or newly created.
2. **Explicit API scope loss:** an account check previously classified every 403
   as a generic provider failure. Explicit `insufficientPermissions` or typed
   `ACCESS_TOKEN_SCOPE_INSUFFICIENT` now returns the fixed scope error and saves
   `reconnect_required`. Further checks stop before refresh/network. Other 403s
   (including quota, disabled API and file ACL errors) do not invalidate the grant.
   Malformed/ambiguous error JSON is not interpreted as scope loss. Provider
   messages are never reflected. No scope expansion was made.
3. **CLI service use:** `ledgesync auth status` now uses the same factory, native
   vault adapter, client/account checks and status DTO as the GUI. It explicitly
   returns `onlineVerified: false`. It cannot open a browser, refresh, disconnect
   or revoke. Those CLI actions return `AUTH_REQUIRED` with the GUI workflow,
   before service creation. This meets shared-service use without adding a second
   authentication implementation or an unsafe concurrent mutation path.
   Missing configuration fails closed. The optional `oauth` build tag includes
   the existing generated configuration for native CLI builds; existing portable
   packaging jobs remain unconfigured. End-user GUI setup remains one-click.

## Changed files

- `cmd/ledgesync/main.go`, `auth.go`, `auth_test.go`: routing, safe headless status
  and tests against the real shared service with a synthetic vault.
- `internal/driveauth/http.go`, `scope_loss_test.go`: explicit scope-error mapping
  and persisted reconnect/no-retry/redaction regressions.
- `tools/configure_oauth_client.py`, `tools/test_configure_oauth_client.py`:
  minimal configuration and synthetic build tests for desktop/oauth tags.
- `docs/08_DESKTOP_CLI_AND_AUTOMATION.md`, `docs/OAUTH_BUILD.md`,
  `docs/GOOGLE_DRIVE_AUTH.md`: exact CLI/build behavior and limitations.
- `docs/10_TEST_STRATEGY_AND_ACCEPTANCE.md`: removed the obsolete blanket claim
  that no application exists; keeps acceptance targets distinct from evidence.
- `docs/research/OAUTH_SECURITY_HARDENING.md`, this report and
  `docs/17_AGENT_HANDOFF_AND_STATUS.md`: current evidence and handoff.

## Commands and results

Commands below ran from the project root unless a different directory is noted.
Their workstation-specific paths are now portable transcriptions; the results
are historical and were not rerun for the path update. Output paths are relative
to the repository root; Go cache paths are derived from that root because Go
requires absolute values. Prepare `build/security-review` before the output
redirection below (`mkdir -p build/security-review`). All credentials in tests
were synthetic. Existing tests that Go reported as cached remain cache results,
not newly exercised native integrations.

| Command (portable transcription) | Recorded result |
| --- | --- |
| `go test ./internal/driveauth ./cmd/ledgesync ./internal/connections` | Pass |
| `python3 tools/test_configure_oauth_client.py` | Pass, 9 tests, including both synthetic build tags |
| `go test -json ./... > build/security-review/ledgesync-oauth-review-tests.jsonl` | Pass, 333 test/subtest pass events, 2 explicit test skips; 12 tested packages, 3 packages without tests |
| `go test -race ./...` | Pass |
| `go vet ./...` | Pass |
| `npm test` (in `frontend`) | Pass, all 30 Playwright tests |
| `npm run build` (in `frontend`) | Pass, includes TypeScript checking, Vite and native asset preparation |
| `go test -tags desktop ./cmd/ledgesync-desktop ./internal/transport/desktop ./internal/connections` | Pass; desktop main has no tests |
| `go vet -tags desktop ./cmd/ledgesync-desktop ./internal/transport/desktop ./internal/connections` | Pass |
| `CGO_CFLAGS='-mmacosx-version-min=13.0' CGO_LDFLAGS='-mmacosx-version-min=13.0' go build -tags desktop -o build/security-review/LedgeSync ./cmd/ledgesync-desktop` | Pass, native macOS ARM64 compilation |
| `go build -tags oauth -o build/security-review/ledgesync ./cmd/ledgesync` | Pass, native CLI compilation |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -tags oauth -o build/security-review/ledgesync-linux-amd64 ./cmd/ledgesync` | Pass, cross-compilation only |
| `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags oauth -o build/security-review/ledgesync-linux-arm64 ./cmd/ledgesync` | Pass, cross-compilation only |
| `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags oauth -o build/security-review/ledgesync-windows-amd64.exe ./cmd/ledgesync` | Pass, cross-compilation only |
| `GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build -tags oauth -o build/security-review/ledgesync-windows-arm64.exe ./cmd/ledgesync` | Failed initially: system-volume temporary build directory ran out of space |
| `mkdir -p build/security-review/go-tmp build/security-review/go-cache` | Pass, isolated directories on SSD |
| `GOCACHE="$(pwd)/build/security-review/go-cache" GOTMPDIR="$(pwd)/build/security-review/go-tmp" GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build -tags oauth -o build/security-review/ledgesync-windows-arm64.exe ./cmd/ledgesync` | Pass on retry, cross-compilation only; no user files/shared caches deleted |
| `.venv/bin/python tools/validate_docs.py` | Pass, 43 Markdown files, 147 local links; zero failures/skips |
| `git diff --check` | Pass |

The native vault lifecycle test is opt-in and skipped. The external pinned
rclone differential test skipped because it was not configured for this run.
macOS builds above intentionally have no real publisher configuration; synthetic
helper tests cover configuration inclusion. Linux/Windows CLI cross-builds also
contain no real client; successful compilation is not runtime/vault validation.
No personal client/token bytes were
read for tests, printed or committed. Cross-platform native execution, live
Google consent/refresh/revocation and production integration were not tested.

## Remaining limits and manual steps

- Cloud transfers, authorized folder selection and the scheduler are not
  implemented. There are no running sync jobs to pause; the authentication
  service persists reconnect-required status for future consumers. Any executor
  must honor this before provider operations; this report does not claim a tested
  job-pause integration.
- There is one active account. Isolation is rejection of mismatched client/account
  bindings, not separate simultaneous account slots. OAuth operation serialization
  is in-process. Multiple GUI processes are not protected by a cross-process
  credential lock. CLI status is read-only; CLI credential mutations stay disabled.
- Native vault operations can involve OS unlock interaction. Linux without a
  usable Secret Service and macOS without CGO fail closed. Native platform
  adapter integration needs isolated, authorized platform tests.
- After confirmed remote revocation with a vault cleanup failure, the protective
  tombstone is process-local. Finish local cleanup before closing; it cannot be
  persisted while the vault is unavailable. A restart may require an explicit
  check to discover a revoked saved grant. Existing UI states disclose this.
- Desktop client metadata remains extractable. The same supplied Desktop client
  should be used for the next authorized build. No new Cloud client or shared
  project changes are needed for these source fixes. The owner must verify the
  existing Drive API, test-user/audience and publication/verification settings
  for intended users; those console settings were not inspected or modified.
- Signing/notarization, clean-install testing and native Windows/Linux runtime
  checks remain separate release gates. Current downloads do not contain these
  changes. DPoP-bound refresh tokens are not implemented by this patch.

## Authorization boundaries and references

Local changes and the checks listed above are finished. Live Google
authorization/revocation, personal-vault tests,
Drive writes, shared Cloud setting changes, scope expansion and publishing these
changes require the corresponding explicit authorization. No permission is
needed to review these local changes.

Current official sources were consulted before changing OAuth behavior:

- [Google native-app OAuth](https://developers.google.com/identity/protocols/oauth2/native-app):
  supports the Desktop loopback flow. Its revocation section states that the
  account's project grants, including tokens for other clients in that project,
  are affected. The existing confirmation warning is therefore retained.
- [Google OAuth best practices](https://developers.google.com/identity/protocols/oauth2/resources/best-practices):
  state, PKCE, secure token storage, invalidation handling and system browsers.
- [Drive scopes](https://developers.google.com/workspace/drive/api/guides/api-specific-auth):
  `drive.file` provides per-file access; connecting is not blanket access to Drive.
- [Drive error handling](https://developers.google.com/workspace/drive/api/guides/handle-errors):
  403 can indicate multiple permission/quota causes, requiring reason-aware handling.
- [Google API error definitions](https://github.com/googleapis/googleapis/blob/master/google/api/error_reason.proto):
  defines `ACCESS_TOKEN_SCOPE_INSUFFICIENT`. No third-party advice was used to
  broaden permissions or alter the shared Cloud project.
