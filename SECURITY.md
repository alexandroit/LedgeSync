# Security policy

This project is a developer alpha. The alpha.4 desktop candidate authorizes a
Google Drive account through the publisher's Desktop OAuth client and executes
explicitly approved, create-only folder copies. Tokens stay behind native OS
credential storage and never enter project configurations, transfer journals,
the frontend or the LedgeSync website/server. See
[authorization setup and limits](docs/GOOGLE_DRIVE_AUTH.md). Only the current
development version receives fixes; there is no production support commitment.

macOS uses Keychain, Windows uses Credential Manager, and Ubuntu uses the
logged-in user's Secret Service collection. An unavailable or locked vault
blocks authorization; there is no plaintext credential fallback. Linux checks
the Unix socket's kernel-reported owner before authenticating to the user bus.

The local SQLite transfer journal contains paths, account references, approved
plans and checksums. It is protected separately from credentials: owner-only
POSIX permissions on Linux, native ACL inspection on macOS, and owner-bound
Windows DACLs. Unsafe existing permissions, symlinks, hardlinks, unrecognized
schemas and invalid operation records fail closed instead of being silently
repaired or discarded. The journal must remain outside the upload source.

These controls do not protect against an administrator, a compromised OS or
malicious code already running as the same user. Distribution signatures and
notarization are a separate release boundary; check the platform guide for the
actual status of a downloaded build. Automated tests use synthetic credentials
and files, not personal Google accounts.

Please report suspected vulnerabilities through GitHub's private vulnerability
reporting for this repository. Do not include tokens or private file contents.
If private reporting is unavailable, open an issue requesting a private contact
without describing the vulnerability or attaching sensitive data.

Reports concerning path escapes, ignored-file disclosure, stale plan execution,
unintended overwrites, and credential handling are especially relevant.
