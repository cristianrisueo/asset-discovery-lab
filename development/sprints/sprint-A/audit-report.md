# Sprint A audit report

Auditor: an independent subagent that did not build the sprint. Date: 2026-11-25.
Scope: `git diff sprint-0-done..HEAD` (three commits: datacore `8eb5973`, adapter `790a8fb`, infra `18d53b1`; 46 files), plus the untracked `specs/sprints/sprint-A/sprint-plan.md` and `build-report.md`.
Read in full: spec §1–§18 and Appendix A; sprints plan §3 and §4; the sprint plan and build report; `specs/decision-log.md`, `specs/open-questions.md`; the sprint 0 audit report (format). I read all production code of datacore and the adapter line by line, the compose file, the Dockerfiles, the scripts and the Makefile diff. I also read the unit tests against §16.2.

## 1. Summary

Sprint A is in good shape. The gate is green from a clean state, and so is the sprint 0 gate. Gate A passes 10/10 in 19 s. The race detector finds nothing. datacore follows §7 closely:
- one SQLite transaction per change, holding the change and its outbox row;
- `changed_at` truncated to milliseconds before it is stored;
- a deterministic seed with the fixed §5.3 example;
- a relay that deletes rows only after acks, and chaos that only adds records;
- the right gRPC codes and range checks.

The adapter follows §9: sequential per-partition handling, commit only after acks, the §9.4 classification and backoff, a synchronous cache SET, and the §8.3 DLQ headers. The live checks I added (§1.2) confirmed the behaviours that gate A does not exercise. `/readyz` turns 503 after 30 s of transient retry while `/healthz` stays 200. Versions and the seed survive a datacore restart. Every admin and data error code is correct.

There are **no critical findings** and **one major finding**:
- **F-01 (major):** the adapter never finishes its SIGTERM shutdown. `kgo.Client.Close()` hangs because the client uses `BlockRebalanceOnPoll` and the last poll is never released with `AllowRebalance`. Every `docker compose stop`, `make down` or `restart` of the adapter ends in SIGKILL (exit 137) at the stop timeout, and the member never leaves the group. No data is lost, because the last batch is committed before the hang, but §9.8 ("…commit, then close") is not met.

The remaining findings are minor or informational.

### 1.1 Gate results (re-run exactly as written)

Sprint A gate, run in sequence from the state I found (stack already up):

```
$ make build                       -> exit 0   (images adl/adapter:0.1.0, adl/datacore:0.1.0 built)
$ make clean                       -> exit 0   (volumes adl_kafka-data, adl_datacore-data and network removed)
$ make up-core                     -> exit 0   ("wait: ready"; adapter, datacore, dragonfly, kafka healthy; kafka-init completed)
$ make test lint                   -> exit 0
    go test ./...                  -> every package ok (adapter/..., datacore/..., adlctl, labkit)
    processor/explorer/web         -> "not present yet, skipped"
    buf lint                       -> ok
    buf breaking --against '.git#branch=main' -> ok
    golangci-lint v2.15.0 run ./...-> "0 issues."
$ make acceptance-gate GATE=A      -> exit 0
    --- PASS: TestGateA_01_SeedPublished (0.12s)
    --- PASS: TestGateA_02_AssetChangePublished (1.05s)
    --- PASS: TestGateA_03_TriggerContract (1.05s)
    --- PASS: TestGateA_04_UsersAndGroups (1.06s)
    --- PASS: TestGateA_05_CacheHit (2.08s)
    --- PASS: TestGateA_06_DecodeErrorToDlq (2.06s)
    --- PASS: TestGateA_07_ValidationErrorToDlq (1.02s)
    --- PASS: TestGateA_08_NotFoundToDlq (1.02s)
    --- PASS: TestGateA_09_StaleReadRecovered (2.07s)
    --- PASS: TestGateA_10_TransientErrorsRetried (7.11s)
    PASS
    ok  github.com/cristianrisueo/asset-discovery-lab/acceptance  19.228s
```

Sprint 0 gate:

