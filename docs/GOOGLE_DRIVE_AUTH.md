# Connect Google Drive

Official LedgeSync desktop releases starting with alpha.3 include the project's
Google OAuth Desktop client. You do not create a Google Cloud project, download
credentials, import JSON or paste tokens into the app.

**Release status:** [alpha.3 is published](https://github.com/alexandroit/LedgeSync/releases/tag/v0.1.0-alpha.3)
with the bundled Desktop client. The steps below apply to that release.

The **alpha.4 source/local candidate is not yet published**. It adds manual
folder uploads and native Google destination selection, together with the
[OAuth hardening follow-up](research/OAUTH_SECURITY_HARDENING.md). It renames
**Disconnect account** to **Disconnect from this device** and adds the separate,
confirmed remote revocation action described below. Published alpha.3 installers
do not contain those follow-up changes or a file-transfer executor. The owner
reports production OAuth, enabled Google Picker API and a successful account
connection; live acceptance of the new Picker/upload flow remains pending.

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
for fresh consent after expiry or revocation. **Disconnect account** in alpha.3,
or **Disconnect from this device** in the candidate, must be
used before switching accounts. Authorization can expire or be revoked; a
saved refresh token does not promise permanent access.

## Updating from alpha.2

If the saved authorization belongs to a different OAuth client, LedgeSync shows
**Authorization update required** and retains the existing account. Explicitly
choose **Disconnect account**, then **Connect Google Drive** to authorize the
bundled client. The app never silently moves tokens between clients or connects
a different account. A failed disconnect leaves the previous record intact.

## Copy a folder in the alpha.4 candidate

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

See the [candidate acceptance checklist](research/DRIVE_UPLOAD_ACCEPTANCE.md)
for disposable-fixture verification and unimplemented capabilities.

## Storage and scope

Refresh tokens and account identity are stored in the operating-system vault;
access tokens exist only in process memory. No token is sent to ledgesync.com,
stored in project configuration or exposed to the frontend or diagnostics.
The website/server distributes software and does not mediate authorization.

| System | Credential store |
|---|---|
| macOS | Keychain |
| Windows | Credential Manager, current user |
| Ubuntu desktop | Secret Service over the user's local D-Bus session |

Ubuntu requires a running Secret Service implementation such as GNOME Keyring
and an unlocked default collection. An SSH/headless session alone does not
provide this. There is no plaintext fallback. Use one running app instance;
credential operations are serialized within that process only.

The candidate also keeps a per-user SQLite transfer journal outside source
folders. It records approved plans, provider IDs, checksums and previously
observed policy sources; it contains no OAuth tokens or upload-session URLs.
Transfer-state writes have a process lock. That lock does not replace the
separate, currently process-local credential-operation guard. Journal paths and
filenames may be private, so do not publish the database as a diagnostic dump.

The requested `https://www.googleapis.com/auth/drive.file` scope covers files
created by or explicitly made available to the app. It does not grant access
to every existing Drive file or recursively grant a selected folder's children.
See Google's [scope description](https://developers.google.com/workspace/drive/api/guides/api-specific-auth).

Published alpha.3 reads account identity but does not browse cloud files, select a
remote root, upload, download, overwrite or delete Drive files. The Files screen
still previews an empty simulated destination. CLI previews remain offline.
The unreleased source additionally supports read-only `ledgesync auth status`
in a [configured native build](OAUTH_BUILD.md); this does not check the grant online.
The candidate's desktop upload workflow adds scoped destination selection and
managed-file checks, not a browser over every pre-existing Drive file. The CLI
has no upload/apply command yet.

**Disconnect account** removes the local account credentials while retaining
client configuration. It does not revoke Google's grant or delete Drive files.
You can revoke permission in [Google Account connections](https://myaccount.google.com/connections).
Disconnect before uninstalling if you want to remove the saved local access.

## Local disconnection and remote revocation in the updated source

**Disconnect from this device** cancels the active account operation, waits for
its pending credential writes and removes the local account tokens. In the
candidate, it first cancels and drains any active transfer. It keeps
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

Automated tests use synthetic credentials and local fake endpoints. Earlier
native-vault CI used disposable accounts. The owner has reported successful live
connection; that is distinct from automated evidence and does not prove new
Picker selection, uploads, refresh recovery or remote revocation. Those new live
acceptance checks remain pending.
See [current evidence](17_AGENT_HANDOFF_AND_STATUS.md).
