# Sprint B build report — Processing

## What was built

| Area | Spec | Files |
|---|---|---|
| Doris FE and BE images | §11.1, §11.2, R2, R3 | `infra/doris/fe.Dockerfile` (heap 1280m/1280m, `arrow_flight_sql_port = 8070`), `infra/doris/be.Dockerfile` (BE JVM `-Xmx512m`, `arrow_flight_sql_port = 8050`, `mem_limit = 2048M`). Each sed-edits the image's default conf, and the build fails if a key did not land |
| Core init SQL | §11.3–§11.5, §11.7 | `infra/doris/init-core.sql`: UTC global time zone, database, four tables, view, async MV, `explorer` user and its grant on `internal.inventory.*` |
| doris-init | §11.8 | `infra/scripts/doris-init.sh <sql-file>` in `mysql:8.4.13`: waits for `Alive=true` in `SHOW BACKENDS`, substitutes the placeholders, applies the file. Reusable by sprint C's `doris-init-lake` |
| Flink cluster image | §10.9, §15.7 | `infra/flink/Dockerfile` (`flink:2.1.3-java17`, `/flink-data` owned by 9999), `infra/flink/log4j-console.properties` (JSON console logs with `service`) |
| processor job | §10.1–§10.6, §10.8, §10.10, §10.12 | `processor/pom.xml`, `processor/src/main/java/io/github/cristianrisueo/adl/processor/**`: `ProcessorJob`, `JobOptions`, `decode/{Envelope,EntityDecoder,EntityDeserializer,DecodeValidate}`, `gate/VersionGate`, `sink/{DorisRows,DorisSinks,DlqSerializer}`, `window/{ChangeRate,ChangeRateCount,ChangeRateWindow}` |
| Unit tests | §16.2 | 25 tests: VersionGate (harness, including snapshot and restore), deserializer (bad bytes, null value, key mismatch, every §6.7 rule, valid entities), `DecodeValidate` routing, JSON rows (Appendix A), DLQ headers, window counting (MiniCluster), options, and the §10.10 names and uids |
| Submitter | §10.11, §10.6 | `processor/Dockerfile` (Maven build, then alpine with bash, curl and jq, uid 9999) and `infra/flink/submit-job.sh`, bind-mounted read-only |
| Compose | §15.2–§15.7 | `doris-fe` (172.30.0.2), `doris-be` (172.30.0.3), `doris-init`, `flink-jobmanager`, `flink-taskmanager` and `flink-job`, all in profile `core`, with the §15.4 limits and §15.6 health checks. Volumes `doris-fe-meta`, `doris-be-storage` and `flink-data`. `FLINK_PROPERTIES` is the §10.9 block verbatim, shared through a YAML anchor |
| Makefile | §15.9 | `make test` runs `mvn test` and `make lint` runs `mvn spotless:check`, both in `maven:3.9.16-eclipse-temurin-17` with the `adl-maven-repo` cache volume |
| adlctl | §15.10, §16.5 | `reconcile` skips the lake when the lake profile is not running (D-032), with `TestLakeRunning` |

### Design highlights

- **Topology** (spec §10.1):
  - `KafkaSource` reads the three topics in group `adl-processor`, from committed offsets with fallback to earliest, and commits on checkpoint;
  - the deserializer never throws;
  - `DecodeValidate` sends invalid envelopes to a side output, which feeds an `AT_LEAST_ONCE` `KafkaSink` to `dlq.processor` carrying the §8.3 headers;
  - watermarks: 5 s bounded out-of-orderness plus 30 s idleness, on valid records only;
  - `VersionGate` is keyed by `<type>:<id>` and counts `versionGateAccepted` and `versionGateDropped`;
  - three filters and JSON mappers feed three 2PC `DorisSink`s;
  - a one-minute tumbling event-time window counts per entity type into the fourth `DorisSink`.
- **Envelope:** a Flink POJO, with no Protobuf objects inside, so Flink needs no Kryo setup.
- **Doris sinks:**
  - 2PC set explicitly, `deletable=false`, `format=json` and `read_json_by_line=true`;
  - label prefix `adl-<run-id>-<table>`;
  - optional `benodes` with auto-redirect false (R2), which was not needed: the FE redirect to `172.30.0.3:8040` works.
- **Submitter:**
  - waits for the REST API and free slots, and exits 0 when the job is already active;
  - restores from the newest `_metadata` (sub-second mtime) among savepoints and retained checkpoints;
  - reuses `/flink-data/label-run-id` on restore and writes a new one on a fresh start;
  - uploads, runs and then deletes the jar, waits for RUNNING, and keeps three savepoints;
  - logs JSON lines.

## How to verify

