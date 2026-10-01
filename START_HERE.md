# Start Here

This repository now contains the first offline implementation in addition to the original specification. Read [the current status](docs/17_AGENT_HANDOFF_AND_STATUS.md) and [platform guide](docs/PLATFORMS.md) before using it. Commands and features elsewhere in the original specification remain proposals unless listed as implemented in the status report.

## Reading order for the implementation agent

Read `AGENTS.md`, product requirements, architecture, filter specification, sync safety, Google Drive provider specification, and `docs/20_DESKTOP_EXPLORER_AND_CODE_POLICY_ADAPTERS.md` before writing production code. Then read configuration contracts, test strategy, backlog, and decision records. Load the remaining documents when working on their subsystem.

Use `tests/filter-conformance.json` to seed tests, not as proof that the future engine already works. Run the documentation validators first. The safe initial deliverable is an offline deterministic policy/filter/plan core with a fake provider **plus a functional local desktop Files explorer**. The CLI remains useful as a deterministic verification/automation surface; no network writes are required for this first slice.

## Mandatory upstream source review

First follow [the source-analysis and implementation assignment](docs/19_RCLONE_SOURCE_ANALYSIS_AND_IMPLEMENTATION.md). Clone the real rclone repository outside this project, pin a suitable stable tag and full SHA, review source and tests, record a reuse matrix and ADR, then implement working Confirmar code. Source adaptation and in-process reuse are allowed after review; requiring an external rclone installation remains prohibited. A documentation summary alone does not satisfy this gate.

## First development session

Inspect the checkout and preserve all existing files. Record the selected stable Go, Wails, Node, dependency, Git, and rclone reference versions. Do not install arbitrary versions of tools into the user's global environment. Use an isolated development setup where possible.

Complete P0-00A through P0-00C, then implement the dependency-ready P0/P1 tasks including the policy-source adapter registry, major VCS profiles, and P1-09 GUI explorer as small tested changes. Add a real `go.mod`, a shared domain package, configuration loading, rule parsing, an explain command, and reference tests. Establish tests before connecting OAuth or uploading anything.

The agent may continue with safe, reversible local implementation work without repeatedly asking about decisions already specified. It must stop at genuine authorization boundaries: accessing the user's real credentials, writing to a real Drive destination, enabling automatic jobs, adopting existing files, approving destructive operations, or publishing a release.

## Minimal product success

A user selects a local source and an authorized Drive destination, sees which files will be copied and why, approves a plan, and gets a verifiable report. Multiple custom policy sources work simultaneously, including nested Gitignore rules, rclone-format filter files, and supported VCS adapters such as SVN properties. A second unchanged run makes no unnecessary transfers. A failing ignore file stops the job instead of uploading everything.

## Do not start with

Do not start with a new brand, website, payment system, model API integration, cloud-hosted account service, virtual filesystem mount, or bidirectional sync. Do not wrap `rclone sync` with a translated `.gitignore` file and call that the architecture. Do not copy the PixelJS stack or requirements into this unrelated project.

## Package validation

From the repository root:

```sh
python tools/validate_docs.py
python tools/check_git_reference.py
```

The first script checks local documentation links, parses JSON, validates configuration and plan examples when `jsonschema` is installed, and checks the manifest. The second uses Git only in temporary directories. It does not inspect or mutate any user repository, contact Google Drive, or test a Confirmar executable.

A missing optional dependency must be reported as a skipped check, not as a pass. Product CI commands will be added during P0; this package does not pretend they already exist.
