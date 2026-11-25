# Sprint B plan — Processing

## Context

Sprints 0 and A are done and tagged. Here is what the repository holds now:
- contracts and generated code, including Java in `processor/src/generated/java`. Nothing else exists under `processor/` yet;
- `labkit`, `adlctl` and the 38 acceptance cases (`acceptance/` is **locked**);
- a compose file with `kafka`, `kafka-init`, `dragonfly`, `datacore` and `adapter` in profile `core`;
- a Makefile with real `build`, `up`, `up-core`, `down` and `clean`. Its processor branches in `test` and `lint` are stubs that print "not available until sprint B" and fail once `processor/pom.xml` exists.

Sprint B (plan §4) must make the Flink job write current state and per-minute change counts to Doris exactly once. Scope:
- the Doris FE and BE images, configuration, core init SQL and grants (spec §11, without §11.6);
- the Flink cluster image;
- the processor job (spec §10), tested only with `--iceberg.enabled=false`;
- the `flink-job` submitter, with the restore and run-id logic (spec §10.11);
- `make down` with stop-with-savepoint;
- `adlctl wait` checking the job.

**Gate** (plan §4). The sprint 0 gate is re-run too, because every gate re-runs the earlier ones:
```sh
make build && make clean && make up-core
make test lint
make acceptance-gate GATE=A && make acceptance-gate GATE=B
make reconcile
# sprint 0 gate: buf lint && buf build && go vet ./... && go build ./... && go vet -tags acceptance ./acceptance/... && 38-count && go test ./...
```

**Environment found** (read-only checks on 2026-11-25):
- Docker 29.9.3 and Compose v5.7.1, with about 11.6 GiB and 12 CPUs. Go 1.27.2. The CPU is an i7-8700 with AVX2, so the standard Doris BE build runs.
- The sprint A core stack is running now. The gate's `make clean` removes it.
- None of the Doris, Flink, Maven or mysql images is pulled yet.

**What the locked tests and the existing tools require** (read from `acceptance/gate_b_test.go`, `harness_test.go`, `labkit/{flink,doris,reconcile,config}.go` and `adlctl/{wait,flink,reconcile}.go`):
- **Precheck:** it dials `localhost:9030` (Doris MySQL) and `localhost:8081` (Flink REST), plus the gate A ports. doris-be and flink-taskmanager are not dialed.
- **Job metrics:** the job is found by name `adl-processor` through `/jobs/overview`. `versionGateDropped` is read as vertex metric ids `^\d+\.VersionGate\.versionGateDropped$`, so the operator name must be exactly `VersionGate`.
- **Doris reads:** labkit reads `inventory.assets`, `users` and `` `groups` ``, `v_assets_enriched`, `mv_assets_by_os_site` and `change_rate_1m`, and runs `REFRESH MATERIALIZED VIEW … COMPLETE` and `SHOW BACKENDS` (Host and Alive columns). Reconcile compares `changed_at` at millisecond precision in UTC, so `DATETIME(3)` must hold the exact UTC millisecond.
- **B03 and B04:** B03 produces v-1 and a different v straight to `entities.assets`, and Doris must keep Madrid at v. B04 needs `versionGateDropped` to grow by 3 within 30 s. The Flink metric fetcher caches for about 10 s, which is fine.
- **B05:** random bytes give `DECODE_ERROR`. An empty hostname gives `VALIDATION_ERROR`, and so does key LT-9106 carrying id LT-9105. Each DLQ record has headers `service=processor` and `source.topic=entities.assets`, and its key and value equal the originals byte for byte.
- **B07:** after a Doris reconcile, the MV equals `GROUP BY os, site` within 150 s, after a manual refresh.
- **B08:** every `change_rate_1m` row has `window_end - window_start = 60 s` and `changes > 0`, with no duplicate pairs. At least one closed asset row must exist 3 minutes after the job starts.
- **B09:** p95 ≤ 30 s and maximum ≤ 60 s from upsert to Doris, at default load.
- **`adlctl wait --core`:** it already adds a "SHOW BACKENDS alive" row and a "job RUNNING" row once `doris-be` and `flink-jobmanager` are defined. One-shots (`restart: "no"`) must exit 0, and health checks must be written in compose.
- **`make reconcile`** runs `adlctl reconcile --timeout 3m`, which **includes the lake by default**. In core-only mode there is no `lake` catalog, so the gate's `make reconcile` would fail. Step 8 handles this.
- **E04 (later):** the resubmitted job must get a new job id, so no fixed `jobId` is pinned.
- **CI:** the `java` job runs `mvn -B -f processor/pom.xml verify`, so Spotless check must be bound to `verify`. The `images` job builds every compose `build` service with its own context, with `file = context/dockerfile`.

