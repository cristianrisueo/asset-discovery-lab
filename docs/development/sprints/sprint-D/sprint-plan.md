# Sprint D plan: Serving

## Context

Sprints 0, A, B and C are done and tagged (`sprint-C-done`). The full pipeline works: datacore → Kafka → adapter → Flink → Doris (current state) and Iceberg on RustFS (history). Doris reads the lake through catalog `lake`. Nothing serves the data yet: `explorer/` and `web/` do not exist, the compose file has no `app` profile services, and the `make test` / `make lint` branches for Cargo and npm are stubs that fail on purpose ("not available until sprint D") once `explorer/Cargo.toml` or `web/package.json` exist.

Sprint D (plan §4) has to:
- build the **explorer** (spec §13): Rust and Axum, querying Doris over Arrow Flight SQL following the R4 ladder, and record which step worked;
- build the **web** (spec §14): React, Vite and TypeScript, served by nginx, proxying `/api/` to the explorer;
- add both to compose in profile `app`, and make `make test lint` run Cargo and npm in builder containers.

Gate (plan §4, sprint D), plus the sprint 0 checks, because every gate re-runs the earlier ones (plan §4 preamble):
```sh
make build && make clean && make up
make test lint
for g in A B C D; do make acceptance-gate GATE=$g || exit 1; done
# sprint 0: buf lint && buf build; go vet ./...; go build ./...; go vet -tags acceptance ./acceptance/...; 38-case count; go test ./...
```
`make reconcile` is not in the D gate, but I run it once after the gates to keep the C state honest.

Locked: `specs/system-spec.md`, `specs/sprints-plan.md`, `CLAUDE.md`, `run-sprints.sh` and `acceptance/`. None of them is edited. The next decision id is **D-039**, the next question id **Q-003**.

## What already exists and gets reused

- **Acceptance gate D** (`acceptance/gate_d_test.go`, locked) defines the exact contract: the Asset JSON (with `owner` nested, `name`/`group_id`/`group` nullable), the History JSON, the list shape `{"items","limit","offset"}`, the error body `{"error","message"}`, and `/readyz` with `doris_transport`. `getStable` retries on 503 and transport errors, so 503 is the right answer while Doris is unreachable. D05 compares `/api/stats/os-site` with `fmt.Sprint` of the MV rows read over MySQL, so the explorer must return the MV rows in the database's order with integer `assets`.
- **labkit** already knows `CompExplorer` (`http://localhost:8080`) and `CompWeb` (`http://localhost:3000`) for the precheck, and `ADL_ALLOW_MYSQL_FALLBACK`. **No Go change is expected.**
- **`adlctl wait`** evaluates every defined compose service: long-running services with a health check must be `healthy`. The explorer and web get compose health checks, so `make up` waits for them with no Go change.
- **Doris** already listens for Flight SQL: `arrow_flight_sql_port` is 8070 on the FE and 8050 on the BE (D-026, `infra/doris/{fe,be}.Dockerfile`). The `explorer` user exists with `SELECT_PRIV` on `internal.inventory.*` and `lake.*.*` (`init-core.sql`, `init-lake.sql`). `DORIS_EXPLORER_PASSWORD=explorer` is in `.env.example`.
- **Makefile:** `COMPOSE_ALL` already includes `--profile app`; `up` already waits with `adlctl wait --timeout 15m`. The Maven builder pattern (`docker run --rm`, cache volume outside the compose project, `--memory`) is the template for Cargo and npm.
- **CI:** `ci.yml` already enables the `rust` and `web` jobs when `explorer/Cargo.toml` and `web/package.json` exist. It runs `cargo fmt --check`, `cargo clippy --all-targets -- -D warnings`, `cargo test` in `explorer/`, and `npm ci`, `npm run lint`, `npm run typecheck`, `npm test`, `npm run build` in `web/`. The `images` job picks up any compose service with a `build:` section. So the script names and file locations below are fixed by CI; **no CI change is expected.**
- **Root `.dockerignore`** excludes `explorer` and `web`; that is fine because both images use their own directory as the build context, each with its own `.dockerignore`.
- `.gitignore` already covers `target/`, `dist/` and `node_modules/`.

