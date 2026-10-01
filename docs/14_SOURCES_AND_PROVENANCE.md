# 14 — Sources and Provenance

**Research date:** 2026-10-01. External product/API facts can change; pin actual reference/dependency versions during P0. This register separates observed upstream behavior from original proposed Confirmar requirements.

## User provenance and limitations

The current request explicitly asks for a new `Confirmar` directory under the existing projects folder, an rclone analysis, configurable multiple ignore-rule sources including Gitignore and rclone formats, and agent-ready documentation. Recovered prior conversation context supplies the standalone local-to-Google-Drive application direction, custom basenames and multi-application conventions.

A user-supplied shared conversation page was opened (its personal reference URL is omitted from the public repository). Its public response exposed the conversation title “Ignorar Arquivos no Drive” and a login/share shell, not the complete transcript. No claim is made that the full linked conversation was read. Technology, security defaults, milestone boundaries and mixed-format precedence are proposed decisions documented here, not fabricated prior user quotations.

The destination was verified through the connected Google Drive: `Projects/Confirmar`. Existing sibling projects were not changed. Project folder ID: `15gvWXQ7OcQoFPy_QFXpfyV5H1fzKIRut`; parent Projects ID: `1W91nOMECbeYKXPCE9-OsxWtlyULK09p3`. No public sharing was enabled by this task.

## rclone primary sources

| ID | Official source | Evidence used |
|---|---|---|
| R01 | https://rclone.org/docs/ | Command/option families and operational capabilities |
| R02 | https://rclone.org/commands/rclone_copy/ | Non-deleting copy behavior and previews |
| R03 | https://rclone.org/commands/rclone_sync/ | Destination deletion implications and exclusions |
| R04 | https://rclone.org/commands/rclone_check/ | Verification capability |
| R05 | https://rclone.org/overview/ | Backend/capability diversity |
| R06 | https://rclone.org/drive/ | Google Drive adapter context |
| R07 | https://rclone.org/filtering/ | Native pattern/filter ordering and grammar |
| R08 | https://rclone.org/bisync/ | Separate bidirectional synchronization capability |
| R09 | https://rclone.org/crypt/ | Encryption backend capability |
| R10 | https://rclone.org/commands/rclone_mount/ | Mount/VFS capability |
| R11 | https://rclone.org/commands/rclone_serve/ | Serving capability |
| R12 | https://rclone.org/rc/ | Remote-control interface capability |
| R13 | https://rclone.org/gui/ | Existing GUI capability; do not claim rclone has none |
| R14 | https://github.com/rclone/rclone/blob/master/fs/filter/filter.go | Options/filter construction, inspected lines 1–260 |
| R15 | https://github.com/rclone/rclone/blob/master/fs/filter/rules.go | Rule matching/loading/reset, inspected lines 1–260 |
| R16 | https://rclone.org/licence/ | MIT licensing and notice requirement context |

R14 observed Git blob SHA: `e00d0e8b3e481ff297f9fcb63379b5b6392ed55a`.
R15 observed Git blob SHA: `e5ee2626e778ba01d9aea9d92153c00f4d96be78`.
These are individual blob identities, **not** a pinned repository commit or release. The reviewed ranges are not the full codebase. No whole-repository security audit, compilation, benchmark, or rclone executable test was performed for this package. The research does not establish a forever-current “latest version.”

The gap assessment is limited to the reviewed options, rule-loading code and documentation. It did not identify the proposed combined native Gitignore hierarchy/custom-source orchestration there. This is not a claim that no plugin, downstream wrapper, new commit or external project implements some of these ideas.

## Git primary sources

| ID | Official source | Evidence used |
|---|---|---|
| G01 | https://git-scm.com/docs/gitignore | Hierarchy, matching/negation and tracked-file distinction |
| G02 | https://git-scm.com/docs/git-check-ignore | Isolated reference-check command behavior |

The locally available Git version and actual fixture results are recorded in `tests/git-reference-results.json`. That version is a local test reference, not a recommendation that it is the newest release.

## Source-control and code-policy primary sources

The following sources were checked on **2026-10-01** to ground the expanded policy-adapter specification. They are reference documentation, not evidence that Confirmar already implements the profiles.

