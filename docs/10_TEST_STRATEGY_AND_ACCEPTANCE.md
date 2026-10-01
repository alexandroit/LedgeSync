# 10 — Test Strategy and Acceptance Gates

## Distinguish evidence from plans

The application is not implemented. This package supplies 28 Git reference cases, 12 proposed rclone cases, 10 composition cases, and 32 safety scenarios. The included Git runner checks fixture expectations against an installed Git in temporary repositories. It does not execute a LedgeSync filter engine. The validation report is the authority on checks actually run.

A release needs all relevant layers below; passing a finite example corpus is not proof of complete compatibility for Git, rclone, SVN, Mercurial, Perforce, CVS, Bazaar/Breezy, Fossil, or code-tool profiles.

## Test layers

| Layer | Required evidence |
|---|---|
| Strict configuration | JSON shape, semantic constraints, unknown keys, invalid/missing sources, unsupported modes |
| Dialect unit tests | Grammar, escapes, anchoring, ordering, resets, comments, directory semantics, diagnostics |
| Differential tests | Identical selected sets against pinned/read-only reference tools or authoritative fixtures for every advertised policy profile |
| Composition tests | Conservative/ordered truth tables, priorities, PASS vs ALLOW, global error blocking |
| Traversal tests | Hierarchical activation, parent barriers, multi-group re-entry, pruning equivalence |
| Property/fuzz tests | No panics, bounded resources, determinism, path safety, parser rejection |
| Planner tests | Pure deterministic operations, complete-scan gates, unchanged detection, ownership |
| Executor tests | Journaling, interruption, retries, source changes, ambiguous remote results, integrity |
| Provider contract tests | Capability honesty, pagination, identity, error mapping, hash/restore behavior |
| Authorized live tests | Scoped OAuth and disposable Drive namespace only after consent |
| Desktop/CLI tests | GUI-first file explorer/paired view plus same core use cases, Show excluded/Explain consistency, keyboard accessibility, cancellation, stable CLI JSON/exit codes |
| Release tests | Signed packaging, clean install/uninstall, secure vault, local migrations, upgrade rollback |

## Git reference adapter

Use the included isolated runner as the seed. Disable global/system Git configuration, set an empty excludes file, set `core.ignoreCase=false`, and use `git check-ignore --no-index --stdin -z`. Output reflects the patterns-only profile. A future product test harness must compare its actual decisions to the oracle's excluded set, not merely rerun the oracle with its own expected strings.

Cover all supported `**` placements, escaped punctuation/whitespace, CRLF, missing final newline, nested rules, blocked parents, explicit reopen, directory-only rules, case profiles, non-Git roots, tracked-file distinction, Unicode, and unreadable/oversized sources. Add generated trees and negative syntax/resource cases. Git's accepted syntax and behavior may differ from a convenient third-party glob library; failing cases require fixing or explicitly narrowing the declared profile.

Custom filenames are our configuration feature. Reference testing must emulate each group's native hierarchy in an isolated fixture or compare the canonical combined hierarchy, while separately testing composition. Never alter a user's actual `.gitignore` merely to run reference tests.

## rclone reference adapter

P0 pins an exact rclone version and records its binary checksum/source release. Tests use read-only local listing commands, a dedicated temporary directory, and explicitly selected `--filter-from`, `--include-from`, or `--exclude-from`. Do not use sync/purge for reference testing. Capture both file selection and directory traversal behavior; validate malformed-input outcomes separately.

`tests/filter-conformance.json` contains proposed initial rclone fixtures; they have not been executed in this package. Expand them to directory inference, multiple streams, standalone resets, leading anchors, whitespace/comments, brace alternatives, embedded RE2, class escapes, and no-match fallback. Normalize only a documented fixture-relative path representation, not semantics. Document any profile deviations rather than hiding them by editing expected results to match the product.

## VCS and code-policy reference adapters

Every advertised adapter gets a versioned oracle strategy. Prefer a pinned read-only client command in an isolated temporary working copy when the upstream semantics are metadata-dependent; otherwise use authoritative source fixtures plus upstream command comparison when available.

