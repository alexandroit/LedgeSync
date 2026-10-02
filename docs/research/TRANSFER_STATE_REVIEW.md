# Transfer journal dependency review

Date: 2026-10-01. Scope: the pinned SQLite driver and new runtime notice graph
selected for the create-only transfer journal. This is a bounded source/license
review, not a complete security audit of SQLite, libc, or their generated code.

## Dependency decision

Use `modernc.org/sqlite v1.60.1` through Go's `database/sql` for local operation
intents and verification records. The existing specification requires durable
state around remote mutations. A transactional embedded database supports this
without a separate database service or a platform-specific C compiler for CLI
builds. No OAuth credential, access token, refresh token, resumable session URL or
file content belongs in this database.

The selected driver is a pure-Go translation of SQLite and has a larger dependency
and generated-code footprint than a hand-written JSON journal. That tradeoff buys
existing transactional and recovery mechanisms; it does not make filesystem
durability, safe migrations or the application's state transitions automatic.
Driver and libc versions must be updated together: the driver's own
[`doc.go`](https://gitlab.com/cznic/sqlite/-/blob/v1.60.1/doc.go) explicitly requires
the matching libc version in its `go.mod`. The project pins that match.

| Component | Pin | Local license evidence | Use/decision |
|---|---|---|---|
| `modernc.org/sqlite` | `v1.60.1` | BSD-3-Clause; `LICENSE`, `LICENSE-3RD-PARTY.md`, `LICENSE-SQLITE`, `LICENSE-SQLITE_VEC` | Local SQL driver; transpiled SQLite 3.53.4 is public domain. Optional vector extension is not imported by this application; its retained notice is harmless. |
| `modernc.org/libc` | `v1.77.1` | BSD-3-Clause plus full inherited notices, including musl MIT | Exact driver-required runtime support version. |
| `modernc.org/mathutil` | `v1.7.1` | BSD-3-Clause | Transitive runtime mathematics. |
| `modernc.org/memory` | `v1.12.1` | BSD-3-Clause and retained Go/mmap notices | Transitive runtime allocation support. |
| `github.com/dustin/go-humanize` | `v1.0.1` | MIT | Transitive runtime helper. |
| `github.com/remyoudompheng/bigfft` | `v0.0.0-20230129092748-24d4a6f8daec` | BSD-3-Clause | Transitive runtime arithmetic. |
| `github.com/ncruces/go-strftime` | `v1.0.0` | MIT | Runtime helper on the Darwin/Windows graph. |
| `github.com/mattn/go-isatty` | `v0.0.24` | MIT | Updated transitive version; imported on Darwin/Windows. |
| `golang.org/x/sys` | `v0.48.0` | BSD-3-Clause plus PATENTS | Updated to the version required by the SQLite driver. Native vault/lock platform builds need regression checks after this update. |

`github.com/google/uuid v1.6.0` was already pinned and remains in the graph.
Build/test-only dependencies in the module graph are not automatically runtime
dependencies. The checked [runtime manifest](../../third_party/GO_RUNTIME_LICENSES.json)
records actual imported packages separately for all six desktop/CLI target pairs.
The collector retained the complete SQLite and libc inherited license texts,
not just each module's top-level license. Older already-retained notice versions
were preserved.

## Source evidence and application requirements

The cached source was inspected at the exact module pins, without executing
upstream build scripts. Relevant driver paths and observations:

- [`conn.go`, `newConn`](https://gitlab.com/cznic/sqlite/-/blob/v1.60.1/conn.go): opens SQLite with read-write/create, full-mutex and URI handling. The application must construct a fixed local file URI; user paths cannot be concatenated into unescaped DSN options. Restrict state to the application's private directory, outside the upload source.
- [`sqlite.go`, `getDefensiveMode` and `applyDefensiveConfig`](https://gitlab.com/cznic/sqlite/-/blob/v1.60.1/sqlite.go): `_defensive=1` is validated before opening and applied before caller PRAGMAs. `_dqs=0` disables legacy double-quoted string literals. These are per-connection options, not properties of the database file.
- [`conn.go`, `ExecContext`, `QueryContext`, `BeginTx` and `interrupt`](https://gitlab.com/cznic/sqlite/-/blob/v1.60.1/conn.go): database/sql context methods reach the driver; cancellation uses SQLite interruption with synchronization against close. Callers still need bounded waits and safe state on cancellation.
- [`defensive_test.go`](https://gitlab.com/cznic/sqlite/-/blob/v1.60.1/defensive_test.go): inspected rejection before file creation, prevention of writable-schema/journal-off operations, configuration on every physical connection and ordinary transactional workloads.
- [`bind_test.go`](https://gitlab.com/cznic/sqlite/-/blob/v1.60.1/bind_test.go): inspected positional/named parameter mapping fixtures. Application values must use parameter binding; no user-configured SQL, extension or virtual-table registration is part of the journal workflow.

Source SHA-256 values:

```text
modernc.org/sqlite@v1.60.1/sqlite.go 095b77ed583de9b845814210debf6ca0a4e3b23d0134006db9e577e81db709d9
modernc.org/sqlite@v1.60.1/conn.go bfdea7f66925af2c90b4e414f163818ea4e8c6c1d91d73647dddcbd1f2be5cac
modernc.org/sqlite@v1.60.1/defensive_test.go e9438daf39eca13c594ebc4f3f026b91172ffea56e1b61978aab615eaba1d5e5
modernc.org/sqlite@v1.60.1/bind_test.go a8034f07b561f81f9df65692a6297168b97d918e563c7bfe76ddfd069bbeca84
```

The driver does not replace the application's kernel process lock, account and
destination identity checks, plan approval, fail-closed schema handling, or
secret redaction. A successful transaction must precede the corresponding Drive
mutation. Every file ID and operation marker must be recoverable after restart.

The official [SQLite synchronous documentation](https://sqlite.org/pragma.html#pragma_synchronous)
was checked on the review date. In rollback DELETE mode, EXTRA additionally syncs
the directory after removing the journal, which is relevant to power-loss
durability. Use the documented durability mode and verify it at connection setup;
do not claim that an unchecked pragma or an operating system ignoring flushes
guarantees persistence. The final store implementation and its tests are owned by
the transfer slice, separately from this dependency review.

## Verification and limits

With `GOMODCACHE=/Volumes/SSD/storage/data/go/pkg/mod`, and `GOCACHE`, `GOTMPDIR`
and `TMPDIR` on the task-owned SSD build directory, these commands passed:

```sh
go mod verify
python3 tools/collect_licenses.py
python3 tools/verify_licenses.py
```

- `go mod verify`: all downloaded modules verified against their recorded hashes.
- Collection: six target graphs loaded (Darwin/Linux/Windows, amd64/arm64), 29
  runtime module notice sets retained.
- Notice validation: 63 recorded file hashes verified.

Module verification is integrity evidence, not a vulnerability scan or a
substitute for source review. Package loading is not proof of native builds or
runtime behavior on every target. The inspected upstream tests were not run;
application journal/recovery tests and platform build results are recorded in
the main implementation handoff. No external database, personal credential,
cloud write or real upload source was accessed for this dependency review.

## Follow-up path regression

`internal/transferstate/review_test.go` uses only temporary synthetic state and
checks persistence/reopening in names containing spaces, `?`, `#` and encoded
pragma-looking text. The actual SQLite driver retained the literal paths and the
required journal mode. The focused test passed on macOS ARM64 with the same SSD
cache/temporary settings. Windows-invalid question-mark names are explicitly
skipped on Windows.

```sh
go test -count=1 ./internal/transferstate -run '^TestStoreSpecialCharacterPathsRemainLiteral$'
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o build/drive-provider-test/transferstate-windows-amd64.test.exe ./internal/transferstate
```

Both commands passed. The Windows command is compile evidence only. Review also
identified the need for a leading slash in a Windows drive-letter file URI and
for distinguishing source/state paths on different Windows volumes before the
containment check; these corrections belong to the store implementation.