```
$ go run github.com/bufbuild/buf/cmd/buf@v1.75.0 lint && go run github.com/bufbuild/buf/cmd/buf@v1.75.0 build \
  && go vet ./... && go build ./... && go vet -tags acceptance ./acceptance/... \
  && test "$(grep -hoE '^func TestGate[A-E]_[0-9]{2}_' acceptance/*_test.go | wc -l)" -eq 38 && go test ./...
-> exit 0 (every package ok)
```

Extra: `go test -race -count=1 ./datacore/... ./adapter/...` gives exit 0, with all 14 packages ok.

Locked files: `git status` shows only `?? specs/sprints/sprint-A/`. `acceptance/`, the spec, the plan, `CLAUDE.md` and `run-sprints.sh` are untouched by the diff.

### 1.2 Live checks beyond the gate

All of these ran against the running core stack. I restored everything I changed: chaos is back to zero with a 300 ms window (verified with `adlctl chaos show`), and the generator was never touched. One reserved test asset was created, **LT-9601** (v1). No acceptance case uses it.

| Check | Result |
|---|---|
| `kafka-topics --describe` | `triggers` 3p, delete, 7 d; `entities.*` 3p, compact, `min.cleanable.dirty.ratio=0.1`, `segment.ms=3600000`; `dlq.*` 1p, delete, 14 d; RF 1 everywhere; `min.insync.replicas=1` |
| Broker config | `auto.create.topics.enable=false`, `default.replication.factor=1`, `min.insync.replicas=1` |
| `kafka-init` re-run (`docker compose run --rm kafka-init`) | exit 0: idempotent (§8.4) |
| Volume ownership (§15.7) | `kafka-data` root dir is uid 1000 (`appuser`, the Kafka user); `datacore.db*` are owned by 65532 |
| Logs | Every datacore and adapter line is JSON with `service`. Dead-letter lines carry `key`, `version`, `outcome` and `attempt` |
| `GetAsset("bad")` / `GetAsset("LT-9999")` | `InvalidArgument` / `NotFound`; `get_calls` +2 |
| Upserts: LT-9999; DT with kind LAPTOP; empty hostname | `InvalidArgument` |
| Upserts: owner U-9999 (missing); user in G-99 (missing) | `FailedPrecondition` |
| `Burst(0)`, chaos rate 1.5, window 10001, generator rate 501 | `InvalidArgument` |
| `unavailable_rate=1`, then `UpsertAsset(LT-9601)` | adapter `/readyz` 200 until t≈25 s, **503 at t=30, 35 and 40 s**, `/healthz` 200 throughout. After chaos off, `/readyz` returned to 200 in 1.5 s and the lag reached 0. No DLQ |
| `docker compose restart datacore` | Clean shutdown in 6 ms (exit 0); restart logs `seed complete … "inserted":false`; versions persisted |
| `docker stop adl-adapter-1` (default timeout), then `docker stop -t 25` | **Both SIGKILLed (exit 137, `OOMKilled=false`)** after the full timeout (3 s and 25 s). The only log line after SIGTERM is `adapter shutting down` → F-01 |
| `docker stats` | adapter 12 MiB / 160, datacore 17 MiB / 160, kafka 676 MiB / 1 GiB, dragonfly 38 MiB / 512 |

At the end, `adlctl wait --core` reports `wait: ready` and the `adl-adapter` lag is 0. The stack is left running.

## 2. Findings

### Critical

None.

### Major

**F-01 · major · `adapter/main.go:176` (with `adapter/internal/consumer/consumer.go:66-69`): SIGTERM never completes; `kc.Close()` hangs and the container is SIGKILLed**
- **What is wrong:**
  - The client is built with `kgo.BlockRebalanceOnPoll()` (`main.go:86`).
  - On shutdown, `Consumer.Run` returns straight after the final `PollFetches(ctx)` (`consumer.go:66-69`) without calling `AllowRebalance()`.
  - `run` then calls `kc.Close()`. franz-go documents this case (`pkg/kgo/client.go`, `CloseAllowingRebalance`): "Close itself does not allow rebalances and **will hang if you polled, did not allow rebalances, and want to close**."
- **Consequence:**
  - Every graceful stop (`docker compose stop|restart adapter`, `make down`) waits for the full stop timeout and ends in SIGKILL with exit 137. I reproduced this twice; with `-t 25` it took exactly 25 s.
  - The member never sends LeaveGroup, so the next adapter instance waits for the session timeout (10 s) before it gets partitions. This affects `make down && make up` (E04) and any restart.
  - The HTTP server shutdown and the deferred closes (gRPC connection, Dragonfly) never run.
  - No data is lost: the batch in progress is committed inside `Run` before the hang, and uncommitted records are reprocessed.
