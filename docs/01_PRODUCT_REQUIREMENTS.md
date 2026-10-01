# 01 — Product Requirements

**Status:** implementation specification. Requirement IDs are stable. “Must” is normative; a planned milestone is not an implementation claim.

## Confirmed user requirements

| ID | Requirement |
|---|---|
| U01 | The product name is **LedgeSync**. Use `ledgesync` for the executable/CLI command and `ledgesync.com` for the website/domain. |
| U02 | Build an independent application inspired by rclone's useful capabilities, not instructions for manually operating rclone. |
| U03 | Send selected local projects/files to Google Drive; do not depend on Google Drive for desktop ignoring files. |
| U04 | Use `.gitignore` as the default rule filename, with user-configurable names. |
| U05 | Read multiple rule sources simultaneously, including `.gitignore`, `.ignore`, and arbitrary user-chosen names. |
| U06 | Support rclone-format filtering in addition to Gitignore syntax; map each filename explicitly to its dialect. |
| U07 | Make the solution reusable across application/workflow conventions instead of hardcoding one application. |
| U08 | Supply detailed rules, architecture, research, tests, and execution instructions for Codex or Claude Code. |
| U09 | Make the graphical desktop experience the primary product surface, with a file-browser experience familiar to the **current Google Drive** interface; keep CLI as a secondary automation/headless interface over the same engine. |
| U10 | Support code-management exclusion/policy conventions broadly, including Git, Mercurial, Subversion/SVN, Perforce, CVS, Bazaar/Breezy, Fossil, and extensible adapters for additional code-tool ignore formats. SVN support must model `svn:ignore` / `svn:global-ignores` properties rather than inventing a fake `.svnignore` standard. |

Recovered prior context supports a local Mac application, folder/destination selection, manual and automatic operation, subfolder rule handling, configurable precedence, and a preview showing the applied rule. These are carried forward below. A complete transcript was not accessible through the shared link; no unstated language/framework commitment is inferred.

## Proposed implementation baseline

Go for the shared core, Wails with TypeScript for desktop, SQLite for durable local state, and direct Google Drive API integration. macOS is the first desktop acceptance target; Linux and Windows must remain viable through portable ports and CI tests. These choices are ADRs, not falsely attributed user statements. The chosen product identity is fixed: **LedgeSync**, command `ledgesync`, domain `ledgesync.com`.

The architecture must permit a future reusable library and additional cloud providers. It must not require a hosted backend, a paid service, a model API, Google Drive desktop, Git installation for basic filtering, or a separately installed rclone for ordinary operation.

## Functional requirements

| ID | Required behavior | Delivery gate |
|---|---|---|
| F01 | Multiple independent projects with one local root and one explicitly authorized remote root per project | P2 |
| F02 | Recursive default `.gitignore` discovery with correct directory-relative interpretation | P1 |
| F03 | Arbitrary configured basenames; multiple files per directory; enable/disable and reorder controls | P1/P3 |
| F04 | Explicit Gitignore, rclone-filter, rclone-include, and rclone-exclude dialects | P1 |
| F05 | Deterministic conservative and ordered composition with provenance and conflict warnings | P1 |
| F06 | Explain one path and preview an entire plan without changing local or remote data | P1/P2 |
| F07 | Native Google OAuth, account verification, authorized destination selection, disconnect | P2/P3 |
| F08 | Incremental copy, bounded concurrency, progress, cancellation, retry, and resumable uploads | P2 |
| F09 | Content-integrity verification and truthful per-file/run reports | P2 |
| F10 | Explicit conflict handling and recovery copies before permitted managed overwrites | P2/P4 |
| F11 | Optional manual mirror mode with deletion guards and trash-based recovery | P4 |
| F12 | Restore to a new local directory with collision/path safety; no source replacement by default | P4 |
| F13 | Desktop workflow and CLI using the exact same planning/filtering core | P3 |
| F14 | Opt-in scheduled and watch-triggered copy with sleep/offline/missing-volume handling | P3 |
| F15 | Configuration import/export without secrets; schema/version migration | P1/P3 |
| F16 | Presets for developer output and sensitive paths, visibly editable and never silently inferred | P3 |
| F17 | Searchable logs and diagnostics with redaction and bounded retention | P2/P3 |
| F18 | Provider and dialect capability reporting; no false compatibility claims | All |
| F19 | Local filesystem fake/test provider and deterministic fault injection | P1/P2 |
| F20 | A staged rclone capability catalog, including future multi-cloud, bidirectional, crypt, mount, and serve work | P5+ |
| F21 | GUI-first Drive-style file explorer: local/cloud navigation, list/grid views, breadcrumbs, search, status badges, context actions, and a toggle to reveal excluded files | P1/P2 |
| F22 | Paired Local ↔ Cloud comparison view showing planned direction, state, conflicts, excluded items, and exact reason/provenance before mutation | P2/P3 |
| F23 | Extensible `PolicySourceAdapter` registry supporting file-backed and metadata/property-backed sources, with built-in profiles for major VCS families including SVN | P1/P2 |
| F24 | Per-adapter capability/version reporting and compatibility tests; unsupported or unavailable adapters fail closed and never silently upload paths that may have been excluded | All |

