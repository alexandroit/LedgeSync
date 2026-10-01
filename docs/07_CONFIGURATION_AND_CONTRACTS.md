# 07 — Configuration, CLI Contracts, and Serialization

## Version and validation

`schemas/project.schema.json` and `schemas/plan.schema.json` define the version-1 JSON envelopes. They are strict about unknown properties to catch misspelled safety controls. JSON Schema checks shape; the Go loader must also enforce cross-field semantics, duplicate IDs/priorities, path security, provider capabilities, and runtime authorization.

Examples are non-secret specification examples, not configured access to a real account. `SOURCE_ROOT`, `ACCOUNT_REFERENCE`, and `DESTINATION_FOLDER_ID` must be replaced through onboarding; they are deliberately not working credentials. JSON configuration is the initial canonical format. YAML/TOML/import adapters are future work and cannot introduce implicit execution or templating.

## Project envelope

| Field | Meaning |
|---|---|
| `schemaVersion` | Exact major/minor schema identifier, current specification `1.1` |
| `project` | Stable project ID and editable display name |
| `source` | Root, case profile, and explicit symlink rejection |
| `destination` | Provider, opaque account reference, root folder ID, namespace mode |
| `filters` | Composition, patterns-only Git mode, default decision, fail policy, ordered groups |
| `sync` | Mode, conflict/overwrite/verification policies, worker/retry and deletion limits |
| `automation` | Disabled by default; trigger and interval |

Configuration stores references, never refresh/access tokens, passwords, resumable URLs, or service-account private keys. State and credentials paths are resolved by the backend outside the source. Do not accept `rclone.conf` as project configuration.

Every group declares `id`, unique integer `priority`, `enabled`, `dialect`, `scope: "project"`, and an ordered `sources` array. Each source declares `type`, `value`, and `required`. `recursive-basename` discovers a basename only for profiles whose upstream semantics support hierarchical discovery. `root-file` refers to an explicitly listed path relative to the source root. `recursive-vcs-property` names a read-only metadata property such as `svn:ignore` or `svn:global-ignores`; its adapter/capability is part of the immutable rule snapshot. Cross-group mixing happens through composition, not by feeding all policy material into one parser.

`caseSensitive: false` is a separately tested alternate profile, not inferred from the host filesystem. `gitMode` is initially fixed at `patterns-only`. `defaultDecision: include` applies only after group composition and fixed guards. `failOnError: true` cannot be disabled in version 1.

## Semantic validation

Reject duplicate project/group identifiers where uniqueness is required, duplicate priorities, absolute file-backed rule-source paths, traversal, NUL, separators in a basename, zero-length names, invalid source-type/dialect combinations, symlink roots/rule sources outside the supported profile, and destinations nested in unsafe local source configurations. Metadata/property sources use an allowlist of identifiers rather than path validation. Resolve real filesystem identity at runtime; string-prefix tests are insufficient.

A disabled automation configuration must use `trigger: manual` and `intervalSeconds: 0`. An enabled interval schedule uses a positive bounded interval and is allowed only for copy with `copy-preauthorized` approval and non-destructive operation classes. File-watch trigger is also copy-only; its interval is a reconciliation/debounce control, not a guarantee of continuous real-time mirroring. Schema-valid automation does not imply user consent has been recorded.

`interactive` is the default approval policy. A preview exists for every job, including preauthorized copy: unattended execution is restricted to an explicit durable policy, requires unchanged rules/config/root identity, and writes a report. New exclusions, includes, detected conflicts, required-source loss, root changes, or update/delete requests pause for review.

For `mirror`, automation must be disabled and approval interactive. Both delete caps must be explicitly positive before any trash operation can be proposed for approval; zero remains a valid configuration meaning deletion is blocked. `overwritePolicy: recover-managed` is an unavailable capability until its milestone gates pass, even though the versioned schema can describe it.

## Proposed CLI

These commands describe the product to implement; the executable is not included in this package.

```text
confirmar config validate --config project.json
confirmar explain --config project.json --path src/main.go --json
confirmar plan --config project.json --output plan.json
confirmar plan inspect --plan plan.json
confirmar apply --plan plan.json --approve PLAN_SHA256
confirmar status --project PROJECT_ID --json
confirmar runs show RUN_ID --json
confirmar cancel --run RUN_ID
confirmar restore --recovery RECOVERY_ID --to NEW_EMPTY_DIRECTORY
```

`explain` is offline when the source can be inspected locally. `plan` may read an authorized provider but never mutates it. `apply` checks exact digest, expiry/start rules, authorization, versioned capabilities, and operation preconditions. A digest flag is not a secret or an independent security token; a managed scheduler also needs its separately stored permission policy.

