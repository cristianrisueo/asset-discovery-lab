# Sprint A plan — Ingestion

## Context

Sprint 0 is done and tagged `sprint-0-done`. The repository holds:
- the contracts and generated code;
- `labkit` and `adlctl`, complete;
- the 38 red acceptance cases (`acceptance/`, **locked**);
- a Makefile whose `build`, `up` and `up-core` targets are stubs;
- a compose file with only the `adl` network.

Sprint A (plan §4) must make a change in datacore arrive complete on `entities.*`, with the cache working and a DLQ for what fails. That means Kafka with `kafka-init`, Dragonfly, datacore (§7) and the adapter (§9). Each gets a Dockerfile where needed and a compose service in profile `core` with a health check, and `make up-core` must work.

**Gate** (plan §4, plus the sprint 0 gate, because every gate re-runs the earlier ones):
```sh
make build && make clean && make up-core
make test lint
make acceptance-gate GATE=A
# sprint 0 gate, re-run: buf lint && buf build && go vet ./... && go build ./... && go vet -tags acceptance ./acceptance/... && the 38-count check && go test ./...
```

**Environment found** (read-only checks on 2026-11-25):
- Docker 29.9.3 and Compose v5.7.1, with about 11.6 GiB and 12 CPUs. Go 1.27.2 locally; `go.mod` says `go 1.26.11` (D-003).
- No containers are running, and host ports 29092, 6379, 50051, 8090 and 8091 are free.
- These image tags exist (manifest inspect): `apache/kafka:4.3.2`, `ghcr.io/dragonflydb/dragonfly:v2.1.0`, `golang:1.26.11-bookworm`, `gcr.io/distroless/static-debian12:nonroot`.
- `modernc.org/sqlite` latest at build is **v1.63.0** (proxy, published 2026-11-22).

**What the locked tests require of the services** (read from `acceptance/gate_a_test.go`, `harness_test.go` and labkit):
- The precheck dials `localhost:29092`, `:50051`, `:8090` and `:8091`, plus `:6379` for A05.
- One gRPC port (50051) serves both `AdminService` and `DataCoreService`.
- A02 and A05 compare with `proto.Equal`. The `UpsertAsset` response, `GetAsset`, the Kafka record and the Dragonfly value must therefore be byte-equivalent Assets. In particular, `changed_at` is truncated to milliseconds **before** it is stored and returned.
- A03: the trigger's `changed_at` equals the entity's.
- A05: the system goes quiet when `outbox_pending` = 0 and the committed lag of `adl-adapter` on `triggers` is 0. Duplicate triggers must be served from the cache with no `Get*` call, and the cache value is the serialized Asset with a TTL in (0, 3600].
- A06: the DLQ value equals the original bytes, and the `source.partition` and `source.offset` headers equal the produced coordinates.
- A07: a key that does not match `entity_id` is a VALIDATION_ERROR. A08: phantom LT-8001 gives NOT_FOUND.
- A09: stale reads are retried, never published, and never dead-lettered within 10 s.
- A10: UNAVAILABLE is retried without limit and never dead-lettered.
- `adlctl wait --core` treats `restart: "no"` services as one-shot, which must exit 0. Services with a compose health check must be `healthy`. Health checks defined only in an image are not seen, so every health check is written in compose.

## Step 0 — Save the plan
Save this plan verbatim to `specs/sprints/sprint-A/sprint-plan.md` (plan §3.2).

## Step 1 — Infra: Kafka, kafka-init, Dragonfly, Makefile (spec §8, §15.2–§15.9)

Files:
- **`infra/scripts/create-topics.sh`** (POSIX sh). It runs `/opt/kafka/bin/kafka-topics.sh --bootstrap-server "$BOOTSTRAP" --create --if-not-exists` for the six topics of §8.2:
  - `triggers`: 3 partitions, `cleanup.policy=delete`, `retention.ms=604800000`;
  - `entities.assets`, `entities.users`, `entities.groups`: 3 partitions each, `cleanup.policy=compact`, `min.cleanable.dirty.ratio=0.1`, `segment.ms=3600000`;
  - `dlq.adapter`, `dlq.processor`: 1 partition each, `cleanup.policy=delete`, `retention.ms=1209600000`;
  - replication factor 1 for all. It is idempotent (§8.4): it exits 0 when re-run, and `set -eu` makes it fail loudly on any real error.
