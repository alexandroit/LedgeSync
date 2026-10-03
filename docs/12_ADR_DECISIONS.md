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

**Accepted for specification, 2026-10-01; implementation pending.** The owner selected a graphical experience similar in familiarity to Google Drive instead of making a command interface the main product. The desktop Files explorer, policy inspector, paired Local ↔ Cloud preview, activity, and history are first-class deliverables. The CLI remains supported for automation/headless use and must call the same application services. “Drive-style” means familiar information architecture/interactions, not copying Google branding/assets or hiding LedgeSync's explicit policy/safety model.

## ADR-019 — Generalize rule files into policy-source adapters

**Accepted for specification, 2026-10-01; implementation pending.** Selection policy can come from files, VCS properties, or versioned settings. Introduce an allowlisted `PolicySourceAdapter` registry and keep source mechanism separate from dialect grammar. Required built-in compatibility targets include Git, Mercurial, SVN, Perforce, CVS, Bazaar/Breezy, Fossil, and rclone; code-tool formats use the same extension model. This supersedes wording that implied every source is a normal file.

## ADR-020 — SVN properties through supported read-only integration, not `.svn` database parsing

**Accepted for specification, 2026-10-01; implementation pending.** SVN uses `svn:ignore` and `svn:global-ignores` directory properties. The first adapter may use an installed `svn` client through a narrow fixed-argument, read-only `ProcessRunner` and XML output; a later native binding may replace it. Do not treat `.svn/wc.db` as a public stable API and do not invent `.svnignore`. Missing required capability fails closed. The app remains usable for non-SVN projects without an SVN installation.

## ADR-021 — Selective rclone parser adaptation; independent safe offline core

**Accepted for implementation, 2026-10-01.** The actual official rclone v1.75.1 tree at `687d264b689b8c49a67e2e52a8a5e0caa01c04ce` was cloned and reviewed before application implementation. See [source audit](research/RCLONE_SOURCE_AUDIT.md), [baseline](research/UPSTREAM_BASELINE.json) and [reuse matrix](research/RCLONE_REUSE_MATRIX.md).

The concrete problem is that upstream command/RPC/sync integration imports configuration/global state and performs live traversal-driven mutations, whereas LedgeSync requires immutable policy snapshots and approved object-level plans. Upstream Drive listing also logs `incompleteSearch` without converting it into a completeness error, and name lookup can select the first duplicate. These are contract differences, not a claim of an upstream security vulnerability.

Choose a standard-library offline core and selectively adapt `fs/filter/glob.go` conversion/directory-inference code, preserving the upstream MIT copyright/permission notice and full source pin. Keep source discovery, Git/rclone dialects, group composition, explanations and planning independent. Do not import the whole rclone Go module, fork/rename the whole product, use librclone RPC, or invoke an installed rclone during production. A pinned development oracle is permitted. The source/test review established reuse boundaries; local upstream `fs/filter` and `lib/pacer` tests passed in isolated configuration with no cloud credentials.

Tradeoffs: extraction avoids unused backend/dependency/global state but makes LedgeSync responsible for tracking upstream parser fixes and carrying differential/regression tests. Keep source hashes and a list of local changes; review newer stable releases deliberately. Do not claim full rclone compatibility from the parser alone. Native Drive upload/retry mechanisms remain candidates for later selective adaptation after fake-server, identity, journal, cancellation and ambiguous-response contracts are tested. This is a scoped decision, not a full upstream dependency or security audit.

No persistent-state migration exists yet. The first slice contains strict configuration, multi-source policy evaluation, explain, deterministic read-only preview, a fake provider and a local desktop explorer using the same core. Missing/invalid/unsupported policy and incomplete scans fail closed. Initial preview has no deletion/cloud-write capability. Application licensing is resolved by the owner's current Apache-2.0 request; MIT notices remain required for adapted portions. No account connection or cloud-write authorization is inferred from the source audit.

## ADR-022 — Owner-authorized open source and platform expansion

**Accepted, 2026-10-01.** The owner explicitly requested starting the project, a
public GitHub project and website, Apache licensing, and versions for Ubuntu,
macOS ARM and AMD/Intel, Windows 11 and server systems. Original LedgeSync code
is licensed under Apache-2.0; third-party notices and licenses remain intact.
This resolves the license part of ADR-014. The owner selected LedgeSync as the project name.

Target ARM64 and x64 for macOS, Ubuntu desktop/server, and Windows. Headless
servers use the same Go core through the CLI; there is no automatically installed
service or scheduler. Native desktop builds use Wails. CI build evidence and
installation/runtime acceptance are reported separately in PLATFORMS.md.

