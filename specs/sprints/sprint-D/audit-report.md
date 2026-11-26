# Sprint D audit report

Auditor: an independent subagent that did not build the sprint. Date: 2026-11-26.
Scope: `git diff sprint-C-done..HEAD` (five commits, `580d866` to `73d27cc`, 47 files): the new `explorer/` crate (11 source files, `Cargo.toml`, `Cargo.lock`, `rust-toolchain.toml`, `Dockerfile`), the new `web/` app (12 source files, configuration, `Dockerfile`, `nginx.conf`, `package-lock.json`), `docker-compose.yml` (profile `app`), `Makefile` (Cargo and npm builders), `infra/doris/fe.Dockerfile` (D-044), and the sprint's plan, build report and decision-log entries D-039 to D-044.
Read in full: spec §1–§5, §11–§18 and Appendix A, with extra attention on §3 (R4, R6, R9, G1–G5), §5.5, §13, §14, §15.2–§15.11, §16.2–§16.4 and §18; sprints plan §3 and the sprint D and E entries of §4; `specs/sprints/sprint-D/sprint-plan.md` and `build-report.md`; `specs/decision-log.md` (D-038 to D-044); `specs/build-progress.md`; `specs/open-questions.md`; and the sprint C audit report, for the format and its carry-over findings F-01 (FE memory) and F-04 (lake `timestamptz`). I read every production and test file of the diff line by line, plus `acceptance/gate_d_test.go` and the relevant parts of `acceptance/harness_test.go`, `labkit/compose.go` and `.github/workflows/ci.yml`.
I then re-ran the gate exactly as written, probed the live stack (every §13.2 endpoint and error case, the web through nginx, logs, Doris sessions and BE tasks, cgroup memory), and ran three state-changing probes: an explorer restart, an explorer re-created with a new IP address (to test nginx re-resolution), and a `doris-be` restart (forced by finding F-01, see there). After the last one I re-ran gate D and the reconcile. I left the stack running in full mode, healthy, with the generator at its default rate.

## 1. Summary

Sprint D works. The gate is green from a clean state, and so are the sprint 0 checks and `make reconcile`:
- gates A, B, C and D pass 10/10, 9/9, 5/5 and 8/8;
- `make test lint` is green with the full stack up (Cargo 24/24, Vitest 7/7, clippy `-D warnings`, rustfmt, ESLint, Prettier, `tsc --noEmit`, plus the earlier Go and Maven checks).

The serving layer follows the spec:
- **R4:** the explorer speaks Arrow Flight SQL (step 1): handshake with the FE (basic → bearer), `GetFlightInfo` on the FE, `DoGet` against the endpoint location `grpc+tcp://172.30.0.3:8050` translated to `http://…`, with the token. `/readyz` reports `"doris_transport":"flight-sql"`. D-040 records it with log evidence, which I reproduced.
- **API:** every endpoint, query parameter, error code and JSON shape of §13.2–§13.3 matches, and so does the SQL of §13.4. Datetimes are RFC 3339 with milliseconds and `Z`, including the lake's `timestamptz(6)` (sprint C F-04 is handled and unit-tested). `owner` is `null` for unassigned assets. Every injection string I tried is rejected with 400 before any SQL is built; a non-UTF-8 path gives 400 JSON; unknown routes give 404 JSON.
- **Web:** the asset page (search prefilled with LT-0042, a card refreshing every 5 s with "Last updated", a History table) and the stats page (OS × site matrix, change-rate CSS bars) call only relative `/api/*` URLs. nginx proxies `/api/` with a re-resolving Docker DNS resolver, and I confirmed that it finds an explorer re-created on a new IP address at once.
- **Compose, Makefile, pins, CI:** profile `app` matches §15.2, §15.4, §15.6 and §15.8; the long-running limits total exactly 11,456 MiB. Every version is exact and both lockfiles are committed. The CI `rust`, `web` and `images` jobs will pick up the committed files unchanged.

There are **no critical and no major findings**. The most important finding is F-01: a request the explorer accepts as valid (`offset` near `i64::MAX`) triggers a Doris bug that spins a BE core forever, and only a BE restart stops it. It does not break any gate, but it is cheap to fix and should be fixed before sprint E. The other findings are three minor ones and six informational ones.

### 1.1 Gate results (re-run exactly as written, sequentially, from the repository root)

