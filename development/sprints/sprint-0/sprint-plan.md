# Sprint 0 plan — Foundations and the test contract

## Context

The repository holds only the locked inputs: `specs/system-spec.md`, `specs/sprints-plan.md`, `CLAUDE.md`, `run-sprints.sh` and `.gitignore`. The `plan-v1` tag exists. There is no `build-progress.md`, no `open-questions.md` and no earlier sprint report. Sprint 0 (plan §4) must deliver:
- the repository skeleton and the contracts;
- `labkit` and `adlctl`;
- a skeleton Makefile, compose file and CI;
- all **38 acceptance cases** of spec §16.4. They must compile and fail only because the stack is not running.

Nothing may make those tests pass. After this sprint the script tags `sprint-0-done`, and `acceptance/` is locked for good.

**Design principle that follows from the lock.** `acceptance/` can never be edited again, so it stays thin and literal to §16.4. Every mechanism that could need a fix later lives in `labkit`, which stays editable: Kafka tailing, compose and make execution, the repository-root lookup, SQL, Flink REST parsing and reconciliation. The labkit **API** that acceptance uses must therefore be stable from day one.

**Environment found** (read-only checks on 2026-11-25):
- Go 1.27.2 local, buf 1.75.0, golangci-lint 2.14.0, Docker 29.9 / Compose v5.7.1 with about 12 GB.
- Network access works: proxy.golang.org, buf.build, GitHub API and Docker Hub all answer.
- `docker compose config` accepts `name: adl` + `services: {}` + the `adl` network.
- Latest versions seen:

| Component | Latest |
|---|---|
| grpc | v1.85.0 |
| franz-go | v1.22.3 |
| kadm | v1.20.0 (separate module) |
| protobuf-go | v1.36.12 |
| protoc-gen-go-grpc | v1.6.2 |
| protobuf release | v36.2 (protobuf-java 4.36.2 is on Maven Central) |
| go-redis | v9.24.0 |
| mysql driver | v1.10.2 |
| aws s3 | v1.116.0 |
| golangci-lint | v2.15.0 |
| Go 1.26 | 1.26.11 |
| alpine | 3.24.4 |

## Step 0 — Save the plan
Save this plan verbatim to `specs/sprints/sprint-0/sprint-plan.md` (plan §3.2).

## Step 1 — Repository scaffolding (spec §15.3, §17.1, §17.2)
Files:
- **`go.mod`**: module `github.com/cristianrisueo/asset-discovery-lab`, `go 1.26.11`, a single module for all Go code.
- **`.gitignore`**: extend it. Keep `logs/` and `.env`, and add `target/ bin/ dist/ node_modules/`, which are already mostly there.
- **`.gitattributes`**: `* text=auto eol=lf`, plus `gen/go/** linguist-generated=true` and `processor/src/generated/** linguist-generated=true`.
- **`.env.example`**: exactly the §17.2 block.
- **`docker-compose.yml`**: `name: adl`, the `adl` network exactly as in §15.3, and `services: {}`. A comment says services are added sprint by sprint (plan §3.6).
- **`LICENSE`**: MIT, holder "Cristian", 2026.
- **`README.md`**: a short stub pointing to the specs. The full README belongs to sprint E.
- **`.golangci.yml`**: v2 format. Default linters plus the gofmt formatter, `run.build-tags: [acceptance]`, and generated code excluded.

Service directories (`datacore/`, `adapter/`, `infra/`, …) are not created: they arrive in their own sprints, and git cannot hold empty dirs.

Verify: `docker compose config` exits 0.

## Step 2 — Contracts and code generation (spec §6)
- Copy `proto/adl/v1/{entities,trigger,datacore,admin}.proto` **verbatim** from §6.2–§6.5.
- Write `buf.yaml` exactly as in §6.6.
- Write `buf.gen.yaml` with remote plugins pinned at:
  - `buf.build/protocolbuffers/go:v1.36.12`;
  - `buf.build/grpc/go:v1.6.2`, or the newest version buf.build actually serves;
  - `buf.build/protocolbuffers/java:v36.2`, which pairs with `protobuf-java 4.36.2` (§6.6 rule). The pairing goes in the decision log for sprint B.
- Run `buf lint`, `buf build` and `buf generate`. Commit `gen/go/adl/v1/*.go` and `processor/src/generated/java/**`.
- `go get` the exact module versions above. Then `go build ./...`.

Verify: `buf lint && buf build` are green, and generated Go compiles. A second `buf generate` followed by `git diff --exit-code` is clean.

