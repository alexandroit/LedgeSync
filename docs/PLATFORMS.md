# Platform builds and installation

**0.1.0-alpha.5 — current developer pre-release (2026-10-02).** It fixes the
reported synchronization failure and adds saved sync pairs, opt-in automatic
copies, restore to a new folder, typed errors and server CLI commands; see the
[failure analysis](research/DRIVE_SYNC_FAILURE_ANALYSIS.md).

| Gate | Result |
|---|---|
| Pull request CI | [run 37052557562](https://github.com/alexandroit/LedgeSync/actions/runs/37052557562): 16/16; native vault [37052557564](https://github.com/alexandroit/LedgeSync/actions/runs/37052557564): 6/6 |
| Official build (publisher OAuth client on all six targets) | [run 37053500290](https://github.com/alexandroit/LedgeSync/actions/runs/37053500290): 16/16 at `64cf420` |
| Release | [v0.1.0-alpha.5](https://github.com/alexandroit/LedgeSync/releases/tag/v0.1.0-alpha.5): 37 assets, `SHA256SUMS`, `RELEASE.json`, `INSTALLERS_RELEASE.json` |
| Clean-machine installers | [run 37054397393](https://github.com/alexandroit/LedgeSync/actions/runs/37054397393): Windows Server 2022 x64 and Windows 11 ARM64 wizard/install/reinstall/uninstall; Ubuntu amd64/arm64 package, GUI startup and removal; local signed APT lifecycle |
| Public APT | Snapshot `20261002-alpha5-64cf420` active; [run 37054874133](https://github.com/alexandroit/LedgeSync/actions/runs/37054874133): `apt-get install ledgesync` and `ledgesync-cli` on clean amd64/arm64 |
| Local macOS check | Official arm64 DMG mounted; app arm64, ad-hoc signature valid, macOS 13 minimum, starts; `spctl` rejects it as unsigned |
| Publisher signing | Not available: see [Publisher signing](#publisher-signing) |

Exact hashes and results: [alpha.5 release evidence](research/DRIVE_SYNC_ALPHA5_RELEASE.json).
Windows 11 x64 has no GitHub-hosted runner; the x64 installer was exercised on
Windows Server 2022 and the core tests on Server 2022 and 2025. Alpha.4 and
earlier releases below remain unchanged.

LedgeSync **0.1.0-alpha.4** implements explicitly approved Google Drive folder
copies in the desktop and native CLI. The [application release](https://github.com/alexandroit/LedgeSync/releases/tag/v0.1.0-alpha.4)
contains native archives for macOS, Ubuntu and Windows in AMD64/ARM64, macOS
DMGs, Windows setup EXEs and Ubuntu DEBs. Google consent uses the bundled
publisher Desktop client; no end-user client creation or credential import is
needed.

All 37 alpha.4 GitHub release assets are publicly verified. Native Windows/Ubuntu
installer, local APT and public APT checks passed. Snapshot
`20261002-alpha4-fcd5784` is active and verified on native Ubuntu amd64/arm64.
The canonical Ubuntu website and secondary GitHub Pages copy serve the updated
release. Prior release/tag/package identities are preserved unchanged.

## Alpha.4 copy behavior and release boundary

The desktop connects through the same publisher Desktop client and
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
mirror deletion or bidirectional synchronization is enabled. The native CLI now
shares browser authorization, folder selection and the approved copy engine.
`copy` requires an interactive terminal and confirmation of the complete preview;
offline `plan` output cannot be applied. A server needs an available native vault
in the user's session; SSH authorization uses an explicit local loopback tunnel.
See [CLI usage](CLI.md). The native alpha.4 CLI archives are published; installer
and public APT availability are recorded separately below.

The owner reports Production OAuth, enabled Picker API and successful account
connection. Independent live Picker/upload and SSH acceptance remain separate
from the automated build, vault and installer results below.

## Alpha.4 native validation and publication gates

Application/tag source: `fcd578488d07f627372e7f5dd2221e162634bf05`.
[build run 36963525743](https://github.com/alexandroit/LedgeSync/actions/runs/36963525743) passed **16 of 16 jobs**. Native core tests and vet passed on
Ubuntu 24.04 AMD64/ARM64, macOS 15 Apple Silicon/Intel, Windows Server 2022/2025
x64 and Windows 11 ARM64. Race checks passed on the supported runners; the
Windows ARM64 race step is explicitly skipped. Contracts, pinned-rclone
differential checks and frontend build/tests passed as separate jobs.

All six desktop jobs completed publisher OAuth-client injection, native graphical
builds, native CLI packaging with the same client and generated-source cleanup.
macOS CLI builds use CGO for Keychain; Linux and Windows CLI builds use their
native vaults without CGO. Both macOS jobs packaged and verified their DMGs.
These CI outcomes establish native builds, not a complete interactive Google
journey. [native-vault run 36963524884](https://github.com/alexandroit/LedgeSync/actions/runs/36963524884) passed **six of six jobs**,
covering the same OS/architecture combinations with disposable native vault
create/read/update/delete checks. No personal account or live Google copy was
used for this evidence.

The application checks OS-native ownership and access to transfer state and lock
files, including macOS extended ACLs and Windows protected owner-only DACLs.
Linux verifies the D-Bus Unix socket's peer user before authentication. OAuth
transactions and transfer writers have separate cross-process locks. No plaintext
credential fallback or silent permission relaxation is provided.

All 37 public alpha.4 assets were anonymously downloaded and verified against
recorded SHA-256 hashes and sizes; the tag resolves to the exact application SHA.
Release/tag identities and all 106 alpha.1/alpha.2/alpha.3 asset IDs, names, sizes
and digest metadata remain unchanged. See [application archive/DMG evidence](research/DRIVE_COPY_RELEASE.json)
and [public asset verification](research/DRIVE_COPY_PUBLIC_ASSETS.json).

[Installer run 36964667348](https://github.com/alexandroit/LedgeSync/actions/runs/36964667348)
passed all five required jobs at packaging source
`13342144685824daa38c774cb3ef7bdf14e315d3`: Windows x64/ARM64 graphical wizard,
install/reinstall/uninstall and payload checks; Ubuntu amd64/arm64 package,
GUI startup and lifecycle checks; and local signed APT installation. Public APT
was intentionally not part of that run. These checks do not establish live
Google/SSH interaction or cross-version data migration. The [installer evidence](research/DRIVE_COPY_INSTALLERS_RELEASE.json)
records the pinned application inputs, packaging source, native results and hashes.

[Public APT run 36965018881](https://github.com/alexandroit/LedgeSync/actions/runs/36965018881)
passed on native Ubuntu 24.04 amd64 and arm64 at the same packaging SHA. Both
jobs verified the pinned HTTPS key/source, signed metadata, tamper rejection,
by-hash acquisition, desktop and separate headless CLI installation, and removal
preserving a synthetic user fixture. Installed Debian version: `0.1.0~alpha.4-1`.
The active snapshot is `20261002-alpha4-fcd5784`; the signing key is unchanged.
No LedgeSync application was installed on the production web server. These
checks do not establish an unlocked vault or a Google consent/copy journey in
a real server session.

The canonical Ubuntu site and secondary GitHub Pages copy serve source
`6c8f1d19f8c2aa398d98d0a5b39ba5a1999772d1`.
[Pages run 36965146423](https://github.com/alexandroit/LedgeSync/actions/runs/36965146423)
passed. Seven files matched exact tracked bytes through public HTTPS, origin
TLS and Pages (21 checks). Seventeen shared configuration hashes and the
existing HiperMusicas service PID remained unchanged; public/origin health
checks passed. No Nginx reload was required; earlier site/APT directories remain
available for rollback. [Deployment evidence](research/DRIVE_COPY_DEPLOYMENT_VERIFICATION.json)
records the APT snapshot, native public installation, site bytes and preservation
checks. The public GitHub repository and release identify Apache-2.0.

**Release evidence and remaining acceptance boundaries:**

| Gate | Alpha.4 status |
|---|---|
| Application archives/DMGs and immutable tag | Public bytes verified; source/tag fixed at `fcd578488d07f627372e7f5dd2221e162634bf05` |
| Windows setup EXEs and Ubuntu DEBs | Native installer/local APT run `36964667348` passed; all 37 release assets publicly verified |
| Public signed APT repository | Snapshot `20261002-alpha4-fcd5784` active; run `36965018881` passed native amd64/arm64 installation and removal |
| Canonical Ubuntu site and secondary Pages | Source `6c8f1d19f8c2aa398d98d0a5b39ba5a1999772d1`, Pages run `36965146423`, 21 exact-byte checks passed |
| Live Google Picker, copy/recovery and SSH return | Pending separately authorized native acceptance |
| Apple Developer ID/notarization and Windows Authenticode | Not supplied; no trusted publisher signature is claimed |

The alpha.3 sections below are historical release evidence. All previous assets,
tags, package bytes and APT snapshots are preserved; alpha.4 publication does not
replace them.

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
Darwin packages target macOS; there is no iPhone/iPad build or iOS acceptance claim.

## Historical alpha.3 authorization and validation boundary

This section records alpha.3 only; current copy and CLI behavior is described above.

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
tests; the alpha.4 Picker/upload workflow still needs independent live acceptance.

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
| Apple Silicon, M-series | [LedgeSync alpha.4 ARM64 DMG](https://github.com/alexandroit/LedgeSync/releases/download/v0.1.0-alpha.4/LedgeSync-0.1.0-alpha.4-macos-arm64.dmg) |
| Intel, x64 | [LedgeSync alpha.4 Intel DMG](https://github.com/alexandroit/LedgeSync/releases/download/v0.1.0-alpha.4/LedgeSync-0.1.0-alpha.4-macos-amd64.dmg) |

1. Download the image matching the processor shown in **About This Mac**.
2. Open the `.dmg` and drag `LedgeSync.app` to the `Applications` shortcut.
3. Eject the image and open LedgeSync from Applications. Choose a local folder
   to browse files, review the policy decisions and prepare a folder copy.
4. To authorize Google Drive, open **Connections** and follow the
   [Google Drive connection guide](GOOGLE_DRIVE_AUTH.md). The OS Keychain must
   be available. Uploads require a separate preview and explicit approval.

The image contains the graphical app, Applications shortcut, installation
instructions and license notices. It does not install the command-line tool.
Check its adjacent `.dmg.sha256` file with `shasum -a 256 -c <file>.dmg.sha256`.
The developer-signing limits below still apply to disk-image downloads.

### Windows graphical installer

| Windows architecture | Download |
|---|---|
| AMD/Intel x64 | [LedgeSync alpha.4 setup EXE](https://github.com/alexandroit/LedgeSync/releases/download/v0.1.0-alpha.4/LedgeSync-0.1.0-alpha.4-windows-amd64-setup.exe) |
| ARM64 | [LedgeSync alpha.4 setup EXE](https://github.com/alexandroit/LedgeSync/releases/download/v0.1.0-alpha.4/LedgeSync-0.1.0-alpha.4-windows-arm64-setup.exe) |

Both setup EXEs passed native lifecycle checks and public byte verification.
The downloaded setup EXE opens a graphical installation wizard. Follow its folder/shortcut
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
does not discover third-party repositories automatically; add this source once.
The system paths in this installation block are Ubuntu APT destinations, not
checkout locations. They intentionally remain absolute; making them relative to
the repository would prevent APT from finding the signing key and source:

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

APT snapshot `20261002-alpha4-fcd5784` publishes Debian version
`0.1.0~alpha.4-1`. Native packaging, local APT and both architectures of public
installation run `36965018881` passed. The alpha.4 CLI supports interactive
copies with an available user vault and has no graphical-library dependencies.
Earlier packages, binaries and notices remain in their archived releases and
repository snapshots. GitHub normalizes
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
remain offline. `auth status` explicitly reads local vault metadata.

Published alpha.1–alpha.4 desktop archives are developer builds: **no trusted
publisher signature or notarization**. The macOS build uses an ad-hoc local
signature. No claim is made that Gatekeeper, SmartScreen, clean installation,
update, uninstall, accessibility, or a complete interactive account-connection
journey has passed on every target. Native vault lifecycle results are recorded
above. Do not disable operating-system security controls to run a downloaded
package; building from reviewed source is an available development path.

## Publisher signing

The pipeline is implemented and fails closed; it runs only for official `main`
builds when the material below is configured. Signed artifacts reduce operating
system warnings but cannot guarantee the absence of reputation prompts
(Microsoft SmartScreen builds reputation per release; Gatekeeper still shows a
first-launch confirmation for downloaded apps). Self-signed certificates are
never used. Builds without signing material carry `SIGNING-STATE.txt` with
`unsigned-developer-build`.

| Platform | What is signed | Tooling | Verification |
|---|---|---|---|
| macOS Intel and Apple Silicon | `LedgeSync.app` (hardened runtime, secure timestamp, no entitlement exceptions), DMG, CLI | [sign_macos.py](../tools/sign_macos.py), [entitlements](../deploy/macos/LedgeSync.entitlements) | `codesign --verify --deep --strict`, notarization `Accepted`, `stapler validate` (app and DMG), `spctl --assess` (exec and open) |
| Windows 11 and Windows Server, x64 and ARM64 | `LedgeSync.exe`, `ledgesync.exe`, setup EXE and its uninstaller | [sign_windows.ps1](../tools/sign_windows.ps1), [pinned client](../tools/install_signing_client.ps1), Inno `SignTool` | `signtool verify /pa /all`, `Get-AuthenticodeSignature` Valid with a trusted RFC 3161 timestamp, not self-signed |
| Ubuntu desktop and server | APT `InRelease`/`Release.gpg` metadata over the `.deb` hashes | [build_apt_repository.py](../tools/build_apt_repository.py) with the existing server key | `apt-get install ledgesync` with `Signed-By`, tamper rejection ([APT runbook](APT_REPOSITORY.md)) |

The ad-hoc hardened-runtime check passed locally on macOS arm64: the app ran
with `flags=0x10002(adhoc,runtime)` and no entitlements. An ad-hoc build is
correctly rejected by `spctl`.

### Signing material the owner must provide

Never paste certificates, passwords or keys into a chat or the repository. Add
them as GitHub Actions secrets/variables of `alexandroit/LedgeSync`, or install
them only on a trusted signing machine.

**Apple (both macOS architectures).** An active, paid Apple Developer Program
membership and its Account Holder to create a **Developer ID Application**
certificate (the existing *Apple Development* certificate on the build Mac
cannot be notarized for distribution). Export it as `.p12`. Create an App Store
Connect API key for notarization (Users and Access → Integrations; note the
Issuer ID and Key ID and download the `.p8` once). Configure:

- secrets `APPLE_DEVELOPER_ID_CERTIFICATE` (base64 of the `.p12`),
  `APPLE_DEVELOPER_ID_PASSWORD`, `APPLE_NOTARY_API_KEY` (base64 of the `.p8`),
  `APPLE_NOTARY_KEY_ID`, `APPLE_NOTARY_ISSUER`;
- variable `APPLE_DEVELOPER_ID_IDENTITY`, the full identity name
  `Developer ID Application: NAME (TEAMID)`.

For local signing instead, install the certificate in the login keychain, run
`xcrun notarytool store-credentials` once, then
`python3 tools/sign_macos.py --keychain-profile PROFILE --app build/bin/LedgeSync.app --dmg-arch arm64 --version VERSION`.

**Windows (all four Windows targets).** Either Azure Artifact Signing
(recommended): an Azure subscription, an Artifact Signing account with completed
identity validation and a public-trust certificate profile, and a Microsoft
Entra application holding the *Artifact Signing Certificate Profile Signer* role
on that account. Configure secrets `ARTIFACT_SIGNING_TENANT_ID`,
`ARTIFACT_SIGNING_CLIENT_ID`, `ARTIFACT_SIGNING_CLIENT_SECRET` and variables
`ARTIFACT_SIGNING_ENDPOINT` (the account's regional `https://….codesigning.azure.net/`
URI), `ARTIFACT_SIGNING_ACCOUNT`, `ARTIFACT_SIGNING_PROFILE`. Or an OV/EV
code-signing certificate whose key is on a hardware token or cloud HSM, used on a
trusted Windows signing machine with `LEDGESYNC_WINDOWS_SIGNING=thumbprint`,
`LEDGESYNC_WINDOWS_CERT_SHA1` and the CA's `LEDGESYNC_TIMESTAMP_URL`.

**Ubuntu.** No new material: the existing RSA4096 APT key on the production
server signs repository metadata. Publishing a new snapshot needs the existing
SSH access described in the [APT runbook](APT_REPOSITORY.md).

## Build from source

Pinned toolchain: Go 1.27.1, Node.js 24.20.0, Wails 2.14.0. Dependencies are
recorded in `go.sum` and `frontend/package-lock.json`. No global Wails install is
required.

```sh
go test ./...
go build -trimpath -o build/cli/ ./cmd/ledgesync
python3 tools/package_cli.py --version 0.1.0-alpha.4
```

The commands above create unconfigured developer CLI builds. To package the
configured native CLI, follow [maintainer build configuration](OAUTH_BUILD.md),
then run on the matching native host, for example:

```sh
python3 tools/package_cli.py --version 0.1.0-alpha.4 --platform darwin/arm64 --native --require-oauth-client
```

Select the actual host target (`darwin`, `linux` or `windows`, each with `amd64`
or `arm64`) and clean the generated configuration after both app/CLI builds.
Portable developer packaging must not contain injected OAuth configuration.

For desktop OAuth, first follow [maintainer build configuration](OAUTH_BUILD.md).
Without that optional build input, the local explorer works and account connection
and uploads are explicitly unavailable. These commands build the local alpha.4
source; they do not publish a release or replace existing artifacts. Run `npm ci` in `frontend`, then from
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
current alpha.4 installation text describes the manual upload workflow.

## CI artifacts and checksums

The [build workflow](https://github.com/alexandroit/LedgeSync/actions/workflows/ci.yml)
runs core tests, static analysis, supported race checks, frontend compilation,
native desktop builds, and CLI packaging. Successful runs attach artifacts.
CLI archives include `SHA256SUMS`; desktop archives include an adjacent
SHA-256 file. macOS desktop jobs also produce a DMG and its separate SHA-256
file. These checksums detect corruption; they are not code signatures.

The [session handoff](17_AGENT_HANDOFF_AND_STATUS.md) records the specific
commands and platform results actually observed. Matrix entries remain targets
until their corresponding jobs pass. Publisher-signed installers, MSI,
automatic updates, and unattended synchronization are later milestones.
