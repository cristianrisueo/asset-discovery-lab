# v0.1.0 verification report

| | |
|---|---|
| Verified commit | `f14d7d7` (`v0.1.0` = `sprint-E-done`) |
| Date | 2026-11-26 |
| Verified against | `specs/system-spec.md` v1.0, `specs/decision-log.md` (D-001 to D-050), `specs/open-questions.md` (Q-001 to Q-003) |
| Machine | Intel Core i7-8700, 16 GB, macOS 15.7.7. Docker Desktop with 11.63 GiB and 12 CPUs, engine 29.9.3, Compose v5.7.1, cgroup v2. Go 1.27.2 on the host |
| Method | Owner-attended verification after the unattended build. Seven read-only audit subagents, one per area. Every DEVIATION, MISSING, UNVERIFIED and MUST row was re-checked by hand against the code and the decision log. Then static test-integrity and hygiene checks, and a cold run on the reference machine |
| Scope rule | Nothing was fixed. Every problem found is recorded here as a candidate ticket |

## 1. Verdict: PASS

| Blocking criterion | Result |
|---|---|
| Any acceptance case fails (after one retry of the failed cases) | **No.** 38/38 PASS on the first attempt; no retry was needed (§8) |
| A secret is found in the working tree or in git history | **No.** Regex scan and gitleaks v8.30.1 over all 45 commits. The only literal credentials are the lab defaults that spec §17.2 mandates (§7) |
| A locked file changed | **No.** `git diff plan-v1 -- specs/system-spec.md specs/sprints-plan.md CLAUDE.md run-sprints.sh` and `git diff sprint-0-done HEAD -- acceptance/` are both empty (§3) |
| An undocumented deviation from a MUST / MUST NOT requirement | **No.** The spec has 13 MUST/MUST NOT statements, and all 13 are met (§5.1). The 4 undocumented deviations found are all against keyword-less statements or G3 logging (§5.3) |

None of the four blocking criteria holds, so the verdict is **PASS**. Every step of the cold run also passed:
- `make up`: 159 s;
- `make acceptance`: 38/38;
- `make reconcile`: PASS;
- `make down`: savepoint written;
- `make test` and `make lint`: green.

The most serious finding is F-01: the pipeline stops converging after about 5 hours at default load. It is not a deviation by the implementation. The BE runs exactly the `mem_limit = 2048m` that spec §11.2 fixes, so it is a limitation of the spec's own memory budget, and no blocking criterion covers it. It is the first ticket candidate.

## 2. Summary

- **Traceability:** every numbered section of the spec (§1 to §18 and Appendix A) was traced to code and tests. There is **no MISSING requirement**.
- **Deviations:** 21 are documented in the decision log, none of them against a MUST (§5.2). 4 are undocumented, none of them against a MUST (§5.3).
- **Acceptance suite:** 38 cases, with names identical to spec §16.4. None is skipped, ignored, commented out or changed since `sprint-0-done`. The cold run passed 38/38.
- **Unit tests** (static count): Go 120 top-level tests, Java 37, Rust 24, Web 7. `make test` in this run passed: Go 16 packages ok, Maven 37/37, Cargo 24/24, Vitest 7/7.
- **Cold run:** `make clean` → `build` → `up` → `acceptance` → `reconcile` took **21 min 0 s** in total. `make up` reached ready in **159 s**, against the 10-minute DoD target. `make reconcile` passed in 11 s (`assets=3018 users=303 groups=11 lake_assets=3018`).
- **Memory:** peak concurrent usage was **6.107 GiB (53.1%)** of the 11.5 GiB cap. No container was OOM-killed (§9).
- **Long-run finding (most important non-blocking item):** the stack left over from the sprint E gate degraded after about 5 hours at default load.
  - The Doris BE hit its own `mem_limit` (`MEM_LIMIT_EXCEEDED`).
  - The Flink job then entered a restart loop: 68 restores, and no completed checkpoint for the last 4 hours.
  - Nothing in the spec, the gates or the acceptance suite measures runs that long. See F-01 and §11.

## 3. Repository state and integrity (phase 1)

| Check | Result |
|---|---|
| `git status --porcelain` | Empty |
| Branch | `main` |
| Tags | All eight exist. They are lightweight tags. Recorded SHAs, re-checked before the push: |
| | `plan-v1` `ce8888f` |
| | `sprint-0-done` `3842888` |
| | `sprint-A-done` `5849955` |
| | `sprint-B-done` `4889604` |
| | `sprint-C-done` `625316c` |
| | `sprint-D-done` `56b13ff` |
| | `sprint-E-done` and `v0.1.0` `f14d7d7` |
| Remote | `origin` = `github.com/cristianrisueo/asset-discovery-lab` (public). `origin/main` = `0fec405`, an ancestor of `HEAD`, so the push is a fast-forward. Only `plan-v1` existed on the remote |
| `gh auth status` | Valid, with the `repo` and `workflow` scopes |
| Locked spec files, build runner and CLAUDE.md | `git diff plan-v1 -- specs/system-spec.md specs/sprints-plan.md CLAUDE.md run-sprints.sh` is empty, and `git log plan-v1..HEAD` on those paths lists no commit, so nothing was changed and later reverted either |
| `acceptance/` | `git diff sprint-0-done HEAD -- acceptance/` is empty. The only commits that touch it are `d8bd9e7` and `334c468`, both before the tag |
| Audit subagents | `git status` was still empty after all seven returned, which shows they were read-only |

## 4. Traceability table

Status values:
- **OK:** implemented as specified.
- **DEV-D:** a deviation documented in the decision log.
- **DEV-U:** an undocumented deviation.
- **MISSING:** not implemented. No row has this status.

Level values: **MUST**, **SHOULD**, **MAY**, or **stmt** (a requirement without a keyword).

Abbreviations:
- `M/` = `processor/src/main/java/io/github/cristianrisueo/adl/processor/`
- `compose` = `docker-compose.yml`
- Test ids A01–E06 are acceptance cases.

### §1 Purpose and scope

| Req | Level | Implementation | Tests | Status | Decision |
|---|---|---|---|---|---|
| §1.1 Purpose | stmt | Whole repository | A01–E06 | OK | — |
| §1.2 In scope: twelve components, contracts, environment, tests, CI, README | stmt | `datacore/`, `adapter/`, `processor/`, `infra/`, `explorer/`, `web/`, compose; `proto/`; `.github/workflows/`; `README.md` | A01–E06 | OK | — |
| §1.3 Out of scope: no ClickHouse, Kubernetes, entity deletion, metrics stack, auth or Arrow Stream Load | stmt | None of these are present: grep finds no ClickHouse, Kubernetes manifests or metrics stack. Doris sinks set `deletable` to false (`M/sink/DorisSinks.java`) | — | OK | — |
| §1.4 Everything MUST run on the reference machine | **MUST** | compose limits total 11,456 MiB | Cold run on the reference machine (§8) | OK | D-050 |
| §1.4 One command starts, one verifies; English; a §3 rule for every risky choice | stmt | `Makefile:60-62,105-106`; repository; decision log | — | OK | — |
| §1.5 Default load 10/s; Doris freshness ≤ 30 s p95 | stmt | `datacore/config.go` (`GENERATOR_RATE=10`) | B09: p95 10.37 s, max 10.41 s | OK | — |
| §1.5 Lake freshness ≤ 60 s p95 | stmt | Iceberg commit per checkpoint | C02 20.9 s, D03 21.7 s (single samples, not a p95) | OK | — |
| §1.5 Recovery with no lost and no duplicated data | stmt | §9.5, §10.6, §10.7 | E02, E03 | OK | — |
| §1.5 Cold start ≤ 10 min | stmt | `make up` | 159 s (§8) | OK | — |

### §2 Pinned versions

