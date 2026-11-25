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

## Sprint B

### D-025 · 2026-11-25 · G3 · "Latest at build" pins for sprint B
- **Prompted by:** spec §2.2–§2.4 ("Maven 3.9.x latest at build", "mysql:8.4" and plugins without versions); CLAUDE.md: pin exact versions.
- **Evidence:** `repo1.maven.org/maven2/<artifact>/maven-metadata.xml` and the Docker Hub tags API, on 2026-11-25. The spec's fixed versions all resolve: `flink-connector-kafka:5.0.0-2.1`, `flink-doris-connector-2.1:26.3.0` and `protobuf-java:4.36.2` (D-004).
- **Decision:**

  | Item | Version | Note |
  |---|---|---|
  | Maven builder | `maven:3.9.16-eclipse-temurin-17` | Newest 3.9 / Temurin 17 tag. Used by `processor/Dockerfile` and `make test lint` |
  | mysql client | `mysql:8.4.13` | Newest 8.4 patch (`doris-init`) |
  | `maven-compiler-plugin` | 3.16.0 | Newest non-beta |
  | `maven-surefire-plugin` | 3.6.1 | |
  | `maven-shade-plugin` | 3.6.2 | |
  | `build-helper-maven-plugin` | 3.8.0 | |
  | `spotless-maven-plugin` | 3.12.0 | |
  | `google-java-format` | **1.28.0** | Newer releases (1.36+, 2.0) are compiled for Java 21. Spotless 3.12 also refuses anything above 1.28.0 on JDK 17 ("You are running Spotless on JVM 17, which limits you to google-java-format 1.28.0"). The processor builds on Java 17 (§2.2) |
  | `jackson-databind` | 2.23.0 | JSON rows |
  | JUnit | 5.14.4 (BOM) | JUnit 6.1.3 clashed with the JUnit 5.11 vintage engine that `flink-test-utils` 2.1.3 pulls in, and surefire refused to run. The 5.x BOM aligns every JUnit artifact |
  | Flink test utilities | `flink-test-utils` and the `flink-runtime` test-jar, 2.1.3 | In 2.1 the operator test harnesses live in the `flink-runtime` test-jar |
  | Submitter base | `alpine:3.24.4` (D-006) with `bash curl jq coreutils findutils` | apk packages come from that pinned release |

- **Transitive versions worth knowing:** `kafka-clients` 4.2.0, from the Kafka connector.
- **Diagram:** no change.

### D-026 · 2026-11-25 · §3 R2 / §11.1 / §11.2 · Doris entrypoints and configuration
- **Prompted by:** R2 says to read the 4.1.5 entrypoints before writing compose.
- **Evidence:**
  - **FE:** `init_fe.sh` supports election mode (`FE_SERVERS` and `FE_ID`). It appends `priority_networks = <ip>/24` to `fe.conf` on first start.
  - **BE:** `entry_point.sh` takes election mode (`FE_SERVERS` and `BE_ADDR`). It runs `init_be.sh`, which appends `priority_networks` and registers the BE itself with `ALTER SYSTEM ADD BACKEND`. `init_be.sh` exports `SKIP_CHECK_ULIMIT=true` before `start_be.sh`; that is the image's own behaviour, not something this repository turns off.
  - The default `fe.conf` sets `-Xmx8192m -Xms8192m`, which would be OOM-killed in a 1792 MiB container. The default `be.conf` sets BE JVM `-Xmx2048m`.
  - Both images ship `curl` and the `mysql` client.
  - `arrow_flight_sql_port` is already 8070 (FE) and 8050 (BE) in the defaults.
- **Decision:**
  - Election mode, exactly as §11.1 says.
  - `infra/doris/fe.Dockerfile` and `be.Dockerfile` edit the default files with sed: FE heap 1280m/1280m, BE JVM `-Xmx512m`, both Flight SQL ports set explicitly, and `mem_limit = 2048M`.
  - The BE log confirms the limit was accepted: "Mem Limit: 2.00 GB, origin config value: 2048M". `SHOW BACKENDS` shows `172.30.0.3 Alive=true`.
  - FE health check: `/api/bootstrap` must return `"code":0`. HTTP 200 alone is not enough.
