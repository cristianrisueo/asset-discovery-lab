# Sprint C plan: Lake

## Context

Sprints 0, A and B are done and tagged (`sprint-B-done`). The core pipeline works: datacore → Kafka → adapter → Flink → Doris, exactly once. The lake is still missing. `ProcessorJob.main` fails fast when `--iceberg.enabled=true` (D-029), so `make up` fails at `flink-job`. The compose file has no lake services. `make reconcile` skips the lake because no `iceberg-rest` container exists (D-032/D-034).

Sprint C (plan §4) has to:
- land every accepted asset version in Iceberg on RustFS;
- let Doris read the lake through the catalog `lake`;
- make `make up` (full stack) work from clean.

Gate (plan §4, sprint C), plus the sprint 0 checks, because every gate re-runs the earlier ones:
```sh
make build && make clean && make up
make test lint
make acceptance-gate GATE=A && make acceptance-gate GATE=B && make acceptance-gate GATE=C
make reconcile
# sprint 0: buf lint && buf build; go vet ./...; go build ./...; go vet -tags acceptance ./acceptance/...; 38-case count; go test ./...
```

Locked: `specs/system-spec.md`, `specs/sprints-plan.md`, `CLAUDE.md`, `run-sprints.sh` and `acceptance/`. None of them is edited. The next decision id is D-035, the next question id Q-003.

## What already exists and gets reused

- `infra/scripts/doris-init.sh <sql-file>`: waits for a live BE, substitutes `<NAME>` placeholders from the environment, applies the file. `doris-init-lake` reuses it unchanged except for a log service name taken from the environment.
- `infra/flink/submit-job.sh` already passes `--iceberg.enabled "$ICEBERG_ENABLED"`, `--iceberg.uri`, `--iceberg.warehouse`, `--s3.*` and the S3 keys. `flink-job` already receives `S3_ACCESS_KEY`, `S3_SECRET_KEY` and `S3_REGION`. Its `rest_slow` (300 s) already covers a slower `/run`, since `main()` will now create the table.
- `JobOptions` already parses every §10.12 Iceberg and S3 argument.
- `ProcessorJob.OfType`, the filter-then-map pattern of `entitySink`, and `DorisRows`' null rules: `LakeRows` mirrors them.
- labkit already has `Catalog.LoadTable`, `S3.ListKeys`, `Doris.LakeVersions/LakeMaxVersions/LakeDuplicates`, `CompareLake`, and `ConfigFromEnv` S3 defaults that match `.env.example`. `adlctl wait` already evaluates every defined service (one-shot means `restart: "no"`). `adlctl reconcile` compares the lake as soon as an `iceberg-rest` container exists. **No Go changes are expected.**
- The `Makefile` already has `up` with all three profiles, `clean` with `--volumes`, and Maven test and lint in the builder container. **No Makefile changes are expected.**
- CI `images` detects build contexts from `docker compose config`. The lake services use upstream images, so nothing changes there.

## Steps

### 0. Save the plan (auto mode, first action)
Save this plan verbatim to `specs/sprints/sprint-C/sprint-plan.md`.

### 1. Check the upstream images and pin the "latest at build" versions (spec §2.1, §2.3, §2.4, G3)
Read-only probes, before writing any compose:
- `rustfs/rustfs:1.0.6`:
  - does it have `curl`, `wget` or a shell for the health check?
  - the user id (10001?) and the owner of `/data`;
  - the env names (`RUSTFS_VOLUMES`, `RUSTFS_ADDRESS`, `RUSTFS_CONSOLE_ENABLE`, the console address);
  - does `/health` answer?
- `apache/iceberg-rest-fixture:1.10.1`: does it have curl, which user it runs as, the image's own HEALTHCHECK, and the default `CATALOG_URI` (the SQLite file in `/tmp`, R8).
- `flink:2.1.3-java17`: which `/opt/flink/lib` jars exist (`flink-table-*` must be there at runtime for `RowData` and `InternalTypeInfo`).
- Pins from Maven Central and Docker Hub:
  - `hadoop-client-api` and `hadoop-client-runtime`: newest 3.4.x;
  - `amazon/aws-cli`: newest 2.x tag;
  - confirm that `iceberg-flink-runtime-2.1:1.12.0` and `iceberg-aws-bundle:1.12.0` exist;
  - `flink-table-api-java-bridge` and `flink-table-runtime` 2.1.3, both `provided`.
