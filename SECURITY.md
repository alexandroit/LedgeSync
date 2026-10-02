# Security policy

This project is an experimental developer alpha. The desktop application can
authorize a Google Drive account using an owner-provided Desktop OAuth client;
it does not execute transfer plans. Tokens stay behind native OS credential
storage and never enter project configurations or the frontend. See
[authorization setup and limits](docs/GOOGLE_DRIVE_AUTH.md). Only the current
development version receives fixes; there is no production support commitment.

Please report suspected vulnerabilities through GitHub's private vulnerability
reporting for this repository. Do not include tokens or private file contents.
If private reporting is unavailable, open an issue requesting a private contact
without describing the vulnerability or attaching sensitive data.

Reports concerning path escapes, ignored-file disclosure, stale plan execution,
unintended overwrites, and credential handling are especially relevant.
