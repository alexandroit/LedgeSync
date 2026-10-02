# Connect Google Drive

Official LedgeSync desktop releases starting with alpha.3 include the project's
Google OAuth Desktop client. You do not create a Google Cloud project, download
credentials, import JSON or paste tokens into the app.

**Release status:** [alpha.3 is published](https://github.com/alexandroit/LedgeSync/releases/tag/v0.1.0-alpha.3)
with the bundled Desktop client. The steps below apply to that release.

The current source also contains an unreleased
[OAuth hardening follow-up](research/OAUTH_SECURITY_HARDENING.md). It renames
**Disconnect account** to **Disconnect from this device** and adds the separate,
confirmed remote revocation action described below. Published alpha.3 installers
do not yet contain those follow-up changes.

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
for fresh consent after expiry or revocation. **Disconnect account** must be
used before switching accounts. Authorization can expire or be revoked; a
saved refresh token does not promise permanent access.

## Updating from alpha.2

If the saved authorization belongs to a different OAuth client, LedgeSync shows
**Authorization update required** and retains the existing account. Explicitly
choose **Disconnect account**, then **Connect Google Drive** to authorize the
bundled client. The app never silently moves tokens between clients or connects
a different account. A failed disconnect leaves the previous record intact.

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

The requested `https://www.googleapis.com/auth/drive.file` scope covers files
created by or explicitly made available to the app. It does not grant access
to every existing Drive file or recursively grant a selected folder's children.
See Google's [scope description](https://developers.google.com/workspace/drive/api/guides/api-specific-auth).

This release reads account identity but does not browse cloud files, select a
remote root, upload, download, overwrite or delete Drive files. The Files screen
still previews an empty simulated destination. The CLI remains offline.

**Disconnect account** removes the local account credentials while retaining
client configuration. It does not revoke Google's grant or delete Drive files.
You can revoke permission in [Google Account connections](https://myaccount.google.com/connections).
Disconnect before uninstalling if you want to remove the saved local access.

## Local disconnection and remote revocation in the updated source

**Disconnect from this device** cancels the active account operation, waits for
its pending credential writes and removes the local account tokens. It keeps
Google's grant and the client configuration. If the vault is unavailable or the
operation cannot finish within its deadline, the app reports failure; it does
not pretend credentials were removed. No synchronization jobs exist in this
alpha yet.

**Revoke access on Google** first opens a confirmation dialog for the displayed
account. **Cancel** is the default. Review the warning before confirming:
revocation can also remove this account's authorizations for other applications
whose OAuth clients belong to the same Google Cloud project. The confirmation
is bound to that account and cannot silently retarget a different saved account.
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

The owner controls the Google OAuth project's testing audience and publication.
While it is in Testing, only configured test accounts may be able to authorize.
A Google access-denied or unverified-app message needs owner configuration;
LedgeSync does not bypass Google's restrictions or ask for broader permissions.

An unconfigured developer build displays an unavailable connection state instead
of asking users to supply credentials. Maintainers can follow
[OAuth build configuration](OAUTH_BUILD.md).

Automated tests use synthetic credentials, local fake endpoints and disposable
CI vaults. They do not establish actual Google consent, refresh or revocation.
Live acceptance remains pending until the owner completes the browser flow.
See [current evidence](17_AGENT_HANDOFF_AND_STATUS.md).
