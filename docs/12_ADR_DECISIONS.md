# 12 — Architecture Decision Records

Status language: **accepted for specification** means a selected implementation baseline, not already-built code. Owner decisions are explicitly marked pending.

| ADR | Decision | Rationale / alternatives / consequences |
|---|---|---|
| ADR-001 | Independent product; no external rclone runtime. Accepted. | No separately installed rclone executable or unrestricted sync subprocess. The 2026-10-01 source-analysis directive permits vetted code adaptation or pinned in-process dependencies inside our native provider and approved-plan boundary; independence is a product/runtime boundary, not a ban on source reuse. |
| ADR-002 | Go core, TypeScript/Wails desktop, SQLite state. Accepted for specification. | One portable core and native Drive adapter; Wails documents Go/web integration. Rust/Tauri or C#/.NET remain alternatives if the packaging spike proves unsuitable. Do not switch silently; update this ADR with measured evidence. [A01] |
| ADR-003 | One modular monolith behind CLI and desktop. Accepted. | Prevent divergent sync/filter logic. Microservices/hosted backend have no initial requirement. |
| ADR-004 | Separate selectors, dialects, groups and composition. Accepted. | Custom naming is not a new grammar. Direct concatenation/translation across dialects would change rule order and negation meaning. |
| ADR-005 | Conservative default, explicit ordered advanced mode. Accepted. | Deny-wins prevents one application convention from silently overriding another; users can opt into explainable priority overrides. Switching invalidates approval. |
| ADR-006 | Git patterns-only, explicit root boundary. Accepted. | Useful for non-Git files/folders; avoids hidden dependence on index/global Git configuration. Not identical to Git's tracked-file behavior. [G01] |
| ADR-007 | Google Drive first, app-managed namespace, explicit scope capability. Accepted. | Minimize accidental access and ambiguous ownership. Existing-tree adoption/shared-drive workflows are gated, not inferred from a selected parent. [D01] |
| ADR-008 | Copy default, keep-both/pause conflicts, zero deletion caps. Accepted. | Preserve data while developing provider/journal correctness; controlled managed mirror arrives only after recovery gates. |
| ADR-009 | Immutable plans and per-operation preconditions. Accepted. | User authorization must correspond to actual work. A displayed dry-run followed by a completely fresh unchecked sync is rejected. |
| ADR-010 | Local OS vault and state outside source. Accepted. | Source-controlled config contains no secrets; prevent credentials from being uploaded with projects. No plaintext fallback. |
| ADR-011 | Reject symlinks/special nodes initially. Accepted. | Smaller safe cross-platform surface; later link support needs its own root/restore semantics. |
| ADR-012 | No universal atomicity promise for Drive. Accepted. | A version check is not necessarily atomic conditional mutation. Dedicated single-writer namespaces plus conflict checks are the supported baseline; stronger claims need method-level proof. [D06] |
| ADR-013 | English implementation artifacts. Accepted working convention. | Stable naming and agent handoff. User-facing conversation stays in the user's language; later UI localization is separate. |
| ADR-014 | License and final brand pending owner. Pending. | Do not publish a license grant, reserve a brand, or release under an assumed name. Reviewed rclone source is MIT; preserve notices for any deliberate reuse. [R16] |
| ADR-015 | No telemetry, arbitrary plugins, shell hooks or model APIs by default. Accepted. | Not required for deterministic local/cloud synchronization; avoid credential/data exposure and execution attack surface. |
| ADR-016 | Reference tests and capability labels, not “full compatibility” marketing. Accepted. | State dialect/reference versions, supported selectors/case/index behavior, and documented deviations. |

To replace an ADR, append the observed problem, alternatives, decision, owner/authorization implications, migration, and test impact. Preserve the superseded record. Do not retroactively describe a recommendation as a user-mandated technology choice.

## ADR-017 — Source-first implementation and deliberate reuse

**Accepted for specification, 2026-10-01; implementation pending.** The owner requests that the agent download https://github.com/rclone/rclone, analyze the actual code, and generate the new project. Complete the reproducible source audit in [assignment 19](19_RCLONE_SOURCE_ANALYSIS_AND_IMPLEMENTATION.md) before production implementation. Compare fork, selective adaptation, in-process reuse, and new implementation with source/test evidence. Choose per component, preserve notices and provenance, and record measured tradeoffs. Do not force a rewrite from scratch or merely relabel the upstream product. This refines ADR-001 and supersedes reference-only wording; safety, data access, scope, and publication boundaries are unchanged.

## ADR-018 — GUI-first desktop explorer; CLI secondary

