# Decision log

Every rule applied, fallback taken and "latest at build" pin (spec §3, G3). Each entry has the date, the rule, what failed or prompted it, the evidence, the decision, and whether it changes the diagram (§4.1).

## Sprint 0

### D-001 · 2026-11-25 · G3 · "Latest at build" pins for Go modules
- **Prompted by:** spec §2.5 lists several modules as "latest at build".
- **Evidence:** `https://proxy.golang.org/<module>/@latest` on 2026-11-25.
- **Decision:**

  | Module | Version |
  |---|---|
  | `google.golang.org/protobuf` | v1.36.12 |
  | `github.com/redis/go-redis/v9` | v9.24.0 |
  | `github.com/go-sql-driver/mysql` | v1.10.2 |
  | `github.com/aws/aws-sdk-go-v2/service/s3` | v1.116.0 |
  | `github.com/aws/aws-sdk-go-v2` | v1.48.0 |
  | `github.com/aws/aws-sdk-go-v2/credentials` | v1.21.3 |

  The fixed versions of the spec are used as given: `google.golang.org/grpc` v1.85.0 and `github.com/twmb/franz-go` v1.22.3. `modernc.org/sqlite` is pinned in sprint A, together with datacore.
- **Diagram:** no change.

### D-002 · 2026-11-25 · G1 · `franz-go/pkg/kadm` has its own version
- **Prompted by:** spec §2.5 lists "`github.com/twmb/franz-go` (with `pkg/kadm`) v1.22.3". `pkg/kadm` is a separate Go module with its own versions, so `kadm@v1.22.3` does not exist.
- **Evidence:** the proxy lists `github.com/twmb/franz-go/pkg/kadm` latest = v1.20.0, published on 2026-11-09 alongside franz-go v1.22.3.
- **Decision:** franz-go v1.22.3, plus kadm v1.20.0 (the matching release). kmsg v1.15.1 comes in indirectly.
- **Diagram:** no change.

### D-003 · 2026-11-25 · G3 / G5 · Go version
- **Prompted by:** spec §2.2 asks for "1.26.x, latest patch". The build machine has Go 1.27.2.
- **Evidence:** `go.dev/dl` lists go1.26.11 as the newest 1.26 patch. A `go 1.26.11` directive builds unchanged with the local 1.27.2 toolchain: no toolchain download, and the gate is green.
- **Decision:** `go.mod` declares `go 1.26.11`. CI pins `go-version: 1.26.11`. Go 1.27 is newer, so local builds also satisfy "Go 1.26+".
- **Diagram:** no change.

### D-004 · 2026-11-25 · §6.6 / R16 · buf plugins
- **Prompted by:** spec §6.6 asks for remote plugins pinned at "latest at build".
- **Evidence:** `buf generate` with remote plugins worked on the first attempt. R16 was not needed.
  - Go output reports `protoc-gen-go v1.36.12` and `protoc-gen-go-grpc v1.6.2`.
  - Java output reports `Protobuf Java Version: 4.36.2`.
  - The newest protobuf release on GitHub is v36.2, and `protobuf-java` 4.36.2 exists on Maven Central.
- **Decision:**

  | Plugin | Version |
  |---|---|
  | `buf.build/protocolbuffers/go` | v1.36.12 |
  | `buf.build/grpc/go` | v1.6.2 |
  | `buf.build/protocolbuffers/java` | v36.2 |

  **The processor (sprint B) MUST use `com.google.protobuf:protobuf-java` 4.36.2** (spec §6.6: plugin vX.Y goes with protobuf-java 4.X.Y). The buf CLI is v1.75.0.
- **Diagram:** no change.

### D-005 · 2026-11-25 · §15.1 · Tools run through `go run` at pinned versions
- **Prompted by:** spec §15.1 says the host needs only Go, make and git, yet `make lint` and `make generate` need buf and golangci-lint.
- **Decision:** the Makefile invokes the tools through `go run` at pinned versions. Both variables can be overridden.
  - `BUF ?= go run github.com/bufbuild/buf/cmd/buf@v1.75.0`
  - `GOLANGCI_LINT ?= go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0`

  golangci-lint v2.15.0 is the latest at build (proxy, 2026-10-16), and CI pins the same version.