```
$ make build && make clean && make up            -> exit 0 (98 s in total)
    images built from cache; "down --volumes --remove-orphans"; "wait: ready":
    explorer and web healthy, plus every core and lake service healthy or completed,
    "doris-be (SHOW BACKENDS) 172.30.0.3 alive=true", "flink job adl-processor RUNNING"
$ make test lint                                  -> exit 0 (85 s, full stack up)
    go test ./...                                 -> every package ok
    mvn test                                      -> Tests run: 37, Failures: 0, Errors: 0
    cargo test --locked                           -> test result: ok. 24 passed; 0 failed
    npm ci && npm test                            -> Test Files 2 passed (2), Tests 7 passed (7)
    buf lint, buf breaking against main           -> ok
    golangci-lint v2.15.0                         -> 0 issues.
    mvn spotless:check                            -> BUILD SUCCESS
    cargo fmt --check; cargo clippy --all-targets --locked -- -D warnings -> clean
    npm run lint (eslint . && prettier --check .); npm run typecheck      -> clean
$ make acceptance-gate GATE=A                     -> ok, 10/10 PASS (22 s)
$ make acceptance-gate GATE=B                     -> ok, 9/9 PASS (312 s)
    B09 freshness: p95 10.18 s, max 10.23 s
$ make acceptance-gate GATE=C                     -> ok, 5/5 PASS (31 s; C02 14.1 s)
$ make acceptance-gate GATE=D                     -> ok, 8/8 PASS (51 s)
    --- PASS: TestGateD_01_AssetEndpoint (8.49s)
    --- PASS: TestGateD_02_ErrorsAndValidation (0.06s)
    --- PASS: TestGateD_03_HistoryEndpoint (20.80s)
    --- PASS: TestGateD_04_ListEndpoint (10.72s)
    --- PASS: TestGateD_05_StatsEndpoints (1.30s)
    --- PASS: TestGateD_06_FlightSqlTransport (0.01s)
    --- PASS: TestGateD_07_WebServes (0.05s)
    --- PASS: TestGateD_08_HealthEndpoints (0.01s)
```

The whole gate took 10 minutes. Sprint 0 checks and the reconcile:

```
$ go run …buf@v1.75.0 lint && go run …buf@v1.75.0 build && go vet ./... && go build ./... \
  && go vet -tags acceptance ./acceptance/... && test "$(grep … | wc -l)" -eq 38 && go test ./...
-> exit 0 (14 s, 16 packages ok)
$ make reconcile                                  -> exit 0 (7 s)
    "reconcile: PASS (checked assets=3015 groups=11 lake_assets=3015 users=303)"
```

After the `doris-be` restart of F-01, I re-ran gate D (8/8 PASS, 36 s; D03 21.0 s) and the reconcile (PASS, same counts). There were 0 duplicate `change_rate_1m` pairs and 0 duplicate lake (`id`, `version`) pairs.

### 1.2 Extra live checks (not part of the gate)

