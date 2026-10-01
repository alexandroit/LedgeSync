# Third-party notices

Original Confirmar code is licensed under Apache-2.0. Third-party code retains its own copyright and license. Include this file and the applicable `third_party/` license texts in source and binary distributions.

## Adapted rclone filtering code

- Project: [rclone](https://github.com/rclone/rclone).
- Version: v1.75.1, commit `687d264b689b8c49a67e2e52a8a5e0caa01c04ce`.
- Origin: [`fs/filter/glob.go`](https://github.com/rclone/rclone/blob/687d264b689b8c49a67e2e52a8a5e0caa01c04ce/fs/filter/glob.go), glob-to-regexp conversion and directory-glob inference.
- Copyright: Copyright (C) 2012 by Nick Craig-Wood.
- License: MIT; full unchanged text in [third_party/rclone.LICENSE](third_party/rclone.LICENSE).
- Local adaptation: [internal/filters/rclone_glob.go](internal/filters/rclone_glob.go) retains the upstream converter functions, changes the package to `filters`, and removes the `fs` import and informational directory-inference log. Source/group/provenance handling is implemented separately. The adapted source includes its upstream identity. The original upstream source SHA-256 is in [UPSTREAM_BASELINE.json](docs/research/UPSTREAM_BASELINE.json).

Confirmar does not package or invoke an rclone executable. The selectively adapted compiler is not a claim that every rclone command or filtering capability is implemented. The complete original rclone repository and its unrelated dependencies are not redistributed by this extraction. See the [reuse decision](docs/research/RCLONE_REUSE_MATRIX.md).

## Direct framework and build-tool inventory

This inventory was checked on 2026-10-01 against local module/package metadata and the official license sources. It records the versions selected for this milestone. Lockfiles and build manifests determine the actual dependency graph.

| Component | Selected version | Usage | License and retained source |
|---|---|---|---|
| Go toolchain / standard library | 1.27.0 | Builds Go code; standard-library portions are linked into binaries | BSD-3-Clause; [third_party/go.LICENSE](third_party/go.LICENSE), copied from the installed toolchain |
| Wails | 2.14.0 | Desktop shell and Go/frontend bridge | MIT; [third_party/wails.LICENSE](third_party/wails.LICENSE), from the pinned module's [LICENSE](https://github.com/wailsapp/wails/blob/v2.14.0/LICENSE) |
| TypeScript | 7.0.2 | Frontend build-time compiler | Apache-2.0; official [LICENSE.txt](https://github.com/microsoft/TypeScript/blob/v7.0.2/LICENSE.txt); npm metadata verified |
| Vite | 8.3.2 | Frontend build-time bundler | MIT; official [LICENSE](https://github.com/vitejs/vite/blob/v8.3.2/LICENSE); npm metadata verified |

Wails contains additional components with their own notices and has transitive dependencies. The table above is a direct dependency inventory, not a completed license audit of every transitive dependency or a binary bill of materials. Preserve generated/bundled dependency notices and include all applicable transitive licenses when assembling redistributable desktop artifacts. Build-time packages need their notices if the tools/packages themselves are redistributed.

The isolated upstream audit additionally used testify v1.11.1 (MIT) and golang.org/x/time v0.15.0 (BSD-3-Clause), among upstream test dependencies. Their checked license hashes are recorded in the source baseline; those modules are not introduced into the Confirmar core by the audit.

## Updating adapted code

Retain the source commit, source-file hash and license text when changing adapted portions. Record meaningful local deviations and rerun upstream-reference and Confirmar regression tests before accepting a new source baseline. New reused files, substantial excerpts or runtime dependencies require an entry here and their applicable notices.