## Pins (checked during plan mode on 2026-11-25; re-verify and record in D-039)

| Item | Pin | Source |
|---|---|---|
| Rust toolchain (`rust-toolchain.toml`, builder image) | `1.100.0` (`rust:1.100.0-bookworm`) | Docker Hub; newest stable, ≥ 1.88 (spec §2.2) |
| Runtime base, explorer | `debian:bookworm-20261123-slim` | Docker Hub, spec §2.3 "debian:bookworm-slim" pinned to a dated tag |
| `arrow-flight` (feature `flight-sql`), `arrow` family | `=60.2.0` | crates.io (MSRV 1.88) |
| `tonic` | the version `arrow-flight 60.2.0` requires (0.14.x; 0.14.9 is newest) | crates.io, Cargo.lock |
| `axum` / `tokio` | `=0.8.9` / `=1.54.1` | crates.io |
| `serde`, `serde_json`, `tracing`, `tracing-subscriber` (json, env-filter), `futures`, `chrono` | latest at build, exact | crates.io; `chrono` matched to arrow's own |
| Node builder | `node:24.22.3-bookworm-slim` | Docker Hub, Node 24 LTS |
| nginx runtime | `nginx:1.30.8-alpine` | Docker Hub; 1.30 is the stable line (1.31 is mainline) |
| React / react-dom | `19.3.1` | npm |
| Vite / `@vitejs/plugin-react` | `8.3.8` / `6.3.0` | npm |
| TypeScript | `5.9.3` (newest 5.x; 7.0 exists but spec §2.7 says 5.x) | npm |
| Vitest / jsdom / Testing Library (react, dom, jest-dom) | `5.1.2` / `31.0.1` / newest compatible | npm |
| ESLint / `@eslint/js` / `typescript-eslint` / `eslint-plugin-react-hooks` / `globals` / Prettier | `10.15.0` / `10.0.1` / `8.74.0` / `7.1.1` / `17.14.0` / `3.10.1` | npm |

Every version is pinned exactly in `Cargo.toml` (`=x.y.z`) and `package.json` (no `^`/`~`), with `Cargo.lock` and `package-lock.json` committed. If a peer-dependency conflict appears at install time, step down to the newest compatible version and record it (G3).

## Steps

### 0. Save the plan (auto mode, first action)
Save this plan verbatim to `specs/sprints/sprint-D/sprint-plan.md`.

### 1. Stop the stack and pin versions (plan §3.6, spec §2.1, §15.11, G3)
- `make down` (savepoint), so the heavy Cargo and npm builds run with the stack stopped (build-progress note; spec §15.11).
- Re-check the pins above (crates.io API, Docker Hub, npm registry). Check the exact `arrow-flight 60.2.0` feature name (`flight-sql`) and its `tonic` requirement in its `Cargo.toml`.
- Record them in **D-039** ("latest at build" pins for sprint D).

**Verification:** each pin cited in D-039 with its source.

### 2. explorer crate skeleton, config and logging (spec §13.1, §13.7, §13.8, §17)
Create `explorer/`:
- `Cargo.toml` (package `explorer`, edition 2024, exact pins, `[profile.release] strip = true`), `Cargo.lock`, `rust-toolchain.toml` (`channel = "1.100.0"`, components `rustfmt`, `clippy`, profile minimal), `.dockerignore` (`target`).
- Source files, flat in `explorer/src/` (no single-file directories, §17.1), layered transport → service → repository like the Go services:
  - `main.rs`: subcommands `serve` (default) and `healthcheck`; tokio runtime; graceful shutdown on SIGTERM/SIGINT.
  - `config.rs`: the §13.7 variables with their defaults (`LISTEN_ADDR`, `DORIS_FLIGHT_ADDR`, `DORIS_USER`, `DORIS_PASSWORD`, `DORIS_TRANSPORT`, `DORIS_MYSQL_ADDR`, `LAKE_ENABLED`, `QUERY_TIMEOUT_MS`, `RUST_LOG`). An unknown `DORIS_TRANSPORT` fails at start.
  - `logging.rs`: a JSON `tracing` formatter that adds `service=explorer` to **every** line (including tonic and hyper events), plus an Axum middleware logging `route` (matched path), `method`, `status` and `latency_ms` per request.
  - `health.rs`: `explorer healthcheck` does a plain HTTP/1.1 `GET /healthz` to `127.0.0.1:<LISTEN_ADDR port>` with `std::net::TcpStream` and a 2 s timeout, exiting 0 on a 200 (spec §13.8; the slim image has no curl, same pattern as `datacore healthcheck`).

