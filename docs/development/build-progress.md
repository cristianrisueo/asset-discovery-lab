# Build progress

External memory between build sessions (sprints plan §2). Read it right after the spec and the sprints plan.

## Status

| Sprint | Status | Report |
|---|---|---|
| 0 · Foundations and the test contract | **Done** (gate green, audited, committed) | `specs/sprints/sprint-0/` |
| A · Ingestion | **Done** (gate green, audited, committed) | `specs/sprints/sprint-A/` |
| B · Processing | **Done** (gate green, audited, committed) | `specs/sprints/sprint-B/` |
| C · Lake | **Done** (gate green, audited, committed) | `specs/sprints/sprint-C/` |
| D · Serving | **Done** (gate green, audited, committed) | `specs/sprints/sprint-D/` |
| E · Failures and close | Next | — |

## What exists after sprint 0

- **Contracts:** `proto/adl/v1/` and `buf.yaml`, with generated code in `gen/go/adl/v1` and `processor/src/generated/java`. Regenerate with `make generate`.
- **One Go module:** `github.com/cristianrisueo/asset-discovery-lab`, `go 1.26.11`.
- **`labkit/`:** clients for Kafka, Dragonfly, datacore (both gRPC services), Doris over MySQL, S3, the Iceberg REST catalog, Flink REST, HTTP, docker compose and make. Also §6.7 validation, §16.5 reconciliation (`labkit.Reconcile`, `(*Lab).Reconcile`), `Poll` and the wait evaluation. It reads configuration through `labkit.ConfigFromEnv()` (the ADL_* variables of §16.3, plus `S3_*` and `ADL_REPO_ROOT`).
- **`adlctl/`:** every §15.10 subcommand. With an empty compose file, `adlctl wait` succeeds at once. It only checks services that are defined, plus the BE when `doris-be` is defined and the Flink job when `flink-jobmanager` is defined.
- **`acceptance/`:** the 38 cases, red. **LOCKED after the `sprint-0-done` tag: never edit it.**
- **`Makefile`:**
  - every §15.9 target exists;
  - `build`, `up` and `up-core` are stubs that sprint A replaces;
  - `test` and `lint` skip the Java, Rust and web parts until `processor/pom.xml`, `explorer/Cargo.toml` and `web/package.json` exist. When those files appear, the stub branch prints "not available until sprint X" and fails, so the sprint that adds the component must replace it.
- **`docker-compose.yml`:** only `name: adl` and the `adl` network. Add services sprint by sprint, in their final profile (§15.2).
- **CI:** `ci.yml` enables the java, rust, web and images jobs automatically once their files exist. `e2e.yml` only passes from sprint E.

## What exists after sprint A

- **Compose, profile `core`:**
  - `kafka` (KRaft, host port 29092, `kafka-data` volume);
  - `kafka-init`, one-shot, running `infra/scripts/create-topics.sh` for the six §8.2 topics; it is idempotent;
  - `dragonfly` (6379);
  - `datacore` (50051, 8090, `datacore-data`);
  - `adapter` (8091).
  All of them have compose health checks and §15.4 memory limits.
- **`make build`, `make up` and `make up-core` are real.** `make up-core` reaches ready in about 35 s from clean.
- **`datacore/`:** SQLite (`modernc.org/sqlite` v1.63.0) with the transactional outbox, the deterministic seed (about 0.1 s), the generator, chaos, the relay, `DataCoreService` and `AdminService`, and gRPC reflection.
  - Packages: `internal/{model,store,chaos,generator,outbox,grpcapi}`.
  - `datacore healthcheck` calls `/readyz`.
- **`adapter/`:**
  - Packages: `internal/{contract,consumer,handler,fetcher,cache,publisher}`.
  - Manual commits after acks; one sequential goroutine per partition.
  - `adapter healthcheck` calls `/healthz`.
- **`infra/scripts/`:** `create-topics.sh` and `host-prep.sh`.
- **Gate A:** 10/10 green, in about 15 s.

## What exists after sprint B