- Record everything in **D-035** ("latest at build" pins for sprint C).

**Verification:** each probe's output is cited in D-035.

### 2. Lake services in compose, profile `lake` (spec §12.1, §12.2, §15.2–§15.8, R8)
Edit `docker-compose.yml` and add four services and two volumes:
- **`rustfs`:**
  - `rustfs/rustfs:1.0.6`;
  - the §12.1 env, with keys from `.env`;
  - ports 9000 and 9001, volume `rustfs-data:/data`;
  - health check `curl -sf http://localhost:9000/health`, 30 s / 5 s / 20, or whatever tool the image has (§15.6);
  - `mem_limit` 384M, `restart: unless-stopped`.
- **`rustfs-init`:**
  - pinned `amazon/aws-cli`, `restart: "no"`;
  - depends on `rustfs` healthy;
  - runs the new `infra/scripts/create-bucket.sh`, mounted read-only. The script:
    - sets path-style addressing with `aws configure set default.s3.addressing_style path`;
    - checks the bucket with `head-bucket`, then runs `s3api create-bucket --bucket warehouse --endpoint-url http://rustfs:9000`;
    - treats `BucketAlreadyOwnedByYou` and `BucketAlreadyExists` as success;
    - logs JSON lines with `service=rustfs-init`;
    - is idempotent.
  - Small memory limit (256M), like `doris-init`.
- **`iceberg-rest`:**
  - `apache/iceberg-rest-fixture:1.10.1`;
  - the §12.2 env verbatim, with keys from `.env`;
  - port 8181, volume `iceberg-rest-data:/tmp` (R8);
  - depends on `rustfs-init` completed;
  - health check `curl -sf http://localhost:8181/v1/config`, 60 s / 5 s / 20;
  - 512M, unless-stopped.
- **`doris-init-lake`:**
  - `mysql:8.4.13`, `restart: "no"`;
  - depends on `doris-init` completed and `iceberg-rest` healthy;
  - env `S3_ACCESS_KEY` and `S3_SECRET_KEY`, plus `ADL_SERVICE=doris-init-lake`;
  - mounts `doris-init.sh` and `init-lake.sql`;
  - entrypoint `bash /opt/adl/doris-init.sh /opt/adl/init-lake.sql`; 256M.
- **`flink-job`:** add `iceberg-rest: service_healthy, required: false` and `rustfs-init: service_completed_successfully, required: false` (§15.2).
- **`infra/scripts/doris-init.sh`:** the log `service` comes from `${ADL_SERVICE:-doris-init}`.

**Verification:**
- `docker compose --profile core --profile lake --profile app config` is valid;
- `docker compose --profile core config` is also valid, so `make up-core` still resolves with the optional dependencies left out;
- the memory limits add up within §15.4.

### 3. `infra/doris/init-lake.sql` (spec §11.6, §11.7, R6)
- The §11.6 `CREATE CATALOG IF NOT EXISTS lake` verbatim, with `<S3_ACCESS_KEY>` and `<S3_SECRET_KEY>` placeholders.
- Then `GRANT SELECT_PRIV ON lake.*.* TO 'explorer'@'%';`.
- No placeholder-shaped words in comments (the sprint B lesson).

**Verification:**
- `SHOW CREATE CATALOG lake` shows the properties, including `s3.use_path_style` and `meta.cache.iceberg.table.ttl-second`;
- `SHOW GRANTS FOR 'explorer'@'%'` shows the lake grant;
- re-running `doris-init-lake` exits 0.
- R6: if a property is rejected, try `use_path_style`, record it, and note the explorer REFRESH fallback for sprint D.

### 4. Processor dependencies (spec §2.4, R7, R11, R18)
`processor/pom.xml`:
- add `iceberg-flink-runtime-2.1:1.12.0`, `iceberg-aws-bundle:1.12.0`, and `hadoop-client-api` plus `hadoop-client-runtime` at the step 1 pins, all compile scope and shaded;
- add `flink-table-api-java-bridge` and `flink-table-runtime` 2.1.3 as `provided`, for `RowData`, `GenericRowData` and `InternalTypeInfo`;
- keep the `ServicesResourceTransformer`; exclude signature files as today.

