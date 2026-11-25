# Build progress

External memory between build sessions (sprints plan §2). Read it right after the spec and the sprints plan.

## Status

| Sprint | Status | Report |
|---|---|---|
| 0 · Foundations and the test contract | **Done** (gate green, audited, committed) | `specs/sprints/sprint-0/` |
| A · Ingestion | Next | — |
| B · Processing | Pending | — |
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

## Key decisions (see `specs/decision-log.md`)

- D-002: kadm v1.20.0, a separate module from franz-go v1.22.3.
- D-003: `go 1.26.11` in `go.mod`; the local toolchain is 1.27.2.
- D-004: the processor must use `protobuf-java` **4.36.2**.
- D-005: buf and golangci-lint run through pinned `go run` in the Makefile.
- D-007: `host-prep` is inline in the Makefile. Sprint A may move it into `infra/scripts/`, together with `create-topics.sh`.
- D-011 and D-012: acceptance harness rules and the interpretations of §16.4 rows.

## Next: sprint A (Ingestion)

Plan §4, sprint A:
- Kafka and `kafka-init`, Dragonfly, datacore (§7) and the adapter (§9), each with a Dockerfile and a compose service in profile `core` with health checks.
- Replace the `build`, `up` and `up-core` stubs in the Makefile (§15.9): `up` runs `env` and `host-prep`, then compose up and `adlctl wait --timeout 15m`; `up-core` uses `--profile core`, `ICEBERG_ENABLED=false` and `adlctl wait --core`.
- Add the required unit tests of §16.2 for datacore and the adapter.
- The gate needs `make test lint` and `make acceptance-gate GATE=A` green.

## Known issues and notes

- **E04** expects the job resubmitted by `make up` after `make down` to have a new job id. The sprint B submitter must not pin a fixed `jobId`.
- **The acceptance precheck** dials only the host ports of §15.8. Every service must publish exactly those ports.
- **Ports that must answer:** the precheck connects to datacore on both `50051` and `8090`, and to the adapter on `8091`. All must answer once the services are up.
- **Open questions:** Q-001 (interpretations of A05, B08, D04, E04) and Q-002 (how kadm is versioned).
