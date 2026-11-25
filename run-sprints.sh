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