## Step 0 — Save the plan
Save this plan verbatim to `specs/sprints/sprint-B/sprint-plan.md`.

## Step 1 — Pin versions and inspect the images (spec §2, §3 R2, G3)
- **Maven Central metadata:** confirm `flink-connector-kafka:5.0.0-2.1`, `flink-doris-connector-2.1:26.3.0` and `protobuf-java:4.36.2` (D-004). Choose "latest at build" for:
  - `maven-shade-plugin`, `maven-compiler-plugin`, `maven-surefire-plugin` and `build-helper-maven-plugin`;
  - `spotless-maven-plugin` and `google-java-format`;
  - `junit-jupiter` and `jackson-databind`.
- **Docker Hub:** pin exact tags for `mysql:8.4.x` (newest patch), `maven:3.9.x-eclipse-temurin-17` and the alpine base of the submitter image (`alpine:3.24.4`, already pinned in D-006).
- **Read the 4.1.5 entrypoints** before writing compose (R2):
  - `docker run --rm --entrypoint sh apache/doris:fe-4.1.5 -c 'ls /opt/apache-doris; cat /opt/apache-doris/*.sh'`, and the same for `be-4.1.5`;
  - check election mode against assign mode, `priority_networks`, how the BE registers with the FE, whether `curl` is present, the default `JAVA_OPTS_FOR_JDK_17` in `fe.conf` and `be.conf`, the swap and ulimit checks, and the `mem_limit` syntax.
- **Read `flink:2.1.3-java17`:**
  - `/docker-entrypoint.sh` (how `FLINK_PROPERTIES` reaches `config.yaml`);
  - the `flink` uid (9999);
  - whether `curl` is present;
  - the log4j version and `conf/log4j-console.properties`;
  - whether `flink-dist` already bundles `flink-connector-base`. If it does, that dependency is `provided` and is not shaded.
- **Verify the Flink 2.1 API signatures** with `javap` on the resolved jars:
  - `KafkaSource` and `OffsetsInitializer.committedOffsets(...)`, which may take the kafka-clients 4.x reset-strategy type;
  - `org.apache.flink.util.ParameterTool`;
  - the `DorisOptions` and `DorisExecutionOptions` builders (`enable2PC`, `setDeletable`, `setAutoRedirect`, `setBenodes`).
- Record every pin in a new decision-log entry.
- **Verify:** every tag resolves (`docker manifest inspect`), and every artifact exists on Maven Central.

## Step 2 — Doris images, config and init (spec §11.1–§11.5, §11.7, §11.8, §15.2–§15.7, R2, R3, R13, R15)
- **`infra/doris/fe.Dockerfile`:**
  - `FROM apache/doris:fe-4.1.5`;
  - edit the default `fe.conf` in place with sed and append: `arrow_flight_sql_port = 8070`, and the FE heap `-Xmx1280m -Xms1280m` inside `JAVA_OPTS_FOR_JDK_17`;
  - the file stays writable.
- **`infra/doris/be.Dockerfile`:** the same for `be.conf`: `arrow_flight_sql_port = 8050`, `mem_limit` = 2048 MB in the syntax the 4.1.5 docs and parser accept, and BE JVM `-Xmx512m`.
- **`infra/doris/init-core.sql`:**
  - `SET GLOBAL time_zone = 'UTC'`;
  - the database, the four tables, the view and the MV, exactly as spec §11.3–§11.5;
  - `CREATE USER IF NOT EXISTS 'explorer'@'%' IDENTIFIED BY '<DORIS_EXPLORER_PASSWORD>'` and `GRANT SELECT_PRIV ON internal.inventory.* …`.

  Every statement is idempotent.
