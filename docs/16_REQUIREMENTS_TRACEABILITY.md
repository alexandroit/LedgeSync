# 16 — Requirements Traceability

Identifiers originate in product requirements and sync safety. A trace points to implementation work and acceptance evidence, not completed functionality.

| Requirement | Owning specification | Backlog | Evidence |
|---|---|---|---|
| U01 | README / provenance | Documentation delivery | Verified Projects/Confirmar directory and manifest |
| U02 | Architecture / ADR-001 | P0-02, P2-02 | Runtime independent of rclone |
| U03 | Drive / safety | P2-01–08 | Authorized copy journey |
| U04–U05 | Filter specification | P1-01, P1-02, P1-04 | Custom basenames, multiple sources, nested hierarchy tests |
| U06 | Filter/rclone research | P1-03 | Pinned rclone differential suite |
| U07 | Architecture / filter groups | P1-04, P1-08 | Reusable policy interfaces, composition corpus |
| U08 | Agent entry files / handoff | P0-01 onward | Task reports, validated documentation links |
| U09 | Desktop explorer / ADR-018 | P1-09, P2-09, P3-01–02 | GUI-first navigation, Show excluded, paired Local ↔ Cloud preview, same core as CLI |
| U10 | Filter spec / ADR-019–020 / source register | P1-02A, P1-03A | Versioned adapter catalog and reference/oracle suites including SVN property inheritance |
| F01 | Configuration / desktop | P2-02, P3-01 | Source/account/root identity confirmation |
| F02–F04 | Filter specification | P1-01–03 | GIT and RCL corpus plus expanded grammar |
| F05–F06 | Filter / CLI contracts | P1-04, P1-06 | MIX corpus and exact explain/plan agreement |
| F07 | Drive / security | P2-01–02 | Scoped OAuth sandbox and redaction tests |
| F08 | Sync / Drive | P2-03, P2-06 | Resume/cancel/timeout/crash tests |
| F09 | Sync / tests | P2-04, P2-08 | Integrity mismatch and unchanged rerun |
| F10 | Sync / recovery | P2-05, P4-01–02 | Keep-both and verified recovery before updates |
| F11 | Sync safety | P4-03 | All deletion gates and manual review |
| F12 | Sync / security | P4-01 | Verified restore into new directory |
| F13 | Architecture / desktop | P3-01–02 | CLI/UI invoke same use cases |
| F14 | Desktop / configuration | P3-03 | Opt-in copy-only automation and policy-change pause |
| F15 | Configuration / security | P1-01, P1-07 | No secrets in exports; explicit schema migrations |
| F16 | Desktop / filter guards | P3-01 | Visible editable sensitive/build presets |
| F17 | Security / operations | P2-08 | Redacted diagnostics and privacy defaults |
| F18 | Architecture / provider | P0-03, P2-02 | Unsupported capabilities fail closed |
| F19 | Test strategy | P1-06, P2-06 | Fake provider fault injection |
| F20 | Research / backlog | P5+ | Separate scoped ADR and suite per extension |
| F21 | Desktop explorer | P1-09, P2-09 | File-browser navigation/status/search/list-grid and excluded visibility tests |
| F22 | Desktop / planner | P2-09, P3-02 | Local ↔ Cloud row-level preview matches immutable plan/explain |
| F23 | Architecture / filter policy sources | P1-02A, P1-03A | File/property adapter contracts, source snapshot/provenance tests |
| F24 | Tests / capability reporting | P1-03A onward | Per-profile pinned oracle/version report; unavailable required source blocks mutation |

## Safety invariants and scenario coverage

| Invariant | Seed scenarios / additional gate |
|---|---|
| S01 source protection | SAFE-027; all upload/restore workflows assert source read-only |
| S02 preview purity | SAFE-001 |
| S03 incomplete source protection | SAFE-002–003 |
| S04 filter fail-closed | SAFE-004, SAFE-026 |
| S05 exclusion protection | SAFE-006, SAFE-023 |
| S06 exact-plan authority | SAFE-010, SAFE-032 |
| S07 identity/security preconditions | SAFE-008–009, SAFE-030 |
| S08 destructive authorization | SAFE-020–022 |
| S09 verified completion | SAFE-011, SAFE-025 |
| S10 ambiguous result reconciliation | SAFE-012, SAFE-014 |
| S11 incomplete/permission state | SAFE-007, SAFE-028 |
| S12 conflicts/concurrency | SAFE-016–019, SAFE-031 |
| S13 durable journal | SAFE-013 |
| S14 honest cancellation | SAFE-015 |
| S15 recovery prerequisite | SAFE-024 |
| S16 policy change invalidation | SAFE-005, SAFE-029 |

The seed corpus is intentionally finite. Every discovered bug adds a regression test and updates the appropriate trace. Each released feature must have executed evidence, not merely a row in this table.