**Verification:**
- `make test` compiles;
- the shaded jar size is recorded (expected about 200 to 250 MB, under the 512 MiB limit);
- `mvn dependency:tree` shows no second Flink minor version (R1).

### 5. The Iceberg branch of the job (spec §10.1, §10.7, §10.10, §10.12, §12.3)
New package `processor/src/main/java/.../processor/lake/`, with three or more files (no single-file directory):
- **`LakeTable`**:
  - the §12.3 schema: ids 1 to 9; required and optional as in the table; `string`, `long` and `timestamptz` via `TimestampType.withZone()`;
  - `PartitionSpec` `day(changed_at)`;
  - properties `format-version=2`, `write.format.default=parquet` and `write.parquet.compression-codec=zstd`;
  - identifiers `inventory` and `asset_changes`.
- **`LakeCatalog`**:
  - builds the §10.7 property map (`uri`, `warehouse`, `io-impl`, `s3.endpoint`, `s3.path-style-access=true`, `s3.access-key-id`, `s3.secret-access-key`, `client.region`) from `JobOptions`;
  - fails fast with a clear message when the S3 keys are empty;
  - `CatalogLoader.rest("lake", new Configuration(), props)`;
  - `ensureTable()`: creates the namespace `inventory` and the table if missing, treating `AlreadyExistsException` as success (idempotent, and safe against a race);
  - returns `TableLoader.fromCatalog(loader, id)`;
  - logs the table location.
- **`LakeRows`**:
  - `RowData` mapping `Envelope → GenericRowData`, in schema order;
  - `StringData` for strings, and `null` for an empty `owner_user_id`;
  - `TimestampData.fromEpochMillis` for `changed_at`, and for `ingested_at` at mapping time;
  - `ROW_TYPE = FlinkSchemaUtil.convert(LakeTable.SCHEMA)` and `TYPE_INFO = InternalTypeInfo.of(ROW_TYPE)`, so the row type is derived from the table schema and the two cannot diverge;
  - a `MapFunction` class.
- **`ProcessorJob`**:
  - remove the D-029 fail-fast;
  - `main()`: when `icebergEnabled`, run `LakeCatalog.ensureTable(o)` before building the graph. This runs on the JobManager (§10.12 note);
  - `build(env, o)` becomes `build(env, o, TableLoader lake)`. A null `lake` means core-only mode with no branch (`--iceberg.enabled=false`);
  - the branch: `accepted.filter(OfType(ASSET))` (name `OnlyAssetChanges`, uid `filter-asset-changes`) → `map(LakeRows)` with `.returns(TYPE_INFO)` (name `RowDataAssetChanges`, uid `rowdata-asset-changes`) → `FlinkSink.forRowData(...).tableLoader(lake).uidPrefix("iceberg-sink").writeParallelism(1).append()`;
  - `.name("IcebergAssetChanges")` on the returned sink.
  - Iceberg derives its writer and committer operator names from the uid prefix. If the §10.10 name cannot be applied to them, record it as a G1 note in D-036.
- **D-036**: the processor's lake design. It covers:
  - the table created in `main()`;
  - the `TableLoader` injected into `build`;
  - the row type derived from the schema;
  - the operator names;
  - removing D-029.

**Verification:** step 6 tests, then live in step 8: the vertices in `/jobs/<id>` include the Iceberg writer and committer, and the job is RUNNING with `iceberg=true`.

### 6. Unit tests (spec §16.2, plan §3.5; sprint B audit F-08)
- **`LakeRowsTest`:**
  - field order and types against `ROW_TYPE`;
  - strings equal the envelope's fields;
  - an empty owner becomes null;
  - `changed_at` and `ingested_at` are exact `TimestampData` values for the Appendix A example;
  - `kind` is a name such as `LAPTOP`.
- **`LakeTableTest`:**
  - the schema equals §12.3 (names, ids, types, required);
  - the partition spec is `day(changed_at)`;
  - the three properties are set.
- **`LakeCatalogTest`:** the property map has exactly the §10.7 keys and values from `JobOptions`, and empty keys are rejected.
- **`ProcessorJobTest`:**
  - the existing names and uids test runs with `--iceberg.enabled false`, with `lake=null`;
  - a new test builds with a `TableLoader.fromHadoopTable(tmpDir)` table created from `LakeTable`, a local Hadoop FS with no network, and asserts the `IcebergAssetChanges` sink and the operators with the `iceberg-sink` uid prefix;
  - a core-only test asserts that no Iceberg operator exists.