- **`infra/scripts/doris-init.sh <sql-file>`**, reusable by sprint C's `doris-init-lake`:
  - wait until `SHOW BACKENDS` shows `Alive=true` (bounded, polling);
  - substitute the placeholders from the environment with sed;
  - run the file with `mysql -h doris-fe -P 9030 -uroot`, then exit 0. Any SQL error exits non-zero.
- **Compose** (profile `core`):
  - **`doris-fe`:** static IP `172.30.0.2`, `FE_SERVERS=fe1:172.30.0.2:9010` and `FE_ID=1` (or whatever the entrypoint says, R2). Ports 8030 and 9030 only (§15.8). Volume `doris-fe-meta`, 1792M limit, health check `curl -sf localhost:8030/api/bootstrap` (or `/api/health`) with a 180 s start period, 10 s interval and 30 retries. Image `adl/doris-fe:0.1.0`, built with context `infra/doris`.
  - **`doris-be`:** static IP `172.30.0.3`, `FE_SERVERS` and `BE_ADDR=172.30.0.3:9050`. `ulimits nofile 65536`, volume `doris-be-storage`, 3328M limit, health check `curl -sf localhost:8040/api/health` with the §15.6 timings. Depends on doris-fe healthy. No host ports.
  - **`doris-init`:** `mysql:8.4.x`, one-shot (`restart: "no"`), depends on doris-be healthy. It mounts `init-core.sql` and `doris-init.sh` read-only and gets `DORIS_EXPLORER_PASSWORD` from `.env`.
- **Verify:**
  - `make build`, then `docker compose --profile core up -d doris-fe doris-be doris-init`;
  - `SHOW BACKENDS` shows `172.30.0.3 Alive=true`;
  - `SHOW TABLES` lists the four tables, the view and the MV;
  - `SHOW VARIABLES LIKE 'time_zone'` gives UTC;
  - the explorer user can `SELECT` but not `INSERT`;
  - re-running `doris-init` exits 0;
  - `docker stats` stays within the limits.
- **Fallbacks:** an MV failure goes to R13 (a plain view with the same name and columns). A keyword clash goes to R15 (backticks everywhere). A BE start failure goes to R3: read `be.INFO` and `be.out` and fix the cause; never disable checks. All of them are recorded.

## Step 3 — Flink cluster image (spec §10.9, §15.4, §15.6, §15.7, R17)
- **`infra/flink/Dockerfile`:**
  - `FROM flink:2.1.3-java17`;
  - `mkdir /flink-data` owned by 9999;
  - curl installed only if missing;
  - JSON console logging with a `service` field: add the `log4j-layout-template-json` jar at the image's log4j version, downloaded from Maven Central with a checksum, plus `infra/flink/log4j-console.properties`, keeping the file appender for the Flink UI. If this fails after G4, keep the default layout and record it.
- **Compose:**
  - **`flink-jobmanager`:** `command: jobmanager`, `FLINK_PROPERTIES` holding the §10.9 block verbatim, port 8081, volume `flink-data`, 1280M limit, health check `curl -sf localhost:8081/overview` (90 s start period, 5 s interval, 30 retries).
  - **`flink-taskmanager`:** `command: taskmanager`, the same properties and volume, 2048M limit, depends on JM healthy, no health check (§15.6).
  - Both build the same `adl/flink:0.1.0` with identical build sections; BuildKit caches the second build.
- **Verify:**
  - the JM log shows the effective configuration: every §10.9 key accepted, no "unknown or deprecated key" warnings;
  - `/taskmanagers` shows 2 slots;
  - `/flink-data` is writable by uid 9999.