There is no v1 `purge`, `delete-excluded`, `--no-safety`, arbitrary provider-flag passthrough, shell hook, or command to import plaintext remote credentials. CLI parsing never constructs a shell command from a path or rule pattern.

## Plan envelope

The plan records `schemaVersion`, IDs, creation/expiry times, mode, source/destination identity, config/rule/inventory digests, completeness, operation array, risks, and summary. `planId` and each `operationId` are opaque identifiers. The serialized plan is not sufficient without corresponding local trusted state and current checks.

Operations have an explicit type, validated logical relative path, known object IDs where applicable, dependencies, preconditions, source/destination fingerprints, and a human explanation. Unknown operation kinds fail closed. `skip` and `conflict` are visible decisions, not mutations. For a directory create, dependent operations refer to its operation ID; resolve the created provider ID from the journal, never invent an ID during preview.

Canonical digest calculation: omit only the top-level `planDigest`, canonicalize JSON with the pinned, tested canonical encoding chosen in P0/P1, and hash UTF-8 bytes with SHA-256. Version that encoding. The documentation example's digest is computed using the package's explicitly labeled canonical JSON profile (sorted object keys, no insignificant whitespace, UTF-8, finite integers); the product must either preserve that profile or migrate plans explicitly. Paths/strings are not silently Unicode-normalized. Approval covers operation ordering/dependencies and every risk/limit; no mutable unchecked extension object is allowed.

`sourceDigest` in the serialized envelope is a SHA-256 source fingerprint. Provider verification records must separately include their hash algorithm and value; never compare a Drive MD5 value directly with SHA-256. The adapter may compute the provider-compatible digest while streaming, or use read-back SHA-256 when the selected assurance requires it. Hash-based transfer integrity is not a claim of adversarial content authenticity.

The supplied plan schema is an envelope, not the complete provider-specific executable journal schema. Define and test the internal typed journal during P1/P2; never interpret free-form explanation text as instructions.

## Stable errors and exit codes

| Domain code | Meaning / action |
|---|---|
| `CONFIG_INVALID` | Reject before discovery; identify field |
| `RULE_PARSE_ERROR` / `RULE_SOURCE_UNAVAILABLE` | Block; identify group/source/line safely |
| `PATH_UNSAFE` / `NODE_UNSUPPORTED` | No traversal/mutation; report candidate |
| `SOURCE_UNAVAILABLE` / `SCAN_INCOMPLETE` | Block plan/apply; no deletion inference |
| `AUTH_REQUIRED` / `SCOPE_INSUFFICIENT` | Explicit reconnect/consent workflow |
| `DESTINATION_CHANGED` / `AMBIGUOUS_DESTINATION` | Reconcile and review |
| `PLAN_STALE` / `APPROVAL_REQUIRED` | Produce/review a new plan |
| `CONFLICT` | Preserve both sides; review or keep-both |
| `CAPABILITY_UNSUPPORTED` | No unsafe fallback |
| `RATE_LIMITED` / `TRANSIENT_PROVIDER_ERROR` | Bounded retry |
| `INTEGRITY_FAILED` | No success baseline; preserve recovery |
| `UNKNOWN_REMOTE_RESULT` | Read-only reconciliation before retry |
| `CANCELLED` / `PARTIAL_FAILURE` | Report completed and unresolved operations |

Proposed CLI exits: 0 successful requested action; 2 invalid config/input; 3 authorization/permission required; 4 review/conflict/stale plan; 5 transient provider failure after retries; 6 integrity/safety failure; 7 partial result; 130 cancellation. A dry-run plan containing blocked operations exits 4 or 6, not 0 with a hidden warning.

## Preset extension boundary

Version 1.1 examples include both file-backed sources and SVN property sources. The P3 preset editor must either add an explicitly approved rule file through a separate edit workflow or introduce a reviewed schema revision for declarative inline preset rules. It may not secretly append patterns to `.gitignore` or keep undocumented hidden exclusions. Show Git-internal/build-cache/sensitive-file presets during setup; `.gitignore` alone does not imply that `.git/` is excluded. The owner can select and review those policies.

## Events and migrations

Desktop events contain `schemaVersion`, `runId`, monotonic `sequence`, UTC timestamp, event type, and a typed redacted payload. The frontend can re-read authoritative state after missed events. Progress is acknowledged bytes and verified counts; estimates are marked as estimates.

Migrations validate old input, produce a backup, migrate deterministically, show safety-relevant diffs, and invalidate old approvals. Unknown future major versions are read-only/unsupported. Never rename user files or silently convert one filter dialect into another during migration.
