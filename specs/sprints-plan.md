# asset-discovery-lab — Sprints Plan

| | |
|---|---|
| Status | Approved for build |
| Version | 1.0 (2026-11-25) |
| Depends on | `specs/system-spec.md` (the **what**). This document is the **how** and **in which order**. If they disagree, the spec wins (spec §3, G5). |

---

## 1. How the build runs

- **The build is six sprints:** 0, A, B, C, D and E. Each sprint is **one fresh Claude Code session**, launched by `run-sprints.sh` (§5). Sessions never share context; they share only the repository and the files in `specs/`.
- **Every session plans first.** It starts in plan mode, where it explores and writes the sprint plan without touching anything. The script then approves that plan automatically and resumes the same session in auto mode to execute it (§3.2). Repair sessions work the same way.
- **Model and effort:** Opus 5.5 (`claude-opus-5-5`) at effort `high` for every sprint session, and `xhigh` for repair sessions, where something already went wrong. Both are variables of the script (§5).
- **Between sessions, the script checks the sprint's gate (§4)** with plain shell commands. A sprint is done only when its gate is green, the locked files are untouched and the working tree is committed.
- **If a gate fails,** the script launches up to two repair sessions for the same sprint, each with the failure log. If the gate is still red after that, the script stops and leaves everything as it is for the owner.
- **Nobody is present during the build.** The agent decides with the spec's rules (spec §3) and writes down every decision.

---

## 2. Files the build uses

| File | Written by | Purpose |
|---|---|---|
| `specs/system-spec.md` | Owner, before the build | The system. **Locked:** the agent never edits it |
| `specs/sprints-plan.md` | Owner, before the build | This plan. **Locked** |
| `CLAUDE.md`, `run-sprints.sh` | Owner, before the build | Session rules, loaded automatically, and the build script. **Locked** |
| `specs/build-progress.md` | Agent, at the end of every sprint | The external memory between sessions: what is done, what was decided, what is next, and known problems. The first thing every session reads after the spec and this plan. |
| `specs/decision-log.md` | Agent | Every rule applied, fallback taken and "latest at build" pin (spec §3, G3) |
| `specs/open-questions.md` | Agent | Doubts about the spec, or tests that look wrong. The agent writes them down and **keeps working** on everything else. |
| `specs/sprints/sprint-<id>/sprint-plan.md` | Agent, from plan mode | The approved plan of the sprint, saved before executing it |
| `specs/sprints/sprint-<id>/build-report.md` | Agent | What the sprint built, how to verify it, and deviations |
| `specs/sprints/sprint-<id>/audit-report.md` | Audit subagent (§3.7) | Independent review of the sprint |
| `logs/` | `run-sprints.sh` | Session and gate logs. Git-ignored. |

---

## 3. Rules for every session

### 3.1 Reading order

1. `specs/system-spec.md`, complete.
2. This plan, complete.
3. `specs/build-progress.md` and `specs/open-questions.md`, if they exist.
4. The last sprint's `build-report.md` and `audit-report.md`, if they exist.

### 3.2 Plan first, always

Every session, sprint or repair, has two phases in the same Claude Code session:

1. **Plan mode.** Read the files in §3.1, explore the repository, and write a concrete plan for the sprint: steps in order, files to create or change, the spec sections each step implements, how each step will be verified, and the risks with the spec §3 rule that applies to each. Nothing is changed in this phase.
2. **Auto mode.** The script approves the plan without changes and resumes the session. The first action is to save the approved plan, verbatim, to `specs/sprints/sprint-<id>/sprint-plan.md` (repairs append a section `## Repair <n>`). Then execute it.

If execution shows the plan was wrong, adapt, and record the deviation in the build report. The plan guides the work; the gate decides when it is done.

### 3.3 Scope

- Work **only** on the sprint you were given. Do not start the next one, even if there is time left.
- If the sprint finds a bug in an earlier sprint's code, fix it in this sprint and record it in the build report.

### 3.4 Locked files

- After sprint 0, the acceptance suite (`acceptance/`) is **locked**, and the spec and this plan are locked from the start. Never edit them, not even through shell commands.
- `run-sprints.sh` verifies this with `git diff` against the tags `plan-v1` and `sprint-0-done`. A change to a locked file fails the gate, whatever the tests say.
- If a test seems wrong, write it in `specs/open-questions.md` with the evidence and continue with other work. The owner decides after the build.

### 3.5 Tests

- **Unit tests first** for every rule the spec marks as testable in-process (spec §16.2).
- An acceptance test is never weakened, skipped or deleted to get green (spec §3, G2).
- Before ending, run the sprint's gate command yourself (§4) and get it green. The script will run it again.

### 3.6 Environment