| Check | Result |
|---|---|
| Locked files | `git diff --quiet plan-v1 -- specs/system-spec.md specs/sprints-plan.md CLAUDE.md run-sprints.sh` → 0. `git diff --quiet sprint-0-done -- acceptance/` → 0. The diff touches no Go, Java, `labkit/`, `adlctl/` or CI file |
| Success endpoints | `/healthz` → `{"status":"ok"}`. `/readyz` → `{"status":"ready","doris_transport":"flight-sql"}`. `/api/assets/LT-0042` → the §13.3 shape, keys in §13.3 order, nested `owner` with `user_id`, `name`, `group_id`, `group`. `/api/assets?site=Madrid&limit=3` → `{"items":[…],"limit":3,"offset":0}`, ids ascending. `/history` → `{"id","versions":[…]}` ascending. `/api/stats/os-site` → `{"items":[{"os","site","assets"}]}` ordered by os, site. `/api/stats/change-rate?minutes=5` → closed one-minute windows ordered by `window_start`, `entity_type`. All `application/json` |
| Datetimes | `changed_at` `2026-11-25T23:27:23.468Z` (Doris `DATETIME(3)`), the same instant from the lake (`timestamptz(6)`), `window_start` `2026-11-25T23:27:00.000Z` (`DATETIME`). `NOW()` in the change-rate query is UTC: the windows match the UTC wall clock |
| Owner null | Servers and unassigned desktops return `"owner":null` (for example DT-0001) |
| Error cases (§13.2, §13.5) | `LT-9999` → 404 `not_found` "asset LT-9999 not found". `LT-0042'%20OR%20'1'='1`, `LT-0042%27`, `lt-0042`, `LT-0042%00`, `LT-0042x/history` → 400 `invalid_argument`. `%FF%FE` (not UTF-8) → 400 JSON "invalid path: Invalid UTF-8 in `id`". `limit=0`, `501`, `-1`, `1e3`, empty → 400; `limit=500` → 200. `offset=-1`, `99999999999999999999` → 400. `site=Mad'rid`, `site=Madrid' OR '1'='1`, `site=` (empty), a 65-character `os`, `site=%FF` → 400. `minutes=0`, `1441`, `abc` → 400; `1440` → 200. `LT-9999/history` → 404 "has no versions in the lake". Unknown routes (`/`, `/api/nope`, `/api/assets/`, `/readyz/`) → 404 JSON. Every error body is `{"error","message"}` |
| Large offsets | Up to `offset=9223372036854775806&limit=1` → 200 with no items. When `limit + offset` exceeds `i64::MAX` → 503 after 10 s, and a BE thread spins forever (F-01) |
| `/readyz` transport | `doris_transport` is `flight-sql`. Any other `DORIS_TRANSPORT` value stops the explorer at start, pointing to D-040 (steps 2 and 3 are not built, as R4 allows when step 1 works) |
| R4 evidence (D-040) | The explorer log shows `"Flight SQL handshake done","fe":"http://doris-fe:8070","user":"explorer"`, then `"opened Flight SQL channel to the endpoint location","location":"http://172.30.0.3:8050"`. Doris's audit log records the explorer's statements as user `explorer`. The FE answers on 8070 and the BE serves `DoGet` on 8050, both internal only |
| Explorer logs | One JSON object per line with `time`, `level`, `service:"explorer"`, `target`, `msg`. One request line per request with `route` (the matched path, for example `/api/assets/{id}`, or `unmatched`), `method`, `status` and `latency_ms`. 5xx answers add a `warn` line with the error code and message |
| Web through nginx | `/`, `/stats`, `/stats/x`, `/favicon.ico` → 200 HTML with `<div id="root">` (SPA fallback). `/api/assets/LT-0042` → 200 JSON through the proxy, and the injection URL → 400 JSON. `/api/nope` → 404 JSON from the explorer. A missing `/assets/x.js` → nginx 404. The access log is JSON with `"service":"web"` |
| Explorer restart (`docker compose restart explorer`) | The web proxy and `/readyz` answer 200 within 1 s of the restart |
| Explorer re-created with a new IP | Explorer stopped → the web returns nginx's 502 page. I took the old address 172.30.0.135 with a throwaway container and started the explorer again: it came up on 172.30.0.136, and `localhost:3000/api/assets/LT-0042` answered 200 at once. nginx re-resolves as D-042 says |
| Explorer when the BE restarts | During and right after `docker compose restart doris-be`, the explorer kept working: the next `/readyz` and `/api/assets/LT-0042` answered 200 (the cached BE channel reconnects by itself). I did not restart the FE (see F-02) |
| Explorer sessions in Doris | `SHOW PROCESSLIST` lists 5 `explorer` Flight SQL sessions after two timeouts and two restarts; only one is in use (F-02) |
| Doris settings | `arrow_flight_token_alive_time_second=86400`, `arrow_flight_max_connections=4096`, `max_user_connections=100` for `explorer`, `wait_timeout=28800`, `query_timeout=900` |
| History latency | The first history query after start took 5.3 s (cold catalog), later ones 0.17–0.21 s. D03 sees both versions about 21 s after the second upsert (§1.5 target: 60 s). R6's explorer `REFRESH TABLE` fallback is not needed |
| Memory (`docker stats`, default load, end of the audit) | doris-fe 1.492 GiB / 1.75 GiB (85%), doris-be 1.257 / 3.25 GiB, TM 1.284 / 2 GiB, JM 738 MiB / 1.25 GiB, kafka 710 MiB / 1 GiB, iceberg-rest 197 / 512 MiB, rustfs 128 / 384 MiB, dragonfly 48 / 512 MiB, datacore 16 / 160 MiB, adapter 12 / 160 MiB, **explorer 4 MiB / 192 MiB, web 7 MiB / 64 MiB**. No container has `OOMKilled=true` or a restart count |
| FE cgroup (D-044) | `-Xms1024m -Xmx1024m` in the running process. `memory.max 1879048192`, `memory.peak 1647341568` (87.7%), `anon 1559552000`, `memory.events max 0 oom 0 oom_kill 0`. Heap RSS 852 MB of 1024 MB, so native memory is about 690 MB with 421 threads (F-04) |
| Pins | `Cargo.toml` uses `=x.y.z` for every crate; `Cargo.lock` has `arrow-flight 60.2.0`, `tonic 0.14.9`, `axum 0.8.9`, `tokio 1.54.1`, `prost 0.14.6`. `package.json` has no `^` or `~`; `package-lock.json` resolves react 19.3.1, vite 8.3.8, vitest 5.1.2, typescript 5.9.3, eslint 10.15.0, prettier 3.10.1. Images: `rust:1.100.0-bookworm`, `debian:bookworm-20261123-slim`, `node:24.22.3-bookworm-slim`, `nginx:1.30.8-alpine`. `rust-toolchain.toml` pins 1.100.0. No `latest` anywhere. All recorded in D-039 |
| Images | `adl/explorer:0.1.0` 83 MB (runs as uid 10001), `adl/web:0.1.0` 69 MB |
| CI | The detect job's `docker compose … config` now lists `explorer` and `web` with `build.context` `explorer` and `web`, so the `images` matrix builds both. The `rust` job runs in `explorer/`, where `rust-toolchain.toml` (with rustfmt and clippy) is; the `web` job runs `npm ci`, `lint`, `typecheck`, `test` and `build`, all of which exist in `package.json`. The same commands passed locally in the builder containers |
| Layout and language | `explorer/src` (11 files) and `web/src` (12 files) are flat; no single-file directory. Everything is in English; no TODO or FIXME. Configuration comes only from environment variables (§13.7 names and defaults, unit-tested). The builders left only the empty, git-ignored mount points `explorer/target` and `web/node_modules` on the host; `git status` is clean |

