# 04 — Filter Engine Specification

**Normative owner of filtering behavior.** Do not replace this specification with a generic glob library or a translation of one format into another.

## 1. Vocabulary and separation

A **policy source** is a bounded source of exclusion/selection rules. It may be a normal file, a VCS property, or a versioned VCS setting. A **selector** locates file-backed policy sources. A **source adapter** reads the declared mechanism without mutation. A **dialect** parses the resulting policy material. A **group** combines compatible sources under one dialect's native ordering. A **composition policy** combines independent groups. A **safety guard** applies outside user-configurable filter precedence.

Names are arbitrary. `.gitignore` is the default discovered basename. `.ignore` has no magic until mapped to `gitignore`. `upload.rules` may be `rclone-filter`. The Google destination is unrelated to the filename. `rclone.conf` is not a dialect: reject it as an automatic filter import and warn that it may contain credentials.

Required adapter/dialect catalog is staged but architecture-complete. Core profiles include `gitignore`, `rclone-filter`, `rclone-include`, `rclone-exclude`, `hgignore`, `p4ignore`, `cvsignore`, `bzrignore`, `fossil-ignore-glob`, `svn-ignore`, and `svn-global-ignores`. Code-tool profiles such as `dockerignore`, `npmignore`, `prettierignore`, and `helmignore` use separate adapters/profiles even where they resemble Gitignore. Never imply compatibility based on a filename alone. A release only advertises profiles whose conformance suite has passed.

## 2. Inputs and outputs

Each candidate path is relative to the selected local source root. Use `/` as the logical separator, preserve original path component bytes where the platform supports them, and do not silently lowercase or Unicode-normalize filenames. Initial portable mode accepts valid UTF-8 names; invalid encodings or unrepresentable names cause a surfaced unsupported-path error before mutation.

Reject absolute paths, NUL, `..` components, root escapes, alternate-drive prefixes, and unsupported separator interpretations at the path boundary. A backslash in a rule belongs to that dialect's escaping syntax; do not blindly convert it to `/`.

Group evaluation returns one of:

- `ALLOW`: an explicit positive decision, including a valid negation/re-inclusion.
- `DENY`: a matching exclusion or a dialect-defined implicit exclusion.
- `PASS`: no decisive rule in this group.
- `ERROR`: invalid/unsupported source or evaluation failure.

Also return provenance, ancestor-blocking state, and conservative descendant reachability. The final evaluator returns include/exclude/error plus all relevant group decisions. A no-match `PASS` is not an overriding allow.

## 3. Configuration and deterministic order

See the examples and schema. Each group has a unique ID and unique integer priority, a dialect, enabled flag, scope, and an ordered array of sources. Higher priority is evaluated first by ordered composition. Duplicate priorities are a configuration error rather than an invisible tie-break.

A `recursive-basename` selector names exactly one basename to discover at each visited directory under the root. A `root-file` selector names one validated path relative to the root. A `recursive-vcs-property` source is not a file selector: it asks an allowlisted VCS adapter for a named property at applicable directories. The first compatibility wave permits recursive basenames where the upstream format is actually hierarchical (Gitignore, Perforce P4IGNORE with relative discovery, CVS `.cvsignore`, npm package subdirectories where that profile is enabled). Mercurial `.hgignore`, Bazaar `.bzrignore`, Dockerignore, Prettierignore, Helmignore, Fossil versioned settings, and rclone files use their documented root/include behavior instead of invented recursive discovery. SVN uses property sources. rclone sources use explicit root-relative files; hierarchical rclone merge files are a future product extension, not asserted upstream behavior.

For a required `root-file`, that exact file must exist. For a required metadata/property source, the adapter capability must be available and the source must resolve according to its declared scope; inability to query it is a blocking source error, not an empty rule set. A required `recursive-basename` means at least one reachable occurrence must exist in the selected root tree, not that every directory must contain it. Report zero occurrences after discovery as a source error. An actual rule file resolved twice within one group is a configuration/discovery error; do not silently change its precedence. The same file may be explicitly referenced by separate groups, which remain independent.

For rclone sources, patterns are relative to the project source root, regardless of the folder containing the rule file. For Gitignore sources, patterns are relative to the rule file’s directory.