- **Compose, profile `core`, adds:**
  - `doris-fe` (static IP 172.30.0.2, host ports 8030 and 9030, volume `doris-fe-meta`);
  - `doris-be` (172.30.0.3, no host ports, volume `doris-be-storage`, nofile 65536);
  - `doris-init`, one-shot `mysql:8.4.13`;
  - `flink-jobmanager` (8081), `flink-taskmanager` (2 slots) and the one-shot `flink-job`, all sharing volume `flink-data`.

  The §10.9 Flink configuration lives once, as the `x-flink-properties` YAML anchor.
- **`make up-core`** reaches ready in about 45 s from clean. `adlctl wait --core` shows the BE row (`172.30.0.3 alive=true`) and the job row (RUNNING).
- **`infra/doris/`:** `fe.Dockerfile` and `be.Dockerfile` (§11.2 overrides by sed on the default confs) and `init-core.sql` (§11.3–§11.5, §11.7).
- **`infra/scripts/doris-init.sh <sql-file>`:** waits for a live BE, substitutes `<NAME>` placeholders from the environment, and applies the file. Sprint C reuses it for `init-lake.sql`.
- **`infra/flink/`:** the cluster `Dockerfile`, `log4j-console.properties` (JSON console logs with `service`), and `submit-job.sh`, which implements §10.11 and is bind-mounted into `flink-job`.
- **`processor/`:** Maven project (Flink 2.1.3, Kafka connector 5.0.0-2.1, Doris connector 26.3.0, protobuf-java 4.36.2), 25 unit tests, Spotless with google-java-format 1.28.0. The shaded jar is about 75 MB. Packages: `decode`, `gate`, `sink`, `window`; the entry point is `ProcessorJob`.
- **`make test` and `make lint`** run Maven in `maven:3.9.16-eclipse-temurin-17`, with the cache volume `adl-maven-repo`, outside the compose project.
- **`adlctl reconcile`** (and so `make reconcile`) skips the lake comparison when no `iceberg-rest` container exists (D-032, refined by D-034).
- **Gates:** A is 10/10 and B is 9/9 (about 5 minutes; B09 p95 freshness is about 10 s).

## What exists after sprint C

- **Compose, profile `lake`, adds:**
  - `rustfs` (`rustfs/rustfs:1.0.6`, host ports 9000 and 9001, volume `rustfs-data`);
  - `rustfs-init`, one-shot `amazon/aws-cli:2.42.4`, running `infra/scripts/create-bucket.sh` (bucket `warehouse`, idempotent);
  - `iceberg-rest` (`apache/iceberg-rest-fixture:1.10.1`, 8181, volume `iceberg-rest-data` at `/tmp`);
  - `doris-init-lake`, one-shot, running `infra/doris/init-lake.sql` (catalog `lake` plus `GRANT SELECT_PRIV ON lake.*.*`) through `doris-init.sh`.

  `flink-job` depends on `iceberg-rest` and `rustfs-init` with `required: false`, so `make up-core` still works.
- **`make up`** (full stack) reaches ready in about 80 s from clean. `make reconcile` now compares the lake (`lake_assets=…` in the PASS line).
- **`processor/`:**
  - the Iceberg branch: package `lake` (`LakeTable`, `LakeCatalog`, `LakeRows`);
  - `main()` creates `inventory.asset_changes` on the JobManager;
  - `FlinkSink.forRowData` with uid prefix `iceberg-sink`, write parallelism 1;
  - `ProcessorJob.build(env, options, tableLoader)`, where a null loader means core-only mode;
  - Iceberg 1.12.0, the AWS bundle and Hadoop 3.4.3 are shaded, so the jar is 222 MiB;
  - 37 unit tests.
- **Doris catalog `lake`:** uses `use_path_style`, not the spec's `s3.use_path_style`, which 4.1.5 ignores (D-037). Lake data is visible through Doris about 10–20 s after a change.
- **`doris-fe`:** has `MALLOC_ARENA_MAX=2` (D-038).
- **Gates:** A is 10/10, B is 9/9 (B09 p95 about 10.2 s), C is 5/5 (C02 about 13–19 s).

## What exists after sprint D