- **Diagram:** no change.

### D-027 · 2026-11-25 · G1 · Doris init script
- **Decision:** `infra/scripts/doris-init.sh <sql-file>` runs in the `mysql:8.4.13` container:
  - it waits until a `SHOW BACKENDS` row has `Alive=true`, finding the Host and Alive columns by header name;
  - it replaces every upper-case `<NAME>` placeholder with the environment variable `NAME`, and fails if one has no value;
  - it runs the file and fails on the first SQL error.

  Sprint C reuses it for `init-lake.sql`. The SQL itself is §11.3–§11.5 and §11.7 verbatim; `groups` needed no backticks (R15).
- **Diagram:** no change.

### D-028 · 2026-11-25 · §10 / R11 / R17 · Processor design
- **Decision:**
  - **Envelope:** a Flink POJO with the decoded entity fields in plain columns. It never holds Protobuf messages, so no Kryo or Protobuf serializer is involved.
  - **Decoding:** it happens in the Kafka deserializer, which never throws (§10.2, §16.2). The `DecodeValidate` operator only routes invalid envelopes to the DLQ side output.
  - **DLQ reasons:**
    - an unparseable value is `DECODE_ERROR`;
    - a §6.7 rule break or a key that differs from the id is `VALIDATION_ERROR`;
    - a null value parses as an empty message and fails validation.
  - **Flink operators:** every operator has a uid, not only the stateful ones in §10.10. The filter and JSON mappers are `Only<table>`, `Json<table>`, `EntityType` and `ChangeRateJson`.
  - **Shading:** `flink-connector-base` is `provided`, because `flink-dist` 2.1.3 already bundles it. The Doris connector jar bundles `org/slf4j`, which the shade step drops: Flink loads slf4j parent-first. No protobuf relocation was needed (R11).
  - **Change-rate bounds:** written as `yyyy-MM-dd HH:mm:ss`, the exact form of Appendix A item 7. Entity datetimes are `yyyy-MM-dd HH:mm:ss.SSS` (§10.6).
  - **RowData mapping (§16.2):** it belongs to the Iceberg branch (§10.7) and moves to sprint C with that branch.
- **Diagram:** no change.

### D-029 · 2026-11-25 · Plan §4 sprint B / G1 · `--iceberg.enabled=true` fails fast until sprint C
- **Prompted by:** plan §4 tests only `--iceberg.enabled=false` in sprint B, and the lake services do not exist yet.
- **Decision:** with `--iceberg.enabled=true`, `main()` throws "--iceberg.enabled=true needs the Iceberg branch … use make up-core". The submission fails loudly instead of silently running without the lake (G1: keep §4 and the tests meaningful). Until sprint C, `make up` fails at `flink-job`, and `make up-core` is the supported mode. Sprint C replaces this check with the branch.
- **Diagram:** no change.

### D-030 · 2026-11-25 · §10.11 / §15.7 · Job submitter details
- **Decision:**
  - **Where the script lives:** `infra/flink/submit-job.sh` (§17.1) is bind-mounted read-only into `flink-job`, like `create-topics.sh`, because the processor image's build context (`processor/`) cannot reach `infra/`.
  - **Restore point:** the newest `_metadata` by GNU `find -printf %T@` (sub-second mtime) among `savepoints/*/_metadata` and `checkpoints/*/chk-*/_metadata`.
  - **Program arguments:** empty values (password, benodes, S3 keys) are left out, so the job's §10.12 defaults apply. The JSON array is built with jq from one argument per line.
  - **Cleanup:** the uploaded jar is deleted after `/run`, so the JobManager's upload directory does not grow on every `make up`.
  - **Job id:** none is pinned, so a resubmission gets a new one (E04).
  - **Logs:** JSON lines with `service=flink-job`.
  - **Users:** the submitter image runs as uid 9999, and `/flink-data` is created owned by 9999 in both `infra/flink` and the submitter image.
- **Diagram:** no change.