- **`infra/scripts/host-prep.sh`**: the §15.5 command, moved out of the Makefile now that `infra/scripts/` has two files (D-007 said sprint A may move it). The alpine pin stays `alpine:3.24.4`.
- **`docker-compose.yml`**: add `kafka`, `kafka-init` and `dragonfly` in profile `core`, on network `adl`, plus volume `kafka-data`.
  - **kafka:** the §8.1 environment, with a fixed `CLUSTER_ID` generated once with `kafka-storage.sh random-uuid` and committed. Heap `-Xmx512m -Xms512m`. `KAFKA_LOG_DIRS` points at the mounted volume. Port `29092:29092`. Memory limit 1024M. Health check `kafka-broker-api-versions.sh --bootstrap-server localhost:9092` (60 s, 5 s, 20). `restart: unless-stopped`.
  - **kafka-init:** same image, `restart: "no"`, `depends_on: kafka: service_healthy`. It bind-mounts `create-topics.sh` read-only, runs it with `sh`, and sets `KAFKA_HEAP_OPTS=-Xmx256m`.
  - **dragonfly:** command `--proactor_threads=1 --maxmemory=256mb --cache_mode=true`, `ulimits.memlock` -1/-1, port `6379:6379`, memory limit 512M. Health check: the image's own probe command copied into compose, with the §15.6 timings (10 s, 5 s, 10); fallback `redis-cli ping`.
- **`Makefile`**: replace the stubs (§15.9).
  - `build: env` runs `$(COMPOSE_ALL) build`.
  - `up: env host-prep` runs `$(COMPOSE_ALL) up -d`, then `$(ADLCTL) wait --timeout 15m`.
  - `up-core: env host-prep` runs `ICEBERG_ENABLED=false $(COMPOSE_CORE) up -d`, then `$(ADLCTL) wait --core --timeout 15m`.
  - `host-prep` calls the script.
  - The other targets stay as they are.
- **`.dockerignore`** (root build context): exclude `.git`, `logs`, `specs`, `processor`, `explorer`, `web`, `acceptance`, `.env`, `**/target` and `**/node_modules`. This keeps the Go build context small.

**Execution-time checks** (images are pulled only in auto mode):
- Read the `apache/kafka:4.3.2` docs and environment, and check the uid and the ownership of the chosen log directory. Mount `kafka-data` on a directory that exists in the image and belongs to the Kafka user, so the named volume inherits that owner (§15.7). If none fits, fall back to a one-shot chown and record it.
- Read the Dragonfly image's `Healthcheck` config and copy its probe.

**Verify:**
- `docker compose --profile core config` succeeds.
- `docker compose --profile core up -d kafka kafka-init dragonfly` brings Kafka up healthy, and `kafka-init` exits 0.
- `kafka-topics.sh --describe` shows the six topics with the right partitions and configs. A second `kafka-init` run also exits 0, which proves idempotency.
- Dragonfly is healthy and `PING` answers.

## Step 2 — datacore code and unit tests (spec §5, §6, §7, §16.2)

Layout (§17.1 layering; no single-file directories):
- **`datacore/main.go`:** subcommands `serve` (the default) and `healthcheck`, which GETs `http://127.0.0.1:<HTTP port>/readyz` and exits 0 or 1. It also does the wiring and the SIGTERM order of §7.11: stop the generator, let the transaction in progress finish, stop the relay after its batch, `GracefulStop` gRPC with a timeout, shut down HTTP, close the DB.
- **`datacore/config.go`** and **`config_test.go`:** every §7.10 variable with its default. Invalid values make the process exit with a clear error.
- **`datacore/health.go`:** `/healthz` returns 200 while serving. `/readyz` returns 200 once the seed is complete **and** the relay has reached Kafka.
- **`datacore/internal/model/`:**
  - id patterns, the kind for each prefix, the reserved and phantom ranges (§5.4);
  - the pools of §5.3, plus a fixed pool of group names for renames;
  - Spanish first-name and surname lists;
  - request validation helpers;
  - tests.
