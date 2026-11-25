# Sprint C audit report

Auditor: an independent subagent that did not build the sprint. Date: 2026-11-25.
Scope: `git diff sprint-B-done..HEAD` (four commits, `d5532c9` to `97b0424`, 16 files): `docker-compose.yml`, `infra/scripts/create-bucket.sh`, `infra/scripts/doris-init.sh`, `infra/doris/init-lake.sql`, `processor/pom.xml`, `ProcessorJob.java`, the new package `processor/.../lake/` (`LakeTable`, `LakeCatalog`, `LakeRows`) and its tests, plus the sprint's plan, build report and decision-log entries D-035 to D-038.
Read in full: spec §1–§18 and Appendix A, with extra attention on §3 (R6, R7, R8, R9, R17, R18, G1–G5), §10.1, §10.7, §10.10–§10.12, §11.6–§11.8, §12, §15, §16.2–§16.5 and gate C; sprints plan §3 and §4; `specs/sprints/sprint-C/sprint-plan.md` and `build-report.md`; `specs/decision-log.md` (D-035 to D-038); `specs/open-questions.md`; and the sprint B audit report, for the format. I read every changed production file and test line by line. I then inspected the live stack: the REST catalog, the bucket over the S3 API, Doris over MySQL, the Flink REST API, container logs and cgroup memory. I also ran three state-changing probes (idempotent `make up`, a TaskManager kill mid-burst, and `make down` / `make up`). I left the stack running in full mode, healthy, with the generator running.

## 1. Summary

Sprint C works. The gate is green from a clean state, and so are the sprint 0 checks:
- gates A, B and C pass 10/10, 9/9 and 5/5;
- `make reconcile` compares the lake and passes (`lake_assets=3013`).

The lake follows the spec:
- **Table:** the REST catalog's `inventory.asset_changes` has the §12.3 schema exactly (ids, names, types and required flags), the spec `day(changed_at)`, format version 2, and the Parquet and zstd properties. Its location is `s3://warehouse/inventory/asset_changes`.
- **Files:** RustFS holds Parquet data under `data/changed_at_day=…/` and `*.metadata.json` under `metadata/`. A downloaded data file has `PAR1` magic and zstd frames.
- **Sink:** the job writes through `FlinkSink.forRowData` at write parallelism 1, with uid prefix `iceberg-sink`, and commits one snapshot per checkpoint. Each snapshot summary carries `flink.job-id` and `flink.max-committed-checkpoint-id`.
- **Doris:** reads the lake through catalog `lake`, and the `explorer` user can read it but cannot create anything in it.

Exactly-once into the lake also holds under failure, which matters for sprint E. I checked it independently:
- **TaskManager killed** 5 s into `Burst(1000)` and restarted 15 s later: the job recovered from `chk-49`, the reconcile (Doris and the lake) passed, and there were 0 duplicate (`id`, `version`) pairs in the lake and 0 duplicate `change_rate_1m` pairs.
- **`make down`, then `make up`:** the job restored from the new savepoint (`is_savepoint=true`) with the same label run id. The old job's last snapshot is checkpoint 54, the savepoint id, and the new job continues from 55, with the same committer operator id. A later `make move ID=LT-0042 SITE=Madrid` reached the lake, the reconcile passed, and there were still no duplicates.

The one spec deviation, `use_path_style` instead of `s3.use_path_style` (D-037), is a correctly applied and recorded R6 fallback. I reproduced its evidence: a temporary catalog with `s3.use_path_style` fails with `UnknownHostException: warehouse.rustfs`.

There are **no critical and no major findings**. Two minor findings and five informational ones follow. The main risk for sprint D is still the Doris FE's memory headroom (F-01).

### 1.1 Gate results (re-run exactly as written, sequentially)

Sprint C gate, starting from the stack I found:

