# 11 — Dependency-Ordered Implementation Backlog

Every task begins `NOT STARTED`. Documentation validation is not implementation progress. Work in small reviewable changes; preserve existing user code. Each completion report names changed files, commands and actual results, remaining risks, and the next task. A task is done only when its acceptance evidence exists.

The mandatory download/audit/reuse workflow is specified in [assignment 19](19_RCLONE_SOURCE_ANALYSIS_AND_IMPLEMENTATION.md). P0-00A–C are new, unexecuted tasks; complete them before production code and then proceed into actual implementation.

## P0 — Reproducible foundation

| ID | Work and dependency | Acceptance evidence |
|---|---|---|
| P0-00A | Acquire isolated upstream clone and pin a stable tag/full SHA; no dependency | Actual source checkout, dates, toolchain/license hashes, and baseline JSON; no user credentials or source-root mutation |
| P0-00B | Audit relevant rclone implementation and tests; P0-00A | Source audit with real paths/symbols/line ranges, pinned permalinks, isolated test evidence, and limitations |
| P0-00C | Select implementation/reuse strategy; P0-00B | Component reuse matrix, provenance/notices, ADR, dependency/security review, and first implementation slice selected |
| P0-01 | Inspect checkout, establish version/dependency/license register; P0-00C | Pin compatible stable Go/Wails/Node and Git/rclone reference versions; record checksums and provenance; no global install side effects |
| P0-02 | Create Go core/CLI layout and TypeScript desktop skeleton; P0-01 | Local hello/status path, shared service boundary, formatting/lint/test commands, no network or secrets |
| P0-03 | Define typed IDs, paths, errors, capability interfaces and injected clocks; P0-02 | Domain tests, safe paths, no SDK/UI imports in domain, process cancellation contract |
| P0-04 | CI and dependency controls; P0-02 | Pinned actions/dependencies, least permissions, formatting/test/race checks, no cloud secrets in untrusted PRs |

## P1 — Offline policy and planning

| ID | Work and dependency | Acceptance evidence |
|---|---|---|
| P1-01 | Strict configuration and migration framework; P0-03 | Schemas mirrored in loader, duplicate/unknown/path checks, example round trips, secrets rejected |
| P1-02 | Gitignore parser and hierarchy; P1-01 | GIT-001–028 plus expanded grammar/fuzz corpus compared to actual product; patterns-only distinction visible |
| P1-02A | Policy-source adapter registry and typed source snapshots; P1-01 | File/property source interfaces, capability/version metadata, invalid combination checks, no arbitrary plugin execution |
| P1-03 | rclone filter/include/exclude adapters; P1-01 | RCL-001–012 plus expanded pinned reference tests; first-match/reset/include fallback/directory semantics |
| P1-03A | Major VCS policy adapters: Mercurial, SVN, Perforce, CVS, Bazaar/Breezy, Fossil; P1-02A | Per-profile parser/reader tests, read-only reference/oracle evidence, property inheritance for SVN, truthful unavailable capability behavior |
| P1-04 | Composition and explain service/CLI; P1-02, P1-03, P1-03A | MIX corpus and fixed-guard tests; custom filenames/property provenance, group ordering and exact explanations |
| P1-05 | Safe local discovery and immutable rule snapshots; P1-04 | Symlink/mount/permission tests, stable enumeration, parent barriers and pruning/non-pruning equivalence |
| P1-06 | Fake provider and pure planner; P1-05 | Complete inventories, deterministic operation graph, preview without mutation, conflicts/ownership represented |
| P1-07 | SQLite journal, migrations and root-pair locks; P0-03, P1-06 | Restart/transaction tests, state outside source, no plaintext secret records, overlapping-root detection |
| P1-08 | Offline integration gate and versioned JSON contracts; P1-06, P1-07 | CLI exit/event schemas, plan digest tests, local fixture suite and documentation updated |
| P1-09 | GUI-first local Files explorer and policy inspector using fake provider; P1-04, P1-06 | List/grid navigation, breadcrumbs/search, Show excluded, Explain parity, paired preview prototype, keyboard/accessibility smoke tests |

## P2 — Native Google Drive copy alpha

