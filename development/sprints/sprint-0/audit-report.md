# Sprint 0 audit report

Auditor: an independent subagent that did not build the sprint. Date: 2026-11-25.
Scope: `git diff plan-v1..HEAD` (generated code only spot-checked), plus the untracked `specs/decision-log.md`, `specs/open-questions.md` and `specs/sprints/sprint-0/sprint-plan.md`.
Read in full: spec §1–§18 and Appendix A, sprints plan §3 and §4 (sprint 0), the sprint plan, the decision log and the open questions.

## 1. Summary

Sprint 0 is in good shape. The gate is green. All 38 acceptance cases compile and fail fast (about 0.2–1 s each, 22.6 s in total) for the right reason: an endpoint precheck reports `connection refused` for every service involved, and nothing panics, hangs or skips. The contracts match spec §6 byte for byte, and `buf generate` reproduces the committed code exactly. labkit and adlctl are complete and fail cleanly when the stack is down. The Makefile has every §15.9 target, and the CI workflows cover §16.6.

I compared every acceptance case with its §16.4 row. No case would fail against a correct implementation for a deterministic reason. Two locked cases assert less than their rows, and both need fixing before `sprint-0-done` locks `acceptance/`:
- **A10** checks "nothing in `dlq.adapter`" while triggers for LT-9008 may still be in flight (F-01).
- **E06** runs a Doris-only reconcile where the row says "the reconcile" (F-02).

There are no critical findings. Two are major (F-01, F-02); the rest are minor or info.

### Gate result (re-run exactly as written)

```
$ bash -c 'buf lint && buf build && go vet ./... && go build ./... && go vet -tags acceptance ./acceptance/... \
   && test "$(grep -hoE "^func TestGate[A-E]_[0-9]{2}_" acceptance/*_test.go | wc -l)" -eq 38 && go test -count=1 ./...; echo "GATE_EXIT=$?"'
?   	github.com/cristianrisueo/asset-discovery-lab/acceptance	[no test files]
ok  	github.com/cristianrisueo/asset-discovery-lab/adlctl	0.574s
?   	github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1	[no test files]
ok  	github.com/cristianrisueo/asset-discovery-lab/labkit	1.581s
GATE_EXIT=0
```

### Acceptance suite with no stack running

```
$ go test -tags acceptance -count=1 -timeout 10m -v ./acceptance/...
--- FAIL: TestGateA_01_SeedPublished (0.36s)
    gate_a_test.go:25: the stack is not running or not reachable (start it with make up):
        Kafka (kafka) not reachable at localhost:29092: dial tcp 127.0.0.1:29092: connect: connection refused
        datacore gRPC (datacore) not reachable at localhost:50051: ... connection refused
        ...
... (38 × --- FAIL, each with the same kind of precheck message)
FAIL	github.com/cristianrisueo/asset-discovery-lab/acceptance	22.640s
```

- 38 FAIL out of 38. There is no panic, no compile error and no `t.Skip`.
- Every failure is the precheck naming the unreachable host endpoint.
- The failure dump (`docker compose ps` plus the logs of the services involved) runs and degrades gracefully ("no such service").
- The precheck dials only host-exposed ports (§15.8). doris-be and flink-taskmanager have no host endpoint and are correctly not dialed. A correct stack will therefore not trip it.

Other checks run:
- `buf generate -o /tmp/...` followed by `diff -r` shows the committed `gen/` and `processor/src/generated/` are identical to fresh output.
- The four `.proto` files are identical to the §6.2–§6.5 blocks (diffed against the spec text).
- `golangci-lint run ./...` (local v2.14.0) reports 0 issues.
- Every adlctl subcommand exits 1 within a second with an `adlctl: <target> at <addr> …` message when its target is down. `wait` with no services exits 0.
- `make help` lists all targets. `make build`, `make up` and `make up-core` print `not available until sprint A` and exit non-zero.

## 2. Findings

### Critical

None.

### Major