## 2. Findings

### Critical

None.

### Major

None.

### F-01 · minor · An accepted `offset` near `i64::MAX` makes a Doris BE thread spin forever

- **What:** `validate.rs` accepts any `offset` from 0 to `i64::MAX`, as §13.2 allows (it sets no upper bound). When `limit + offset` exceeds `i64::MAX`, Doris 4.1.5 never finishes the query:
  - the explorer answers 503 `upstream_unavailable` after `QUERY_TIMEOUT_MS`, drops its session and handshakes again (which leaks one Doris session, F-02);
  - on the BE, one pipeline thread stays at 100% CPU for good. Doris's `query_timeout` (900 s) does not stop it, and neither do `KILL QUERY '<query id>'` or `KILL QUERY <connection id>`. Only a BE restart does.
  - So one anonymous request permanently takes one of the four CPUs that §15.1 asks for. A few of them would starve Flink's Stream Load and the freshness targets of gate B and gate E.
- **Evidence:**
  - `GET /api/assets?limit=1&offset=9223372036854775807` and `?limit=2&offset=9223372036854775806` → 503 "the Doris query timed out" after 10.00 s. `?limit=1&offset=9223372036854775806` (sum exactly `i64::MAX`) → 200 in 0.04 s, and so are offsets from 10⁶ to 9.22·10¹⁸.
  - `top -H` in `doris-be`: threads `p_normal` 1230 and 1233 at 99.9% CPU, with 16 and 8 minutes of CPU time, still running 912 s after the first request. `information_schema.backend_active_tasks` keeps the two query ids (`fee579a405d436c-…`, `5d54ecbc34eb4a05-…`) with growing `TASK_TIME_MS`. `docker stats` shows the BE at about 280% CPU with the generator at 10/s.
  - The same SQL over MySQL as `root` (`SET query_timeout=10; SELECT id FROM inventory.assets ORDER BY id LIMIT 2 OFFSET 9223372036854775806`) → `ERROR 1105 … query timeout`, and a third thread starts spinning. So it is a Doris bug, not an explorer bug, but the explorer is what lets it through from the browser side.
  - I restarted `doris-be` to clear the threads, then re-verified gate D and the reconcile (§1.1).
- **Spec:** §13.1 ("the only way into the data"), §13.5 (validation before SQL), §13.6 (on timeouts, 503), §1.5 and gates B and E (freshness and recovery need a healthy BE).
- **Fix:** bound `offset` so that `limit + offset` can never overflow, for example `offset` ≤ 1,000,000,000 (or ≤ `i64::MAX − 500`), still returning 400 `invalid_argument` above it. Add the overflow case to the validation and router unit tests, and record the bound and its reason in the decision log (G3). The spec sets no upper bound, so a bound is a G1 choice that keeps the API's contract for every realistic offset.

### F-02 · minor · Every session reset or explorer restart leaves an idle Doris session behind

- **What:** `FlightSql::reset` (`explorer/src/flight.rs:87-90`) drops the session locally and the next request handshakes again, but the old Doris session is never closed (Flight SQL has no logout; Doris keeps it until `wait_timeout`, 8 h, or the token expires, 24 h). The same happens on every explorer restart.
  - Resets happen on every timeout (F-01 is one way), on every `Unavailable`, `Cancelled`, `DeadlineExceeded`, `Unknown`, `ResourceExhausted` or `Aborted` status (`error.rs:86-91`), and on `Unauthenticated`.
  - `explorer` has `max_user_connections = 100`, and `SHOW PROCESSLIST` lists the Flight SQL sessions next to MySQL ones. If they count toward the limit (likely, since they are listed in the same pool; I did not push it to 100), about 100 resets within 8 hours would make every new handshake fail, and the explorer would answer 503 until the old sessions expire.
  - The `Unknown` class is the least clear: if Doris or tonic reports an ordinary statement error as `Unknown`, every such error also resets and leaks a session.
  - I could not test the explorer against an FE restart without disrupting the FE. By the code, the FE channel reconnects and a rejected token re-handshakes, which is the §13.6 behaviour; it depends on Doris answering `UNAUTHENTICATED` for an unknown token.
