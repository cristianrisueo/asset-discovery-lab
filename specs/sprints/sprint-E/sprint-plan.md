# Sprint E plan · Failures and close

## Context

Sprints 0 to D are done and tagged. Gates A to D are green: 32/32 on the last run. The full stack is up right now, from the `make clean && make up` of the sprint D gate run.

Sprint E, as plan §4 describes it:
- make gate E pass (E01–E06), fixing earlier components where needed;
- check `e2e.yml` end to end;
- write the README (§17.2);
- record the `docker stats` snapshot (§18 item 7);
- walk the LT-0042 walkthrough (§4.3) with `make move` and record it;
- have the audit subagent review the **whole repository** against the spec and the Definition of Done (§18).

Gate E has **never been run**. Two read-only investigations, of the adapter and datacore and of Flink, Doris and the lake, found no certain failure. They did find the risks listed below.

Gate:
```sh
make build && make clean && make up
make test lint
make acceptance                  # every gate, A to E
make chaos-on && sleep 180 && make chaos-off && make reconcile
```

## Steps

### 0. Save this plan (plan §3.2)
Save it verbatim to `specs/sprints/sprint-E/sprint-plan.md`.

### 1. Baseline on the running stack (read-only checks first)
- **FE cgroup baseline**, for the R9 watch from sprint D audit F-04: `docker stats --no-stream`, plus `memory.peak`, `memory.events` and the threads of `doris-fe` read through `docker compose exec doris-fe cat /sys/fs/cgroup/...`.
- **Leftover duplicates.** Look for duplicate (`window_start`, `entity_type`) pairs in `change_rate_1m` and duplicate (`id`, `version`) pairs in the lake. `DuplicateWindows` scans the whole table, so an old duplicate would fail E03 for the wrong reason (Flink risk 3).
- **Verification:** the numbers are written down for the decision log.

### 2. First exploratory gate E run (spec §16.4 gate E)
- Run `make acceptance-gate GATE=E` on the running full stack.
- While it runs, sample the FE's `memory.peak` and `memory.events` before and after E01, and `docker inspect` its `RestartCount` and `OOMKilled`.
- For every failure, apply G4: write the hypothesis, reproduce it, and fix the root cause in the earlier component. Never touch `acceptance/` (G2).

### 3. Proactive robustness fixes
These are small, unit-tested fixes for gaps the investigations found and that gate E exercises. They follow sprints plan §3.3 ("fix bugs of earlier sprints in this sprint").

**a. Adapter: a failed offset commit is never retried.**
- **The gap:** `adapter/internal/consumer/consumer.go:128-134` only logs it. With the generator paused (E02), no later batch covers it, so `adl-adapter` lag can stay above 0 forever.
- **The fix:** retry the commit with a short backoff, a few attempts within the existing 10 s context. If it still fails, remember the last handled records per partition and commit them on the next poll cycle, even an empty one.
- **Unit test:** a fake committer that fails first, then succeeds (§16.2: "offsets are committed only after the publish is acknowledged").

**b. adlctl `flink stop-with-savepoint` skips jobs in a transient state.**
- **The gap:** a job in RESTARTING or CREATED is skipped, with "nothing to stop" (`adlctl/flink.go:31-37`, `labkit/flink.go`). E04 would then fail with "did not restore from a savepoint".
- **The fix:** wait up to about 60 s, within `--timeout`, for such a job to become RUNNING, then stop it as §10.11 says.
- **Unit test:** a fake Flink REST server.

Each fix gets a decision-log entry (G3).

### 4. Conditional fixes
Each fix below is applied only when its symptom is observed, with G4 hypotheses recorded.

| Symptom | Rule | Fix |
|---|---|---|
| FE `memory.events max > 0`, an OOM kill, or `memory.peak` ≥ 95% | R9 next step, inside the FE budget (D-044) | In `infra/doris/fe.Dockerfile`'s `JAVA_OPTS_FOR_JDK_17`, add `-XX:MaxDirectMemorySize=256m -XX:ReservedCodeCacheSize=128m -Xss512k`. Record `docker stats` before and after. Move budget between services only after that, and stay at or under 11.5 GiB |
| E04: stale gRPC or MySQL clients after `make up` | G1 | `labkit/datacore.go`: `grpc.WaitForReady(true)`, with the existing 10 s `callCtx` cap |
| E03: recovery slower than 300 s because of restart backoff | R17, G1 | Set `restart-strategy.exponential-delay.max-backoff`, checked against the Flink 2.1 docs, in the `x-flink-properties` anchor, and record it as an addition to §10.9 |
| E03 or E04: the Iceberg committer fails to restore, or lake duplicates appear | R7 | Diagnose from the JobManager and TaskManager logs. Check the uids against §10.10 and the committer's state |
| E01: `STALE_VERSION`, stray DLQ keys, or `dlq.processor` records | §9.4, §6.7 | Fix the adapter or datacore classification |