**F-01 · major · `acceptance/gate_a_test.go:263-268` (A10 TransientErrorsRetried): the DLQ is checked before the adapter has finished handling LT-9008's triggers**
- **What is wrong:** the test reads `dlq.adapter` as soon as the first LT-9008 record at `vf` appears. The adapter publishes whatever version `GetAsset` returns, as long as it is ≥ the trigger's version (§9.4 "even if it is newer than the trigger's version"). The 20 upserts land within a few hundred milliseconds. So the first trigger that gets a successful fetch usually publishes `vf` already, while the remaining ~19 triggers for LT-9008 are still being retried under 50 % UNAVAILABLE chaos.
- **Consequence:** an adapter that wrongly dead-letters a transient error, for example after the three fast retries, does so *after* `vf` has appeared. The test reads the DLQ at that moment, so it often misses those records. The case then asserts less than its row ("Within `TPipe` … and `dlq.adapter` has nothing for `asset:LT-9008`"), and §16.3 adds that asserting that something does not happen needs a wait.
- **Evidence:** spec §16.4 A10; §9.4 (transient errors are "Never dead-lettered"); §16.3 "No sleeps as assertions. The exception is asserting that something does **not** happen: then wait at least three checkpoint intervals (30 s)."
- **Suggested fix** (the generator is already paused in this test):
  ```go
  deadline := time.Now().Add(TPipe)
  h.waitEntityRecord(entities, "LT-9008", last.GetVersion(), remaining(deadline))
  h.waitQuiet(remaining(deadline))        // outbox_pending = 0 and adl-adapter lag = 0
  for _, r := range h.readAll(dlq) { ... } // as now
  ```
  `waitQuiet` guarantees that every LT-9008 trigger was fully handled and committed. Since §9.5 commits only after the DLQ produce is acked, every DLQ record already exists when lag reaches 0. Optionally add `h.sleep(NotHappeningWait)` before reading, to follow the §16.3 wording literally. Record the reading in D-012.

**F-02 · major · `acceptance/gate_e_test.go:246` (E06 DatacoreRestart): the reconcile skips the lake**
- **What is wrong:** the final step calls `h.reconcile("Doris reconcile", labkit.ReconcileOptions{Timeout: TReconcile, NoLake: true})`. The E06 row says "…which becomes visible in Doris, **and the reconcile passes**".
  - Where the spec means a Doris-only reconcile it says so explicitly: B01 "Reconcile Doris only", E02 and E04 "the Doris reconcile passes".
  - `labkit.Reconcile` per §16.5 includes the lake unless `--no-lake` is given, and gate E always runs on the full stack with the lake (§16.3).
  - This interpretation is not recorded in D-012 (the E06 bullet only covers `/readyz` and the `GetAsset` retry).
- **Consequence:** a datacore restart that broke the lake path, for example versions restarting at 1 so the lake later shows "ahead" or "behind", would go unnoticed by the case written for exactly that risk (§7.2 "Persistence is mandatory").
- **Evidence:** spec §16.4 E06, §16.5 step 4, §16.3 (gates C to E run on the full stack with the lake), G2.
- **Suggested fix:** `h.reconcile("Doris and lake reconcile", labkit.ReconcileOptions{Timeout: TReconcile})`. The lake's 60 s p95 freshness (§1.5) fits comfortably in `TReconcile`. If the builder prefers to keep `NoLake`, record it under G1 in D-012 and in `open-questions.md`, but G1 ("keep the §16 tests meaningful") favours the fix.

### Minor

**F-03 · minor · `acceptance/gate_d_test.go:150-186` (D04 ListEndpoint): pages can shift while changes still in flight land**
- **What is wrong:** the test pauses the generator but then pages straight away. Changes already in the pipeline (up to ~30 s of them at 10/s, committed to Doris in bursts at each 10 s checkpoint) can still move an asset with a low id into or out of Madrid between the two requests. The second page then shifts by one and repeats the last item of the first page, which gives a false "appears on both pages". I estimate the probability at well under 1 % per run, but `acceptance/` is locked.
- **Evidence:** §16.4 D04, §16.3 isolation ("A test that needs a quiet system pauses the generator"), §1.5 freshness.
- **Suggested fix:** after `h.pauseGenerator()`, add `h.reconcile("Doris reconcile", labkit.ReconcileOptions{Timeout: TReconcile, NoLake: true, NoPause: true})`, exactly as D05 does. Doris is then quiet before paging.