```
$ make build && make clean && make up            -> exit 0
    "down --volumes --remove-orphans", then "wait: ready" (21:51:26Z):
    adapter, datacore, doris-be, doris-fe, dragonfly, flink-jobmanager, iceberg-rest, kafka, rustfs healthy;
    doris-init, doris-init-lake, flink-job, kafka-init, rustfs-init completed; flink-taskmanager running;
    "doris-be (SHOW BACKENDS) 172.30.0.3 alive=true"; "flink job adl-processor RUNNING"
$ make test lint                                  -> exit 0 (39 s)
    go test ./...                                 -> every package ok
    mvn test                                      -> Tests run: 37, Failures: 0, Errors: 0, BUILD SUCCESS
                                                     (LakeCatalogTest 3, LakeRowsTest 5, LakeTableTest 3, ProcessorJobTest 2)
    buf lint, buf breaking against main           -> ok
    golangci-lint v2.15.0                         -> 0 issues.
    mvn spotless:check                            -> BUILD SUCCESS (27 files)
    explorer, web                                 -> "not present yet, skipped"
$ make acceptance-gate GATE=A                     -> ok, 10/10 PASS (14.3 s)
$ make acceptance-gate GATE=B                     -> ok, 9/9 PASS (293.5 s)
    B09 freshness: p95 10.29 s, max 10.39 s
$ make acceptance-gate GATE=C                     -> ok, 5/5 PASS (27.4 s)
    --- PASS: TestGateC_01_TableExists (0.23s)
    --- PASS: TestGateC_02_ChangesLandInLake (13.44s)
    --- PASS: TestGateC_03_ParquetInRustFS (0.03s)
    --- PASS: TestGateC_04_NoDuplicatesInLake (0.30s)
    --- PASS: TestGateC_05_LakeReconciled (12.55s)
$ make reconcile                                  -> exit 0
    "reconcile: PASS (checked assets=3013 groups=10 lake_assets=3013 users=302)"
```

Sprint 0 checks:

```
$ go run …buf@v1.75.0 lint && go run …buf@v1.75.0 build && go vet ./... && go build ./... \
  && go vet -tags acceptance ./acceptance/... && test "$(grep … | wc -l)" -eq 38 && go test ./...
-> exit 0 (16 packages ok)
```

After the state-changing probes of §1.2, gate C was run once more: 5/5 PASS (32.8 s; C02 took 23.2 s).

### 1.2 Extra live checks (not part of the gate)