## Step 4 — processor job: Maven project and code (spec §10.1–§10.6, §10.8, §10.10, §10.12, §6.7, §8.3, R5, R11, R12, R14, R17)
**`processor/pom.xml`:**
- Java 17 (`maven.compiler.release=17`).
- Flink 2.1.3 `flink-streaming-java` and `flink-clients` as `provided`.
- `flink-connector-kafka 5.0.0-2.1`, `flink-doris-connector-2.1 26.3.0`, `protobuf-java 4.36.2` and `jackson-databind`.
- `build-helper-maven-plugin` adds `src/generated/java`.
- `maven-shade-plugin`:
  - merge `META-INF/services` (`ServicesResourceTransformer`);
  - strip signatures;
  - exclude provided Flink and log4j;
  - relocate `com.google.protobuf` only if conflicts appear (R11).
- Spotless with google-java-format on `src/main` and `src/test` only (generated code excluded), with `check` bound to `verify`.
- Test dependencies:
  - JUnit 5;
  - `flink-test-utils`;
  - the Flink test-jars (`flink-streaming-java` and/or `flink-runtime` with classifier `tests`, whichever holds the 2.1 harnesses).
- Add `processor/.mvn/jvm.config` only if google-java-format needs the `--add-exports` flags on JDK 17. Maven forces that nesting.

**Code** (package `io.github.cristianrisueo.adl.processor`; Java package directories are exempt from the single-file rule):
- **`ProcessorJob`:**
  - `main` parses the arguments with `JobOptions` (§10.12 defaults; `--label-run-id` required);
  - it builds the §10.1 topology with every name and uid of §10.10;
  - it uses no `SourceFunction`/`SinkFunction`, uses `Duration`, and relies on the cluster's checkpoint config;
  - with `--iceberg.enabled=true` it fails fast with a clear message ("the Iceberg branch arrives in sprint C; use make up-core"), recorded in the decision log.
- **`decode/Envelope`:** a Flink POJO. It holds the entity type, id, version, `changedAtMillis`, the decoded entity fields as typed columns (hostname, kind, os, site, owner, display name, email, group id, name) and the Kafka coordinates. Invalid envelopes also hold the raw key and value, the reason and the message. It never holds Protobuf objects, so no Kryo or Protobuf serializer is needed.
- **`decode/EntityDecoder` and `EntityValidator`:**
  - the topic decides the expected type;
  - the §6.7 rules for Asset, User and Group, with the key equal to the id;
  - a parse failure is `DECODE_ERROR`; a rule break or key mismatch is `VALIDATION_ERROR`.
- **`decode/EntityDeserializer`** (`KafkaRecordDeserializationSchema<Envelope>`): never throws, and always emits exactly one envelope.
- **`decode/DecodeValidate`** (`ProcessFunction`): routes invalid envelopes to the DLQ `OutputTag`.
- **`sink/DlqSerializer`** (`KafkaRecordSerializationSchema<Envelope>`):
  - topic `dlq.processor`, the original key and value;
  - the §8.3 headers: `error.reason`, `error.message` (at most 1 KB), `source.topic`, `source.partition`, `source.offset`, `failed.at` (RFC 3339 UTC) and `service=processor`.

  `KafkaSink` uses `AT_LEAST_ONCE`.
- **Watermarks:** `forBoundedOutOfOrderness(5 s)` on `changed_at`, plus `withIdleness(30 s)`, after DecodeValidate. The source uses `noWatermarks()`.
- **`gate/VersionGate`** (`KeyedProcessFunction<String, Envelope, Envelope>`):
  - keyed by `<type>:<id>`, with `ValueState<Long> lastVersion` and no TTL;
  - counters `versionGateAccepted` and `versionGateDropped` registered in `open(OpenContext)`.
- **`sink/DorisRows`:**
  - the JSON row mapping: keys are the Doris column names, datetimes are `yyyy-MM-dd HH:mm:ss.SSS` in UTC, an empty owner or group becomes `null`, `kind` is `LAPTOP`, `DESKTOP` or `SERVER`, and `ingested_at` is the time of mapping;
  - change-rate rows follow Appendix A (`yyyy-MM-dd HH:mm:ss` for the minute bounds). Doris `DATETIME` accepts either form; the choice is recorded.
