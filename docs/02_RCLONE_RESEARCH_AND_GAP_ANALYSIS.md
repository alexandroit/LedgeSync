# 02 — rclone Research and Gap Analysis

**Research date:** 2026-10-01. Sources are identified in [14_SOURCES_AND_PROVENANCE.md](14_SOURCES_AND_PROVENANCE.md). This is a focused design review, not a claim to have audited all of rclone or measured its performance.

## What was actually inspected

Official documentation for filtering, copy, sync, check, bisync, Google Drive, cloud backend capabilities, crypt, mount, serve, remote control, GUI, and licensing was reviewed. The following source ranges were also read through GitHub:

- `fs/filter/filter.go`, lines 1–260; blob `e00d0e8b3e481ff297f9fcb63379b5b6392ed55a`.
- `fs/filter/rules.go`, lines 1–260; blob `e5ee2626e778ba01d9aea9d92153c00f4d96be78`.

The hashes identify observed file blobs, not a release or a whole-repository commit. No complete backend/security audit, benchmark, native app build, or rclone differential execution was performed for this package. P0 must pin an exact release/commit and extend the source review before implementation relies on internals.

The inspected code separates filtering options, compiled path/directory rules, and ordered rule parsing. It also contains package-level default configuration. Our design will use immutable per-project/per-run state, avoiding cross-job global configuration coupling. This is an architectural choice, not an allegation that rclone's normal CLI use is unsafe. [R14, R15]

## Capability catalog and adoption plan

| rclone capability | LedgeSync treatment | Stage | Evidence |
|---|---|---|---|
| Copy with unchanged-file avoidance | Native incremental upload; no deletion in copy mode | P2 | R02 |
| One-way sync | Explicit managed-object mirror with stricter deletion policy | P4 | R03 |
| Move/copyto/moveto | Future explicit operations; never silently delete source | P5 | R01 |
| Check/checksum/hash reporting | Verification with honest unknown/hash-unavailable results | P2 | R04 |
| Listing/tree/size/about | Project inventory, usage, and summaries | P2/P3 | R01 |
| Multiple remote backends | Provider port first; additional adapters later | P5 | R05 |
| Google Drive backend | Native API integration; vetted source adaptation/in-process reuse permitted | P2 | R06 |
| Repeated filter files | Explicit ordered sources inside one dialect group | P1 | R07 |
| Include/exclude/filter dialects | Supported distinctly; do not confuse fallback semantics | P1 | R07 |
| Size/age filters | Explicit metadata gate, frozen evaluation time | P5 | R07 |
| `files-from`, raw, NUL lists | Separate manifest selector in later capability; not an ignore parser | P5 | R07 |
| Exclude-if-present marker | Declarative marker selector after traversal tests | P5 | R07 |
| Metadata/hash partition filters | Preserve as backlog, no initial compatibility promise | P5 | R07 |
| Dry-run/interactive | Persisted preview plan and explicit authorization | P1/P2 | R02, R03 |
| Retry, concurrency, bandwidth controls | Bounded jobs and typed retries first; configurable bandwidth shaping later | P2/P5 | R01 |
| Server-side copy/move | Optional capability, never emulated unsafely | P4/P5 | R01, R05 |
| Backup-directory style retention | Project-managed recovery history before overwrites | P4 | R01 |
| Bidirectional synchronization | Separate baseline/conflict/tombstone design and acceptance gate | P5 | R08 |
| Crypt wrapper | Optional mature audited encryption integration; no home-grown cryptography | P5+ | R09 |
| Mount and VFS caching | Optional later subsystem, not required for direct upload | P5+ | R10 |
| Serve protocols | Off by default; separate network threat model | P5+ | R11 |
| Remote-control API | Typed local interface initially; no public control server | P3/P5 | R12 |
| GUI | Primary Drive-style desktop explorer over the shared core; rclone GUI is reference context, not the intended UX | P1/P2/P3 | R13 |
| Dedupe | Diagnose duplicate IDs/names; never auto-pick or delete duplicates | P2/P5 | R03, R06 |
| Archives/compression/union/chunker | Later provider decorators with capability contracts | P5+ | R05 |
| Connection configuration | App-managed accounts; no automatic plaintext credential import | P2 | R01, R06 |
| Public links/sharing | Not part of first release; require separate explicit permission | P5+ | R01 |
| Purge/cleanup | No generic recursive purge in initial product | Deferred | R01, R07 |