```sh
make build && make clean && make up-core   # ready in about 45 s from clean
make test lint
make acceptance-gate GATE=A && make acceptance-gate GATE=B
make reconcile
```

Results at the end of the sprint, before the audit:
- **`make up-core` from clean:** `wait: ready` in about 45 s.
  - The FE was healthy in about 30 s, the BE about 20 s later.
  - `SHOW BACKENDS` shows `172.30.0.3 alive=true`, and the job is RUNNING.
  - The seed was in Doris (3000 assets, 300 users, 8 groups) within a minute.
- **`make test lint`:** green.
  - Go unit tests, buf lint and breaking, golangci-lint (0 issues);
  - Maven: 25 tests and Spotless check pass.
- **Gate A:** 10/10.
- **Gate B:** 9/9 in 294 s. B09 freshness: p95 10.2 s, max 10.2 s; latency is the 10 s checkpoint interval plus the commit.
- **`make reconcile`:** PASS, with the lake skipped and the reason printed (core-only mode).
- **Sprint 0 gate:** `go vet`, `go build`, `go vet -tags acceptance` and the 38-case count are green. `buf lint` and `go test` run inside `make lint` and `make test`.
- **Lifecycle checks** (manual, not committed as tests):
  1. **`make down`:** the savepoint `savepoint-575a90-…` was written. `make up-core` then submitted a new job id with `latest.restored.is_savepoint=true` on that path, and the label run id stayed `20261125203246`. There was no "Label Already Exists" error. A new move plus `make reconcile` passed.
  2. **`docker compose kill flink-jobmanager`, then `make up-core`:** the job restored from the newest retained checkpoint (`…/chk-49`) with the same run id.
  3. **TaskManager killed 5 s into `Burst(1000)` and started 15 s later:** the job restarted from checkpoint 51 and the reconcile passed. `change_rate_1m` had no duplicate (`window_start`, `entity_type`) pairs. This is an early look at E03.
- **Logs:**
  - the JobManager log lists every §10.9 key as "Loading configuration property", with no deprecation warnings;
  - the TaskManager shows only a benign metric-name collision warning (`pendingCommittables`, from the chained Doris committers);
  - the BE log confirms "Mem Limit: 2.00 GB, origin config value: 2048M".

`docker stats --no-stream`, core stack at default load:

| Container | Usage / limit |
|---|---|
| doris-fe | 1.19 GiB / 1.75 GiB |
| doris-be | 1.09 GiB / 3.25 GiB |
| flink-jobmanager | 508 MiB / 1.25 GiB |
| flink-taskmanager | 531 MiB / 2 GiB |
| kafka | 556 MiB / 1 GiB |
| dragonfly | 44 MiB / 512 MiB |
| datacore | 14 MiB / 160 MiB |
| adapter | 10 MiB / 160 MiB |

## Deviations from the plan

- **google-java-format:** pinned at 1.28.0, not at its newest release. Newer releases need Java 21, and Spotless refuses them on JDK 17 (D-025).
- **JUnit:** 5.14.4 through the BOM, not 6.x, because of a version clash with the JUnit 5 artifacts that Flink's test utilities pull in (D-025).
- **Log4j JSON layout:** the plan expected to download `log4j-layout-template-json`, but the Flink image already ships it, so only the properties file was added (D-031).
- **`DecodeValidateTest`:** a small extra test of the side-output routing, beyond the plan's list.
- **`processor/.mvn/jvm.config`:** not needed. google-java-format 1.28.0 runs on JDK 17 without extra flags.
- **Placeholder bug:** the first `doris-init` run failed on a placeholder-shaped word in a comment of `init-core.sql`. The comment was reworded, and the script still fails loudly on any placeholder without a value.
- **No R-ladder fallback was needed:** R1, R2's benodes, R11's relocation, R13 and R15 all stayed unused.

## Notes for the next sprints

- **Until sprint C, `make up` fails at `flink-job`** with a clear message: `--iceberg.enabled=true` needs the Iceberg branch (D-029). Sprint C:
  - adds the branch (§10.7);
  - adds the Iceberg, AWS and Hadoop dependencies to `processor/pom.xml`;
  - adds the RowData mapping and its unit test (§16.2);
  - adds the `iceberg-rest` and `rustfs-init` dependencies (`required: false`) to `flink-job`.
- **Run ids on failed submissions:** every fresh submission attempt writes a new label run id, even if it then fails. That is harmless, because a fresh start must use a new run id anyway.
- **`infra/scripts/doris-init.sh`** takes the SQL file as its argument, so `doris-init-lake` can run `init-lake.sql` with it. The `GRANT … ON lake.*.*` statement belongs in `init-lake.sql`.
- **Checkpoints of old job ids stay under `/flink-data/checkpoints/<jid>/`**, three per job. Only savepoints are pruned (§10.11 step 7).