| ID | Work and dependency | Acceptance evidence |
|---|---|---|
| P2-01 | Security review, vault and OAuth flow; P1-08 | D01/D02 workflow documented; no webview password login; callback/state/redaction tests; live account authorization required |
| P2-02 | Native provider identity/listing/capabilities; P2-01 | Disposable namespace, pagination and duplicate-name tests; no assumption of parent-selection descendant access |
| P2-03 | Journaled resumable new-file uploads; P2-02 | Stable IDs, bounded workers, source-change checks, pause/resume and ambiguous-response reconciliation |
| P2-04 | Verification and unchanged-file detection; P2-03 | Streamed/provider digest agreement, read-back fallback capability, no metadata-only content success |
| P2-05 | Conflict/keep-both and recovery-copy primitive; P2-04 | Unknown objects preserved, deterministic collision mapping, repeated retries do not multiply copies |
| P2-06 | Executor crash/retry/cancel/error classification; P2-03, P1-07 | Checkpoint fault injection, honest partial states, bounded retries, no source mutation |
| P2-07 | Authorized end-to-end copy acceptance; P2-04–06 | Relevant SAFE cases, unchanged rerun, malformed-filter fail-closed, live-test cleanup only exact fixture IDs |
| P2-08 | Copy alpha operations/diagnostics gate; P2-07 | Redacted report, secure disconnect, capability/known-limit docs, no destructive mode enabled |
| P2-09 | Connect primary desktop explorer to Drive provider; P1-09, P2-08 | Cloud browse/listing, Local ↔ Cloud paired preview, statuses/progress, no frontend-only transfer decisions |

## P3 — Desktop and optional automation

| ID | Work and dependency | Acceptance evidence |
|---|---|---|
| P3-01 | Project/connection/policy-source setup refinement; P2-09 | GUI is primary path, explicit adapter/dialect/source ordering/capability, same core as CLI, no unsafe frontend binding |
| P3-02 | Explorer/paired-view/preview/explain/progress/history polish; P3-01 | Keyboard/accessibility tests, large-list virtualization, truthful verified progress/cancellation, excluded-file visibility and reason parity |
| P3-03 | Opt-in interval/watch scheduler; P3-02 | Copy-preauthorization only, unchanged-policy restriction, locks/coalescing, rule changes pause, overflow reconciliation |
| P3-04 | Desktop packaging and beta gate; P3-03 | Clean macOS installation/vault/uninstall first; other OS claims only after their CI/packaging passes |

## P4 — Controlled managed mirror and recovery

| ID | Work and dependency | Acceptance evidence |
|---|---|---|
| P4-01 | Verified recovery catalog and restore-to-new-location; P2-05, P3-04 | Hash-verified recovery and safe restore, retention visibility, no overwrite-source default |
| P4-02 | Managed updates and provider concurrency gate; P4-01 | Single-writer contract, observed-version conflict checks, exact supported API preconditions documented; leave unsupported operations disabled |
| P4-03 | Managed-mirror planner/executor; P4-01, P4-02 | All S01–S16 and SAFE scenarios, caps/approval/complete scans, excluded/unmanaged protection, no recursive folder trash |
| P4-04 | Full initial-release safety review; P4-03 | Human review of destructive paths, successful recovery rehearsal, signing/privacy/license decisions resolved |

## P5+ — Explicitly separate roadmap

Add providers only through the same contract suite. Candidate capabilities: OneDrive/S3/SFTP, additional code-tool policy profiles beyond the built-in VCS set, literal manifests, metadata/size/age/marker rules, richer scheduling, bandwidth controls, bidirectional reconciliation, encrypted content, snapshot archives, mount/VFS, and serve/API. Each requires a new ADR, threat model, capability contract, tests, and migration plan. None is automatically authorized by the rclone comparison table.

Bidirectional sync in particular requires two-sided baselines, conflict semantics, deletion propagation rules, offline recovery, and independent release gates; it is not a small option on copy. Public branding, cloud OAuth publication, code signing and final licensing remain owner decisions.

## Per-change definition of done

A change is traceable to a task/requirement, has tested behavior and errors, preserves module boundaries and safety defaults, updates documentation/contracts, includes no secrets/unrelated changes, and passes actual required checks. Report skipped tools/tests as skipped. Never mark future integration or performance checks complete based only on a mock or specification.