- **Compose, profile `app`, adds:**
  - `explorer` (`adl/explorer:0.1.0`, host port 8080, 192M, `explorer healthcheck`);
  - `web` (`adl/web:0.1.0`, host port 3000 → 80, 64M, `wget` health check).

  `explorer` depends on `doris-init` completed, and on `doris-init-lake` completed with `required: false`. Its `LAKE_ENABLED` is `${ICEBERG_ENABLED:-true}`. `web` depends on a healthy explorer. The long-running limits total exactly 11,456 MiB (§15.4).
- **`make up`** (full stack) reaches ready in about 100 s from clean.
- **`explorer/`** (Rust 1.100.0 toolchain; axum 0.8.9, tokio 1.54.1, arrow-flight 60.2.0, tonic 0.14.9):
  - flat `src/`: `api`, `service`, `flight`, `convert`, `validate`, `sql`, `error`, `config`, `logging`, `health`, `main`;
  - 24 unit tests.
  - **R4: step 1 works (D-040).** Handshake with `doris-fe:8070`, `GetFlightInfo`, then `DoGet` against `grpc+tcp://172.30.0.3:8050`, translated to `http://`, with the bearer token. The token survives transport errors; only `UNAUTHENTICATED` triggers a new handshake (D-045).
  - `/readyz` reports `doris_transport: flight-sql`.
  - `offset` is capped at 10⁹, because of a Doris BIGINT-overflow hang (D-045).
- **`web/`** (React 19.3, Vite 8.3, TypeScript 5.9, Vitest 5.1, ESLint 10, Prettier):
  - asset page (LT-0042 prefilled, card refreshed every 5 s, history table) and stats page (OS × site matrix, change-rate bars);
  - 7 tests.
  - `web/nginx.conf`: proxies `/api/` to `explorer:8080` through Docker DNS (re-resolved), falls back to `index.html`, listens on IPv4 and IPv6, uses `absolute_redirect off`, and writes a JSON access log.
- **`make test` and `make lint`** run Cargo in `adl/explorer-toolchain:0.1.0`, the `toolchain` stage of `explorer/Dockerfile`, and npm in `node:24.22.3-bookworm-slim`. They use cache volumes `adl-cargo-registry`, `adl-cargo-target`, `adl-web-node-modules` and `adl-npm-cache`. With warm caches, `make test lint` takes about 1.5 minutes with the stack up.
- **Doris FE heap is 1024m**, not §11.2's 1280m (R9, D-044). `docker stats` after the gate: FE 1.43 GiB / 1.75 GiB, with no `max` events.
- **Gates:** A 10/10, B 9/9 (B09 p95 about 10.2 s), C 5/5, D 8/8 (D03 lake history about 20 s).

## Key decisions (see `specs/decision-log.md`)

- D-002: kadm v1.20.0, a separate module from franz-go v1.22.3.
- D-003: `go 1.26.11` in `go.mod`; the local toolchain is 1.27.2.
- D-004: the processor must use `protobuf-java` **4.36.2**.
- D-005: buf and golangci-lint run through pinned `go run` in the Makefile.
- D-011 and D-012: acceptance harness rules and the interpretations of §16.4 rows.
- D-014: sprint A pins: sqlite v1.63.0, `golang:1.26.11-bookworm`, distroless static nonroot, local image tags `adl/<svc>:0.1.0`.
- D-017: the datacore SQLite design: a single connection, so seq order equals commit order.
- D-018: the admin API rejects LT-9999 and LT-8000 to LT-8999.
- D-022: adapter delivery details: commit after ack, `ErrIncomplete` exit, `CloseAllowingRebalance`, 7 s shutdown grace.
- D-023: the Go images build from the repository root; `.dockerignore` excludes `processor`, `explorer`, `web` and `infra`.
- D-025: sprint B pins. google-java-format is 1.28.0, the newest that Spotless allows on JDK 17. JUnit is 5.14.4 through its BOM, aligned with Flink's test utilities.
- D-026: the Doris election-mode entrypoints. The FE default heap of 8 GB is overridden to 1280m.
- D-028: the processor design. The Envelope is a Flink POJO, decoding happens in the deserializer, and every operator has a uid.
- D-029: `--iceberg.enabled=true` fails fast until sprint C.
- D-030: the submitter details.
- D-032 and D-034: `adlctl reconcile` skips the lake in core-only mode. D-034 also covers the audit fixes: parallelism passed explicitly, the idempotency check before the slot wait.
- D-035: sprint C pins (Hadoop 3.4.3, aws-cli 2.42.4) and image checks.
- D-036: the processor lake design. Iceberg names its writer and committer from the uid prefix; `IcebergAssetChanges` names the terminal sink.
- D-037: R6. The Doris catalog uses `use_path_style`.
- D-038: R9, preventive. FE malloc arenas capped.
- D-039: sprint D pins (Rust 1.100.0, arrow-flight 60.2.0, Node 24.22.3, nginx 1.30.8-alpine, TypeScript 5.9.3 because the spec says 5.x).
- D-040: R4. Flight SQL step 1 works, with evidence and the Arrow types Doris sends.
- D-041 and D-042: explorer and web design details (error mapping, `lake_unavailable`, JSON logs, nginx).
- D-043: Cargo and npm builder containers.
- D-044: R9. FE heap lowered from 1280m to 1024m, with `docker stats` before and after.
- D-045: sprint D audit fixes: `offset` cap, token kept across transport errors, nginx relative redirects, `/healthz` logged at debug.