For Gitignore groups, ancestor directory order is shallow to deep. Within one directory, source-selector order is the order in configuration, and line order is preserved. Later applicable rules within that group have precedence. Configuring `.gitignore` then `.ignore` in the same group means `.ignore` can override at the same directory level. That additional-filename ordering is a documented product extension.

For rclone groups, source-file array order defines one combined stream. Apply the dialect's parsing/ordering to that stream. A reset in the second rclone-filter file clears rules accumulated from the first file **inside that group**. It cannot clear Gitignore groups or safety guards.

The plan digest includes resolved source ordering, content hashes, parser versions, all case/scope settings, and composition mode. OS directory enumeration order must not affect results.

## 4. Gitignore profile

Reference: G01/G02 in the source register. Preserve the supported Gitignore grammar and its precedence, escaping, anchoring, directory-only patterns, character classes, wildcard behavior, and negation. A parser that only handles `*` is not sufficient.

Important compatibility conditions: patterns are relative to the rule file's directory; later applicable rules override earlier ones; an excluded parent blocks ordinary descendant re-inclusion. A file's tracked status is a separate Git concern. [G01]

The initial product mode is `patterns-only`: evaluate ignore patterns for **all** candidate regular files, including tracked ones. This is useful for transfer selection but differs from Git's treatment of already tracked files. The UI/help must say so. A future `respect-index` mode requires repository discovery, index semantics, worktrees/submodules, and separate tests; it is not silently enabled.

In the initial portable profile, matching is case-sensitive. Explicit `caseSensitive=false` is a documented alternate profile and must have tests; filesystem case folding must not silently change a profile. Reference-oracle tests pin `core.ignoreCase=false` and disable machine-global ignore configuration.

Only configured rule files are read. Do not automatically import a user's `core.excludesFile`, `.git/info/exclude`, parent-repository rules above the selected root, or nested repository configuration. These are possible explicit adapters later. The initial source root is the upper rule boundary even when it is a subdirectory of a larger Git checkout.

### Ancestor exclusion

Evaluate whether a directory is traversable within a Gitignore group using applicable ancestor rules. If the group excludes `build/`, the rule `!build/keep.txt` does not by itself reopen that parent. A nested ignore file inside a directory blocked by that group must not become active for that group's decision.

The physical scanner may nevertheless enter that directory because another higher-priority group could permit descendants under ordered composition. That does not magically make the blocked Gitignore group's nested files authoritative. Track group traversal state separately from physical traversal.

### Parser edge cases

Fixture coverage must include comments, escaped initial `#` and `!`, escaped trailing spaces, CRLF, blank lines, leading/root slashes, trailing directory slashes, slashless basenames, `*`, `?`, classes, all supported `**` placements, dangling escape behavior, and invalid encodings. Do not silently trim Gitignore lines with a generic whitespace trim.

Rules may be read even when the rule file itself is not selected for upload. The selection decision for `.gitignore` as a data file is separate from its role as configuration. Do not upload a rule file solely because it was read as configuration.

## 5. rclone profiles

Reference: R07 and inspected source R15. rclone filter-file ordering differs from Gitignore. A matching filter rule is decisive at its first occurrence; a standalone `!` clears accumulated filter rules; include-file and filter-file fallback are distinct. [R07, R15]

For `rclone-filter`, concatenate configured source streams in order. Parse `+ pattern`, `- pattern`, reset `!`, documented comments, and documented whitespace processing. A positive rule alone does not create an implicit deny-all. If no rule matches, return `PASS`, allowing the composition layer's default include unless another group denies.

For `rclone-exclude`, parse every non-comment pattern as exclusion. No match returns `PASS`. For `rclone-include`, compile the union of include patterns plus one final implicit deny-all within the group. Thus an unmatched candidate returns `DENY`. Keep include and exclude profiles distinct rather than mixing flag categories with undocumented ordering.

Support the documented glob constructs, anchors, alternatives, classes, escaping, and RE2-compatible embedded regular-expression regions before claiming the full declared profile. Use Go's non-backtracking regexp engine for the relevant compiled representation. Do not accept arbitrary PCRE/backtracking syntax. Any unimplemented construct must fail validation with a precise capability error, not be interpreted as a literal that changes selection.

Directory rules and inferred traversal behavior require independent tests. A path filter is not automatically a directory filter. Avoid unsafe pruning when regular expressions or alternatives obscure descendant reachability. Separate semantic inclusion from traversal optimization; compare both normal and non-pruned test walks.