The first distributed version is an explicitly labeled offline alpha: local
exploration, policy explanations and fake-destination previews. Real Drive
OAuth, mutations, scheduling, mirror and restore remain gated by their milestone
requirements. Public source/site authorization does not grant access to private
Google Drive accounts or authorize synchronization.

## ADR-023 — Project identity and authoritative Drive review

**Accepted, 2026-10-01.** The owner explicitly fixed the project and GitHub name
to **LedgeSync**. This supersedes every temporary-name instruction in the
original specification. Use `LedgeSync` for the product/repository and `ledgesync`
for the command and package identifiers. The GitHub repository was renamed to
`alexandroit/LedgeSync`; the previous website deployment was withdrawn before
republishing under the correct identity. Existing history is preserved.

The implementation agent reread the four root entry documents and all 20
technical documents through the Google Drive connector in the owner's folder
named LedgeSync. Their SHA-256 hashes match the original documentation manifest.
They retain stale temporary-name wording, which the owner's explicit correction
overrides. The technical Go/Wails, GUI-first, policy and safety contracts remain
in force. No requirements from the unrelated PixelJS folder were adopted.

## ADR-024 — Canonical specification 0.2.1 reread

**Accepted, 2026-10-01.** Read PROJECT_IDENTITY.md first. The updated Drive
specification fixes the identity to LedgeSync, command `ledgesync`, primary
domain `ledgesync.com`, and requires familiar current-Google-Drive navigation,
breadcrumbs, search, list/grid, details, statuses, activity and paired comparison
without copying Google's branding or assets. All six root Markdown documents
and all twenty technical documents were fetched and compared with the prior
read. Filtering, mutation safety, native Drive boundaries and the source-analysis
gate are unchanged. Existing source audit evidence remains valid.

The `.com` domain is the requested canonical destination. Temporary GitHub Pages
and private preview URLs do not establish domain ownership, DNS setup or a
working custom domain; those require separate observed hosting evidence.

The owner initially requested preparation of the DNS records only. At that
stage, GitHub Pages was the provisional public site. The later instruction
recorded in ADR-025 supersedes that hosting decision.

## ADR-025 — Canonical website on the existing Ubuntu origin

**Accepted and deployed, 2026-10-01 America/Toronto.** The owner explicitly
requested publishing ledgesync.com on the Ubuntu server hosting HiperMusicas,
confirmed that DNS already points there, and instructed use of existing access.
The static website now has its own Nginx vhost, immutable release directory and
current symlink. Existing shared vhosts and application services were preserved.
GitHub Pages remains a secondary copy; the Sites deployment remains a private
preview. Neither service is the requested production origin.

The existing Cloudflare routing was retained. Let's Encrypt issuance used the
existing server-side DNS authenticator, with transient validation TXT records
and automatic renewal. Public and origin HTTPS, redirects, matching HTML/CSS
hashes and continued HiperMusicas availability were verified. See
[deployment evidence and rollback](WEBSITE.md). This website deployment does
not change the offline alpha's product capabilities or release artifacts.

## ADR-026 — Approval binds selected content; changed approved files are skipped

**Accepted, 2026-10-02.** An upload approval is bound to the selected entries'
paths, kinds, sizes and SHA-256 values, the rule snapshot and the configuration.
Excluded entries and directory timestamps are not part of it, so activity in
ignored files no longer invalidates a run. Before each mutation the executor
checks the source root, known rule files and new rule files in traversed folders;
changed rules or configuration, a different account, or a changed destination
stop the run before further mutation. An approved file whose content changed is
not uploaded: it is reported as a per-file `SOURCE_CHANGED` issue and the run ends
`partial`. Files added after the preview are not part of the approved work.
Unapproved content is never uploaded and approved work is never extended. See
[failure analysis](research/DRIVE_SYNC_FAILURE_ANALYSIS.md).

## ADR-027 — Policy-aware traversal; links are recorded, never followed

**Accepted, 2026-10-02; refines ADR-011.** Under conservative composition, a
directory that an enabled Gitignore group excludes is recorded but not read,
because no descendant can be selected. Rule files that can affect a directory are
read before its children are considered; ancestors of configured root-file
sources are never pruned; a disagreement with the complete policy fails closed.
Symbolic links and special nodes remain unsupported for copying: they are never
followed, opened or copied, but they no longer abort a scan. Selected ones are
listed as `Not copied`. Rule-source baselines are versioned by traversal profile
(`ledgesync-traversal-v2`).

## ADR-028 — My Drive under the drive.file scope

**Accepted, 2026-10-02.** `drive.file` cannot read My Drive's root metadata, but
the `root` alias is a valid parent. Choosing My Drive uses the alias; creation
under it is confirmed with a parent query restricted to the object's operation
marker, and the canonical root ID is then recorded for later verification. The
scope is unchanged.