- **Diagram:** no change.

### D-006 · 2026-11-25 · G3 · Image and action pins
- **Prompted by:** spec §2.1 and CLAUDE.md: pin exact versions, never `latest`. Spec §2.3 lists `alpine:3.x` as "latest at build".
- **Evidence:** Docker Hub tags of `library/alpine`, and the latest release of each action from `api.github.com/repos/<action>/releases/latest`, both on 2026-11-25.
- **Init helper:** `alpine:3.24.4` is the latest at build (Docker Hub), used by `make host-prep`.
- **GitHub Actions**, pinned to exact release tags (latest on 2026-11-25):

  | Action | Tag |
  |---|---|
  | `actions/checkout` | v7.0.1 |
  | `actions/setup-go` | v7.1.0 |
  | `actions/setup-java` | v6.3.0 |
  | `actions/setup-node` | v7.1.0 |
  | `actions/upload-artifact` | v7.0.2 |
  | `golangci/golangci-lint-action` | v9.3.0 |
  | `docker/setup-buildx-action` | v4.4.1 |
  | `docker/build-push-action` | v7.4.0 |

- **Workflow check:** the workflows were checked with `rhysd/actionlint:1.7.12`. This is a local check only; it is not part of the repository.
- **Diagram:** no change.

### D-007 · 2026-11-25 · CLAUDE.md "no single-file directory" · host-prep inline
- **Prompted by:** spec §17.1 places `host-prep.sh` in `infra/scripts/`, but in sprint 0 it would be that directory's only file.
- **Evidence:** CLAUDE.md and spec §17.1: "No directory with a single file".
- **Decision:** `make host-prep` runs the spec §15.5 command inline. A separate `infra/scripts/host-prep.sh` would leave `infra/scripts/` with a single file until sprint A adds `create-topics.sh`. Sprint A MAY move it there.
- **Diagram:** no change.

### D-008 · 2026-11-25 · G1 · labkit design choices
- **Prompted by:** sprints plan §3.4 locks `acceptance/` after sprint 0, and spec §16.3 leaves some client details open.
- **Evidence:** the labkit unit tests (`go test ./labkit/`) cover each choice below.
- **Why labkit holds the logic:** `acceptance/` is locked after sprint 0, so every mechanism that might need fixing lives in `labkit`. The acceptance tests stay thin and call labkit. The labkit API they use must stay stable: implementations may change, signatures may not.
- **S3 credentials:** spec §16.3 lists endpoints only. labkit reads `S3_ACCESS_KEY`, `S3_SECRET_KEY` and `S3_REGION`, with the `.env.example` values as defaults.
- **Repository root:** for `docker compose` and `make`, it comes from `ADL_REPO_ROOT`, or from the nearest ancestor holding `docker-compose.yml` and `go.mod`. Compose commands activate all three profiles, so any defined service can be addressed.
- **Doris client:** it forces `parseTime=true`, `loc=UTC` and `interpolateParams=true` on top of `ADL_DORIS_DSN`. Queries therefore never depend on server-side prepared statements, and `DATETIME` values are read as UTC.
- **Lake reconcile categories:** missing, behind, ahead and duplicate (id, version). Spec §16.5 step 4 does not ask for an "extra id in the lake" category, so there is none.
- **Reconcile retries:** `labkit.Reconcile` also retries source errors until the timeout, because §16.5 says "repeat until it is equal or the timeout expires". For lake-only reconciliation (test C05) there is an internal `NoDoris` option; adlctl does not expose it.
- **Diagram:** no change.