**F-04 · minor · `acceptance/gate_a_test.go:87-90` (A03 TriggerContract): one undecodable record fails the whole test**
- **What is wrong:** any record keyed `asset:LT-9002` that does not decode as a `Trigger` aborts the test through `labkit.Stop`. §16.3 says scans "MUST count only records that pass §6.7". The row asks only that *a* record with that key decodes correctly.
- **Risk:** nothing in the spec produces such a record (chaos uses `malformed:<seq>` keys), so the practical risk is nil. It is still stricter than the row in a locked file.
- **Suggested fix:** replace the `Stop` with `continue`, so undecodable records are ignored and the test keeps polling for a valid one.

**F-05 · minor · `labkit/kafka.go:316-321` (`Tail.ReadToEnd`): buffered records can be skipped**
- **What is wrong:** when a 3 s poll returns nothing while partitions are behind, the code assumes compaction and jumps `pos` to the end offsets. If the first fetch is just slow (a cold client, a loaded VM, a broker restart), the kgo client still holds those records. Later calls then see `behind()` empty, never poll again, and never read them until newer records arrive.
- **Consequence:** A01, which reads 3,000+ records from the start with the generator paused, could stall until `TReconcile`.
- **Evidence:** spec §16.4 A01; §16.3 Waiting rule.
- **Suggested fix** (labkit is editable, so this can wait): use the per-partition `HighWatermark` and `LogStartOffset` that come back in each `FetchPartition` to tell compaction gaps from slowness, or treat "no records" as caught up only after several consecutive empty polls. Also drain whatever kgo has buffered (a non-blocking `PollFetches`) on every call, even when `behind()` is empty.

**F-06 · minor · `Makefile:76-78` (`make lint`): `buf breaking` never runs during the build**
- **What is wrong:** `buf breaking` is skipped whenever HEAD is `main`, and the unattended build runs entirely on `main`. §15.9 says "buf breaking when `main` exists", and §6.1 says "starting from the second commit".
- **Evidence:** spec §15.9 `lint` row, §6.1. CI does compare against `HEAD~1` on main; the Makefile does not.
- **Suggested fix:** on `main`, run `buf breaking --against '.git#ref=HEAD~1'` when `HEAD~1` holds `buf.yaml` (skip otherwise), mirroring `ci.yml`. Record it in D-010.

**F-07 · minor · `.github/workflows/ci.yml:44` (`detect` job) and `e2e.yml` (`make build`): `.env` is missing in CI**
- **What is wrong:** `.env` is git-ignored and never created in CI. Once later sprints add services that use `env_file: .env` or `${S3_ACCESS_KEY}`-style variables, `docker compose config` fails or substitutes empty values. The `images` job then breaks, and `make build` in e2e may fail before `make up` creates `.env`.
- **Suggested fix:** add a step `cp .env.example .env` (or `make env`) before `docker compose config` in `detect`. In sprint A, make `build` depend on `env` in the Makefile.

**F-08 · minor · `specs/decision-log.md` D-006 to D-012: entries are missing G3 fields**
- **What is wrong:** G3 requires date, rule id, what failed (exact error or symptom), evidence, decision and diagram impact for every entry. Several entries carry only a decision and the diagram line:
  - D-007 has no symptom and no evidence;
  - D-008, D-009, D-010 and D-012 have no "Prompted by" or evidence lines;
  - D-006 states "latest at build" without a source per pin.
- **Missing entries:** the `make lint` breaking skip (F-06) and the E06 Doris-only reading (F-02), if either is kept.
- **Suggested fix:** add the missing "Prompted by" and "Evidence" bullets, for example "spec §16.4 row X leaves Y open" or "proxy.golang.org @latest on 2026-11-25".

### Info

**F-09 · info · `specs/sprints/sprint-0/`: reports not written yet**
- `build-report.md` does not exist at audit time, though plan §3.7 orders it before the audit.
- `specs/build-progress.md` does not exist either, and `README.md` already links to it.
- Write both before `chore(sprint-0): close sprint`.

