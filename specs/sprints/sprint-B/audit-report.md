# Sprint B audit report

Auditor: an independent subagent that did not build the sprint. Date: 2026-11-25.
Scope: `git diff sprint-A-done..HEAD` (six commits, `8e0a789` to `c3c46f9`, 38 files): `infra/doris/*`, `infra/flink/*`, `infra/scripts/doris-init.sh`, `processor/**`, `docker-compose.yml`, `Makefile`, `adlctl/reconcile.go` and `adlctl/main_test.go`, plus the sprint's plan, build report and decision-log entries D-025 to D-033. (While I worked, `specs/build-progress.md` showed as modified in the working tree. I did not touch it.)
Read in full: spec §1–§18 and Appendix A, with extra attention on §3, §10, §11 (except §11.6), §15, §16.2, §16.4 gate B and §17; sprints plan §3 and §4; `specs/sprints/sprint-B/sprint-plan.md` and `build-report.md`; `specs/decision-log.md` (D-025 to D-033); `specs/open-questions.md`; and the sprint A audit report, for the format. I read all production code line by line: the Java job, `submit-job.sh`, `doris-init.sh`, the Dockerfiles, the compose and Makefile diffs, and the adlctl change. I read the unit tests against §16.2. I also inspected the live stack: the Flink REST API, Doris over MySQL, container configuration files, cgroup memory and logs.

## 1. Summary

Sprint B works. The gate is green from a clean state, and so is the sprint 0 gate:
- gate A passes 10/10;
- gate B passes 9/9 in 302 s, with a B09 p95 of 10.2 s;
- `make reconcile` passes, with the lake skipped as D-032 describes.

Exactly-once into Doris is built correctly:
- 2PC is enabled explicitly on all four sinks, with JSON lines;
- the label prefix is `adl-<run-id>-<table>` (live: `adl-20261125204448-assets_inventory_assets_0_<chk>`);
- the run id is new on a fresh start and reused on restore. I checked this independently: `make down` wrote `savepoint-54c1fc-…`; `make up-core` submitted a new job that restored from that savepoint (`is_savepoint=true`) with the same run id; no "Label Already Exists" appeared; reconcile was green and `change_rate_1m` had no duplicate pairs.

The submitter picks the restore point correctly. I tested it with fabricated directories: a newer `chk-N` beats an older savepoint, using sub-second mtimes.

The rest also checks out:
- operator names and uids follow §10.10;
- watermarks follow §10.2, VersionGate follows §10.5, the DLQ headers follow §8.3 and the JSON rows follow Appendix A;
- the Doris overrides of §11.2 and the init SQL of §11.3–§11.5 and §11.7 match the spec;
- memory limits, health checks, ports, static IPs and volume ownership follow §15.

There are **no critical findings** and **two major findings**:
- **F-01 (major):** the job runs at **parallelism 1**, not the cluster's `parallelism.default: 2` (§10.9). Flink's `POST /jars/{id}/run` handler falls back to parallelism 1 when the request does not set it, so the cluster setting is silently ignored. The deviation is not recorded anywhere.
- **F-02 (major, latent; it surfaces when F-01 is fixed):** the submitter waits for a TaskManager with **free slots** before it checks whether the job already runs. At parallelism 2 the job holds both slots, so re-running `make up-core` or `make up` on a live stack would wait 300 s and exit 1. That breaks "submission is idempotent" (§10.11) and "re-running make up is safe" (§11.8). Today it works only because parallelism 1 leaves one slot free.

The other findings are minor or informational.

### 1.1 Gate results (re-run exactly as written, sequentially)

Sprint B gate, starting from the stack I found running:

```
$ make build && make clean && make up-core      -> exit 0 (50 s in total; images cached)
    wait: ready — adapter, datacore, doris-be, doris-fe, dragonfly, flink-jobmanager, kafka healthy;
    doris-init, flink-job, kafka-init completed; flink-taskmanager running;
    "doris-be (SHOW BACKENDS) 172.30.0.3 alive=true"; "flink job adl-processor RUNNING"
$ make test lint                                -> exit 0
    go test ./...                               -> every package ok
    mvn test (maven:3.9.16-eclipse-temurin-17)  -> Tests run: 25, Failures: 0, Errors: 0, BUILD SUCCESS
    buf lint, buf breaking against main         -> ok
    golangci-lint v2.15.0                       -> 0 issues.
    mvn spotless:check                          -> BUILD SUCCESS (21 files clean)
    explorer, web                               -> "not present yet, skipped"
$ make acceptance-gate GATE=A                   -> exit 0, 10/10 PASS (15.2 s)
$ make acceptance-gate GATE=B                   -> exit 0 (301.8 s)
    --- PASS: TestGateB_01_SeedInDoris (2.42s)
    --- PASS: TestGateB_02_ChangeVisible (8.24s)
    --- PASS: TestGateB_03_OlderVersionIgnored (40.31s)
    --- PASS: TestGateB_04_DuplicatesDropped (21.42s)
    --- PASS: TestGateB_05_ProcessorDlq (8.24s)
    --- PASS: TestGateB_06_EnrichedView (10.69s)
    --- PASS: TestGateB_07_MaterializedViewConsistent (11.74s)
    --- PASS: TestGateB_08_ChangeRateRows (0.06s)
    --- PASS: TestGateB_09_Freshness (198.07s)   p95 10.17 s, max 10.20 s
$ make reconcile                                -> exit 0
    "reconcile: lake comparison skipped: the lake profile is not running (core-only mode …)"
    "reconcile: PASS (checked assets=3012 groups=10 users=302)", every category 0
```

Sprint 0 gate:

```
$ go run …buf@v1.75.0 lint && go run …buf@v1.75.0 build && go vet ./... && go build ./... \
  && go vet -tags acceptance ./acceptance/... && test "$(grep … | wc -l)" -eq 38 && go test ./...
-> exit 0
```

### 1.2 Extra live checks (not part of the gate)

| Check | Result |
|---|---|
| Effective Flink cluster configuration (`GET /jobmanager/config`) | Every §10.9 key is present with the spec value, including `parallelism.default=2`. There are no deprecation warnings in the JobManager log |
| Job parallelism (`GET /jobs/<id>`, `/jobs/<id>/config`) | **`job-parallelism: 1`**; all three vertices `p=1`; the TaskManager has `freeSlots: 1` of 2 (F-01) |
| Checkpoint config (`/jobs/<id>/checkpoints/config`) | `exactly_once`, 10 s interval, 2 min timeout, 2 s min pause, 1 concurrent, retained on cancellation, `HashMapStateBackend`, filesystem storage |
| Label prefixes (TaskManager log) | `labelPrefix adl-20261125204448-assets`, …`-change_rate_1m`. The connector aborts lingering transactions on start ("abort for labelPrefix …, start chkId 1") |
| `make down` then `make up-core` (E04-like) | Savepoint written. The submitter logged "restoring from /flink-data/savepoints/savepoint-54c1fc-… with label run id 20261125204448". `latest.restored = {id 72, is_savepoint true}`. 0 "Label Already Exists" lines. Reconcile PASS. 0 duplicate `change_rate_1m` pairs |
| Restore-point selection (fabricated tree in the submitter image) | Sub-second mtimes sort correctly. `checkpoints/j1/chk-6` (10:05:00.9) beats `chk-5` (10:05:00.5) and `savepoint-b` (10:03), and a savepoint dir without `_metadata` is ignored |
| Idempotency | Re-running `doris-init` exits 0 ("applied /opt/adl/init-core.sql"). Re-running `flink-job` exits 0 ("already present (… RUNNING); nothing to submit") |
| Doris config in the containers | FE `-Xmx1280m -Xms1280m`, `arrow_flight_sql_port = 8070`, `priority_networks = 172.30.0.0/24`; BE `-Xmx512m`, `arrow_flight_sql_port = 8050`, `mem_limit = 2048M` |
| Doris SQL state | `time_zone` UTC (global). DDL equals §11.3. MV `NORMAL`. `explorer` has `Select_priv` on `internal.inventory` |
| `change_rate_1m` | 9 rows per type; every row has a 60 s window and `changes > 0`; no duplicate pairs |
| `docker stats` (core, default load, about 10 min after start) | doris-fe **1.676 GiB / 1.75 GiB** (F-03), doris-be 1.27 / 3.25 GiB, TM 799 MiB / 2 GiB, JM 538 MiB / 1.25 GiB, kafka 498 MiB / 1 GiB, dragonfly 46 MiB, datacore 14 MiB, adapter 9 MiB |
| Logs | JM, TM and the submitter log JSON lines with `service` (`flink-jobmanager`, `flink-taskmanager`, `flink-job`), and so does `doris-init`. The Doris FE and BE logs are the image's own format |