Bare wildcard pattern files belong to include/exclude modes, not filter mode. rclone `--files-from` variants are literal manifest selectors with separate interaction rules; do not parse them as this dialect. Metadata/hash filters, marker exclusions, and command-line option import are separately versioned capabilities.

A filename extension does not prove it is a supported rclone rules file. Import requires an explicit profile and a parse-only preview. Do not read saved remotes/tokens from rclone connection configuration as part of this operation.

## 6. VCS and code-management policy profiles

These profiles are distinct even when they share wildcard concepts. Implement common parser primitives only behind dialect-specific conformance tests; never alias formats merely because their examples look similar.

### Mercurial (`hgignore`)

The canonical project source is root `.hgignore`. Preserve Mercurial's `syntax: regexp`, `syntax: glob`, and `syntax: rootglob` switching plus documented `include:` / `subinclude:` behavior. Included policy files are resolved with root-boundary and explicit-external-policy controls; a path outside the selected project is not read implicitly. Reference behavior against a pinned `hg` version before advertising compatibility.

### Subversion (`svn-ignore`, `svn-global-ignores`)

Subversion ignore policy is stored primarily as directory properties, not a standard `.svnignore` file. `svn:ignore` applies to unversioned names in the directory on which the property is set; `svn:global-ignores` is inheritable and applies beneath its directory. Runtime `global-ignores` is user/machine configuration and is opt-in only.

Do not parse `.svn/wc.db` directly as a stable contract. The initial property adapter may invoke an installed `svn` binary with fixed, read-only argument arrays such as `propget --xml` / `--show-inherited-props`, bounded output, sanitized environment, timeout, and no shell. If the adapter is configured and the capability is unavailable, block mutation with `CAPABILITY_UNSUPPORTED` / `RULE_SOURCE_UNAVAILABLE`. A future native Subversion binding may replace the process adapter without changing the policy-source contract.

### Perforce Helix Core (`p4ignore`)

Support `.p4ignore`, `p4ignore.txt`, and an explicitly configured P4IGNORE filename list according to the pinned client profile. Preserve closest-file precedence, `!` re-inclusion semantics, `*` versus `**`, and recursive discovery rules. Do not import server credentials, tickets, or arbitrary P4CONFIG values while reading ignore policy.

### CVS (`cvsignore`)

Support project `.cvsignore` files with their directory-local scope, whitespace-separated patterns, inherited/default list behavior only when the selected profile explicitly requests it, and `!` list reset semantics. CVS has no comment syntax in `.cvsignore`; a parser must not copy Git's comment behavior.

### Bazaar/Breezy (`bzrignore`)

Support root `.bzrignore` glob behavior and optionally the user-global ignore file only with explicit opt-in. A slash-bearing pattern is evaluated against the path from the tree root; otherwise it is filename-oriented. If Breezy compatibility diverges from historical Bazaar, version the profile and oracle separately.

### Fossil (`fossil-ignore-glob`)

Support the versioned `.fossil-settings/ignore-glob` source and the documented Fossil glob list syntax. Local/global Fossil settings are opt-in metadata sources. Do not treat `clean-glob` as equivalent to ignore policy: it has destructive cleanup meaning and requires a separate, explicitly named profile if ever exposed.

### Code-tool adapters

Docker `.dockerignore` / Dockerfile-specific `.dockerignore`, npm `.npmignore`, Prettier `.prettierignore`, Helm `.helmignore`, and similar code-tool policies belong to separate profiles. Examples: Docker has its own preprocessing, `**`, exception ordering, and Dockerfile-specific precedence; npm also has package-specific defaults/fallback behavior; Prettier documents Gitignore-style syntax plus its own default exclusions; Helm differs from Gitignore. Implement an adapter only after defining its exact transfer-policy interpretation and reference tests. Executable configurations such as JavaScript config files must never be run merely to extract ignores.

### Non-policy repository metadata

`.gitattributes`, `.git/config`, hooks, credentials, `.svn` database internals, package scripts, CI workflows, and repository metadata are not automatically filter sources. They may be visible in the file browser but are interpreted only by a separately specified feature. The policy engine must not execute repository content.

## 7. Cross-group composition

### Default: `conservative`

Evaluate every applicable enabled group against the candidate. Any `ERROR` blocks the plan. Any `DENY` excludes the candidate. Otherwise include it, whether remaining groups return `ALLOW` or `PASS`.

