# Sprint D build report: Serving

## What was built

| Area | Spec | Files |
|---|---|---|
| explorer crate | §13, §2.6, §17 | `explorer/`: `Cargo.toml` (exact pins), `Cargo.lock`, `rust-toolchain.toml` (1.100.0), and a flat `src/`: `api.rs` (Axum routes, request log), `service.rs` (use cases, §13.3 JSON), `flight.rs` (Flight SQL), `convert.rs` (Arrow → JSON), `validate.rs`, `sql.rs`, `error.rs`, `config.rs`, `logging.rs`, `health.rs`, `main.rs` |
| Flight SQL, R4 step 1 | §3 R4, §13.6 | `flight.rs`: handshake (basic → bearer), `GetFlightInfo`, `DoGet` against the endpoint location (`grpc+tcp://172.30.0.3:8050` → `http://…`) with the token. One reused session; re-handshake once on `UNAUTHENTICATED`; drop and reconnect on transport errors; `QUERY_TIMEOUT_MS`; 64 MiB messages (D-040) |
| Endpoints | §13.2–§13.5 | `/healthz`, `/readyz` (`doris_transport`), `/api/assets/{id}`, `/api/assets`, `/api/assets/{id}/history`, `/api/stats/os-site`, `/api/stats/change-rate`. Every error uses the §13.3 body; unknown routes return 404 JSON (D-041) |
| explorer image | §15.11, §2.3 | `explorer/Dockerfile`: `toolchain` stage (`rust:1.100.0-bookworm` + rustfmt and clippy), release build with cache mounts, runtime `debian:bookworm-20261123-slim` as uid 10001. Image 83 MB |
| web | §14 | `web/`: Vite 8, React 19, TypeScript 5.9, no UI library. Asset page (search prefilled with LT-0042, card refreshing every 5 s with "Last updated", History table), Stats page (OS × site matrix, change-rate CSS bars for the last 60 minutes). Loading, empty and error states; 404 shows "not found" (D-042) |
| web image | §14.3 | `web/Dockerfile` (Node 24 build, `nginx:1.30.8-alpine`) and `web/nginx.conf`: `/api/` proxied to `explorer:8080` through Docker DNS, SPA fallback, JSON access log with `service=web`, 2 workers |
| Compose, profile `app` | §15.2–§15.8 | `explorer` (8080, 192M, `explorer healthcheck` 15/5/10, depends on `doris-init` and optionally `doris-init-lake`, `LAKE_ENABLED=${ICEBERG_ENABLED:-true}`) and `web` (3000:80, 64M, `wget` check 10/5/10, depends on a healthy explorer). The long-running limits total 11,456 MiB, the §15.4 budget |
| Makefile | §15.9 | The sprint D stubs in `test` and `lint` are replaced: Cargo in `adl/explorer-toolchain:0.1.0` (`test --locked`, `fmt --check`, `clippy --all-targets -D warnings`) and npm in `node:24.22.3-bookworm-slim` (`npm ci`, then `npm test`, or `lint` and `typecheck`), with cache volumes outside the compose project (D-043) |
| Unit tests | §16.2 | explorer: 24 tests (validation with injection strings, SQL text, Arrow → JSON for every type Doris sends, error → HTTP mapping, router tests with a fake transport, config, health check). web: 7 Vitest tests (card from a fixture, error state, "not found", network failure) |

No Go, Java or CI change was needed. `adlctl wait` picks up the two new services through their health checks, labkit already knew the explorer and web endpoints, and `ci.yml` enables its `rust` and `web` jobs once `explorer/Cargo.toml` and `web/package.json` exist.

## How to verify

```sh
make down                               # heavy Cargo and npm builds run with the stack stopped
make test lint                          # warms the Cargo and npm cache volumes
make build && make clean && make up
for g in A B C D; do make acceptance-gate GATE=$g || exit 1; done
curl -s localhost:8080/readyz           # {"status":"ready","doris_transport":"flight-sql"}
open http://localhost:3000/             # LT-0042 card; History; Stats
```

## Results

