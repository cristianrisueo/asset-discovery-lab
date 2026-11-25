# Sprint A build report — Ingestion

## What was built

| Area | Spec | Files |
|---|---|---|
| Kafka (KRaft, single node) | §8.1 | `docker-compose.yml` service `kafka`: §8.1 environment, fixed `CLUSTER_ID`, volume `kafka-data` on `/var/lib/kafka/data`, host port 29092, 1024M limit, health check |
| Topic creation | §8.2, §8.4 | `infra/scripts/create-topics.sh`, run by the one-shot `kafka-init` (`restart: "no"`). It is idempotent: a second run exits 0 |
| Dragonfly | §2.3, §15.4, §15.6 | Service `dragonfly`: `--proactor_threads=1 --maxmemory=256mb --cache_mode=true`, memlock unlimited, port 6379, 512M limit, `redis-cli ping` health check |
| datacore | §7 | `datacore/`: `main.go` (serve, healthcheck, SIGTERM order), `config.go`, `health.go`; `internal/model`, `internal/store`, `internal/chaos`, `internal/generator`, `internal/outbox`, `internal/grpcapi`; `Dockerfile` |
| adapter | §9 | `adapter/`: `main.go`, `config.go`, `health.go`; `internal/contract` (§6.7 rules), `internal/consumer`, `internal/handler`, `internal/fetcher`, `internal/cache`, `internal/publisher`; `Dockerfile` |
| Images | §15.11 | Multi-stage builds: `golang:1.26.11-bookworm` with BuildKit cache mounts, then `gcr.io/distroless/static-debian12:nonroot`. Build context is the repository root, kept small by `.dockerignore` |
| Makefile | §15.9 | `build` runs `env`, then `compose build`. `up` runs `env` and `host-prep`, then `compose up -d` and `adlctl wait --timeout 15m`. `up-core` does the same with `--profile core`, `ICEBERG_ENABLED=false` and `adlctl wait --core`. `host-prep` calls `infra/scripts/host-prep.sh` |
| Go dependency | §2.5 | `modernc.org/sqlite` v1.63.0 |

**Design highlights**
- **datacore storage:** one SQLite connection serialises every write, so outbox seq order equals commit order. `Apply` runs §7.3 in one transaction and is detached from cancellation, so a change that has started always finishes. `changed_at` is truncated to milliseconds before it is stored. The `UpsertAsset` response, `GetAsset`, the trigger and the Kafka record therefore all carry the same timestamp, which A02, A03 and A05 compare with `proto.Equal`.
- **Seed:** deterministic from `SEED`, with the fixed LT-0042, U-0117 and G-03 example. It runs in batches of 500 and resumes if a previous start was interrupted, guarded by a `seed_complete` meta row. On the reference stack it takes about 0.1 s.
- **Relay:**
  - It produces each batch with `ProduceSync` (idempotent, `acks=all`) and deletes rows only after every ack.
  - Chaos extras are added next to the real trigger, never instead of it.
  - `/readyz` waits for the seed **and** the first successful Kafka ping.
- **Adapter consumer:**
  - `DisableAutoCommit`, `BlockRebalanceOnPoll`, reset to the earliest offset, and a 10 s session timeout.
  - One goroutine per partition handles its records in order. Produces are asynchronous and return an ack.
  - After the batch, each partition commits the end of its fully acknowledged prefix. After SIGTERM, records not handled within the 7 s grace are not committed. A record that fails outside shutdown stops the consumer (`ErrIncomplete`), and the adapter exits so it restarts from the last committed offset.
- **Adapter handler:** decode, validate, cache GET on the trigger version, fetch, cache SET under the entity's own version (synchronously), then publish or dead-letter.
- **Fetcher:**
  - Every §9.4 outcome is classified.
  - The fast retries are 100, 200 and 400 ms with ±20 % jitter, then `min(2 × previous, 5 s)` without limit.
  - Stale reads retry until `STALE_MAX_WAIT_MS`.
  - Transient time is tracked per partition, and `/readyz` reports 503 after 30 s of it.

## How to verify

```sh
make build && make clean && make up-core   # ready in about 35 s on the reference machine
make test lint
make acceptance-gate GATE=A
```