| Check | Result |
|---|---|
| Locked files | `git diff --quiet plan-v1 -- specs/system-spec.md specs/sprints-plan.md CLAUDE.md run-sprints.sh` → 0. `git diff --quiet sprint-0-done -- acceptance/` → 0. The sprint diff touches no `acceptance/`, `labkit/`, `adlctl/` or `Makefile` file |
| REST catalog (`GET /v1/namespaces/inventory/tables/asset_changes`) | `format-version 2`. Schema: `(1 id string req) (2 hostname string req) (3 kind string req) (4 os string req) (5 site string req) (6 owner_user_id string opt) (7 version long req) (8 changed_at timestamptz req) (9 ingested_at timestamptz req)`. Spec `changed_at_day`, `day`, source 8. Properties `write.format.default=parquet`, `write.parquet.compression-codec=zstd`. One `append` snapshot per checkpoint; the first holds 3,105 records (the seed) |
| Bucket (aws-cli against `rustfs:9000` with the `.env` keys) | 12 objects under `inventory/asset_changes/data/changed_at_day=2026-11-25/` (`*.parquet`) and 37 under `metadata/` a few minutes after start. An anonymous `GET /warehouse/` returns 403. The console answers 200 at `:9001/rustfs/console/` |
| Parquet codec | A downloaded data file starts and ends with `PAR1` and contains 14 zstd frame headers (`28 b5 2f fd`) |
| `SHOW CREATE CATALOG lake` | `type=iceberg`, `iceberg.catalog.type=rest`, `iceberg.rest.uri=http://iceberg-rest:8181`, `warehouse=s3://warehouse/`, `s3.endpoint=http://rustfs:9000`, `s3.region=us-east-1`, keys masked, `use_path_style=true`, `meta.cache.iceberg.table.ttl-second=0` |
| D-037 evidence, reproduced | A temporary catalog with the §11.6 properties verbatim (`s3.use_path_style`) fails on `SELECT COUNT(*) …` with `UnknownHostException: warehouse.rustfs`. I dropped it afterwards |
| `SHOW GRANTS FOR 'explorer'@'%'` | `CatalogPrivs: lake: Select_priv`; `internal.inventory: Select_priv`. As `explorer`, `SELECT … FROM lake.inventory.asset_changes` works, and `CREATE DATABASE lake.x` is denied |
| Lake through Doris | `SHOW CREATE TABLE lake.inventory.asset_changes`: `text` columns, `timestamptz(6)`, `PARTITION BY LIST (DAY(changed_at))`. `MAX(changed_at)` was about 8 s behind `NOW()` (§1.5 target: 60 s). 0 duplicate pairs |
| Flink vertices (`GET /jobs/<id>`) | `Source: EntitiesSource -> DecodeValidate -> (DlqSink…, Watermarks)` p=2; `VersionGate -> (… OnlyAssetChanges -> RowDataAssetChanges)` p=2; `ChangeRateWindow -> …` p=2; `iceberg-sink-IcebergStreamWriter -> iceberg-sink-IcebergFilesCommitter -> IcebergAssetChanges: Writer` p=1. `job-parallelism: 2`. No exceptions; 0 failed checkpoints at steady state |
| JobManager log | `starting adl-processor: … iceberg=true`, `created namespace lake.inventory`, `created table lake.inventory.asset_changes at s3://warehouse/inventory/asset_changes`. These are JSON lines with `service=flink-jobmanager` |
| One-shot logs | `rustfs-init`, `doris-init-lake` and `flink-job` write JSON lines with their own `service` value. No container log contains the S3 secret key |
| Idempotency (`make up` on the live stack, 20 s) | `rustfs-init` exit 0 ("bucket warehouse already exists"); `doris-init-lake` exit 0 ("applied /opt/adl/init-lake.sql"); `flink-job` exit 0 ("already present (53c0e9… RUNNING); nothing to submit"). Same job id |
| Core-only mode | `docker compose --profile core config --services` resolves (11 services, no lake service) with Compose v5.7.1. The `required: false` dependencies are dropped |
| TaskManager kill (E03-like) | Generator paused; `Burst(1000)`; `kill flink-taskmanager` after 5 s; `start` 15 s later. The job was RUNNING again at 22:00:18 (restored from `chk-49`, after four `NoResourceAvailableException` restarts while the TM was away). Reconcile PASS with every lake category 0. 0 duplicate lake pairs, 0 duplicate `change_rate_1m` pairs |
| `make down`, then `make up` (E04-like) | Savepoint `savepoint-53c0e9-c9c109b00fb3`; the submitter logged "restoring from … with label run id 20261125215112". `latest.restored = {id 54, is_savepoint true}`. Lake snapshots: old job up to checkpoint 54, new job from 55, one committer operator id. LT-0042 v3 (Madrid) reached the lake. Reconcile PASS; 0 duplicates |
| Memory (`docker stats`, default load) | After the gate: doris-fe 1.362 GiB / 1.75 GiB, doris-be 1.234 / 3.25 GiB, TM 887 MiB / 2 GiB, JM 657 MiB / 1.25 GiB, kafka 673 MiB / 1 GiB, iceberg-rest 157 / 512 MiB, rustfs 77 / 384 MiB. After the restart probes: doris-fe **1.523 GiB / 1.75 GiB**, `memory.peak` 1,668,268,032 of `memory.max` 1,879,048,192 (89%), `memory.events max 0`, `oom_kill 0`, and `MALLOC_ARENA_MAX=2` in the FE process environment. No container has `OOMKilled=true` or a restart |
| Memory budget | The limits of the 10 long-running services add up to 11,200 MiB. With the app profile's explorer (192) and web (64) that makes 11,456 MiB, the §15.4 total. One-shots: `rustfs-init` and `doris-init-lake` 256M each |
| Pins | `amazon/aws-cli:2.42.4` is the newest numbered tag on Docker Hub. `hadoop-client-*:3.4.3` is the newest 3.4.x on Maven Central (3.5.0 exists). Both are in D-035. Every image tag in the compose file is exact |
| Layout and language | `processor/.../lake/` has three files. No new single-file directory (the only ones are Java package directories, which §17.1 allows). Everything is in English; no TODO or FIXME in the changed files |