- **Evidence:** `SHOW PROCESSLIST` → five `explorer` rows (ids 13, 16, 55, 62, 63), four of them idle for 52–622 s, after two timeouts and two restarts. Each reset is followed by `"Flight SQL handshake done"` in the explorer log.
- **Spec:** §13.6 ("one authenticated client is created at start and reused"; "reconnect on the next request"); robustness for sprint E and for a lab left running.
- **Fix:** on `Unavailable`, `Cancelled` and similar transport errors, keep the token and let tonic's channel reconnect; re-handshake only on `Unauthenticated` or when the handshake itself failed. On a timeout, keep the session too (the FE session is still valid; only the stream was abandoned). Optionally make the lingering sessions visible: log the session count after a handshake, or lower `wait_timeout` for user `explorer` with `SET PROPERTY FOR 'explorer' …` in `init-core.sql` if Doris supports it for Flight sessions.

### F-03 · minor · The explorer accepts unknown query parameters and the web's `/api` redirect points at port 80

- **What:** two small API-edge deviations.
  - `GET /api/assets?foo=bar&limit=1` → 200; unknown parameters are ignored. §13.5 says "Anything else returns 400 `invalid_argument`". The sentence is ambiguous (it may only mean invalid values of the listed parameters), and no test depends on it.
  - `GET http://localhost:3000/api` (no slash) → nginx `301` with `Location: http://localhost/api/`, which drops the host port 3000. The browser never calls `/api` itself, so this only affects a person typing the URL.
- **Evidence:** the curl outputs in §1.2; `curl -sI http://localhost:3000/api | grep -i location` → `Location: http://localhost/api/`.
- **Spec:** §13.5; §14.3.
- **Fix:** optional. Either reject unknown parameter names with 400 (or record in D-041 that they are ignored on purpose), and add `absolute_redirect off;` (or `location = /api { return 404; }`) to `web/nginx.conf`.

### F-04 · minor · The Doris FE still has a thin memory margin after D-044 (carry-over of sprint C F-01)

