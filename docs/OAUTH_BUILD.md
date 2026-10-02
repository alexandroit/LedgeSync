# OAuth configuration for maintainers

End users authorize through **Connections → Connect Google Drive**. Only the
maintainer creates the Google project and Desktop client used by release builds.
This file describes build configuration, not an end-user onboarding requirement.

## Google project

Enable the Google Drive API. In Google Auth Platform, configure Branding,
Audience and the exact `https://www.googleapis.com/auth/drive.file` scope.
Create a client of type **Desktop app**, then download its `installed` JSON.
Web and service-account credentials are not accepted. Testing requires the
owner to add the chosen test accounts; public availability and branding require
completion of the applicable Google publication and verification steps.

References: [native OAuth](https://developers.google.com/identity/protocols/oauth2/native-app),
[consent configuration](https://developers.google.com/workspace/guides/configure-oauth-consent).

## Local native build

Before the normal desktop build, run:

```sh
python3 tools/configure_oauth_client.py --client-file /absolute/path/to/desktop-client.json
```

The helper validates the downloaded Desktop client and creates the ignored
`internal/connections/oauth_client_generated.go` with restrictive permissions.
It refuses invalid credentials and existing output. It never prints the client
contents. Run `python3 tools/configure_oauth_client.py --clean` after building;
do not commit the generated file or the original JSON. Build without it to test an explicitly unconfigured developer
app. Normal tests use synthetic configuration and access no personal vault.

## Official CI builds

The canonical repository stores the Desktop JSON in the Actions secret
`GOOGLE_DESKTOP_CLIENT_JSON`. Only trusted main-branch push/manual desktop jobs
receive it, scoped to the configuration helper step. Pull requests, forks and
other branches do not receive this value. Official main builds fail if the
configuration is missing or invalid. Generated source is removed after the
native build, and desktop build caches are not uploaded.

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