- `mvn spotless:apply`, then check.

**Verification:** `make test lint` is green and the number of Maven tests goes up from 25.

### 7. Build from clean, full stack
`make down` if a stack is running, then `make build && make clean && make up`.

**Verification:**
- `adlctl wait` is ready and shows `rustfs`, `iceberg-rest` healthy; `rustfs-init`, `doris-init-lake` completed; and the job RUNNING;
- the submitter log shows `iceberg=true`;
- `curl localhost:8181/v1/namespaces/inventory/tables/asset_changes` returns 200;
- `aws s3 ls` through labkit, or the C03 test, shows Parquet files;
- `SELECT COUNT(*) FROM lake.inventory.asset_changes` reaches about 3000 or more within a minute or two;
- record the time `make up` takes to reach ready.

### 8. Gates and lifecycle check
- Run the full sprint C gate and the sprint 0 checks, in order.
- Record:
  - pass counts and durations;
  - B09 freshness under the extra Iceberg load;
  - lake freshness in C02 (spec §1.5: at most 60 s at p95);
  - `docker stats --no-stream`, full stack at default load (watch `doris-fe`, sprint B F-03; and the TaskManager with the Iceberg writer).
- Extra manual check, not a gate step: `make down && make up`.
  - The job restores from the savepoint with the Iceberg committer state.
  - Then `make reconcile`, with the lake, is green, and C04 has no duplicates.
  - This is an early look at E04 and E03 for the lake, because sprint E depends on it.

### 9. Reports, audit, fixes, progress, commits (plan §3.7)
1. Write `specs/sprints/sprint-C/build-report.md`: what was built (table with the spec sections), how to verify, the results, deviations from this plan, and notes for sprint D.
2. Decision log: D-035 (pins), D-036 (processor lake design), plus one entry for every R-rule or fallback actually applied (R6, R7, R8, R9, R18 and others as they happen) and every G1 choice (for example a RustFS health check or volume workaround). Each entry has the date, the rule, the symptom, the evidence, the decision and the diagram impact.
3. **Audit:** a fresh `general-purpose` subagent that did not build the sprint.
   - It reads the spec, the sprints plan and `git diff sprint-B-done..HEAD`.
   - It re-runs the full gate.
   - It inspects the live stack: catalog, bucket, Doris catalog and grants, Flink vertices, memory.
   - It writes `specs/sprints/sprint-C/audit-report.md`, with findings by severity and a table of the in-scope requirements.
   - It gets the code and the spec, not my conclusions.
4. Fix every critical and major finding, re-run the gate from `make build && make clean && make up`, and add a fix-status section to the audit report.
5. Update `specs/build-progress.md`: sprint C done, "What exists after sprint C", known issues, and "Next: sprint D". Doubts go to `specs/open-questions.md` as Q-003 and later, if any appear.
6. Commits, Conventional Commits, no tags:
   - `feat(lake): add rustfs, rustfs-init, iceberg-rest and doris-init-lake`;
   - `feat(processor): add the Iceberg branch and the lake table`;
   - `test(processor): …`;
   - `docs(sprint-C): …`;
   - `fix: address the sprint C audit findings`;
   - finally `chore(sprint-C): close sprint`.
   - Check `git status` is clean and the locked files have no diff against `plan-v1` and `sprint-0-done`.

## Files

| File | Change |
|---|---|
| `docker-compose.yml` | Add `rustfs`, `rustfs-init`, `iceberg-rest` and `doris-init-lake` (profile `lake`), the volumes `rustfs-data` and `iceberg-rest-data`, and the optional `flink-job` dependencies |
| `infra/scripts/create-bucket.sh` | New: idempotent `warehouse` bucket creation |
| `infra/scripts/doris-init.sh` | Log service name from `ADL_SERVICE` |
| `infra/doris/init-lake.sql` | New: §11.6 catalog plus the lake grant |
| `processor/pom.xml` | Iceberg, AWS bundle and Hadoop client (shaded); flink-table `provided` |
| `processor/src/main/java/.../lake/{LakeTable,LakeCatalog,LakeRows}.java` | New |
| `processor/src/main/java/.../ProcessorJob.java` | Remove the fail-fast; add the Iceberg branch; `build(env, o, lake)` |
| `processor/src/test/java/.../lake/*Test.java`, `ProcessorJobTest.java` | New and updated tests |
| `specs/sprints/sprint-C/{sprint-plan,build-report,audit-report}.md`, `specs/decision-log.md`, `specs/build-progress.md`, maybe `specs/open-questions.md` | Reports |

