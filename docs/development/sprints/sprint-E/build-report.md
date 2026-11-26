# Sprint E build report: Failures and close

## What was built or changed

| Area | Spec | Files |
|---|---|---|
| Adapter: a failed offset commit is retried | §9.2, §9.5 | `adapter/internal/consumer/consumer.go` and its test. Retries with backoff for up to 30 s while rebalances stay blocked; after that, `ErrCommit` and a restart from the last committed offset. Before, the failure was only logged, which could leave `adl-adapter` lag above 0 forever when no later batch arrived (D-047) |
| `stop-with-savepoint` waits for a failing-over job | §10.11 | `labkit/flink.go` (`SettledJob`), `adlctl/flink.go`, and tests. A job in RESTARTING or INITIALIZING used to be skipped with "nothing to stop", so `make down` took no savepoint (D-048) |
| Adapter walkthrough logs | §4.3 step 2, §9.8 | `adapter/internal/handler/handler.go`: the `debug` publish lines carry `partition` and `offset`. `docker-compose.yml`: `LOG_LEVEL: ${ADAPTER_LOG_LEVEL:-info}` (D-049) |
| README | §17.2 | `README.md`, complete: overview, architecture (§4.1 diagram, data flow, layout), quick start, Make targets, services and ports, the LT-0042 walkthrough with commands, testing, troubleshooting, "How this was built", licence |
| CI check | §16.6 | No workflow change was needed. actionlint v1.7.12 is clean, and every pinned action tag exists (D-046). Pushing is left to the owner (Q-003) |
| `docker stats` snapshot | §18 item 7 | D-050 |
| Decision log and open questions | G3 | D-046 to D-050; Q-003 |

No acceptance test, spec or plan file was touched.

## Results

### Exploratory gate E run (plan step 2)
This was gate E's first run ever, at 00:27–00:35 UTC, on the stack left by the sprint D gate. Before any sprint E change was deployed, it passed **6/6** in 462 s:
- E01: 186 s.
- E02: 42 s.
- E03: 56 s.
- E04: 145 s.
- E05: 11 s.
- E06: 20 s.

FE memory, sampled every 30 s, peaked at 1,644,564,480 bytes (87.5% of its limit), with no `max` events. So no conditional fix of plan step 4 was needed. The two fixes of step 3 are proactive: they close gaps the plan's investigation found, and gate E would hit them only under unlucky timing.

### The gate, exactly as plan §4 writes it (00:36–00:56 UTC)
It ran in `/tmp/adl-e/gate.sh` after `make down`, with images rebuilt with the sprint E changes.

| Step | Result |
|---|---|
| `make down` | Savepoint `savepoint-1e639b-b990a5007ec0` written |
| `make build && make clean && make up` | Green. `make up` from clean took **66 s** to `wait: ready` (DoD item 1: 10 minutes or less) |
| `make test lint` | Green in 1 min 49 s: Go ok, Maven 37/37, Cargo 24/24, Vitest 7/7, buf, golangci-lint 0 issues, Spotless, rustfmt, clippy `-D warnings`, ESLint, Prettier, `tsc` |
| `make acceptance` | **38 PASS, 0 FAIL**, in 828 s. B09 p95 10.34 s, max 10.37 s. C02 20.6 s. D03 20.2 s. E01 188 s, E02 42 s, E03 53 s, E04 116 s, E05 10 s, E06 24 s |
| `make chaos-on && sleep 180 && make chaos-off && make reconcile` | `reconcile: PASS (checked assets=3018 groups=11 lake_assets=3018 users=303)` |

The sprint 0 checks are also green: buf lint and build, `go vet`, `go build`, the acceptance vet, the 38-case count, and `go test ./...`. So are the locked-file checks (`git diff --quiet plan-v1 …` and `git diff --quiet sprint-0-done -- acceptance/`).

### `docker stats` (§18 item 7)
The snapshot was taken 5 minutes after the gate, at the default load. Total usage was **5.64 GiB of 11.19 GiB** of limits. Every container was under its limit, with `RestartCount=0` and `OOMKilled=false` everywhere. The FE was at 1.388 GiB of 1.75 GiB, with cgroup peak 84.4% and `memory.events max 0`. The full table is in D-050.

## LT-0042 walkthrough (§4.3, DoD item 6)

It was run at 01:03–01:05 UTC on the gate's stack, with the README's own commands.

**Before:** the generator was paused, and the adapter was recreated with `ADAPTER_LOG_LEVEL=debug`. The explorer said LT-0042 was already **in Madrid at v4**, owned by U-0117 "Sara Gómez" in G-05 "Finanzas": the generator had moved the asset and renamed and regrouped the user. This is why the README does not assume seed values. To reproduce the map's scenario, `make move ID=LT-0042 SITE=Alicante` gave v5, then `make move ID=LT-0042 SITE=Madrid` gave **v6** at `changed_at 2026-11-26T01:03:54.449Z`.