## 2. Findings

### Critical

None.

### Major

None.

### F-01 · minor · The Doris FE still has a thin memory margin going into sprint D

- **What:** D-038 (`MALLOC_ARENA_MAX=2`) helped: `memory.events max` stays at 0, and RSS dropped from 1.72 GiB to about 1.4–1.5 GiB.
  - However, after a few minutes of restarts and gate C, the FE reached a cgroup `memory.peak` of 1.67 GB, 89% of its 1,792 MiB limit. Its `anon` memory is 1.59 GB.
  - D-038 itself notes that the heap is not fully touched yet: 1280 MB of heap plus about 600 MB of non-heap would exceed the limit.
  - Sprint D adds Arrow Flight SQL on the FE, and the explorer's history queries go through the lake catalog (Iceberg metadata, with the table cache TTL at 0, so every query reloads metadata).
  - This is the same risk as sprint B's F-03. It is lower now, but not gone.
- **Evidence:** `docker exec adl-doris-fe-1 cat /sys/fs/cgroup/memory.peak` → `1668268032`; `memory.max` → `1879048192`; `memory.stat anon 1592901632`; `docker stats` → `1.523GiB / 1.75GiB`. The iceberg-rest log shows "Table loaded by catalog" on every lake query.
- **Spec:** §15.4, §3 R9, §18 item 7.
- **Fix:** no change is required in sprint C. In sprint D, measure `memory.peak` and `memory.events` in the FE after the gate D load. If it approaches the limit, apply R9 in order within the FE budget and record `docker stats` before and after. Options include a `-XX:MaxDirectMemorySize` cap for the Flight SQL buffers, or a smaller FE heap.

### F-02 · minor · `LakeCatalog.closeQuietly` is not quiet and can hide the real error

- **What:** `ensureTable` closes the catalog in a `finally` block (`LakeCatalog.java:86-87`). `closeQuietly` (`:92-100`) rethrows an `IOException` from `close()` as `UncheckedIOException` (`:97`).
  - If the `try` block already failed (catalog unreachable, wrong credentials, schema conflict), an exception from `close()` replaces the original one. The JobManager log and the submitter's printed job exceptions would then show a close error instead of the cause.
  - The name says "quietly", and the `catch (Exception e)` branch only logs. The two branches are inconsistent.