**Verification:** `cargo build`, `cargo fmt --check`, `cargo clippy --all-targets -- -D warnings` in the builder container (step 6).

### 3. Input validation and SQL building (spec §13.4, §13.5, §16.2)
- `validate.rs`, hand-written checks (no regex crate):
  - `id`: `^(LT|DT|SV)-\d{4}$`;
  - `site`, `os`: `^[A-Za-z0-9 .\-]{1,64}$`;
  - `limit` 1–500 (default 50), `offset` ≥ 0 (default 0, bounded to `i64`/`u32` parse), `minutes` 1–1440 (default 60); non-integers, signs, empty values and overflow are rejected.
- `sql.rs`: builds exactly the §13.4 statements. Request values reach SQL only as validated, single-quoted literals (the patterns exclude quotes). Numbers are formatted from parsed integers, never copied from the request.
- Query strings are parsed from the raw `HashMap<String,String>` and the path id is taken as a raw `String`, so every bad input becomes our own 400 `invalid_argument` JSON instead of Axum's default plain-text rejection (a custom rejection handler covers a non-UTF-8 path too).
- **Unit tests (§16.2):** valid ids; wrong prefix, wrong digit count, lowercase, trailing characters; injection strings (`LT-0042' OR '1'='1`, `'; DROP TABLE x; --`, `Madrid' --`, Unicode quotes, `%27`); `limit=0`, `501`, `-1`, `abc`, `1e3`; `minutes=0`, `1441`; the exact SQL text for each endpoint, including both list filters, one, or none.

### 4. Doris access: the Flight SQL ladder, step 1 (spec §3 R4, §13.6)
`flight.rs` (repository) behind a small trait `DorisQuery { async fn query(&self, sql) -> Result<Vec<RecordBatch>, DorisError>; fn transport(&self) -> &'static str }`, so a later ladder step only swaps the implementation.
- **Step 1, `arrow-flight` `FlightSqlServiceClient`:**
  1. Open a tonic channel to `DORIS_FLIGHT_ADDR` with `max_decoding_message_size(64 MiB)`.
  2. `handshake(user, password)` (basic auth); the client keeps the bearer token Doris returns.
  3. `execute(sql)` → `FlightInfo`.
  4. For each endpoint, in order: take its location URI, translate `grpc://` / `grpc+tcp://` to `http://` (§13.6), or use the FE address when there is no location; open (and cache per location) a channel to the BE; `do_get(ticket)` carrying `authorization: Bearer <token>`; collect the record batches.
- **Session handling:** one authenticated session, created at start (retried lazily if Doris is not up yet) and reused across requests. On `UNAUTHENTICATED`, re-handshake once and retry the query. On transport errors (connect failures, `UNAVAILABLE`, `CANCELLED`, broken streams), drop the session so the next request reconnects, and answer 503 `upstream_unavailable`.
- **Timeout:** the whole query (execute plus every DoGet) runs under `QUERY_TIMEOUT_MS`.
- **Error classes (`error.rs`):** `InvalidArgument` → 400 `invalid_argument`; `NotFound` → 404 `not_found`; `Upstream` (transport, timeout, any Doris SQL error) → 503 `upstream_unavailable`; `LakeUnavailable` → 503 `lake_unavailable` for history when `LAKE_ENABLED=false` (without querying) or when Doris reports the lake catalog or table as missing/unreachable. Unknown routes → 404 `not_found` JSON. Record this mapping in **D-041**.
- **Unit tests (§16.2):** error → HTTP status and code mapping (table-driven, every class); location translation (`grpc://172.30.0.3:8050`, `grpc+tcp://…`, empty); tonic status classification (`Unauthenticated` → re-handshake, `Unavailable` → drop session, SQL error → upstream).