### D-031 · 2026-11-25 · CLAUDE.md (JSON logs) · Flink logging
- **Evidence:** `flink:2.1.3-java17` already ships `log4j-layout-template-json-2.24.3.jar` in `/opt/flink/lib`.
- **Decision:** `infra/flink/log4j-console.properties` is the image's file with the console appender switched to `JsonTemplateLayout` (`JsonLayout.json`) plus an extra `service` field from `ADL_SERVICE` (`flink-jobmanager`, `flink-taskmanager`). The rolling file keeps the plain layout, because the Flink UI shows that file.
- **Diagram:** no change.

### D-032 · 2026-11-25 · G5 / G1 · `make reconcile` on a core-only stack
- **Prompted by:** the plan's gate B runs `make reconcile` on `make up-core`. Spec §15.9 defines `make reconcile` as a full reconcile, lake included, but §15.2 says core-only mode has no lake, so the lake query would fail.
- **Decision:**
  - `adlctl reconcile` skips the lake comparison, and says so on stderr, when no `iceberg-rest` container is running (core-only mode, or before sprint C).
  - When the lake profile runs, the lake is always compared, so a missing catalog or lake data still fails. `--no-lake` keeps working.
  - If docker compose cannot be read, the lake is compared.
  - `labkit.Reconcile` is unchanged, and the acceptance tests keep passing explicit options.
- **Evidence:** the unit test `TestLakeRunning`, and a live `make reconcile` on the core stack ("lake comparison skipped …", PASS).
- **Diagram:** no change.

### D-033 · 2026-11-25 · §15.9 · Processor test and lint in a builder container
- **Decision:**
  - `make test` runs `mvn -B test` and `make lint` runs `mvn -B spotless:check` in `maven:3.9.16-eclipse-temurin-17`, with the processor mounted and a named cache volume `adl-maven-repo`.
  - The volume is outside the compose project, so `make clean` keeps it.
  - The container is limited to 1536 MiB with a 512 MiB Maven heap; surefire forks with `-Xmx768m`. This keeps it within the memory budget while the core stack runs during the gate.
  - The Docker image build skips tests (`-DskipTests`), because `make test` runs them.
  - CI's `java` job runs `mvn verify`, which includes the Spotless check, bound to `verify`.
- **Diagram:** no change.

### D-034 · 2026-11-25 · §10.9 / §10.11 · Sprint B audit fixes
- **Prompted by:** audit findings F-01, F-02, F-04, F-05, F-07 and F-10 (`specs/sprints/sprint-B/audit-report.md`).
- **Evidence:** before the fix, `/jobs/<id>/config` showed `job-parallelism: 1` although `/jobmanager/config` had `parallelism.default: 2`. Flink's REST run handler uses parallelism 1 when the request does not set it.
- **Decision:**
  - **Parallelism:** the submitter passes the cluster's `parallelism.default` explicitly in `POST /jars/{id}/run`. The job runs at parallelism 2, as §10.9 says.
  - **Step order:** JobManager reachable, then the active-job check (with INITIALIZING counted as present), then a wait for at least `parallelism` free slots. This keeps submission idempotent while a running job holds every slot.
  - **Robustness:** REST failures are retried with JSON logs. Upload and run time out after 300 s. Savepoint pruning ranks complete savepoints by the mtime of `_metadata` and removes incomplete directories.
  - **D-032 refined:** `adlctl reconcile` skips the lake only when no `iceberg-rest` container exists. A stopped or crashed one means the lake is compared.
  - **Log noise:** the logger `org.apache.flink.metrics.MetricGroup` is set to ERROR, which silences the per-checkpoint "pendingCommittables" collision warning from the four chained Doris committers. The metric itself is only dropped from reporting.
  - **Doris FE memory (F-03):** RSS is about 1.58 GiB of its 1.75 GiB (1792 MiB) limit, with no OOM kill. No change for now; R9 applies on an OOM kill.
- **Diagram:** no change.

## Sprint C