**Accepted for specification, 2026-10-01; implementation pending.** The owner selected a graphical experience similar in familiarity to Google Drive instead of making a command interface the main product. The desktop Files explorer, policy inspector, paired Local ↔ Cloud preview, activity, and history are first-class deliverables. The CLI remains supported for automation/headless use and must call the same application services. “Drive-style” means familiar information architecture/interactions, not copying Google branding/assets or hiding Confirmar's explicit policy/safety model.

## ADR-019 — Generalize rule files into policy-source adapters

**Accepted for specification, 2026-10-01; implementation pending.** Selection policy can come from files, VCS properties, or versioned settings. Introduce an allowlisted `PolicySourceAdapter` registry and keep source mechanism separate from dialect grammar. Required built-in compatibility targets include Git, Mercurial, SVN, Perforce, CVS, Bazaar/Breezy, Fossil, and rclone; code-tool formats use the same extension model. This supersedes wording that implied every source is a normal file.

## ADR-020 — SVN properties through supported read-only integration, not `.svn` database parsing

**Accepted for specification, 2026-10-01; implementation pending.** SVN uses `svn:ignore` and `svn:global-ignores` directory properties. The first adapter may use an installed `svn` client through a narrow fixed-argument, read-only `ProcessRunner` and XML output; a later native binding may replace it. Do not treat `.svn/wc.db` as a public stable API and do not invent `.svnignore`. Missing required capability fails closed. The app remains usable for non-SVN projects without an SVN installation.

## ADR-021 — Selective rclone parser adaptation; independent safe offline core

**Accepted for implementation, 2026-10-01.** The actual official rclone v1.75.1 tree at `687d264b689b8c49a67e2e52a8a5e0caa01c04ce` was cloned and reviewed before application implementation. See [source audit](research/RCLONE_SOURCE_AUDIT.md), [baseline](research/UPSTREAM_BASELINE.json) and [reuse matrix](research/RCLONE_REUSE_MATRIX.md).

The concrete problem is that upstream command/RPC/sync integration imports configuration/global state and performs live traversal-driven mutations, whereas Confirmar requires immutable policy snapshots and approved object-level plans. Upstream Drive listing also logs `incompleteSearch` without converting it into a completeness error, and name lookup can select the first duplicate. These are contract differences, not a claim of an upstream security vulnerability.

Choose a standard-library offline core and selectively adapt `fs/filter/glob.go` conversion/directory-inference code, preserving the upstream MIT copyright/permission notice and full source pin. Keep source discovery, Git/rclone dialects, group composition, explanations and planning independent. Do not import the whole rclone Go module, fork/rename the whole product, use librclone RPC, or invoke an installed rclone during production. A pinned development oracle is permitted. The source/test review established reuse boundaries; local upstream `fs/filter` and `lib/pacer` tests passed in isolated configuration with no cloud credentials.

Tradeoffs: extraction avoids unused backend/dependency/global state but makes Confirmar responsible for tracking upstream parser fixes and carrying differential/regression tests. Keep source hashes and a list of local changes; review newer stable releases deliberately. Do not claim full rclone compatibility from the parser alone. Native Drive upload/retry mechanisms remain candidates for later selective adaptation after fake-server, identity, journal, cancellation and ambiguous-response contracts are tested. This is a scoped decision, not a full upstream dependency or security audit.

No persistent-state migration exists yet. The first slice contains strict configuration, multi-source policy evaluation, explain, deterministic read-only preview, a fake provider and a local desktop explorer using the same core. Missing/invalid/unsupported policy and incomplete scans fail closed. Initial preview has no deletion/cloud-write capability. Application licensing is resolved by the owner's current Apache-2.0 request; MIT notices remain required for adapted portions. No account connection or cloud-write authorization is inferred from the source audit.

## ADR-022 — Owner-authorized open source and platform expansion

**Accepted, 2026-10-01.** The owner explicitly requested starting the project, a
public GitHub project and website, Apache licensing, and versions for Ubuntu,
macOS ARM and AMD/Intel, Windows 11 and server systems. Original Confirmar code
is licensed under Apache-2.0; third-party notices and licenses remain intact.
This resolves the license part of ADR-014. Confirmar remains the working name.

Target ARM64 and x64 for macOS, Ubuntu desktop/server, and Windows. Headless
servers use the same Go core through the CLI; there is no automatically installed
service or scheduler. Native desktop builds use Wails. CI build evidence and
installation/runtime acceptance are reported separately in PLATFORMS.md.

The first distributed version is an explicitly labeled offline alpha: local
exploration, policy explanations and fake-destination previews. Real Drive
OAuth, mutations, scheduling, mirror and restore remain gated by their milestone
requirements. Public source/site authorization does not grant access to private
Google Drive accounts or authorize synchronization.
