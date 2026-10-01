# 20 — Desktop Explorer and Code-Policy Adapter Architecture

**Revision:** 0.2.0  
**Prepared:** 2026-10-01  
**Status:** normative product/implementation specification; no implementation is claimed.

## 1. Owner direction

Confirmar is no longer conceived as a command-centric tool with an optional graphical wrapper. The **desktop GUI is the primary product**. It should feel familiar to a Google Drive user: browse folders, switch list/grid, search, inspect status, see transfers, and navigate Local/Cloud content visually. The CLI remains important for automation, CI, scripting, remote/headless systems, and reproducible testing, but it calls the same use cases and does not define a separate product.

The second direction is to support the exclusion/selection conventions developers already use across source-control and code-management ecosystems, including SVN. This cannot be implemented safely as “read every file ending in `ignore` using Git syntax.” Formats differ in grammar, hierarchy, implicit behavior, and even **where rules are stored**.

## 2. Product UX: familiar, not copied

Use familiar file-manager/Drive interaction patterns without copying Google trademarks, proprietary icons, branding, or pixel-perfect layouts. Confirmar's own identity and safety model must remain obvious.

The primary window contains:

1. left navigation (`Files`, `Sync pairs`, `Policies`, `Activity`, `History & Recovery`, `Connections`, `Settings`);
2. breadcrumb/path toolbar with navigation/search;
3. central list/grid file browser;
4. optional details/policy inspector;
5. transfer/activity drawer or dedicated view;
6. a project switch to **paired Local ↔ Cloud** comparison.

The Files browser displays ordinary file metadata plus synchronization state. Suggested statuses include `Local only`, `Cloud only`, `Verified`, `Pending`, `Uploading`, `Excluded`, `Conflict`, `Error`, and `Unsupported`. These labels are semantic states, not merely colors.

## 3. Excluded-file visualization

`Show excluded files` is a core control, not a debug-only feature.

When off, excluded paths stay out of the ordinary file list but remain represented in policy totals. When on, excluded paths appear de-emphasized with an `Excluded` badge. Selecting one opens Explain with:

- final include/exclude/error result;
- adapter (`git`, `svn`, `mercurial`, `perforce`, etc.);
- source mechanism (`file`, `property`, `setting`);
- concrete source identity/path/property;
- dialect/profile and version;
- raw matching pattern or property entry;
- line number when the source has meaningful lines;
- directory/property scope and inheritance;
- ancestor blocker if any;
- group decision, composition winner, and fixed safety guards.

An excluded file is **not** a delete instruction. Remote content remains untouched unless a separately approved managed-mirror plan proposes an allowed deletion.

## 4. Paired Local ↔ Cloud explorer

Provide a comparison view that shows the local tree beside its authorized Drive namespace. The row relationship is backed by the immutable planner, not guessed by the frontend. Show operation direction and state (`→ copy`, `= verified`, `! conflict`, `× excluded`, `? unsupported`).

The paired view supports filtering to `Changes`, `Excluded`, `Conflicts`, `Errors`, or `All`; searching; sorting; and Explain. It should answer the user's practical question before execution: **what will happen to each file, and why?**

The UI must not imply bidirectional synchronization until that separately gated feature exists.

## 5. Policy-source model

Replace the assumption “rule source = file” with:

```text
PolicySourceDescriptor
  id
  adapterId
  sourceType          file | vcs-property | vcs-setting
  locator             basename/path/property/setting descriptor
  required
  scope
  dialect/profile
  expected capabilities

PolicySourceAdapter
  Detect(...)
  Resolve(...)
  Snapshot(...)
  CapabilityReport(...)
```

Adapters produce immutable policy material and provenance. Dialect parsers consume that material. This keeps **discovery/storage semantics** separate from **matching grammar**.

Adapters are compiled/allowlisted application code. A filename/config entry cannot load arbitrary executable plugins.

## 6. Compatibility catalog

### Required source-control family

| System | Policy source | Initial integration direction | Key semantic warning |
|---|---|---|---|
| Git | `.gitignore`; optional `.git/info/exclude` / global excludes as explicit profiles | Native parser; optional reference Git for tests | hierarchy, later-match precedence, blocked parent behavior, tracked-file distinction |
| Mercurial | root `.hgignore` plus include/subinclude | Native bounded file parser | syntax can switch between regexp/glob/rootglob |
| Subversion / SVN | `svn:ignore`, `svn:global-ignores`; optional runtime `global-ignores` | Read-only property adapter; initially fixed-argument `svn propget --xml` when client exists | `svn:ignore` is directory-local; `svn:global-ignores` is inheritable; no standard `.svnignore` |
| Perforce Helix Core | `.p4ignore`, `p4ignore.txt`, explicit P4IGNORE list | Native file parser/discovery | multiple files, nearest precedence, `*` vs `**`, negation |
| CVS | `.cvsignore`; optional repository/user/env sources | Native file parser | directory-local patterns, whitespace tokenization, `!` resets list, no Git comment semantics |
| Bazaar / Breezy | `.bzrignore`; optional global ignore | Native file parser | root-tree versus filename matching differs by slash presence |
| Fossil | `.fossil-settings/ignore-glob`; optional local/global `ignore-glob` | Native versioned-setting parser; metadata adapter later | Fossil glob-list semantics; `clean-glob` is a distinct destructive-cleaning concept |
| rclone | filter/include/exclude files | Reviewed/adapted rclone-compatible parser | first-match/reset/fallback behavior differs from Git |

