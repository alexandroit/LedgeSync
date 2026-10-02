# LedgeSync

LedgeSync copies explicitly approved local folders to Google Drive, preserving
the included hierarchy and empty folders while applying your ignore policies.
The desktop is the primary interface; the native CLI uses the same authorization,
filtering, approval, transfer and recovery services.

[Website](https://ledgesync.com/) ·
[Download 0.1.0-alpha.4](https://github.com/alexandroit/LedgeSync/releases/tag/v0.1.0-alpha.4) ·
[Connection guide](docs/GOOGLE_DRIVE_AUTH.md) ·
[CLI/server guide](docs/CLI.md) ·
[Apache-2.0 license](LICENSE)

**Current developer alpha: 0.1.0-alpha.4.** Native application archives, macOS
DMGs, Windows setup EXEs and Ubuntu DEBs are published from application source
`fcd578488d07f627372e7f5dd2221e162634bf05`.
This is a manual copy release: watching, scheduling, bidirectional sync,
shared-drive transfers, download/restore, overwrite and deletion are unavailable.

All 37 GitHub release assets are publicly verified. The signed APT repository
serves alpha.4, and public installation checks passed on both Ubuntu architectures.
The [website](https://ledgesync.com/) and secondary GitHub Pages copy serve the
updated release. Exact delivery evidence is in the
[publication status](docs/PLATFORMS.md#alpha4-native-validation-and-publication-gates).

## Downloads

| System | Alpha.4 download |
|---|---|
| macOS 13+, Apple Silicon | [ARM64 DMG](https://github.com/alexandroit/LedgeSync/releases/download/v0.1.0-alpha.4/LedgeSync-0.1.0-alpha.4-macos-arm64.dmg) |
| macOS 13+, Intel | [x64 DMG](https://github.com/alexandroit/LedgeSync/releases/download/v0.1.0-alpha.4/LedgeSync-0.1.0-alpha.4-macos-amd64.dmg) |
| Windows 11, x64 | [Graphical setup EXE](https://github.com/alexandroit/LedgeSync/releases/download/v0.1.0-alpha.4/LedgeSync-0.1.0-alpha.4-windows-amd64-setup.exe) |
| Windows 11, ARM64 | [Graphical setup EXE](https://github.com/alexandroit/LedgeSync/releases/download/v0.1.0-alpha.4/LedgeSync-0.1.0-alpha.4-windows-arm64-setup.exe) |
| Ubuntu 24.04, amd64/arm64 | [Signed APT repository](docs/PLATFORMS.md#ubuntu-apt): `apt-get install ledgesync` after setup |
| Native CLI, all six targets | [CLI archives and checksums](https://github.com/alexandroit/LedgeSync/releases/tag/v0.1.0-alpha.4) |

On macOS, open the DMG and drag `LedgeSync.app` to Applications. Windows setup
opens a graphical installation wizard and installs for the current user. Ubuntu
uses the project's signed APT repository; it is separate from Ubuntu's official
archive. Native prerequisites and server-session limitations are in the
[platform guide](docs/PLATFORMS.md).

These are developer builds. macOS applications use ad-hoc signing and are not
Developer ID signed/notarized; Windows app/setup publisher signing is not
available. Checksums and APT repository signatures do not replace those publisher
signatures. Do not disable operating-system security protections.

## First folder copy

1. Open a local folder or project configuration in LedgeSync.
2. Choose **Connections → Connect Google Drive**, authorize on Google's page
   and return to the app. End users do not import JSON or supply tokens.
3. Choose **Use My Drive** or **Choose existing Drive folder**. Google's browser
   Picker grants access to the selected existing parent.
4. Choose **Preview folder upload**, review the account, destination ID,
   included/excluded entries and actions, then choose **Upload folder**.
5. Keep LedgeSync open until **Folder upload verified**. Use **Open destination
   folder on Google Drive** to inspect the result.

The local root becomes one managed child folder within the selected parent.
Verified unchanged copies are checked and skipped. Changed files keep both
versions with a stable `.ledgesync-` suffix; existing cloud data is never
replaced or deleted. Local source files stay read-only.

Canceling leaves completed copies in Drive. After cancellation or restart,
select the same source/destination and approve a fresh preview to reconcile
saved object IDs and continue. An incomplete file may restart its upload because
session URLs exist only in memory. If previously observed ignore rules disappear,
the upload stops even after restart. The native close prompt defaults to
**Keep Open** during a transfer.

The default policy discovers `.gitignore`; configurations can select multiple
rule sources and explicit dialects. Unsupported required adapters and unreadable
or invalid rules fail closed. A filename alone does not identify an ignore
syntax, and `rclone.conf` is not a filter file. See [policy semantics](docs/04_FILTER_ENGINE_SPEC.md).

## CLI and servers

Use a configured native build in an interactive terminal with an available
user credential vault:

```sh
ledgesync auth connect
ledgesync copy --root "/path/to/project" --destination picker
```

Review the complete preview and type its exact digest to approve. For an SSH
server, use `--no-browser` and the displayed loopback tunnel to authorize in your
computer's browser. The [CLI guide](docs/CLI.md) covers Ubuntu Secret Service,
Windows logon-session requirements, macOS Keychain and cancellation.
There is no unattended `apply`, `--yes`, background service or plaintext token
fallback. The older `plan` command remains an offline inspection against a fake
destination; its exported file cannot approve a Drive upload.

## Credentials and local state

Authorization goes directly between your computer/server and Google with
PKCE/state and the limited `drive.file` scope. The LedgeSync distribution server
receives no account tokens. Refresh tokens use macOS Keychain, Windows Credential
Manager or Ubuntu Secret Service; access tokens stay in process memory.
`drive.file` does not expose every existing Drive file or recursively authorize
all children of a selected parent.

A private per-user SQLite journal outside upload roots stores plans, object IDs,
checksums and observed rules. It contains no OAuth tokens, file payloads or upload
session URLs. Native permission/ACL checks protect journal and lock files;
Linux also checks the D-Bus peer user. Separate process locks coordinate OAuth
transactions and journal writers across CLI/desktop. Unavailable protection
stops the operation. See [security and limits](SECURITY.md).

## Validation and known limits

The exact application source passed [all 16 build/test jobs](https://github.com/alexandroit/LedgeSync/actions/runs/36963525743)
and [all six native vault jobs](https://github.com/alexandroit/LedgeSync/actions/runs/36963524884).
The [installer workflow](https://github.com/alexandroit/LedgeSync/actions/runs/36964667348)
passed both Windows, both Ubuntu and local signed APT jobs at packaging source
`13342144685824daa38c774cb3ef7bdf14e315d3`. All 37 public alpha.4 assets were
anonymously downloaded and checked against their recorded sizes/hashes. The
identities of all 106 earlier public assets and their release tags remain
unchanged. See the [application evidence](docs/research/DRIVE_COPY_RELEASE.json),
[installer results](docs/research/DRIVE_COPY_INSTALLERS_RELEASE.json),
[public asset verification](docs/research/DRIVE_COPY_PUBLIC_ASSETS.json),
[APT/site deployment verification](docs/research/DRIVE_COPY_DEPLOYMENT_VERIFICATION.json) and
[platform publication gates](docs/PLATFORMS.md#alpha4-native-validation-and-publication-gates).
Public [APT run 36965018881](https://github.com/alexandroit/LedgeSync/actions/runs/36965018881)
passed on native Ubuntu amd64/arm64 against snapshot `20261002-alpha4-fcd5784`,
including signed metadata, tamper rejection, installation and removal.

These checks use synthetic credentials/data and native disposable stores.
The owner reports successful connection and Production OAuth/Picker configuration.
Independent live Picker, whole-folder upload/recovery and SSH return acceptance
remain unverified. Native compilation, installer lifecycle and public byte checks
do not establish that live provider journey. No full roadmap completion or iOS
package is claimed. The [acceptance checklist](docs/research/DRIVE_UPLOAD_ACCEPTANCE.md)
records the remaining checks.

Alpha.3 and earlier releases are preserved as immutable history. Alpha.3 provided
account connection and offline previews; it did not contain the new copy engine.
Its exact release, installer and APT evidence remains in the
[historical platform results](docs/PLATFORMS.md#observed-alpha3-release-results).

## Build and inspect locally

Without publisher configuration, local development remains available:

```sh
go run ./cmd/ledgesync --help
go run ./cmd/ledgesync browse --root /path/to/project --json
go run ./cmd/ledgesync config validate --config project.json
go run ./cmd/ledgesync explain --config project.json --path src/main.go --json
go run ./cmd/ledgesync plan --config project.json --output /outside/source/plan.json
go run ./cmd/ledgesync plan inspect --plan /outside/source/plan.json
go run ./cmd/ledgesync capabilities
```

Follow [platform build instructions](docs/PLATFORMS.md#build-from-source) and
[maintainer OAuth configuration](docs/OAUTH_BUILD.md) for configured native builds.
`auth status` reads local safe connection metadata; it neither checks the grant
online nor opens a browser. Configuration examples contain some future contract
fields that the current copy workflow does not enable.

## Architecture and product scope

The user's requirements are recorded in [Product requirements](docs/01_PRODUCT_REQUIREMENTS.md). They include a standalone product inspired by rclone, Google Drive as the first destination, customizable rule filenames, multiple simultaneous rule sources, and documentation suitable for Codex or Claude Code.

The implementation uses **Go core + TypeScript desktop UI through Wails + SQLite local state**, with a native Google Drive API provider. The **desktop explorer is the primary user experience**; it uses an information architecture familiar to the **current Google Drive experience** without copying Google branding or assets. No external rclone installation is required. CLI and desktop reuse one engine; they are not two independently maintained synchronization systems. Optional read-only VCS adapters may invoke a locally installed VCS client with fixed argument arrays when the policy is not stored as a normal file (notably SVN properties); absence of that optional client must be reported as a capability limitation, never silently ignored.

The 2026-10-01 follow-up explicitly requires source analysis before implementation. Selective source adaptation or pinned in-process reuse is allowed; an independent product does not require rewriting all upstream code. Required notices and our safety boundary must be preserved.

All repository work should be in English: code, identifiers, comments, documentation, tests, commits, and agent implementation reports. **LedgeSync** is the selected product name. Use `ledgesync` for the CLI/binary name and `ledgesync.com` for the website/domain.

## Start here

1. Read [START_HERE.md](START_HERE.md) and [AGENTS.md](AGENTS.md).
2. Use [CODEX_CLAUDE_BOOTSTRAP.md](CODEX_CLAUDE_BOOTSTRAP.md) as the first agent prompt. Claude-specific entry instructions are in [CLAUDE.md](CLAUDE.md).
3. Complete [the mandatory rclone source-analysis assignment](docs/19_RCLONE_SOURCE_ANALYSIS_AND_IMPLEMENTATION.md): actually download the source, audit it and its tests, select reusable components, and then implement the new application in tested slices.
4. Implement the ordered backlog, not the entire rclone feature catalog in a single change.

## Documentation map

| File | Purpose |
|---|---|
| [Project identity](PROJECT_IDENTITY.md) | Canonical product name, CLI command, domain, and GUI identity rules |
| [01 · Product requirements](docs/01_PRODUCT_REQUIREMENTS.md) | Confirmed requirements, assumptions, milestones, and non-goals |
| [02 · rclone analysis](docs/02_RCLONE_RESEARCH_AND_GAP_ANALYSIS.md) | Verified capabilities, inspected source, gaps, and staged feature parity |
| [03 · Architecture](docs/03_ARCHITECTURE.md) | Modules, boundaries, ports, persistence, and execution model |
| [04 · Filter specification](docs/04_FILTER_ENGINE_SPEC.md) | Dialects, discovery, hierarchy, precedence, traversal, and explanations |
| [05 · Sync safety](docs/05_SYNC_SAFETY_AND_STATE.md) | Planning, application, ownership, conflicts, deletion, and recovery |
| [06 · Google Drive](docs/06_GOOGLE_DRIVE_PROVIDER.md) | OAuth, permissions, file IDs, uploads, integrity, quotas, and API limitations |
| [Google Drive connection](docs/GOOGLE_DRIVE_AUTH.md) | Connect in the browser, select a destination, and understand scope and credential storage |
| [Drive upload acceptance](docs/research/DRIVE_UPLOAD_ACCEPTANCE.md) | Manual folder-copy behavior, release limitations and live acceptance checklist |
| [07 · Configuration and contracts](docs/07_CONFIGURATION_AND_CONTRACTS.md) | Schemas, types, commands, errors, and migrations |
| [08 · Desktop, CLI, automation](docs/08_DESKTOP_CLI_AND_AUTOMATION.md) | Screens, workflows, scheduling, headless use, and accessibility |
| [09 · Security and privacy](docs/09_SECURITY_AND_PRIVACY.md) | Threat model, secret handling, path safety, and trust boundaries |
| [10 · Tests](docs/10_TEST_STRATEGY_AND_ACCEPTANCE.md) | Differential, property, integration, fault-injection, and acceptance tests |
| [11 · Backlog](docs/11_IMPLEMENTATION_BACKLOG.md) | Dependencies, implementation slices, and completion gates |
| [12 · Decisions](docs/12_ADR_DECISIONS.md) | Architecture decision records and alternatives |
| [13 · Release and operations](docs/13_RELEASE_AND_OPERATIONS.md) | CI, packaging, signing, migrations, diagnostics, and runbooks |
| [14 · Sources](docs/14_SOURCES_AND_PROVENANCE.md) | Official references, inspected source hashes, and research limitations |
| [15 · Risks and decisions](docs/15_RISKS_AND_OPEN_DECISIONS.md) | Risks, implementation spikes, and owner decisions |
| [16 · Traceability](docs/16_REQUIREMENTS_TRACEABILITY.md) | Requirements mapped to documents, tasks, and tests |
| [17 · Handoff and status](docs/17_AGENT_HANDOFF_AND_STATUS.md) | Starting state and session handoff contract |
| [18 · Validation](docs/18_VALIDATION_REPORT.md) | Checks actually performed on this documentation package |
| [19 · Source audit and implementation](docs/19_RCLONE_SOURCE_ANALYSIS_AND_IMPLEMENTATION.md) | Required real clone, commit-pinned source analysis, reuse decision, and new executable code |
| [20 · Desktop explorer and code-policy adapters](docs/20_DESKTOP_EXPLORER_AND_CODE_POLICY_ADAPTERS.md) | GUI-first Drive-style UX, paired local/cloud explorer, ignored-file explanations, and extensible Git/SVN/Mercurial/Perforce/CVS/Bazaar/Fossil/code-tool policy adapters |

The `schemas/` directory contains machine-readable configuration and plan envelopes. `examples/` contains non-secret configuration samples. `tests/` contains reference cases and safety scenarios. `tools/` validates this package and runs isolated Git reference checks. Those tools do not implement the application.

## Safety defaults

Manual, preview-first copy is the initial mode. No deletions, remote adoption, credential imports, public sharing, automatic uploads, or background startup are enabled without explicit setup. Excluded files remain untouched at the destination. The local source is read-only during upload workflows. Missing volumes and failed scans are errors, never evidence of an empty source.

A future mirror mode operates only on this project's explicitly managed remote objects and requires a fresh plan, backups, scan completeness, and deletion approval. Sync is not a substitute for independent versioned backups. Multi-writer atomicity is not assumed for Google Drive.

## Scope honesty

The rclone analysis is a focused documentation and source review, not a full repository audit or benchmark. Every advertised policy dialect must be proved against a pinned reference implementation or authoritative fixture set before being labeled compatible. Mixed-dialect precedence is our own documented product behavior, not a claim that Git, SVN, Mercurial, rclone, or other tools share identical semantics. “Support all code-management ignore sources” means an extensible adapter architecture plus an explicitly tested compatibility catalog, not treating every `.*ignore` file as Gitignore.

The shared conversation URL did not expose its complete transcript through the public page during preparation. The user-confirmed requirements here are grounded in the current request and recovered prior conversation context; implementation choices are labeled accordingly. See [Sources and provenance](docs/14_SOURCES_AND_PROVENANCE.md).