| Req | Level | Implementation | Tests | Status | Decision |
|---|---|---|---|---|---|
| §2.1 Exact pins in every build file and image tag; never `latest`, never a range | stmt | `go.mod`, `processor/pom.xml`, `explorer/Cargo.toml` (`=x.y.z`) and `Cargo.lock`, `web/package.json` (with `save-exact`) and `package-lock.json`, compose and every `FROM` line | Grep (§7) | OK | D-001, D-006, D-014, D-025, D-035, D-039, D-046 |
| §2.1 / §2.3 `gcr.io/distroless/static-debian12:nonroot` is a floating tag | stmt | `datacore/Dockerfile:15`, `adapter/Dockerfile:12` | — | DEV-D | D-014 (§2.3 names this exact tag) |
| §2.2 Java 17, Maven 3.9.x, Go 1.26.x, Rust ≥ 1.88 in `rust-toolchain.toml`, Node 24 | stmt | Temurin 17 (`processor/Dockerfile:5`); Maven 3.9.16; `go 1.26.11`; Rust 1.100.0; Node 24.22.3 | — | OK | D-003, D-025, D-039 |
| §2.3 Images | stmt | kafka 4.3.2, doris fe-4.1.5 and be-4.1.5, flink 2.1.3-java17, iceberg-rest-fixture 1.10.1, rustfs 1.0.6, dragonfly v2.1.0, mysql 8.4.13, aws-cli 2.42.4, alpine 3.24.4, bookworm-20261123-slim, nginx 1.30.8-alpine | — | OK | D-006, D-025, D-035, D-039 |
| §2.4 Flink job dependencies | stmt | `processor/pom.xml:22-33`: Flink 2.1.3 (`provided`), Kafka connector 5.0.0-2.1, Doris connector 26.3.0, Iceberg 1.12.0 (runtime and AWS bundle), Hadoop 3.4.3, protobuf-java 4.36.2 | — | OK | D-025, D-035 |
| §2.5 franz-go, grpc, protobuf, go-redis, sqlite, mysql, aws s3, buf | stmt | `go.mod:3-46`; `Makefile:9` | — | OK | D-001, D-014 |
| §2.5 kadm "v1.22.3" | stmt | `go.mod:12` pins v1.20.0 (separate module) | — | DEV-D | D-002, Q-002 |
| §2.6 `arrow-flight` 60.x with `flight-sql` enabled | **MUST** | `explorer/Cargo.toml:13` (`=60.2.0`, `features = ["flight-sql"]`) | D06 | OK | D-039 |
| §2.6 axum, tokio, tonic, serde, tracing | stmt | `explorer/Cargo.toml:12-25` | — | OK | D-039 |
| §2.6 `arrow` and `arrow-json` at the same version | stmt | Not dependencies; `arrow-array` and `arrow-schema` =60.2.0 instead | — | DEV-D | D-041 (`arrow-json`), D-039 (sub-crate pins) |
| §2.7 React 19, Vite, TypeScript 5, Vitest, ESLint, no UI library | stmt | `web/package.json`: 19.3.1, 8.3.8, 5.9.3, 5.1.2, 10.15.0 | — | OK | D-039 |
| §2.8 Fallback set | stmt | Not taken: Flink 2.1.3 everywhere | — | OK | — |

### §3 Decision rules

| Req | Level | Implementation | Tests | Status | Decision |
|---|---|---|---|---|---|
| §3.1 G1 Prefer §4, then §16, then the simplest option | stmt | Decision log applies it in D-007 to D-050 | — | OK | — |
| §3.1 G2 Never weaken, skip or delete an acceptance test | stmt | `acceptance/` unchanged since `sprint-0-done`; no skips (§6) | — | OK | D-011, D-012, Q-001 |
| §3.1 G3 Log every rule, fallback and pin | stmt | D-001 to D-050 | — | DEV-U | Two G1 choices are not logged: U-3 and U-4 (§5.3) |
| §3.1 G4 Ladders move on after three hypotheses | stmt | No ladder needed more than step 1: R4 (D-040) and R6 (D-037) | — | OK | — |
| §3.1 G5 The spec wins over the plan; conflicts recorded | stmt | D-032 | — | OK | — |
| R1 Fallback only on a version failure; never mix Flink minor versions | stmt ("never") | `processor/pom.xml:22-30`; `infra/flink/Dockerfile:2` | — | OK (not taken) | — |
| R2 Separate FE and BE, static IPs, election mode, no all-in-one image | stmt | compose:187-253; `infra/doris/*.Dockerfile:4` | Gate B | OK | D-026 |
| R2 `SHOW BACKENDS` MUST show the BE at 172.30.0.3 | **MUST** | compose:234 (`BE_ADDR`), compose:253 | `adlctl wait` table in this cold run: `172.30.0.3 alive=true` | OK | D-026 |
| R3 `vm.max_map_count`, `nofile`; no checks disabled | stmt | `infra/scripts/host-prep.sh:7-10`; compose:235-238 | — | OK | D-026 |
| R4 Flight SQL ladder; transport in `/readyz` | stmt | `explorer/src/flight.rs`: step 1 | D06 (`flight-sql`) | OK | D-040 |
| R4 Step 1 client is `FlightSqlServiceClient` | stmt | Generated `FlightServiceClient` sending `CommandStatementQuery` (`flight.rs:14,39`) | D06 | DEV-D | D-040 |
| R5 Stream Load in JSON lines | stmt | `M/sink/DorisSinks.java:30-35` | `DorisRowsTest` | OK | — |
| R6 Path style, cache TTL, REFRESH fallback, SeaweedFS fallback | stmt | `infra/doris/init-lake.sql:18-19` | C02, D03 | DEV-D | D-037 (`use_path_style`); D-041 (REFRESH fallback not needed) |
| R7 Hadoop client in the jar; `FlinkSink.forRowData` | stmt | `processor/pom.xml:112-121`; `M/ProcessorJob.java:149` | `ProcessorJobTest` | OK | D-036 |
| R8 Catalog volume at `/tmp` | stmt | compose:435-436 | C01–C05 | OK | D-035 |
| R9 Total limits MUST NOT exceed 11.5 GiB | **MUST** | compose limits sum to 11,456 MiB | Measured usage (§9) | OK | D-038, D-044, D-050 |
| R10 Generous start periods | stmt | compose health checks | — | OK | — |
| R11 Shaded jar; `META-INF/services` merged | stmt | `processor/pom.xml:194-241` | — | OK | D-028 |
| R12 `KafkaSource` and `KafkaSink` only | stmt | `M/ProcessorJob.java:80,97` | — | OK | — |
| R13 Async MV, or a view fallback | stmt | `infra/doris/init-core.sql:90-98` (the MV) | B07, D05 | OK (not taken) | — |
| R14 Fresh run id; never disable 2PC | stmt ("never") | `M/sink/DorisSinks.java:51`; `infra/flink/submit-job.sh:89-92` | E04 | OK | — |
| R15 Doris keywords | stmt | No backticks needed | B01, B06 | OK | D-027 |
| R16 Local buf plugins only if the remote ones fail | stmt | Remote plugins worked | — | OK (not taken) | D-004 |
| R17 Flink 2.x APIs and keys | stmt | No `SourceFunction`, `SinkFunction`, `Time` or `open(Configuration)`. All 23 §10.9 keys appear in the Flink 2.1 configuration reference | — | OK | — |
| R18 Jar upload size | stmt | compose:31 (512 MiB limit); the jar is 222 MiB | — | OK (not taken) | D-036 |

### §4 Architecture and flow

| Req | Level | Implementation | Tests | Status | Decision |
|---|---|---|---|---|---|
| §4.1 Diagram | stmt | `README.md:13-31` is identical to §4.1 | — | OK | — |
| §4.2 Components and ports | stmt | compose; §15.8 rows | D08 | OK | — |
| §4.3 The walkthrough MUST be followable end to end | **MUST** | `make move`, Flink UI, Doris, web; `README.md:143-224` | Sprint E build report, walkthrough steps 1–14 (2026-11-26 01:03 UTC); A02, A03, A05, B02, C02, D01, D03, D07 in this cold run | OK | D-049 |
| §4.4 datacore → triggers: outbox relay | stmt | `datacore/internal/outbox/relay.go:88-137` | A01, A03 | OK | — |
| §4.4 triggers → adapter: group `adl-adapter` | stmt | `adapter/main.go:81-87` | A01–A10, E02 | OK | D-022 |
| §4.4 adapter → datacore: `Get*` | stmt | `adapter/internal/fetcher/fetcher.go:71-85` | A02, A04, A09 | OK | — |
| §4.4 adapter ↔ Dragonfly: GET and SET | stmt | `adapter/internal/cache/cache.go:59-80`; `handler.go:70,90` | A05, E05 | OK | — |
| §4.4 adapter → entities.*: idempotent producer, publish before commit | stmt | `adapter/main.go:89-90`; `consumer.go:199-206` | A02, A04 | OK | — |
| §4.4 adapter → dlq.*: reason headers | stmt | `adapter/internal/publisher/publisher.go:133-154` | A06–A08, E01 | OK | — |
| §4.4 entities.* → processor: `KafkaSource` | stmt | `M/ProcessorJob.java:80-92` | B01, B02 | OK | — |
| §4.4 processor → dlq.*: side output to a `KafkaSink` | stmt | `M/ProcessorJob.java:97-103` | B05 | OK | — |
| §4.4 processor → Doris: 2PC JSON Stream Load | stmt | `M/sink/DorisSinks.java:38-62` | B01–B09, E03, E04 | OK | — |
| §4.4 processor → Iceberg: `FlinkSink` | stmt | `M/ProcessorJob.java:138-155` | C01–C05, E03 | OK | D-036 |
| §4.4 Iceberg → RustFS: S3FileIO | stmt | `M/lake/LakeCatalog.java:38-43`; compose:424-427 | C03 | OK | — |
| §4.4 Doris → Iceberg: REST catalog `lake` | stmt | `infra/doris/init-lake.sql:9-20` | C02, C05, D03 | OK | D-037 |
| §4.4 web → explorer: nginx proxies `/api` | stmt | `web/nginx.conf:43-53` | D07 | OK | D-042 |
| §4.4 explorer → Doris: Arrow Flight SQL | stmt | `explorer/src/flight.rs` | D06 | OK | D-040 |