- **`datacore/internal/store/`:** SQLite through `modernc.org/sqlite` v1.63.0 with `journal_mode=WAL`, `synchronous=NORMAL` and `busy_timeout`. It uses a single connection (`SetMaxOpenConns(1)`), so writes are serialised and seq order equals commit order.
  - **Schema:**
    - `entities(type, id, state BLOB, prev_state BLOB NULL, changed_at_ms, PK(type,id))`: proto blobs for the current and previous state;
    - `outbox(seq INTEGER PRIMARY KEY AUTOINCREMENT, topic, key, value, created_at_ms)`;
    - `meta(key, value)`, which holds `seed_complete`.
  - **`Apply`** runs §7.3 in one transaction: read, copy current to previous, mutate, `version+1`, `changed_at = now` truncated to ms, insert the outbox trigger with key `<type>:<id>`, commit. Creation is the same with version 1 and no previous state.
  - Get (current and previous plus `changed_at`), List ordered by id, counts, outbox fetch, delete up to a seq, and pending count.
  - **`seed.go`:** deterministic, from `rand.New(rand.NewPCG(SEED, …))`.
    - Groups G-01 to G-08 with the §5.3 names.
    - Users U-0001 to U-0300 with generated names, uniform groups and `email = lower(id)@acme.example`.
    - Assets split 60/30/10 into LT, DT and SV. OS by the weighted pools; uniform site; 90 % of laptops and desktops assigned to a random user; servers unassigned.
    - Fixed overrides: LT-0042 is a Windows 11 laptop in Alicante owned by U-0117; U-0117 is "Ana Ruiz" in G-03; G-03 is "Ventas".
    - Batched transactions, one outbox row per entity. It runs when `seed_complete` is absent and inserts only the missing ids, so a crash mid-seed resumes. It sets `seed_complete` at the end.
- **`datacore/internal/chaos/`:** a `ChaosConfig` holder (mutex), range validation (§7.8), and decision helpers over an injected, mutex-guarded `*rand.Rand`:
  - `Unavailable()` and `StaleRead(changedAt, now, hasPrev)`;
  - `Duplicate()`, `Malformed(seq)` (odd seq gives 16–64 random bytes; even seq gives a `Trigger` with `UNSPECIFIED`), and `Phantom()` (LT-8000 to LT-8999, version 1).
  - Initial values come from the `CHAOS_*` environment variables.
