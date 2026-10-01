# 18 — Documentation Package Validation Report

**Prepared:** 2026-10-01. **Package:** Confirmar specification 0.2.0.

## Actual validation scope

This report concerns the documentation, JSON contracts and fixture expectations. It is not an application test, a cloud synchronization test, a security certification, or proof of complete rclone/Git compatibility.

| Check | Result / evidence |
|---|---|
| UTF-8 Markdown, balanced code fences and local document links | Checked by `tools/validate_docs.py`; final validation must have zero failures |
| JSON syntax | All packaged JSON parsed by the validator |
| JSON Schema definitions | Two Draft 2020-12 schemas checked using installed `jsonschema` |
| Configuration/plan examples | Four project examples and one plan example validated; cross-field/selector checks applied |
| Example plan digest | Recomputed SHA-256 using the documented canonical JSON profile |
| Fixture structure | 50 filter fixtures and 32 safety scenarios checked for unique IDs and consistent referenced candidate paths |
| Git reference expectations | **28 of 28 passed** against `git version 2.47.3` in isolated temporary repositories |
| Manifest | SHA-256 and byte counts checked for every indexed file; the manifest excludes itself |
| ZIP integrity | Archive CRC/content consistency checked when the delivery archive was created |

The actual Git case-by-case output is in `tests/git-reference-results.json`. The runner disables machine-global/system Git configuration, uses temporary directories, never contacts Drive, and never accesses a user repository. Its `--no-index` profile checks the intended patterns-only behavior.

## Review corrections before handoff

The review aligned advanced size/age/marker filters and bandwidth shaping with the later backlog rather than implying unsupported first-release configuration. It clarified recursive-required semantics, duplicate resolved sources, and Git-versus-rclone rule-file scope. It corrected the proposed embedded-rclone-regex example to match a complete path segment, distinguished SHA-256 plan/source fingerprints from provider-specific checksums, and documented the future schema/edit boundary for visible presets.

These are specification reviews, not executed product tests. During assembly, the local-link checker correctly detected the not-yet-created validation report; the final package includes it and must pass a fresh run.

## Checks explicitly not performed

No Confirmar executable exists. The 12 rclone fixture cases and 10 mixed-composition cases remain proposed application/reference tests. The 32 safety scenarios remain release-gate specifications. The local rclone executable was unavailable and no rclone differential run was claimed. No native Go/desktop build, full grammar proof, live OAuth test, API write test, full rclone repository audit, performance benchmark, signing, or public release was performed.

The shared ChatGPT link did not expose the complete conversation transcript. Research used official documentation, two inspected rclone source ranges, the current request, and recovered relevant conversation context. Sources and blob hashes are recorded separately.

## Reproduction

```sh
python tools/validate_docs.py
python tools/check_git_reference.py
```

The validator reports a missing optional `jsonschema` dependency as skipped, not passed. Install it only in an approved isolated development environment when necessary. Git must be installed to execute its reference runner. Neither command executes a production synchronization operation.

To generate a new Git result file deliberately, use the runner's `--output` option, then regenerate the package manifest if distributing a changed package. Routine validation without `--output` does not rewrite the archived evidence. The manifest is an integrity inventory, not a digital signature or authority to trust executable code.


## Specification 0.1.1 historical follow-up validation — 2026-10-01

The new source-download/audit/implementation assignment was added and the bootstrap, shared agent rules, entry points, reuse policy, ADRs, backlog, and handoff were updated consistently. The existing technical contracts and fixture evidence were preserved.

Actual local checks performed on the updated documentation package:

- `python tools/validate_docs.py`: 24 Markdown files, 38 local links, 10 JSON files, 2 schemas, and 4 configuration/plan examples checked; 50 filter fixtures and 32 safety scenarios checked structurally; 38 manifest entries verified (excluding the manifest itself). Result: **0 failures, 0 skipped checks**.
- `python tools/check_git_reference.py`: **28/28 reference cases passed**, Git **2.47.3**, using temporary fixture repositories. The original archived result JSON was not rewritten.

These checks validate documentation, sample contracts, integrity, and Git reference expectations, not the future application. The new P0-00A/B/C source-clone/audit/reuse tasks remain **NOT STARTED**. No Codex or Claude Code process was launched, no upstream rclone source was cloned or built, no rclone differential test was run, and no Confirmar code was generated in this documentation update. Official repository/license/filter pages were consulted on 2026-10-01 to ground the assignment.


## Specification 0.2.0 follow-up — GUI-first and VCS policy adapters

This revision changes product direction and contracts: the desktop Drive-style Files explorer is now the primary UX; policy sources are generalized beyond ordinary files; SVN `svn:ignore` / `svn:global-ignores` properties are explicit first-class sources; and major VCS/code-management profiles have a staged adapter/test plan. These are specification changes, not claims of implemented compatibility.

Actual local checks performed for specification 0.2.0:

- `python tools/validate_docs.py`: **25 Markdown files**, **40 local links**, **11 JSON files**, **2 schemas**, and **5 configuration/plan examples** checked; **50 filter fixtures** and **32 safety scenarios** checked structurally; **40 manifest entries** verified (excluding the manifest itself). Result: **0 failures, 0 skipped checks**.
- `python tools/check_git_reference.py`: **28/28 Git reference cases passed** against Git **2.47.3** in isolated temporary repositories.

The new VCS/code-policy adapters and desktop explorer remain specifications, not implemented features. No SVN working-copy command, Perforce command, remote Drive write, OAuth credential, or production synchronization was executed during this documentation revision. Compatibility for each new adapter must be proven during implementation using the versioned reference strategy in documents 10 and 20.