- **Evidence:** spec §9.8: "SIGTERM: stop polling, finish the current batch, commit, then close." Live: only `adapter shutting down` is logged after SIGTERM, and `adapter stopped` never appears.
- **Suggested fix:**
  - Close with `kc.CloseAllowingRebalance()`, or call `c.Client.AllowRebalance()` before every return in `Consumer.Run`. Doing both is clearest.
  - Optionally bound the leave with `kc.LeaveGroupContext(ctxWithTimeout)` before `Close`.
  - Add a unit test asserting that `Run` calls `AllowRebalance` on the shutdown path. The fake client already counts `allowed`.
  - Re-check with `docker compose stop adapter`: it should exit 0 in well under a second when idle.

### Minor

**F-02 · minor · `adapter/internal/consumer/consumer.go:122-139` (with `processBatch` and `Run`): a failed ack outside shutdown would let a later commit cover unhandled records**
- **What is wrong:** when an ack fails at record *i*, `handlePartition` commits the prefix up to *i−1*. The client's fetch position has already moved past the whole batch, though. If `Run` kept polling, a later batch on the same partition would commit a higher offset, which also covers records *i…n* that were never handled.
- **Why it is not a bug today:** acks only fail when the `work` context is cancelled or the publisher's `life` has ended, and both happen only after SIGTERM, after which `Run` returns at the next poll. The safety depends on that implicit invariant.
- **Related:** once `work` is cancelled, `a.Wait(work)` sees two ready channels and picks one at random. An already-acked record can therefore be reported as failed. That only causes duplicates.
- **Consequence:** this is a latent data-loss path. A future change that resolves an `Ack` with an error during normal operation, for example a produce timeout, would silently skip triggers.
- **Evidence:** spec §9.2 "Offsets are never committed for a record that is not fully handled".
- **Suggested fix:**
  - When a partition returns a short prefix and the process is not shutting down, rewind the partition to the first unhandled offset with `kc.SetOffsets`, or stop consuming and exit.
  - In `Wait`, check `a.done` first: a non-blocking receive before the `select`.

**F-03 · minor · `adapter/internal/publisher/publisher.go:166-187`: a re-sent produce can reorder versions of a key on a compacted topic**
- **What is wrong:** a failed produce is re-sent 500 ms later as a new record. By then the next record of the same partition, possibly a newer version of the same entity, may already be acknowledged. The broker log then holds v(n+1) followed by vn for that key.
- **Consequence:** `entities.*` are compacted. After compaction, the surviving record for the key is the older vn. A processor that later starts from earliest on a new group would see only vn, and Doris would stay behind datacore until the next change. The window is narrow: franz-go already retries retriable errors internally, so this needs a non-retriable produce error.
- **Evidence:** spec §5.2 (the latest version always arrives); §8.2 compaction.
- **Suggested fix:** treat a failed entity produce as a partition-level failure: do not acknowledge the record, and redo it in offset order, for example as part of F-02's rewind. Or record the trade-off in the decision log.

**F-04 · minor · `adapter/main.go:94-96`: readiness does not drop when partitions are revoked**
- **What is wrong:** `joined` is set by `OnPartitionsAssigned` and cleared only by `OnPartitionsLost`. A cooperative or eager revoke that leaves the member with no partitions keeps `/readyz` at 200.
- **Consequence:** with one adapter this is cosmetic. It becomes misleading if a second adapter instance is ever added.
- **Evidence:** spec §9.8: `/readyz` 200 "when the consumer has joined the group".
- **Suggested fix:** track the assigned partition set and report ready only while it is non-empty, or clear `joined` in `OnPartitionsRevoked` when everything is revoked.