This catalog preserves the long-term scope while making the first implementation tractable. Each future capability needs a design record, provider capability tests, and a safety review. Adding a UI button without an implementation is not parity.

## Important semantic findings

Gitignore gives precedence to later applicable rules and directory hierarchy; an excluded parent prevents ordinary re-inclusion of its descendants. Tracked-file behavior is part of Git itself, not just pattern parsing. [G01]

rclone's ordered filter list uses the first applicable rule. Filter-file `!` is a reset, not a Gitignore-style negation. Unmatched filter-file paths are not implicitly denied merely because a `+` rule exists. Include-file mode has a different implicit fallback. [R07, R15]

Copy does not delete destination-only files. Sync may delete destination content; rclone's documentation also describes error protection and preservation of excluded destination files unless explicitly overridden. Our product should retain those distinctions, not present all operations as a generic “sync” button. [R02, R03]

The official filtering options and inspected configuration do not establish native recursive `.gitignore` semantics. We therefore treat that integration as a product gap to implement and test, rather than claiming that feeding `.gitignore` to `--exclude-from` is equivalent. This conclusion is limited to the reviewed sources; recheck upstream at the pinned version. [R07, R14]

## Product value to add

The differentiator is not merely “rclone with a GUI.” The GUI is now the primary product experience: a familiar file explorer and paired Local ↔ Cloud preview backed by a configurable, explainable policy engine with safe composition across dialects, per-project profiles, policy-source discovery, stale-plan detection, protected defaults, and native onboarding. A single path explanation must show every contributing group and the final composition result, not just a boolean.

Make arbitrary filenames **and non-file policy sources** first-class. SVN `svn:ignore` / `svn:global-ignores` properties are the canonical example of why a file-only design is insufficient.  The user should configure a file such as `release-rules.txt` as `rclone-filter` or `gitignore` independently of its name. Offer `.ignore` as an optional Gitignore-dialect preset; never scan every hidden file as configuration.

Make changes visible. Adding, removing, reordering, or changing rule files produces a reviewed inclusion/exclusion diff. A disappeared mandatory filter is a blocking event. Newly ignored remote files are retained until an explicit cleanup workflow exists.

## Build versus reuse

Updated baseline (2026-10-01, specification 0.2.0): download and audit actual rclone source/tests before choosing the implementation strategy. Build our domain model, policy engine, approved-plan executor, state journal, and native Google Drive integration; use vetted source adaptation or pinned in-process packages where they satisfy these contracts. New code is required for genuine gaps, not merely to disguise upstream origins. A separately installed rclone process must not be the application behind a facade. See [the mandatory assignment](19_RCLONE_SOURCE_ANALYSIS_AND_IMPLEMENTATION.md).

Evaluate relevant embedded rclone packages now during the source audit, including for the initial Drive target. A subprocess adapter is not the baseline and remains a separately gated future architectural change. It introduces version coupling, configuration isolation, capability mismatches, licensing inventory, and plan-execution hazards. It must not bypass the approved object-level planner. In particular, passing a filtered manifest to an unrestricted `sync`/`purge` command is prohibited.

rclone is MIT-licensed; any actual reuse must retain required notices and comply with dependency licenses. The final license for LedgeSync is an owner decision, not automatically granted by this document. No upstream source has been pasted into this package. [R16]

## Reference implementation plan

P0 records exact Git and rclone versions and hashes in CI. Compare a standalone Gitignore group against Git, and a standalone rclone group against read-only rclone listings. P1 adds versioned/read-only oracles for each advertised VCS adapter, especially SVN property scope/inheritance. Mixed-dialect composition is tested against this product's specification, not against a nonexistent common standard. Include negative tests and malformed inputs. Fix discrepancies or publish a narrow compatibility profile before release.