### D-035 · 2026-11-25 · G3 · "Latest at build" pins and image checks for sprint C
- **Prompted by:** spec §2.3 (`amazon/aws-cli:2.x` latest at build) and §2.4 (Hadoop client "3.4.x, latest at build"); CLAUDE.md: pin exact versions.
- **Evidence:** `repo1.maven.org/maven2/<artifact>/maven-metadata.xml` and the Docker Hub tags API, on 2026-11-25. The spec's fixed versions resolve: `iceberg-flink-runtime-2.1:1.12.0` (its POM declares no dependencies; it is a shaded bundle), `iceberg-aws-bundle:1.12.0`, `rustfs/rustfs:1.0.6` and `apache/iceberg-rest-fixture:1.10.1`.
- **Decision:**

  | Item | Version | Note |
  |---|---|---|
  | `hadoop-client-api`, `hadoop-client-runtime` | **3.4.3** | Newest 3.4.x. 3.5.0 exists but is outside the spec's 3.4.x line |
  | `amazon/aws-cli` | **2.42.4** | Newest 2.x tag (`rustfs-init`) |
  | `flink-table-api-java-bridge`, `flink-table-runtime` | 2.1.3, `provided` | For `RowData`, `GenericRowData` and `InternalTypeInfo`. The image ships `flink-table-api-java-uber-2.1.3.jar` and `flink-table-runtime-2.1.3.jar` in `/opt/flink/lib` |

- **Image checks (spec §15.6, §15.7, R8):**
  - `rustfs/rustfs:1.0.6` runs as `rustfs` (uid 10001) and owns `/data`, so the named volume inherits a writable directory. No chown init is needed. It has `curl`, so the §15.6 check is used as written. Its server reads `RUSTFS_CONSOLE_ADDRESS`, set to `0.0.0.0:9001`.
  - `apache/iceberg-rest-fixture:1.10.1` runs as uid 1000 with `CATALOG_URI=jdbc:sqlite:/tmp/iceberg_catalog.db`. `/tmp` is mode 1777, so the named volume at `/tmp` is writable (R8). It has `curl`.
  - `iceberg-flink-runtime-2.1:1.12.0` bundles `zstd-jni` with `linux/amd64` native libraries, so `write.parquet.compression-codec=zstd` works in the TaskManager.
- **Diagram:** no change.

### D-036 · 2026-11-25 · §10.7 / §10.10 / R7 / G1 · Processor lake design
- **Decision:**
  - **D-029 removed:** `--iceberg.enabled=true` builds the Iceberg branch.
  - **Table creation:** `main()` runs on the JobManager (§10.12) and calls `LakeCatalog.ensureTable`, which:
    - creates the namespace `inventory` and the table `asset_changes` in the REST catalog `lake` when they are missing;
    - treats `AlreadyExistsException` as success.
  - **Table definition:** `lake.LakeTable` holds the §12.3 schema (ids 1 to 9), `day(changed_at)` and the three properties. The REST catalog stores `format-version` as the table's format version (2), not as a property.
  - **Wiring:** `ProcessorJob.build(env, options, tableLoader)` takes the `TableLoader`, so tests can pass a local Hadoop table and no network is involved. A null loader means core-only mode, with no branch.
  - **Row mapping:** `lake.LakeRows` maps assets to `GenericRowData` in schema order. Its row type is `FlinkSchemaUtil.convert(LakeTable.SCHEMA)`, so the Flink type and the Iceberg schema cannot diverge. An empty owner becomes null; timestamps are `TimestampData` (timestamptz); `ingested_at` is the mapping time.
  - **Catalog properties:** exactly §10.7. Empty S3 keys fail fast with a clear message instead of falling back to the AWS default credential chain. Hadoop `Configuration` comes from the shaded `hadoop-client-api` and `hadoop-client-runtime` (R7). No HTTP-client override was needed.
  - **Sink:** `FlinkSink.forRowData(...).tableLoader(...).uidPrefix("iceberg-sink").writeParallelism(1).append()` (R7; `IcebergSink` not needed).
  - **Operator names (G1):** Iceberg names its own operators from the uid prefix:
    - `iceberg-sink-IcebergStreamWriter` and `iceberg-sink-IcebergFilesCommitter`, with uids under `iceberg-sink`;
    - the terminal sink that `append()` returns is named `IcebergAssetChanges` (§10.10).

    The builder exposes no way to rename the writer and committer, so the §10.10 name is on the sink and the uid prefix on the stateful operators.
  - **Extra operators:** `OnlyAssetChanges` (uid `filter-asset-changes`) and `RowDataAssetChanges` (uid `rowdata-asset-changes`).
  - **Jar size:** the shaded jar is 222 MiB (232,624,133 bytes), below `rest.server.max-content-length` (512 MiB). R18 was not needed.