### 5. Check `e2e.yml` end to end (§16.6)
- **Static check:** `actionlint`, pinned through `go run github.com/rhysd/actionlint/cmd/actionlint@<latest at build>`, on both workflows. Record the pin (G3).
- **Action tags exist:** confirm every pinned action tag in `ci.yml` and `e2e.yml` with `git ls-remote --tags https://github.com/<owner>/<action>`. These are public read-only lookups.
- **Compare with §16.6:** free disk, Go, `make build`, `make up`, `make acceptance`, logs saved as an artifact on failure, and always `make clean` at the end. The workflow already has all of these; fix any gap actionlint reports.
- **Run its steps locally, in its order:** `make env`, `make build`, `make up`, `make acceptance`, with the log collection command checked by hand. This is step 7's gate run, so it is not run twice.
- **Not pushed to GitHub.** Pushing is an outward action that nobody authorised; the script only tags. DoD item 8 ("pass when run on GitHub") is recorded in `open-questions.md` as Q-003, an owner step after the build, with the local evidence.

### 6. README (§17.2)
Replace `README.md` with every section §17.2 lists:
- overview;
- architecture, with the §4.1 mermaid diagram;
- quick start: prerequisites from §15.1, then `make build`, `make up` and `make acceptance`;
- Make targets (§15.9);
- services and ports (§4.2, §15.8);
- the LT-0042 walkthrough with real commands, without assuming seed values, because the generator moves LT-0042 too (sprint A audit F-10). It pauses the generator, reads the current site, then runs `make move`;
- testing: unit, acceptance, reconcile and chaos;
- troubleshooting:
  - memory: the budget, and the FE heap at 1024m (D-044);
  - `vm.max_map_count` and `make host-prep`;
  - slow starts (R10);
  - `docker compose` needs the profile flags for `app` services (sprint D audit F-08); use `make logs SERVICE=…`;
  - switching between `up-core` and `up` needs `make clean`;
  - the adapter's restart loop points at Kafka;
- notes on known deviations: R4 step 1 works (D-040); `use_path_style` (D-037); the FE heap (D-044);
- "How this was built";
- licence (MIT).

**Verification:** check the README against the §17.2 list item by item. Every command in it is one used in step 8.

### 7. Full gate run, exactly as plan §4 writes it
1. Run `make down` first; rebuilds of Go images only are light.
2. Run the gate in order: `make build && make clean && make up`, then `make test lint`, then `make acceptance` (all 38 cases), then `make chaos-on && sleep 180 && make chaos-off && make reconcile`.
3. Also run the sprint 0 checks:
   ```sh
   buf lint && buf build && go vet ./... && go build ./... && go vet -tags acceptance ./acceptance/...
   ```
   plus the count of 38 cases, `go test ./...`, and the locked-file checks:
   ```sh
   git diff --quiet plan-v1 -- specs/system-spec.md specs/sprints-plan.md CLAUDE.md run-sprints.sh
   git diff --quiet sprint-0-done -- acceptance/
   ```
4. Time `make up` from clean, for DoD item 1 (10 minutes or less).

### 8. `docker stats` snapshot and the LT-0042 walkthrough (on the gate's stack)

**Snapshot (§18 item 7).**
- After the gate, let the stack run at the default load (generator at 10/s, chaos off) for about 5 minutes.
- Record `docker stats --no-stream` for all 12 containers, their total against 11.5 GiB, and that each stays under its §15.4 limit.
- Record the FE `memory.peak`, `memory.events`, `RestartCount` and `OOMKilled` after the soak.
- All of it goes in a decision-log entry.

