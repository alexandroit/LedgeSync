# Native storage boundary review

Date: 2026-10-01. Scope: the desktop macOS, Ubuntu/Linux, and Windows credential
vault boundary and the local transfer journal. This is a source review with
synthetic tests, not a claim of live Google interoperability or protection from
code running as the same user, root, SYSTEM, or an administrator.

## Findings and implemented changes

### Windows journal ACL inheritance

The previous journal check only rejected symbolic links. Windows ignores Unix
0700/0600 access restrictions, so a journal could inherit ordinary-user or
Everyone access from its parent. The journal contains names and provider IDs,
even though it contains no OAuth tokens or upload-session URLs.

`security_windows.go` creates new journal directories with a native security
descriptor: explicit current-user owner, protected DACL, and one inheritable
current-user full-access ACE. SQLite sidecars inherit that grant. Existing
objects are inspected without changing their permissions. Native handles are
opened with `FILE_FLAG_OPEN_REPARSE_POINT`; reparse points, changed identities,
and multiply linked files are refused before examining the owner and DACL via
`GetSecurityInfo`.

The owner and every allow ACE must be the current user, SYSTEM, or built-in
Administrators. The privileged exceptions accommodate elevated Windows tokens
and native administrative access, which already permits taking ownership. An
ordinary foreign owner is refused even without an explicit foreign grant.
Everyone/Users grants, including inherit-only grants to future files, missing
or null DACLs, and unrecognized ACE forms fail closed. Deny ACEs cannot broaden
access and are permitted. This is intentionally stricter than calculating the
effective permissions of an arbitrary enterprise ACL.

Microsoft documents [parent ACL inheritance and explicit security descriptors](https://learn.microsoft.com/en-us/windows/win32/fileio/file-security-and-access-rights),
[directory creation with security attributes](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-createdirectoryw),
and [handle-based security descriptor inspection](https://learn.microsoft.com/en-us/windows/win32/api/aclapi/nf-aclapi-getsecurityinfo).
The implementation uses the already pinned `golang.org/x/sys/windows` bindings.

### macOS extended ACLs

A synthetic native fixture established that `chmod 0600` plus an extended ACL
granting Everyone read still reports POSIX mode 0600. The prior owner/mode/link
check therefore accepted it. Directory mode 0700 has the same limitation.

`security_darwin.go` opens the node without following links, compares the opened
identity with the inspected identity, and queries native extended security with
`fgetattrlist(ATTR_CMN_EXTENDED_SECURITY)`. Nonempty ACLs and unreadable or
malformed security metadata are refused. This conservative policy also refuses
restrictive nonempty ACLs; it does not rewrite existing settings or source
permissions. The existing owner, permission-bit, and hardlink checks remain.

The native SDK files read for this implementation were
`MacOSX.sdk/usr/include/sys/attr.h`, `sys/kauth.h`, and
`usr/share/man/man2/getattrlist.2` under the installed Command Line Tools SDK.
They specify the packed attribute reference, `kauth_filesec` layout, maximum
128 ACL entries, and no-ACL marker. Apple's [getattrlist documentation](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/getattrlist.2.html)
describes the packed buffer and possible truncation; the parser checks all
offsets and lengths. The already pinned `x/sys/unix` supplies the syscall number,
attribute structure, and flag. No cgo requirement or new dependency was added.

### Linux Secret Service peer identity

The previous transport accepted local Unix sockets but did not verify the
server's Unix identity before starting a plain Secret Service session. D-Bus
EXTERNAL authenticates the client; it does not by itself prove the identity of
an endpoint selected by `DBUS_SESSION_BUS_ADDRESS`.

`dialUserBus` now checks `SO_PEERCRED` through the connected socket's descriptor
before any D-Bus traffic. The peer UID must equal the effective process UID.
Unavailable credentials, another UID, or an inspection error close the socket
and return the redacted unavailable error. Same-user process impersonation is
outside this check's boundary. The [Linux Unix-socket manual](https://man7.org/linux/man-pages/man7/unix.7.html)
documents kernel-supplied `SO_PEERCRED`; the [D-Bus specification](https://dbus.freedesktop.org/doc/dbus-specification.html)
documents EXTERNAL client authentication.

On Ubuntu/Linux, the retained owner-only mode check also excludes effective
named-user/group POSIX ACL grants because their mask is represented by the group
mode bits. No filesystem credential fallback was introduced. The existing
Keychain, Credential Manager, and Secret Service backends remain native stores.

## Integration contract

- `prepareStateDirectory(path string) error` creates application-owned state
  directories. Windows uses the protected native DACL at creation; Unix uses
  mode 0700. Existing nodes are not repaired.
- `privateNode(path string, info os.FileInfo, directory bool) bool` validates
  type and privacy, including native Windows/macOS metadata. Unreadable or
  unsupported checks return false.
- `Store.Open` calls these helpers for the directory, existing state files, and
  newly opened lock/database files. The default remains `os.UserConfigDir()` plus
  `LedgeSync/transfers`, outside upload roots.

## Validation evidence and remaining native execution

All fixtures were synthetic. No developer credential vault, personal tokens,
Drive account, or real upload source was accessed. No changes were committed or
published by this review task.

The following checks passed on this macOS ARM64 host:

```sh
CGO_ENABLED=0 go test -count=1 ./internal/transferstate
CGO_ENABLED=1 go test -race -count=1 ./internal/transferstate ./internal/credentialvault
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go test -c -o build/security-review/transferstate-darwin-amd64.test ./internal/transferstate
CGO_ENABLED=0 go vet ./internal/transferstate ./internal/credentialvault
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o build/security-review/transferstate-windows-amd64.test.exe ./internal/transferstate
GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go test -c -o build/security-review/transferstate-windows-arm64.test.exe ./internal/transferstate
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go vet ./internal/transferstate
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -o build/security-review/credentialvault-linux-amd64.test ./internal/credentialvault
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c -o build/security-review/credentialvault-linux-arm64.test ./internal/credentialvault
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go vet ./internal/credentialvault
```

Commands used `GOMODCACHE=/Volumes/SSD/storage/data/go/pkg/mod` and
`GOCACHE=$PWD/build/security-review/go-cache`; `GOTMPDIR` and `TMPDIR` both used
`$PWD/build/security-review/go-tmp`. The native-vault opt-in was not enabled.

macOS executed ACL grant rejection despite unchanged private mode, replaced
identity/hardlink rejection, and malformed ACL-buffer tests. Windows fixtures
cover protected creation despite an Everyone-inheritable parent, sidecar
inheritance, foreign grants/owners, privileged owners, null/missing DACLs,
hardlinks, and reparse refusal. The reparse test explicitly skips only if the
native runner cannot create a synthetic symbolic link. Linux fixtures cover a
same-user Unix listener, mismatched expected UID with zero transmitted bytes
and socket closure, and closed/uninspectable sockets.

Windows and Linux binaries were cross-compiled here; their new tests still
require native CI execution. Darwin amd64 was also compile-only in this review.
These checks do not establish arbitrary filesystem support, installer migration,
live OAuth acceptance, resistance to same-user/admin modification after the
checks, or end-to-end cloud copy behavior.