**F-10 · info · `acceptance/gate_a_test.go:238-250` (A09): the "nothing happens" checks need no wait**
- These checks run without the 30 s wait. That is sound here: there is a single post-upsert trigger, and partitions are processed sequentially (§9.2). A stale publish or a `STALE_VERSION` dead letter would therefore come instead of, or before, `v` on the same partition.

**F-11 · info · `acceptance/gate_e_test.go:186` (E04): the test needs a new job id after `make up`**
- It requires `j.ID != before.ID`. That holds as long as the sprint-B submitter does not pass a fixed `jobId` to `POST /jars/{id}/run`. §10.11 step 5 lists no `jobId`, so a correct submitter yields a random id.
- Add a note to `build-progress.md` so sprint B does not pin the job id.

**F-12 · info · B05: Go and Java may judge "undecodable" bytes differently**
- `labkit.UndecodableBytes` picks bytes that Go's protobuf parser rejects, but the processor decodes with protobuf-java. The two parsers agree on the relevant failure classes (truncation, invalid wire types, invalid UTF-8 in proto3 strings, which Java checks with `readStringRequireUtf8`), so a mismatch is very unlikely.
- If B05 ever reports `VALIDATION_ERROR` for the garbage record, check this first, before touching the processor.

**F-13 · info · `adlctl/main_test.go:150`: a unit test skips when Docker is absent**
- `TestWaitNoServices` calls `t.Skip` when no Docker socket exists. It is a unit test, not an acceptance test, so the "never skip" rule does not apply. On GitHub runners Docker is present.

**F-14 · info · B08: "since the stack started" is read as the Flink job's start time**
- Gate A tests pause the generator for a while, so the 3 minutes may not all be generator activity. The `TPipe` wait for a closed asset row covers this. The reading is recorded in D-012 and Q-001.

## 3. Requirements table (sprint 0 scope)