### §5 Data model

| Req | Level | Implementation | Tests | Status | Decision |
|---|---|---|---|---|---|
| §5.1 Entity fields, patterns, kind/prefix, references | stmt | `proto/adl/v1/entities.proto`; `datacore/internal/model/model.go:44-165` | `model_test.go`; A02 | OK | — |
| §5.1 `os` and `site` come from the pools | stmt | The seed and generator use the pools; the admin upsert accepts any non-empty value (`model.go:125-130`) | `model_test.go` | DEV-D | D-024 |
| §5.2 Versions: 1, then +1, never decrease or skip; `changed_at` at commit | stmt | `datacore/internal/store/store.go:135-197` | `store_test.go`; E06 | OK | D-017 |
| §5.3 Deterministic seed 42; pools; 8/300/3000; LT-0042, U-0117, G-03 | stmt | `datacore/internal/store/seed.go`; `model/pools.go` | `seed_test.go` | OK | D-017 |
| §5.4 Reserved, not-found and phantom ids | stmt | `model.go:77-96`; `generator.go:80` | `model_test.go`; A08, D02 | OK | D-018 |
| §5.5 Time in UTC everywhere | stmt | Protobuf `Timestamp`; `init-core.sql:6`; `LakeTable` timestamptz; explorer RFC 3339 with `Z` | `convert::timestamps_render_as_rfc3339_millis_z`; B02 | OK | — |

### §6 Contracts

| Req | Level | Implementation | Tests | Status | Decision |
|---|---|---|---|---|---|
| §6.1 One contract; generated code committed; buf STANDARD and FILE | stmt | `proto/adl/v1/`; `gen/go/adl/v1/`; `processor/src/generated/java/`; `buf.yaml` | CI contracts job | OK | D-004, D-013 |
| §6.2 `entities.proto` | stmt | **Byte-identical** to the spec block (diff) | A02 | OK | — |
| §6.3 `trigger.proto` | stmt | Byte-identical | A03 (exactly 4 fields) | OK | — |
| §6.4 `datacore.proto`; Get* implemented | stmt | Byte-identical; `datacore/internal/grpcapi/grpcapi.go:97-121` | `grpcapi_test.go` | OK | — |
| §6.5 `admin.proto`; all 12 RPCs implemented | stmt | Byte-identical; `grpcapi.go:152-295` | `grpcapi_test.go` | OK | D-021 |
| §6.6 `buf.yaml`, `buf.gen.yaml`, output paths, `build-helper` | stmt | `buf.yaml`; `buf.gen.yaml:3-10`; `processor/pom.xml:163-182` | CI regenerates and diffs | OK | D-004 |
| §6.6 `protobuf-java` MUST match the Java plugin | **MUST** | `pom.xml:26` 4.36.2 ↔ `buf.gen.yaml:9` v36.2; the generated header says 4.36.2 | — | OK | D-004 |
| §6.7 Validation rules: adapter (triggers, entities), processor (entities), datacore (admin input) | stmt | `adapter/internal/contract/contract.go:59-150`; `M/decode/EntityDecoder.java:67-134`; `datacore/internal/model/model.go:108-165` | `contract_test.go`, `EntityDeserializerTest`, `model_test.go`; A07, B05 | OK | D-019 |
| §6.8 Keys and the `trigger.version` header | stmt | `datacore/internal/store/store.go:205`; `adapter/internal/publisher/publisher.go:121-130` | `publisher_test.go`; A02, A03 | OK | — |

### §7 datacore

| Req | Level | Implementation | Tests | Status | Decision |
|---|---|---|---|---|---|
| §7.1 Responsibility | stmt | `datacore/main.go:69-186` | Gate A | OK | — |
| §7.2 modernc SQLite, WAL, NORMAL, named volume; current, previous and outbox | stmt | `store.go:19-88` | `store_test.go`; E06 | OK | D-014, D-017 |
| §7.3 One transaction per change | stmt | `store.go:135-197` | `store_test.go:83-154` | OK | D-017 |
| §7.4 Seed when the database is empty, batched | stmt | `seed.go:79-119` | `seed_test.go` | OK | D-017 |
| §7.4 The seed MUST complete before `/readyz` | **MUST** | `main.go:97-106,136-147`: `seeded` is set only after `Seed` returns | `config_test.go:72-94`; A01 | OK | — |
| §7.5 Outbox relay: order, idempotent, acks=all, delete after acks, chaos only adds | stmt | `datacore/internal/outbox/relay.go:65-137`; `main.go:85-90` | `relay_test.go` | OK | — |
| §7.6 Generator: rate, mix, seeded entities only, configuration in memory | stmt | `datacore/internal/generator/generator.go` | `generator_test.go` | OK | D-020 |
| §7.7 DataCoreService: INVALID_ARGUMENT, NOT_FOUND, `get_calls`, chaos | stmt | `grpcapi.go:76-92` | `grpcapi_test.go`; A08–A10 | OK | — |
| §7.8 AdminService: validation, upserts, List*, Set*, Burst, stats | stmt | `grpcapi.go:125-295`; `model.go:187-206` | `grpcapi_test.go`, `model_test.go` | OK | D-018, D-021, D-024 |
| §7.9 Chaos knobs and the `make chaos-on` column | stmt | `datacore/internal/chaos/chaos.go`; `labkit/datacore.go:19-28` | `chaos_test.go`; E01 | OK | — |
| §7.10 Configuration defaults | stmt | `datacore/config.go:34-68` | `config_test.go` | OK | — |
| §7.11 Health checks, `datacore healthcheck`, JSON logs, SIGTERM order | stmt | `datacore/health.go`; `main.go:40-60,168-199` | `config_test.go`; D08, E06 | OK | — |
| §7.11 gRPC reflection | SHOULD | `main.go:28,117` | — | OK | — |

### §8 Kafka

| Req | Level | Implementation | Tests | Status | Decision |
|---|---|---|---|---|---|
| §8.1 KRaft single node, RF 1, no auto-create, listeners, fixed CLUSTER_ID, heap 512m | stmt | compose:35-75 | Gate A | OK | D-015 |
| §8.2 Six topics: partitions, cleanup, retention, compaction settings | stmt | `infra/scripts/create-topics.sh:9-31` | A01–A10, B05 | OK | — |
| §8.3 DLQ format and headers; reasons per service | stmt | `adapter/internal/publisher/publisher.go:139-166`; `M/sink/DlqSerializer.java:34-42` | `publisher_test.go`, `DlqSerializerTest`; A06, B05 | OK | — |
| §8.4 `kafka-init` MUST be idempotent | **MUST** | `create-topics.sh:18` (`--if-not-exists`) | Re-run by E04's `make up` in this cold run | OK | D-015 |
| §8.5 Consumer groups and offset handling | stmt | `adapter/main.go:83-87`; `M/ProcessorJob.java:85-87` | E02, E03 | OK | — |

### §9 adapter

| Req | Level | Implementation | Tests | Status | Decision |
|---|---|---|---|---|---|
| §9.1 Responsibility | stmt | `adapter/main.go:74-202`; `handler.go:55-103` | Gate A, E02 | OK | — |
| §9.2 Manual commits, sequential per partition, never commit an unhandled record | stmt | `consumer.go:71-207` | `consumer_test.go` | OK | D-022, D-047 |
| §9.3 Cache key, TTL, lookup and store versions, 100 ms timeout, errors count as a miss | stmt | `cache.go:17-80`; `handler.go:69-93` | `cache_test.go`, `handler_test.go`; A05, E05 | OK | — |
| §9.3 The SET MUST finish before the next record of the partition | **MUST** | `handler.go:90`: a synchronous `Cache.Set` before the publish; `consumer.go:192-197` is sequential | `handler_test.go:108` | OK | — |
| §9.4 Outcome classes, backoff, transient never dead-lettered, readiness after 30 s | stmt | `fetcher/classify.go:37-63`; `backoff.go:23-53`; `fetcher.go:90-193` | `TestClassifyEveryOutcome`, `TestBackoffTimings`; A08–A10 | OK | D-022 |
| §9.5 Idempotent, acks=all, 5 ms linger | stmt | `adapter/main.go:89-90` | Gate A | OK | D-022 |
| §9.5 Offsets committed "only then" | stmt | On failure paths each partition's acknowledged prefix is committed, then the process exits (`consumer.go:135-148`) | `consumer_test.go` | DEV-D | D-022, D-047 |
| §9.6 Dead letters; acknowledged before the commit | stmt | `handler.go:59-110`; `publisher.go:133-182` | `handler_test.go`; A06–A08 | OK | — |
| §9.7 Configuration defaults | stmt | `adapter/config.go:13-57` | `config_test.go` | OK | D-049 |
| §9.8 Health checks, `adapter healthcheck`, log fields | stmt | `adapter/health.go`; `main.go:45-60,150-157` | D08 | OK | D-022 |
| §9.8 SIGTERM: finish the current batch | stmt | A 7 s grace bound (`main.go:41`) | `consumer_test.go` | DEV-D | D-022 |