**Walkthrough (§4.3, DoD item 6).** Record each step's observable in the build report:
1. `adlctl generator pause`, then read LT-0042's current site and version (from datacore through `adlctl`, or from the explorer). If it is already in Madrid, first move it to another site.
2. `make move ID=LT-0042 SITE=Madrid` gives version n+1.
3. Observe each step:
   - `triggers` record with key `asset:LT-0042`: `kafka-console-consumer.sh` inside the `kafka` container, with `print.key`, partition and offset;
   - adapter logs;
   - the Dragonfly key: `docker compose exec dragonfly redis-cli GET` and `TTL entity:asset:LT-0042:v<n+1>`;
   - the `entities.assets` record;
   - Flink UI and REST: the job RUNNING and the `VersionGate` accepted counter rising;
   - Doris: `inventory.assets` and `v_assets_enriched` for LT-0042, with a throwaway `mysql:8.4.13` client on network `adl`;
   - the MV row count;
   - `lake.inventory.asset_changes` holding v(n+1);
   - the web: `curl localhost:3000/api/assets/LT-0042` and `/history` through nginx.
4. Resume the generator.

### 9. DoD walk (§18)
Check items 1 to 10 one by one, with evidence in the build report:
- each §4.4 row mapped to its observed mechanism;
- the TODO and FIXME grep, which is empty today;
- `make down && make up` is covered by E04, and `make clean && make up` by the gate.

### 10. Close (plan §3.7)
1. Write `specs/sprints/sprint-E/build-report.md`.
2. **Audit subagent** (general-purpose, not this session's builder context):
   - it reads the spec, the plan, `build-progress.md`, the decision log and the **whole repository**;
   - it re-runs the gate;
   - it writes `specs/sprints/sprint-E/audit-report.md`: findings by severity, and a table of every spec requirement and DoD item.
3. Fix every critical and major finding, then re-run the full gate.
4. Update `build-progress.md` (sprint E done, final state), `decision-log.md` (sprint E entries) and `open-questions.md` (Q-003).
5. Commit with Conventional Commits:
   - `fix(adapter): …`, `fix(adlctl): …`, `docs: write the README`;
   - `docs(sprint-E): plan, build report, decision log`;
   - audit fixes;
   - last, `chore(sprint-E): close sprint`.

   No tag and no push.

## Files likely to change
- `adapter/internal/consumer/consumer.go` and its test (step 3a).
- `adlctl/flink.go` and/or `labkit/flink.go` and their tests (step 3b).
- Only if their symptom appears: `infra/doris/fe.Dockerfile`, `labkit/datacore.go`, `docker-compose.yml`.
- `.github/workflows/e2e.yml` and `ci.yml`, only if actionlint or a tag check finds something.
- `README.md`.
- `specs/sprints/sprint-E/{sprint-plan,build-report,audit-report}.md`, `specs/decision-log.md`, `specs/build-progress.md`, `specs/open-questions.md`.
- Never: `specs/system-spec.md`, `specs/sprints-plan.md`, `CLAUDE.md`, `run-sprints.sh`, `acceptance/`.

## Risks and the rule for each

| Risk | Likelihood | Rule and response |
|---|---|---|
| Doris FE out of memory during the long run (chaos soak, about 40 min of gates) | Medium | R9: measure; apply the next in-budget step (step 4); record before and after |
| Iceberg recovery (E03 TaskManager kill; E04 savepoint restore under a new job id) never exercised with the lake | Low–medium | R7 and G4: diagnose from the logs; uids per §10.10; never `allowNonRestoredState=true` |
| E02 lag stuck by a failed commit | Low–medium | Fixed proactively (step 3a) |
| E04 savepoint skipped while the job is transient | Low | Fixed proactively (step 3b) |
| Old duplicates in `change_rate_1m` fail E03 falsely | Low | Step 1 check; the gate starts from `make clean` anyway |
| A test looks wrong | — | G2: `open-questions.md` with evidence; never edit `acceptance/` |
| Memory pressure from builder containers with the stack up | Low | Plan §3.6: `make down` before heavy rebuilds; `make test lint` with the stack up was proven safe in sprint D |
| CI cannot be verified on GitHub without a push | Certain | G1: verify locally (actionlint, tag checks, the same commands); record Q-003 for the owner |
| The gate is slow (about 45 min) | — | R10: run it in the background and wait; failures count only after their timeouts |

## Verification summary
- The gate of plan §4 for sprint E is green, run in order.
- The sprint 0 checks and the locked-file checks are green.
- New unit tests pass in `make test`.
- Walkthrough evidence, the `docker stats` table, the DoD checklist and the e2e check results are in the build report and decision log.
- The audit report exists, its critical and major findings are fixed, and the gate is green again.
- The working tree is clean and committed.