## Step 3 — labkit (plan §4 sprint 0 scope; spec §16.1, §16.3, §16.5)
One package, `labkit/`, split into files by client. Each client takes a context, uses short connect timeouts and returns wrapped errors that name the endpoint, for example `datacore admin API at localhost:50051: connection refused`.

- **`config.go`**: `ConfigFromEnv()` with every §16.3 variable and its host default.
  - `S3_ACCESS_KEY`, `S3_SECRET_KEY` and `S3_REGION` default to the `.env.example` values. §16.3 does not list credentials, so this goes in the decision log.
  - `ADL_ALLOW_MYSQL_FALLBACK`.
  - `ADL_REPO_ROOT`. When unset, walk up from the cwd to the directory holding `docker-compose.yml` and `go.mod`.
- **`validate.go`**: §6.7 validation for Trigger, Asset, User and Group, including the key rules (§6.8). It also decodes per topic: `ValidEntityRecord(topic, rec)` decodes and validates, which A01 and the topic scans need (§16.3 test data rule).
- **`kafka.go`** (franz-go + kadm):
  - `Ping`;
  - `EndOffsets(topic)`;
  - `NewTail(topic, from)`, a long-lived direct consumer whose `ReadToEnd(ctx)` reads up to the end offsets captured at call time and accumulates records;
  - `Produce(ctx, topic, key, value, headers)`, synchronous, returning the partition and offset;
  - `GroupLag(group, topic)`;
  - a `Record` type with a header map.
- **`dragonfly.go`** (go-redis): `Ping`, `Get`, `TTL`.
- **`datacore.go`** (gRPC `AdminService` + `DataCoreService`):
  - `ListAll{Assets,Users,Groups}`;
  - `PauseGenerator(ctx) (restore, error)`, `SetGenerator(ctx, cfg) (restore, error)`, `SetChaos(ctx, cfg) (restore, error)`;
  - `ChaosOnConfig`, the §7.9 "make chaos-on" column;
  - helpers to build valid upsert requests for reserved ids (hostname `lower(id)+".acme.local"`, kind from the prefix).
- **`doris.go`** (go-sql-driver; forces `interpolateParams=true` on the DSN so Doris never needs server-side prepare):
  - reads of `assets`, `users` and `groups`, plus `AssetByID`;
  - `EnrichedByID` (`v_assets_enriched`);
  - `MVRows` and `GroupByOSSite`, plus `RefreshMV`;
  - `ChangeRateRows`;
  - `LakeVersions(id)`, `LakeMaxVersions` and `LakeDuplicates`;
  - `Backends` (SHOW BACKENDS: host, Alive).
- **`s3.go`** (aws-sdk-go-v2 s3 + static credentials, path-style): `ListKeys(bucket, prefix)` and `ParseS3URI`.
- **`catalog.go`** (REST catalog over net/http): `LoadTable(ns, table)` returns the status, format version, location, current schema fields (name, type, required, id) and default partition spec fields.
- **`flink.go`** (Flink REST):
  - `JobByName("adl-processor")`, returning jid, state and start-time;
  - `SumOperatorMetric(jid, "VersionGate", "versionGateDropped")`, which scans every vertex's metric ids `N.VersionGate.<metric>` and sums them;
  - `LatestRestored(jid)`, from `/jobs/{id}/checkpoints`: `is_savepoint` and `external_path`;
  - `TaskManagers`;
  - `StopWithSavepoint(jid, dir, drain=false, timeout)`, which POSTs `/stop` and polls `/savepoints/{trigger}` (§10.11).
- **`httpx.go`**: `Get(ctx, url)` returns the status, headers and body. Also `GetJSON`.
- **`compose.go`**: runs `docker compose --project-directory <root> --profile core --profile lake --profile app …`. It offers:
  - `PS`, `Logs(tail, services…)`, `Kill`, `Start`, `Stop`, `Restart`, `Exec`;
  - `Services(profiles)` and `ServiceConfigs` (restart policy, healthcheck) from `config --format json`;
  - `PSJSON`;
  - `Make(ctx, target, vars)`.
