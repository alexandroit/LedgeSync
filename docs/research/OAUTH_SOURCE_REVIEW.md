# Native Google Drive authorization source review

Reviewed 2026-10-02 for the authorization slice only. This is a bounded source
review, not a security audit of rclone or a claim of working live Google access.

## Verified upstream baseline

The existing official reference clone was read outside the LedgeSync repository.
`git rev-parse HEAD` returned
`687d264b689b8c49a67e2e52a8a5e0caa01c04ce` (the previously audited stable
`v1.75.1` baseline); `git status --porcelain` returned no changes. Its OAuth file
`lib/oauthutil/oauthutil.go` has SHA-256
`a18088e7c8cf1577822b08d08973bb6363c1922aea0d03707ce8cacdfb742620`.
The broader clone/license inventory is [UPSTREAM_BASELINE.json](UPSTREAM_BASELINE.json).
No upstream configuration, credential store, application identity or live remote
was loaded. This continuation deliberately retains the already selected stable
baseline; it does not claim to have selected a newer release.

## Observations and decisions

| Pinned source evidence | Observed behavior | LedgeSync decision |
| --- | --- | --- |
| [`GetToken`, `PutToken`, `TokenSource`, lines 181–250](https://github.com/rclone/rclone/blob/687d264b689b8c49a67e2e52a8a5e0caa01c04ce/lib/oauthutil/oauthutil.go#L181-L250) | Tokens are read and written through the upstream configuration mapper. | Implement an injected OS-vault port, with one atomic client/refresh-token/account record; never import rclone credentials. Keep access tokens process-local. |
| [`OverrideCredentials`, lines 447–483](https://github.com/rclone/rclone/blob/687d264b689b8c49a67e2e52a8a5e0caa01c04ce/lib/oauthutil/oauthutil.go#L447-L483) | Upstream configuration can override client identity and authorization/token endpoints. | Accept only the downloaded Google `installed` JSON shape; reject web/service-account clients, unknown/duplicate fields and custom endpoints. Production endpoints are fixed HTTPS constants. |
| [`getAuthURL`, `configSetup`, `configExchange`, lines 886–1027](https://github.com/rclone/rclone/blob/687d264b689b8c49a67e2e52a8a5e0caa01c04ce/lib/oauthutil/oauthutil.go#L886-L1027) | The source generates random state, requests offline access, launches the browser, supports cancellation and exchanges the code. This path also logs URLs and delegates token persistence to the mapper. | Implement a bounded authorization-code flow with independent random state and PKCE S256, a fresh IPv4 loopback port, an injected system-browser opener and no URL/token logging. No out-of-band code-paste flow. |
| [`authServer`, lines 1053–1159](https://github.com/rclone/rclone/blob/687d264b689b8c49a67e2e52a8a5e0caa01c04ce/lib/oauthutil/oauthutil.go#L1053-L1159) | This callback parses forms, permits an optional blank-state mode, includes provider error descriptions/state values in errors and uses a result channel. | Require GET, exact loopback host/path, one constant-time-checked state, bounded query/header/read time and a single nonblocking buffered result. Untrusted state never consumes the flow; valid-state malformed/denied results end it. Return fixed redacted responses. |
| [`TestRcOAuthStatus`, `TestRcOAuthStop`](https://github.com/rclone/rclone/blob/687d264b689b8c49a67e2e52a8a5e0caa01c04ce/lib/oauthutil/rc_test.go#L12-L81) | The inspected tests exercise shared OAuth status/cancellation and expose the authorization URL through the upstream RC status response. | LedgeSync's DTO contains state, account labels/reference, scope and a safe message only. Test callback/PKCE/HTTP/vault behavior directly against local fakes. |

**Inference:** directly importing this upstream package would introduce upstream
configuration/global-state conventions and require replacing much of its OAuth
boundary. A small standard-library implementation gives this slice explicit,
reviewable scope, persistence and redaction boundaries. No upstream OAuth source
was copied or adapted, and no new OAuth library/module was added. Existing MIT
notices for previously adapted filter code remain unchanged. Upstream OAuth tests
were inspected but not run; LedgeSync's own fake-provider tests were run.

## Provider contract checked against official documentation

- [Google native-app OAuth](https://developers.google.com/identity/protocols/oauth2/native-app): system browser, authorization code, PKCE S256, loopback redirect, offline access and token refresh. A Desktop app OAuth client must first exist in Google Cloud. A desktop client secret is not a confidential-server secret; the application still keeps imported configuration backend-only in its vault.
- [Drive `about.get`](https://developers.google.com/workspace/drive/api/reference/rest/v3/about/get): account lookup with the explicit `user(displayName,emailAddress,permissionId)` fields mask. The provider ID is hashed with a versioned LedgeSync domain separator into the UI's account reference.

Only `https://www.googleapis.com/auth/drive.file` is requested and accepted.
Initial token responses must explicitly confirm that exact grant. Refresh
responses may omit scope and retain the already validated grant. An unexpected
broader or different grant fails closed; there is no automatic escalation.
`about.get` is the only Drive API operation in this slice. It does not list,
create, modify, synchronize or delete files.

## Local verification and limits

`go test -race ./internal/driveauth` and `go vet ./internal/driveauth` pass using
synthetic credentials, an in-memory fake vault, local HTTP servers and local
loopback callbacks. The tests cover initial state and client validation, PKCE
exchange, callback method/host/path/state/replay guards, browser failure,
timeout/cancellation and listener cleanup, denied authorization, token/body
bounds and redaction, exact scope/Bearer validation, restart refresh, refresh
rotation/retention, refresh failure/one-refresh budget, revoked grants,
account-change rejection, vault failure/capacity, concurrent operation exclusion,
redirect rejection and local-only disconnection.

The vault record is capped at 2560 UTF-8 bytes for Windows Credential Manager.
Access tokens are never serialized. If necessary, persisted cosmetic account
labels are omitted while the immutable account reference is preserved; a later
account check repopulates the labels. A credential still exceeding that bound
fails closed. Mutations are serialized within the service instance; there is no
claim of a cross-process credential transaction manager.

A real OAuth client was not available, and no real Google authorization or token
exchange was performed. Google consent-screen publishing/verification, genuine
OS-vault round trips, headless remote authorization, multiple simultaneous
accounts, cloud namespace selection and synchronization remain separate gates.
Disconnect clears local tokens and retains the client configuration. It does not
revoke the Google grant; revocation is a separate explicit user action.

## System-browser boundary and duplicate instances

An integration review also inspected the pinned `github.com/pkg/browser`
`v0.0.0-20240102092130-5ac0b6a4141c` implementation. Its Unix `runCmd` calls
`exec.Command(...).Run()` without a deadline and forwards child stdout/stderr.
On Linux it may choose `xdg-open`, `x-www-browser` or `www-browser`. That boundary
could block cancellation or print an authorization URL through launcher output.
LedgeSync therefore uses `internal/systembrowser`, with no copied upstream code:

- Only the exact HTTPS Google authorization endpoint and one fixed Google Cloud
  Drive API setup page are accepted. Userinfo, explicit ports, fragments, encoded
  path alternatives, arbitrary files and other schemes/origins are rejected.
- macOS uses `/usr/bin/open`; Linux discovers only `xdg-open`. Both pass a single
  typed URL argument, discard output, have a five-second context deadline and a
  bounded process wait. There is no shell-command assembly or browser fallback.
- Windows uses the already pinned `golang.org/x/sys/windows.ShellExecute`, whose
  implementation calls the native `ShellExecuteW` API. The launcher initializes
  COM in an STA on a locked OS thread and uses the explicit `open` verb. The wrapper returns by
  its deadline and permits at most one pending OS call. A native OS call cannot
  be force-canceled; if it outlives the deadline, another attempt fails until it
  returns. No detached or unlimited retry goroutines are created.
- `go test -race ./internal/systembrowser` and `go vet ./internal/systembrowser`
  passed. Tests exercise destination rejection, safe errors, canceled contexts
  and an actual synthetic helper process exceeding its launch deadline. Windows
  ARM64 and Linux AMD64 test binaries cross-compiled; those cross-compilations
  are not native launcher execution evidence.

The pinned Wails `v2.14.0` `SingleInstanceLock` implementation was inspected in
`internal/frontend/desktop/{darwin,linux,windows}/single_instance.go`. macOS uses
an advisory file lock, Linux a D-Bus name and Windows a named mutex plus a message
window. The Linux and Windows code explicitly permit continuation after certain
IPC/startup failures; Windows can also encounter the mutex before the first
window exists. This option may reduce duplicate windows, but is **not** evidence
of a fail-closed cross-process OAuth transaction lock. Cross-process credential
serialization remains a separate implementation gate.

Reference: [Microsoft ShellExecuteW](https://learn.microsoft.com/en-us/windows/win32/api/shellapi/nf-shellapi-shellexecutew).
