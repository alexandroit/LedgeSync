# Connect Google Drive

The LedgeSync desktop app supports Google Drive authorization through your
system browser. This developer alpha requires your own Google OAuth **Desktop
app** client. No shared LedgeSync client is bundled. The downloaded client JSON
configures the app; it is not an access token or a service-account key.

## Create the client once

1. Open [Google Cloud Console](https://console.cloud.google.com/), select or
   create your project, and enable the **Google Drive API** in the API Library.
2. Open **Google Auth Platform** and complete its initial setup. Set the app name
   to **LedgeSync**, choose your support/contact email, and configure the audience
   appropriate to your account. For an External app in Testing, add your Google
   account under **Audience → Test users**.
3. Under **Data Access**, add only
   `https://www.googleapis.com/auth/drive.file`. LedgeSync requests that exact
   scope. Do not add full-Drive access for this alpha.
4. Under **Clients**, choose **Create client → Desktop app**. Name it
   **LedgeSync Desktop**, create it, and download its JSON configuration. Do not
   select Web application or Service account. Desktop loopback redirects use a
   temporary local port; no server, public callback URL or DNS change is needed.
5. Keep that downloaded file on your own computer. Do not put it in a project
   being synced, commit it, paste it into chat or attach it to an issue.

Google's console labels can change. The official
[installed-app OAuth guide](https://developers.google.com/identity/protocols/oauth2/native-app)
describes enabling APIs, creating credentials, browser consent and loopback
callbacks. Your Google project settings determine which accounts can authorize
it. A public shared-client rollout requires a separate owner configuration and
any verification Google requires for that rollout.

## Authorize in LedgeSync

1. Open **Connections → Google Drive**.
2. Choose **Import OAuth client JSON** and select the downloaded file in the native
   file picker. The application validates Google's Desktop configuration and
   stores it in the operating-system credential vault.
3. Choose **Connect Google Drive**. Your default browser opens Google's account
   selection and permission screen. Sign in there and approve access if desired.
   LedgeSync never asks for your Google password or a pasted access token.
4. Return to LedgeSync. The app displays the authorized account and its connection
   status. **Check connection** performs a read-only account check and renews an
   expired access token when possible. **Cancel authorization** stops a pending authorization;
   the browser tab may remain open and can be closed.

Only one account is supported. Disconnect before importing a different client
or changing accounts. If authorization expires or is revoked, **Reconnect Google Drive**
requires the same account; selecting another account does not silently replace
the saved identity.

## What access means in this alpha

Authorization obtains and stores a refresh token and reads the account's display
name, email and stable Drive identity. Access tokens exist only in application
memory and are refreshed when needed. No token is exposed to the web frontend,
project configuration, exported plans or diagnostics.

The requested `drive.file` scope covers files created by or explicitly shared
with this app, not every existing file in your Drive. Selecting a parent folder
does not automatically grant access to its existing children. See Google's
[scope description](https://developers.google.com/workspace/drive/api/guides/api-specific-auth).

This release does not browse cloud files, select a remote root, upload, download,
overwrite or delete anything in Drive. The Files screen still previews an empty
simulated destination. Account authorization does not apply a transfer plan or
start background synchronization. The headless CLI remains an offline preview
tool and does not implement account authorization.

## Storage and disconnect

| System | Credential store |
|---|---|
| macOS | Keychain |
| Windows | Credential Manager, current user |
| Ubuntu desktop | Secret Service over the user's local D-Bus session |

Unlock the normal login credential store if the app reports it unavailable.
Ubuntu requires a running Secret Service implementation such as GNOME Keyring
and an unlocked default collection; an SSH/headless session alone does not
provide this. There is no plaintext file fallback.

**Disconnect account** removes the local account credentials while preserving the
imported client configuration for a future connection. It does not delete Drive
files or revoke Google's permission grant. You can separately revoke that grant
in [your Google Account connections](https://myaccount.google.com/connections).
Uninstalling an application does not necessarily remove OS-vault entries;
disconnect before uninstalling if you want to remove this local account access.

## Verification boundary

Automated tests use synthetic credentials, a fake token/API server and a local
loopback callback. Native vault CI uses disposable runner accounts only. No
personal Google account or existing token was used to develop these tests.
Real consent/refresh/revocation acceptance remains pending because the owner
has not yet created a Desktop OAuth client. See
[OAuth source review](research/OAUTH_SOURCE_REVIEW.md),
[vault source review](research/OAUTH_VAULT_REVIEW.md) and
[current evidence](17_AGENT_HANDOFF_AND_STATUS.md).