## 2. Findings

### F-01 · major · The job runs at parallelism 1, not `parallelism.default: 2`

- **What:** spec §10.9 sets `parallelism.default: 2` and `taskmanager.numberOfTaskSlots: 2`. The cluster has the key, but the submitted job does not use it.
  - Flink's `JarRunHandler` takes the parallelism from the run request. When the request does not set it, the handler falls back to the option's built-in default, which is 1, not to the cluster's configured value.
  - `submit-job.sh` sends no `parallelism`, and `ProcessorJob` never sets one, so every operator runs at 1. Half the TaskManager's slots stay idle.
  - R9 lists "lower Flink parallelism to 1" as the second step of the OOM ladder. Parallelism 1 is therefore a fallback, taken here without an OOM, without a G4 attempt and without a decision-log entry.
  - The build report says every §10.9 key was loaded, which is true for the cluster. It did not check the job's effective parallelism.
- **Evidence:**
  - `curl localhost:8081/jobmanager/config` → `parallelism.default=2`;
  - `curl localhost:8081/jobs/<jid>/config` → `"job-parallelism":1`;
  - `/jobs/<jid>` vertices: `Source: EntitiesSource -> DecodeValidate -> (DlqSink…, Watermarks) p=1`, `VersionGate -> (…) p=1` and `ChangeRateWindow -> … p=1`;
  - `/taskmanagers` → `slotsNumber 2, freeSlots 1`;
  - `infra/flink/submit-job.sh:105-107`: the run body has only `entryClass`, `programArgsList`, `allowNonRestoredState` and `savepointPath`;
  - `grep -ri parallelism specs/decision-log.md` → nothing.
- **Spec:** §10.9, §3 R9, G3.
- **Fix:** make the submitter pass the cluster value explicitly. For example:
  - read it with `curl -sf $FLINK_REST/jobmanager/config | jq -r '.[] | select(.key=="parallelism.default") | .value'`;
  - add `parallelism: <n>` to the `/run` body;
  - or set it from a `PARALLELISM` environment variable that defaults to the same value.

  Then fix F-02 in the same change, re-run gate B and the E03/E04-like checks, and record the decision. If memory turns out to be the reason to stay at 1, go through R9 and record it.

### F-02 · major (latent) · The submitter requires free slots before its idempotency check

- **What:** `submit-job.sh` step 1 (lines 39–42) loops until a TaskManager reports `freeSlots > 0`, for up to 300 s. Only after that does step 2 (lines 46–51) check whether `adl-processor` is already CREATED, RUNNING or RESTARTING.
  - With the spec's parallelism of 2 (F-01), the running job occupies both slots of the only TaskManager.
  - Any later run of `flink-job` (`make up-core` or `make up` on a live stack, which re-runs one-shot containers) would loop for 300 s, log "no TaskManager with free slots", and exit 1. `adlctl wait` then reports `flink-job` as failed.
  - The step order follows §10.11 literally, but those two steps conflict once all slots are in use. The idempotency of §10.11 step 2 and §11.8 is the intent.
  - It is invisible today only because of F-01.
- **Evidence:** the code order in `infra/flink/submit-job.sh:34-51`. The live re-run succeeded only with `freeSlots: 1` ("JobManager and a TaskManager with free slots are available", then "already present … nothing to submit").
- **Spec:** §10.11 steps 1–2, §11.8, G1.
- **Fix:** after the REST API answers, query `/jobs/overview` first and exit 0 if the job is active. Wait for a TaskManager with free slots only when a submission is actually needed. Record the reordering as a G1 decision.

### F-03 · minor · Doris FE runs at about 95% of its container limit