### D-009 · 2026-11-25 · G1 · adlctl behaviour
- **Prompted by:** spec §15.10 names the subcommands but not their behaviour at the edges. Plan §4 asks them to fail cleanly when their target is not running.
- **Evidence:** `adlctl/main_test.go` runs every subcommand against closed ports: exit 1, one line naming the target, in well under a second each.
- **`move ID SITE`** reads the asset through `AdminService.ListAssets`, not `GetAsset`. Chaos only applies to `DataCoreService` (§7.7), so `move` never upserts a stale or UNAVAILABLE read.
- **`wait`:**
  - It evaluates only the services defined for the active profiles (sprints plan §3.6).
  - With no services defined, it prints a message and exits 0.
  - A one-shot service that exited non-zero fails `wait` at once, since waiting cannot fix it.
  - The BE check requires a backend with `Alive=true` and prints its host. R2's `172.30.0.3` is visible in the table.
- **`flink stop-with-savepoint`** exits 0 when no job is RUNNING ("nothing to stop"), and 1 when Flink is unreachable. `make down` turns a failure into a warning and stops anyway (§10.11).
- **Failures:** every command fails with exit code 1 and one `adlctl: …` line naming the target. Usage errors exit 2.
- **Diagram:** no change.

### D-010 · 2026-11-25 · G1 · Makefile and CI for components that do not exist yet
- **Prompted by:** plan §4, sprint 0: CI jobs must tolerate components that do not exist yet, and Makefile targets that cannot work must say so. The sprint A gate runs `make test lint` before any Java, Rust or web code exists.
- **Evidence:** `make test` and `make lint` are green in sprint 0. `actionlint` 1.7.12 reports 0 errors on both workflows.
- **Makefile:** `build`, `up` and `up-core` print `not available until sprint A` and exit 1, as plan §4 asks. `test` and `lint` skip the Java, Rust and web parts while their build files do not exist. The sprint that adds a component replaces its stub; a present component with a stub still fails loudly.
- **`ci.yml`:** a `detect` job enables the java, rust, web and images jobs only once `processor/pom.xml`, `explorer/Cargo.toml`, `web/package.json` or compose services with a `build` section exist.
  - The images job builds every compose build service with its own context and Dockerfile.
  - The web job expects the npm scripts `lint`, `typecheck`, `test` and `build`. Sprint D must provide them.
- **Diagram:** no change.

### D-011 · 2026-11-25 · G1 / G2 · Acceptance harness rules
- **Prompted by:** plan §4, sprint 0: every test must fail for the right reason. Spec §16.3 sets the harness rules.
- **Endpoint precheck:** every test first dials the host endpoints of the services it involves, once, with a 5 s timeout. It fails, never skips, with `<endpoint> not reachable at <addr>: <err>`. In sprint 0 all 38 cases therefore fail fast for the right reason: connection refused. Evidence: `go test -tags acceptance ./acceptance/...` with no stack gave 38 FAIL, every one with "the stack is not running or not reachable", in 25 s.
- **Failure dump:** on failure the harness prints `docker compose ps` and `logs --tail 200` per involved service (§16.3).
- **Random bytes:** "random bytes" (A06, B05) are sampled until they fail to decode as the target message. Random bytes occasionally form valid Protobuf, which would turn an expected DECODE_ERROR into a VALIDATION_ERROR by chance.
- **Explorer and web requests:** these steps without a stated timeout (D02, D04–D08) poll within `TShort` until the response is neither a transport error nor 503, then assert. A 503 is never the asserted outcome.
- **Diagram:** no change.

### D-012 · 2026-11-25 · G1 · Interpretations of §16.4 rows
Each case asserts what its row says. Where a row leaves something open, I chose the following:

