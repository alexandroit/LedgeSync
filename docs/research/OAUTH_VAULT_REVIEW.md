# OAuth native credential vault review

Review date: 2026-10-02. Scope: backend credential persistence for LedgeSync Google Drive authorization. This review is source inspection and targeted tests, not a third-party security audit or evidence of live Google consent.

## Selected components and provenance

Modules were downloaded using `go mod download -json module@version` before implementation; their module-cache source, native call boundaries, tests, and complete licenses were inspected. New modules are pinned; no source was copied into LedgeSync.

| Component | Exact version / upstream commit | Module checksum | License / use |
| --- | --- | --- | --- |
| `github.com/keybase/go-keychain` | `v0.0.1` / `dd79abb5f55f5239037126b5943c0b1335a84abc` | `h1:way+bWYa6lDppZoZcgMbYsvC7GxljxrskdNInRtuthU=` | MIT, Copyright 2015 Keybase. Darwin-only Security.framework / CoreFoundation bindings; its Linux Secret Service package is not imported. |
| `github.com/danieljoos/wincred` | `v1.2.3` / `623325312d3224d48d131159187b93e906216563` | `h1:v7dZC2x32Ut3nEfRH+vhoZGvN72+dQ/snVXo/vMFLdQ=` | MIT, Copyright 2014 Daniel Joos. Windows-only generic credential API. |
| `github.com/godbus/dbus/v5` | Existing repository pin `v5.1.0` | Existing `go.sum` pin retained | BSD-2-Clause. LedgeSync implements the narrow Linux Secret Service protocol using this existing D-Bus transport. |

The release notice generator must preserve both new MIT licenses. `go-keychain`'s historical iOS examples/framework artifacts are neither imported nor distributed as application payload. Its root package builds only on Darwin with cgo. No provider passwords or Google tokens were used for this review.

Inspected upstream sources:

- [Keychain native add/update/query/delete and conversion](https://github.com/keybase/go-keychain/blob/dd79abb5f55f5239037126b5943c0b1335a84abc/keychain.go), [CoreFoundation memory conversion](https://github.com/keybase/go-keychain/blob/dd79abb5f55f5239037126b5943c0b1335a84abc/corefoundation.go), [license](https://github.com/keybase/go-keychain/blob/dd79abb5f55f5239037126b5943c0b1335a84abc/LICENSE).
- [Wincred syscall implementation](https://github.com/danieljoos/wincred/blob/623325312d3224d48d131159187b93e906216563/sys.go), [public API](https://github.com/danieljoos/wincred/blob/623325312d3224d48d131159187b93e906216563/wincred.go), [license](https://github.com/danieljoos/wincred/blob/623325312d3224d48d131159187b93e906216563/LICENSE).
- [Existing godbus connection setup and contextual calls](https://github.com/godbus/dbus/blob/v5.1.0/conn.go), [EXTERNAL authentication](https://github.com/godbus/dbus/blob/v5.1.0/auth_external.go).

## Adapter behavior and trust boundaries

All backends use namespace `com.ledgesync.oauth` and opaque lowercase references. They never enumerate a user's credential store. The public adapter returns constant errors instead of OS/provider error strings. `New` has no credential side effects. Unsupported platforms and Darwin builds without cgo fail with `ErrUnavailable` when used. There is no disk, environment, process-command, or pretend-persistent memory fallback.

macOS uses in-process `SecItemAdd`, `SecItemCopyMatching`, `SecItemUpdate`, and `SecItemDelete`. It disables synchronizable items and requests unlocked, device-only accessibility. Updates preserve the existing item instead of deleting before writing. It does not spawn `/usr/bin/security`. The native OS controls interaction/access prompts; local Go tests do not automatically exercise the user's Keychain. A compromised user/OS is outside this guarantee.

Windows loads `advapi32.dll` through `NewLazySystemDLL`, then calls the generic credential APIs. Persistence is `CRED_PERSIST_LOCAL_MACHINE`: the same user's subsequent logins on this computer, with no roaming setting. The adapter never uses credential enumeration or domain-password APIs. [Microsoft's credential structure reference](https://learn.microsoft.com/en-us/windows/win32/api/wincred/ns-wincred-credentialw) documents the maximum blob as 2,560 bytes. This becomes the adapter's portable limit; oversized data fails explicitly without truncation. References are lowercase to avoid Windows' case-insensitive target-name collisions.

Linux uses the [freedesktop Secret Service protocol](https://specifications.freedesktop.org/secret-service/latest/) and an existing default collection. A dedicated local Unix D-Bus connection uses EXTERNAL authentication and a 30-second connection/operation deadline. TCP, shell autolaunch, and multiple-address fallback configurations are rejected. No `dbus-launch` or `secret-tool` process is spawned. Missing bus, missing service, missing default collection, refusal to unlock, and ambiguous matching items fail closed. Secret Service is responsible for protecting secrets at rest. Its standard `plain` session transports values over local IPC; this is not end-to-end encryption against the session bus or a compromised same-user process. It is never a plaintext file fallback. Secrets may exist in process and OS IPC memory while used.

Default application tests use fake stores and nonexistent temporary socket paths. The opt-in native test (`LEDGESYNC_TEST_NATIVE_VAULT=1`) is intended for disposable CI users. It generates a random reference, creates synthetic data, verifies read/update/delete, and cleans up only that reference. It never reads or lists existing account entries.

## Alternatives inspected

`99designs/keyring@v1.2.2` was inspected but not imported: its multi-backend package includes file and external-program providers unnecessary for this limited native-only requirement. `zalando/go-keyring@v0.2.8` was inspected but not imported: its macOS provider invokes `/usr/bin/security` (this version passes the write command through stdin rather than secret arguments), and its Linux prompt wait is unbounded. Direct native adapters avoid those process/access-control and fallback surfaces. These observations concern the downloaded pinned source, not a claim about future upstream versions.

## Validation boundary

`go test ./internal/credentialvault` passed on the development Mac without native-vault opt-in. Cross-architecture compilation and native lifecycle CI evidence are recorded with the overall OAuth implementation results in the agent handoff. No local live Google authorization, real-token import, or personal credential access is part of this review. Platform-native behavior requires the explicit disposable-runner lifecycle test; compilation alone does not establish it.