| ID | Official/source documentation | Evidence used |
|---|---|---|
| V01 | https://git-scm.com/docs/gitignore | Git hierarchy, precedence, `.git/info/exclude`, global excludes |
| V02 | https://mercurial-scm.org/help/topics/hgignore | `.hgignore`, regexp/glob/rootglob and include/subinclude semantics |
| V03 | https://svnbook.red-bean.com/en/1.8/svn.advanced.props.special.ignore.html | `svn:ignore`, `svn:global-ignores`, runtime global ignores, scope/inheritance |
| V04 | https://svnbook.red-bean.com/en/1.8/svn.ref.svn.c.propget.html | Read-only XML property query approach and inherited-property inspection |
| V05 | https://help.perforce.com/helix-core/server-apps/cmdref/current/Content/CmdRef/P4IGNORE.html | P4IGNORE files/list, recursive discovery, precedence and wildcard semantics |
| V06 | https://www.gnu.org/software/trans-coord/manual/cvs/cvs.html | `.cvsignore`, per-directory scope, reset behavior and global/user sources |
| V07 | https://documentation.help/bazaar-help/tutorial.html | `.bzrignore`, root/path glob behavior and global ignore file |
| V08 | https://fossil-scm.org/home/doc/trunk/www/globs.md | Fossil `ignore-glob`/versioned `.fossil-settings` semantics |
| T01 | https://docs.docker.com/build/concepts/context/ | `.dockerignore`, Dockerfile-specific ignore files, matching/exception ordering |
| T02 | https://docs.npmjs.com/using-npm/developers.html | `.npmignore`, Gitignore-like patterns, subdirectory behavior and package defaults |
| T03 | https://prettier.io/docs/next/ignore/ | `.prettierignore` and documented Gitignore syntax relationship |
| T04 | https://helm.sh/docs/v3/chart_template_guide/helm_ignore_file/ | `.helmignore` behavior and documented differences from Gitignore |

SVN deserves special treatment: the authoritative ignore policy is property-based, so a file-only architecture would be incorrect. Perforce, CVS, Mercurial, Bazaar, and Fossil also have distinct source/discovery/grammar rules. Similar-looking wildcard files must remain separate compatibility profiles.

## Google primary sources

| ID | Official source | Evidence used |
|---|---|---|
| D01 | https://developers.google.com/workspace/drive/api/guides/api-specific-auth | OAuth scopes and least-privilege capability boundaries |
| D02 | https://developers.google.com/identity/protocols/oauth2/native-app | Installed-application OAuth workflow |
| D03 | https://developers.google.com/workspace/drive/api/guides/manage-uploads | Resumable upload protocol and recovery constraints |
| D04 | https://developers.google.com/workspace/drive/api/guides/handle-errors | Structured error reasons and retry classification |
| D05 | https://developers.google.com/workspace/drive/api/guides/manage-changes | Paginated change tracking |
| D06 | https://developers.google.com/workspace/drive/api/reference/rest/v3/files | Object identity, metadata, types, hashes and method constraints |
| D07 | https://developers.google.com/workspace/drive/api/guides/folder | Folder/parent operations |
| D08 | https://developers.google.com/workspace/drive/api/guides/limits | Quota/usage considerations |

Scope coverage, checksum availability, provider preconditions, upload idempotency and account-specific limits still require live capability tests. Do not infer a complete production integration from reading documentation.

## Framework and agent entry points

| ID | Official source | Evidence used |
|---|---|---|
| A01 | https://v2.wails.io/docs/introduction/ | Go/web architecture and desktop framework selection context |
| A02 | https://developers.openai.com/codex/guides/agents-md | Codex project instruction entry-point convention; redirected official documentation was reviewed |
| A03 | https://agents.md | AGENTS.md instruction-file convention |

AGENTS.md and the bootstrap prompt use those conventional entry points. They do not disable permission prompts, grant real-account authorization, or promise identical behavior across agent versions.

## Original specification versus upstream facts

Conservative/ordered multi-group composition, priority uniqueness, fixed guards, safety caps, staged ownership, schema fields, proposed CLI, module boundaries, fixtures and backlog are original product specifications. They must not be attributed as existing rclone or Git features. No upstream source code has been copied into this documentation package. Future code reuse requires its own provenance and license review.
