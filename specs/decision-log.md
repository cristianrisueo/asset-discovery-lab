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

## Sprint A

### D-014 · 2026-11-25 · G3 · "Latest at build" pins for sprint A
- **Prompted by:** spec §2.5 lists `modernc.org/sqlite` as "latest at build"; §15.11 needs a Go builder image; CLAUDE.md forbids `latest`.
- **Evidence:** `proxy.golang.org/modernc.org/sqlite/@latest` gave v1.63.0 (published 2026-11-22). `docker manifest inspect` found `golang:1.26.11-bookworm` and `gcr.io/distroless/static-debian12:nonroot`.
- **Decision:**
  - `modernc.org/sqlite` v1.63.0.
  - Builder `golang:1.26.11-bookworm`. Debian 12 is fixed, and it matches `go 1.26.11` in `go.mod` (D-003).
  - Runtime `gcr.io/distroless/static-debian12:nonroot`, exactly as spec §2.3 writes it.
  - Local images are tagged `adl/datacore:0.1.0` and `adl/adapter:0.1.0`, never `latest`.
- **Diagram:** no change.

### D-015 · 2026-11-25 · §8.1 / §15.7 · Kafka broker details
- **Prompted by:**
  - §8.1 asks for a fixed `CLUSTER_ID` and says to check the environment against the image documentation;
  - §15.7 requires a writable volume.
- **Evidence:** `docker image inspect apache/kafka:4.3.2` shows user `appuser` (uid 1000) and a declared volume `/var/lib/kafka/data` owned by `appuser`. `kafka-storage.sh random-uuid` gave `97agkXLTQ7GFSS3sN2JpZA`.
- **Decision:**
  - `CLUSTER_ID: 97agkXLTQ7GFSS3sN2JpZA`, committed.
  - `KAFKA_LOG_DIRS=/var/lib/kafka/data`, with volume `kafka-data` mounted there. The volume inherits the owner, so no chown is needed.
  - Added `KAFKA_MIN_INSYNC_REPLICAS=1` (spec §8.1 "min.insync.replicas 1") and `KAFKA_DEFAULT_REPLICATION_FACTOR=1`.
  - `kafka-init` runs `infra/scripts/create-topics.sh` with `KAFKA_HEAP_OPTS=-Xmx256m`.
  - Verified: `kafka-topics.sh --describe` matches §8.2, and a second run exits 0.
- **Diagram:** no change.

### D-016 · 2026-11-25 · §15.4 / §15.6 · Dragonfly flags and health check
- **Prompted by:**
  - the image's own health check (`/usr/local/bin/healthcheck.sh`) is invisible to `adlctl wait`, which only reads compose health checks;
  - the startup log showed an outbound version check (`version_monitor.cc`).
- **Decision:**
  - Command `dragonfly --logtostderr --proactor_threads=1 --maxmemory=256mb --cache_mode=true --version_check=false`.
  - Compose health check `redis-cli -p 6379 ping | grep -q PONG`, with the §15.6 timings (`redis-cli` ships in the image).
  - No volume: it is a cache.
- **Diagram:** no change.

### D-017 · 2026-11-25 · §7.2 · datacore storage design
- **Prompted by:** §7.2 leaves the schema to the implementer.
- **Decision:**
  - **Tables:**
    - `entities(type, id, state BLOB, prev_state BLOB, version, changed_at_ms)`, holding the current and previous state as deterministic Protobuf;
    - `outbox(seq AUTOINCREMENT, topic, key, value, created_at_ms)`;
    - `meta(seed_complete)`.
  - **One SQLite connection** (`SetMaxOpenConns(1)`): writes are serialised, so outbox seq order equals commit order and the relay can delete with `seq <= max`.
  - **`Apply`** is detached from cancellation, so a change that has started always commits or rolls back as a whole (§7.11).
  - **`changed_at`** is truncated to milliseconds before it is stored.
  - **Seed:** batches of 500. The seed only inserts missing ids, so an interrupted seed resumes, and `seed_complete` marks the end.
- **Evidence:** unit tests in `datacore/internal/store`. A live restart kept versions (v2 then v3 after the restart) and did not seed again.
- **Diagram:** no change.

### D-018 · 2026-11-25 · G1 · The admin API rejects ids that are never created
- **Prompted by:** §5.4 says LT-9999 and LT-8000 to LT-8999 are "never created by anyone", but §7.8 does not list that check.
- **Decision:** `UpsertAsset` returns INVALID_ARGUMENT for LT-9999 and LT-8000 to LT-8999. G1 favours keeping the §16 tests meaningful: A08, E01 and D02 rely on those ids never existing.
- **Diagram:** no change.