- The reference machine has 16 GB of RAM and 12 GB for Docker (spec §1.4, §15.4).
- Run `make build` **before** `make up`, and stop the stack (`make down`) before heavy rebuilds of the Rust or Java images.
- Never raise memory limits above the spec's total budget.
- **The compose file grows sprint by sprint.** It contains only the services already built, in their final profile (spec §15.2). `adlctl wait` checks only the services that are defined.

### 3.7 End of every sprint

1. The gate command is green.
2. Write `specs/sprints/sprint-<id>/build-report.md`.
3. **Audit with a subagent** that did not build the sprint. It reads the spec, this plan and the sprint's diff, re-runs the gate, and writes `audit-report.md`: findings ranked by severity, and whether each spec requirement in scope is met. Fix every finding marked critical or major, then re-run the gate.
4. Update `specs/build-progress.md` and `specs/decision-log.md`.
5. Commit everything with Conventional Commits. The last commit is `chore(sprint-<id>): close sprint`. Do not tag: the script tags.

### 3.8 When stuck

- Apply spec §3: the ladders, G4 (three distinct hypotheses before moving to the next step), and fallbacks recorded in the decision log.
- Never stall waiting for an answer: nobody will answer. Choose, record and continue.

---

## 4. The sprints

Every gate also re-runs the gates of the sprints before it. A sprint can break earlier work, and the script must catch it.

### Sprint 0 · Foundations and the test contract

**Goal:** everything a builder needs before writing a service, and the complete acceptance suite written from spec §16 and **red**.

**Scope:**
- Repository layout (spec §17).
- Contracts and code generation (spec §6).
- Skeleton Makefile with every target from spec §15.9; targets that cannot work yet print `not available until sprint X` and exit 1.
- `.env.example`, `.gitignore`, `.gitattributes` and `docker-compose.yml` with the `adl` network (spec §15.3) and no services yet.
- `labkit` complete: clients for Kafka, Dragonfly, the datacore admin API, Doris (MySQL protocol), S3, the REST catalog, Flink REST and HTTP, plus reconciliation (spec §16.5).
- `adlctl` with every subcommand, failing cleanly when its target is not running.
- **The 38 acceptance cases** of spec §16.4, with the harness rules of §16.3.
- CI `ci.yml` (spec §16.6), with jobs that tolerate components that do not exist yet. `e2e.yml` is written in this sprint and only works from sprint E.

**Rules specific to sprint 0:**
- This sprint writes the tests and nothing that makes them pass. Every acceptance test must fail **for the right reason**: a connection refused or a missing service, never a compile error or a wrong assertion.
- Each test asserts exactly what spec §16.4 says, no less.

**Gate:**
```sh
buf lint && buf build
go vet ./... && go build ./...
go vet -tags acceptance ./acceptance/...
test "$(grep -hoE '^func TestGate[A-E]_[0-9]{2}_' acceptance/*_test.go | wc -l)" -eq 38
go test ./...                    # labkit and adlctl unit tests
```
After the gate, the script tags `sprint-0-done`. From then on `acceptance/` is locked.

### Sprint A · Ingestion

**Goal:** a change in datacore arrives complete on `entities.*`, with the cache working and the DLQ for what fails.

**Scope:**
- Kafka and `kafka-init` (spec §8).
- Dragonfly.
- datacore (spec §7): storage, outbox, seed, generator, chaos, both gRPC services.
- adapter (spec §9).
- Their Dockerfiles and compose services with health checks, in profile `core`.
- `make up-core` works with these services.

**Gate:**
```sh
make build && make clean && make up-core
make test lint
make acceptance-gate GATE=A
```

### Sprint B · Processing

**Goal:** the Flink job writes current state and change counts to Doris exactly once.

**Scope:**
- Doris FE and BE images, configuration, init SQL (core part) and grants (spec §11, without §11.6).
- The Flink cluster image and the processor job (spec §10), with `--iceberg.enabled=false` as the only mode tested in this sprint, though the Iceberg branch MAY already exist.
- The `flink-job` submitter with the restore and run-id logic (spec §10.11), and `make down` with stop-with-savepoint.
- `adlctl wait` checks the job.

**Gate:**
```sh
make build && make clean && make up-core
make test lint
make acceptance-gate GATE=A && make acceptance-gate GATE=B
make reconcile
```

### Sprint C · Lake

**Goal:** every accepted asset version lands in Iceberg on RustFS, and Doris reads it.

**Scope:**
- RustFS, `rustfs-init`, the REST catalog (spec §12), and the Iceberg branch of the job with table creation (spec §10.7).
- The Doris catalog `lake` (spec §11.6, §11.7).
- Profile `lake` and the full `make up`.

**Gate** (the lake must be enabled from a clean state, spec §16.3):
```sh
make build && make clean && make up
make test lint
make acceptance-gate GATE=A && make acceptance-gate GATE=B && make acceptance-gate GATE=C
make reconcile
```