Two full gate runs, each exactly as plan §4 writes it, plus the sprint 0 checks and `make reconcile`.

**Run 1** (FE heap 1280m, 23:02–23:13 UTC):
- `make build && make clean && make up`: `wait: ready` about 100 s after `make clean`. explorer and web are healthy.
- `make test lint` with the full stack up: green in 1 min 43 s. Go ok, Maven 37/37, Cargo 24/24, Vitest 7/7, buf, golangci-lint 0 issues, Spotless, rustfmt, clippy `-D warnings`, ESLint, Prettier, `tsc --noEmit`.
- Gates: A 10/10, B 9/9 (B09 p95 10.27 s, max 10.35 s), C 5/5, D 8/8.
- Sprint 0 checks green. `make reconcile`: `PASS (checked assets=3015 groups=11 lake_assets=3015 users=303)`.

**Run 2** (after D-044, FE heap 1024m, 23:14–23:24 UTC): the same, all green.
- `make up` ready in about 2 minutes, with the FE image rebuilt.
- `make test lint`: 1 min 23 s.
- Gates: A 10/10, B 9/9 (B09 p95 10.19 s, max 10.26 s), C 5/5 (C02 15.2 s), D 8/8. That is 32 PASS and 0 FAIL.
- Gate D timings: D01 2.6 s, D02 0.04 s, D03 20.3 s (both versions visible in the history through the explorer), D04 10.7 s (includes a reconcile), D05 1.3 s, D06–D08 under 0.1 s.
- Sprint 0 checks green; `make reconcile` PASS with the lake compared.

**Live checks:**
- `/readyz` returns `{"status":"ready","doris_transport":"flight-sql"}`.
- The explorer log shows the FE handshake, then a channel to the endpoint location `http://172.30.0.3:8050` (Doris sends `grpc+tcp://172.30.0.3:8050`).
- The web serves `/` and `/stats` (`<div id="root">`), and `/api/assets/LT-0042` through the proxy.
- Arrow types Doris sends are listed in D-040, and each one is covered by a unit test.

### Memory (`docker stats --no-stream`, full stack at default load after run 2)

| Container | Usage / limit |
|---|---|
| doris-fe | 1.431 GiB / 1.75 GiB (cgroup peak 83%, no `max` events) |
| doris-be | 1.4 GiB / 3.25 GiB |
| flink-taskmanager | 1.033 GiB / 2 GiB |
| flink-jobmanager | 714 MiB / 1.25 GiB |
| kafka | 708 MiB / 1 GiB |
| iceberg-rest | 195 MiB / 512 MiB |
| rustfs | 101 MiB / 384 MiB |
| dragonfly | 45 MiB / 512 MiB |
| datacore | 15 MiB / 160 MiB |
| adapter | 12 MiB / 160 MiB |
| explorer | 4.5 MiB / 192 MiB |
| web | 7.3 MiB / 64 MiB |

No container was OOM-killed or restarted.

## Deviations from the plan

- **R4:** step 1 worked first time (D-040). No hypothesis ladder was needed, and steps 2 and 3 are not built. The client is `arrow-flight`'s generated `FlightServiceClient`, speaking Flight SQL commands, rather than the `FlightSqlServiceClient` wrapper: the wrapper hides gRPC status codes that §13.6's re-handshake and reconnect rules need.
- **R9 applied (D-044):** step 11 found the FE at 95.6% of its limit at peak, with the heap still growing toward a size that cannot fit. The FE heap is now 1024m instead of §11.2's 1280m, and the gate was re-run from clean (run 2).
- **nginx IPv6 listener (D-042):** the first `make up` stalled on the web health check. `localhost` resolves to `::1` in Alpine, and nginx listened only on IPv4. Adding `listen [::]:80` fixed it without changing the §15.6 check.
- **Extra explorer debug logs:** the raw endpoint location and the first batch's schema at `debug` level, used as D-040 evidence.
- **`serde_json` `preserve_order`:** JSON keys follow the §13.3 order. Not in the plan; it is cosmetic.
- **No Go, Java or CI change** was needed, as the plan expected. No bug from an earlier sprint was found.
