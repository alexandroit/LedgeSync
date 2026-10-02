# LedgeSync

> **Canonical identity:** read [PROJECT_IDENTITY.md](PROJECT_IDENTITY.md) first. It defines LedgeSync, `ledgesync`, and `ledgesync.com`.

**Product name:** **LedgeSync**.
**CLI command:** `ledgesync`.
**Primary website/domain:** `ledgesync.com`.
**Specification version:** 0.2.1 · **Prepared:** 2026-10-01.
**Implementation status:** offline developer alpha; no production release or live Drive transfers. See [current implementation status](docs/17_AGENT_HANDOFF_AND_STATUS.md) and [platform builds](docs/PLATFORMS.md).

**Public project:** [GitHub](https://github.com/alexandroit/LedgeSync) ·
[Website](https://ledgesync.com/) ·
[Download offline alpha](https://github.com/alexandroit/LedgeSync/releases/tag/v0.1.0-alpha.1) ·
[Builds](https://github.com/alexandroit/LedgeSync/actions/workflows/ci.yml) ·
[Apache-2.0 license](LICENSE).
The website is live on the owner's Ubuntu server with HTTPS. See
[deployment and renewal details](docs/WEBSITE.md).

## Try the offline alpha

The first implementation browses local folders, explains policy decisions and
creates plans against an explicitly simulated, empty destination. Plans are
deterministic for the same snapshots and creation time.
It contains no live Google Drive connection, transfer executor or scheduler.
Source files are read-only. Additional VCS adapters are reported as unavailable
and required unsupported sources stop preview rather than being skipped.

The desktop is the primary interface. Build instructions for macOS, Ubuntu,
Windows 11 and headless servers are in [PLATFORMS.md](docs/PLATFORMS.md).
Release 0.1.0-alpha.1 provides six desktop and six CLI archives, with checksums
and retained license notices. All 16 jobs passed in the
[release CI run](https://github.com/alexandroit/LedgeSync/actions/runs/36942481310).
Open a folder in the app to explore it with the default `.gitignore` policy,
or open a project JSON configuration to select multiple rule sources.

The CLI calls the same application service:

```sh
go run ./cmd/ledgesync --help
go run ./cmd/ledgesync browse --root /path/to/project --json
go run ./cmd/ledgesync config validate --config project.json
go run ./cmd/ledgesync explain --config project.json --path src/main.go --json
go run ./cmd/ledgesync plan --config project.json --output /outside/source/plan.json
go run ./cmd/ledgesync plan inspect --plan /outside/source/plan.json
go run ./cmd/ledgesync capabilities
```

Configuration examples describe the complete contract, including future Drive
destinations. In this alpha every preview uses a fake destination. A plan can
be inspected but cannot be applied. JSON output contains paths from the selected
folder; review it before sharing diagnostics.

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