- **`poll.go`**: `Poll(ctx, timeout, interval=1s, fn func() (done bool, state string, err error))`. On timeout the error includes the last observed state (§16.3 waiting rule).
- **`stats.go`**: `Percentile(durations, p)`, used by B09.
- **`reconcile.go`** (§16.5):
  - pure `CompareDoris(snapshot, dorisState) Report`, with the normalisation of §16.5 step 2 (empty = NULL, UTC ms timestamps, enum names);
  - categories missing, extra, behind, ahead and field-diff;
  - `CompareLake(assets, lakeMax, dups)`;
  - the `Report` keeps per-category counts and the first 20 diffs;
  - the `Reconcile(ctx, srcs, Options{Timeout, NoPause, NoLake, NoDoris})` orchestrator: pause and restore the generator, then poll every 2 s. `NoDoris` is an internal option for C05 "reconcile the lake"; adlctl does not expose it.

Unit tests (`go test ./...`), table-driven:
- validation (every rule, key mismatches);
- `CompareDoris` and `CompareLake` (each category, normalisation, the 20-diff cap);
- the `Reconcile` loop with fakes (pause and restore, success after N polls, timeout report);
- `Poll` (last state on timeout);
- `Percentile`;
- config defaults and overrides;
- the repo-root walk;
- Flink, catalog and HTTP parsing against `httptest` servers: job lookup, metric summing, stop-with-savepoint success, failure and timeout, restored path;
- parsing of compose ps and config JSON;
- `GroupLag` arithmetic as a pure function;
- S3 URI parsing;
- unreachable endpoints fail fast with a clear error. This uses a closed localhost port and a bounded ctx.

## Step 4 — adlctl (spec §15.10)
`adlctl/` is package main and uses only labkit. It has `main.go` (dispatch with the standard `flag` package, no new dependency) and one file per command group: `wait.go`, `control.go` (generator, chaos, burst, move), `reconcile.go`, `flink.go`, `stats.go`. Tests sit next to them.
- **`wait [--core] [--timeout 15m]`**:
  - Read the defined services of the active profiles from compose config. Plan §3.6 says to check only those.
  - Long-running services must be healthy, or running when they have no healthcheck. One-shot services (no restart policy, or `no`) must have exited 0.
  - If `doris-be` is defined, it must be Alive in SHOW BACKENDS. If `flink-jobmanager` is defined, the job `adl-processor` must be RUNNING.
  - Print a status table and poll until ready or until the timeout.
  - With zero services it prints "no services defined" and exits 0. A Docker error exits 1.
- **`generator pause|resume|rate N`**: Get, then Set.
- **`chaos on|off|show`**: `on` uses `ChaosOnConfig`.
- **`burst N`**.
- **`move ID SITE`** reads the asset through `AdminService.ListAssets`, filtered by id, rather than `GetAsset`. Chaos does not apply to the admin API, so no stale or UNAVAILABLE reads; decision log. It then upserts with the new site.
- **`reconcile [--timeout 3m] [--no-pause] [--no-lake]`**: prints the report and exits 0 or 1.
- **`flink stop-with-savepoint [--timeout 3m]`**: with no running job, prints a message and exits 0. If Flink is unreachable, it prints a clean error and exits 1; `make down` warns and continues (§10.11 "warn and stop anyway").
- **`stats`**:
  - datacore stats;
  - lag of `adl-adapter` on `triggers` and of `adl-processor` on `entities.*`;
  - Flink job state;
  - Doris row counts.
  - Each section prints its own error, and the command exits 1 if any section failed.
- **Errors**: every command exits 1 with a one-line `adlctl: <target> at <addr> is not reachable: <err>` and never panics. Connection attempts are bounded (about 5 s) so nothing hangs.

Unit tests:
- argument parsing and usage errors;
- the chaos on/off configurations;
- wait status evaluation from canned ps and config JSON (healthy, unhealthy, exited 0, exited 1, missing);
- each command against unreachable endpoints returns exit 1 with the target named.

## Step 5 — Makefile (spec §15.9) and host-prep (§15.5)
All targets from §15.9, with `help` as the default. The tools are invoked as pinned `go run` commands, because spec §15.1 lists only Go, make and git as host prerequisites:
- `BUF ?= go run github.com/bufbuild/buf/cmd/buf@v1.75.0`
- `GOLANGCI_LINT ?= go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0`
- `ADLCTL := go run ./adlctl`

These go in the decision log.