**Ladder discipline (G4):** step 2 (ADBC through `adbc_driver_manager` with the Apache ADBC Flight SQL driver in the image) is taken only after **three distinct written hypotheses** for step 1 have failed (for example: wrong location scheme or host; token not carried to the BE; Doris rejecting the client's handshake or metadata). Step 3 (MySQL protocol, `ADL_ALLOW_MYSQL_FALLBACK=1` for D06, known deviation in the decision log and README) only if step 2 also fails after G4. Whatever happens, **D-040** records the step that worked, with evidence (an explorer log line showing `FlightInfo` with the BE location `172.30.0.3:8050` and the DoGet against it). `/readyz` reports the transport in use.

### 5. Arrow → JSON mapping and the HTTP API (spec §13.2, §13.3, §5.5, §16.2)
- `convert.rs`: column extraction by name, tolerant of the Arrow types Doris may send:
  - strings: `Utf8`, `LargeUtf8`, `Utf8View`;
  - integers: every signed and unsigned width → `i64`;
  - datetimes: `Timestamp(unit, None)` (Doris `DATETIME`, UTC wall clock), `Timestamp(unit, Some(tz))` (the lake's `timestamptz(6)`, sprint C audit F-04), `Date64`, and string forms `yyyy-MM-dd HH:mm:ss[.ffffff][+00:00]` as a fallback. All render as RFC 3339 with milliseconds and `Z` (`2026-11-25T10:14:03.120Z`), truncating microseconds.
  - nulls → JSON `null`.
- `api.rs` (transport) + `service.rs` (queries and shaping):
  - `GET /healthz` → `{"status":"ok"}`.
  - `GET /readyz` → `SELECT 1`; 200 `{"status":"ready","doris_transport":"flight-sql"}` or 503 `{"status":"not_ready","doris_transport":"…","error":"…"}`.
  - `GET /api/assets/{id}` → §13.3 Asset; `owner` is `null` when `owner_user_id` is null; `name`, `group_id`, `group` are `null` when the joined rows are missing; 404 `not_found` with `"asset <id> not found"` when no row.
  - `GET /api/assets` → `{"items":[…],"limit":n,"offset":m}`, ordered by id.
  - `GET /api/assets/{id}/history` → `{"id","versions":[…]}` in ascending version; 404 when there are no versions.
  - `GET /api/stats/os-site` → `{"items":[{"os","site","assets"}]}` in the query's order.
  - `GET /api/stats/change-rate` → `{"items":[{"window_start","window_end","entity_type","changes"}]}`.
- R6's explorer-side `REFRESH TABLE` fallback is **not** implemented: `meta.cache.iceberg.table.ttl-second=0` was accepted (D-037) and lake lag is 13–23 s, below 60 s. If D03 or the live check shows lag > 60 s, apply it then (at most once every 10 s) and record it.
- **Unit tests (§16.2):** batches built in the test → JSON for every endpoint: Appendix A values, null owner, owner without a user row, `Timestamp(Millisecond/Microsecond, None)`, `Timestamp(Microsecond, "UTC")` and `"+00:00"`, string datetimes, `Int32` counts, empty batches, multiple batches concatenated. Router tests with a fake `DorisQuery` (`tower::ServiceExt::oneshot`): 400/404/503 bodies, the D02 URLs exactly (`/api/assets/LT-0042'%20OR%20'1'='1`, `?limit=0`, `?minutes=0`), `LAKE_ENABLED=false` → 503 `lake_unavailable` without a query, unknown route → 404 JSON.

### 6. explorer image and Makefile builders (spec §15.9, §15.11, §2.3)
- `explorer/Dockerfile`, multi-stage with BuildKit cache mounts:
  - stage `toolchain`: `rust:1.100.0-bookworm` + `rustup component add rustfmt clippy` (reused by `make test lint`);
  - stage `build`: `cargo build --release --locked` with cache mounts for the registry and `target`, copying the binary out of the cache mount in the same `RUN`;
  - runtime: `debian:bookworm-20261123-slim`, non-root user (uid 10001), `ENTRYPOINT ["/usr/local/bin/explorer"]`, `CMD ["serve"]`, `EXPOSE 8080`.
- **Makefile** (replace the sprint D stubs in `test` and `lint`):
  - `CARGO`: build the toolchain stage once as `adl/explorer-toolchain:0.1.0` (`docker build --target toolchain`, cached), then `docker run --rm` with `explorer/` bind-mounted, cache volumes `adl-cargo-registry` and `adl-cargo-target` (outside the compose project, so `make clean` keeps them), `--memory 3g` and `CARGO_BUILD_JOBS=4`.
  - `make test`: `cargo test --locked`; `make lint`: `cargo fmt --check` and `cargo clippy --all-targets --locked -- -D warnings`.
  - `NODE`: `docker run --rm` on `node:24.22.3-bookworm-slim` with `web/` bind-mounted, `node_modules` in volume `adl-web-node-modules` (keeps host `web/` clean), npm cache in `adl-npm-cache`, `--memory 1536m`.
  - `make test`: `npm ci` then `npm test`; `make lint`: `npm ci`, then `npm run lint` (ESLint and Prettier check) and `npm run typecheck` (`tsc --noEmit`).
  - The memory caps matter because the gate runs `make test lint` with the full stack up: the caches are warmed with the stack down (step 9), so the gate run is incremental.
- Record the builder design in **D-043**.

**Verification:** `make test lint` green with the stack down; `docker build` of the explorer image; image size and `explorer healthcheck` exit code in a throwaway container.

### 7. web app (spec §14, §16.2)
Create `web/` (Vite, React 19, TypeScript 5, no UI library):
- `package.json` (scripts `dev`, `build` = `vite build`, `lint` = `eslint . && prettier --check .`, `typecheck` = `tsc --noEmit`, `test` = `vitest run`), `package-lock.json`, `index.html` with `<div id="root"></div>`, `vite.config.ts` (React plugin; `server.proxy['/api'] → http://localhost:8080` for `npm run dev`, §14.3; Vitest config with jsdom and a setup file), `tsconfig.json`, `eslint.config.js` (flat config: `@eslint/js`, `typescript-eslint`, react-hooks; ignores `dist`), `.prettierrc.json`, `.prettierignore`, `.dockerignore`.
- `src/` (flat, several files):
  - `main.tsx`, `App.tsx` (two pages, Asset by default and Stats; client routes `/` and `/stats` through `history.pushState`, no router library);
  - `api.ts`: typed fetch helpers that call only relative `/api/*` URLs (§14.2), returning a discriminated result `ok | not_found | error` so pages never break;
  - `AssetPage.tsx`: search box prefilled with `LT-0042`; `AssetCard` (hostname, kind, OS, site, owner, group, version, `changed_at`); refreshes every 5 s and shows "Last updated HH:MM:SS"; a "History" button showing `HistoryTable` (version table);
  - `StatsPage.tsx`: OS × site table from `/api/stats/os-site` (OS rows, site columns), and the last 60 minutes of `/api/stats/change-rate` with simple CSS bars per window and entity type;
  - loading, empty and error states are always visible; a 404 shows "not found" (§14.2);
  - `styles.css`.
- **Tests (§14.4, §16.2):** `AssetCard.test.tsx` renders the card from a §13.3 fixture (all fields, owner and group shown; `owner: null` shows "Unassigned"); `AssetPage.test.tsx` mocks `fetch` to return 503 and checks the error state, and 404 and checks "not found".
- `web/Dockerfile`: build stage `node:24.22.3-bookworm-slim` (`npm ci` with an npm cache mount, `npm run build`); runtime `nginx:1.30.8-alpine` with:
  - `web/nginx.conf` replacing `/etc/nginx/nginx.conf`: `worker_processes 2` (fits the 64 MiB limit), JSON access log with `"service":"web"`, `location /api/ { proxy_pass $explorer; }` with `resolver 127.0.0.11 valid=10s` and `set $explorer http://explorer:8080` so a restarted explorer is re-resolved, and `location / { try_files $uri /index.html; }` (§14.3).
- Record the web design in **D-042**.

**Verification:** `npm run lint`, `typecheck`, `test`, `build` in the node builder; `docker build` of the web image.

### 8. Compose services, profile `app` (spec §15.2–§15.8)
Add to `docker-compose.yml`:
- **`explorer`:** build `./explorer`, image `adl/explorer:0.1.0`, profile `app`, `restart: unless-stopped`; `depends_on` `doris-init` completed and `doris-init-lake` completed with `required: false`; env `DORIS_FLIGHT_ADDR=http://doris-fe:8070`, `DORIS_USER=explorer`, `DORIS_PASSWORD=${DORIS_EXPLORER_PASSWORD}`, `DORIS_TRANSPORT=flight-sql`, `LAKE_ENABLED=${ICEBERG_ENABLED:-true}` (the lake switch of §15.2 drives both the job and the explorer; recorded in D-041), `RUST_LOG=info`; port `8080:8080`; health check `["CMD","/usr/local/bin/explorer","healthcheck"]` 15 s / 5 s / 10; memory 192M; network `adl`.
- **`web`:** build `./web`, image `adl/web:0.1.0`, profile `app`, `restart: unless-stopped`; `depends_on` explorer healthy; port `3000:80`; health check `wget -qO- http://localhost/ > /dev/null` 10 s / 5 s / 10; memory 64M; network `adl`.
- Budget check: the long-running limits now total 11,456 MiB, exactly §15.4.

**Verification:** `docker compose --profile core --profile lake --profile app config` resolves; `docker compose --profile core config --services` still resolves without the app services.

### 9. Bring up, smoke test, and fix (spec §4.3 steps 10–14, R4)
With the stack down: `make test lint` (warms the Cargo and npm caches), then `make build && make clean && make up`.
- Smoke checks by hand: `curl localhost:8080/readyz` (transport `flight-sql`), `/api/assets/LT-0042`, `/history`, `/api/assets?site=Madrid&limit=10`, both stats endpoints, `localhost:3000/` and `localhost:3000/api/assets/LT-0042`; explorer logs show `FlightInfo` → BE location → DoGet; look at the web page's HTML.
- Check the Arrow types Doris actually sends for `DATETIME(3)`, `DATETIME`, `timestamptz(6)`, `VARCHAR` and `COUNT(*)`, and add a unit test for any type not yet covered.
- If Flight SQL fails, follow the ladder discipline of step 4 and write each hypothesis down.
- Record **D-040** (R4 step that worked).

### 10. Run the gate (plan §3.5, §4)
Exactly as written: `make build && make clean && make up`, `make test lint`, `for g in A B C D; do make acceptance-gate GATE=$g || exit 1; done`, then the sprint 0 checks, then `make reconcile`. Fix anything red at its cause; never touch `acceptance/`.

### 11. Memory measurement (spec §15.4, §18 item 7, R9; sprint C audit F-01)
After gate D: `docker stats --no-stream` for every container, plus `memory.peak`, `memory.events` and `memory.stat anon` in `doris-fe`, and the explorer and web usage against 192 MiB and 64 MiB. If the FE is OOM-killed or `memory.events max` climbs, apply R9 in order (first lower the FE heap or cap its direct memory within its 1792 MiB budget), record `docker stats` before and after in a decision entry, and re-run the gate.

### 12. Build report, audit, fixes (plan §3.7)
- Write `specs/sprints/sprint-D/build-report.md`: what was built, how to verify, gate results (timings, D03 lake latency), memory table, deviations from this plan.
- **Audit subagent** (fresh context, did not build the sprint): it reads the spec, the sprints plan, the sprint D plan and build report, and `git diff sprint-C-done..HEAD`. It re-runs the gate and writes `specs/sprints/sprint-D/audit-report.md`: findings by severity, a requirements table for §13, §14, §15.2–§15.9 (app parts), §16.2 explorer and web, gate D and R4, and whether earlier gates are put at risk.
- Fix every critical and major finding (cheap minors too), add a "Fix status" section to the audit report, and re-run the full gate.

### 13. Progress files and commit (plan §3.7)
- `specs/build-progress.md`: sprint D done; "What exists after sprint D"; the R4 step; notes for sprint E (FE memory, explorer behaviour under chaos and restarts, README items: the web URL and the walkthrough step for the web).
- `specs/decision-log.md`: D-039 to D-043 (plus any R6/R9 entries). `specs/open-questions.md`: only if something new comes up (Q-003+).
- Commits (Conventional Commits), for example: `feat(explorer): …`, `feat(web): …`, `build: add the app profile and the Cargo and npm builders`, `docs(sprint-D): …`, `fix: address the sprint D audit findings`, and last `chore(sprint-D): close sprint`. No tag. Working tree clean. Leave the stack running in full mode.

## Files to create or change

- **New:** `explorer/{Cargo.toml,Cargo.lock,rust-toolchain.toml,Dockerfile,.dockerignore}`, `explorer/src/{main,config,logging,health,validate,sql,error,flight,convert,service,api}.rs`; `web/{package.json,package-lock.json,index.html,vite.config.ts,tsconfig.json,eslint.config.js,.prettierrc.json,.prettierignore,.dockerignore,Dockerfile,nginx.conf}`, `web/src/{main.tsx,App.tsx,api.ts,AssetPage.tsx,AssetCard.tsx,HistoryTable.tsx,StatsPage.tsx,styles.css,test-setup.ts,AssetCard.test.tsx,AssetPage.test.tsx}`; `specs/sprints/sprint-D/{sprint-plan,build-report,audit-report}.md`.
- **Changed:** `docker-compose.yml` (explorer, web), `Makefile` (Cargo and npm in `test` and `lint`), `specs/build-progress.md`, `specs/decision-log.md`.
- **Not touched:** locked files; `acceptance/`; Go code unless the gate reveals an earlier-sprint bug (then fixed here and recorded in the build report, plan §3.3).

## Risks and the rule for each

| Risk | Rule | Mitigation |
|---|---|---|
| Flight SQL from Rust fails (handshake, BE location, token on DoGet, types) | R4 ladder + G4 | Step 1 first; three written hypotheses before step 2 (ADBC), then step 3 (MySQL + `ADL_ALLOW_MYSQL_FALLBACK=1`, known deviation). Transport behind a trait so the swap is local. D-040 records the step. |
| Doris sends unexpected Arrow types (`timestamptz(6)`, string datetimes, `Utf8View`) | §5.5, §13.6, sprint C F-04 | Tolerant converters plus unit tests per type; check the live types in step 9. |
| Doris FE OOM under Flight SQL load | R9, §15.4 | Measure in step 11; lower heap / cap direct memory within the FE's 1792 MiB; record before and after. |
| Heavy Cargo and npm builds starve the running stack (`make test lint` runs with the stack up in the gate) | §15.11, plan §3.6, R9 | Build images and warm the cache volumes with the stack down; `--memory` caps and `CARGO_BUILD_JOBS=4` on builders; the gate run is incremental. |
| History lags more than 60 s | R6 | Explorer `REFRESH TABLE` at most once per 10 s, only if observed; record it. |
| npm peer conflicts with the newest majors (ESLint 10, Vite 8, Vitest 5) | §2.1, §2.7, G3 | Step down to the newest compatible exact version and record it in D-039. |
| nginx keeps a stale explorer IP after a restart (sprint E kills) | G1 | Docker DNS resolver with a variable `proxy_pass`. |
| nginx exceeds 64 MiB with `worker_processes auto` on 12 CPUs | §15.4 | `worker_processes 2`; measured in step 11. |
| A gate D test looks wrong | G2 | Never edit it; write Q-003 in `open-questions.md` with evidence and continue. |
| Stale-state switch between core-only and full mode | §15.2 | The gate always starts from `make clean && make up`. |