- **`sink/DorisSinks`:**
  - a factory for the four `DorisSink<String>`: fenodes, optional benodes with auto-redirect false (R2), table `inventory.<t>`, root with an empty password;
  - 2PC set explicitly, `deletable=false`, `format=json` and `read_json_by_line=true` (R5);
  - label prefix `adl-<run-id>-<table>`;
  - `SimpleStringSerializer`.
- **`window/ChangeRateCount`** (`AggregateFunction`) and **`window/ChangeRateWindow`** (`ProcessWindowFunction`): `TumblingEventTimeWindows.of(Duration.ofMinutes(1))`, keyed by entity type, allowed lateness 0, output `{window_start, window_end, entity_type, changes}`.

**Unit tests** (spec §16.2):
- **`VersionGateTest`** (keyed operator test harness): newer passes; equal and older are dropped; no state passes; state is per key.
- **`EntityDeserializerTest`:**
  - random bytes and a null value give invalid envelopes without throwing;
  - a key/id mismatch, an empty hostname, a kind that does not match the prefix, and a bad owner pattern are each invalid;
  - valid Asset, User and Group records decode.
- **`DorisRowsTest`:** column names, the datetime format in UTC, nulls and the kind name, for the three entities and change-rate rows.
- **`ChangeRateWindowTest`:** a MiniCluster with `fromData` and event timestamps, then `executeAndCollect`. It checks the per-type counts and the bounds of each window.
- **`DlqSerializerTest`:** the headers and the original bytes.
- **RowData mapping** (§16.2) belongs to the Iceberg branch (§10.7) and moves to sprint C. Recorded in build-progress.

**Verify:** `mvn -B verify` in the builder container (Step 6) is green, with tests and Spotless.

## Step 5 — Submitter image and script (spec §10.11, §10.6, §10.12, R14, R18)
- **`processor/Dockerfile`:**
  - **Stage 1:** pinned `maven:3.9.x-eclipse-temurin-17`, BuildKit cache mount on `/root/.m2`, `mvn -B -DskipTests package`. Tests run in `make test`; recorded.
  - **Stage 2:** `alpine:3.24.4` with `bash curl jq coreutils findutils`, user `flink` uid 9999, `/flink-data` owned by 9999, the jar at `/opt/adl/processor.jar`.
- `processor/.dockerignore` excludes `target/`.
- **`infra/flink/submit-job.sh`**, bind-mounted read-only into `flink-job`, the same way `kafka-init` mounts its script. The processor image's build context cannot reach `infra/`. Steps:
  1. Wait, up to 5 minutes, for `/overview` and for a TaskManager with free slots (`/taskmanagers`).
  2. If a job named `adl-processor` is CREATED, RUNNING or RESTARTING, exit 0.
  3. Choose the restore point: the newest `_metadata`, by `find -printf %T@` (sub-second), among `/flink-data/savepoints/*/_metadata` and `/flink-data/checkpoints/*/chk-*/_metadata`. Without one, start fresh.
  4. Run id: with a restore point, read `/flink-data/label-run-id`, and if it is missing, generate one and warn. Without a restore point, write `date -u +%Y%m%d%H%M%S`.
  5. `POST /jars/upload`, then `POST /jars/{id}/run` with `entryClass`, a `programArgsList` built from the environment with the §10.12 defaults and `--iceberg.enabled $ICEBERG_ENABLED`, `savepointPath` when restoring, and `allowNonRestoredState=false`. Delete the uploaded jar afterwards.
  6. Poll for RUNNING, up to 5 minutes. On failure, print `/jobs/{id}/exceptions` and exit 1.
  7. Keep the three newest savepoint directories and delete the rest.

  Its logs are JSON lines with `service=flink-job`, built with jq.