## ADR-029 — Replacement generations for earlier copies missing or changed in Drive

**Accepted, 2026-10-02.** A recorded object that is missing, trashed, renamed or
moved is never modified, moved back, adopted or deleted. The preview shows the
item as **Copy again** (`recreate`); after approval a new object is created under
a new operation identity generation, and for a folder its whole subtree is copied
again under the new folder. Generation 0 preserves alpha identities.

## ADR-030 — Saved sync pairs and opt-in automatic copies

**Accepted, 2026-10-02.** Approved runs save their source, destination and
policy as a sync pair in a private catalog separate from the transfer journal.
Reopening never restores an approval. Automatic copies are off by default and
require an explicit authorization bound to a reviewed preview: account,
destination, source identity, configuration digest, rules digest and conflict
policy. Automatic runs perform only create-only work (new files, folders,
keep-both versions and reconciliation of reserved copies). Any change to the
bound inputs, or earlier copies missing in Drive, pauses the job for review; an
offline network, a busy transfer or an unmounted source only delays it. The
desktop checks authorized pairs only while it is open; `ledgesync automatic run`
and `automatic watch` are explicit entry points for servers. Packages never
install a service, login item, timer or scheduled task.

## ADR-031 — Restore to a new location; managed overwrite and mirror stay disabled

**Accepted for this release, 2026-10-02; owner confirmation of the remaining
scope is pending.** Restore downloads only journal-recorded, verified objects of a
pair's managed copy into a new, empty folder outside the source and LedgeSync's
private data. Each file must match Drive's MD5 and the journal's SHA-256; an
existing file is never overwritten. Managed overwrite (`recover-managed`),
managed mirror and every deletion remain disabled: changed files keep both
versions. Enabling them requires the P4 recovery, concurrency and deletion gates.

## ADR-032 — Publisher signing for macOS and Windows; signed APT metadata