## Next: sprint E (Failures and close)

Plan §4, sprint E:
- **Make gate E pass,** fixing earlier components where needed.
- **Check `e2e.yml` end to end.**
- **Write the README** (§17.2). Include:
  - the R4 outcome (step 1, no deviation);
  - the FE heap deviation (D-044) in the memory or troubleshooting section;
  - the D-037 `use_path_style` note;
  - `docker compose` commands for `explorer` and `web` need the profile flags. Use `make logs SERVICE=explorer`, or `docker compose --profile core --profile lake --profile app …`; without them, `stop` and `logs` fail with "no such service: doris-init" (audit F-08).
- **Record the `docker stats` snapshot** in the decision log (§18 item 7).
- **Walk the LT-0042 walkthrough** with `make move`, the Flink UI, Doris and the web (`http://localhost:3000`), and record it. Do not assume seed values (sprint A audit F-10).
- **The audit is of the whole repository.**

## Known issues and notes

- **E04:** the submitter pins no `jobId`, so the job resubmitted after `make down` and `make up-core` gets a new id and restores from the savepoint. This was checked by hand in sprint B.
- **The Doris FE is still the tightest container** (sprint D audit F-04).
  - With the 1024m heap (D-044), `memory.peak` reached about 83–88% of 1792 MiB. A fully touched heap leaves a margin of about 130–170 MB.
  - After the sprint E soak, record `memory.peak`, `memory.events` and the FE's `RestartCount`.
  - If `max` events or an OOM kill appear, apply R9's next step within the FE budget: cap `-XX:MaxDirectMemorySize`, or reduce the code cache or thread stacks. Moving budget between services comes only after that.
- **Switching between `make up-core` and `make up` needs `make clean`** (§15.2).
- **The submitter passes the cluster's `parallelism.default` (2) explicitly** (D-034). Flink's REST run handler would otherwise use 1.
- **The acceptance precheck** dials only the host ports of §15.8. Every service must publish exactly those ports.
- **The generator changes LT-0042, U-0117 and G-03 too** (allowed by §5.4). The sprint E walkthrough and README must not assume the seed values; for example, pause the generator or say "from its current site" (sprint A audit F-10).
- **The adapter exits on purpose** when a produce fails outside shutdown (`ErrIncomplete`), and compose restarts it. A restart loop in its logs points at Kafka, not at the adapter.
- **Open questions:** Q-001 (interpretations of A05, B08, D04, E04) and Q-002 (how kadm is versioned). Sprints A to D raised no new ones.
- **Explorer under failures:** the explorer answers 503 `upstream_unavailable` while Doris is unreachable, and recovers on the next request. nginx re-resolves the explorer after a restart. A Doris BE restart was survived during the sprint D audit.
- **Unknown query parameters are ignored by the explorer on purpose** (D-045).
