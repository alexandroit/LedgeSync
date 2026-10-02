# OAuth protection verification and hardening

Reviewed on 2026-10-02 against the owner's seven-part protection request.
This is a source review with isolated regression tests, not a third-party audit
or evidence of a real Google account authorization. The published alpha.3
downloads remain immutable; this follow-up describes the updated source.

## Existing protections retained

| Requirement | Verified implementation and evidence |
| --- | --- |
| Authorization code, PKCE S256, independent state | `internal/driveauth/oauth.go` draws two independent 32-byte values from `crypto/rand`; the verifier is exchanged only in the token POST body. Existing `TestConnectionPKCEPersistenceRestartAndDisconnect` checks challenge/verifier correspondence. |
| System browser, private callback | `internal/systembrowser` uses the platform browser launcher with a fixed destination allowlist, discarded launcher output and a deadline. OAuth binds `tcp4` to `127.0.0.1:0`; there is no website callback. Existing callback and launcher tests check host/path/method/state/replay, safe errors and listener cleanup. |
| Native vault, backend-only tokens | `internal/credentialvault` uses macOS Keychain, Windows Credential Manager and Linux Secret Service. Native errors are normalized, with no file/environment fallback. `credential.AccessToken` and expiry have `json:"-"` tags. The refresh token is serialized only inside the protected vault record. Frontend DTOs have no token fields. |
| Client and account binding | The current product has one active account, stored atomically with its OAuth client and immutable account reference. Runtime token reuse requires matching client ID, client secret, refresh token and account reference. Different clients are blocked; a different account cannot replace the saved grant during reconnect/check. Tests in `bundled_test.go` and `service_test.go` retain the prior record on failed transitions. This is not a multi-account credential catalog. |
| Minimum permissions | Only `drive.file` is requested. Initial token responses must confirm that grant; a refresh may omit scope and retain the already validated grant. A broader/different grant is rejected. Files still use a simulated destination; connection does not expose every Drive file. |
| Refresh and provider failures | Refresh is bounded to one attempt per check, omitted refresh tokens retain the previous token, and rotation is saved before account lookup. Revoked grants, identity changes and scope failures require reconnection. Provider response bodies are not returned as diagnostics. |
| Client configuration and distribution | The existing Desktop client is injected only into trusted publisher builds. Raw development JSON/generated source are excluded from Git; the strict helper rejects user-token fields. Desktop client metadata is recoverable from the application by design. No personal token is a build input. |

The subsequent [local audit follow-up](OAUTH_LOCAL_REVIEW.md) adds read-only CLI
connection status through the same `internal/driveauth` and `internal/connections`
backend. CLI preview stays offline; interactive authentication and credential
mutations remain GUI actions. No alternate credential store was introduced.

## Corrections in this follow-up

- The callback handler now checks the attempt context directly. Cancellation
  closes its listener even while a browser launcher is returning, and a received
  callback closes the listener before token exchange starts.
- Callback route parsing rejects encoded aliases, absolute-form requests,
  fragments and request bodies. Duplicate parameters, implicit-flow token
  fields, mismatched optional scope/issuer and mixed code/error responses fail
  closed. Unknown bounded single-valued extensions remain ignored as required
  by [RFC 6749 section 4.1.2](https://www.rfc-editor.org/rfc/rfc6749#section-4.1.2).
- **Disconnect from this device** cancels and drains the active account operation
  before removing local tokens, preventing a delayed in-process refresh/save
  from restoring them. New operations cannot start during this transition. A
  drain timeout reports failure rather than claiming credentials were removed.
- **Revoke access on Google** is a separate action. Its dialog identifies the
  account, explains the shared-project impact and focuses **Cancel** by default.
  The backend requires explicit confirmation and the same opaque account
  reference before making any revocation request. A changed OAuth client is
  rejected. The token is sent in a form POST body to Google's fixed endpoint,
  never in a URL, and revocation is never retried automatically.
- Confirmed revocation drops the in-process access token and then clears the
  local grant. A local vault cleanup failure is displayed separately and blocks
  further account operations in that process until cleanup succeeds. An
  unconfirmed network/provider result retains the local record and reports the
  uncertainty; it does not claim Google kept or removed the grant.
- Provider JSON rejects ambiguous duplicate fields while retaining support for
  unknown extensions. The CLI help now describes the bundled Desktop client.
- A subprocess regression captures stdout/stderr as well as returned status and
  errors across success, refresh, revoked-grant, launcher, vault and revocation
  failures. It checks synthetic credential markers without printing them.

Google documents that revocation invalidates grants across clients in the same
Cloud project and can take time to propagate. This is the reason for the
explicit impact warning, not a reason to alter other clients or shared branding.
See [Google's revocation contract](https://developers.google.com/identity/protocols/oauth2/native-app#tokenrevoke).

## Tests and boundaries

Callback regression cases are in
[`callback_hardening_test.go`](../../internal/driveauth/callback_hardening_test.go).
Lifecycle/confirmation/provider cases are in
[`lifecycle_hardening_test.go`](../../internal/driveauth/lifecycle_hardening_test.go).
Process-output and DTO checks are in
[`diagnostics_test.go`](../../internal/driveauth/diagnostics_test.go).
Desktop binding tests and browser interaction tests exercise the confirmation
boundary and recovery states without a personal Google account.

Exact executed commands and outcomes are recorded in
[the current handoff](../17_AGENT_HANDOFF_AND_STATUS.md) and the
[verification report](OAUTH_HARDENING_VERIFICATION.json). All 16 CI jobs passed
at the recorded source; the later cleanup-warning text has a targeted passing
recovery test and a passing TypeScript/production build. Existing alpha.3 native
vault CI evidence remains historical to that release; the local native-vault
opt-in is not enabled against the owner's credential store.

Known limits:

- No live Google authorization, refresh or revocation was performed. Tests use
  synthetic credentials, local HTTP servers and fake stores. No uploads, cloud
  file changes, Google Cloud configuration changes or other-app actions occur.
- Credential serialization is per service instance. The project still does not
  claim a cross-process OAuth transaction lock or simultaneous multi-account
  support. Use one application instance. A stale confirmation is rejected if
  the account changed before the backend loads it; external writes after that
  read require the separate cross-process transaction work.
- Native OS vault calls may wait for OS interaction and are not all interruptible
  through the storage port. Draining an existing call is bounded; a timeout does
  not mean the credential was removed.
- The confirmed-revocation/failed-cleanup marker is process-local because the
  vault write failed. After restart, a retained old record can appear connected
  until a connection check discovers the revoked grant. Unlock the vault and
  finish local disconnection before closing the app after such a warning.
- There is no synchronization executor or scheduler to stop yet. All account
  requests implemented in this slice are serialized/canceled through the service;
  future transfer jobs must participate in the same lifecycle gate.
- The website/server stores no account tokens. This does not protect a compromised
  user session, operating system or maliciously replaced software distribution.

## Manual Google Cloud acceptance

Use the existing LedgeSync Desktop client. Confirm the Drive API and the intended
testing audience/publication settings in the existing project without changing
shared branding, deleting clients or modifying other applications. If the
project is still in Testing, use an authorized test account. Complete browser
consent and a read-only connection check manually; live revocation requires a
separate deliberate decision after considering every affected application.
No broader Drive scope or Cloud configuration change is required by this patch.

Protocol references: [Google installed-app OAuth](https://developers.google.com/identity/protocols/oauth2/native-app),
[PKCE RFC 7636](https://www.rfc-editor.org/rfc/rfc7636), and
[Drive scope definitions](https://developers.google.com/workspace/drive/api/guides/api-specific-auth).