- **Evidence:** `processor/src/main/java/io/github/cristianrisueo/adl/processor/lake/LakeCatalog.java:86-100`.
- **Spec:** §10.7 (table creation in `main()`), §10.11 step 6 (print the job's exceptions); general robustness.
- **Fix:** log every close failure as a warning (drop the `UncheckedIOException` branch), or use try-with-resources so a close failure is added as a suppressed exception.

### F-03 · info · The §11.6 property deviation is a correct R6 fallback (D-037)

`infra/doris/init-lake.sql:18` uses `"use_path_style" = "true"` instead of the spec's `"s3.use_path_style"`. R6 names exactly this fallback, and D-037 records it with the symptom, the bytecode evidence and the result. I reproduced the symptom (§1.2). The property `meta.cache.iceberg.table.ttl-second` is accepted, and lake freshness is about 8–23 s, so the explorer REFRESH fallback of R6 is not needed.

One side note: `CREATE CATALOG IF NOT EXISTS` never updates an existing catalog. A future change to `init-lake.sql` only takes effect after `make clean`, or with an `ALTER CATALOG`. That is consistent with §11.8 (idempotent statements) and with how the gates run.

### F-04 · info · Lake timestamps reach Doris as `timestamptz(6)` (for sprint D)

Internal tables use `DATETIME(3)`. The lake columns `changed_at` and `ingested_at` are `timestamptz(6)` in Doris (`SHOW CREATE TABLE lake.inventory.asset_changes`), and MySQL renders them as `2026-11-25 21:51:09.848000+00:00`. Over Flight SQL they will most likely arrive as Arrow timestamps with a time zone and microsecond unit. The §13.6 note covers only `DATETIME`. The explorer's history mapping must accept this type, and must still render RFC 3339 with milliseconds and `Z` (§5.5, §13.3). Its unit test should build such a batch.

### F-05 · info · The Iceberg operator names are a recorded G1 choice (D-036)

`FlinkSink` names its stateful operators from the uid prefix: `iceberg-sink-IcebergStreamWriter` and `iceberg-sink-IcebergFilesCommitter`. The §10.10 name `IcebergAssetChanges` sits on the terminal discarding sink (`IcebergAssetChanges: Writer`). The uid prefix `iceberg-sink` gives the writer and committer stable uids, and the committer's operator id is unchanged across restore (`0e4eaa3db14ae3b6752b25172fd05c50` in every snapshot). This is what matters for savepoints. The builder exposes no rename, so the choice is reasonable and recorded.

### F-06 · info · `doris-init.sh` would log the S3 secret key if `init-lake.sql` fails

`infra/scripts/doris-init.sh:48` runs `mysql -v`, which echoes every statement, and puts the output in the ERROR log line on failure. For `doris-init-lake`, the echoed SQL contains the substituted `s3.secret_key`. On success nothing is logged, and no live log contains the key. The lab's keys are non-secret defaults (§17.2), so this is informational. If you want to fix it, drop `-v`, or mask `<…_KEY>` and `<…_PASSWORD>` values before logging.

### F-07 · info · Upstream lake images log plain text and expected 404s as ERROR

- `rustfs` and `iceberg-rest` write their images' own plain-text logs, not JSON with `service`. This is the same situation as the Doris FE and BE in sprint B: their formats cannot be changed without wrapping the images.
- At every fresh start, `iceberg-rest` logs three `ERROR … RESTCatalogServlet - Error processing REST request` stack traces. They are the expected 404s of `namespaceExists`, `tableExists` and the view check that runs before the table is created (`NoSuchNamespaceException`, `NoSuchTableException`, `NoSuchViewException`).
- The TaskManager logs one WARN, "Cannot load Dropwizard metrics", from `IcebergStreamWriterMetrics`.

All of these are harmless. They are worth knowing when reading logs in sprint E.

## 3. Requirements table (sprint C scope)

| # | Requirement (spec) | Status | Note |
|---|---|---|---|
| 1 | §2.3 Images: `rustfs/rustfs:1.0.6`, `apache/iceberg-rest-fixture:1.10.1`, `amazon/aws-cli` 2.x latest at build, `mysql:8.4` client | Met | `aws-cli:2.42.4` (newest numbered tag) and `mysql:8.4.13`, both pinned and recorded (D-035, D-025) |
| 2 | §2.4 `iceberg-flink-runtime-2.1:1.12.0`, `iceberg-aws-bundle:1.12.0`, `hadoop-client-api` and `-runtime` 3.4.x latest at build | Met | 3.4.3 recorded in D-035; flink-table 2.1.3 `provided`; jar 222 MiB |
| 3 | §3 R1 No fallback set; no minor-version mixing | Met | Engine 2.1.3 in every snapshot summary |
| 4 | §3 R6 Path style, cache TTL, REFRESH fallback, SeaweedFS only after G4 | Met | `use_path_style` fallback applied and recorded (D-037, F-03); TTL accepted; RustFS kept |
| 5 | §3 R7 Hadoop client in the jar from the start; `FlinkSink.forRowData`; HTTP-client override only if needed | Met | No override needed; `IcebergSink` not used |
| 6 | §3 R8 Fixture persisted with a named volume at `/tmp` | Met | `iceberg-rest-data:/tmp`; the catalog survived `make down` and `make up` |
| 7 | §3 R9 Memory: limits within budget; `docker stats` before and after any change | Met | D-038 records before and after; the margin is still thin (F-01) |
| 8 | §3 R17 Flink 2.x APIs only; no new configuration keys | Met | `MapFunction`, Sink V2 discarding sink, no `SourceFunction` or `SinkFunction` |
| 9 | §3 R18 Job jar upload | Met | 222 MiB < 512 MiB; uploaded in about 5 s |
| 10 | §10.1 Iceberg branch: assets from the VersionGate output → RowData → `FlinkSink`, only when `--iceberg.enabled` | Met | `ProcessorJob.java:132-155`; core-only graph has no Iceberg operator (unit test) |
| 11 | §10.7 Catalog `lake` with the six property groups; keys from the job arguments | Met | `LakeCatalog.properties`; exact-map unit test; empty keys fail fast |
| 12 | §10.7 `main()` creates namespace `inventory` and table `asset_changes` if missing | Met | JobManager log "created namespace", "created table"; `AlreadyExistsException` tolerated; close-error handling (F-02) |
| 13 | §10.7 `TableLoader.fromCatalog`, `uidPrefix("iceberg-sink")`, `writeParallelism(1)`, `append()` | Met | Live writer and committer at p=1 |
| 14 | §10.7 Row mapping: empty owner → null, timestamps as timestamptz | Met | `LakeRowsTest` (Appendix A values, null owner, mapping time) |
| 15 | §10.7 / §12.3 Only assets; commits on checkpoint; exactly-once | Met | One snapshot per checkpoint; 0 duplicates after a TM kill and after a savepoint restart |
| 16 | §10.7 `--iceberg.enabled=false` removes the branch | Met | Unit test; `ICEBERG_ENABLED` passed by the submitter |
| 17 | §10.10 Iceberg sink name `IcebergAssetChanges`, uid prefix `iceberg-sink` | Met | Name on the terminal sink (F-05, D-036) |
| 18 | §10.12 Iceberg and S3 arguments and defaults; `main()` reaches the catalog from the JobManager | Met | `iceberg=true` logged; table created by the JobManager |
| 19 | §11.6 Catalog `lake` | Met (R6 fallback) | Every property verbatim except `use_path_style` (D-037) |
| 20 | §11.7 `GRANT SELECT_PRIV ON lake.*.* TO 'explorer'@'%'` after the catalog | Met | `CatalogPrivs: lake: Select_priv`; explorer can SELECT, not CREATE |
| 21 | §11.8 `doris-init-lake` (one-shot, lake profile) runs `init-lake.sql`; idempotent | Met | Re-run exits 0 |
| 22 | §12.1 RustFS: image, `/data` volume, ports 9000 and 9001, the five environment variables, uid 10001, `/health` | Met | Plus `RUSTFS_CONSOLE_ADDRESS` (D-035); console reachable |
| 23 | §12.1 `rustfs-init` creates `warehouse`, path style, "already owned" is success | Met | First run "created bucket warehouse", re-run "already exists"; JSON logs |
| 24 | §12.2 Fixture environment verbatim, port 8181, `/tmp` volume, `/v1/config` health | Met | `AWS_REGION` uses `${S3_REGION:-us-east-1}`, which resolves to the same value |
| 25 | §12.3 Schema, partition spec, properties, catalog default location, created by the processor | Met | Verified in the catalog and in C01 |
| 26 | §12.4 Small files and many snapshots accepted | Met | 56 snapshots after about 15 minutes, as expected |
| 27 | §15.2 Profile `lake`: services, dependencies and restart policies; `flink-job` depends on `iceberg-rest` healthy and `rustfs-init` completed with `required: false`; full `make up` | Met | `--profile core config` still resolves; full `make up` ready |
| 28 | §15.3 Every service on `adl` | Met | |
| 29 | §15.4 rustfs 384 MiB, iceberg-rest 512 MiB (heap 384 MiB); total ≤ 11,456 MiB | Met | |
| 30 | §15.6 Health checks: rustfs 30/5/20, iceberg-rest 60/5/20 | Met | |
| 31 | §15.7 Volumes `rustfs-data` and `iceberg-rest-data`, writable; `make clean` removes them | Met | Owners checked in D-035; `down --volumes` |
| 32 | §15.8 Host ports 8181, 9000 and 9001 | Met | |
| 33 | §16.2 RowData mapping unit test (carried from sprint B F-08) | Met | Plus `LakeTableTest`, `LakeCatalogTest` and the lake graph test |
| 34 | §16.4 Gate C (C01–C05), with gates A and B re-run | Met | 10/10, 9/9, 5/5 |
| 35 | §16.5 step 4 Lake reconcile through `make reconcile` | Met | `lake_assets=3013`; not skipped |
| 36 | §1.5 Lake freshness within 60 s (p95); cold start ≤ 10 min | Met | C02 13–23 s; `make up` ready about 1 minute after `make clean` |
| 37 | §17 Layout, no single-file directories, English, JSON logs with `service` for our own code | Met | Upstream images log in their own format (F-07) |
| 38 | G2 No acceptance test weakened; `acceptance/` and the specs untouched | Met | Locked-file checks return 0 |
| 39 | G3 Every rule applied, fallback taken and "latest at build" pin recorded | Met | D-035 to D-038 |
| 40 | Later gates not put at risk (D: lake over Flight SQL; E: TM kill, `make down` / `make up`) | Met with notes | E03- and E04-like probes pass. Sprint D must watch FE memory (F-01) and map `timestamptz(6)` (F-04) |

## 4. Gate summary

| Gate step | Result |
|---|---|
| `make build && make clean && make up` | Green ("wait: ready"; every lake service healthy or completed) |
| `make test lint` | Green (Go ok; Maven 37/37; buf; golangci-lint 0 issues; Spotless clean) |
| `make acceptance-gate GATE=A` | Green, 10/10 |
| `make acceptance-gate GATE=B` | Green, 9/9 (B09 p95 10.29 s, max 10.39 s) |
| `make acceptance-gate GATE=C` | Green, 5/5 (and 5/5 again after the restart probes) |
| `make reconcile` | Green, lake compared (`lake_assets=3013`) |
| Sprint 0 checks (buf lint and build, vet, build, acceptance vet, 38 cases, `go test`) | Green |

I left the stack running in full mode, restored from the audit's savepoint (`savepoint-53c0e9-c9c109b00fb3`). The job id is `0ef7bf89a28428dc30ccf8b0d93249d3`, the label run id is still `20261125215112`, and the generator runs at its default rate. The temporary catalog `audit_c_tmp` was dropped.

## 5. Fix status (written by the builder after the audit)

| Finding | Severity | Status |
|---|---|---|
| F-01 FE memory headroom for sprint D | Minor | **Recorded, not changed.** D-038 already capped the malloc arenas, and the §11.2 heap stays at 1280m. R9 applies on an OOM kill. Listed as the main sprint D risk in `build-progress.md` |
| F-02 `closeQuietly` can mask the table-creation error | Minor | **Fixed.** `LakeCatalog.closeQuietly` never throws; it logs a warning on any close failure |
| F-03 `use_path_style` instead of `s3.use_path_style` | Info | No change; recorded R6 fallback (D-037) |
| F-04 Lake timestamps arrive as `timestamptz(6)` | Info | Passed on to sprint D in `build-progress.md` |
| F-05 `IcebergAssetChanges` on the terminal sink | Info | No change (D-036) |
| F-06 `doris-init.sh` could print the S3 secret on a SQL failure | Info | **Fixed.** On failure, every substituted placeholder value is masked as `***` before the error line is logged. The masking was checked in the `mysql:8.4.13` image's bash 5.1 |
| F-07 Upstream images log plain text | Info | No change: these are third-party images (the CLAUDE.md JSON rule covers our own services and scripts) |

After the fixes, the full gate was re-run from `make down && make build && make clean && make up`:
- `make up`: ready.
- `make test lint`: green (Maven 37/37).
- Gates: A 10/10, B 9/9 (B09 p95 10.25 s, max 10.47 s), C 5/5. That is 24 PASS and 0 FAIL.
- `make reconcile`: `PASS (checked assets=3013 groups=10 lake_assets=3013 users=302)`.
- Sprint 0 checks: green.