- **Compose `flink-job`:**
  - one-shot, image `adl/processor:0.1.0`;
  - depends on flink-jobmanager healthy, kafka-init completed and doris-init completed. Sprint C adds iceberg-rest and rustfs-init with `required: false`;
  - `ICEBERG_ENABLED: ${ICEBERG_ENABLED:-true}` (`make up-core` exports false), S3 keys from `.env`, volume `flink-data`, a small memory limit.
- **Verify:** see Step 9, the lifecycle checks.

## Step 6 — Makefile and CI (spec §15.9, §16.6)
- **`make test`:** replace the processor stub with `docker run --rm -v $(CURDIR)/processor:/workspace -v adl-maven-repo:/root/.m2 -w /workspace --memory 1536m -e MAVEN_OPTS=-Xmx512m <maven image> mvn -B test`.
  - Surefire's fork gets `-Xmx768m`.
  - The cache volume is not part of the compose project, so `make clean` keeps it.
- **`make lint`:** the same container running `mvn -B spotless:check`.
- **`MAVEN_IMAGE`** is a Makefile variable shared with `processor/Dockerfile`.
- `ci.yml` needs no change: the `java` and `images` jobs switch on by themselves. Check them with actionlint 1.7.12.
- **Verify:** `make test lint` is green with the stack up.

## Step 7 — `adlctl wait` and `make down` (spec §15.9, §15.10, §10.11)
- `adlctl wait --core` already evaluates `doris-init` and `flink-job` as one-shots, plus the BE and job rows. Check it live, and fix it in adlctl or labkit if a row misbehaves, for example the BE host column format.
- `make down` already calls `adlctl flink stop-with-savepoint`. Check it live: a savepoint is written under `/flink-data/savepoints`, the job ends FINISHED, and compose stops.

## Step 8 — `make reconcile` in core-only mode (G5, G1, spec §15.2, §16.5)
- **Conflict:** the plan's gate B runs `make reconcile` on a core-only stack, but spec §15.9 defines it with the lake included, and §15.2 says core-only mode has no lake.
- **Decision:** `adlctl reconcile` skips the lake comparison, printing why, when the compose service `iceberg-rest` has no running container: core-only mode, or a sprint before C. It still compares the lake when the lake profile runs, so a missing catalog in full mode still fails. `--no-lake` keeps working.
- Put the decision in a small pure function with a unit test. labkit's `Reconcile` is unchanged, and acceptance tests pass explicit options.
- Record it in the decision log (G5: spec and plan reconciled).

## Step 9 — Run the gate and iterate (plan §3.5, §3.6)
1. `make down` of the running sprint A stack (harmless), `make build`, `make clean`, `make up-core`. Record the time to ready.
2. `make test lint`.
3. `make acceptance-gate GATE=A`, then `make acceptance-gate GATE=B`, then `make reconcile`. Then the sprint 0 gate commands.
4. **Lifecycle checks**, not committed as tests:
   - `make down` writes a savepoint;
   - `make up-core` resubmits a job with a new id, and `/jobs/{id}/checkpoints` `latest.restored` points to that savepoint;
   - `label-run-id` is unchanged, there is no "Label Already Exists" error (R14), and `make reconcile` is green;
   - `docker compose kill flink-jobmanager`, then `make up-core`, restores from the newest checkpoint;
   - a TaskManager kill during `Burst(1000)` recovers, `change_rate_1m` shows no duplicates, and the reconcile passes (an early look at E03).
5. `docker stats --no-stream` at default load. Every container stays under its §15.4 limit.
6. On failures, apply G4: three written hypotheses before moving down a ladder (R1, R2, R3, R9, R13). Fix the cause in services or labkit, never in `acceptance/`. Finally, re-run the full gate from `make build && make clean`.

## Step 10 — Reports, audit, records, commits (plan §3.7)
1. Write `specs/sprints/sprint-B/build-report.md`: what was built, how to verify it, gate results, `docker stats`, and deviations from this plan.
2. **Audit subagent**, a fresh agent that did not build the sprint:
   - it reads the spec, the plan, this sprint's diff (`git diff sprint-A-done..HEAD`), the build report and the decision log;
   - it re-runs the gate;
   - it writes `specs/sprints/sprint-B/audit-report.md`, with findings ranked by severity and a table of every in-scope requirement.

   Fix every critical and major finding, then re-run the gate.