## Risks and the rule for each

| Risk | Rule | Action |
|---|---|---|
| `iceberg-flink-runtime-2.1:1.12.0` fails against Flink 2.1.3 for a version reason (missing class or API) | R1, G4 | Three written hypotheses first (classloading, shading, provided scope). Then move the **whole** job to the §2.8 fallback set (Flink 2.2.1, matching connectors and image) and record it |
| Hadoop `Configuration` missing, or S3FileIO fails to pick an HTTP client | R7 | Hadoop jars are shaded from the start. On an HTTP client error, set `http-client.type=urlconnection` in the catalog properties (or add `apache5-client`) and record it |
| `FlinkSink` unavailable or broken on Flink 2.1 | R7 | Only then use `IcebergSink` (SinkV2) with the same uid prefix and name, and record it |
| Fixture 1.10.1 incompatible with 1.12 clients | R8 | Use the `latest` tag pinned by digest and record it |
| Doris rejects `s3.use_path_style` or the cache TTL, or history lags more than 60 s; Doris caches the database list from before the job created `inventory` | R6, G1 | Try `use_path_style`. Record the TTL fallback (explorer REFRESH, sprint D). For the list cache, look for a catalog refresh property in the 4.1.5 docs, record it, and never touch the tests |
| RustFS cannot serve Flink, Iceberg and Doris | R6, G4 | After three hypotheses, switch to SeaweedFS (latest at build), same bucket and paths. This changes a diagram label, so record it |
| The RustFS or fixture image has no curl, or `/data` is not owned by 10001 | §15.6, §15.7, G1 | Use wget, bash `/dev/tcp` or the image's own probe. For ownership, a one-shot chown init in profile `lake` (§15.7 allows it). Record it |
| The job jar is too big to upload | R18 | Check `rest.server.max-content-length` (already 512 MiB). Then move Iceberg, AWS and Hadoop into `/opt/flink/lib` of `infra/flink` and keep the job jar thin |
| Protobuf or other class conflicts after adding the bundles | R11 | Relocate `com.google.protobuf` in the shade plugin |
| OOM: the FE is already near its limit (sprint B F-03); the TaskManager now runs a Parquet writer; the JobManager loads a bigger jar in `main()` | R9 | Record `docker stats` before and after. On exit 137: lower the heap or managed memory inside that container's budget, then parallelism 1, then move budget. Total ≤ 11.5 GiB |
| Slow JVM starts (fixture, JobManager `main()` creating the table) | R10 | Generous start periods; the submitter's `/run` already allows 300 s |
| Outdated Flink idioms in new code | R17 | Only Flink 2.x APIs; no new config keys |
| Native zstd (zstd-jni) fails in the TaskManager or Doris cannot read it | G4, then G1 | Check that the runtime bundles zstd-jni. If zstd cannot work after G4, record it and raise Q-003 before changing the codec. The §12.3 property is a spec value, so it stays unless it truly cannot work |
| `required: false` dependencies make `docker compose --profile core` fail | §15.2, G1 | Check with `config` and `up-core`. If needed, keep the spec semantics and record the compose version behaviour |
| Gate A or B slows down under the extra load (B09 p95 ≤ 30 s) | §1.5 | Measure. The checkpoint interval stays 10 s; the Iceberg commit is part of the checkpoint |
| A gate C test looks wrong | G2 | Never edit `acceptance/`. Write it to `open-questions.md` and keep working |

## Verification summary

- **Unit:** `make test lint` (Go plus Maven with the new lake tests, Spotless, golangci-lint, buf).
- **Live:**
  - `make build && make clean && make up` reaches ready;
  - gates A, B and C pass;
  - `make reconcile` compares the lake (no "skipped" line) and passes;
  - the sprint 0 checks pass;
  - `make down && make up`, then reconcile, passes;
  - `docker stats` is recorded.
- **Audit:** an independent subagent re-runs everything; critical and major findings are fixed and the gate is re-run.
- **Close:** clean tree, locked files untouched, last commit `chore(sprint-C): close sprint`. Sprint D is not started.