### Sprint D · Serving

**Goal:** the web shows LT-0042, its history and the stats, through the Rust explorer over Arrow Flight SQL.

**Scope:**
- The explorer (spec §13), following the Flight SQL ladder (spec §3 R4) and recording in the decision log which step worked.
- The web (spec §14), and profile `app`.

**Gate:**
```sh
make build && make clean && make up
make test lint
for g in A B C D; do make acceptance-gate GATE=$g || exit 1; done
```

### Sprint E · Failures and close

**Goal:** the system survives chaos and crashes, and the Definition of Done (spec §18) holds.

**Scope:**
- Make gate E pass. Expect fixes in earlier components: that is the point of this sprint.
- Check `e2e.yml` end to end.
- Write the README (spec §17.2).
- Record the `docker stats` snapshot (spec §18, item 7).
- Walk the LT-0042 walkthrough (spec §4.3) by hand with `make move` and record the result in the build report.
- The audit subagent of this sprint audits the **whole repository** against the spec and the Definition of Done, not only the sprint's diff. Its report goes to `specs/sprints/sprint-E/audit-report.md`.

**Gate:**
```sh
make build && make clean && make up
make test lint
make acceptance                  # every gate, A to E
make chaos-on && sleep 180 && make chaos-off && make reconcile
```
After the gate, the script tags `v0.1.0`.

---

## 5. `run-sprints.sh`

The owner places this script at the repository root before the build, next to the two specs. Before the first run, check the Claude Code flags against `claude --help`, because CLI flags change between versions. The script uses `uuidgen`, which ships with macOS.

```bash
#!/usr/bin/env bash
# Runs the asset-discovery-lab build: one fresh Claude Code session per sprint,
# always plan mode first, then the same session resumed in auto mode.
# Usage: ./run-sprints.sh [first-sprint]   e.g. ./run-sprints.sh B to resume
set -uo pipefail

MODEL="${MODEL:-claude-opus-5-5}"
EFFORT="${EFFORT:-high}"                 # sprint sessions
REPAIR_EFFORT="${REPAIR_EFFORT:-xhigh}"  # repair sessions
MAX_REPAIRS="${MAX_REPAIRS:-2}"
SPRINTS=(0 A B C D E)
START="${1:-0}"
mkdir -p logs

# Plan first, then execute in the same session. $1 = task prompt, $2 = log prefix, $3 = effort, $4 = sprint id
plan_then_execute() {
  local sid; sid="$(uuidgen | tr '[:upper:]' '[:lower:]')"
  claude -p "$1

You are in plan mode. Explore and write the complete plan for this work as section 3.2 of specs/sprints-plan.md describes. Do not change anything yet." \
    --session-id "$sid" --model "$MODEL" --effort "$3" --permission-mode plan \
    --output-format stream-json --verbose > "$2-plan.log" 2>&1
  claude -p "The plan is approved without changes. First save it verbatim to specs/sprints/sprint-$4/sprint-plan.md (for a repair, append it as a new section), then execute it until the work described above is complete." \
    --resume "$sid" --model "$MODEL" --effort "$3" --permission-mode auto \
    --output-format stream-json --verbose > "$2-exec.log" 2>&1
}

prompt_for() {  # $1 = sprint id
  cat <<EOF
You are building asset-discovery-lab unattended. Nobody will answer questions: decide with the rules and record your decisions.
Read specs/system-spec.md and specs/sprints-plan.md completely, then specs/build-progress.md and specs/open-questions.md if they exist.
Your job in this session is sprint $1 only, exactly as section 4 of the plan describes it, following every rule in section 3 of the plan and section 3 of the spec.
Finish only when the sprint's gate command is green, the audit subagent has reviewed the sprint, its critical and major findings are fixed, the reports and progress files are updated, and everything is committed.
Do not start the next sprint.
EOF
}

repair_prompt_for() {  # $1 = sprint id, $2 = gate log
  cat <<EOF
$(prompt_for "$1")
The gate of sprint $1 failed when run by the build script. Its full output is in $2.
Diagnose the cause, fix it within the rules (never touch locked files, never weaken a test), re-run the gate until it is green, update the reports and commit.
EOF
}

gate() {  # $1 = sprint id; the commands of section 4 of the plan
  case "$1" in
    0) buf lint && buf build && go vet ./... && go build ./... \
       && go vet -tags acceptance ./acceptance/... \
       && test "$(grep -hoE '^func TestGate[A-E]_[0-9]{2}_' acceptance/*_test.go | wc -l)" -eq 38 \
       && go test ./... ;;
    A) make build && make clean && make up-core && make test lint \
       && make acceptance-gate GATE=A ;;
    B) make build && make clean && make up-core && make test lint \
       && make acceptance-gate GATE=A && make acceptance-gate GATE=B && make reconcile ;;
    C) make build && make clean && make up && make test lint \
       && make acceptance-gate GATE=A && make acceptance-gate GATE=B \
       && make acceptance-gate GATE=C && make reconcile ;;
    D) make build && make clean && make up && make test lint \
       && for g in A B C D; do make acceptance-gate GATE=$g || exit 1; done ;;
    E) make build && make clean && make up && make test lint && make acceptance \
       && make chaos-on && sleep 180 && make chaos-off && make reconcile ;;
  esac
}

locks_intact() {  # $1 = sprint id
  git diff --quiet plan-v1 -- specs/system-spec.md specs/sprints-plan.md CLAUDE.md run-sprints.sh || return 1
  if [ "$1" != "0" ]; then git diff --quiet sprint-0-done -- acceptance/ || return 1; fi
  [ -z "$(git status --porcelain)" ]
}

host_prep() {  # privileged Docker call; run here so auto mode never has to approve it
  docker run --rm --privileged --pid=host alpine:3 \
    nsenter -t 1 -m -u -n -i sh -c 'sysctl -w vm.max_map_count=2000000' > /dev/null
}

started=0
for s in "${SPRINTS[@]}"; do
  [ "$s" = "$START" ] && started=1
  [ "$started" = 1 ] || continue
  host_prep
  echo "=== sprint $s: session (plan, then execute) ==="
  plan_then_execute "$(prompt_for "$s")" "logs/sprint-$s" "$EFFORT" "$s"
  attempt=0
  until ( gate "$s" ) > "logs/gate-$s.log" 2>&1 && locks_intact "$s"; do
    attempt=$((attempt + 1))
    if [ "$attempt" -gt "$MAX_REPAIRS" ]; then
      echo "=== sprint $s: gate still red after $MAX_REPAIRS repairs. Stopping. See logs/gate-$s.log ==="
      exit 1
    fi
    if ! locks_intact "$s"; then echo "Locked files changed or tree not committed." >> "logs/gate-$s.log"; fi
    echo "=== sprint $s: repair $attempt (plan, then execute) ==="
    host_prep
    plan_then_execute "$(repair_prompt_for "$s" "logs/gate-$s.log")" "logs/sprint-$s-repair-$attempt" "$REPAIR_EFFORT" "$s"
  done
  git tag -f "sprint-$s-done"
  echo "=== sprint $s: done ==="
done
git tag -f v0.1.0
echo "=== build complete: v0.1.0 ==="
```