- **Diagram:** no change.

### D-037 · 2026-11-25 · §3 R6 · Doris path-style property is `use_path_style`
- **Rule:** R6, "use `s3.use_path_style = true` … if Doris rejects it, try `use_path_style`".
- **What failed:** with the §11.6 catalog verbatim, `SELECT … FROM lake.inventory.asset_changes` failed with `errCode = 2, detailMessage = UnknownHostException: warehouse.rustfs`. Doris accepted the property silently but used virtual-hosted addressing.
- **Evidence:**
  - `fe.warn.log`: the failure came from `org.apache.iceberg.aws.s3.BaseS3File.getObjectMetadata` → `ManifestLists.read`, under `IcebergExternalMetaCache`. The FE's Iceberg `S3FileIO` was not set to path style.
  - `javap -v` of `org.apache.doris.datasource.property.storage.S3Properties` in the 4.1.5 `doris-fe.jar`: the field `usePathStyle` has `@ConnectorProperty(names = ["use_path_style", "s3.path-style-access"])`.
  - `AbstractIcebergProperties.toS3FileIOProperties` copies that field to Iceberg's `s3.path-style-access`. `s3.use_path_style` is not among the names, so it is ignored.
  - After `ALTER CATALOG lake SET PROPERTIES ("use_path_style" = "true")`, the same query returned 3,796 rows for 3,000 distinct assets.
- **Decision:**
  - `infra/doris/init-lake.sql` uses `"use_path_style" = "true"` instead of `"s3.use_path_style"`; every other §11.6 property is verbatim.
  - `meta.cache.iceberg.table.ttl-second` exists in 4.1.5 (`IcebergExternalMetaCache`, next to the snapshot and manifest cache keys) and is kept as written.
- **Diagram:** no change.

### D-038 · 2026-11-25 · §3 R9 / §15.4 / G1 · Doris FE: cap glibc malloc arenas
- **Rule:** R9 (memory), applied before an OOM kill. Its first step is "lower JVM heaps or managed memory inside that container's budget". Here only native memory is lowered: the §11.2 heap of 1280m stays.
- **What prompted it:** after the first full-stack gate, `doris-fe` was at 1.72 GiB of its 1.75 GiB (1792 MiB) limit. Sprint B saw 1.58–1.68 GiB without the lake.
  - cgroup `memory.stat`: `anon 1812180992` of `memory.max 1879048192`. `memory.events` showed `max 7`, `oom_kill 0`, `OOMKilled=false`.
  - smaps of the FE JVM:
    - the heap had touched only 983 MB of its 1280 MB, so RSS would keep growing toward the limit;
    - 64 MiB-aligned glibc malloc arenas held 332 MB, across 386 threads and 549 anonymous regions;
    - metaspace was 198 MB, plus code cache, thread stacks and GC structures.
  - The FE uses glibc (no jemalloc or tcmalloc mapped), and `start_fe.sh` launches Java with the inherited environment.
- **Decision:** `MALLOC_ARENA_MAX: "2"` in the `doris-fe` environment.
- **Evidence after:** with the same load, plus gate C and a reconcile: `anon 1575534592` (1.58 GB), heap RSS 941 MB, non-heap 587 MB (before: about 830 MB), `memory.events max 0`. `docker stats`: 1.48 GiB / 1.75 GiB.
- **Remaining risk:** heap fully touched (1280 MB) plus about 600 MB of non-heap is still close to the limit. Sprint D adds Arrow Flight SQL on the FE. On an OOM kill, follow R9 in order (lower the FE heap within its budget, then move budget between services) and record `docker stats` before and after.
- **Diagram:** no change.