| # | Observed |
|---|---|
| 1 | `triggers`: key `asset:LT-0042` at partition 0, offsets 6686 (v5) and 6687 (v6) |
| 2–4 | Adapter log: `{"msg":"published","key":"asset:LT-0042","version":6,"entity_version":6,"outcome":"success","attempt":1,"partition":0,"offset":6687}` at 01:03:54.494, 44 ms after the commit. Dragonfly `entity:asset:LT-0042:v6`: TTL 3571 s, 75 bytes |
| 5 | `entities.assets`: key `LT-0042` with header `trigger.version:6` |
| 6–7 | Flink: job `adl-processor` RUNNING. The `VersionGate` vertex's counters over REST: `versionGateAccepted` sum 6346, `versionGateDropped` sum 103 |
| 8 | Doris `inventory.assets`: `LT-0042 Madrid 6 2026-11-26 01:03:54.449`. `v_assets_enriched`: owner "Sara Gómez", group "Finanzas". The MV lists the Madrid rows per OS |
| 9 | `lake.inventory.asset_changes`: versions 1 to 6 (Alicante, Valencia, Bilbao, Madrid, Alicante, Madrid), with `timestamptz` values |
| 10–13 | `curl localhost:3000/api/assets/LT-0042` through nginx: Madrid, v6, the nested owner and group. The explorer logged `route /api/assets/{id}`, 200, 47 ms. `/readyz`: `{"status":"ready","doris_transport":"flight-sql"}`. `localhost:3000/` serves `<div id="root">` |
| 14 | `/api/assets/LT-0042/history`: v1 to v6 in ascending order, 221 ms |

**After:** the adapter went back to `LOG_LEVEL=info` and the generator was resumed. One correction: the README's `entities.assets` grep was checked against the real output format, `trigger.version:6<TAB>LT-0042`.

## Definition of Done (§18)

| # | Item | Status | Evidence |
|---|---|---|---|
| 1 | `make build && make up` reaches ready; `make up` takes 10 minutes or less once built | Met | 66 s from `make clean`. Every step of `adlctl wait` is green |
| 2 | `make acceptance` passes every case of gates A to E | Met | 38/38 in the gate run. D06 passes on `flight-sql` (R4 step 1, D-040) |
| 3 | `make test` and `make lint` pass | Met | Gate step 2 |
| 4 | Chaos on, three minutes, chaos off, then reconcile is green | Met | Gate step 4: reconcile PASS, lake compared |
| 5 | `make down && make up` resumes from a savepoint; `make clean && make up` starts from scratch; a green reconcile follows each | Met | E04, twice: a new savepoint, `latest.restored` is a savepoint, then a Doris reconcile. Also the `make clean && make up` of the gate, then 38/38 with reconciles |
| 6 | Every row of §4.4 exists and works; the walkthrough can be followed | Met | The walkthrough above, plus the table below |
| 7 | A `docker stats` snapshot is in the decision log; no container is over its limit; the total is at most 11.5 GiB | Met | D-050 |
| 8 | Both CI workflows exist and pass on GitHub | **Partly met** | Both exist, actionlint is clean, the tags exist, and the commands pass locally. They have not been run on GitHub, because the build never pushes (Q-003) |
| 9 | The README is complete (§17.2) | Met | All ten §17.2 sections |
| 10 | The decision log lists every rule and pin; no TODO or FIXME without a reference | Met | D-001 to D-050. `grep -rnE 'TODO\|FIXME'` over code, scripts, SQL, configs and workflows (excluding `specs/`, `node_modules`, `target`) finds nothing |

**§4.4 rows,** each with its working evidence:

| Row | Evidence |
|---|---|
| datacore → triggers | Walkthrough step 1; A03 |
| triggers → adapter | Walkthrough step 2 (partition and offset); `adl-adapter` lag 0 (E02) |
| adapter → datacore | `get_calls` rising; A05 and A09 |
| adapter ↔ Dragonfly | The v6 key with its TTL; A05 |
| adapter → entities.* | Walkthrough step 5; A02 and A04 |
| adapter → dlq.* | A06–A08; E01 (DECODE_ERROR, VALIDATION_ERROR and NOT_FOUND) |
| entities.* → processor | VersionGate counters; B02 |
| processor → dlq.* | B05 |
| processor → Doris | Walkthrough step 8; B01–B09 |
| processor → Iceberg | Walkthrough step 9; C02 |
| Iceberg → RustFS | C03 |
| Doris → Iceberg | Walkthrough steps 9 and 14; C02 and C05 |
| web → explorer | Walkthrough steps 10–13; D07 |
| explorer → Doris | `/readyz` `flight-sql`; D06 |

## Deviations from the plan

- **Gate E passed before any fix.** Plan step 2 expected failures to diagnose; none appeared, in either the exploratory run or the gate run. The plan step 3 fixes were still made, because they close real gaps the investigation found (D-047, D-048).
- **D-049 was not in the plan.** The walkthrough needs the adapter's log to show the trigger's partition and offset (§4.3 step 2), and nothing logged them before.
- **The plan's step 4 conditional fixes were not needed:** no FE memory event, no stale clients after E04, recovery well inside `TRecover`, and no lake issue.
- **The workflows were not changed:** actionlint and the tag check found nothing.
- **No bug from an earlier sprint broke a gate.**

## After the audit

See `audit-report.md` and its fix-status section.