Results at the end of the sprint:
- **`make up-core`:** `wait: ready` in about 34 s from a clean state. `host-prep` ran inside it without being blocked.
- **`make test lint`:** all Go unit tests pass. buf lint passes, buf breaking against main passes, and golangci-lint v2.15.0 reports **0 issues**. `go test -race ./datacore/... ./adapter/...` also passes.
- **`make acceptance-gate GATE=A`:** **10/10 PASS** in 15 s. Two more runs right after also passed (30 s and 18 s).
- **Sprint 0 gate:** buf lint, buf build, go vet, go build, go vet with the acceptance tag, the 38-case count and go test are all green.
- **After the seed:** `adlctl stats` shows `outbox_pending 0` and `adl-adapter` lag 0 a few seconds after ready. Dead letters and retry escalations in the adapter log match exactly what the gate A tests provoke.
- **Early checks of gate E behaviour** (a throwaway test against the live stack, not committed):
  - with Dragonfly stopped, an upsert reached `entities.assets` in about 1 s (E05);
  - after `docker compose restart datacore`, `GetAsset` kept the version and a new upsert gave v+1 (E06);
  - when the adapter was killed 1 s into `Burst(1000)` and started 10 s later, `adl-adapter` lag reached 0 about 1 s after the restart (E02).

`docker stats --no-stream`, core stack at default load:

| Container | Usage / limit |
|---|---|
| adapter | 16.7 MiB / 160 MiB |
| datacore | 17.2 MiB / 160 MiB |
| kafka | 627 MiB / 1 GiB |
| dragonfly | 37.7 MiB / 512 MiB |

## Deviations from the plan

- **`internal/contract` in the adapter:** this package is not in the plan's layout. The §6.7 rules are needed by both the handler (triggers and cache hits) and the fetcher (entities), so they live in one package instead of creating an import cycle. datacore's `internal/` cannot be imported from `adapter/`, so the adapter keeps its own copy of the id patterns (decision D-019).
- **Dragonfly `--version_check=false`:** added so the cache never phones home from the lab. The startup log showed an outbound version check.
- **Kafka volume:** the plan's fallback was not needed. The image declares `/var/lib/kafka/data` as a volume owned by `appuser`, so `kafka-data` mounts there directly.
- **One more early check:** besides E05 and E06, the throwaway check also covered E02 (adapter kill mid-burst), because the cost was the same.
- **Audit fixes** (audit report §4, D-022):
  - **F-01 (major):** the adapter's graceful stop used to hang until SIGKILL, because `Close` with `BlockRebalanceOnPoll` waits for `AllowRebalance`. It now closes with `CloseAllowingRebalance`, and `Run` always allows rebalances when it returns.
  - **F-02 to F-05:** a record that fails outside shutdown stops the consumer, and the process restarts from the last committed offset. Failed produces are no longer re-sent. Readiness tracks the assigned partitions. The escalation log fires at the right attempt.
  - **F-09:** the gRPC reconnect backoff is bounded by `RETRY_MAX_MS`.
  - The full gate was re-run afterwards and is green.

## Notes for the next sprints

- **Every service publishes exactly the §15.8 host ports:** 29092, 6379, 50051, 8090 and 8091.
- **Sprint B adds to `docker-compose.yml`:**
  - the Doris and Flink services in profile `core`;
  - `doris-fe` and `doris-be` with the static IPs of §15.3; dynamic addresses come from `172.30.0.128/25`, so the static ones are free.
- **Producing to `entities.*`:** the adapter's entity records have key = id, the value is the deterministic Protobuf serialization, and the header is `trigger.version`. The processor's `KafkaSource` reads these. The topics already exist (`kafka-init`).
- **`make down`** runs `adlctl flink stop-with-savepoint` before `compose stop`. With no Flink service it fails cleanly and turns into a warning, as before.
- **The images build from the repository root.** `.dockerignore` excludes `processor`, `explorer`, `web` and `infra`. A later image that builds from the root context and needs one of those must adjust it; images with their own context (`processor/`, `explorer/`, `web/`, `infra/...`) are not affected.