| Target | Sprint 0 behaviour |
|---|---|
| `help` | Works |
| `env` | Works |
| `host-prep` | Works. Inline `docker run … alpine:3.24.4 nsenter …`: a separate script would make `infra/scripts/` a single-file directory |
| `generate` | Works |
| `test` | Works: `go test ./...`. Maven, Cargo and npm run in builder containers **only if** `processor/pom.xml`, `explorer/Cargo.toml` or `web/package.json` exist |
| `lint` | Works: buf lint, and buf breaking against `main` when it exists and HEAD is not `main`; golangci-lint; Spotless, cargo and npm parts only when present |
| `acceptance`, `acceptance-gate GATE=X` | Work exactly as §15.9 says, and are red |
| `reconcile`, `chaos-on`, `chaos-off`, `burst N=`, `move ID= SITE=` | Work, and fail cleanly through adlctl |
| `ps`, `logs` | Work |
| `clean` | Works |
| `down` | Works: adlctl stop-with-savepoint or a warning, then compose stop |
| `build`, `up`, `up-core` | Print `not available until sprint A` and exit 1 |

Verify: `make help`, `make env`, `make generate`, `make test` and `make lint` are green. `make build` exits 1 with the message.

## Step 6 — Acceptance suite (spec §16.3, §16.4)
Every file under `acceptance/` except `doc.go` carries `//go:build acceptance`. `doc.go` is untagged, so `go build ./...` and `go test ./...` see a valid empty package.

- **`harness_test.go`**:
  - the timeout constants `TShort` 30 s, `TPipe` 90 s, `TLake` 120 s, `TReconcile` 180 s and `TRecover` 300 s, defined once;
  - `newHarness(t, services...)`, which builds the labkit clients from env;
  - a precheck that dials each required endpoint once (5 s timeout) and fails with `t.Fatalf("<service> not reachable at <addr>: <err> (is the stack up?)")`. This makes every case fail for the right reason in sprint 0. It is a fail, never a skip (G2);
  - a `t.Cleanup` that, on `t.Failed()`, prints `docker compose ps` and `logs --tail 200` of the services involved;
  - the helpers `pauseGenerator`, `setGenerator` and `setChaos`, each restoring in `t.Cleanup`;
  - `eventually(t, timeout, fn)`, a wrapper over `labkit.Poll` (1 s);
  - `notHappening(t, 30s)`, which waits 30 s; per §16.3 it is the only allowed sleep.
- **`gate_a_test.go` … `gate_e_test.go`**: 10 + 9 + 5 + 8 + 6 = **38** functions named `TestGate<X>_<NN>_<Name>`, with the names taken from §16.4 (for example `TestGateA_01_SeedPublished`). Alphabetical file order gives the A→E run order.
- **Assertions**: each case asserts what its §16.4 row says.
  - Topic checks open a `Tail` at offsets snapshotted before the action, and count only records that pass §6.7 where they scan.
  - Each test touches only its own reserved ids: LT-90xx for A, LT-91xx for B, and so on (§5.4, §16.3).
- **Interpretations** (all recorded in the decision log under G1):
  - **A05**: re-produce the **exact bytes** of the original trigger read from `triggers`, twice. Pass when at least 2 new LT-9003 v records appear.
  - **A06**: also checks that `source.partition` and `source.offset` equal the produced coordinates.
  - **B05**: unspecified timeout, so `TPipe`.
  - **B07 and D05**: issue a manual refresh, log a refresh error without failing (it tolerates the R13 view fallback), then poll for 150 s.
  - **B08**: "stack started" means the Flink job's start-time; the test also guarantees 3 minutes of generator activity.
  - **B09**: p95 is the 19th of 20 sorted latencies; each wait is bounded by `TPipe`.
  - **C05**: uses `NoDoris`.
  - **E01**: DLQ windows run from the offsets at the soak start to the end at check time. Phantom keys match `^asset:LT-8\d{3}$`.
  - **E04**: lists `/flink-data/savepoints` through `compose exec` before `make down`. After `make up`, it requires `restored.is_savepoint` and a path not in that list.
  - **E05**: starts Dragonfly again in `t.Cleanup` as well.
  - **E06**: `readyz` within `TPipe`.

Verify:
- `go vet -tags acceptance ./acceptance/...` is green.
- The grep count is 38.
- `golangci-lint` covers the acceptance tag.
- With no stack: `go test -tags acceptance -count=1 -run '^TestGateA_02_|^TestGateB_01_|^TestGateC_01_|^TestGateD_06_|^TestGateE_05_' ./acceptance/...` fails fast, and each failure names an unreachable service ("connection refused"). No compile error, no assertion failure, no hang. Then `make acceptance-gate GATE=A` to see every A case fail the same way.