**F-05 · minor · `adapter/internal/fetcher/fetcher.go:138-142`: the "retry escalation" line is one attempt early and reports the wrong delay**
- **What is wrong:** the line is logged after attempt 3 with `next_delay_ms = Nominal(4)` (800 ms). The delay actually slept next is the third fast retry, `Delay(3)` ≈ 400 ms. The schedule only leaves the fast phase after attempt 4.
- **Consequence:** the log is misleading when diagnosing stalls (§9.8 "one per retry escalation"). Behaviour is unaffected.
- **Suggested fix:** log at `attempt == f.Backoff.Fast+1` with `delay`, or keep the line where it is and report `delay`.

### Info

**F-06 · info · `datacore/main.go:168-185` (and `datacore/internal/outbox/relay.go:54`): shutdown details**
- The relay stops before gRPC `GracefulStop`. Admin upserts or a Burst still in flight during shutdown commit outbox rows that are published only on the next start. They are persisted, so nothing is lost, and §7.11's order is satisfied.
- The batch produce is detached from cancellation with a 60 s timeout. If Kafka is unreachable at shutdown, the container is SIGKILLed at the compose stop timeout. The rows stay in the outbox and are republished, which is allowed by §7.5.

**F-07 · info · `datacore/internal/outbox/relay.go:96-106`: `/readyz` reports "relay connected" forever after the first ping.** §7.11 says "has connected", so this is compliant. A later Kafka outage does not show in datacore's readiness.

**F-08 · info · stats semantics (`datacore/internal/store/store.go:93-95`, `datacore/internal/outbox/relay.go:58-60`)**
- `changes_applied` excludes the seed, but `triggers_published` includes the seed triggers and excludes chaos extras.
- Both counters reset on restart. This is consistent with §6.5 ("since start") and A05/A10 depend on neither, but it must be recorded in the decision log (it is listed in plan step 7.5).

**F-09 · info · `adapter/main.go:107`: default gRPC reconnect backoff.** `grpc.NewClient` uses grpc-go's default connection backoff (base 1 s, up to 120 s). After a long datacore outage, the adapter can wait up to 2 minutes before it reconnects, although each `Get*` keeps failing fast as UNAVAILABLE and the partition retries every 5 s. E06 (a sub-second restart) is not affected. Consider `grpc.WithConnectParams(grpc.ConnectParams{Backoff: backoff.Config{BaseDelay: 100ms, Multiplier: 1.6, MaxDelay: 5s}, MinConnectTimeout: 1s})` to match `RETRY_MAX_MS`.

**F-10 · info · the generator also mutates the §5.3 example entities.** This is spec-compliant: §5.4 says the generator mutates seeded entities. Live, after about 5 minutes at the default rate:
- LT-0042 was at v3 in **Sevilla**;
- U-0117 was "**Laura Gil**" in **G-08**;
- G-03 was at v24, renamed.

The §4.3 walkthrough ("moves from Alicante to Madrid") and the README walkthrough in sprint E should not assume the seed values still hold. Sprint E could pause the generator, or say "from its current site".

**F-11 · info · blocked rebalances during unbounded transient retry.** With `BlockRebalanceOnPoll`, a partition stuck in transient retry (datacore down) blocks any rebalance. If one is triggered, for example by a second member, the member is kicked after the rebalance timeout (60 s). Its commit then fails (logged) and the new owner reprocesses. At-least-once holds. This is a documented trade-off of §9.2's option.

**F-12 · info · `.dockerignore` excludes `infra`, `processor`, `explorer`, `web`, `acceptance`, `specs` and `**/*.md`.** This is correct for the root-context Go images. A later image that builds from the root context must adjust it. The build report already says this.

**F-13 · info · decisions that must be recorded (G3).** These are not defects of the code.
- **"Latest at build" pins:**
  - `modernc.org/sqlite` v1.63.0;
  - the builder `golang:1.26.11-bookworm`;
  - the local image tags `adl/datacore:0.1.0` and `adl/adapter:0.1.0`.