Notes:
- **Plan, then auto.** The plan call uses `--permission-mode plan`, and the execution call resumes the same session (`--resume`) with `--permission-mode auto`. Auto mode's classifier approves routine actions by itself. If it blocks one, the agent finds another way or records it in `open-questions.md`. If the classifier blocks too much in practice, the escape hatch is replacing `auto` with `bypassPermissions` in the execution call.
- **The privileged Docker call** of `make host-prep` runs in the script (`host_prep`), before every session, so the agent never needs it approved.
- **The real lock** is `locks_intact`. It runs outside the session, on git, and the agent cannot bypass it. Run the build in a repository with nothing else in it.
- **Cost:** sprint sessions use `high` and repairs `xhigh`. To give one sprint more effort, resume it with the variable set, for example `EFFORT=xhigh ./run-sprints.sh B`.
- If a locked file was changed, a repair session cannot fix it by editing more. The owner restores it (`git checkout plan-v1 -- <file>`) and resumes with `./run-sprints.sh <sprint>`.

---

## 6. Before the first run (owner checklist)

1. Create an empty repository with `specs/system-spec.md`, `specs/sprints-plan.md`, `CLAUDE.md`, `run-sprints.sh` (executable) and a `.gitignore` containing `logs/`. Commit, then run `git tag plan-v1`.
2. Docker Desktop with 12 GB of memory, Go 1.26, buf 1.75 and make installed. Claude Code logged in. Check with `claude --help` that your version accepts `-p` together with `--permission-mode plan|auto`, `--session-id`, `--resume` and `--effort`.
3. `mkdir -p logs && caffeinate -i nohup ./run-sprints.sh > logs/run.log 2>&1 &` and leave it running. `caffeinate` stops the Mac from sleeping, and `nohup` keeps the build alive if the terminal closes. Keep the Mac plugged in. To resume after a stop: `./run-sprints.sh <sprint>`.

## 7. After the build (owner)

Phase 2 of the method starts here, with the owner present:
1. Read `specs/sprints/sprint-E/audit-report.md`, `specs/open-questions.md` and `specs/decision-log.md`.
2. Bring the stack up and run your own regression.
3. If the result is accepted, keep the `v0.1.0` tag.

Then comes the exploration (`specs/repo-exploration.md`), which leads to CLAUDE.md and `.claude/`, and after that the tickets.
