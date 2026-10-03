# Connect Google Drive

LedgeSync **0.1.0-alpha.7** connects directly to Google and keeps folders in
**two-way sync** with Google Drive through its desktop and native CLI. One-time
approved copies remain available. Official builds include the project's Google
OAuth Desktop client. You do not create a Google Cloud project, download
credentials, import JSON or paste tokens into the app.

## Access requested (alpha.7 and later)

Sync requests **full Google Drive access**
(`https://www.googleapis.com/auth/drive`; owner decision, see
[ADR-034](12_ADR_DECISIONS.md)). Google shows this as permission to see, edit,
create and delete all of your Google Drive files. Full access is needed because
two-way sync must see and download files that you add to a synced folder
through the Drive website, your phone or other apps; the earlier per-file
`drive.file` scope only covers files the app created. LedgeSync uses the access
only for the folders you sync or copy and for listing folders when you choose
where a synced folder lives.

- **Existing connections reconnect once.** A connection made by alpha.6 or
  earlier holds only `drive.file`. After updating, Connections shows
  **Reconnect** (or `ledgesync auth connect`). Approve in the browser.
- **Owner setup for the OAuth client.** In the Google Cloud Console project of
  the bundled client, open **Google Auth Platform → Data Access → Add or remove
  scopes** and add `.../auth/drive`. Until Google verifies the restricted scope,
  Google shows an "unverified app" screen at consent (**Advanced → Go to
  LedgeSync**). Unverified apps are limited to 100 users. Public distribution
  needs Google's restricted-scope verification; see
  [Google verification](GOOGLE_VERIFICATION.md).


## Connect your account

1. Open **Connections → Google Drive** and choose **Connect Google Drive**.
2. Your system browser opens Google's account selection and permission screen.
   Select your account, review the requested access and authorize it if desired.
   Your Google password is entered only on Google's page.
3. The authorization response returns directly to the running LedgeSync app
   through a temporary local callback. LedgeSync requests that its window be
   shown again after connection succeeds; your operating system controls focus.
   You can close the browser tab and return manually if necessary.
4. LedgeSync displays the connected account. **Check connection** performs a
   read-only account check and renews an expired access token when possible.
   **Cancel authorization** stops a pending request; the browser tab may remain.

Only one account is supported. **Reconnect Google Drive** asks the same account
for fresh consent after expiry or revocation. Use **Disconnect from this device**
before switching accounts. Authorization can expire or be revoked; a
saved refresh token does not promise permanent access.

## Updating from alpha.2

If the saved authorization belongs to a different OAuth client, LedgeSync shows
**Authorization update required** and retains the existing account. Explicitly
choose **Disconnect from this device**, then **Connect Google Drive** to authorize the
bundled client. The app never silently moves tokens between clients or connects
a different account. A failed disconnect leaves the previous record intact.

## Copy a folder

1. Choose a local folder, or open a project configuration, and connect Google
   Drive using the flow above.
2. In Files, choose **Use My Drive** or **Choose existing Drive folder**.
   The existing-folder option opens Google's native Picker in your system
   browser. Select one writable folder in My Drive, authorize it and return to
   LedgeSync. Canceling keeps the previous destination; preview again before
   uploading. Shared drives are not supported by this workflow.
3. Choose **Preview folder upload** and review the account, destination ID,
   included hierarchy, exclusions and planned actions. Choose **Upload folder**
   to approve that exact preview. Selecting files or a destination alone never
   starts a transfer.
4. Wait for **Folder upload verified**, then choose **Open destination folder on
   Google Drive**. Progress counts verified content, including already verified
   copies that did not need uploading again.

The local root becomes a managed child folder inside the chosen parent; its
children are not flattened into the parent. Included files and empty directories
retain their structure. Ignore rules still apply. Subsequent previews verify and
skip unchanged copies; changed files keep both versions with a stable
`.ledgesync-` suffix. Existing cloud files are never overwritten or deleted.

Keep the application open while uploading. **Cancel upload** stops the transfer
and leaves completed files in Drive. A fresh preview reconciles the saved object
IDs before continuing; an unfinished file may restart from the beginning after
an application restart. Closing during an active operation defaults to **Keep
Open**. There is no scheduler, watcher or automatic resumption on startup.

See the [acceptance checklist](research/DRIVE_UPLOAD_ACCEPTANCE.md)
for disposable-fixture verification and unimplemented capabilities.

## CLI connection and server copies in alpha.4

In an interactive terminal, use `ledgesync auth connect`, followed by
`ledgesync copy --root "./local-folder" --destination picker`. Review the complete
preview and type its exact digest before uploading. `--destination root` selects
My Drive; a folder ID works only when already authorized to this application.
The same account binding, filtering, journal and verification rules apply as in
the desktop. Online commands do not accept redirected approval or unattended
`--yes` execution.

For an SSH server, `auth connect --no-browser` displays Google's consent URL and
the temporary loopback forwarding command to run on your own computer. The
Picker also supports `--no-browser`; each attempt needs its displayed port.
Credentials remain in the server user's native vault. A tunnel supplies the
browser return path; it does not supply or unlock a missing vault. See the
[CLI/server guide](CLI.md) for session requirements, cancellation and recovery.

## Storage and scope