- **Mercurial:** verify `.hgignore` regexp/glob/rootglob, include/subinclude scope, and debug-ignore explanations against a pinned `hg` profile.
- **SVN:** create isolated working copies/repositories, set `svn:ignore` and `svn:global-ignores` properties, query them read-only, and compare status/add candidate behavior. Test property inheritance and immediate-child scope. Never fabricate `.svnignore` cases.
- **Perforce:** compare P4IGNORE source ordering and `p4 ignores -v` where a disposable local test setup/client is available; otherwise mark the oracle skipped, not passed.
- **CVS:** validate `.cvsignore` per-directory scope, whitespace tokenization, default/inherited list choices, and `!` reset.
- **Bazaar/Breezy:** validate `.bzrignore` root/path behavior against the selected maintained client/profile.
- **Fossil:** validate `ignore-glob` versioned settings and glob syntax against a pinned Fossil client.
- **Code tools:** Docker/npm/Prettier/Helm profiles each receive separate fixtures because similar filenames do not imply Git semantics.

Optional VCS executables run with fixed argument arrays, sanitized environment, bounded cwd/output/time, and temporary test roots. Tests must prove the product never executes repository hooks/scripts merely to evaluate an ignore policy.

## Composition and traversal properties

Required properties: repeated evaluation of a snapshot is deterministic; order of filesystem enumeration does not change semantics; in conservative mode any group denial excludes; in ordered mode only the first decisive group wins; a lower-priority error still blocks; no-match PASS does not erase another denial; fixed guards cannot be overridden; group reset affects that group only.

Compare a deliberately non-pruning traversal implementation with the optimized walker on generated trees. Every selected file and explanation must agree. Include a Gitignore-blocked branch that must be physically visited for a different high-priority group without activating the blocked group's hidden nested rules.

## Failure injection

Use the fake provider with controllable clocks/randomness and injected failures before request, after remote commit but before response, after acknowledgement but before local commit, during each pagination page, during verification, and during recovery. Restart the executor at each journal checkpoint. Confirm no hidden deletes, duplicated retry uploads, false verified success, or baseline advancement for unresolved operations.

Execute every SAFE scenario before its required milestone. Add revoked credentials, insufficient scope, exhausted storage, duplicate names, missing volumes, symlink swaps, API timeouts, repeated 429/5xx, token expiry, expired sessions, case/Unicode collisions, invalid config migrations, watcher overflow, and unsupported native documents/shortcuts.

## Nonfunctional targets — not measured claims

Initial engineering targets: bounded memory while planning 100,000 files, aiming below 512 MiB RSS on a documented reference machine; bounded transfer workers/buffers; cancellable scanning; responsive UI; no accidental quadratic rule-by-path trace storage. Record hardware, OS/filesystem, file count/distribution, rule count, cold/warm cache, network conditions, and selected provider before reporting performance.

Do not claim rclone performance superiority without a like-for-like benchmark with pinned versions and matching semantics. Optimization cannot relax verification or delete gates. Test thousands of rules, deep directory trees, many tiny files, large binary files, and slow/error-prone networks separately.

## Release acceptance journeys

**Offline alpha:** strict config plus explain/select output is deterministic, the initial declared policy profiles pass their reference tests, custom basenames/property sources/hierarchy/composition work, malicious/invalid inputs fail closed, and the local GUI explorer can reveal excluded files with the same explanations as the core/CLI.

**Drive copy alpha:** an explicitly authorized disposable namespace receives only approved selected regular files; rerun skips verified unchanged entries; ignored remote files remain; cancellation/restart/retry are truthful; no local source mutation occurs.

**Desktop beta:** the primary product is usable through a Drive-style Files explorer, paired Local ↔ Cloud preview, policy manager, explain/progress/history, opt-in non-destructive automation, accessible controls, secure connection management, and redacted diagnostics.

**Managed mirror gate:** complete scans and stable policy, explicit managed ownership, zero-default caps, verified recovery and restore, manual plan approval, no recursive protected-folder deletion, and documented provider concurrency limitations. Any unmet predicate keeps mirror disabled.