| # | Requirement (spec) | Status | Note |
|---|---|---|---|
| 1 | Layout §17.1: `proto/adl/v1`, `gen/go/adl/v1`, `adlctl/`, `labkit/`, `acceptance/`, `processor/src/generated/java`, `.github/workflows`, `buf.yaml`, `buf.gen.yaml`, one `go.mod`, compose file, `.env.example`, Makefile, README, LICENSE (MIT) | Met | Service dirs and `infra/` are deferred to their sprints (git cannot hold empty dirs). Host-prep is inline (D-007) |
| 2 | No single-file directories (CLAUDE.md, §17.1) | Met | Only buf/Java/GitHub-forced nesting (`proto/adl`, `gen/go/adl`, `processor/src/generated/java/...`, `.github/workflows`) |
| 3 | English everywhere; Conventional Commits | Met | Commit history follows `feat(...)`, `test(...)`, `ci:` and `chore:` |
| 4 | Pinned versions, never `latest` (§2.1) | Met | Go modules, buf plugins, buf CLI, golangci-lint, alpine 3.24.4 and actions are all exact. No `latest` or `@main` found |
| 5 | Contracts §6.2–§6.5 verbatim | Met | All four files are byte-identical to the spec blocks |
| 6 | `buf.yaml` exactly §6.6 | Met | Identical |
| 7 | `buf.gen.yaml` plugins pinned, outputs as specified | Met | go v1.36.12, grpc/go v1.6.2, java v36.2 → `gen/go`, `processor/src/generated/java` |
| 8 | protobuf-java pairing recorded (§6.6, §2.4) | Met | D-004: protobuf-java 4.36.2 for sprint B |
| 9 | Generated code committed and reproducible (§6.1, §6.6) | Met | Fresh `buf generate` gives identical output |
| 10 | `.gitattributes` marks generated code `linguist-generated=true` (§17.2) | Met | `gen/go/**` and `processor/src/generated/**` |
| 11 | `.gitignore` covers `.env`, `target/`, `bin/`, `dist/`, `node_modules/` (§17.2) | Met | Also `logs/` and `build/` |
| 12 | `.env.example` exactly the §17.2 block | Met | Identical five lines |
| 13 | `docker-compose.yml`: `name: adl`, `adl` network per §15.3, no services | Met | Subnet, `ip_range` and gateway are exact; `services: {}` |
| 14 | Makefile: every §15.9 target, `help` default | Met | All 20 targets present |
| 15 | Targets that cannot work yet print `not available until sprint X` and exit 1 | Met | `build`, `up` and `up-core` (sprint A). Java, Rust and web parts of `test`/`lint` fail loudly once their build file exists |
| 16 | `make lint` runs buf breaking when `main` exists | Partially met | Skipped on `main` itself, where the build runs (F-06) |
| 17 | labkit Kafka client (produce, tail, end offsets, lag) | Partially met | Correct API and lag maths. The empty-poll compaction heuristic can skip buffered records (F-05) |
| 18 | labkit Dragonfly client | Met | GET and TTL with the Redis -1/-2 semantics; `CacheKey` per §9.3 |
| 19 | labkit datacore admin and data API client | Met | Upserts, List* (streams), generator, chaos, Burst, stats, GetAsset; the chaos-on config equals §7.9 |
| 20 | labkit Doris (MySQL protocol) | Met | Forces UTC parse and client-side interpolation. Queries match §11.3–§11.6 and §16.4 (C02 and C04 queries verbatim) |
| 21 | labkit S3 (RustFS) | Met | Path-style, static credentials, paginated ListObjectsV2 |
| 22 | labkit REST catalog | Met | Parses LoadTable: current schema, default spec, format version, location |
| 23 | labkit Flink REST | Met | `/jobs/overview`, vertex metrics `N.VersionGate.<metric>`, `/jobs/{id}/checkpoints` `latest.restored`, `/stop`, `/savepoints/{trigger}` |
| 24 | labkit HTTP | Met | Raw URL kept for D02's injection string |
| 25 | Reconciliation §16.5: pause and restore, poll every 2 s, normalisation, five Doris categories, lake max version and duplicates, counts plus first 20 diffs | Met | Pure comparison functions are unit-tested. "Extra in lake" is deliberately absent, as §16.5 step 4 does not ask for it (D-008) |
| 26 | adlctl §15.10: every subcommand exists | Met | wait, generator, chaos, burst, move, reconcile, flink stop-with-savepoint, stats |
| 27 | adlctl fails cleanly when its target is down | Met | Verified live: exit 1 within 1 s with a one-line error naming the target. Usage errors exit 2 |
| 28 | Harness §16.3: package `acceptance`, tag `acceptance`, uses labkit | Met | `doc.go` is untagged so `go build ./...` sees the package |
| 29 | Harness: test names `TestGate<X>_<NN>_<Name>`, 38 functions, gates run A→E | Met | Names match §16.4 exactly; file order gives A→E |
| 30 | Harness: env var names and defaults | Met | All 11 match §16.3, including the Doris DSN |
| 31 | Harness: timeout constants defined once | Met | 30, 90, 120, 180 and 300 s |
| 32 | Harness: 1 s polling, last state on timeout | Met | `labkit.Poll`, `PollInterval = 1s` |
| 33 | Harness: no sleeps as assertions (30 s exception) | Partially met | B03 uses the 30 s wait. A10's absence check has no wait and no quiet barrier (F-01) |
| 34 | Harness: generator and chaos restored in `t.Cleanup` | Met | `pauseGenerator`, `setGenerator`, `setChaos`, reconcile's own restore; LIFO order keeps clients open |
| 35 | Harness: failure dump (compose ps and the last 200 lines per involved service) | Met | Verified in the no-stack run |
| 36 | Harness: reserved ids §5.4, no shared ids between tests | Met | A: LT-9001–9008, G-90, U-9001, LT-8001. B: LT-9101–9110, G-91, U-9101. C: LT-9201. D: LT-9301/9302, G-93, U-9301. E: LT-9501–9503. LT-9999 is only read. No overlap |
| 37 | Harness: topic scans count only §6.7-valid records | Met | `DecodeEntity` in A01 and every entity wait; A03 is stricter than needed (F-04) |
| 38 | Every case fails only for the right reason with no stack | Met | 38/38 connection-refused prechecks in 22.6 s |
| 39 | Gate A cases A01–A09 vs §16.4 | Met | A05 "two more" read as ≥ 2 (Q-001). A06 also checks the coordinates |
| 40 | Gate A case A10 vs §16.4 | Partially met | The DLQ absence is asserted too early (F-01) |
| 41 | Gate B cases B01–B09 vs §16.4 | Met | B03 30 s wait; B04 metric delta; B05 three reasons plus RUNNING plus LT-9107; B07 150 s; B08 closed row, 60 s windows, `changes > 0`, no duplicates; B09 nearest-rank p95 and max |
| 42 | Gate C cases C01–C05 vs §16.4 | Met | C01 checks names, types, required, `day(changed_at)` and v2; C03 uses the catalog location; C05 checks lake max version and duplicates |
| 43 | Gate D cases D01–D03, D05–D08 vs §16.4 | Met | §13.3 JSON shapes, RFC 3339 ms with `Z`, error codes, ordering, `<div id="root">`, the `doris_transport` rule with `ADL_ALLOW_MYSQL_FALLBACK` |
| 44 | Gate D case D04 vs §16.4 | Met (minor flake risk) | F-03 |
| 45 | Gate E cases E01–E05 vs §16.4 | Met | E01 reason set, key rule and empty processor DLQ; E02/E03 kill and start timing; E04 new savepoint restored, upsert, Doris reconcile; E05 Dragonfly down |
| 46 | Gate E case E06 vs §16.4 | Partially met | The reconcile excludes the lake (F-02) |
| 47 | CI `ci.yml` §16.6: contracts, go, java, rust, web, images; tolerant of missing components | Met | `detect` gates the java, rust, web and images jobs. Breaking runs against main, or `HEAD~1` on main. The generated-code diff check is present. `.env` is missing for later sprints (F-07) |
| 48 | CI `e2e.yml` §16.6 | Met | Free disk, setup-go, `make build`/`up`/`acceptance`, logs artifact on failure, `make clean` always. A comment says it works from sprint E |
| 49 | Decision log G3 format and every "latest at build" pin | Partially met | Pins recorded (D-001 to D-006); several entries lack symptom and evidence fields (F-08) |
| 50 | Open questions recorded instead of weakening tests (G2) | Met | Q-001 and Q-002 |
| 51 | Sprint reports (plan §3.7): `build-report.md`, `build-progress.md` | Not met (yet) | Not present at audit time (F-09); expected before the close commit |