Refresh tokens and account identity are stored in the operating-system vault;
access tokens exist only in process memory. No token is sent to ledgesync.com,
stored in project configuration or exposed to the frontend or diagnostics.
The website/server distributes software and does not mediate authorization.

| System | Credential store |
|---|---|
| macOS | Keychain |
| Windows | Credential Manager, current user |
| Ubuntu | Secret Service over the user's local D-Bus session |

Ubuntu requires a running Secret Service implementation such as GNOME Keyring
and an unlocked default collection. An SSH/headless session alone does not
provide this. There is no plaintext fallback. Protected native process locks
serialize credential operations across the GUI and CLI. A competing process
reports that the connection is in use and requires a fresh status check.

Native filesystem protections also apply to the local journal and process locks:
owner/permission checks on Linux, extended-ACL checks on macOS and protected
owner-only DACLs on Windows. Linux additionally checks the D-Bus socket peer's
user identity before authentication. A missing native protection fails closed;
these checks do not make a compromised operating-system user or administrator
unable to access that user's files or credentials.

LedgeSync also keeps a per-user SQLite transfer journal outside source
folders. It records approved plans, provider IDs, checksums and previously
observed policy sources; it contains no OAuth tokens or upload-session URLs.
Transfer-state writes and credential operations have separate native process
locks. Neither lock contains credentials. Journal paths and
filenames may be private, so do not publish the database as a diagnostic dump.

The requested `https://www.googleapis.com/auth/drive` scope is full Drive
access (see above). LedgeSync reads and changes only the folders you sync or
copy, plus folder names in the location browser. See Google's
[scope description](https://developers.google.com/workspace/drive/api/guides/api-specific-auth).

The alpha.4 native CLI supports explicit browser consent and interactive `copy`
through the same services as the desktop. `auth status` reads safe local metadata
without checking the grant online. See [CLI usage](CLI.md) and [native build
configuration](OAUTH_BUILD.md). Both interfaces support scoped destination
selection and managed-file checks, not a browser over every pre-existing Drive
file. The offline `plan` command remains inspection-only.

Disconnect before uninstalling if you want to remove saved local access. You can
also review permission in [Google Account connections](https://myaccount.google.com/connections).

## Local disconnection and remote revocation

**Disconnect from this device** cancels the active account operation, waits for
its pending credential writes and removes the local account tokens. In the
alpha.4 desktop, it first cancels and drains that application's active transfer. It keeps
Google's grant and the client configuration. If the vault is unavailable or the
operation cannot finish within its deadline, the app reports failure; it does
not pretend credentials were removed. Completed Drive files are preserved.

**Revoke access on Google** first opens a confirmation dialog for the displayed
account. **Cancel** is the default. Review the warning before confirming:
revocation can also remove this account's authorizations for other applications
whose OAuth clients belong to the same Google Cloud project. The confirmation
is bound to that account and cannot silently retarget a different saved account.
Only confirming the warning cancels/drains an active transfer before revocation;
canceling the dialog leaves it running.
Google describes the [shared-project revocation effect](https://developers.google.com/identity/protocols/oauth2/native-app#tokenrevoke).

After Google confirms revocation, the app removes local credentials. If that
local cleanup fails, unlock the vault and choose **Disconnect from this device**
to finish; the app blocks further use of that grant in the running process.
Complete cleanup before closing the app because that failure marker is not
durable when the vault cannot be written. A network failure may leave the remote
result unknown; the app retains the local record and never retries revocation
automatically. Neither action deletes Drive files.

The app refuses remote revocation of an earlier, different OAuth client's saved
grant. Use local disconnection, or review that earlier grant separately in
Google Account settings after considering its effect on other applications.

## Availability and verification

The owner controls the Google OAuth project's audience and publication and has
reported that the project is in Production. A Testing configuration would limit
authorization to its configured test audience.
A Google access-denied or unverified-app message needs owner configuration;
LedgeSync does not bypass Google's restrictions or ask for broader permissions.

An unconfigured developer build displays an unavailable connection state instead
of asking users to supply credentials. Maintainers can follow
[OAuth build configuration](OAUTH_BUILD.md).

Application source `fcd578488d07f627372e7f5dd2221e162634bf05` passed all 16 jobs in
[build run 36963525743](https://github.com/alexandroit/LedgeSync/actions/runs/36963525743); the six native desktop jobs also packaged the CLI with the
publisher configuration and removed generated configuration afterward.
[native-vault run 36963524884](https://github.com/alexandroit/LedgeSync/actions/runs/36963524884) passed synthetic vault lifecycle checks on macOS,
Ubuntu and Windows, each for AMD64 and ARM64. Tests use synthetic credentials,
local fake endpoints and disposable runner accounts, not personal Google data.

The owner has reported successful live connection. That report and the automated
results do not prove the new Picker selection, folder upload/recovery, remote
revocation or SSH consent return. Those live acceptance checks remain pending.
Native installer lifecycle, public APT installation and site publication passed;
trusted Apple/Windows publisher signing remains unavailable. Their exact evidence
and boundaries are in the [release results](PLATFORMS.md#alpha4-native-validation-and-publication-gates).
See [current evidence](17_AGENT_HANDOFF_AND_STATUS.md).