### D-019 · 2026-11-25 · §17.1 · Extra Go packages
- **Prompted by:** §17.1 gives `datacore/internal/{store,outbox,generator,chaos,grpcapi}` and `adapter/internal/{consumer,fetcher,cache,publisher}` as examples.
- **Decision:**
  - **`datacore/internal/model`:** ids, pools and request validation, shared by `store`, `generator` and `grpcapi`.
  - **`adapter/internal/handler`:** the service layer between the consumer (transport) and the fetcher, cache and publisher.
  - **`adapter/internal/contract`:** the §6.7 rules, used by both `handler` and `fetcher`. Without it they would form an import cycle.
  - **No shared copy of the rules:** Go's `internal/` rule stops `adapter/` from importing `datacore/internal/model`. Each service keeps its own copy of the id patterns, as two independent services would.
- **Diagram:** no change.

### D-020 · 2026-11-25 · §7.6 · Generator details
- **Prompted by:** §7.6 gives the mix but not every detail.
- **Decision:**
  - Every change picks a value different from the current one.
  - "Reassign" draws uniformly from the seeded users plus "unassigned".
  - "OS" stays within the asset's pool.
  - "Group rename" uses a fixed pool of 16 names: the 8 seed names plus Legal, Compras, Calidad, Logística, Innovación, Datos, Seguridad and Comunicación.
  - The rate loop never owes more than one second of changes, so a slow moment does not turn into a flood.
  - The random source is seeded from the clock. Only the seed dataset must be deterministic (§5.3).
- **Note:** the generator also changes LT-0042, U-0117 and G-03, as §5.4 allows. The sprint E walkthrough must not assume the seed values still hold (audit F-10).
- **Diagram:** no change.

### D-021 · 2026-11-25 · §6.5 · Stats semantics
- **Decision:**
  - `changes_applied` counts every §7.3 change since start (upserts, generator, Burst), not the seed.
  - `triggers_published` counts real triggers acknowledged since start, seed included, chaos extras excluded.
  - `get_calls` counts every `Get*` call, failed ones included (§6.5).
  - All three reset on restart ("since start").
  - `outbox_pending` and the entity counts come from the database.
- **Diagram:** no change.

### D-022 · 2026-11-25 · §9.2 / §9.4 / §9.5 / §9.8 · Adapter delivery details
- **Prompted by:** the spec leaves these open; audit findings F-01 to F-05 and F-09 of sprint A.
- **Decision:**
  - **Consumer:** one franz-go client with `DisableAutoCommit`, `BlockRebalanceOnPoll`, reset to start, `SessionTimeout` 10 s (faster recovery for E02), idempotent producer with `acks=all` and a 5 ms linger.
  - **Commit:** after each batch, every partition commits the end of its acknowledged prefix.
  - **A failed ack outside shutdown:** `Run` returns `ErrIncomplete` and the process exits, so the restarted adapter handles the record again, in order, from the last committed offset. This means offsets are never committed past an unhandled record (F-02).
  - **Failed produces are not re-sent by the publisher.** franz-go already retries retriable errors, and a later re-send could place an older version after a newer one on a compacted topic (F-03).
  - **Shutdown:** after SIGTERM the batch in progress has 7 s, below compose's 10 s stop timeout. Unfinished records are not committed. The client closes with `CloseAllowingRebalance`, so it leaves the group instead of hanging (F-01). `Run` also always calls `AllowRebalance` on return.
  - **Codes outside the §9.4 list** (for example PERMISSION_DENIED) are transient: "losing data is worse than a visible stall".
  - **Readiness** needs at least one assigned partition, and drops on revoke or loss (F-04).
  - **"Retry escalation"** is logged once, when the fast retries are used up (F-05).
  - **gRPC reconnect backoff** is 100 ms to `RETRY_MAX_MS` instead of grpc-go's 120 s default (F-09).
- **Evidence:** consumer and publisher unit tests. Live: a graceful `docker compose stop adapter` took 0.25 s with exit 0, versus 10 s and exit 137 before the fix.
- **Diagram:** no change.

### D-023 · 2026-11-25 · D-007 · Repository and build context
- **Decision:**
  - `host-prep` moved to `infra/scripts/host-prep.sh`, next to `create-topics.sh`, as D-007 allowed.
  - The Go images build from the repository root, because there is one module (§17.1).
  - `.dockerignore` excludes `.git`, `specs`, `acceptance`, `processor`, `explorer`, `web`, `infra` and Markdown. A later image that builds from the root context and needs one of those must adjust it.
  - `make build` depends on `make env`, so `.env` exists before compose interpolates it (sprint 0 audit F-07).
- **Diagram:** no change.

### D-024 · 2026-11-25 · §7.8 · Upsert validation does not enforce the pools
- **Prompted by:** §5.1 says `os` and `site` come from pools, but §7.8 lists only the id pattern, kind/prefix, required fields and references.
- **Decision:** the admin API checks what §7.8 lists. `os` and `site` must be non-empty, but they are not restricted to the pools. Tests and `adlctl move` use pool values anyway.
- **Diagram:** no change.