3. Update `specs/decision-log.md` (D-025 onward: pins, the Doris entrypoint mode, config syntax, the iceberg fail-fast, change-rate datetime format, the reconcile lake auto-skip, the submitter details, any R-rule applied), `specs/build-progress.md` and `specs/open-questions.md` if needed.
4. **Commits** (Conventional Commits, each with the Co-Authored-By trailer):
   - `feat(doris): …`
   - `feat(flink): …`
   - `feat(processor): …`
   - `feat(adlctl): …`
   - `build: …` for the Makefile
   - `fix: address the sprint B audit findings`
   - `docs(sprint-B): …`
   - `chore(sprint-B): close sprint`, last.

   No tags. Never touch locked files.

## Risks and the rule for each

| Risk | Rule | Mitigation |
|---|---|---|
| A connector fails on Flink 2.1.3 (missing class or API) | R1 | Confirm the error is a version error, with three hypotheses (G4). Then move the **whole** job to the §2.8 set (Flink 2.2.1 image and connectors). Record it |
| BE registers an unreachable address, or Stream Load cannot follow the FE redirect | R2 | Static IPs and entrypoint variables read in Step 1. Then `--doris.benodes=172.30.0.3:8040` with auto-redirect false |
| BE does not start (`max_map_count`, nofile, swap check) | R3 | `host-prep` runs inside `make up-core`, and nofile is 65536. Read `be.out` and fix the cause; never disable checks. Record it |
| The FE default heap (often 8 GB) causes an OOM in its 1792M container | R9, §11.2 | Override `JAVA_OPTS_FOR_JDK_17` to 1280m. On exit 137, follow the R9 order. Record `docker stats` |
| Core stack (~10.3 GiB of limits) plus the Maven builder during `make test` exceed the 11.6 GiB Docker VM | R9, §15.4 | Builder capped at 1536m with small heaps. Actual use is well below the limits. If memory is short, lower heaps inside each budget; never raise limits |
| Old Flink keys, or `FLINK_PROPERTIES` mishandled in 2.x `config.yaml` | R17 | Check every key in the JM startup log against the 2.1 reference |
| Protobuf or Jackson class conflicts in the shaded jar | R11 | Merge `META-INF/services`. Relocate `com.google.protobuf` on `NoSuchMethodError` |
| Jar upload rejected | R18 | `rest.server.max-content-length` is set. The sprint B jar has no Iceberg or Hadoop, so it stays small |
| "Label Already Exists" | R14 | A new run id on each fresh start, the same run id on restore. Never disable 2PC |
| MV cannot be created or refreshed | R13 | A plain view with the same name and columns. Record it |
| An identifier clashes in Doris | R15 | Backticks everywhere. Record it |
| Slow JVM start on the Intel Mac | R10 | §15.6 start periods; `adlctl wait` 15 min; the submitter waits up to 5 min |
| B08 or B09 timing (watermarks with idle partitions, 10 s checkpoints) | §1.5, §10.2 | `withIdleness(30 s)`. A 10 s checkpoint gives about 10–15 s visibility. Measured in Step 9 |
| Reconcile field mismatches from timestamp precision | §5.5, §16.5 | `DATETIME(3)` with formatting from epoch ms in UTC. Unit-tested |
| `make reconcile` requires the lake on a core-only stack | G5, G1 | Step 8 auto-skip, recorded |
| The privileged `host-prep` is blocked in auto mode | Plan §5 | The script already ran it. Run the compose and `adlctl wait --core` steps by hand to verify, and record it |
| A bug in sprint 0 or A code (labkit, adlctl, adapter) | Plan §3.3 | Fix it in this sprint without changing the stable labkit signatures. Record it in the build report |
| A test looks wrong | G2 | `open-questions.md` with the evidence. Never edit `acceptance/` |
