# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Build-phase artifacts (specs, build runner, build-phase CLAUDE.md) moved to development/

### Fixed

- The processor's Maven builds retry HTTP 429 and 503 responses from Maven Central 10 times, 10 s apart, instead of the resolver's default of 3 times, 5 s apart. This applies to the `flink-job` image, the CI `java` job, `make test` and `make lint`. The CI image build of `flink-job` had failed on a Maven Central rate limit.
- `make down` right after a Flink failover now takes its savepoint:
  - `adlctl flink stop-with-savepoint` waits until every task of the job runs, not only until Flink reports the job as RUNNING;
  - it retries a rejected savepoint within its timeout while the same job is still active.

  Before, a stop triggered while tasks were still starting was rejected ("Not all required tasks are currently running"), and the next `make up` restored from an older checkpoint. This failed E04 on the GitHub runner.

## [0.1.0] - 2026-11-26

The base of the lab. An AI agent built it unattended from the system specification, in six sprints: 0 and A to E.

### Added

#### datacore

- Simulated DataCore service in Go. SQLite (`modernc.org/sqlite`, WAL) is the source of truth for assets, users and groups, and every change raises the version by exactly one.
- Transactional outbox and franz-go relay (idempotent, `acks=all`) that publishes one trigger per change to `triggers`.
- Deterministic seed (value 42): 8 groups, 300 users and 3,000 assets, including the LT-0042 / U-0117 / G-03 example.
- `DataCoreService` (`Get*`) and the lab `AdminService` (upserts, streaming lists, generator, chaos, burst, stats), with gRPC reflection.
- Change generator, 10 changes per second by default, and five chaos knobs: duplicate, malformed, phantom, stale read and unavailable.
- Health endpoints, a `datacore healthcheck` subcommand, JSON logs and graceful shutdown.

#### adapter

- Go adapter that turns triggers into full entities. It looks in the Dragonfly cache, then calls `Get*` with deadlines and retry with backoff, then publishes to `entities.*` with the `trigger.version` header.
- At-least-once delivery. Offsets are committed only after every publish is acknowledged. A failed commit is retried, then the adapter restarts.
- Dead letters on `dlq.adapter` with reason headers: `DECODE_ERROR`, `VALIDATION_ERROR`, `NOT_FOUND` and `STALE_VERSION`.
- Readiness that reports a stall after 30 s of transient retries.
- Debug publish logs with partition and offset; the log level comes from compose.

#### processor

- Flink 2.1.3 job `adl-processor` (Java 17, DataStream API, Source and Sink V2):
  - a `KafkaSource` over the three entity topics;
  - a decoder that never throws, with validation and a dead-letter side output to `dlq.processor`.
- `VersionGate` keyed state that drops stale and duplicate versions, with `versionGateAccepted` and `versionGateDropped` counters.
- Four two-phase-commit JSON Stream Load sinks into Doris, with a label prefix per run.
- Iceberg branch: a `FlinkSink` into `inventory.asset_changes`. The job creates the table on start, and commits happen on each checkpoint.
- One-minute event-time change-rate window, written to `change_rate_1m`.
- Flink cluster image with JSON console logs.
- REST job submitter that restores from the newest savepoint or checkpoint.

#### Doris

- Doris 4.1.5 FE and BE as separate containers in election mode, with static addresses and Arrow Flight SQL enabled.
- Database `inventory` with:
  - merge-on-write Unique Key tables that use `version` as the sequence column;
  - the enriched view `v_assets_enriched`;
  - the async materialized view `mv_assets_by_os_site`;
  - the Duplicate Key table `change_rate_1m`.
- Read-only `explorer` user, UTC time zone, and an idempotent `doris-init`.
- FE heap of 1024m and capped malloc arenas, so the FE stays inside its 1792 MiB limit.

#### lake

- RustFS object storage with the `warehouse` bucket.
- Iceberg REST catalog fixture, persisted on a named volume.
- Iceberg table `inventory.asset_changes`: format version 2, partitioned by `day(changed_at)`, Parquet with zstd.
- Doris Iceberg catalog `lake` for history queries.

#### explorer

- Rust (Axum) HTTP JSON API with these endpoints:
  - asset, list, history, OS × site and change rate;
  - `/healthz`, and `/readyz` reporting the Doris transport.
- Arrow Flight SQL to Doris: handshake with the FE, then `DoGet` against the BE. The token is reused, and a new handshake happens only on `UNAUTHENTICATED`.
- Injection-safe input validation, the specified JSON shapes, error-to-status mapping and JSON logs.

#### web

- React 19, Vite and TypeScript app:
  - asset page: LT-0042 prefilled, the card refreshed every 5 s, and a version history table;
  - stats page: OS × site matrix and change-rate bars;
  - loading, empty, not-found and error states.
- nginx image that proxies `/api/` to the explorer and falls back to `index.html`.

#### tooling

- `adl.v1` Protobuf contracts with buf (STANDARD lint, FILE breaking) and committed generated Go and Java code.
- Docker Compose environment:
  - profiles `core`, `lake` and `app`;
  - a fixed network, health checks, and a memory budget of 11,456 MiB;
  - Kafka 4.3 in KRaft mode with six topics, and Dragonfly.
- Makefile with every target of the specification: `up`, `up-core`, `down` with a savepoint, `clean`, `test`, `lint`, `acceptance`, `reconcile`, `chaos-on`, `chaos-off`, `burst` and `move`. Java, Rust and Node run in builder containers.
- `labkit` client library and the `adlctl` control CLI: `wait`, `generator`, `chaos`, `burst`, `move`, `reconcile`, `flink stop-with-savepoint` and `stats`.
- Reconciliation of Doris and the lake against datacore (`make reconcile`).
- README, decision log (D-001 to D-050) and open questions.

#### CI

- `ci.yml`, with these jobs:
  - contracts: buf lint, breaking and generation drift;
  - Go: vet, golangci-lint, race tests;
  - Java: Maven verify with Spotless;
  - Rust: fmt, clippy, tests;
  - web: lint, type-check, tests, build;
  - Docker image builds.
- `e2e.yml`: builds the images, starts the full stack and runs the acceptance suite on pushes to `main`.

#### acceptance suite

- 38 black-box acceptance cases in five gates: A ingestion, B processing, C lake, D serving and E failures. The failure gate covers a chaos soak, killing the adapter and the Flink TaskManager mid-burst, a graceful restart from a savepoint, the cache down and a datacore restart. Run them with `make acceptance`.

### Known limitations

- Some acceptance cases implement a recorded reading of their row (Q-001):
  - A05 accepts at least two cache-served copies;
  - B08 counts from the Flink job's start;
  - D04 pauses the generator before paging;
  - E04 proves the new savepoint through `docker compose exec`.
- `github.com/twmb/franz-go/pkg/kadm` is pinned at v1.20.0. It is a separate module, and the v1.22.3 that the specification lists next to franz-go does not exist for it (Q-002).
- Neither CI workflow had run on GitHub when 0.1.0 was tagged. `e2e.yml` needs a runner with at least 16 GB of RAM (Q-003).

[Unreleased]: https://github.com/cristianrisueo/asset-discovery-lab/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/cristianrisueo/asset-discovery-lab/compare/plan-v1...v0.1.0
