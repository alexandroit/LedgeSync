# Platform builds and installation

LedgeSync **0.1.0-alpha.3** includes one-click Google Drive authorization with
a bundled Desktop client.
The desktop and CLI share the local policy/preview engine; the headless CLI
remains offline and has no account-authorization workflow. No package installs
a service, schedules jobs, connects an account automatically, uploads files or
enables deletion. Connecting requires an explicit click and browser consent.

[Alpha.3 is published](https://github.com/alexandroit/LedgeSync/releases/tag/v0.1.0-alpha.3).
Evidence for this release and the previous alpha.1/alpha.2 releases is recorded
separately below.

The current **alpha.4 source/local candidate** adds manual folder uploads,
native Google folder selection and [OAuth hardening](research/OAUTH_SECURITY_HARDENING.md).
It is not yet published. The alpha.3 download links and historical validation
below do not establish those new capabilities; existing release bytes and tags
are unchanged. See the [candidate acceptance checklist](research/DRIVE_UPLOAD_ACCEPTANCE.md).

## Alpha.4 candidate behavior and release boundary

The desktop candidate connects through the same publisher Desktop client and
exact `drive.file` scope. It can select My Drive or an existing parent using
Google's system-browser Picker, preview the local folder and upload only after
the user approves the exact plan. The local root becomes a managed child folder;
included empty directories and nested files retain their structure. Verified
copies are reused, changed files keep both versions, and existing cloud objects
are not overwritten or deleted. Source files are read-only.

Transfer history and observed rule sources use a private per-user SQLite journal
outside source roots, without tokens or upload-session URLs. Missing previously
observed rules block uploads across restarts. Cancellation preserves completed
files; a fresh preview reconciles known IDs before resuming. Session URLs remain
in memory, so an unfinished file may restart after the process exits. The native
close prompt defaults to **Keep Open** during a transfer.

No scheduler, file watcher, background service, shared-drive upload, download,
mirror deletion or bidirectional synchronization is enabled. CLI previews remain
offline; the source adds read-only `ledgesync auth status` for configured native
builds, but no CLI copy/apply command. The current public CLI packages remain
the alpha.3 artifacts described below.

The owner reports Production OAuth, enabled Picker API and successful account
connection. New live Picker/upload acceptance, candidate native installer checks,
and publication are separate gates. A local build or mock test does not satisfy
them; use the current handoff for evidence as it becomes available.

## Target matrix

| System | Architecture | Deliverable | Validation gate |
|---|---|---|---|
| macOS 13+ | Apple Silicon / ARM64 | DMG containing native `.app`; separate CLI | Native build and local smoke; CI on macOS 15 |
| macOS 13+ | Intel / x64 | DMG containing native `.app`; separate CLI | Native CI on macOS 15 Intel |
| Ubuntu 24.04 LTS | AMD/Intel x64 and ARM64 | APT `ledgesync` desktop package | Native package and installation CI for each architecture |
| Ubuntu Server 24.04 | AMD/Intel x64 and ARM64 | APT `ledgesync-cli` | Same Linux CLI; no graphical libraries required |
| Windows 11 | AMD/Intel x64 and ARM64 | Graphical setup `.exe`; separate CLI | Native installer lifecycle CI; ARM64 runner uses Windows 11 |
| Windows Server 2022 / 2025 | x64 | CLI | Native core tests on both Server runners |
| Windows Server 2016 / 2019 | x64 | CLI candidate | Go runtime baseline permits these; installation/runtime acceptance pending |

The `amd64` name means the same 64-bit architecture on AMD and Intel processors.
ARM means ARM64 in this project; there are no 32-bit packages. Windows Server
desktop use is not an acceptance target; use the CLI for Server Core/headless
systems. No macOS Server-specific package or operating-system service is required.

## Alpha.3 authorization and validation boundary

Read [Google Drive connection](GOOGLE_DRIVE_AUTH.md). Official alpha.3 builds
include the publisher's Desktop client. Choose **Connections → Connect Google
Drive** and authorize the limited `drive.file` scope in your system browser.
End users do not create clients or import JSON. Only one account is supported.
Connecting, checking, reconnecting and disconnecting do not browse or transfer
cloud files; every file preview still uses a simulated empty destination.

Credentials use macOS Keychain, Windows Credential Manager or Ubuntu Secret
Service. Ubuntu requires an active graphical user's D-Bus session and an unlocked
default credential collection, such as GNOME Keyring. An SSH/headless login is
not sufficient for desktop authorization, and there is no plaintext fallback.
The `ledgesync-cli` package does not require a keyring or connect to Google.

Local synthetic OAuth and frontend tests do not establish real Google consent,
refresh or revocation. Native credential storage was separately tested on
disposable runner accounts as recorded below.
The owner supplied a Desktop client for alpha.3 and subsequently reported a
successful real account connection. No personal account was used in automated
tests; the new candidate's Picker/upload workflow still needs live acceptance.

## Observed alpha.3 release results

Application/tag source: `4da377c311b78a99b1a9fde1127d77e21e6e05ec`.
[Build run 36953803971](https://github.com/alexandroit/LedgeSync/actions/runs/36953803971)
passed all 16 jobs, including 22 frontend tests. All six desktop jobs completed
publisher-client injection, native compilation and generated-source cleanup.
A separate private byte comparison confirmed the supplied Desktop client in all
six released graphical binaries and its absence from all six CLI binaries;
client values were not printed or recorded in the report. This does not establish
live Google authorization. [Vault run 36953805903](https://github.com/alexandroit/LedgeSync/actions/runs/36953805903)
passed synthetic create/read/update/delete on all six native OS/architecture
runners. The local macOS ARM64 WebView displayed **Connect Google Drive** without
import/setup controls; no Google consent was started or user token written.

[Installer run 36954256173](https://github.com/alexandroit/LedgeSync/actions/runs/36954256173)
passed all five required jobs at packaging source
`0ec2f3fb66624522638b69f3b7a71ef517cf5259`: Windows x64/ARM64 wizard and
install/reinstall/remove tests, Ubuntu x64/ARM64 package tests and signed local
APT installation. The installers preserve the released application payloads.
Ubuntu GUI checks observe eight seconds of Xvfb process startup. Windows checks
exercise the installer and compare installed bytes; neither is a complete
interactive application or live-account acceptance test. Same-version reinstall
is covered; cross-version migration is not established.

[Public APT run 36954499569](https://github.com/alexandroit/LedgeSync/actions/runs/36954499569)
passed on Ubuntu 24.04 amd64 and arm64: pinned HTTPS key, signed metadata,
tamper rejection, forced by-hash indexes, desktop plus GNOME Keyring, separate
headless CLI and removal preserving synthetic user data. The active snapshot is
`20261002-alpha3-0ec2f3f`, with Debian version `0.1.0~alpha.3-1`; the existing
signing key and all 24 previous pool/by-hash files are preserved.

All 37 alpha.3 assets passed anonymous download, size and SHA-256 verification.
The 24 initial alpha.3 assets and all 69 alpha.1/alpha.2 assets remain unchanged.
At that release verification, canonical Ubuntu and secondary Pages HTML/CSS matched website source
`5c702076b39a8170f2065c3262f5060d7b65698f`.

Detailed evidence: [application archives/DMGs](research/OAUTH_ONECLICK_RELEASE.json),
[installers](research/OAUTH_ONECLICK_INSTALLERS_RELEASE.json),
[public assets](research/OAUTH_ONECLICK_PUBLIC_ASSETS.json), and
[APT/site deployment](research/OAUTH_ONECLICK_DEPLOYMENT_VERIFICATION.json).

## Historical alpha.2 release results

Application source: `42474b558d3b1557318f9cb0ae714748869f90b3`.
[Build run 36949133758](https://github.com/alexandroit/LedgeSync/actions/runs/36949133758)
passed all 16 jobs, including 23 frontend tests. The same six OS/architecture
combinations passed actual synthetic native vault create/read/update/delete in
[run 36949135946](https://github.com/alexandroit/LedgeSync/actions/runs/36949135946).
The macOS ARM64 WebView also displayed Connections, opened the native client
picker, and returned unchanged after cancellation; no client or token was imported.

[Installer run 36950139047](https://github.com/alexandroit/LedgeSync/actions/runs/36950139047)
passed Windows x64/ARM64 wizard and install/reinstall/remove tests, Ubuntu
x64/ARM64 package tests, and the signed local APT lifecycle. Ubuntu GUI checks
observe eight seconds of Xvfb process startup, not a complete interactive journey.
Windows tests compare installed payload bytes and preserve a synthetic user file.
They do not establish cross-version migration or a complete Windows GUI journey.
[Public APT run 36950554723](https://github.com/alexandroit/LedgeSync/actions/runs/36950554723)
passed on both architectures: HTTPS key/signature checks, tamper rejection,
forced by-hash indexes, desktop plus GNOME Keyring recommendation, separate
headless CLI, and removal preserving a synthetic user file.

All 37 public assets were verified against their recorded digests and sizes;
all 32 alpha.1 assets remained unchanged. macOS DMGs preserve the exact native
CI apps; only installation text was repackaged at commit
`b4e3d7261776309648f203c29119bf859ca49695`. Detailed evidence:
[application archives/DMGs](research/OAUTH_ALPHA_RELEASE.json),
[installers](research/OAUTH_INSTALLERS_RELEASE.json),
[public assets](research/OAUTH_PUBLIC_ASSETS.json), and
[APT/site deployment](research/OAUTH_DEPLOYMENT_VERIFICATION.json).

## Historical alpha.1 release results

[Version 0.1.0-alpha.1](https://github.com/alexandroit/LedgeSync/releases/tag/v0.1.0-alpha.1)
contains six desktop archives and six CLI archives for `darwin`, `linux` and
`windows`, each in `amd64` and `arm64`. All 16 jobs in
[CI run 36942481310](https://github.com/alexandroit/LedgeSync/actions/runs/36942481310)
passed at source commit `89a9121a279c843f77b2d72f8b6e93dc332cb03b`.

Native desktop builds passed on all six targets. Native core tests/static
checks passed on Ubuntu 24.04 x64/ARM64, macOS 15 ARM64/Intel, Windows Server
2022/2025 x64 and Windows 11 ARM64. Race tests passed where supported; Windows
ARM64 excludes that detector. Tests for unavailable POSIX permissions/filenames,
case-insensitive paths or symlink privileges explicitly skip where inapplicable.

The macOS ARM64 app was additionally opened and exercised with the real native
folder picker, file explorer, excluded-rule inspector and simulated plan.
Later installer validation is recorded below; it does not establish a full
interactive application journey on Windows or Linux. The release includes `SHA256SUMS` and `RELEASE.json`; archive architecture and public asset
digests were verified after downloading the CI artifacts.

Two macOS DMGs were subsequently added to the same alpha release, using the
exact apps extracted from its original verified desktop archives. The source
tag, original archives, `SHA256SUMS` and `RELEASE.json` were preserved. Each DMG
has a separate `.dmg.sha256` checksum; `DMG_RELEASE.json` records the source
archive, binary and disk-image identities. These are a packaging addition,
not new application functionality or a signed production release.

The additional [installer evidence](research/INSTALLERS_RELEASE.json) records
Windows setup EXEs and Debian packages built from the unchanged released apps.
[Windows installer run 36946941074](https://github.com/alexandroit/LedgeSync/actions/runs/36946941074)
passed on native x64 (Server 2022 Desktop Experience) and ARM64 (Windows 11).
Both exercised the real wizard welcome/Next/cancel path, per-user installation,
exact payload and notices, registration, default/optional shortcuts, same-version
reinstallation and removal preserving synthetic user data. This does not claim
full application interaction, x64 Windows 11 execution or cross-version migration.

Ubuntu packages were built and installed on native Ubuntu 24.04 x64 and ARM64
runners. Each check verified the original payload and notices, CLI execution,
package removal, and eight seconds of graphical process startup under Xvfb.
That startup observation is not an interactive GUI acceptance test. Public
[APT verification run 36946856179](https://github.com/alexandroit/LedgeSync/actions/runs/36946856179)
passed on both architectures: pinned HTTPS key, valid signature, tamper rejection,
by-hash acquisition, desktop installation, separate headless CLI installation,
and removal preserving a synthetic user fixture.

Go 1.27 raises the effective macOS minimum to 13. Framework-only minimums are
insufficient to determine the packaged application's minimum. See the official
[Go requirements](https://go.dev/wiki/MinimumRequirements),
[Wails prerequisites](https://wails.io/docs/gettingstarted/installation/), and
[GitHub runner matrix](https://docs.github.com/en/actions/reference/runners/github-hosted-runners).

## Desktop prerequisites

### macOS graphical app

| Mac | Download |
|---|---|
| Apple Silicon, M-series | [LedgeSync alpha.3 ARM64 DMG](https://github.com/alexandroit/LedgeSync/releases/download/v0.1.0-alpha.3/LedgeSync-0.1.0-alpha.3-macos-arm64.dmg) |
| Intel, x64 | [LedgeSync alpha.3 Intel DMG](https://github.com/alexandroit/LedgeSync/releases/download/v0.1.0-alpha.3/LedgeSync-0.1.0-alpha.3-macos-amd64.dmg) |

1. Download the image matching the processor shown in **About This Mac**.
2. Open the `.dmg` and drag `LedgeSync.app` to the `Applications` shortcut.
3. Eject the image and open LedgeSync from Applications. Choose a local folder
   to browse files and preview the offline policy decisions.
4. To authorize Google Drive, open **Connections** and follow the
   [Google Drive connection guide](GOOGLE_DRIVE_AUTH.md). The OS Keychain must
   be available; authorization does not enable file transfers.

The image contains the graphical app, Applications shortcut, installation
instructions and license notices. It does not install the command-line tool.
Check its adjacent `.dmg.sha256` file with `shasum -a 256 -c <file>.dmg.sha256`.
The developer-signing limits below still apply to disk-image downloads.

### Windows graphical installer

Download the [alpha.3 x64 installer](https://github.com/alexandroit/LedgeSync/releases/download/v0.1.0-alpha.3/LedgeSync-0.1.0-alpha.3-windows-amd64-setup.exe)
or the [alpha.3 ARM64 installer](https://github.com/alexandroit/LedgeSync/releases/download/v0.1.0-alpha.3/LedgeSync-0.1.0-alpha.3-windows-arm64-setup.exe).
The downloaded setup EXE opens a graphical installation wizard, rather than
launching the portable application immediately. Follow its folder/shortcut
steps, then open LedgeSync from the Start menu. It installs for the current
Windows account without requiring administrator privileges. A desktop shortcut
is optional, and uninstall is available through **Settings > Apps > Installed apps**.

Windows 11 and Microsoft Edge WebView2 Runtime are required. Setup detects a
missing runtime and displays Microsoft's official download address; it does
not silently download and run additional software. On Windows Server 2022+
with Desktop Experience, setup mechanics are supported, but server GUI runtime
acceptance is not claimed. Use the separate CLI archive on Server Core/headless
machines. The installer does not install a service, startup task or CLI.

The installer and its bundled app remain unsigned developer builds. The x64
setup engine can run under Windows 11 ARM64 emulation; the ARM64 package
contains the native ARM64 app and accepts only ARM64 Windows.

### Ubuntu APT

The signed project repository supports **Ubuntu 24.04, amd64 and arm64**. APT
does not discover third-party repositories automatically; add this source once:

```sh
sudo apt-get update &&
sudo apt-get install ca-certificates curl &&
sudo install -d -m 0755 /etc/apt/keyrings &&
sudo curl --fail --show-error --silent --location \
  --output /etc/apt/keyrings/ledgesync-archive-keyring.gpg \
  https://ledgesync.com/apt/ledgesync-archive-keyring.gpg &&
sudo chmod 0644 /etc/apt/keyrings/ledgesync-archive-keyring.gpg &&
sudo curl --fail --show-error --silent --location \
  --output /etc/apt/sources.list.d/ledgesync.sources \
  https://ledgesync.com/apt/ledgesync.sources &&
sudo chmod 0644 /etc/apt/sources.list.d/ledgesync.sources
```

The deb822 source scopes `Signed-By` to the LedgeSync key; it does not add the
key to APT's global trust store. Public key fingerprint:
`11B35F4E066806C33AA8653A51AD694F4729F5AB`.

Then install the graphical app:

```sh
sudo apt-get update &&
sudo apt-get install ledgesync
```

Open **LedgeSync** from the application menu, or run `ledgesync-desktop`. The
desktop package depends on the matching CLI package and its native GTK/WebKit
runtime libraries, resolved automatically by APT. For Google authorization,
use an active desktop session with GNOME Keyring or another Secret Service
implementation and an unlocked default collection. Installing a keyring package
alone does not create or unlock that collection. See [account setup](GOOGLE_DRIVE_AUTH.md).
For a headless server:

```sh
sudo apt-get update &&
sudo apt-get install ledgesync-cli
ledgesync --version
```

The published alpha.3 CLI package has no graphical-library dependencies and no OAuth commands.
The published alpha.3 Debian version is `0.1.0~alpha.3-1`; the application reports
`0.1.0-alpha.3`. The previous alpha.1 and alpha.2 packages, binaries and notices
remain in their archived releases and repository snapshots. GitHub normalizes
`~` to `.` in download filenames; the package's internal Debian version and the APT pool
filenames retain `~`. Adjacent checksums use the actual GitHub download names.
Package removal does not delete user
configuration or source files:

```sh
sudo apt-get remove ledgesync ledgesync-cli
```

APT verifies the signed repository metadata and package hashes. This is
separate from Windows/macOS publisher signing. See [APT operations](APT_REPOSITORY.md)
for publication, key scope and rollback details.

### Runtime dependencies and developer signing

Ubuntu desktop needs GTK3 and WebKitGTK 4.1 (`libgtk-3-0t64` and
`libwebkit2gtk-4.1-0` on Ubuntu 24.04). Development builds need `libgtk-3-dev`,
`libwebkit2gtk-4.1-dev`, `pkg-config`, and a C compiler. Windows desktop needs
Microsoft Edge WebView2 Runtime. The macOS app uses the system WebKit.
Ubuntu OAuth additionally requires the user's D-Bus session and an unlocked
Secret Service store such as GNOME Keyring. Windows uses the current user's
Credential Manager, and macOS uses Keychain. These stores are only accessed
when account or authorized upload actions require them; headless CLI previews
remain offline. Candidate `auth status` explicitly reads local vault metadata.

Desktop archives are developer builds: **no trusted publisher signature or notarization**.
The macOS build uses an ad-hoc local signature. No claim
is made that Gatekeeper, SmartScreen, clean installation, update, uninstall,
accessibility, or a complete interactive account-connection journey has passed
on every target. Native vault lifecycle results are recorded above. Do not disable
operating-system security controls to run a downloaded package; building from
reviewed source is an available development path.

## Build from source

Pinned toolchain: Go 1.27.1, Node.js 24.20.0, Wails 2.14.0. Dependencies are
recorded in `go.sum` and `frontend/package-lock.json`. No global Wails install is
required.

```sh
go test ./...
go build -trimpath -o build/cli/ ./cmd/ledgesync
python3 tools/package_cli.py --version 0.1.0-alpha.4
```

For desktop OAuth, first follow [maintainer build configuration](OAUTH_BUILD.md).
Without that optional build input, the local explorer works and account connection
and uploads are explicitly unavailable. These commands build the local alpha.4
candidate; they do not publish or replace alpha.3. Run `npm ci` in `frontend`, then from
`cmd/ledgesync-desktop`:

```sh
go run github.com/wailsapp/wails/v2/cmd/wails@v2.14.0 build -tags desktop -nosyncgomod -m -trimpath
```

On Ubuntu use `-tags desktop,webkit2_41`. Native packages are built on each
target OS; a successful cross-compilation alone is not a runtime test.

On macOS set `CGO_CFLAGS=-mmacosx-version-min=13.0` and
`CGO_LDFLAGS=-mmacosx-version-min=13.0` for the build command. The frontend build
automatically runs `frontend/scripts/prepare-native.mjs`, generating the app's
own icon and macOS property-list templates from tracked source. The native
bundle identifier is `com.ledgesync.app`; the minimum macOS version is 13.0.

To create the macOS disk image from an existing native app:

```sh
python3 tools/package_dmg.py --app build/bin/LedgeSync.app --arch arm64 --version 0.1.0-alpha.4 --output build/packages
```

Use `--arch amd64` for an Intel build. The script requires macOS, verifies the
actual Mach-O architecture, preserves the app, and refuses to overwrite an
existing output. It verifies the image and its read-only mounted contents.
Use `--notices-root <extracted-release-directory>` when packaging an existing
release so the notices come from that same artifact. Creating a DMG does not
sign or notarize the app.
For historical artifact work, use packaging code from the matching release;
current candidate installation text describes the new manual upload workflow.

## CI artifacts and checksums

The [build workflow](https://github.com/alexandroit/LedgeSync/actions/workflows/ci.yml)
runs core tests, static analysis, supported race checks, frontend compilation,
native desktop builds, and CLI packaging. Successful runs attach artifacts.
Portable CLI archives include `SHA256SUMS`; desktop archives include an adjacent
SHA-256 file. macOS desktop jobs also produce a DMG and its separate SHA-256
file. These checksums detect corruption; they are not code signatures.

The [session handoff](17_AGENT_HANDOFF_AND_STATUS.md) records the specific
commands and platform results actually observed. Matrix entries remain targets
until their corresponding jobs pass. Publisher-signed installers, MSI,
automatic updates, and unattended synchronization are later milestones.