- **A05:** "produce the identical trigger (same key and bytes) twice" re-produces the exact key and bytes of the original trigger, read back from `triggers`. "Two more records appear" passes when at least two new valid LT-9003 v records appear after that point.
- **A06:** besides "partition and offset present", it checks they equal the coordinates of the produced record. This is always true for a correct adapter.
- **A07, A08:** no timeout given; they use `TShort`, like A06.
- **A09:** "nothing in dlq.adapter" and "no lower version after the upsert" are checked over the records written since the test started, once the expected version has arrived. The adapter handles each partition in order, so once v is published, the trigger was not dead-lettered.
- **A10** (changed after audit F-01): once the final version is on `entities.assets`, the test also waits within the same `TPipe` budget for a quiet system (outbox empty, `adl-adapter` lag 0). Only then does it assert that `dlq.adapter` holds nothing for `asset:LT-9008`. The final version can arrive while earlier triggers are still being retried, so checking earlier would assert less than the row.
- **B05:** no timeout given for the DLQ records; `TPipe`, since the path crosses Flink.
- **B07, D05:** the manual `REFRESH MATERIALIZED VIEW` runs first. If it errors (for example with the R13 view fallback), the error is logged and the test still waits up to 150 s for equality.
- **B08:** "since the stack started" is the start time of the Flink job. If the generator was not at its default rate, the test sets it and counts from that moment. It then requires a closed asset row and checks every row.
- **B09:** p95 is the nearest-rank percentile, the 19th of 20 sorted latencies. Each wait is bounded by `TPipe`, and the 60 s maximum is asserted separately.
- **C01:** fields are compared by name, type and required flag. Field ids are not compared: the catalog may reassign them, and §16.4 does not list them. The partition source is resolved through its source id to `changed_at`.
- **C04:** the query is retried on errors within `TShort`, then must return zero rows.
- **C05:** "reconcile the lake" runs the lake part only, with `NoDoris`, within `TLake + TReconcile`.
- **D04:** pauses the generator and runs a Doris reconcile before paging (§16.3 isolation; the reconcile was added after audit F-03). Otherwise an asset moving in or out of Madrid between the two requests, or a change still in flight, can shift the second page and create a false overlap.
- **D07:** besides "200 JSON", it checks the body is valid JSON with `id` = LT-0042.
- **E01:** the DLQ window runs from the soak start until after the reconcile, so dead letters caused by the soak but written slightly later are included. A phantom-id key is `^asset:LT-8\d{3}$`.
- **E02, E03:** the `TRecover` window starts after the killed service has been started again and the burst has returned. Lag, job state and reconciles share that one deadline.
- **E04:** proves "a savepoint was written" in two steps:
  - before `make down`, list `/flink-data/savepoints` through `docker compose exec`;
  - after `make up`, require that the new job's `latest.restored` is a savepoint whose path was not in that list.

  It uses LT-9503 for the new upsert. `make up` is bounded by 30 minutes, `make down` by 15.
- **E05:** Dragonfly is also started in `t.Cleanup`, so a failed test does not leave it down.
- **E06:** `/readyz` within `TPipe`. `GetAsset` is retried within `TShort` until the restarted server answers. "The reconcile passes" is the full §16.5 reconcile, Doris and the lake (changed after audit F-02). Rows that mean Doris only say "Doris reconcile" (B01, E02, E04).
- **A03** (after audit F-04): records keyed `asset:LT-9002` that do not decode are ignored instead of aborting the test. The test only needs the trigger of version v.
- **Evidence:** the audit report (`specs/sprints/sprint-0/audit-report.md`). After the fixes, `go vet -tags acceptance`, the gate and a no-stack acceptance run are green again (38 FAIL, all for unreachable services).
- **Diagram:** no change.

### D-013 · 2026-11-25 · G1 · labkit and tooling fixes from the sprint 0 audit
- **Prompted by:** audit findings F-05, F-06 and F-07.
- **Evidence:** `specs/sprints/sprint-0/audit-report.md`.
- **Decision:**
  - `Tail.ReadToEnd` treats a lagging partition as compacted only after two consecutive empty 3 s polls, not one.
  - `make lint` runs `buf breaking --against '.git#branch=main'` whenever a `main` branch with `buf.yaml` exists, including on `main` itself, where it compares the working tree with the last commit.
  - `ci.yml` creates `.env` from `.env.example` before running compose commands, and `e2e.yml` runs `make env` before `make build`.
- **Diagram:** no change.