**Accepted, 2026-10-02.** Release builds for macOS are signed with a Developer
ID Application identity (hardened runtime, secure timestamp, no entitlement
exceptions), notarized and stapled (app and DMG; CLI binaries are notarized but a
bare Mach-O cannot be stapled). Windows executables, setups and uninstallers are
Authenticode-signed with an RFC 3161 timestamp through Azure Artifact Signing or
a hardware-protected certificate. Ubuntu packages are distributed through the
GPG-signed APT repository (ADR-022). Builds without signing material are labeled
`unsigned-developer-build`; they are never presented as signed releases, and no
self-signed certificate is used. See [platform signing](PLATFORMS.md#publisher-signing).

## ADR-033 — Provider-issued upload session URIs and detected media types

**Accepted, 2026-10-02.** The client uses a resumable session URI only when all
of the following hold:
- scheme, host and path are exactly `https://www.googleapis.com/upload/drive/v3/files`;
- it has `uploadType=resumable` and exactly one `upload_id`;
- every parameter is a single plain key with one value;
- echoed initiation parameters equal the values that were sent;
- no credential parameter is present (`access_token`, `oauth_token`, `refresh_token`, `key`).

Other parameters that Google adds to this opaque URI, such as `session_crd`, are
accepted. Pinning the origin and path is what keeps the bearer token on
Google's upload endpoint.

An uploaded file is verified by ID, parent, name, operation marker, size and
MD5. Its stored media type may be any regular type that Drive detects, but never
a folder or a Google-native type.

**Why:** the first live acceptance showed that the stricter rules rejected every
real upload ([live findings](research/DRIVE_SYNC_FAILURE_ANALYSIS.md#live-acceptance-findings-alpha6)).
**Consequence:** the Drive emulator models both behaviors so CI covers them.
Provider-facing checks need live evidence, not only the emulator.

## ADR-034 — Two-way sync like Google Drive for desktop; full Drive access

**Accepted, 2026-10-02 (owner decision).** The owner asked that LedgeSync
behave like Google Drive for desktop: once a folder is chosen it starts sending
and receiving changes. The owner chose full Drive access over the per-file
scope, and chose to propagate deletions with trash recovery.

- **Scope.** The app requests `https://www.googleapis.com/auth/drive`.
  `drive.file` cannot list files that the Drive website or other apps add to a
  synced folder, so those could never be received. Access is used only for
  folders the user syncs or copies and for the location browser. This
  supersedes ADR-028's reliance on `drive.file`. Existing connections must
  reconnect, and the owner's Google Cloud consent screen must list the scope.
  Public distribution requires Google's restricted-scope verification.
- **Model.** Each pass is a three-way comparison of the local folder, the Drive
  folder and the last state both agreed on. Additions, edits and deletions
  propagate both ways. An edit wins over a deletion. When both sides changed a
  file, the Drive version keeps the name and the local version is kept as
  `name (conflict <time>).ext` and uploaded.
- **Safety.**
  - Drive edits are stored as new revisions of the same file.
  - Deletions go to the Drive trash, or locally to `.ledgesync-trash` inside
    the synced folder for 30 days.
  - A pass that would delete at least 20 files and more than 30% of the synced
    files, or that empties one side, waits for an explicit confirmation of
    that exact deletion set or a restore.
  - An unavailable local folder or a trashed Drive folder never causes
    deletions.
  - Creations reserve their Drive ID first.
  - Local writes go through `os.Root`, use temporary files verified by MD5,
    and check the target first.
- **Scope of what syncs.** Ignore rules apply in both directions. Links, special
  files, Google-native files, duplicate Drive names and names the local system
  cannot store are reported and never synced. Sync runs while LedgeSync (or
  `ledgesync sync watch`) runs; nothing is installed.
- **Supersedes.** ADR-031's disabled overwrite and mirror, and the read-only
  source rule (local writes now happen inside the synced folder only). One-time
  approved copies (ADR-026, ADR-030) remain available unchanged.

**Consequence:** the emulator models full access, revisions, trash, the change
feed and files added outside the app. Live acceptance must include a file added
through another app.

## ADR-035 — Grants that include full Drive access; in-app destination for copies

**Accepted, 2026-10-03.** A Windows connection failed with "The authorization
callback was invalid" after Google's consent. The callback and the token
response accepted only the exact scope string. Any other report from Google
looked like a malformed callback, for example the earlier `drive.file` grant
listed next to full access, basic profile scopes granted to another client of
the same Cloud project, or a permission left unchecked.

- **Accepted grant.** Full Drive access must be granted. Two kinds of scope may
  appear beside it: `drive.file`, which full access contains, and Google's
  basic profile scopes (`openid`, `email`, `profile`, `userinfo.email`,
  `userinfo.profile`). Any other or repeated scope rejects the grant.
- **Errors.**

  | Code | Cause | What the message tells the user |
  |---|---|---|
  | `AUTH_SCOPE_NOT_GRANTED` | The grant lacks full Drive access. | How to allow it. |
  | `AUTH_SCOPE_UNEXPECTED` | The grant includes scopes that were not requested. | Remove the app's access at Google, then connect again. |
  | `DRIVE_FOLDER_NOT_SELECTED` | A browser folder selection returned no folder. | Choose a folder in Google's window. |
  | `AUTH_CALLBACK_INVALID` | The callback is malformed. | Which check failed, for example "(repeated parameter)", then connect again. |

  The callback page still shows none of the returned values, and neither do the
  messages.
- **Copies destination.** With full access, the desktop chooses an existing
  Drive folder for one-time copies in the same in-app browser as sync. It
  validates the folder again before use. Choosing a folder no longer needs a
  second browser consent or Google's Picker. The CLI keeps `--destination` and
  the optional browser Picker.

## ADR-036 — Microsoft Store distribution as an unsigned MSIX bundle

**Accepted, 2026-10-03 (owner decision).** The owner asked for the cheapest
way, preferably free, to distribute LedgeSync on Windows. Building an EXE, MSI
or MSIX installer costs nothing; signing is what costs money, and an unsigned
EXE or MSI gets the same SmartScreen warning. Individual developer accounts on
the Microsoft Store are free, and the Store signs the MSIX packages it
publishes.

- **Package.** [package_msix.py](../tools/package_msix.py) packages the
  released and verified x64 and ARM64 desktop payloads unchanged, as two MSIX
  packages in one `.msixbundle`. The app runs as a full-trust desktop app
  (`runFullTrust`). Logos are rasterized from the app's vector mark at the
  sizes Windows uses.
- **Identity and signing.** The package identity comes from Partner Center
  (`deploy/msix/identity.json`). The bundle is unsigned, and the Store signs it
  on publication. No self-signed or test certificate is used. Without the
  Store identity, the pipeline still runs and produces a bundle marked not for
  upload.
- **Version.** The Store requires the first version part to be at least 1 and
  reserves the fourth. `MAJOR.MINOR.PATCH-alpha.N` maps to
  `(MAJOR+1).MINOR.(PATCH×1000+N).0`; beta adds 300, rc adds 600, and a final
  release uses 999. For example, 0.1.0-alpha.8 is 1.1.8.0.
- **Local state.** Windows keeps a packaged app's AppData separate, so the
  Store app and the setup EXE do not share local state. Install only one of
  them.
- **Distribution.** The setup EXE stays available on the website and on GitHub.