### §10 processor

| Req | Level | Implementation | Tests | Status | Decision |
|---|---|---|---|---|---|
| §10.1 Job `adl-processor`, Java 17, DataStream topology | stmt | `M/ProcessorJob.java:48-155` | `ProcessorJobTest` | OK | D-028 |
| §10.2 KafkaSource, offsets, a deserializer that never throws, watermarks after decoding | stmt | `M/ProcessorJob.java:80-113`; `M/decode/EntityDeserializer.java:18-27` | `EntityDeserializerTest`; B09 | OK | — |
| §10.3 Decode and validate into envelopes | stmt | `M/decode/EntityDecoder.java:42-174` | `EntityDeserializerTest` | OK | D-028 |
| §10.4 DLQ side output, AT_LEAST_ONCE, never fails the job | stmt | `M/ProcessorJob.java:97-103`; `M/decode/DecodeValidate.java` | `DecodeValidateTest`; B05 | OK | — |
| §10.5 VersionGate state, counters and name | stmt | `M/gate/VersionGate.java` | `VersionGateTest`; B03, B04 | OK | — |
| §10.6 Four Doris sinks, 2PC, JSON, label prefix, row mapping, run id | stmt | `M/sink/DorisSinks.java`; `M/sink/DorisRows.java`; `submit-job.sh:79-93` | `DorisRowsTest`; B01–B09, E04 | OK | — |
| §10.6 Datetimes `yyyy-MM-dd HH:mm:ss.SSS` | stmt | The `change_rate_1m` bounds use `HH:mm:ss` (`DorisRows.java:23-24`); Appendix A item 7 and the `DATETIME` column agree with the code | `DorisRowsTest` | DEV-D | D-028 |
| §10.7 Iceberg catalog, table creation, `FlinkSink`, row mapping, assets only, core-only switch | stmt | `M/lake/LakeCatalog.java`; `LakeRows.java`; `M/ProcessorJob.java:132-155` | `LakeCatalogTest`, `LakeRowsTest`, `ProcessorJobTest`; C01–C05 | OK | D-036 |
| §10.8 One-minute tumbling event-time window, aggregate plus window function, lateness 0 | stmt | `M/window/ChangeRateWindow.java`; `ChangeRateCount.java` | `ChangeRateWindowTest`; B08, E03 | OK | — |
| §10.9 Cluster configuration: 23 keys through `FLINK_PROPERTIES` | stmt | compose:8-31 | Keys checked against the Flink 2.1 reference | OK | D-034 |
| §10.9 `/flink-data` MUST be writable by uid 9999 | **MUST** | `infra/flink/Dockerfile:7-8` (`chown 9999`); `processor/Dockerfile:15-18` | E04 | OK | D-030 |
| §10.10 Operator names and uids | stmt | `M/ProcessorJob.java:91-173` | `ProcessorJobTest` | OK | D-028 |
| §10.10 Iceberg sink named `IcebergAssetChanges` | stmt | The name is on the terminal sink; the writer and committer are `iceberg-sink-*` | `ProcessorJobTest` | DEV-D | D-036 |
| §10.11 Submitter: restore point, run id, upload and run, wait, prune; graceful stop | stmt | `infra/flink/submit-job.sh`; `adlctl/flink.go`; `labkit/flink.go` | E03, E04 | OK | D-030, D-048 |
| §10.11 Step order (slots first) and active states (CREATED, RUNNING, RESTARTING) | stmt | Active check first, then wait for slots ≥ parallelism; INITIALIZING also counts as active (`submit-job.sh:46-71`) | E04 | DEV-D | D-034 |
| §10.11 Missing run-id file on restore | stmt | Falls back to a new run id with a WARN (`submit-job.sh:84-88`) | — | DEV-U | U-3 |
| §10.12 Program arguments | stmt | `M/JobOptions.java:37-57`; `submit-job.sh:99-114` | `JobOptionsTest` | OK | D-030 |

### §11 Doris

| Req | Level | Implementation | Tests | Status | Decision |
|---|---|---|---|---|---|
| §11.1 Topology, election mode, derived images | stmt | compose:187-253; `infra/doris/*.Dockerfile` | Gate B | OK | D-026 |
| §11.2 Flight SQL ports, BE `mem_limit` 2048m, BE heap 512m | stmt | `fe.Dockerfile:14-20`; `be.Dockerfile:9-22` | D06 | OK | D-026 |
| §11.2 / §15.4 FE heap 1280m | stmt | 1024m (`fe.Dockerfile:10`) | — | DEV-D | D-044 (R9 step 1) |
| §11.3 Database and tables | stmt | `init-core.sql:8-71`: verbatim | B01–B09 | OK | — |
| §11.4 View | stmt | `init-core.sql:73-88`: verbatim | B06, D01 | OK | — |
| §11.5 Materialized view | stmt | `init-core.sql:90-98`: verbatim | B07, D05 | OK | — |
| §11.6 Lake catalog | stmt | `init-lake.sql:9-20` | C02, D03 | DEV-D | D-037 (`use_path_style`) |
| §11.7 UTC, `explorer` user, grants | stmt | `init-core.sql:6,100-101`; `init-lake.sql:22` | Gate D | OK | D-027 |
| §11.8 Init procedure, idempotent | stmt | `infra/scripts/doris-init.sh`; compose:255-273,449-471 | Gates B and C; E04 re-runs it | OK | D-027 |

### §12 Lake

| Req | Level | Implementation | Tests | Status | Decision |
|---|---|---|---|---|---|
| §12.1 RustFS image, volume, ports, environment, health, bucket | stmt | compose:362-407; `infra/scripts/create-bucket.sh` | C03 | OK | D-035 |
| §12.1 / §15.7 `/data` MUST be writable by uid 10001 | **MUST** | The image owns `/data` (D-035) | C03 | OK | D-035 |
| §12.2 REST catalog image, port, `/tmp` volume, environment, health | stmt | compose:414-446 | C01 | OK | D-035 |
| §12.2 `AWS_REGION: us-east-1` | stmt | `${S3_REGION:-us-east-1}` (compose:430). Same value with the §17.2 `.env.example` | — | DEV-U | U-1 |
| §12.3 Table schema (9 fields), `day(changed_at)`, properties, location, append-only | stmt | `M/lake/LakeTable.java:13-38` | `LakeTableTest`; C01–C05 | OK | D-036 |
| §12.4 Small files and snapshots accepted | stmt | No compaction or expiry configured | — | OK | — |

### §13 explorer

| Req | Level | Implementation | Tests | Status | Decision |
|---|---|---|---|---|---|
| §13.1 Only way into the data; Doris never exposed | stmt | `explorer/src/api.rs`; FE publishes only 8030 and 9030 | D07 | OK | — |
| §13.2 Endpoints and status codes | stmt | `api.rs:25-93`; `service.rs` | `api::*`; D01–D08 | OK | D-041 |
| §13.2 `offset` ≥ 0 | stmt | Also capped at 10⁹ (`validate.rs:68-72`) | `validate::list_params` | DEV-D | D-045 |
| §13.3 JSON shapes and key order | stmt | `service.rs:129-162`; `serde_json` with `preserve_order` | `api::asset_matches_the_spec_shape`; D01, D03 | OK | D-041 |
| §13.4 SQL | stmt | `explorer/src/sql.rs`: exact text | `sql::statements_match_the_spec` | OK | — |
| §13.5 Validation, injection-safe literals | stmt | `explorer/src/validate.rs` | `validate::*`; D02 | OK | D-041 |
| §13.5 "Anything else returns 400" | stmt | Unknown parameter names are ignored | — | DEV-D | D-045 |
| §13.6 Doris access: ladder, scheme translation, client reuse, re-handshake, 503, timeout, 64 MiB, DATETIME rendering, `LAKE_ENABLED` | stmt | `explorer/src/flight.rs`; `convert.rs`; `service.rs:72-76` | `flight::*`, `convert::*`, `error::*`; D06 | OK | D-040, D-045 |
| §13.7 Configuration defaults | stmt | `explorer/src/config.rs:63-84` | `config::defaults_follow_the_spec` | OK | D-041 |
| §13.8 `explorer healthcheck`; JSON logs | stmt | `health.rs`; `logging.rs` | `health::*`; D08 | OK | D-041 |

### §14 web

| Req | Level | Implementation | Tests | Status | Decision |
|---|---|---|---|---|---|
| §14.1 Asset page (LT-0042, card, History, 5 s refresh) and Stats page | stmt | `web/src/AssetPage.tsx`, `AssetCard.tsx`, `HistoryTable.tsx`, `StatsPage.tsx` | `AssetCard.test.tsx`, `AssetPage.test.tsx` | OK | D-042 |
| §14.2 Loading, empty, error and 404 states; `/api/*` only | stmt | `web/src/api.ts`; `Status.tsx` | `AssetPage.test.tsx` | OK | D-042 |
| §14.3 Vite, React, TypeScript; nginx proxy and fallback; dev proxy | stmt | `web/nginx.conf`; `vite.config.ts` | D07 | OK | D-042, D-045 |
| §14.4 Type-check, lint, Vitest card fixture and error state | stmt | `web/package.json` scripts | 7 Vitest tests | OK | D-043 |