- **What:** D-044 lowered the FE heap from 1280m to 1024m. That helped, but the margin is still small:
  - after about 25 minutes (gate, probes, a BE restart), `memory.peak` is 1.647 GB of 1.879 GB (87.7%);
  - the heap is 852 MB resident of 1024 MB, and native memory is about 690 MB with 421 threads (395 in D-044's measurement);
  - with a fully touched heap the FE would sit at about 1.71–1.75 GB, a margin of 130–170 MB. The thread count, and so native memory, grows with Flight SQL sessions and with the BE restart's reconnects.
  - Sprint E runs much longer, under chaos and a 20/s soak.
- **Evidence:** `jcmd GC.heap_info` → `total 1048576K, used 256055K`; heap mapping `Rss 851988 kB`; `memory.stat anon 1559552000`; `memory.events max 0 oom_kill 0`; `/proc/579/status Threads: 421`.
- **Spec:** §15.4, §3 R9, §18 item 7.
- **Fix:** no change is required in sprint D. In sprint E, record `memory.peak`, `memory.events` and the FE's `RestartCount` after the soak. If `max` events appear, apply R9's next step inside the budget (cap `-XX:MaxDirectMemorySize`, or `-XX:ReservedCodeCacheSize`/`-Xss` for native memory) before moving budget between services, and record it.

### F-05 · info · D-044 is a correctly applied and recorded R9 step, taken before an OOM kill

R9 is written for an OOM kill ("first lower JVM heaps … inside that container's budget"). No OOM kill happened: D-044 acted on a 95.6% `memory.peak` and a projection that a fully touched 1280 MiB heap plus about 680 MB of native memory exceeds the 1792 MiB limit. My measurements confirm the projection (native memory about 690 MB). The sprint C audit (F-01) recommended exactly this. The entry has what G3 and R9 ask for: the rule, the symptom, `docker stats` before and after, the container limit and the total budget unchanged, and the spec deviation named (§11.2's 1280m; §15.4's "Heap 1280 MiB" column should be read the same way). `fe.Dockerfile` makes the value a build argument and still verifies it with `grep` at build time. I agree with the decision. The sprint E README should mention the deviation in its troubleshooting or memory section.

### F-06 · info · The R4 ladder outcome is well evidenced, with one recorded client choice

D-040 says step 1 worked first time, so no G4 hypotheses were needed, and steps 2 and 3 are not built. The evidence is reproducible (log lines, the BE location, `/readyz`). The client is `arrow-flight`'s generated `FlightServiceClient` sending Flight SQL commands (`CommandStatementQuery`), not the `FlightSqlServiceClient` wrapper that R4 names. D-040 justifies it: the wrapper turns gRPC statuses into strings and would hide `UNAUTHENTICATED` and `UNAVAILABLE`, which §13.6 needs. The wire protocol and the transport name are the same, so this is a faithful reading of step 1, not a ladder step.

### F-07 · info · Logging notes for sprint E

- The explorer logs one `info` line per `/healthz` health check (every 5 s, about 17,000 lines a day). That is correct per §13.8, but it buries other lines; consider logging `/healthz` at `debug`.
- nginx's `error_log` (for example "connect() failed" while the explorer is down) is plain text on stderr; only the access log is JSON. The CLAUDE.md rule targets our own services, and nginx's error format cannot be made JSON.
- Doris's BE keeps `information_schema.backend_active_tasks` rows for some finished Flight SQL queries on `v_assets_enriched` for about 15 minutes (the multi-fragment join plans). Their current memory is about 1 MB in total, so this is harmless; it is worth knowing when reading that table in sprint E.

### F-08 · info · `docker compose stop|logs explorer` without profile flags fails

`docker compose stop explorer` and `docker compose logs explorer` (and the same for `web`) fail with "no such service: doris-init", because naming an `app` service without `--profile` leaves its `core` dependencies undefined. `docker compose restart explorer` works. `make logs SERVICE=explorer` and labkit (which always passes the three profiles) work too. The sprint E README and walkthrough should use the make targets or the profile flags.

### F-09 · info · Smaller notes

- `arrow-json` (§2.6) is not used; `convert.rs` reads the columns directly to build the nested §13.3 shapes. Recorded in D-041.
- `history` maps lake errors to `lake_unavailable` with a message heuristic (`error.rs:108-123`). It is unit-tested on realistic messages but was not exercised live, because stopping `iceberg-rest` would fail the Flink job's Iceberg commits. A Doris message that does not match falls back to 503 `upstream_unavailable`, which §13.2 also allows.
- Unsupported methods (for example `POST /healthz`) get Axum's default 405 with an empty body, not the §13.3 error body. §13.2 lists only `GET` routes.
- `specs/build-progress.md` still shows sprint D as "Next". That is expected at this point: plan §3.7 updates it after the audit.

## 3. Requirements table (sprint D scope)

| # | Requirement (spec) | Status | Note |
|---|---|---|---|
| 1 | §2.2 Rust stable ≥ 1.88 pinned in `rust-toolchain.toml`; Node 24 LTS | Met | 1.100.0 (`rust-version = "1.88"` kept); `node:24.22.3-bookworm-slim` (D-039) |
| 2 | §2.3 Runtime bases: `debian:bookworm-slim` (Rust), nginx stable-alpine latest at build (web) | Met | `debian:bookworm-20261123-slim`, `nginx:1.30.8-alpine`; recorded in D-039 |
| 3 | §2.6 axum 0.8.x, tokio 1.x, `arrow-flight` 60.x with `flight-sql`, tonic as required, serde, tracing (JSON) | Met | Exact pins; `arrow-json` unused (F-09, D-041) |
| 4 | §2.7 React 19, Vite, TypeScript 5.x, Vitest, ESLint; no UI library | Met | TypeScript 5.9.3 although 7.x exists (correct per §2.7) |
| 5 | §2.1 Exact versions, lockfiles committed, no `latest`, no ranges | Met | `Cargo.lock`, `package-lock.json`, `save-exact=true` |
| 6 | §3 R4 Flight SQL ladder, step recorded, `/readyz` reports the transport | Met | Step 1; D-040; `"doris_transport":"flight-sql"` (F-06) |
| 7 | §3 R6 Explorer `REFRESH TABLE` only if the TTL property fails or history lags > 60 s | Met | Not needed: TTL accepted (D-037), D03 about 21 s |
| 8 | §3 R9 Memory: limits within budget; `docker stats` before and after a change | Met | D-044 (F-05); margin still thin (F-04) |
| 9 | §4.3 steps 10–14 and §4.4 rows "web → explorer" and "explorer → Doris" | Met | nginx proxy; Flight SQL to the FE, `DoGet` to the BE at 172.30.0.3:8050; history through `lake.inventory.asset_changes` |
| 10 | §5.5 JSON datetimes RFC 3339 with milliseconds and `Z` | Met | `DATETIME(3)`, `DATETIME` and `timestamptz(6)` all rendered as `YYYY-MM-DDTHH:MM:SS.mmmZ`; truncation, not rounding (D-041) |
| 11 | §13.1 The explorer is the only way into the data; Doris not exposed to the browser | Met | Only relative `/api/*` calls; Flight ports internal. See F-01 for one request that still harms Doris |
| 12 | §13.2 `/healthz`, `/readyz` (200 and 503 bodies) | Met | 503 body unit-tested (`not_ready`, transport, error) |
| 13 | §13.2 `GET /api/assets/{id}`: 200, 400, 404, 503 | Met | Live and unit-tested |
| 14 | §13.2 `GET /api/assets`: filters, `limit` 1–500 default 50, `offset` ≥ 0 default 0, ordered by id, `{"items","limit","offset"}` | Met with note | Unknown parameters ignored (F-03); overflowing `offset` (F-01) |
| 15 | §13.2 History: 200, 400, 404 (no versions), 503 `lake_unavailable` or `upstream_unavailable` | Met | `LAKE_ENABLED=false` → 503 `lake_unavailable` without a query (unit test); lake-error heuristic not exercised live (F-09) |
| 16 | §13.2 `/api/stats/os-site` ordered by os, site; `/api/stats/change-rate` `minutes` 1–1440 default 60, ordered | Met | D05 compares with the MV rows exactly |
| 17 | §13.3 Asset, History and Error JSON shapes; `owner` null; null name, group id and group when the user row is missing | Met | Live for null owner; unit test for the missing user row |
| 18 | §13.4 SQL statements | Met | Unit tests compare the exact text |
| 19 | §13.5 Validation patterns; values reach SQL only after validation, as single-quoted literals; anything else 400 | Met with note | Hand-written checks equal to the patterns; injection strings rejected; unknown parameter names ignored (F-03) |
| 20 | §13.6 Location scheme translation, one reused client, re-handshake once on `UNAUTHENTICATED`, 503 and reconnect on transport errors, `QUERY_TIMEOUT_MS`, 64 MiB messages, UTC rendering, `LAKE_ENABLED=false` | Met with note | Session resets leak Doris sessions (F-02); FE restart not tested live |
| 21 | §13.7 Configuration variables and defaults | Met | Unit-tested; `LAKE_ENABLED=${ICEBERG_ENABLED:-true}` (D-041) |
| 22 | §13.8 `explorer healthcheck` subcommand; JSON logs with `service=explorer`, route, status, latency | Met | Health-check noise (F-07) |
| 23 | §14.1 Asset page (search prefilled with LT-0042, card fields, History button, 5 s refresh with last update), Stats page (OS × site, last 60 minutes with CSS bars) | Met | |
| 24 | §14.2 Loading, empty and error states; 404 shows "not found"; data only from `/api/*` | Met | Unit-tested (503, 404, network error) |
| 25 | §14.3 Vite, React and TypeScript; nginx serves the build, proxies `/api/` to `http://explorer:8080`, falls back to `index.html`; `npm run dev` proxies `/api` to `localhost:8080` | Met | Re-resolution verified with a new IP; `/api` redirect nit (F-03) |
| 26 | §14.4 / §16.2 web: type-check, lint, Vitest tests of the card from a fixture and the error state | Met | 7 tests |
| 27 | §16.2 explorer: validation with injection strings, Arrow batches → JSON, error → HTTP status mapping | Met | 24 tests, including every Arrow type Doris sends |
| 28 | §15.2 Profile `app`: `explorer` after `doris-init` (and `doris-init-lake`, `required: false`), `web` after a healthy explorer, `unless-stopped` | Met | |
| 29 | §15.3 Both on network `adl` | Met | Dynamic addresses inside `ip_range` |
| 30 | §15.4 explorer 192 MiB, web 64 MiB; total 11,456 MiB | Met | Usage 4 MiB and 7 MiB |
| 31 | §15.6 Health checks: explorer `explorer healthcheck` 15/5/10; web `wget -qO- http://localhost/` 10/5/10 | Met | nginx also listens on IPv6 so the check works (D-042) |
| 32 | §15.8 Host ports 8080 and 3000; Flight SQL ports internal | Met | |
| 33 | §15.9 `make test` and `make lint` run Cargo and npm in builder containers, with cache volumes | Met | D-043; green with the stack up, 85 s |
| 34 | §15.11 Multi-stage Dockerfiles with BuildKit cache mounts; heavy builds not while the stack is up | Met | Cargo registry and target cache mounts; npm cache mount; builders capped at 3 GiB and 1.5 GiB |
| 35 | §16.4 Gate D (D01–D08), with gates A, B and C re-run | Met | 10/10, 9/9, 5/5, 8/8; gate D 8/8 again after the BE restart |
| 36 | §16.6 CI `rust`, `web` and `images` jobs work with the committed files | Met (by inspection) | Same commands pass locally; not run on GitHub in this audit |
| 37 | §17.1 Layout; no single-file directories | Met | |
| 38 | §17.2 English; Prettier and ESLint; rustfmt and clippy; configuration only through environment variables; JSON logs with `service` | Met | nginx error log plain text (F-07) |
| 39 | G2 No acceptance test weakened; `acceptance/` and the specs untouched | Met | Locked-file checks return 0 |
| 40 | G3 Every rule applied and every "latest at build" pin recorded | Met | D-039 to D-044 |
| 41 | Later gates not put at risk (E: chaos soak, adapter and TaskManager kills, `make down` / `make up`, Dragonfly down, datacore restart) | Met with notes | No E scenario touches the explorer, and `adlctl wait` only needs its `/healthz`. The risks are F-01 (an accidental huge offset starves the BE), F-04 (FE memory during the long soak) and F-02 (session leak over a long run) |

## 4. Gate summary

| Gate step | Result |
|---|---|
| `make build && make clean && make up` | Green ("wait: ready", 98 s; explorer and web healthy) |
| `make test lint` | Green (Go ok; Maven 37/37; Cargo 24/24; Vitest 7/7; buf; golangci-lint 0 issues; Spotless; rustfmt; clippy; ESLint; Prettier; tsc), 85 s |
| `make acceptance-gate GATE=A` | Green, 10/10 (22 s) |
| `make acceptance-gate GATE=B` | Green, 9/9 (312 s; B09 p95 10.18 s, max 10.23 s) |
| `make acceptance-gate GATE=C` | Green, 5/5 (31 s) |
| `make acceptance-gate GATE=D` | Green, 8/8 (51 s), and 8/8 again after the `doris-be` restart (36 s) |
| Sprint 0 checks (buf lint and build, vet, build, acceptance vet, 38 cases, `go test`) | Green (14 s) |
| `make reconcile` | Green, lake compared (`assets=3015 groups=11 lake_assets=3015 users=303`), before and after the BE restart |

I left the stack running in full mode: every long-running service healthy, the Flink job `adl-processor` RUNNING (it restarted by itself after the `doris-be` restart and passed the reconcile), the generator at its default rate (about 10 changes per second), and no spinning BE threads. The throwaway container `audit-ipgrab` was removed. The explorer runs on 172.30.0.136 after the re-creation probe.

## 5. Fix status (written by the builder after the audit)

| Finding | Severity | Status |
|---|---|---|
| F-01 `offset` near `i64::MAX` makes a BE thread spin | Minor | **Fixed.** `offset` is capped at 1,000,000,000 (`validate::OFFSET_MAX`), above which it returns 400 `invalid_argument`. Unit tests cover the cap and offsets near `i64::MAX`. Recorded in D-045. Live: `?limit=1&offset=9223372036854775807` → 400; `offset=1000000000` → 200 |
| F-02 Session resets leak idle Doris sessions | Minor | **Fixed.** The FE channel and the bearer token are kept apart. Transport errors and timeouts drop only the channels, which reconnect on the next request with the same token. Only `UNAUTHENTICATED` triggers a new handshake (D-045) |
| F-03 Unknown query parameters; `/api` redirect drops port 3000 | Minor | **Partly fixed.** `absolute_redirect off` in `web/nginx.conf`; live: `Location: /api/`. Unknown query parameters are ignored on purpose, and D-045 records the reading of §13.5 |
| F-04 FE memory margin still thin | Minor | **Recorded, not changed.** After the re-run gate: FE 1.44 GiB / 1.75 GiB, `memory.peak` 1,583,243,264 (84%), `memory.events max 0`, `oom_kill 0`. Sprint E must watch it after the soak; R9's next steps are listed in `build-progress.md` |
| F-05 D-044 is a correct R9 step | Info | No change. The README (sprint E) will mention the §11.2 deviation |
| F-06 R4 outcome evidenced | Info | No change |
| F-07 Logging notes | Info | **Fixed in part:** `/healthz` requests are logged at `debug`. nginx's plain-text error log and the BE's finished-task rows are upstream behaviour |
| F-08 `docker compose stop/logs explorer` needs the profile flags | Info | Passed on to sprint E (README) in `build-progress.md` |
| F-09 Smaller notes | Info | No change. `build-progress.md` is now updated |

After the fixes, the full gate was re-run from `make down && make build && make clean && make up` (23:52–00:04 UTC):
- `make up`: ready.
- `make test lint`: green (Cargo 24/24, Vitest 7/7, Maven 37/37, every linter).
- Gates: A 10/10, B 9/9 (B09 p95 10.26 s, max 10.89 s), C 5/5, D 8/8 (D03 20.1 s). That is 32 PASS and 0 FAIL.
- Sprint 0 checks: green.
- `make reconcile`: `PASS (checked assets=3015 groups=11 lake_assets=3015 users=303)`.
- The locked-file checks against `plan-v1` and `sprint-0-done` exit 0.