- **What:** the FE's anonymous memory is 1.66 GiB of its 1.75 GiB limit after about 10 minutes, and it creeps upward (1,783,816,192 → 1,786,777,600 bytes in 60 s).
  - The heap is fully committed (`-Xms1280m`), and native memory (metaspace, code cache, threads, direct buffers) adds about 400 MiB.
  - Java's default `MaxDirectMemorySize` equals `-Xmx`, so Arrow Flight SQL buffers in sprint D, plus the Iceberg catalog metadata in sprint C, could push the FE into an OOM kill.
  - The build report's `docker stats` (1.19 GiB) was taken early and understates this.
  - No OOM happened: `memory.events oom_kill 0`, `OOMKilled=false`, `RestartCount 0`.
- **Evidence:** `/sys/fs/cgroup/memory.stat` in `doris-fe`: `anon 1783816192`; `memory.max 1879048192`. `docker stats`: `adl-doris-fe-1 1.676GiB / 1.75GiB`.
- **Spec:** §15.4, §3 R9, §18 item 7.
- **Fix:**
  - Record the real steady-state figure in the build report or decision log, and keep watching it in sprints C and D.
  - Optionally cap native memory inside the same budget without touching the spec's heap, for example `-XX:MaxDirectMemorySize=256m` and a bounded `-XX:ReservedCodeCacheSize`, recorded as a decision.
  - On an OOM kill, follow R9 in order.

### F-04 · minor · D-032 treats a stopped or crashed `iceberg-rest` as core-only mode