### §15 Local environment

| Req | Level | Implementation | Tests | Status | Decision |
|---|---|---|---|---|---|
| §15.1 Host prerequisites; only Go, make and git on the host | stmt | `README.md:66-72`; `Makefile:9-30` (`go run`, builder containers) | — | OK | D-005, D-033, D-043 |
| §15.2 17 services: profiles, builds, dependencies (incl. `required: false`), restart policies, `name: adl` | stmt | compose | `adlctl wait` table | OK | D-041 |
| §15.3 Network, subnet, static FE and BE addresses | stmt | compose:544-551 | — | OK | — |
| §15.4 Memory budget: per-service limits and settings, total 11,456 MiB | stmt | compose `deploy.resources.limits` | §9 | OK (FE heap: see §11.2) | D-016, D-044, D-050 |
| §15.4 Usage SHOULD stay well below the limits | SHOULD | — | §9 | OK | D-050 |
| §15.5 Host preparation | stmt | `infra/scripts/host-prep.sh` | Ran in `make up` | OK | D-023 |
| §15.6 Health checks: command, start period, interval, retries | stmt | compose (all eleven checks match) | `adlctl wait` | OK | D-016, D-026 |
| §15.7 Volumes; MUST be writable by the container user | **MUST** | compose volumes; ownership set in the images (D-015, D-030, D-035) | Cold run, E04, E06 | OK | D-015, D-030, D-035 |
| §15.8 Host ports | stmt | compose: all 13 match; 8070, 8050 and 8040 stay internal | Acceptance endpoints | OK | — |
| §15.9 Makefile targets | stmt | `Makefile`: every target, with the recipes of the table | Cold run | OK | D-005, D-009, D-013, D-033, D-043 |
| §15.10 adlctl subcommands | stmt | `adlctl/*.go`; `labkit/` | `adlctl/main_test.go`, `labkit/*_test.go` | OK | D-009, D-048 |
| §15.11 Multi-stage builds with cache mounts; CGO off; distroless | stmt | `*/Dockerfile` | CI images job | OK | D-014 |
| §15.11 Heavy builds SHOULD NOT run with the full stack up | SHOULD | Gate `make test lint` ran with the stack up, memory-capped | — | DEV-D | D-033, D-043 |

### §16 Tests

| Req | Level | Implementation | Tests | Status | Decision |
|---|---|---|---|---|---|
| §16.1 Unit, acceptance, reconciliation and CI levels | stmt | `Makefile`; `acceptance/`; `labkit/reconcile.go`; `.github/workflows/` | — | OK | — |
| §16.1 Acceptance through public interfaces only | stmt | E04 lists `/flink-data/savepoints` through `docker compose exec` | E04 | DEV-D | D-012, Q-001 |
| §16.2 datacore unit tests (4 required) | stmt | `store_test.go`, `seed_test.go`, `chaos_test.go`, `model_test.go`, `grpcapi_test.go` | — | OK | — |
| §16.2 adapter unit tests (7 required) | stmt | `contract_test.go`, `fetcher_test.go` (classify, backoff), `cache_test.go`, `consumer_test.go`, `publisher_test.go` | — | OK | — |
| §16.2 processor unit tests (5 required) | stmt | `VersionGateTest`, `EntityDeserializerTest`, `DorisRowsTest`, `LakeRowsTest`, `ChangeRateWindowTest` (MiniCluster) | — | OK | D-028 |
| §16.2 explorer unit tests (3 required) | stmt | `validate::*` (injection strings), `convert::*` and `api::*` (batches built in the test), `error::*` | — | OK | — |
| §16.2 web = §14.4 | stmt | 7 Vitest tests | — | OK | — |
| §16.3 Package, tag, names, gate order, endpoint defaults, test data, isolation, timeouts, polling, no-sleep rule, failure dump | stmt | `acceptance/harness_test.go`; `labkit/config.go:19-29` | §6 | OK | D-008, D-011 |
| §16.3 Topic scans MUST count only §6.7-valid records | **MUST** | `harness_test.go:225-235`; `gate_a_test.go:40-43,242` | `labkit/validate_test.go` | OK | — |
| §16.4 The 38 cases | stmt | `acceptance/gate_{a..e}_test.go` | §6 | OK, with documented readings (A05, D04, E02/E03, E04) and one undocumented one (C02) | D-012, Q-001; U-2 |
| §16.5 Reconciliation steps 1–5 | stmt | `labkit/reconcile.go`; `adlctl/reconcile.go` | `reconcile_test.go`; B01, C05, E01–E06; `make reconcile` (§8) | OK | D-008 |
| §16.5 Lake compared "unless `--no-lake`" | stmt | Also skipped when no `iceberg-rest` container exists | `TestLakeCreated` | DEV-D | D-032, D-034 |
| §16.6 `ci.yml` jobs and steps | stmt | `.github/workflows/ci.yml` | Not run on GitHub when this report was written (Q-003). The workflows first run on the push that publishes this report | OK | D-006, D-010, D-046 |
| §16.6 `e2e.yml` steps | stmt | `.github/workflows/e2e.yml` | Not run on GitHub when this report was written (Q-003). The workflows first run on the push that publishes this report | OK | D-013 |

### §17 Repository and conventions

| Req | Level | Implementation | Tests | Status | Decision |
|---|---|---|---|---|---|
| §17.1 Layout; layered Go `internal/` packages; no single-file directory | stmt | Repository tree | — | OK | D-007, D-019, D-023 |
| §17.2 English; Conventional Commits | stmt | 45/45 subjects follow Conventional Commits | — | OK | — |
| §17.2 Formatters and linters enforced in CI | stmt | `.golangci.yml`; Spotless; rustfmt and clippy; ESLint and Prettier; `ci.yml` | `make lint` green in this run | OK | D-025 |
| §17.2 Configuration through env vars; `.env.example` matches the spec | stmt | `.env.example` identical to §17.2 | — | OK | D-008 |
| §17.2 JSON logs with `service` | stmt | datacore, adapter, explorer, Flink (`log4j-console.properties`), submitter, web access log | — | OK | D-031, D-041, D-042 |
| §17.2 Generated code committed, `linguist-generated`, excluded from linters | stmt | `.gitattributes:6-7`; `.golangci.yml`; `pom.xml:249-252` | — | OK | — |
| §17.2 `.gitignore` | stmt | `.gitignore` covers `.env`, `bin/`, `dist/`, `target/` and `node_modules/` | — | OK | — |
| §17.2 README: the ten sections | stmt | `README.md`: overview, architecture, quick start, Make targets, services and ports, walkthrough, testing, troubleshooting, "How this was built", licence | — | OK | — |

### §18 Definition of Done

| Item | Status | Evidence |
|---|---|---|
| 1. `make build && make up` reaches ready; `make up` ≤ 10 min | Met | 159 s from `make clean`. `adlctl wait` was green for every row (§8). The build was a cache hit (see the caveat in §8) |
| 2. `make acceptance` passes A–E; D06 without the MySQL fallback | Met | 38/38 PASS; D06 reports `flight-sql` |
| 3. `make test` and `make lint` pass | Met | §6.3 and §8 |
| 4. Chaos for three minutes, then `make reconcile` is green | Met (equivalent) | E01 (chaos on, 180 s at 20/s, chaos off, Doris and lake reconcile) passed in 188.8 s. Then `make reconcile`: `make reconcile` passed in 11 s (`assets=3018 users=303 groups=11 lake_assets=3018`). |
| 5. `make down && make up` resumes from a savepoint; `make clean && make up` starts from scratch; a green reconcile after each | Met | E04 PASS (new savepoint restored, Doris reconcile). This run started from `make clean` |
| 6. Every §4.4 row exists and works; the walkthrough can be followed | Met | §4.4 rows above, each with a passing case. Walkthrough: sprint E build report, plus this run's cases |
| 7. A `docker stats` snapshot at default load is in the decision log; no container over its limit; total ≤ 11.5 GiB | Met | D-050, plus this run (§9) |
| 8. Both CI workflows exist and pass on GitHub | Not verifiable in this report | Q-003 |
| 9. README complete | Met | Ten §17.2 sections. One claim is inaccurate (F-03) |
| 10. Every rule and pin logged; no TODO or FIXME without a reference | Met, with two G3 gaps | `git grep -nE 'TODO\|FIXME'` outside `specs/` finds nothing. U-3 and U-4 are unlogged G1 choices |

### Appendix A

| Req | Level | Implementation | Tests | Status |
|---|---|---|---|---|
| Items 1–9: one change in every representation | stmt | `M/sink/DorisRows.java`, `M/lake/LakeRows.java`, `adapter/internal/cache`, `explorer/src/service.rs`, `publisher.go` | `DorisRowsTest`, `LakeRowsTest.assetRowMatchesAppendixA`; A02, A05, A06, D01, D03 | OK |

