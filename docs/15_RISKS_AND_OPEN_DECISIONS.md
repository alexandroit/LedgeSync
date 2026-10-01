# 15 — Risks and Open Decisions

## Decisions that do not block offline implementation

| Decision | Current safe baseline | Resolution point |
|---|---|---|
| Final name | Keep LedgeSync, no brand/domain purchase | Owner before publication |
| Repository license | No inferred license grant | Owner before source publication |
| Hosting/repository destination | Work only in provided checkout/Drive docs | Owner before creating/publishing repository |
| Signing accounts and public OAuth project | No credentials assumed | Owner before public release |
| Secondary desktop OS priority | macOS first; portable core | Packaging/release planning |
| Additional cloud providers | Interfaces now, implementations later | Separate scoped milestone |

Do not repeatedly ask these questions to avoid building the safe offline foundation. Record unresolved decisions and continue tasks that do not require them.

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