This is an intersection of group permissions. A Gitignore negation resolves exclusions **within its own group** but does not erase a denial from another group. Priority is recorded/displayed but does not override deny-wins in this mode. UI must not imply that dragging groups changes conservative results.

Example: Gitignore allows `release.zip` while an independent rclone group excludes `*.zip`; the result is excluded. Put two interacting Gitignore filenames in the same group when native-style override behavior is desired, or explicitly switch to ordered composition.

### Advanced: `ordered`

Validate all sources first. Sort groups by descending unique priority. Use the first `ALLOW` or `DENY`; skip `PASS`. If every group passes, include. Still collect lower-priority decisions for explanation, but they do not override the result. Any source error remains blocking even if an earlier group would decide the path.

This permits an explicit high-priority include to override a lower-priority exclusion. It can increase uploads. Switching modes, adding groups, changing priorities, or enabling a broad positive rule requires a reviewed selection diff and invalidates existing approvals.

### Fixed outer guards

Apply root/path security, unsupported-node policy, application-private credential/state protection, and explicit locked safety exclusions outside group composition. An ordered include cannot override them. User-editable preset exclusions are different from these fixed guards and must be visibly represented in configuration/preview.

Do not silently label every `.env` as a fixed system object. A sensitive-file preset may exclude `.env`/private keys with an explicit user-visible policy; truly necessary application credential/state files must remain outside the source root and protected regardless.

## 8. Traversal algorithm

Perform a non-following directory walk. At each directory, obtain the rule files allowed by active group contexts, validate them, and create a new immutable inherited context. Evaluate child directory traversal separately from child file inclusion.

Under conservative composition, a directory can be pruned when at least one group proves all descendants are denied under that group's semantics and no required source-validation obligation is skipped. Under ordered composition, prune only when no higher-priority group can possibly allow any descendant. If that cannot be proved, traverse. “Unknown” means inspect, not drop.

Validate all explicit root-file sources before walking. Recursive sources inside logically unreachable directories need not be opened merely to search for hidden configuration; report the branch as pruned by the relevant rule. If a physically discovered configured source is unreadable, invalid, or exceeds a declared parser limit, block mutation for the run.

A previously observed rule file disappearing changes the snapshot and requires review. Missing explicitly required sources are errors from the first run. A never-observed optional recursive `.gitignore` is allowed to be absent: a non-Git folder must still work. Disabling a source is an explicit configuration change, not an accidental missing-file fallback.

## 9. Explain contract

For a candidate, output its normalized relative path, node kind, fixed guards, every applicable group decision, active source file and line, raw pattern, parsed action, rule scope, ancestor blocker when applicable, winning composition rule, and final decision. Include implicit decisions such as rclone-include deny-all and final default include.

An excluded parent explanation must identify the parent rule; do not invent a matching child rule. Track overwritten or reset rules as diagnostics where useful. An explanation is deterministic for a given snapshot and must match what the planner/executor uses.

CLI JSON may include redacted source identifiers; human output should show local relative paths. Avoid leaking an absolute home path in exported reports by default.

## 10. Limits and failure behavior

Initial configurable limits: 1 MiB per rule file, 16 KiB per logical rule line, 100,000 compiled rules per project, and a bounded directory depth of 512 in the portable profile. These are product limits, not claims about upstream limits. Exceeding a limit causes a clear error and no mutation; truncation is forbidden. Parser time/resource budgets and cancellation must be tested.

Treat symlink rule files as unsupported in the first release. Do not follow them outside the root. All rule files are configuration data, never scripts, templates, includes fetched from the network, or agent instructions.

## 11. Acceptance and compatibility labels

Run Git reference fixtures in patterns-only mode using an isolated repository and `git check-ignore --no-index --stdin -z`. Run rclone reference fixtures with read-only local listings and a pinned version. Add pinned/read-only or authoritative fixture oracles for Mercurial, SVN, Perforce, CVS, Bazaar/Breezy, and Fossil before marking each profile compatible. SVN tests must cover property scope and inheritance, not a fabricated ignore file. Compare selected sets, traversal edge cases, and malformed/unavailable-source behavior. Mixed cases validate this specification directly.

A release compatibility label includes dialect, grammar/profile version, case mode, supported selectors, Git tracked policy, reference versions, and documented deviations. Do not label arbitrary multi-source composition “identical to Git” or “identical to rclone.”
