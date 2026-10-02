# Native Google folder selection and authorized Drive requests

This change extends the existing direct desktop authorization service. It keeps
the exact `drive.file` scope and does not add browser-side access tokens, a web
OAuth client, a public callback endpoint, or a new credential store.

## Official protocol and implementation decision

Google's [native desktop Picker guide](https://developers.google.com/workspace/drive/picker/guides/desktop-mobile-picker),
updated September 29, 2026, documents Picker through the system-browser OAuth
authorization endpoint. Folder selection adds `prompt=consent`,
`trigger_onepick=true`, `allow_folder_selection=true`, and the folder MIME filter.
Multiple selection is not enabled. The callback returns `picked_file_ids` and a
fresh authorization code. The existing Desktop client, random loopback redirect,
PKCE S256, single-use state, bounded callback and cancellation remain in use.
Google Picker API must be enabled in the same project; this native integration
does not require the JavaScript Picker's browser API key or a web OAuth client.
The owner reports that Google Picker API is now enabled; no Cloud settings were
independently inspected or changed during this implementation.

`Service.ChooseFolder` first checks the connected account and then verifies the
account returned by the new authorization before saving credentials. It requires
one bounded provider ID and rejects missing, empty, multiple, duplicate, malformed
or denied selections. An omitted refresh token may retain the existing bound
client/account's token; authorization-code responses still must explicitly grant
exactly `drive.file`. Selecting a different browser account leaves the prior grant
unchanged. The returned DTO contains only folder ID and account reference.

The provider must retrieve the chosen ID and check folder type, trash state,
supported Drive location and `capabilities.canAddChildren` before using it as a
destination. A selected parent does not establish access to every existing
descendant. Folder selection and an upload approval are separate operations.
See [Drive scopes](https://developers.google.com/workspace/drive/api/guides/api-specific-auth)
and [file metadata](https://developers.google.com/workspace/drive/api/reference/rest/v3/files).

## Backend authorization boundary

`Service.DoAuthorized` provides the native provider with authorized HTTP execution
without returning a raw access token. It accepts only canonical HTTPS
`www.googleapis.com` Drive v3 file-resource and upload-resource URLs and
GET/POST/PUT methods. It rejects alternate origins, ports, userinfo, fragments,
encoded path aliases, traversal, nested permission endpoints, duplicate query
parameters and token query parameters. Caller authorization/cookie headers are
removed case-insensitively, redirects are never followed, and the returned
response does not retain its credential-bearing request.

Requests have a one-minute bound including each response body. The service owns
its credential-operation lock until the body is closed, exhausted or canceled.
Cancellation closes the response and permits disconnect to drain before deleting
credentials. Every provider caller must close the response body. Successful
responses remain streams; explicit scope-error bodies are bounded before parsing.

An expired access token may be refreshed once. A read-only GET receiving 401 may
use the remaining refresh budget and retry once; a mutation receiving 401 is
never replayed and requires reconnection. Explicit insufficient-scope errors and
invalid grants stop subsequent requests. File ACL denials do not invalidate the
whole grant. A refreshed token's account is checked before use. Account and client
binding is reloaded from the native-vault service before each request.

Provider create/upload idempotency, persistent transfer recovery, source/destination
validation and job locking remain responsibilities of the provider/application
layers. The existing source-review baseline and rejection of unrestricted rclone
executor reuse are unchanged; see [the source audit](RCLONE_SOURCE_AUDIT.md).
No upstream code was copied by this authorization extension.

## Validation and remaining evidence

Tests use an in-memory vault, fake token/account servers, synthetic folder IDs and
an injected HTTP transport. They cover Picker parameters and PKCE, account/client
binding, denied/malformed selections, retained refresh tokens, exact scope,
credential redaction, endpoint restrictions, response ownership, disconnect
cancellation, one-refresh budgets, mutation non-replay, scope versus ACL errors,
and redirect rejection. Existing callback replay/expiry and OAuth hardening tests
also pass through the shared callback implementation.

Commands run from the repository root:

```sh
go test ./internal/driveauth
```

Passed immediately after adding the implementation. An initial default-cache
race build then failed during linking with `no space left on device` on the
Mac's internal disk. No user data or caches were removed. Subsequent validation
used ignored build directories on the SSD:

```sh
mkdir -p build/drive-auth-validation/tmp build/drive-auth-validation/cache
GOTMPDIR="$PWD/build/drive-auth-validation/tmp" GOCACHE="$PWD/build/drive-auth-validation/cache" go test -race ./internal/driveauth
GOTMPDIR="$PWD/build/drive-auth-validation/tmp" GOCACHE="$PWD/build/drive-auth-validation/cache" go vet ./internal/driveauth
```

Both pass with the added regressions. This evidence does not establish live
Google Picker behavior, live upload success, native-vault round trips, enabled
Cloud APIs, package publication, or the transfer executor's completion. No real
credentials were loaded and no Google account, Cloud configuration or file was
changed during these tests. Shared-project settings must be changed only by the
owner or within explicit authorization.

## Desktop transfer integration

The desktop bridge now exposes destination selection, My Drive selection, upload
preview for the native-selected source, approval by exact plan digest, transfer
status and cancellation through the shared transfer service. The native shell
constructs one local preview service and connects the same authorization instance
to both account controls and the Drive provider. Missing publisher configuration
leaves the transfer service unavailable without touching the vault.

Source/account/selection actions cannot run during an active transfer. Successful
source changes invalidate prior approval. Disconnect, explicitly confirmed
revocation and shutdown cancel and drain transfers before credential lifecycle
changes. A declined revocation has no cancellation side effects. Canceling the
native Picker preserves the previous destination. A changed connected account
cannot reuse another account's destination in the UI.

Closing during a Drive operation displays a native dialog with **Keep Open** as
the default/cancel choice. Only **Stop and Close** drains the operation and closes.
`OpenUploadedDriveFolder` accepts no frontend arguments and opens only a validated
folder ID from a verified successful transfer at the fixed Google Drive origin.
The Wails v2.14.0 `OnBeforeClose`, `MessageDialogOptions` and `BrowserOpenURL` APIs
were inspected in their locally installed source before wiring them.

Further checks passed using SSD cache paths. An intermediate desktop test
failed extracting the new SQLite dependency to the internal disk; the SSD module
cache avoided that environment failure without deleting user files. The block
below is a portable transcription for use from the repository root, not a new
execution. Go requires absolute cache paths, derived here from that directory.

```sh
mkdir -p build/go-modcache build/security-review/go-cache build/security-review/go-tmp
export GOMODCACHE="$(pwd)/build/go-modcache"
export GOCACHE="$(pwd)/build/security-review/go-cache"
export GOTMPDIR="$(pwd)/build/security-review/go-tmp"
export TMPDIR="$GOTMPDIR"
go test -race ./internal/transport/desktop ./internal/driveauth
go vet ./internal/transport/desktop ./internal/driveauth
go test -tags bindings ./cmd/ledgesync-desktop
go test -race -tags bindings ./cmd/ledgesync-desktop
git diff --check -- internal/driveauth internal/transport/desktop cmd/ledgesync-desktop docs/research/NATIVE_PICKER_REVIEW.md
```

The native-close decision and bridge tests use fakes; they do not constitute a
visual test of the native dialog or a live browser/Google Drive transfer.