## 5. Deviations

### 5.1 MUST and MUST NOT register

The spec has 13 hard requirements (found with `grep MUST`). All 13 are met.

| Line | Requirement | Status |
|---|---|---|
| §1.4 | Everything runs on the reference machine | OK (cold run) |
| §2.6 | `arrow-flight` with `flight-sql` | OK |
| R2 | `SHOW BACKENDS` shows the BE at 172.30.0.3 | OK |
| R9 | Total limits ≤ 11.5 GiB | OK |
| §4.3 | The walkthrough can be followed end to end | OK |
| §6.6 | `protobuf-java` matches the Java plugin | OK |
| §7.4 | The seed completes before `/readyz` | OK |
| §8.4 | `kafka-init` is idempotent | OK |
| §9.3 | The cache SET finishes before the next record | OK |
| §10.9 | `/flink-data` is writable by uid 9999 | OK |
| §12.1 | RustFS `/data` is writable by uid 10001 | OK |
| §15.7 | Every named volume is writable by its user | OK |
| §16.3 | Topic scans count only §6.7-valid records | OK |

### 5.2 Documented deviations (21, none against a MUST)

| # | Spec | What the build does | Decision | Justified? |
|---|---|---|---|---|
| D1 | §11.2 / §15.4 FE heap 1280m | 1024m, inside the same 1792 MiB limit | D-044 | Yes. R9 step 1, taken before an OOM kill, with `docker stats` before and after |
| D2 | §11.6 `s3.use_path_style` | `use_path_style` | D-037 | Yes. The R6 fallback, with bytecode evidence that 4.1.5 ignores the spec key |
| D3 | §5.1 `os` and `site` from the pools | The admin upsert accepts any non-empty value | D-024 | Yes. §7.8 lists the checks to enforce, and the pools are not among them |
| D4 | §9.5 commit "only then" | On failure, each partition's acknowledged prefix is committed, then the process exits | D-022, D-047 | Yes. It never commits an unhandled record (§9.2) |
| D5 | §9.8 SIGTERM "finish the current batch" | A 7 s grace bound | D-022 | Yes. It stays below compose's 10 s stop timeout; a stalled batch could never finish |
| D6 | §10.6 datetimes with `.SSS` | `change_rate_1m` bounds without milliseconds | D-028 | Yes. Appendix A item 7 and the `DATETIME` column agree; the spec contradicts itself |
| D7 | §10.10 Iceberg sink name | The name is on the terminal sink; the writer and committer are `iceberg-sink-*` | D-036 | Yes. The builder cannot rename them, and the uids are stable |
| D8 | §10.11 step 1 order | Active-job check first, then a wait for slots ≥ parallelism | D-034 | Yes. Fixes sprint B audit F-01/F-02 |
| D9 | §10.11 step 2 states | INITIALIZING also counts as present | D-034 | Yes. Additive |
| D10 | §13.2 `offset` ≥ 0 | Capped at 10⁹ | D-045 | Yes. A Doris BIGINT-overflow hang |
| D11 | §13.5 "anything else returns 400" | Unknown parameter names are ignored | D-045 | Yes, as a recorded reading of an ambiguous sentence |
| D12 | R4 step 1 `FlightSqlServiceClient` | The generated `FlightServiceClient` with Flight SQL commands | D-040 | Yes. Same wire protocol; keeps the gRPC codes §13.6 needs |
| D13 | §2.6 `arrow`, `arrow-json` | `arrow-array` and `arrow-schema` instead; no `arrow-json` | D-041, D-039 | Partly. `arrow-json` is explained; the umbrella-crate swap appears only as pins |
| D14 | §2.1 exact tags | `distroless/static-debian12:nonroot` floats | D-014 | Yes. §2.3 names this exact tag (a tension inside the spec) |
| D15 | §2.5 kadm v1.22.3 | v1.20.0 | D-002, Q-002 | Yes. A separate module; that version does not exist |
| D16 | §15.11 SHOULD NOT build with the stack up | Gate `make test lint` ran with the stack up | D-033, D-043 | Yes. Memory-capped builder containers with warm caches |
| D17 | §16.1 public interfaces only | E04 uses `docker compose exec … ls` | D-012, Q-001 | Yes. Shows that a new savepoint was written |
| D18 | §16.5 lake "unless `--no-lake`" | Also skipped when no `iceberg-rest` container exists | D-032, D-034 | Yes. A core-only stack has no lake (§15.2, G5) |
| D19 | §16.4 A05 "two more records" | At least two | D-012, Q-001 | Yes. An at-least-once adapter may publish a third copy |
| D20 | §16.4 E02/E03 `TRecover` window | Starts after the restart and the burst return | D-012 | Yes. One documented deadline |
| D21 | §16.4 D04 Given "—" | Pauses the generator and reconciles before paging | D-012, Q-001 | Yes. Prevents a false overlap |

Documented interpretations that are additive, so they are not counted as deviations:
- D-017: the seed is gated by a `seed_complete` flag rather than an empty database, so an interrupted seed resumes.
- D-018: the admin API rejects LT-9999 and LT-8000 to LT-8999.

### 5.3 Undocumented deviations (4, none against a MUST)

