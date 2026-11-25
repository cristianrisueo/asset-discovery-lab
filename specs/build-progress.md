# Build progress

External memory between build sessions (sprints plan §2). Read it right after the spec and the sprints plan.

## Status

| Sprint | Status | Report |
|---|---|---|
| 0 · Foundations and the test contract | **Done** (gate green, audited, committed) | `specs/sprints/sprint-0/` |
| A · Ingestion | **Done** (gate green, audited, committed) | `specs/sprints/sprint-A/` |
| B · Processing | Next | — |
| C · Lake | Pending | — |
| D · Serving | Pending | — |
| E · Failures and close | Pending | — |

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

## Next: sprint B (Processing)

Plan §4, sprint B:
- Doris FE and BE (`infra/doris/`, static IPs 172.30.0.2 and .3, outside the dynamic `ip_range`).
- `doris-init` and the core init SQL with its grants.
- The Flink image (`infra/flink/`) and the processor job (§10), with `--iceberg.enabled=false` tested.
- The `flink-job` submitter (§10.11) and `make down` with stop-with-savepoint.
- Add the Maven test and Spotless steps to `make test` and `make lint` in a builder container. This replaces the "not available until sprint B" stubs, which fail once `processor/pom.xml` exists.
- The gate also needs `make reconcile`.

## Known issues and notes

- **E04** expects the job resubmitted by `make up` after `make down` to have a new job id. The sprint B submitter must not pin a fixed `jobId`.
- **The acceptance precheck** dials only the host ports of §15.8. Every service must publish exactly those ports.
- **The generator changes LT-0042, U-0117 and G-03 too** (allowed by §5.4). The sprint E walkthrough and README must not assume the seed values; for example, pause the generator or say "from its current site" (sprint A audit F-10).
- **The adapter exits on purpose** when a produce fails outside shutdown (`ErrIncomplete`), and compose restarts it. A restart loop in its logs points at Kafka, not at the adapter.
- **Open questions:** Q-001 (interpretations of A05, B08, D04, E04) and Q-002 (how kadm is versioned). Sprint A raised no new ones.
