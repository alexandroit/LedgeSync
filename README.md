# LedgeSync

> **Canonical identity:** read [PROJECT_IDENTITY.md](PROJECT_IDENTITY.md) first. It defines LedgeSync, `ledgesync`, and `ledgesync.com`.

**Product name:** **LedgeSync**.
**CLI command:** `ledgesync`.
**Primary website/domain:** `ledgesync.com`.
**Specification version:** 0.2.1 · **Prepared:** 2026-10-01.
**Source status:** the **0.1.0-alpha.4 candidate** adds manual Google Drive folder uploads, destination selection in Google's browser Picker, and verified transfer recovery. **Published downloads remain 0.1.0-alpha.3**, which connects an account but does not transfer files. See [current implementation status](docs/17_AGENT_HANDOFF_AND_STATUS.md) and [platform builds](docs/PLATFORMS.md).

The candidate also contains [OAuth hardening](docs/research/OAUTH_SECURITY_HARDENING.md), account-bound revocation confirmation and [native Picker integration](docs/research/NATIVE_PICKER_REVIEW.md). These source changes are not present in the immutable alpha.3 downloads. New live Picker/upload acceptance remains pending; the owner has reported successful account connection and production OAuth/Picker configuration.

**Public project:** [GitHub](https://github.com/alexandroit/LedgeSync) ·
[Website](https://ledgesync.com/) ·
[Download alpha.3](https://github.com/alexandroit/LedgeSync/releases/tag/v0.1.0-alpha.3) ·
[Builds](https://github.com/alexandroit/LedgeSync/actions/workflows/ci.yml) ·
[Apache-2.0 license](LICENSE).
The website is live on the owner's Ubuntu server with HTTPS. See
[deployment and renewal details](docs/WEBSITE.md).

## Published downloads: alpha.3 authorization only

**Alpha.3 is published** for macOS, Windows and Ubuntu. Native builds, credential
vaults, installers and public APT installation passed their respective checks.
See [the release evidence and limits](docs/PLATFORMS.md).

**Download the graphical app for macOS:**
[Apple Silicon (ARM64) DMG](https://github.com/alexandroit/LedgeSync/releases/download/v0.1.0-alpha.3/LedgeSync-0.1.0-alpha.3-macos-arm64.dmg) ·
[Intel (x64) DMG](https://github.com/alexandroit/LedgeSync/releases/download/v0.1.0-alpha.3/LedgeSync-0.1.0-alpha.3-macos-amd64.dmg).
Open the disk image and drag `LedgeSync.app` to `Applications`. Requires macOS
13 or later. These developer builds are not Developer ID signed or notarized;
macOS may block downloaded apps. See [installation and validation limits](docs/PLATFORMS.md).
For Windows and Ubuntu, use the [desktop download section](https://ledgesync.com/#downloads).

**Windows:** download the [x64 setup EXE](https://github.com/alexandroit/LedgeSync/releases/download/v0.1.0-alpha.3/LedgeSync-0.1.0-alpha.3-windows-amd64-setup.exe)
or [ARM64 setup EXE](https://github.com/alexandroit/LedgeSync/releases/download/v0.1.0-alpha.3/LedgeSync-0.1.0-alpha.3-windows-arm64-setup.exe).
Opening it starts the installation wizard. It installs for the current user,
creates a Start menu entry and registers an uninstaller in Windows Settings.
These alpha installers are not Authenticode signed.

**Ubuntu 24.04:** [add the signed LedgeSync APT repository once](docs/PLATFORMS.md#ubuntu-apt),
then run `sudo apt-get update && sudo apt-get install ledgesync` for the graphical
app. Use `ledgesync-cli` for headless servers. These packages come from the
project's own repository, not Ubuntu's default package archive.
Google Drive authorization on Ubuntu desktop requires a running, unlocked
Secret Service credential store such as GNOME Keyring in the graphical session.

**Connect Google Drive:** open **Connections → Connect Google Drive**,
authorize in the system browser and return to LedgeSync. No end-user client
setup or JSON import is required. The application communicates directly with
Google and stores the connected account's refresh token in the OS vault; access
tokens remain in memory. Check, reconnect, cancel and disconnect are available.
See [account connection](docs/GOOGLE_DRIVE_AUTH.md) and
[maintainer build configuration](docs/OAUTH_BUILD.md). The owner has reported
successful live connection. That report does not validate the new upload flow.

Published alpha.3 browses local folders and previews an explicitly simulated,
empty destination. Connecting does not upload files in that version. The source
candidate's real upload workflow is described below. Source files remain
read-only; required unsupported policy adapters stop preview instead of being
silently skipped.

The desktop is the primary interface. Build instructions for macOS, Ubuntu,
Windows 11 and headless servers are in [PLATFORMS.md](docs/PLATFORMS.md).
Release 0.1.0-alpha.3 includes macOS DMGs, Windows setup EXEs, Ubuntu packages,
and separate desktop/CLI archives with checksums and retained license notices.
[All 16 build jobs](https://github.com/alexandroit/LedgeSync/actions/runs/36953803971),
[six native vault jobs](https://github.com/alexandroit/LedgeSync/actions/runs/36953805903),
[installer lifecycle checks](https://github.com/alexandroit/LedgeSync/actions/runs/36954256173),
and [public APT checks](https://github.com/alexandroit/LedgeSync/actions/runs/36954499569)
passed. The previous alpha.1 and alpha.2 assets remain unchanged.
Open a folder in the app to explore it with the default `.gitignore` policy,
or open a project JSON configuration to select multiple rule sources.

## Manual folder uploads in the alpha.4 source candidate

1. Open a local folder or project configuration and connect Google Drive.
2. Choose **Use My Drive** or **Choose existing Drive folder**. The latter opens
   Google's Picker in your system browser and returns the selected parent to
   LedgeSync. Selecting a destination does not upload anything.
3. Choose **Preview folder upload**. Review the connected account, destination
   ID, included/excluded entries and copy actions, then choose **Upload folder**.
4. Keep the app open until **Folder upload verified** appears. Use **Open
   destination folder on Google Drive** to inspect the result in your browser.

LedgeSync creates a managed child folder named after the local root inside the
chosen parent, retaining the included hierarchy and empty folders. Active ignore
rules apply. Verified existing copies are checked and skipped; changed local
files keep both versions using a stable `.ledgesync-` suffix. Nothing is overwritten
or deleted. Canceling preserves completed files; a fresh preview reconciles the
same recorded object IDs before continuing. An interrupted file may restart its
upload after the app restarts; session URLs are kept only in memory.

A per-user SQLite journal outside source roots stores operation IDs, checksums,
approved plans and rule-source history, without OAuth tokens or upload-session
URLs. A previously observed rule source that disappears blocks upload even after
a restart. The source folder is never modified. Closing during a transfer asks
whether to stop, with **Keep Open** as the default.

This is an explicit manual copy workflow. Automatic watching/scheduling,
bidirectional synchronization, shared-drive transfers, downloading, overwriting
and deletion remain unavailable. `drive.file` does not reveal all pre-existing
Drive contents or recursively authorize an existing parent. Read the [workflow
and acceptance checklist](docs/research/DRIVE_UPLOAD_ACCEPTANCE.md) before testing
the candidate with a disposable folder.

The secondary CLI uses the same local preview service; it has no copy/apply
command yet:

```sh
go run ./cmd/ledgesync --help
go run ./cmd/ledgesync browse --root /path/to/project --json
go run ./cmd/ledgesync config validate --config project.json
go run ./cmd/ledgesync explain --config project.json --path src/main.go --json
go run ./cmd/ledgesync plan --config project.json --output /outside/source/plan.json
go run ./cmd/ledgesync plan inspect --plan /outside/source/plan.json
go run ./cmd/ledgesync capabilities
go run ./cmd/ledgesync auth status
```

CLI previews still use a fake destination and cannot be applied. `auth status`
reads safe local connection metadata only in a configured native build; it does
not verify Google access online or open a browser. Configuration examples include
future contract fields that the current copy workflow does not enable. JSON
output and the local journal contain paths; review them before sharing diagnostics.

## Product in one sentence

Build an independent, local-first **GUI-first desktop file manager and synchronization application** that feels familiar to users of the **current Google Drive interface** while keeping its own product identity, sends selected local files to Google Drive, and uses a reusable policy engine that understands multiple configurable code-management ignore/exclude sources and dialects, explains every decision, and makes destructive synchronization explicit. The CLI remains a secondary automation and engineering surface over the exact same core.

The default discovered filename is `.gitignore`, not a mandatory filename. A project can combine `.gitignore`, `.ignore`, a user-chosen filename, rclone filter files, and policy sources from source-control/code-management ecosystems. **Filename, syntax, source mechanism, and composition policy are separate concepts.** Git uses files; Subversion uses directory properties such as `svn:ignore` and `svn:global-ignores`; Mercurial, Perforce, CVS, Bazaar/Breezy, and Fossil each have distinct behavior. A file called `.dockerignore` is not automatically Gitignore-compatible, and `rclone.conf` is a connection/credential configuration file, not an ignore dialect.

## What is fixed and what is proposed

The user's requirements are recorded in [Product requirements](docs/01_PRODUCT_REQUIREMENTS.md). They include a standalone product inspired by rclone, Google Drive as the first destination, customizable rule filenames, multiple simultaneous rule sources, and documentation suitable for Codex or Claude Code.

The proposed implementation baseline is **Go core + TypeScript desktop UI through Wails + SQLite local state**, with a native Google Drive API provider. The **desktop explorer is the primary user experience**; it uses an information architecture familiar to the **current Google Drive experience** without copying Google branding or assets. No external rclone installation is required. CLI and desktop reuse one engine; they are not two independently maintained synchronization systems. Optional read-only VCS adapters may invoke a locally installed VCS client with fixed argument arrays when the policy is not stored as a normal file (notably SVN properties); absence of that optional client must be reported as a capability limitation, never silently ignored.

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
| [Drive upload acceptance](docs/research/DRIVE_UPLOAD_ACCEPTANCE.md) | Manual folder-copy behavior, candidate limitations and live acceptance checklist |
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