| # | Spec | What the build does | Level | Impact |
|---|---|---|---|---|
| U-1 | §12.2 `AWS_REGION: us-east-1` | `${S3_REGION:-us-east-1}` (compose:430) | stmt | None with the shipped `.env.example`. Doris (`init-lake.sql:15`) stays hard-coded to us-east-1, so changing `S3_REGION` would make the components disagree |
| U-2 | §16.4 C02 "returns v1 and v2 with their sites" | The test checks that both rows are present, not that the result is exactly those two rows (`gate_c_test.go:97-105`). Not listed in D-012 | stmt | Low. C04 (no duplicates) and C05 (lake max version equals datacore's) cover extra rows, so the suite as a whole is not weaker |
| U-3 | G3 (log every rule applied) | `submit-job.sh:84-88`: a restore point without `/flink-data/label-run-id` gets a new run id, with a WARN. This G1 choice is not logged | stmt (G3) | Low. A new prefix avoids label collisions |
| U-4 | G3 | `adapter/internal/fetcher/classify.go:55-58` checks validation before staleness, so an entity that is both invalid and older than the trigger is dead-lettered as VALIDATION_ERROR and not retried as stale. Not logged | stmt (G3) | Low. §9.4 does not order the rows |

## 6. Test integrity (phase 3)

### 6.1 The 38 acceptance cases

- **Names:** the names extracted from `acceptance/*.go` are identical to the 38 rows of spec §16.4 (`diff` of the sorted lists).
- **Build tag:** every test file carries `//go:build acceptance`. `doc.go` is untagged on purpose, so the package always builds.
- **Integrity checks:**
  - no `TestMain`;
  - no skip, ignore or focus pattern in `acceptance/`: no `t.Skip`, `SkipNow`, `testing.Short`, `@Disabled`, `@Ignore`, `#[ignore]`, `.skip(`, `xit(`, `.only(` or `it.todo`;
  - no commented-out test anywhere;
  - `acceptance/` is unchanged since `sprint-0-done`.
- **Runtime skips:** none. The cold run printed 38 `--- PASS` lines and no `--- SKIP` line.
- **Harness:** the timeouts are exactly §16.3: TShort 30 s, TPipe 90 s, TLake 120 s, TReconcile 180 s, TRecover 300 s (`harness_test.go:21-27`).
- **Weakening:** every case's assertions match its row or are stronger. The exceptions are the documented readings (D19–D21, D17) and U-2.

| Case | Test function | Cold run | Time |
|---|---|---|---|
| A01 | TestGateA_01_SeedPublished | PASS | 0.20 s |
| A02 | TestGateA_02_AssetChangePublished | PASS | 1.07 s |
| A03 | TestGateA_03_TriggerContract | PASS | 1.05 s |
| A04 | TestGateA_04_UsersAndGroups | PASS | 1.06 s |
| A05 | TestGateA_05_CacheHit | PASS | 2.25 s |
| A06 | TestGateA_06_DecodeErrorToDlq | PASS | 1.13 s |
| A07 | TestGateA_07_ValidationErrorToDlq | PASS | 0.09 s |
| A08 | TestGateA_08_NotFoundToDlq | PASS | 1.04 s |
| A09 | TestGateA_09_StaleReadRecovered | PASS | 2.10 s |
| A10 | TestGateA_10_TransientErrorsRetried | PASS | 24.55 s |
| B01 | TestGateB_01_SeedInDoris | PASS | 6.69 s |
| B02 | TestGateB_02_ChangeVisible | PASS | 5.40 s |
| B03 | TestGateB_03_OlderVersionIgnored | PASS | 40.10 s |
| B04 | TestGateB_04_DuplicatesDropped | PASS | 21.33 s |
| B05 | TestGateB_05_ProcessorDlq | PASS | 7.64 s |
| B06 | TestGateB_06_EnrichedView | PASS | 10.57 s |
| B07 | TestGateB_07_MaterializedViewConsistent | PASS | 12.12 s |
| B08 | TestGateB_08_ChangeRateRows | PASS | 0.07 s |
| B09 | TestGateB_09_Freshness | PASS (p95 10.37 s, max 10.41 s) | 198.01 s |
| C01 | TestGateC_01_TableExists | PASS | 0.02 s |
| C02 | TestGateC_02_ChangesLandInLake | PASS | 20.90 s |
| C03 | TestGateC_03_ParquetInRustFS | PASS | 0.15 s |
| C04 | TestGateC_04_NoDuplicatesInLake | PASS | 0.49 s |
| C05 | TestGateC_05_LakeReconciled | PASS | 10.30 s |
| D01 | TestGateD_01_AssetEndpoint | PASS | 7.50 s |
| D02 | TestGateD_02_ErrorsAndValidation | PASS | 0.05 s |
| D03 | TestGateD_03_HistoryEndpoint | PASS | 21.66 s |
| D04 | TestGateD_04_ListEndpoint | PASS | 9.07 s |
| D05 | TestGateD_05_StatsEndpoints | PASS | 1.39 s |
| D06 | TestGateD_06_FlightSqlTransport | PASS (`flight-sql`) | 0.02 s |
| D07 | TestGateD_07_WebServes | PASS | 0.08 s |
| D08 | TestGateD_08_HealthEndpoints | PASS | 0.01 s |
| E01 | TestGateE_01_ChaosSoak | PASS | 188.76 s |
| E02 | TestGateE_02_KillAdapterMidBurst | PASS | 42.89 s |
| E03 | TestGateE_03_KillTaskManagerMidBurst | PASS | 59.89 s |
| E04 | TestGateE_04_GracefulRestart | PASS | 157.15 s |
| E05 | TestGateE_05_CacheDown | PASS | 10.48 s |
| E06 | TestGateE_06_DatacoreRestart | PASS | 22.30 s |

### 6.2 Skips elsewhere

The only skip in any test file is `adlctl/main_test.go:150`, a unit test (`TestWaitNoServices`). It skips when no Docker socket exists, so it runs on the reference machine and on GitHub's Ubuntu runners.

### 6.3 Unit test counts

| Language | Static count | Runner report (`make test`, this run) |
|---|---|---|
| Go (datacore 40, adapter 32, labkit 42, adlctl 6) | 120 top-level `Test*` functions, plus 5 `t.Run` sites | 16 packages `ok`, served from Go's test cache because the inputs were unchanged. `go test -list` lists 120 tests |
| Java (processor) | 37 `@Test` | Tests run: 37, Failures 0, Errors 0, Skipped 0 |
| Rust (explorer) | 24 `#[test]` / `#[tokio::test]` | `test result: ok. 24 passed; 0 failed; 0 ignored` |
| Web (Vitest) | 7 `it`/`test` | 2 files, `Tests 7 passed (7)` |

## 7. Hygiene before going public (phase 4)

| Check | Result |
|---|---|
| Secrets, working tree | No hit for high-confidence patterns: AWS keys, GitHub, Slack, Anthropic, OpenAI and Google tokens, private keys, JWTs, npm and GitLab tokens. Generic `password/secret/token/key =` assignments show only environment references, placeholders, and the §17.2 lab defaults |
| Secrets, history (45 commits, all refs) | `git log -p --all` regex scan: nothing beyond the lab defaults. **gitleaks v8.30.1** (`git` mode, all refs): "no leaks found". The 5 commits it does not count change no files |
| Lab defaults outside `.env.example` | `labkit/config.go:32-33` and `labkit/config_test.go:23` repeat `adl-admin`/`adl-admin-secret` as client defaults (D-008). compose, `init-lake.sql` and the submitter only reference `${S3_*}` or `<S3_*>`. These values are the spec's own non-secret lab defaults, so this is not a secret. Listed as F-23 |
| Untracked files | gitleaks `dir` mode found 2 false positives (`go.sum` hashes) in `logs/`. `logs/` is gitignored, never committed, and not published |
| `.env` | Untracked (`.gitignore:5`), never committed (0 commits touch it), and identical to `.env.example` |
| `.env.example` | Exactly the five §17.2 lines; lab-only values |
| Sensitive file names ever added | None (`.pem`, `.key`, `id_*`, `credentials*`, `.p12`, `logs/`) among the 288 paths ever added |
| LICENSE | Present, MIT (§17.1) |
| `latest` and ranges | No `latest` in any build file (comments only), no `^`/`~`/`>=` in `package.json`, every Cargo dependency `=x.y.z`, no Maven ranges, every image tag exact. Selectors that are not exact and no decision covers: `ci.yml` `java-version: "17"` and `node-version: "24"` (F-15), and `alpine:3` in the locked `run-sprints.sh` |
| README commands | Every `make` target the README names exists in the Makefile. Every `adlctl` subcommand it names exists (`adlctl/main.go:68-82`, `control.go`). The ports match §15.8 and compose. All ten §17.2 sections are present. One inaccurate claim: F-03 |

## 8. Cold run (phase 5)

Run on the reference machine, in order. The stack was up from the sprint E build, and `make clean` removed it. The memory sampler ran from just before `make up` until `make reconcile` ended.

| Step | Start (UTC) | Duration | Result |
|---|---|---|---|
| `make clean` | 13:04:14 | 18 s | exit 0 |
| `make build` | 13:04:32 | 9 s | exit 0. Every layer was cached (BuildKit cache and images from the sprint E build), so this is not a fresh-clone build time |
| `make up` | 13:04:41 | **159 s** | exit 0, `wait: ready`. Every row was green, including `doris-be (SHOW BACKENDS) 172.30.0.3 alive=true` and `flink job adl-processor RUNNING` |
| Default-load wait | 13:07:20 | 120 s | Generator at 10/s, then a `docker stats` snapshot (§9) |
| `make acceptance` | 13:09:23 | **912 s** (`go test` 890.8 s) | exit 0. **38 PASS, 0 FAIL, 0 SKIP**, first attempt; no retry |
| `make reconcile` | 13:25:03 | **11 s** | exit 0. `reconcile: PASS (checked assets=3018 groups=11 lake_assets=3018 users=303)`. Two polling attempts showed 91 in-flight differences before the pass. Every category was 0: missing, extra, behind, ahead, field diff, and lake missing, behind, ahead and duplicate |
| `make down` | 13:25:16 | 9 s | exit 0. `savepoint written to file:/flink-data/savepoints/savepoint-fb807f-01cd2ba4f5f4`, then `docker compose stop` |
| `make test` (stack down) | 13:25:25 | 52 s | exit 0. Go 16 packages ok; Maven 37/37; Cargo 24/24; Vitest 7/7 |
| `make lint` (stack down) | 13:26:17 | 31 s | exit 0. buf lint 0 issues; buf breaking against `main`; golangci-lint 0 issues; Spotless clean; `cargo fmt --check`; clippy `-D warnings`; ESLint; Prettier ("All matched files use Prettier code style"); `tsc --noEmit` |

**Total, `make clean` to the end of `make reconcile`:** **21 min 0 s**, including the 120 s default-load wait.

**Container state after acceptance:** all 17 containers have `RestartCount=0` and `OOMKilled=false`. The 12 long-running services were running, and the 5 one-shot containers had exited 0. The start times are 13:21–13:24 because E04 runs `make down` and `make up` inside the suite, and E06 restarts datacore and E05 dragonfly.

**Working tree after the run:** `git status --porcelain` was empty, so nothing tracked was written by the run.

## 9. Memory

Caps: each container's §15.4 limit, and **11.5 GiB = 12,348,030,976 bytes** in total (R9).

### 9.1 Default load

Snapshot taken 2 minutes after `make up` was ready, with the generator at 10/s and chaos off.

| Container | Usage | Limit | % of limit |
|---|---|---|---|
| doris-fe | 1.229 GiB | 1.750 GiB | 70.2% |
| doris-be | 1.174 GiB | 3.250 GiB | 36.1% |
| flink-taskmanager | 1.077 GiB | 2.000 GiB | 53.8% |
| kafka | 0.710 GiB | 1.000 GiB | 71.0% |
| flink-jobmanager | 0.704 GiB | 1.250 GiB | 56.3% |
| iceberg-rest | 0.154 GiB | 0.500 GiB | 30.8% |
| rustfs | 0.083 GiB | 0.375 GiB | 22.1% |
| dragonfly | 0.044 GiB | 0.500 GiB | 8.8% |
| datacore | 0.019 GiB | 0.156 GiB | 12.0% |
| adapter | 0.019 GiB | 0.156 GiB | 11.9% |
| web | 0.003 GiB | 0.062 GiB | 5.4% |
| explorer | 0.001 GiB | 0.188 GiB | 0.8% |
| **Total** | **5.217 GiB** | 11,456 MiB of limits | **45.4% of 11.5 GiB** |

Taken at 13:09:21 UTC. For comparison, D-050 recorded 5.64 GiB.

### 9.2 Peaks over the whole run

Sampled every 10 s from `make up` through acceptance and `make reconcile`: 99 samples, 13:04:53–13:25:03 UTC.

| Container | Peak | Limit (= §15.4) | % of limit | At (UTC) |
|---|---|---|---|---|
| doris-be | 1.529 GiB | 3.250 GiB | 47.0% | 13:19:06 |
| doris-fe | 1.467 GiB | 1.750 GiB | **83.8%** | 13:19:43 |
| flink-taskmanager | 1.418 GiB | 2.000 GiB | 70.9% | 13:20:08 |
| kafka | 0.851 GiB | 1.000 GiB | **85.1%** | 13:12:22 |
| flink-jobmanager | 0.737 GiB | 1.250 GiB | 59.0% | 13:19:19 |
| iceberg-rest | 0.196 GiB | 0.500 GiB | 39.2% | 13:21:21 |
| rustfs | 0.116 GiB | 0.375 GiB | 31.0% | 13:19:31 |
| dragonfly | 0.054 GiB | 0.500 GiB | 10.8% | 13:18:42 |
| adapter | 0.025 GiB | 0.156 GiB | 16.2% | 13:06:59 |
| datacore | 0.023 GiB | 0.156 GiB | 14.5% | 13:21:08 |
| web | 0.008 GiB | 0.062 GiB | 12.6% | 13:25:03 |
| explorer | 0.007 GiB | 0.188 GiB | 3.7% | 13:17:41 |
| *one-shots:* kafka-init 0.086 GiB (**no limit**: F-07), flink-job 0.082 of 0.125 GiB, rustfs-init 0.015 GiB, doris-init 0.013 GiB, doris-init-lake 0.003 GiB | | | | |

**Peak concurrent total:** **6.107 GiB at 13:19:43 UTC, 53.1% of the 11.5 GiB cap.** This is the sum over every container running at that moment, one-shot containers included. No container exceeded its limit, and none was OOM-killed. The sum of the limits is 11,456 MiB (≤ 11.5 GiB), as R9 requires.

**Doris cgroups after acceptance:** BE `memory.peak` 1,503,600,640 and FE `memory.peak` 1,530,585,088, both since E04's restart at 13:22 UTC. Both cgroups show `memory.events max 0` and `oom_kill 0`. `be.WARNING` holds 0 `MEM_LIMIT_EXCEEDED` lines (compare §11).

## 10. Non-blocking findings (candidate tickets)

| # | Area | Finding |
|---|---|---|
| F-01 | Doris / processor | **Long-run degradation.** After about 5 h at default load the Doris BE reaches its 2 GB `mem_limit`. Compaction fails with `MEM_LIMIT_EXCEEDED` from 06:39 UTC, and Stream Loads are cancelled. The Flink job then restarts in a loop: 136 state transitions, 68 restores of `chk-640`, and no completed checkpoint after 08:58 UTC. Doris and the lake stop converging. No BE memory tuning exists, and no soak longer than minutes was ever measured (§12) |
| F-02 | Process | `specs/sprints/sprint-E/audit-report.md` is missing. `sprints-plan.md:207` requires the whole-repository audit, and `build-report.md:109` links to it |
| F-03 | README | "How this was built" says every sprint "was reviewed by an independent audit agent", which sprint E was not (F-02) |
| F-04 | Process | `specs/build-progress.md` was not updated for sprint E: its status still says "E · Next" |
| F-05 | Doris / Kafka | Thin memory margins. This run: FE 83.8% of 1792 MiB, Kafka 85.1% of 1024 MiB. D-050 recorded FE peaks of 84–87.5%. This is borderline for §15.4's SHOULD "well below" |
| F-06 | Tests | R2's "the BE at 172.30.0.3" is never asserted: `doris-init.sh` and `adlctl wait` accept any alive backend. No test asserts the topic configurations either |
| F-07 | Infra | `kafka-init` is the only one-shot container without a memory limit (compose:77-90) |
| F-08 | Infra | A bare `docker compose up` starts nothing, because every service has a profile. Only `make up` starts the stack |
| F-09 | Processor | `JobOptions.java:45` uses `Boolean.parseBoolean`, so any value other than `true` (`1`, `yes`, a typo) silently disables the lake branch |
| F-10 | Processor | The decoder has no length checks against the Doris `VARCHAR` sizes. A §6.7-valid oversize field could be rejected by Stream Load and restart the job in a loop (unverified) |
| F-11 | Processor | The submitter does not treat FAILING or CANCELLING as present, so a resubmission during a transition could create a duplicate job. Old job ids' checkpoints are never pruned |
| F-12 | Processor | Unit test gaps: VersionGate counters, late-record drop, unknown topic, the `EntityJson` `ingested_at` value, `LakeCatalog.ensureTable`, and `writeParallelism(1)` |
| F-13 | Adapter | A transient stall on one partition stalls polling for all of them, because of the per-batch commit. The readiness timer also counts stale-retry time. Unit test gaps: the gRPC deadline, the `ready()` composition, client options, log fields, and entity validation cases |
| F-14 | Adapter | Every cache failure logs a warning, two lines per trigger while Dragonfly is down. Rate-limit it |
| F-15 | CI | `ci.yml` pins `java-version: "17"` and `node-version: "24"`, not exact versions. Cargo runs without `--locked` (unlike the Makefile, contrary to D-043). Actions are pinned by tag, not by SHA. The images matrix builds `adl/flink` twice |
| F-16 | CI | `e2e.yml` has no smaller-runner fallback (`make up-core` with gates A and B) |
| F-17 | Explorer | `Code::Unknown` is treated as a transport error, which forces a reconnect. Lake errors are recognised by substring ("s3", "rustfs"). Non-GET methods return Axum's 405 without the §13.3 error body. The re-handshake retry is untested. D06 checks only the reported transport name |
| F-18 | Web | The 5 s refresh can overlap a slow request, so an older response can overwrite a newer one. A single failed refresh unmounts the card and the History table. Time zones are mixed (UTC labels, local "Last updated"). No tests for StatsPage, HistoryTable or the refresh interval |
| F-19 | datacore | A cancelled or expired context is returned as `INTERNAL`, not `CANCELED` or `DEADLINE_EXCEEDED`. The single SQLite connection is shared by reads and writes, which could slow `Get*` under a large Burst. The generator takes its ids from `SEED_*` env, not from the database. D-020's "seeded from the clock" wording is wrong |
| F-20 | Lake | No drift check on an existing table or catalog: `CREATE … IF NOT EXISTS` never updates either. `init-lake.sql` hard-codes the region. Persistence and writability depend on image defaults. No snapshot expiry or compaction (§12.4 leaves this for a ticket) |
| F-21 | Tests | C02 checks presence rather than the exact rows (U-2). A03 filters by key and raw unmarshal rather than `labkit.DecodeTrigger` |
| F-22 | Tooling | `make lint` on `main` runs `buf breaking` against `main` itself (a no-op). `make down` can take about 4.5 minutes in the worst case (90 s settle plus 3 min savepoint) |
| F-23 | Hygiene | The lab credentials (`adl-admin-secret`, `explorer`, Doris `root` with no password) are public by design. The README should say they are for local use only. `labkit/config.go` repeats them as defaults |
| F-24 | Docs | U-1 to U-4 should get decision-log entries (G3) |
| F-25 | Repository | The build-phase files at the root (`CLAUDE.md`, `run-sprints.sh`) sit outside the §17.1 layout, and `CLAUDE.md` describes itself as temporary |

## 11. Pre-run observation: the 11-hour stack

Before `make clean`, the stack left by the sprint E gate (started 2026-11-26 01:30 UTC) was inspected. The evidence is saved in the session scratchpad.

| Time (UTC) | Event |
|---|---|
| 01:31 | Job `adl-processor` (`acc9f2ea…`) RUNNING |
| 06:39 | First BE warning: `failed to do cumulative compaction … [MEM_LIMIT_EXCEEDED] … process memory used 2.00 GB, limit 2.00 GB` |
| 06:56 | First job restart |
| 08:58 | Last completed checkpoint (`chk-640`) |
| 12:31 | TaskManager restarted (RestartCount 1, OOMKilled false) |
| 13:02 | Job in RESTARTING. Latest exception: `Stream load … [MEM_LIMIT_EXCEEDED] … in backend 172.30.0.3 … process memory used 2.00 GB, limit 2.00 GB`. 2,455 `MEM_LIMIT_EXCEEDED` lines in `be.WARNING`; 68 restores; 38 failed checkpoints |

At 13:02:
- The BE container used 2.14 GiB of its 3.25 GiB limit (cgroup `memory.events max 0`, no OOM kill). Doris's own limiter cancels the work before the container limit is ever reached.
- The FE used 1.49 GiB of 1.75 GiB.

Every gate and acceptance run so far lasted minutes. This is the first evidence about uptime beyond five hours, and it means §1.5 freshness is not held on a long-running stack.