- **What:** `adlctl/reconcile.go` `lakeRunning` returns false whenever no `iceberg-rest` container is `running`, and the test case "lake stopped" (`State: exited`) makes this explicit.
  - In full mode (sprints C–E), a crashed or stopped catalog would therefore make `make reconcile` skip the lake and **pass**. The only trace would be a line on stderr.
  - `make reconcile` is part of the gates of sprints C and E and of DoD items 4 and 5.
  - The acceptance tests are not affected, because they call `labkit.Reconcile` with explicit options.
  - The decision itself (spec §15.2 core-only mode against §15.9 and §16.5's "unless `--no-lake`") is a reasonable G5/G1 resolution of the plan's gate B. The problem is only the over-broad trigger.
- **Evidence:** `adlctl/reconcile.go:46-75`; `adlctl/main_test.go` case `{"lake stopped", …"exited"…, false}`.
- **Spec:** §16.5 step 4, §15.10, G1 (keep the §16 tests meaningful).
- **Fix:**
  - Skip only when the stack has **no** container of the lake profile at all, meaning it was never created (core-only mode).
  - If an `iceberg-rest` container exists in any state other than `running`, compare the lake, so the failure is visible.
  - Update `TestLakeRunning` and D-032.

### F-05 · minor · Submitter robustness gaps under `set -euo pipefail`

- **What:**
  - (a) `submit-job.sh:46`: if `GET /jobs/overview` fails transiently, `set -e` kills the script with exit 1 and **no log line**, because the assignment fails inside the command substitution.
  - (b) `:39`: when `/taskmanagers` fails, `[ "" -gt 0 ]` prints `integer expression expected` to stderr. That is a non-JSON line, although the loop survives.
  - (c) `rest` uses `--max-time 30` for the jar upload and for `/run`. `/run` executes `main()` synchronously on the JobManager. In sprint C the jar grows with Iceberg, AWS and Hadoop, and `main()` creates the namespace and table over the network, so 30 s on the reference Intel Mac can be too short. curl would then time out and the container would exit 1 while the JobManager still starts the job.
  - (d) Savepoint pruning (`:137-141`) ranks every directory under `savepoints/` by **directory** mtime, including incomplete ones without `_metadata`. Failed `make down` savepoints can therefore push valid ones out of the newest three.
- **Evidence:** the lines cited.
- **Spec:** §10.11 steps 1, 2, 5 and 7; §17.2 (JSON logs).
- **Fix:**
  - (a, b) wrap both calls (`… || true`) and log a JSON warning before retrying or failing;
  - (c) use a longer timeout for `/jars/upload` and `/run` (for example 300 s), or on a timeout fall through to step 6 and look the job up by name;
  - (d) prune only directories that contain `_metadata`, ranked by the mtime of `_metadata`, and delete incomplete directories separately.

### F-06 · minor · Unpinned BuildKit frontend in `processor/Dockerfile`

- **What:** `# syntax=docker/dockerfile:1` pulls a floating frontend tag at build time, which goes against "pin exact versions, never latest" (CLAUDE.md, spec §2.1). It is also unnecessary: the Go Dockerfiles use `RUN --mount=type=cache` without any syntax line.
- **Evidence:** `processor/Dockerfile:1`.
- **Spec:** §2.1.
- **Fix:** remove the line, or pin an exact frontend version (for example `docker/dockerfile:1.x.y`) and record it.

### F-07 · info · Repeated metric-collision warnings in the TaskManager log

- **What:** every checkpoint logs one WARN per 2PC sink: "Name collision: Group already contains a Metric with the name 'pendingCommittables'" for DorisAssets, DorisUsers, DorisGroups, DorisChangeRate and DlqSink. That is 16 lines per sink after 17 checkpoints, about 43,000 WARN lines a day at the 10 s interval, which buries real warnings. The build report calls it benign, which is true functionally.
- **Fix (optional):** raise the level of the emitting logger for that message in `infra/flink/log4j-console.properties`, and record it.

### F-08 · info · The RowData mapping unit test is deferred to sprint C

§16.2 requires a RowData mapping test. D-028 moves it to sprint C together with the Iceberg branch. That is consistent with plan §4 ("the Iceberg branch MAY already exist"), but sprint C must add it.

### F-09 · info · D-029 (fail fast on `--iceberg.enabled=true`) is consistent and safe

Failing loudly instead of silently running without the lake keeps §4 and the gate C tests meaningful (G1), and `make up` is not part of the sprint B gate.
- **Sprint C must:** remove the check in `ProcessorJob.main`; add the `iceberg-rest` and `rustfs-init` dependencies with `required: false` to `flink-job` (§15.2); and use `make clean` when switching modes (§15.2).
- The current `ICEBERG_ENABLED: ${ICEBERG_ENABLED:-true}` with `make up-core` exporting `false` is correct.

### F-10 · info · The submitter's "active" states omit Flink's INITIALIZING

§10.11 step 2 lists CREATED, RUNNING and RESTARTING, and the script follows it. Flink 2.x also reports INITIALIZING right after submission, and FAILING or CANCELLING during transitions. A second submitter that runs at that moment could submit a duplicate job. The risk is low, because `flink-job` is a one-shot started once per `make up`. Optionally treat INITIALIZING as active and record it.

### F-11 · info · Unit tests meet §16.2 but leave small gaps

- VersionGate tests cover "newer passes; equal and older dropped; per key" plus snapshot and restore, but do not assert the `versionGateAccepted` and `versionGateDropped` counters.
- The window test (MiniCluster) does not cover the late-record drop (allowed lateness 0).
- The deserializer has no test for an unknown topic.

All of these are optional.

### F-12 · info · Checkpoints of old job ids are never pruned

Each submission leaves three `chk-N` directories under `/flink-data/checkpoints/<old-jid>/`, plus `shared` and `taskowned`. The spec only requires pruning savepoints, and the restore choice stays correct because it ranks by `_metadata` mtime. Disk use grows slowly across `make down` and `make up` cycles. Optional cleanup in the submitter.

### F-13 · info · `OffsetResetStrategy` is deprecated in kafka-clients 4.2.0

`ProcessorJob.java:80` uses `OffsetResetStrategy.EARLIEST`. `javap` on `kafka-clients-4.2.0.jar` shows `Deprecated: true`. The code still compiles and is still accepted by connector 5.0.0. This is not a Flink idiom issue (R17). Keep an eye on it if a connector upgrade removes the overload.

## 3. Requirements table (sprint B scope)

| # | Requirement (spec) | Status | Note |
|---|---|---|---|
| 1 | §2.3/§2.4 Pins: `flink:2.1.3-java17`, `apache/doris:fe-4.1.5` and `be-4.1.5`, Flink 2.1.3 `provided`, kafka connector `5.0.0-2.1`, doris connector `flink-doris-connector-2.1:26.3.0`, `protobuf-java` 4.36.2 matching buf; builder and helper images pinned (D-025) | Met | `# syntax=docker/dockerfile:1` is floating (F-06) |
| 2 | §3 R1 No fallback set needed; no minor-version mixing | Met | |
| 3 | §3 R2 FE and BE as separate containers, election mode, BE at `172.30.0.3`, redirect works, benodes option available | Met | `SHOW BACKENDS` shows `172.30.0.3 alive=true`; `--doris.benodes` sets auto-redirect false |
| 4 | §3 R3 `nofile` 65536, host-prep, no checks disabled | Met | `SKIP_CHECK_ULIMIT` is the image's own (D-026) |
| 5 | §3 R5 / §10.6 Stream Load `format=json`, `read_json_by_line=true` | Met | `DorisSinks.streamLoadProperties`; unit-tested |
| 6 | §3 R11 Shaded jar, merged `META-INF/services` | Met | `ServicesResourceTransformer`; no relocation needed |
| 7 | §3 R12 `KafkaSource` and `KafkaSink` only | Met | |
| 8 | §3 R14 2PC never disabled; new run id on fresh start, reused on restore | Met | Verified live through `make down` and `make up-core` |
| 9 | §3 R17 Flink 2.x APIs (Source/Sink V2, `Duration`, `open(OpenContext)`, `config.yaml` keys `execution.checkpointing.*`) | Met | `org.apache.flink.util.ParameterTool`, `TumblingEventTimeWindows.of(Duration)`, `env.fromData` in tests |
| 10 | §10.1 Job `adl-processor`, DataStream API, the topology | Met | The Iceberg branch is absent by design (D-029) |
| 11 | §10.2 One `KafkaSource` on the three topics, group `adl-processor`, committed offsets then earliest, commit on checkpoint; the deserializer never throws; no source watermarks; watermarks after decoding with 5 s bounded out-of-orderness and 30 s idleness | Met | |
| 12 | §10.3 / §6.7 Topic decides the type; validation rules; key equals id; invalid envelope keeps raw key and value, reason and coordinates | Met | `EntityDecoder`; unit-tested for every rule |
| 13 | §10.4 / §8.3 Side output to `KafkaSink` `dlq.processor`, `AT_LEAST_ONCE`, seven headers, `service=processor`, only DECODE_ERROR and VALIDATION_ERROR, message ≤ 1 KB, a malformed message never fails the job | Met | B05 passes; `DlqSerializerTest` |
| 14 | §10.5 VersionGate: `KeyedProcessFunction` on `<type>:<id>`, `ValueState<Long> lastVersion`, no TTL, `>` passes, counters `versionGateAccepted` and `versionGateDropped` | Met | B03 and B04 pass |
| 15 | §10.6 Four `DorisSink`s: fenodes, `inventory.<table>`, root with empty password, 2PC explicit, deletable false, label prefix `adl-<run-id>-<table>`, `SimpleStringSerializer` | Met | |
| 16 | §10.6 / Appendix A JSON rows: column names, `yyyy-MM-dd HH:mm:ss.SSS` UTC, empty refs → null, kind names, `ingested_at` at mapping time | Met | `DorisRowsTest` matches Appendix A items 5 and 7 |
| 17 | §10.6 Run id `yyyyMMddHHmmss` UTC, saved in `/flink-data/label-run-id`, passed as `--label-run-id` | Met | `JobOptions` rejects anything else |
| 18 | §10.7 Iceberg sink | N/A (sprint C) | The MAY in plan §4; fails fast with `true` (F-09) |
| 19 | §10.8 Change-rate window: entity type, keyBy, 1-minute tumbling event-time window, `AggregateFunction` plus `ProcessWindowFunction`, allowed lateness 0, row shape | Met | B08 passes; no duplicate pairs live |
| 20 | §10.9 Cluster configuration through `FLINK_PROPERTIES` on JM and TM, keys valid for 2.1 | **Partially met** | Every key is effective on the cluster, but the job runs at parallelism 1 instead of 2 (F-01) |
| 21 | §10.9 / §15.7 `/flink-data` shared named volume, writable by uid 9999 | Met | `flink:flink` ownership live |
| 22 | §10.10 Operator names and uids | Met | `ProcessorJobTest`; live vertex names match |
| 23 | §10.11 step 1 Wait for the REST API and a TaskManager with free slots | Met | Ordering conflicts with step 2 at full parallelism (F-02) |
| 24 | §10.11 step 2 Idempotent: exit 0 if the job is CREATED, RUNNING or RESTARTING | **Partially met** | Works only while a slot is free (F-02); INITIALIZING not covered (F-10) |
| 25 | §10.11 step 3 Newest `_metadata` among savepoints and `chk-N`; never an older savepoint over a newer checkpoint | Met | Verified with a fabricated tree and live |
| 26 | §10.11 step 4 Run id read on restore, new on fresh start | Met | Missing file on restore → new id with a WARN (sensible G1) |
| 27 | §10.11 step 5 Upload, run with `entryClass`, `programArgsList`, `savepointPath`, `allowNonRestoredState=false` | Met | Timeouts may be short in sprint C (F-05 c) |
| 28 | §10.11 step 6 Wait for RUNNING up to 5 min, print exceptions, exit 1 | Met | |
| 29 | §10.11 step 7 Keep the three newest savepoints | Met | Incomplete directories are counted (F-05 d) |
| 30 | §10.11 Graceful stop: `make down` → `adlctl flink stop-with-savepoint` (`drain: false`, 3 min, warn and stop anyway) | Met | Verified live |
| 31 | §10.12 Program arguments and defaults | Met | Empty values left out so the defaults apply (D-030) |
| 32 | §11.1 FE `172.30.0.2` and BE `172.30.0.3`, election mode, `priority_networks` from the entrypoint, derived images that leave the conf writable | Met | |
| 33 | §11.2 Overrides: FE `arrow_flight_sql_port` 8070, heap 1280m; BE `arrow_flight_sql_port` 8050, `mem_limit` 2048m, JVM heap 512m | Met | Verified in the containers; BE log "Mem Limit: 2.00 GB" |
| 34 | §11.3 Database and four tables verbatim | Met | `SHOW CREATE TABLE` matches |
| 35 | §11.4 View `v_assets_enriched` | Met | B06 passes |
| 36 | §11.5 Async MV every minute (R13 not needed) | Met | B07 passes; MV state NORMAL |
| 37 | §11.7 Global UTC, `explorer` user, SELECT on `internal.inventory.*`, secrets from `.env` substituted | Met | The lake grant belongs to sprint C (`init-lake.sql`) |
| 38 | §11.8 `doris-init`: wait for `Alive=true`, run `init-core.sql`, exit 0, idempotent | Met | Re-run exits 0 |
| 39 | §15.2 Services, profile `core`, `depends_on` and restart policies; `make up-core` sets `ICEBERG_ENABLED=false` | Met | `flink-job`'s lake dependencies with `required: false` come in sprint C (F-09) |
| 40 | §15.3 Every service on `adl`; static IPs outside `ip_range` | Met | |
| 41 | §15.4 Limits: JM 1280M, TM 2048M, FE 1792M, BE 3328M; total within budget | Met | FE uses about 95% of its limit (F-03) |
| 42 | §15.6 Health checks: FE `/api/bootstrap` 180/10/30, BE `/api/health` 180/10/30, JM `/overview` 90/5/30; no TM check | Met | The FE check also requires `"code":0` (D-026) |
| 43 | §15.7 Volumes `doris-fe-meta`, `doris-be-storage` and `flink-data`, mounted where the spec says, writable | Met | |
| 44 | §15.8 Host ports: Doris only 8030 and 9030; Flink 8081; Flight SQL and BE HTTP internal | Met | |
| 45 | §15.9 `test` and `lint` run Maven tests and the Spotless check in a builder container with a cache volume; `down` uses stop-with-savepoint | Met | D-033 |
| 46 | §15.10 `adlctl wait` checks the BE and the job; `reconcile` | Met | Lake auto-skip trigger too broad (F-04) |
| 47 | §15.11 Multi-stage, BuildKit cache mount for Maven | Met | |
| 48 | §16.2 processor unit tests: VersionGate with the harness; deserializer bad bytes and key mismatch without throwing; JSON rows; window counting | Met | 25 tests; RowData deferred to sprint C (F-08); small gaps (F-11) |
| 49 | §16.6 CI `java` job (`mvn verify` with Spotless bound to `verify`); `images` job builds `infra/doris/{fe,be}.Dockerfile`, `infra/flink/Dockerfile` and `processor/Dockerfile` | Met | Reproduced the `detect` matrix locally: contexts and file paths are right. JM and TM build the same image twice (harmless) |
| 50 | §17 Layout (`infra/doris`, `infra/flink`, `infra/scripts`, `processor/`), no single-file directories (Java packages excepted), English, JSON logs with `service`, no unreferenced TODO or FIXME | Met | |
| 51 | G3 Decision log for every rule applied and every "latest at build" pin | Partially met | Parallelism 1 is not recorded (F-01) |
| 52 | Later gates not put at risk (C: D-029 removal, `make reconcile` with the lake; E03 and E04 restore paths) | Met with notes | F-02 (after F-01), F-04 and F-05 (c) affect sprint C and later; E03 and E04 behaviour verified |

## 4. Gate summary

| Gate step | Result |
|---|---|
| `make build && make clean && make up-core` | Green ("wait: ready", 50 s in total with cached images) |
| `make test lint` | Green (Go ok; Maven 25/25; buf; golangci-lint 0 issues; Spotless clean) |
| `make acceptance-gate GATE=A` | Green, 10/10 |
| `make acceptance-gate GATE=B` | Green, 9/9 (B09 p95 10.17 s, max 10.20 s) |
| `make reconcile` | Green (lake skipped in core-only mode, D-032) |
| Sprint 0 gate (buf lint and build, vet, build, acceptance vet, 38 cases, `go test`) | Green |

The stack is left running in core-only mode, restored from the audit's savepoint. Its label run id is still `20261125204448`.

## 5. Fix status (written by the builder after the audit)

| Finding | Severity | Status |
|---|---|---|
| F-01 Job at parallelism 1 | Major | **Fixed.** The submitter reads `parallelism.default` from `/jobmanager/config` and passes it as `parallelism` to `POST /jars/{id}/run`. Live: every vertex runs at parallelism 2 (D-034) |
| F-02 Free-slot wait before the idempotency check | Major | **Fixed.** The order is now: JobManager, then the active-job check, then a wait for at least `parallelism` free slots. Live: `make up-core` on a running stack logs "already present … nothing to submit" and exits 0 in 19 s |
| F-03 FE close to its memory limit | Minor | **Recorded, not changed.** RSS is about 1.58 GiB of 1.75 GiB (1792 MiB): the §11.2 heap of 1280m committed up front (`-Xms` = `-Xmx`) plus non-heap memory. There has been no OOM kill. On an OOM kill, R9 applies: lower the FE heap within its budget and record it. Added to build-progress as a risk for sprints C and D |
| F-04 Lake skip on a crashed `iceberg-rest` | Minor | **Fixed.** The lake is skipped only when no `iceberg-rest` container exists. A stopped or crashed one means the lake is compared (`TestLakeCreated`, D-032 updated by D-034) |
| F-05 Submitter robustness | Minor | **Fixed.** (a, b) `/jobs/overview` and `/taskmanagers` failures are retried with JSON log lines. (c) Upload and run use a 300 s timeout. (d) Pruning ranks complete savepoints by the mtime of `_metadata` and deletes incomplete directories |
| F-06 Unpinned BuildKit frontend | Minor | **Fixed.** The `# syntax` line is removed |
| F-07 Metric-collision log noise | Info | **Fixed.** The logger `org.apache.flink.metrics.MetricGroup` is set to ERROR in `log4j-console.properties` (D-034) |
| F-08 RowData test deferred | Info | Unchanged; it moves to sprint C (build-progress) |
| F-09 D-029 is consistent | Info | No change needed |
| F-10 INITIALIZING not treated as active | Info | **Fixed.** INITIALIZING counts as present |
| F-11 Small unit-test gaps | Info | Unchanged |
| F-12 Old checkpoints not pruned | Info | Unchanged; §10.11 only prunes savepoints |
| F-13 `OffsetResetStrategy` deprecated | Info | Unchanged; it is the signature that connector 5.0.0-2.1 exposes |

After the fixes, the full gate was re-run from `make build && make clean && make up-core`. The results are in the build report.
