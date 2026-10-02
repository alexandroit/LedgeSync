# Platform builds and installation

LedgeSync's first version is **0.1.0-alpha.1, offline only**. Desktop and CLI
share the same Go engine. No package installs a service, schedules jobs, connects
an account, uploads files, or enables deletion.

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

## Observed release results

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
| Apple Silicon, M-series | [LedgeSync ARM64 DMG](https://github.com/alexandroit/LedgeSync/releases/download/v0.1.0-alpha.1/LedgeSync-0.1.0-alpha.1-macos-arm64.dmg) |
| Intel, x64 | [LedgeSync Intel DMG](https://github.com/alexandroit/LedgeSync/releases/download/v0.1.0-alpha.1/LedgeSync-0.1.0-alpha.1-macos-amd64.dmg) |

1. Download the image matching the processor shown in **About This Mac**.
2. Open the `.dmg` and drag `LedgeSync.app` to the `Applications` shortcut.
3. Eject the image and open LedgeSync from Applications. Choose a local folder
   to browse files and preview the offline policy decisions.

The image contains the graphical app, Applications shortcut, installation
instructions and license notices. It does not install the command-line tool.
Check its adjacent `.dmg.sha256` file with `shasum -a 256 -c <file>.dmg.sha256`.
The developer-signing limits below still apply to disk-image downloads.

### Windows graphical installer

Download the [x64 installer](https://github.com/alexandroit/LedgeSync/releases/download/v0.1.0-alpha.1/LedgeSync-0.1.0-alpha.1-windows-amd64-setup.exe)
or the [ARM64 installer](https://github.com/alexandroit/LedgeSync/releases/download/v0.1.0-alpha.1/LedgeSync-0.1.0-alpha.1-windows-arm64-setup.exe).
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
runtime libraries, resolved automatically by APT. For a headless server:

```sh
sudo apt-get update &&
sudo apt-get install ledgesync-cli
ledgesync --version
```

The CLI package has no graphical-library dependencies. The Debian version is
`0.1.0~alpha.1-1`; the application reports `0.1.0-alpha.1`. Both packages preserve
the original release binaries and notices. GitHub normalizes `~` to `.` in
download filenames; the package's internal Debian version and the APT pool
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

Desktop archives are developer builds: **no trusted publisher signature or notarization**.
The macOS build uses an ad-hoc local signature. No claim
is made that Gatekeeper, SmartScreen, clean installation, update, uninstall,
accessibility, or vault integration has passed on every target. Do not disable
operating-system security controls to run a downloaded package; building from
reviewed source is an available development path.

## Build from source

Pinned toolchain: Go 1.27.1, Node.js 24.20.0, Wails 2.14.0. Dependencies are
recorded in `go.sum` and `frontend/package-lock.json`. No global Wails install is
required.

```sh
go test ./...
go build -trimpath -o build/cli/ ./cmd/ledgesync
python3 tools/package_cli.py --version 0.1.0-alpha.1
```

For the desktop, run `npm ci` in `frontend`, then from `cmd/ledgesync-desktop`:

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
python3 tools/package_dmg.py --app build/bin/LedgeSync.app --arch arm64 --version 0.1.0-alpha.1 --output build/packages
```

Use `--arch amd64` for an Intel build. The script requires macOS, verifies the
actual Mach-O architecture, preserves the app, and refuses to overwrite an
existing output. It verifies the image and its read-only mounted contents.
Use `--notices-root <extracted-release-directory>` when packaging an existing
release so the notices come from that same artifact. Creating a DMG does not
sign or notarize the app.

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