- **`datacore/internal/generator/`:**
  - It picks a mutation with the §7.6 weights: move 35 %, reassign 20 % (another seeded user or unassigned), OS 15 % (within the asset's pool), user group 15 %, user name 10 %, group rename 5 % (fixed pool). Each picks a value different from the current one.
  - It touches only seeded ids (it enumerates them from the seed counts) and assigns only seeded users and groups.
  - `Run(ctx)` accumulates `rate × elapsed` on a 20 ms tick while not paused. `Burst(n)` applies n changes synchronously.
  - The config lives in memory and is validated (0–500).
- **`datacore/internal/outbox/`:**
  - The relay reads up to `OUTBOX_BATCH` rows in seq order and builds the records. Each real trigger is followed by the chaos extras (duplicate, malformed with key `malformed:<seq>`, phantom). Chaos only adds records (§7.5, §7.9).
  - It produces through franz-go (idempotent, `acks=all`, which is the default with idempotency) and waits for every ack. Only then does it delete rows ≤ max seq. On error it keeps the rows and backs off.
  - It counts `triggers_published`. It sleeps `OUTBOX_POLL_INTERVAL_MS` when the outbox is empty. The producer sits behind an interface for tests.
- **`datacore/internal/grpcapi/`:**
  - **`DataCoreService`** (§7.7): count `get_calls` first, then unavailable chaos (UNAVAILABLE before any lookup), then pattern validation (INVALID_ARGUMENT), lookup (NOT_FOUND), and stale-read chaos (return the previous state).
  - **`AdminService`** (§7.8):
    - `Upsert*` validates the id pattern, kind against the prefix, required fields, and reference patterns, which give INVALID_ARGUMENT. A missing referenced user or group gives FAILED_PRECONDITION. Every call creates a new version.
    - `List*` streams from a snapshot read into memory, so the DB connection is not held while streaming.
    - `Get/Set` for the generator and chaos, with range checks.
    - `Burst` accepts 1 to 10000.
    - `GetStats`: `changes_applied` counts every §7.3 change since start except the seed. It also returns `triggers_published`, `outbox_pending`, `get_calls` and the counts.
  - gRPC reflection is registered (§7.11).
- **Logs:** JSON through `slog.NewJSONHandler`, with `service=datacore` and `LOG_LEVEL`.

**Unit tests (§16.2):**
- Store: a change raises the version by exactly 1 and writes the outbox row in the same transaction. An injected failure before commit leaves neither the change nor the row. Creation is v1 with no previous state.
- Seed: two seeds into two temp DBs give identical entities. LT-0042, U-0117 and G-03 have the §5.3 values. The counts are 8/300/3000 (1800/900/300), and there are 3308 outbox rows.
- Chaos: every knob's hit rate is within tolerance of its configured rate over many draws with a seeded source. Rate 0 never fires and rate 1 always fires. The stale window and `hasPrev` logic hold. Malformed alternates on odd and even seq.
- Admin validation, table-driven: id patterns, the kind/prefix check, empty required fields, missing references (FAILED_PRECONDITION), and Set ranges. Plus `Get*` with a bad id, an unknown id, and the `get_calls` counting.
- Relay with a fake producer: rows are deleted only after acks, order is kept, chaos extras are added and never replace a trigger, and a failure keeps the rows.
- Generator: only seeded ids are touched, the weights are roughly right with a seeded source, and Burst applies n changes.

**Verify:** `go test ./datacore/...` and `golangci-lint run ./datacore/...`.

## Step 3 — datacore image and compose service (spec §15.2, §15.4, §15.6, §15.7, §15.11)
- **`datacore/Dockerfile`**, built from the repository root context:
  - build stage `golang:1.26.11-bookworm`, with BuildKit cache mounts for the module and build caches;
  - `CGO_ENABLED=0 go build -trimpath` of `./datacore`;
  - `mkdir /out/data`;
  - runtime stage `gcr.io/distroless/static-debian12:nonroot`;
  - copy the binary to `/usr/local/bin/datacore` and `/data` with `--chown=65532:65532`, so the `datacore-data` volume is writable (§15.7).
- **Compose `datacore`:** `build: {context: ., dockerfile: datacore/Dockerfile}`, pinned local tag `adl/datacore:0.1.0`, profile `core`.
  - `depends_on: kafka-init: service_completed_successfully`.
  - Environment `GOMEMLIMIT=128MiB` and `KAFKA_BROKERS=kafka:9092`.
  - Ports 50051 and 8090, volume `datacore-data:/data`, memory limit 160M, `restart: unless-stopped`.
  - Health check `["CMD","/usr/local/bin/datacore","healthcheck"]` (30 s, 5 s, 20).

**Verify:**
- `make build` succeeds and datacore becomes healthy.
- `outbox_pending` drains to 0, and the `triggers` end offsets sum to at least 3308.
- `grpcurl` is not needed: `go run ./adlctl stats` and `adlctl generator pause/resume` work against it.
- After a restart, versions persist and the seed is not repeated.

## Step 4 — adapter code and unit tests (spec §6.7, §6.8, §8.3, §9, §16.2)

Layout:
- **`adapter/main.go`:**
  - subcommands `serve` and `healthcheck` (GETs `/healthz`);
  - wiring;
  - SIGTERM: stop polling, finish the current batch (in-flight retries abort when a grace context of about 8 s expires, and unfinished records are **not** committed), commit, close.
- **`adapter/config.go`** and its test: every §9.7 variable.
- **`adapter/health.go`:** `/healthz` returns 200 while running. `/readyz` returns 200 when partitions are assigned (`OnPartitionsAssigned`) and no partition has been in transient retry for more than 30 s (§9.4, §9.8).
- **`adapter/internal/consumer/`** (transport):
  - franz-go group `CONSUMER_GROUP` on `TRIGGERS_TOPIC`, with `DisableAutoCommit`, `BlockRebalanceOnPoll` and reset to the earliest offset (§8.5).
  - For each poll, it groups records by partition and runs one goroutine per partition. Records are handled **sequentially** in offset order (§9.2).
  - It waits for every record to be published or dead-lettered **and acked** (§9.5, §9.6). Only then does it `CommitRecords` the batch, then `AllowRebalance`.
  - The handler and committer sit behind interfaces.
- **`adapter/internal/handler/`** (service):
  - `validate.go` holds the §6.7 rules for triggers (including the key `<type>:<id>`) and for entities, plus a check that the fetched id equals the trigger's.
  - `handler.go` handles one record:
    - decode the trigger, or dead-letter it with DECODE_ERROR;
    - validate it, or dead-letter it with VALIDATION_ERROR;
    - cache GET on `entity:<type>:<id>:v<trigger version>`. A hit that decodes and validates is published; any cache error counts as a miss (§9.3);
    - on a miss, run the fetch loop, then SET under the entity's **own** version, synchronously, before the next record, and publish.
  - It returns an ack handle. Each outcome is logged with `service`, `key`, `version`, `outcome` and `attempt`, with one line per dead letter and one per retry escalation (§9.8).
- **`adapter/internal/fetcher/`** (repository):
  - `Get*` by type with a `GRPC_TIMEOUT_MS` deadline per attempt.
  - `classify.go` maps every outcome of §9.4: success, stale, invalid entity, NOT_FOUND, INVALID_ARGUMENT, and the transient codes plus connection errors.
  - `backoff.go`: `FAST_RETRIES` fast retries of 100, 200 and 400 ms (±20 % jitter), then `min(2 × previous, RETRY_MAX_MS)` without limit.
  - Stale results retry on the same schedule until `STALE_MAX_WAIT_MS` has passed since the first stale result, then STALE_VERSION.
  - Transient time is reported to a per-partition tracker for `/readyz`.
- **`adapter/internal/cache/`:** go-redis against `DRAGONFLY_ADDR`, with 100 ms dial, read and write timeouts, `MaxRetries: -1` (no retries) and a 100 ms context per operation. The library's logger is silenced. It provides `Key()`, `Get` (miss on error) and `Set` with `EX CACHE_TTL_SECONDS` (errors are logged and never returned).
- **`adapter/internal/publisher/`:**
  - franz-go producer: idempotent, `acks=all`, `ProducerLinger(5ms)`.
  - Entity records go to `ENTITY_TOPIC_PREFIX` + `assets|users|groups`, with key = id and header `trigger.version` (§6.8).
  - DLQ records go to `DLQ_TOPIC` with the original key and bytes and the headers of §8.3: `error.reason`, `error.message` (truncated to 1 KB), `source.topic`, `source.partition`, `source.offset`, `failed.at` (RFC 3339 UTC, milliseconds) and `service=adapter`.
  - Produces are asynchronous and return an ack handle. A failed produce is re-sent with backoff until it succeeds or shutdown arrives, so the batch never commits past an unacked record.

**Unit tests (§16.2):**
- Trigger validation, table-driven.
- Classification of every gRPC outcome in §9.4, table-driven.
- Backoff timings: the fast steps within ±20 %, then doubling capped at 5 s.
- The cache key format.
- A cache error is a miss: a go-redis client pointed at a closed port gives a miss within about 100 ms, without blocking.
- Offsets are committed only after the publish ack, with a fake publisher whose ack is delayed: commit is not called before the ack resolves, and is never called on failure or shutdown.
- DLQ headers: all seven, and the truncation of `error.message`.
- Handler flows with fakes: hit means no fetch, plus stale-then-ok, the stale timeout, NOT_FOUND, INVALID_ARGUMENT and transient-then-ok.

**Verify:** `go test ./adapter/...` and lint.

## Step 5 — adapter image and compose service
- **`adapter/Dockerfile`:** the same pattern as datacore, without `/data`.
- **Compose `adapter`:** tag `adl/adapter:0.1.0`, profile `core`.
  - `depends_on`: `kafka-init` completed, `datacore` healthy, `dragonfly` healthy.
  - `GOMEMLIMIT=128MiB`, port 8091, memory limit 160M, `restart: unless-stopped`.
  - Health check `adapter healthcheck` (20 s, 5 s, 10).

**Verify:** `make up-core` reaches `wait: ready`. `entities.*` fill to 3000/300/8 distinct keys, and `adlctl stats` shows `adl-adapter` lag 0.

## Step 6 — Run the gate and iterate (plan §3.5, §3.6)
1. `make build && make clean && make up-core`, then `make test lint`, then `make acceptance-gate GATE=A`. Fix until green. On a failure, read the harness dump, and use G4 for anything non-obvious.
2. Re-run the sprint 0 gate commands.
3. Record `docker stats --no-stream` for the core services, and check them against the §15.4 limits.
4. Restart checks for E05 and E06 behaviour, which come early here (cheap, not gate-required):
   - `docker compose restart datacore` keeps versions;
   - `docker compose stop dragonfly` still lets upserts through.
   Any bug found is fixed now.

## Step 7 — Reports, audit, records, commits (plan §3.7)
1. Write `specs/sprints/sprint-A/build-report.md`: what was built, how to verify, deviations, and notes for sprint B.
2. **Audit subagent** (general-purpose, it did not build this sprint). It reads the spec, the plan and `git diff sprint-0-done..HEAD` plus the working tree, re-runs the gate, and writes `specs/sprints/sprint-A/audit-report.md`: findings by severity and a requirements table for §7, §8, §9 and the relevant parts of §15 and §16.2.
3. Fix every critical and major finding, re-run the full gate, and add a resolution section to the audit report.
4. Update `specs/build-progress.md` (sprint A done, what exists, next is B, known issues).
5. Update `specs/decision-log.md` (D-014 onwards, full G3 fields) with:
   - the pins: `modernc.org/sqlite` v1.63.0, the `golang:1.26.11-bookworm` builder, the distroless tag, the local image tags;
   - the Kafka `CLUSTER_ID` and log directory;
   - the Dragonfly health check;
   - the SQLite schema choice;
   - the admin rejection of LT-9999 and LT-8000 to LT-8999 (G1: keeps A08 and D02 meaningful);
   - the generator details (rename pool, reassign rule);
   - `changes_applied` semantics;
   - the produce-retry and shutdown-grace behaviour;
   - host-prep moved to `infra/scripts/`;
   - the root build context and `.dockerignore`.
   Update `specs/open-questions.md` only if a real doubt arises.
6. Commit in Conventional Commits, with these logical commits:
   - `feat(infra): add kafka, kafka-init and dragonfly with make build and up`;
   - `feat(datacore): …`;
   - `feat(adapter): …`;
   - `docs(sprint-A): …`;
   - `fix: address the sprint A audit findings`;
   - last, `chore(sprint-A): close sprint`.
   The working tree ends clean, with no tags. `locked` files and `acceptance/` stay untouched; check with `git diff --quiet sprint-0-done -- acceptance/ specs/system-spec.md specs/sprints-plan.md CLAUDE.md run-sprints.sh`.

## Risks and the rule for each

| Risk | Rule | Mitigation |
|---|---|---|
| The Kafka volume is not writable by the image user | §15.7, G1 | Mount it on an image directory owned by the Kafka user; otherwise add a one-shot chown. Record it |
| Dragonfly v2.1.0 rejects a flag, or the 512 MiB limit is too small for `maxmemory=256mb` | R9, G1 | Read the startup error; adjust within the 512 MiB budget; record `docker stats` |
| Auto mode blocks the privileged `host-prep` inside `make up-core` | Plan §5 note, G1 | The script already ran `host_prep` before the session. Run the equivalent compose commands and `adlctl wait --core` by hand to verify; the script re-runs the literal gate. Record it |
| JVM startup slow on Intel Mac (Kafka) | R10 | Generous start periods (60 s / 20 retries), as §15.6 sets |
| Seeding 3308 entities is slow on a single SQLite connection | §7.4 | Batched transactions (for example 500 rows each); readiness waits for the seed. Measured during Step 3 |
| A01 within `TReconcile` (3308 sequential `Get*` calls over 3 partitions) | §1.5, §9.2 | Partitions in parallel, async produces acked per batch, 5 ms linger; measured in Step 5 |
| Flaky A05 or A10 from timing | G2, G4 | Never touch the tests. Diagnose with three hypotheses, fix the service or labkit, and record it |
| Memory: kafka 1024 + dragonfly 512 + datacore 160 + adapter 160 | §15.4, R9 | Well under budget. Verify with `docker stats` |
| Duplicates from crash or rebalance | §7.5, §9.5 | Allowed by spec; tests count at least, or distinct keys |
| A labkit bug surfaces | Plan §3.3 | Fix it in labkit (editable, signatures stable) and record it in the build report. `acceptance/` is never edited |
