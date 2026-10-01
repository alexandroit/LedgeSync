# Platform builds and installation

Confirmar's first version is **0.1.0-alpha.1, offline only**. Desktop and CLI
share the same Go engine. No package installs a service, schedules jobs, connects
an account, uploads files, or enables deletion.

## Target matrix

| System | Architecture | Deliverable | Validation gate |
|---|---|---|---|
| macOS 13+ | Apple Silicon / ARM64 | Native `.app` and CLI | Native build and local smoke; CI on macOS 15 |
| macOS 13+ | Intel / x64 | Native `.app` and CLI | Native CI on macOS 15 Intel |
| Ubuntu 24.04 LTS | AMD/Intel x64 and ARM64 | Desktop executable and CLI | Native CI for each architecture |
| Ubuntu Server | AMD/Intel x64 and ARM64 | CLI | Same Linux CLI; no graphical libraries required |
| Windows 11 | AMD/Intel x64 and ARM64 | Desktop `.exe` and CLI | Native CI; ARM64 runner uses Windows 11 |
| Windows Server 2022 / 2025 | x64 | CLI | Native core tests on both Server runners |
| Windows Server 2016 / 2019 | x64 | CLI candidate | Go runtime baseline permits these; installation/runtime acceptance pending |

The `amd64` name means the same 64-bit architecture on AMD and Intel processors.
ARM means ARM64 in this project; there are no 32-bit packages. Windows Server
desktop use is not an acceptance target; use the CLI for Server Core/headless
systems. No macOS Server-specific package or operating-system service is required.

Go 1.27 raises the effective macOS minimum to 13. Framework-only minimums are
insufficient to determine the packaged application's minimum. See the official
[Go requirements](https://go.dev/wiki/MinimumRequirements),
[Wails prerequisites](https://wails.io/docs/gettingstarted/installation/), and
[GitHub runner matrix](https://docs.github.com/en/actions/reference/runners/github-hosted-runners).

## Desktop prerequisites

Ubuntu desktop needs GTK3 and WebKitGTK 4.1 (`libgtk-3-0t64` and
`libwebkit2gtk-4.1-0` on Ubuntu 24.04). Development builds need `libgtk-3-dev`,
`libwebkit2gtk-4.1-dev`, `pkg-config`, and a C compiler. Windows desktop needs
Microsoft Edge WebView2 Runtime. The macOS app uses the system WebKit.

Desktop archives are developer builds: **unsigned and not notarized**. No claim
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
go build -trimpath -o build/cli/ ./cmd/confirmar
python3 tools/package_cli.py --version 0.1.0-alpha.1
```

For the desktop, run `npm ci` in `frontend`, then from `cmd/confirmar-desktop`:

```sh
go run github.com/wailsapp/wails/v2/cmd/wails@v2.14.0 build -tags desktop -nosyncgomod -m -trimpath
```

On Ubuntu use `-tags desktop,webkit2_41`. Native packages are built on each
target OS; a successful cross-compilation alone is not a runtime test.

## CI artifacts and checksums

The [build workflow](https://github.com/alexandroit/confirmar/actions/workflows/ci.yml)
runs core tests, static analysis, supported race checks, frontend compilation,
native desktop builds, and CLI packaging. Successful runs attach artifacts.
Portable CLI archives include `SHA256SUMS`; desktop archives include an adjacent
SHA-256 file. These checksums detect corruption; they are not code signatures.

The [session handoff](17_AGENT_HANDOFF_AND_STATUS.md) records the specific
commands and platform results actually observed. Matrix entries remain targets
until their corresponding jobs pass. Signed installers, Debian packages, MSI,
automatic updates, and unattended synchronization are later milestones.
