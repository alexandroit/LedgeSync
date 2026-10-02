# LedgeSync CLI and server copies

**0.1.0-alpha.5 candidate.** Adds saved sync pairs, approved pair copies,
opt-in automatic copies for servers and restore to a new folder (see
[Saved pairs, automatic copies and restore](#saved-pairs-automatic-copies-and-restore)).
The alpha.4 description below remains the published release.

**LedgeSync 0.1.0-alpha.4** provides manual Google Drive copies through the same
authorization, provider, filtering, approval and journal services as the desktop.
The [release's native CLI archives](https://github.com/alexandroit/LedgeSync/releases/tag/v0.1.0-alpha.4)
are published and publicly verified for macOS, Linux and Windows in AMD64/ARM64.
Ubuntu CLI DEBs are also published. The signed APT repository serves alpha.4;
public installation and removal passed on native amd64/arm64 in
[run 36965018881](https://github.com/alexandroit/LedgeSync/actions/runs/36965018881).
See the [repository setup](PLATFORMS.md#ubuntu-apt) before running
`sudo apt-get install ledgesync-cli`.
Historical alpha.3 CLI packages remain offline. An unconfigured developer build
reports `OAUTH_UNAVAILABLE` for online commands.

Application source `fcd578488d07f627372e7f5dd2221e162634bf05` passed all 16 jobs in
[build run 36963525743](https://github.com/alexandroit/LedgeSync/actions/runs/36963525743),
including configured native CLI packaging on all six targets. Its
[native-vault run 36963524884](https://github.com/alexandroit/LedgeSync/actions/runs/36963524884)
passed on those targets. Native builds and synthetic vault checks do not establish
Google consent, SSH return or vault access in every server-session configuration.
The Windows downloads have no trusted publisher signature, and Apple Developer
ID signing/notarization has not been supplied. Complete release, installer,
public APT and site evidence is linked from the
[platform release results](PLATFORMS.md#alpha4-native-validation-and-publication-gates).

## Connect and copy

Run as the operating-system user who owns the credential vault. These commands
require an interactive terminal for input, output and errors; piping confirmation
or redirecting online output is refused. Source examples below use folders relative
to the terminal's current directory; replace them with the folder you intend to
copy. On SSH, that directory is on the server.

```sh
ledgesync auth connect
ledgesync auth status
ledgesync copy --root "./local-folder" --destination root
```

`auth connect` opens Google's authorization page in your default system browser.
The browser returns to a temporary loopback listener in LedgeSync. Credentials
stay in the local native vault. `auth status` reads local metadata only and reports
`onlineVerified: false`; it does not check the grant online.

To select an existing My Drive parent in Google's browser Picker:

```sh
ledgesync copy --root "./local-folder" --destination picker
```

`--pick-destination` is an equivalent option. `--destination FOLDER_ID` works only
when that folder is already accessible to this application's `drive.file` grant;
copying a folder ID does not grant permission. The Picker validates that the
selected folder belongs to the already connected account. Shared drives and
shortcuts are unsupported.

For configured ignore policies, replace `--root` with `--config`:

```sh
ledgesync copy --config project.json --destination picker
```

The command displays the source, account reference, immutable destination ID,
included entries, actions, excluded count, warnings and expiration. Review them,
then type the complete displayed `planDigest` and press Enter. Any other answer,
EOF, interruption or expired approval stops the operation. There is no `--yes`.
The source cannot contain LedgeSync's settings or be inside that directory.

The included local root becomes one managed child folder inside the selected
parent, retaining files and empty subfolders. Ignore rules apply. Verified
unchanged copies are reused; changed files get a stable `.ledgesync-` suffix,
preserving both versions. Existing remote data is never overwritten or deleted.
Keep the process running until its status says `succeeded`, after verification
and durable journal finalization.

Ctrl+C or SIGTERM cancels and drains the operation. Completed remote objects
remain. Run the same copy command again, review its fresh preview and approve
to reconcile the saved IDs and continue. Incomplete files may restart because
upload-session URLs are never stored. Uncooperative network/FUSE filesystem reads
can delay cancellation; no unattended startup resumption is enabled.

## SSH and headless servers

A server needs both a usable native credential vault and a browser on your own
computer. Start in an interactive server terminal:

```sh
ledgesync auth connect --no-browser
```

LedgeSync prints a Google authorization URL and a command containing the temporary
port. In another terminal **on your computer**, replace `USER@SERVER` with your
normal SSH destination and run the displayed command, for example:

```sh
ssh -N -L 127.0.0.1:54321:127.0.0.1:54321 USER@SERVER
```

The port above is illustrative: use the actual port displayed for that attempt.
SSH forwarding must be allowed by the server. Then open the displayed Google URL
in your computer's browser and authorize. Keep the CLI and tunnel open until the
return completes. The attempt expires after three minutes. LedgeSync binds only
`127.0.0.1`; no firewall opening, public callback endpoint, copied OAuth code or
token file is needed. Close the tunnel when authorization finishes.

For an existing destination, repeat the browser/tunnel procedure when the Picker
opens; each attempt uses a new ephemeral port:

```sh
ledgesync copy --root "./source-folder" --destination picker --no-browser
```

With an already authorized folder ID or My Drive, `copy` itself does not open a
browser. Windows uses the same commands with a Windows source path, such as
`--root ".\local-folder"`.

## Native credentials and session requirements

- **Ubuntu:** a local D-Bus user session and an unlocked Secret Service default
  collection, such as GNOME Keyring. Installing `gnome-keyring` alone does not
  initialize or unlock it. Provision the user's normal native vault before
  connecting. Do not use an empty keyring password or pass passwords/tokens in
  command arguments, files, environment variables or shell history.
- **Windows:** the current user's Credential Manager. The logon session must
  have a credential set; Windows network logons may not. Console/RDP/SSH session
  behavior needs native acceptance on the intended server configuration.
- **macOS:** a native CGO-enabled CLI and the user's available Keychain.

A missing or locked vault stops authorization and copying. There is no plaintext
fallback or shared server token database. The current release does not install
or implement a background service, boot-time vault unlocking or scheduled jobs.
Close/wait for another LedgeSync authorization operation if `AUTH_BUSY` appears.
Application credentials use an OS-protected process lock; copy journals use a
separate writer lock. Neither lock file contains tokens.

To remove only this device's saved authorization:

```sh
ledgesync auth disconnect
```

Type `disconnect` when prompted. Drive files and remote Google authorization are
not deleted. Google-side revocation remains a separate confirmed desktop action.

## Saved pairs, automatic copies and restore

Saved pairs are shared with the desktop application (same private catalog and
journal). An approved desktop upload saves its pair automatically.

```sh
ledgesync pairs add --root "./local-folder" --destination root --name "Server data"
ledgesync pairs list
ledgesync copy --pair PAIR_ID
```

`copy --pair` previews the pair with its saved policy and requires the exact
digest, like `copy`. Changed approved files are skipped and reported (exit `3`);
files added after the preview wait for the next copy.

Automatic copies are off until you authorize them from a reviewed preview:

```sh
ledgesync automatic enable --pair PAIR_ID --every 15          # or add --watch
ledgesync automatic run                                       # due pairs, once
ledgesync automatic watch                                     # foreground loop
ledgesync automatic disable --pair PAIR_ID
```

`automatic enable` requires an interactive terminal and the preview's digest. The
authorization binds the account, destination, source identity, configuration,
ignore rules and conflict policy. `automatic run` and `automatic watch` execute
only authorized, unpaused pairs and only create-only work. A changed account,
destination, source volume, configuration or rule set, or earlier copies missing
in Drive, pauses the pair until a new authorization; an offline network or an
unmounted source is retried later. Nothing is installed as a service: use a
cron entry or a user systemd timer that runs `ledgesync automatic run` in a
session where the credential vault is available (`automatic run` exits `6` with
`AUTOMATION_ATTENTION` when a pair is paused or waiting).

Restore downloads a pair's verified copy into a new empty folder:

```sh
ledgesync restore --pair PAIR_ID --to "./restored-copy"
```

Only journal-recorded, verified objects are downloaded; each file must match
Drive's MD5 and the recorded SHA-256. The target must be empty and outside the
source and LedgeSync's private data. Existing files are never overwritten; items
missing or changed in Drive are reported (exit `3`).

## Offline inspection and exit status

`browse`, `explain`, `config validate`, `plan`, `plan inspect`, `capabilities` and
help/version remain local inspection commands. Their fake-destination plan files
cannot be applied to Drive. Online copy approval is created and consumed in one
process; saved transfer history assists recovery, not automatic approval.

Exit codes: `0` completed; `2` invalid arguments/configuration; `3` partial
(approved files changed after preview, or restore items unavailable); `4`
conflict or review required; `6` unavailable capability, vault/provider/output
failure or other stopped operation; `130` canceled or unconfirmed. Errors are
JSON objects with a stable `code` and a redacted `message`. Progress JSON contains
safe status and escaped paths, never tokens or upload-session URLs. Live Google,
native installation and headless-session acceptance are recorded separately in
the [acceptance checklist](research/DRIVE_UPLOAD_ACCEPTANCE.md) and
[platform guide](PLATFORMS.md).