- **Kafka:** the fixed `CLUSTER_ID` `97agkXLTQ7GFSS3sN2JpZA`; log dir `/var/lib/kafka/data`; the additions `KAFKA_MIN_INSYNC_REPLICAS` and `KAFKA_DEFAULT_REPLICATION_FACTOR`; `kafka-init` heap 256 MiB.
- **Dragonfly:** the extra flags `--logtostderr --version_check=false`; the `redis-cli ping` health check.
- **datacore storage:** the SQLite schema and the single-connection design.
- **G1, admin rejection of never-created ids:** the admin API rejects LT-9999 and LT-8000 to LT-8999 with `INVALID_ARGUMENT`.
- **Generator details:** a 16-name rename pool; reassign draws uniformly from the seeded users plus "unassigned"; the "different from current" rule; the 1 s cap on owed changes.
- **Stats semantics:** see F-08.
- **Adapter behaviour:** produce retry every 500 ms until shutdown; 7 s shutdown grace; 10 s session timeout; codes outside the §9.4 list classified as transient.
- **Repository:** host-prep moved to `infra/scripts/`; root build context and `.dockerignore`; the extra `adapter/internal/contract` package. The build report cites it as "D-019", which does not exist in the log yet.

## 3. Requirements table (sprint A scope)

| # | Requirement (spec) | Status | Note |
|---|---|---|---|
| 1 | §2 Pinned versions: images `apache/kafka:4.3.2`, `ghcr.io/dragonflydb/dragonfly:v2.1.0`, distroless `static-debian12:nonroot`, builder pinned; Go modules exact (grpc v1.85.0, franz-go v1.22.3, go-redis v9.24.0, sqlite v1.63.0); no `latest` | Met | Decision-log entries pending (F-13) |
| 2 | §7.2 SQLite via modernc at `/data/datacore.db`, WAL, `synchronous=NORMAL`, named volume | Met | `_pragma` journal_mode(WAL), synchronous(NORMAL), busy_timeout; `datacore-data:/data` |
| 3 | §7.2 Stores current state, previous state, outbox (seq monotonic, topic, key, value, created_at) | Met | `entities.prev_state`; `outbox.seq INTEGER PRIMARY KEY AUTOINCREMENT` |
| 4 | §7.2 Persistence across restarts | Met | Live: restart keeps versions; seed not repeated |
| 5 | §7.3 One transaction: read, previous ← current, apply, version+1 (1 on creation), `changed_at` = now (UTC ms), outbox row, commit | Met | `store.Apply`; truncation before storing; unit-tested incl. failure injection before commit |
| 6 | §7.4 / §5.3 Seed deterministic (42), batches, one outbox row per entity, LT-0042 / U-0117 / G-03 fixed, 8/300/3000 (60/30/10), 90 % client assets owned, servers unassigned, OS weights, uniform groups | Met | `NewDataset` with PCG; batches of 500; unit tests check determinism, counts and the example |
| 7 | §7.4 / §7.11 `/readyz` only after seed complete **and** relay connected | Met | `seeded` flag after `markSeedComplete`; `relay.Connected()` after first ping |
| 8 | §5.4 Reserved ids never seeded or generated; LT-9999 and phantoms never created | Met | Seed limits validated; generator uses seeded ids only; admin rejects LT-9999 and LT-8xxx (G1, F-13) |
| 9 | §7.5 Relay: ≤ `OUTBOX_BATCH` rows in seq order, idempotent, acks=all, delete after every ack, rows wait while Kafka is down | Met | `ProduceSync` then `DeleteOutboxThrough(maxSeq)`; single connection makes seq order equal commit order |
| 10 | §7.5 / §7.9 Chaos only adds records: duplicate; malformed with key `malformed:<seq>` (odd → 16–64 random bytes, even → `ENTITY_TYPE_UNSPECIFIED` trigger); phantom LT-8000–8999, v1 | Met | `Relay.Records`; unit tests check rates, alternation and that real triggers are kept |
| 11 | §7.6 Generator: default 10/s, mix 35/20/15/15/10/5, never reserved ids, only seeded users and groups, in-memory config | Met | Unit-tested mix and seeded-only. It also mutates the example entities (F-10, compliant) |
| 12 | §7.7 `Get*`: `get_calls` counted first; UNAVAILABLE chaos before lookup; INVALID_ARGUMENT; NOT_FOUND; stale read only with a previous state inside the window | Met | `grpcapi.get`; live codes verified |
| 13 | §7.7 Chaos never on AdminService | Met | Unit test `TestChaosAppliesOnlyToDataCoreService` |
| 14 | §7.8 Upsert validation: pattern, kind/prefix, required fields → INVALID_ARGUMENT; missing reference → FAILED_PRECONDITION; every call makes a new version | Met | Live and unit-tested |
| 15 | §7.8 `List*` streams ordered by id | Met | `ORDER BY id`, snapshot then stream; tested over gRPC |
| 16 | §7.8 `Set*` ranges (rates 0–1, window 0–10000, rate 0–500) and effective config; Burst 1–10000, synchronous, returns applied | Met | Live: out-of-range values give INVALID_ARGUMENT |
| 17 | §7.8 / §6.5 `GetStats` counters; `outbox_pending` = rows not published | Met | Semantics to record (F-08) |
| 18 | §7.9 Chaos config in memory, reset to env on restart | Met | `chaos.FromEnv` at start |
| 19 | §7.10 Every variable with its default | Met | `datacore/config.go`; invalid values exit with a clear error |
| 20 | §7.11 `/healthz`, `/readyz`, `datacore healthcheck`, reflection, JSON slog with `service=datacore` | Met | |
| 21 | §7.11 SIGTERM order: generator, transaction in progress, relay after its batch, close | Met | Clean exit 0 in 6 ms live (F-06 notes) |
| 22 | §8.1 Broker: KRaft single node, RF 1, min ISR 1, auto-create off, listeners PLAINTEXT/EXTERNAL/CONTROLLER, fixed `CLUSTER_ID`, heap 512 MiB | Met | Verified with `kafka-configs` |
| 23 | §8.2 Topics: partitions, cleanup, retention, compaction settings | Met | Verified with `kafka-topics --describe` |
| 24 | §8.4 `kafka-init` one-shot, `--create --if-not-exists`, idempotent, exit 0 | Met | Re-run exits 0 |
| 25 | §8.3 / §9.6 DLQ: original key and bytes; all seven headers; `error.message` ≤ 1 KB; `failed.at` RFC 3339 UTC; `service=adapter` | Met | Unit-tested; A06–A08 pass |
| 26 | §8.5 `adl-adapter` starts at earliest; commits after publish | Met | `ConsumeResetOffset(AtStart)` |
| 27 | §9.2 Manual commits; sequential per partition; no commit of unhandled records; BlockRebalanceOnPoll | Met | Latent hazard noted in F-02 |
| 28 | §9.3 Cache key `entity:<type>:<id>:v<version>`; TTL 3600; lookup by trigger version; SET under own version before the next record; 100 ms timeouts; errors are misses | Met | A05 passes; unit tests for key, miss on error and timeout |
| 29 | §9.4 Classification table (success, stale, invalid entity, NOT_FOUND, INVALID_ARGUMENT, transient set plus connection errors) | Met | Table-driven test covers every row; unlisted codes are transient (G1) |
| 30 | §9.4 Backoff 100/200/400 ms ±20 %, then `min(2×prev, 5 s)` without limit; stale → STALE_VERSION after 10 s; transient never dead-lettered | Met | Unit-tested; A09 and A10 pass |
| 31 | §9.4 / §9.8 `/readyz` 503 after 30 s of transient retry, `/healthz` stays 200 | Met | Verified live (503 at 30 s) |
| 32 | §9.5 Idempotent producer, acks=all, 5 ms linger; publish or DLQ acked before commit | Met | Unit test `TestCommitOnlyAfterEveryPublishIsAcknowledged`. Ordering caveat on re-sent produces (F-03) |
| 33 | §6.8 Entity key = id, header `trigger.version` = the trigger's version | Met | A02 passes |
| 34 | §6.7 Trigger and entity validation, including key = `<type>:<id>` | Met | `adapter/internal/contract`; A07 passes |
| 35 | §9.7 Every variable with its default | Met | `adapter/config.go` |
| 36 | §9.8 `/healthz`, `/readyz` (joined and not stalled), `adapter healthcheck` on `/healthz` | Partially met | Readiness ignores revokes (F-04) |
| 37 | §9.8 Logs: JSON, one line per dead letter and per retry escalation, fields `service`, `key`, `version`, `outcome`, `attempt` | Met | Escalation line is off by one attempt (F-05) |
| 38 | §9.8 SIGTERM: stop polling, finish batch, commit, **then close** | Not met | `Close()` hangs and the container is SIGKILLed (F-01) |
| 39 | §15.2 Profiles `core`, `depends_on` conditions, restart policies, `name: adl` | Met | kafka-init `restart: "no"`; datacore after kafka-init; adapter after kafka-init, datacore and dragonfly |
| 40 | §15.3 Every service on `adl`; dynamic range leaves .2/.3 free | Met | |
| 41 | §15.4 Limits kafka 1024, dragonfly 512 (flags, memlock −1), datacore 160 and adapter 160 with `GOMEMLIMIT=128MiB` | Met | Usage far below limits |
| 42 | §15.6 Health checks and timings (kafka 60/5/20, dragonfly 10/5/10, datacore 30/5/20, adapter 20/5/10) | Met | All written in compose |
| 43 | §15.7 Volumes writable by their users | Met | Verified ownership live |
| 44 | §15.8 Host ports 29092, 6379, 50051, 8090, 8091 | Met | |
| 45 | §15.9 `build`, `up`, `up-core` (`ICEBERG_ENABLED=false`, `adlctl wait --core`), `host-prep` | Met | `up-core` reached ready in about 35 s |
| 46 | §15.11 Multi-stage, BuildKit cache mounts, `CGO_ENABLED=0`, distroless nonroot | Met | |
| 47 | §16.2 datacore unit tests: version +1 with outbox in the same transaction; seed deterministic and example values; every chaos knob's rate with a seeded source; admin validation incl. references and kind/prefix | Met | They test what the spec says (4-σ rate tolerance; failure injection before commit) |
| 48 | §16.2 adapter unit tests: trigger validation; classification table; backoff timings; cache key; cache error is a miss; commit only after ack (fakes); DLQ headers | Met | No test covers the shutdown `AllowRebalance` path (F-01) |
| 49 | §17 Layout and layering (`internal/{store,outbox,generator,chaos,grpcapi}`, `internal/{consumer,fetcher,cache,publisher}`), no single-file directories, English, JSON logs with `service` | Met | Extra packages `model`, `handler`, `contract` (to record, F-13) |
| 50 | Later gates (E01, E02, E05, E06) not put at risk by this sprint | Met | E01: chaos DLQ reasons and keys match (malformed → DECODE/VALIDATION with `malformed:` keys; phantoms → NOT_FOUND `asset:LT-8xxx`); stale reads cannot reach STALE_VERSION with a 300 ms window. E06: persistence verified. E02/E04: F-01 adds about 10 s per adapter stop but loses nothing |

