# 15 — Risks and Open Decisions

## Current decisions and unresolved acceptance

| Decision | Current safe baseline | Resolution point |
|---|---|---|
| Final name | Owner selected LedgeSync, command ledgesync, domain ledgesync.com | Resolved; PROJECT_IDENTITY.md |
| Repository license | Owner requested Apache-2.0; third-party licenses preserved | Resolved; LICENSE and THIRD_PARTY_NOTICES.md |
| Hosting/repository destination | Public alexandroit/LedgeSync; ledgesync.com live on the owner's existing Ubuntu server; GitHub Pages secondary | Resolved; WEBSITE.md and ADR-025 |
| Signing accounts and public OAuth project | Alpha.4 official desktop/native CLI builds include the publisher Desktop client; the owner reported Production OAuth and enabled Picker. Native credential vaults are implemented. | Apple Developer ID/notarization and Windows Authenticode remain unavailable; live upload/SSH acceptance and the reported non-working sync remain unresolved. See [account setup](GOOGLE_DRIVE_AUTH.md). |
| Desktop OS targets | macOS and Ubuntu ARM64/x64; Windows 11 ARM64/x64; CLI for servers | Owner requested; see PLATFORMS.md for actual validation |
| Additional cloud providers | Interfaces now, implementations later | Separate scoped milestone |

Do not repeatedly ask resolved questions. Continue local work while genuinely
external inputs are pending, and keep observed results separate from assumptions.

The owner now reports that files do not synchronize. Manual-copy code and recorded
alpha.4 native distribution exist; neither proves the failing installed journey.
Use the [Claude Code handoff](../CLAUDE_CODE_HANDOFF.md) for reproduction, safe
error diagnosis and ordered completion. Automatic watch/schedule remains absent.
Ubuntu online use requires the user's D-Bus session and an unlocked Secret Service
collection; installing a keyring alone does not establish those prerequisites.
No plaintext credential fallback is permitted. Existing OAuth client configuration
must be reused through the maintainer build path; end users must not import JSON.

## Engineering risks and required spikes

| Risk | Severity | Required response / exit criterion |
|---|---|---|
| Native Gitignore equivalence | High | Expanded differential corpus; no generic-glob substitution without proof |
| VCS policy semantic drift (SVN/hg/P4/CVS/Bazaar/Fossil) | High | Versioned per-adapter oracle suites and compatibility labels; fail closed when unavailable |
| Optional VCS executable abuse/hook execution | High | Fixed argv/sanitized environment/read-only commands, no shell or repository hooks, adversarial process tests |
| GUI diverges from planner/core | High | GUI consumes typed plan/explain DTOs; parity tests against CLI/core; no TypeScript-only selection logic |
| Cross-group traversal/negation | High | Per-group hierarchy state and non-pruned oracle equivalence |
| Scope does not expose chosen existing subtree | High | Sandbox actual selection flow; app-managed namespace first; explicit broader-scope decision |
| Provider race between version check and mutation | Critical for destructive modes | Document exact method capability, single-writer model, preserve recovery; gate unsupported update/trash |
| Upload ambiguity duplicates files | High | Journal, marker/ID reconciliation, stop unknown results |
| Missing volume or partial inventory looks empty | Critical | Completeness/root-identity guards and fault tests before mirror |
| Hash/type mismatch for native documents | High | Binary-only default, explicit unsupported capability/export profile |
| OS path/symlink/case/Unicode behavior | High | Platform-specific safe-open tests and collision rejection |
| Recovery storage/quota failure | Critical before overwrite | Verified recovery prerequisite, visible retention/quota status |
| Desktop framework packaging issues | Medium | P0 macOS spike; ADR update before stack change |
| Large-rule/tree performance | Medium | Bounded state and repeatable benchmarks; no advertised numbers before measurement |
| Lost state database | High | No remote ownership inference from absence; recovery/reconstruction procedure |
| Feature creep toward all rclone backends | High delivery risk | Enforce phases; ship useful safe copy before mount/bisync/serve |

## Explicit non-guarantees

No promise of a point-in-time backup of active applications, unlimited API quota, atomic multi-file cloud transactions, distributed multi-writer exclusion, universal filesystem metadata preservation, complete rclone feature parity, one universal `.ignore` syntax, or compatibility with all future upstream releases. “Broad VCS support” is a versioned adapter catalog, not a claim that every historical/proprietary tool or future version is already supported.

These limitations do not weaken the required safety behavior: unsupported or ambiguous states must be visible and block unsafe operations rather than be hidden behind optimistic assumptions.