## User stories and acceptance examples

A developer creates a project pointing to a local source tree and Drive. With only `.gitignore` discovery enabled, ignored dependency/build files are not uploaded. The user can inspect any decision and see its filename, line, dialect, and matching pattern.

A non-Git user names a rules file `team-upload-rules.txt`, chooses Gitignore syntax, and uses it without creating a Git repository. An rclone user adds two filter files in explicit order; reset semantics operate within that rclone group and never erase another group's rules.

A project combines Gitignore and rclone sources. A restrictive source remains restrictive under the default conservative policy. Switching to ordered overrides shows a before/after difference and requires explicit review. Unknown dialects and malformed rules produce errors, not “best effort” uploads.

A user opens the desktop application and browses local folders and the authorized Drive destination without learning commands. Excluded files can be hidden by default or revealed as dimmed rows. Selecting an excluded file shows the adapter, source, rule/property, and reason. A paired Local ↔ Cloud view shows what will upload, remain untouched, conflict, or be skipped before the user approves anything.

An SVN working copy is recognized through an explicit SVN policy adapter. The application reads `svn:ignore` and inherited `svn:global-ignores` as directory properties using a supported read-only adapter; it does not parse `.svn/wc.db` as an undocumented contract and does not invent a `.svnignore` file. A missing optional SVN capability blocks SVN-policy application with a clear message rather than falling back to upload-all.

A user unplugs an external SSD. A scheduled job pauses as source unavailable; it never interprets the missing mount as an empty directory and never deletes remote files.

A user edits ignore rules after preview. Application of the old plan is rejected as stale. Previously uploaded files that are now excluded are preserved, not silently deleted.

## Delivery boundaries

P0 establishes reproducible development and tests. P1 delivers offline filtering/configuration **and a functional local GUI explorer using the fake provider**. P2 connects the GUI to native Drive browsing/copy after sandbox authorization. P3 completes paired-view UX, accessibility/polish, and safe automation. P4 completes the first full release with controlled mirror and restore only after safety gates pass. P5+ implements additional rclone-inspired capabilities without changing the default trust model.

No release may claim complete rclone parity until every advertised capability is implemented and tested. A virtual filesystem, bidirectional sync, remote execution/serve endpoints, production encryption, arbitrary third-party plugins, remote-to-remote operations, and advanced rclone metadata/hash filters are not shortcuts into the first release.

## Nonfunctional requirements

Safety and correctness take priority over throughput. The core must be deterministic for a fixed configuration, rule snapshot, source inventory, destination inventory, and clock. It must remain responsive under cancellation and respect configured resource limits.

Use bounded memory for transfers, paginated listings, a persistent inventory for large trees, and backpressure. Initial targets, not measured claims: a 100,000-file local planning benchmark should remain below 512 MiB core RSS on the documented reference environment; incremental idle mode should avoid continuous full-tree scans. Benchmark conditions and observed results must accompany any performance claim.

No telemetry by default, no advertisements, no application account/password database, no uploading file contents to model providers, and no public shares. OAuth authorization is not a reason to add a separate web-service login. Device/Google account security remains separate from this utility's local trust controls.

## Out-of-scope interpretations

This product does not modify Google Drive desktop's internal exclusion behavior or manipulate file permissions/hidden flags to deceive that client. It transfers through its own authorized provider. “Multi-application” means multiple explicitly declared rule conventions; it does not mean every file named `.*ignore` shares a grammar. The scope is exclusion/selection policy relevant to file transfer; arbitrary VCS metadata such as `.gitattributes`, repository credentials, hooks, or executable project configuration is not automatically interpreted as an ignore policy. A complete operating-system backup, preservation of all ACLs/xattrs, and coherent snapshots of running databases require separate designs.