## Step 7 — CI (spec §16.6)
- **`.github/workflows/ci.yml`**, on push and pull_request:
  - a `detect` job outputs which components exist (`processor/pom.xml`, `explorer/Cargo.toml`, `web/package.json`, the Dockerfiles list);
  - `contracts`: buf-action or buf v1.75.0; `buf lint`; `buf breaking` against main, or against `HEAD~1` on main, skipped on the first commit; `buf generate` followed by `git diff --exit-code`;
  - `go`: setup-go 1.26.11, `go vet` (both tags), golangci-lint-action pinned to v2.15.0, `go test -race ./...`;
  - `java`, `rust`, `web` and `images`, each gated on `detect` outputs, so missing components are tolerated;
  - actions pinned to exact release tags: checkout v7.0.1, setup-go v7.1.0, setup-java v6.3.0, setup-node v7.1.0, upload-artifact v7.0.2, golangci-lint-action v9.3.0, buf-action v1.7.0, setup-buildx v4.4.1, build-push v7.4.0. Rust uses `rustup` with `rust-toolchain.toml`.
- **`.github/workflows/e2e.yml`**, on push to main and workflow_dispatch: free disk space, then setup-go, `make build`, `make up`, `make acceptance`. On failure it uploads `docker compose logs` as an artifact, and it always runs `make clean`. A comment states it works only from sprint E.

Verify: both files parse as YAML (`python3 -c 'import yaml…'`, or `docker run` actionlint pinned if one is available).

## Step 8 — Gate, audit and close (plan §3.5, §3.7)
1. Run the exact gate: `buf lint && buf build && go vet ./... && go build ./... && go vet -tags acceptance ./acceptance/... && test $(grep …) -eq 38 && go test ./...`. Also run `make lint` and `make test`.
2. Write `specs/sprints/sprint-0/build-report.md`: what was built, how to verify, deviations, and the red-test evidence.
3. **Audit subagent** (general-purpose, fresh context). It reads the spec, the plan and `git diff plan-v1`, then:
   - re-runs the gate;
   - checks each of the 38 cases against its §16.4 row for under- or over-assertion and wrong failure reasons;
   - checks §6, §15.9, §15.10, §16.3, §16.5 and §16.6 coverage;
   - writes `specs/sprints/sprint-0/audit-report.md` with findings ranked by severity.
   
   I fix every critical and major finding, especially in `acceptance/`, because it locks after this sprint, and then re-run the gate.
4. Write `specs/decision-log.md` (G3 format: date, rule, symptom, evidence, decision, diagram impact), with every pin and interpretation above. Also write `specs/open-questions.md` and `specs/build-progress.md`: done, decisions, next is sprint A, known issues.
5. Commit in logical Conventional Commits: `chore: scaffold repository`, `feat(proto): …`, `feat(labkit): …`, `feat(adlctl): …`, `test(acceptance): …`, `ci: …`, `docs: …`. The last is `chore(sprint-0): close sprint`. No tag. The tree is clean at the end, and the locked files are untouched (`git diff --quiet plan-v1 -- specs/system-spec.md specs/sprints-plan.md CLAUDE.md run-sprints.sh`).

## Risks and the rule for each

| Risk | Rule |
|---|---|
| buf remote plugins fail (auth, rate limit, version missing) | G4 with three hypotheses (version tag, network/auth, plugin name), then R16: local `protoc-gen-go` and `protoc-gen-go-grpc` at pinned versions via `go install`, and protoc's Java generator at the pinned protobuf version. Update CI to match and log it |
| An acceptance test fails for the wrong reason (compile, assertion, hang) in sprint 0 | Plan §4 sprint 0 rule: the precheck in `newHarness` plus bounded contexts. Verified by running a sample with no stack |
| A spec row is ambiguous | G1 (keep §16 meaningful), then the decision log. A test that looks wrong goes to `open-questions.md` (G2), and work continues |
| A spec/plan mismatch: kadm is versioned separately from franz-go (spec §2.5 lists v1.22.3 "with pkg/kadm"); local Go is 1.27.2 vs spec 1.26 | G1 and G5: kadm at its latest v1.20.0 (the module franz-go v1.22.3 pairs with); `go 1.26.11` directive, built fine by 1.27. Both go in the decision log |
| Acceptance locks after this sprint | Keep logic in labkit; audit focus on `acceptance/` before closing |
| Single-file directories (CLAUDE.md, §17.1) | Host-prep inline in the Makefile; no `infra/` yet; `processor/src/generated/java` is allowed (Java nesting) |
| Locked files | Never touched. `.gitignore` is not locked and is only extended |