## 4. Resolution (added by the builder after the audit)

Every critical and major finding is fixed. Most minor findings are fixed too. The gate was re-run afterwards and is green, and the no-stack acceptance run again gives 38 FAIL, all because services are not reachable. Details are in decision log D-012 and D-013.

| Finding | Severity | Resolution |
|---|---|---|
| F-01 A10 checks the DLQ too early | Major | Fixed. After the final version arrives, A10 waits within the same `TPipe` budget for a quiet system (outbox empty, `adl-adapter` lag 0), then asserts that the DLQ holds nothing for `asset:LT-9008`. |
| F-02 E06 uses a Doris-only reconcile | Major | Fixed. E06 now runs the full reconcile, Doris and the lake. |
| F-03 D04 flake risk | Minor | Fixed. D04 runs a Doris reconcile, with no pause because the generator is already paused, before paging. |
| F-04 A03 aborts on an undecodable record | Minor | Fixed. Such records are ignored. |
| F-05 `Tail.ReadToEnd` gives up after one empty poll | Minor | Fixed. It now needs two consecutive empty 3 s polls. |
| F-06 `make lint` skips `buf breaking` on main | Minor | Fixed. It runs against `.git#branch=main` whenever main has contracts. |
| F-07 CI without `.env` | Minor | Fixed. The `detect` job creates `.env`, and e2e runs `make env`. |
| F-08 decision log missing G3 fields | Minor | Fixed. D-006 to D-012 now have "Prompted by" and "Evidence". |
| F-09 sprint reports missing | Info | Written: `build-report.md` and `specs/build-progress.md`. |
| E04 job id note | Info | Recorded in `specs/build-progress.md` for sprint B. |