The implementation agent must pin a compatible reference client or authoritative source corpus for each advertised profile and record the compatibility label. “Adapter exists” and “full compatibility verified” are separate states.

## 7. Code-tool policy profiles

The same architecture supports code-tool selection formats, but each gets its own profile and oracle. Priority examples:

- Docker `.dockerignore` and Dockerfile-specific `*.Dockerfile.dockerignore`;
- npm `.npmignore` and documented package-specific fallback/default behavior;
- Prettier `.prettierignore`;
- Helm `.helmignore`;
- other declarative ignore files only after their source/scope/grammar is documented.

Some tools now place ignore patterns inside general executable configuration. Confirmar must **not execute JavaScript, shell, Python, hooks, package scripts, or arbitrary project configuration** merely to extract ignores. Such formats require a safe declarative parser or remain unsupported.

## 8. SVN implementation contract

SVN is the most important reason to generalize the architecture.

Do not:

- invent `.svnignore` as a Subversion standard;
- recursively inspect undocumented `.svn` database schema as the product contract;
- run repository hooks;
- modify properties while discovering policy;
- silently ignore SVN policy because `svn` is missing.

Initial supported approach:

1. detect/configure an SVN adapter explicitly;
2. use a narrow `ProcessRunner` to call an allowlisted `svn` executable with a fixed argument array and explicit working-copy root;
3. use read-only property commands/XML output;
4. sanitize environment, disable interactive prompts, bound execution time/output, support cancellation;
5. snapshot property values plus directory/inheritance provenance into `RuleSnapshot`;
6. parse/evaluate them with SVN-specific semantics;
7. include SVN client/profile version in the plan digest/capability report.

If the required adapter cannot query policy, planning fails closed. Non-SVN projects do not require SVN to be installed.

## 9. Safe external process boundary

Optional VCS process adapters are **not general shell integration**. `ProcessRunner` accepts typed executable IDs registered by the application, fixed argument arrays, bounded cwd, sanitized environment, and resource limits. No `sh -c`, PowerShell command strings, repository scripts, hooks, aliases, or commands assembled from untrusted policy text.

The project should prefer native parsing for normal files. External clients are justified when semantics live in VCS-managed metadata/properties or when used as a test oracle.

## 10. Configuration/UI behavior

The policy editor shows each source as a card/row:

```text
Enabled  Source                Adapter     Profile              Status
✓        .gitignore            Git         gitignore            Verified
✓        .hgignore             Mercurial   hgignore             Verified
✓        svn:ignore            SVN         svn-ignore           Available
✓        svn:global-ignores    SVN         svn-global-ignores   Available
✓        .p4ignore             Perforce    p4ignore             Verified
```

The user can enable/disable sources, choose a custom filename where the adapter permits it, map arbitrary filenames to explicit profiles, change composition, and inspect a selection diff. “Auto-detected” never means “silently activated”; discovery can recommend a source, while project configuration records what is authoritative.

## 11. Implementation sequence

1. rclone source audit/reuse decision remains mandatory.
2. Build shared policy-source descriptors/registry and immutable snapshots.
3. Preserve existing Git/rclone parser work.
4. Add major VCS adapters and per-profile tests, with SVN property support as a first-class case.
5. Build the local GUI Files explorer against the fake/local provider and policy engine.
6. Add Show excluded + Explain parity.
7. Add paired preview backed by the planner.
8. Connect Drive listing and transfers only after the provider gates pass.
9. Add code-tool profiles incrementally with explicit compatibility evidence.

The GUI must not be a mock that reimplements selection logic in TypeScript. It consumes typed application-service DTOs/events from the Go core.

## 12. Definition of done for this direction

This direction is not complete because the window resembles Drive. It requires evidence that:

- the desktop is usable without terminal commands for normal setup/browse/preview/copy;
- CLI and GUI produce the same plan/explain result for the same snapshots;
- excluded items can be revealed with exact provenance;
- SVN property inheritance and scope are correct in isolated reference tests;
- every advertised adapter reports its profile/version and has conformance evidence;
- unavailable required policy sources block mutation;
- no adapter executes arbitrary repository content;
- the paired view is a faithful visualization of the immutable plan;
- no feature weakens the existing no-delete/no-source-mutation defaults.

## 13. References checked for this revision

Consulted 2026-10-01:

- Git ignore documentation: https://git-scm.com/docs/gitignore
- Mercurial ignore documentation: https://mercurial-scm.org/help/topics/hgignore
- Subversion ignore properties: https://svnbook.red-bean.com/en/1.8/svn.advanced.props.special.ignore.html
- Subversion propget: https://svnbook.red-bean.com/en/1.8/svn.ref.svn.c.propget.html
- Perforce P4IGNORE: https://help.perforce.com/helix-core/server-apps/cmdref/current/Content/CmdRef/P4IGNORE.html
- CVS cvsignore: https://www.gnu.org/software/trans-coord/manual/cvs/cvs.html
- Bazaar ignore documentation: https://documentation.help/bazaar-help/tutorial.html
- Fossil glob settings: https://fossil-scm.org/home/doc/trunk/www/globs.md
- Docker build context / `.dockerignore`: https://docs.docker.com/build/concepts/context/
- npm developer/package ignore docs: https://docs.npmjs.com/using-npm/developers.html
- Prettier ignore docs: https://prettier.io/docs/next/ignore/
- Helm `.helmignore`: https://helm.sh/docs/v3/chart_template_guide/helm_ignore_file/