## 4. Resolution (added by the builder after the audit)

| Finding | Action |
|---|---|
| F-01 (major) | Fixed. `adapter/main.go` closes with `kc.CloseAllowingRebalance()`, and `Consumer.Run` always calls `AllowRebalance()` on return. Unit tests now assert `AllowRebalance` on both shutdown paths. Live: `docker compose stop adapter` took 0.25 s with exit 0, and the log shows "partitions revoked" then "adapter stopped". |
| F-02 (minor) | Fixed. `processBatch` reports whether every record was acknowledged. Outside shutdown, an incomplete batch makes `Run` return `ErrIncomplete` before any later poll, and the adapter exits so it restarts from the last committed offset. `Ack.Wait` checks `done` first, so an acknowledged record never reports failure. Both behaviours are unit-tested. |
| F-03 (minor) | Fixed. The publisher no longer re-sends failed produces. A failed ack goes through the F-02 path, so the record is redone in offset order. Unit test `TestFailedProduceFailsTheAckWithoutResending`. |
| F-04 (minor) | Fixed. Readiness tracks the assigned partition set, adding on assign and removing on revoke or loss. |
| F-05 (minor) | Fixed. "retry escalation" is logged at attempt `FAST_RETRIES + 1` with the delay actually slept. |
| F-09 (info) | Applied. gRPC connect backoff is 100 ms × 1.6 up to `RETRY_MAX_MS`. |
| F-06, F-07, F-08, F-10, F-11, F-12 | No code change. Recorded in D-017, D-020, D-021, D-022 and D-023. F-10 is noted for sprint E in `build-progress.md`. |
| F-13 | Decision-log entries D-014 to D-024 written. |

After the fixes, the full gate (`make build && make clean && make up-core && make test lint && make acceptance-gate GATE=A`) and the sprint 0 gate were re-run and are green.
