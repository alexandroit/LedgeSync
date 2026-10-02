# OAuth configuration for maintainers

End users authorize through **Connections → Connect Google Drive**. Only the
maintainer creates the Google project and Desktop client used by release builds.
This file describes build configuration, not an end-user onboarding requirement.

## Google project

Enable the Google Drive API and Google Picker API in the same project. The
native folder picker uses the existing Desktop OAuth client; no web client,
browser-visible access token or API key is required. In Google Auth Platform, configure Branding,
Audience and the exact `https://www.googleapis.com/auth/drive.file` scope.
Create a client of type **Desktop app**, then download its `installed` JSON.
Web and service-account credentials are not accepted. Testing requires the
owner to add the chosen test accounts; public availability and branding require
completion of the applicable Google publication and verification steps.

References: [native OAuth](https://developers.google.com/identity/protocols/oauth2/native-app),
[consent configuration](https://developers.google.com/workspace/guides/configure-oauth-consent).

## Local native build

Before the normal desktop build, run from the repository root. Replace the
relative example below with the location of the existing private Desktop client
file outside the checkout; this example directory is not asserted to exist.
Do not copy the credential file into the repository.

```sh
python3 tools/configure_oauth_client.py --client-file ../private-build-input/desktop-client.json
```

The helper validates the downloaded Desktop client and creates the ignored
`internal/connections/oauth_client_generated.go` with restrictive permissions.
It refuses invalid credentials and existing output. It never prints the client
contents. Run `python3 tools/configure_oauth_client.py --clean` after building;
do not commit the generated file or the original JSON. Build without it to test an explicitly unconfigured developer
app. Normal tests use synthetic configuration and access no personal vault.

The helper packages only the five fields required by the strict runtime client
parser: client ID, client secret, authorization/token endpoints and loopback
redirect list. The source project's `project_id` and certificate URL are validated
but omitted from generated source and binaries. It does not copy the raw JSON.

The alpha.4 native CLI uses the same generated source with the `oauth` build
tag. Package it on its matching operating system/architecture using, for example,
`python3 tools/package_cli.py --platform darwin/arm64 --native --require-oauth-client`.
macOS requires CGO for Keychain; Linux and Windows use their native vault backends
without CGO. Clean the generated source after both builds. `ledgesync auth status`
reads connection metadata; it does not verify the grant online or open a browser.
Browser consent and approved copy commands are separate explicit operations.
Portable developer builds without `--native` remain unconfigured and cannot
package an injected client. They fail closed for online commands.

## Official CI builds

The canonical repository stores the Desktop JSON in the Actions secret
`GOOGLE_DESKTOP_CLIENT_JSON`. Only trusted main-branch push/manual native jobs
receive it, scoped to the configuration helper step. Pull requests, forks and
other branches do not receive this value. Official main builds fail if the
configuration is missing or invalid. Generated source is removed after the
desktop and CLI builds, and native build caches are not uploaded. Each of the
six target jobs packages its CLI before removing the generated source, so the
macOS CLI retains Keychain support instead of using a cross-compiled stub.

Do not pass client JSON in compiler flags: build metadata and command logs can
retain those values. Never provide access tokens, refresh tokens or account
credentials to this build process. The helper rejects unknown fields and
non-Google endpoints; tests exercise redacted failures and filesystem guards.

## Security boundary

A Desktop OAuth client is a public client. Its identifier and any client secret
bundled in a native binary are recoverable through inspection; encryption or
obfuscation cannot make them confidential on the user's machine. Keeping the
original JSON out of source history avoids unnecessary dissemination but does
not change that OAuth trust model. PKCE/state, explicit user consent and
individual account tokens protect the authorization flow.

The native app communicates directly with Google, using a temporary IPv4
loopback callback. ledgesync.com receives no account tokens and operates no
OAuth token relay. Google handles authentication; each machine's OS vault holds
its refresh token. A compromised endpoint can still expose that user's access.
The distribution server and installers need separate integrity protections;
trusted publisher signing/notarization remains an open release limitation.

Changing the bundled client does not silently migrate an existing authorization.
Users explicitly disconnect the old account record before authorizing the new
client. The Google project/client must remain available for existing installs;
replacing a build does not rotate credentials inside older distributed binaries.
