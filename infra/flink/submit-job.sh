#!/usr/bin/env bash
# One-shot submitter of the adl-processor job (spec §10.11), run by the
# flink-job container. It talks to the Flink REST API only.
#
#   1. wait for the JobManager and a TaskManager with free slots;
#   2. exit 0 when the job already runs (idempotent);
#   3. restore from the newest savepoint or retained checkpoint, if any;
#   4. reuse the label run id on restore, or write a new one on a fresh start;
#   5. upload and run the jar;
#   6. wait for RUNNING;
#   7. keep the three newest savepoints.
set -euo pipefail

FLINK_REST="${FLINK_REST:-http://flink-jobmanager:8081}"
FLINK_DATA="${FLINK_DATA:-/flink-data}"
JAR="${JAR:-/opt/adl/processor.jar}"
ENTRY_CLASS="${ENTRY_CLASS:-io.github.cristianrisueo.adl.processor.ProcessorJob}"
JOB_NAME="adl-processor"
WAIT_TIMEOUT_S="${WAIT_TIMEOUT_S:-300}"
KEEP_SAVEPOINTS="${KEEP_SAVEPOINTS:-3}"
RUN_ID_FILE="${FLINK_DATA}/label-run-id"

# JSON log lines with a service field (spec §17.2).
log() {
  jq -nc --arg level "$1" --arg msg "$2" \
    '{time: (now | strftime("%Y-%m-%dT%H:%M:%SZ")), level: $level, service: "flink-job", msg: $msg}'
}
info() { log INFO "$1"; }
fail() { log ERROR "$1"; exit 1; }

rest() { curl -sS --max-time 30 "$@"; }

# --- 1. JobManager and TaskManager ------------------------------------------
deadline=$(( $(date +%s) + WAIT_TIMEOUT_S ))
until rest -f "${FLINK_REST}/overview" > /dev/null 2>&1; do
  [ "$(date +%s)" -lt "$deadline" ] || fail "JobManager REST API not reachable at ${FLINK_REST} within ${WAIT_TIMEOUT_S}s"
  sleep 2
done
until [ "$(rest -f "${FLINK_REST}/taskmanagers" | jq '[.taskmanagers[] | select(.freeSlots > 0)] | length')" -gt 0 ]; do
  [ "$(date +%s)" -lt "$deadline" ] || fail "no TaskManager with free slots within ${WAIT_TIMEOUT_S}s"
  sleep 2
done
info "JobManager and a TaskManager with free slots are available"

# --- 2. Already submitted? --------------------------------------------------
active="$(rest -f "${FLINK_REST}/jobs/overview" | jq -r --arg n "$JOB_NAME" \
  '[.jobs[] | select(.name == $n and (.state == "CREATED" or .state == "RUNNING" or .state == "RESTARTING"))][0] | if . == null then "" else "\(.jid) \(.state)" end')"
if [ -n "$active" ]; then
  info "job ${JOB_NAME} is already present (${active}); nothing to submit"
  exit 0
fi

# --- 3. Restore point: the newest _metadata among savepoints and checkpoints --
restore="$( {
  find "${FLINK_DATA}/savepoints" -mindepth 2 -maxdepth 2 -name _metadata -printf '%T@ %h\n' 2>/dev/null || true
  find "${FLINK_DATA}/checkpoints" -mindepth 3 -maxdepth 3 -path '*/chk-*/_metadata' -printf '%T@ %h\n' 2>/dev/null || true
} | sort -rn | head -n 1 | cut -d' ' -f2-)"

# --- 4. Label run id (spec §10.6) -------------------------------------------
if [ -n "$restore" ]; then
  if [ -s "$RUN_ID_FILE" ]; then
    run_id="$(tr -d '[:space:]' < "$RUN_ID_FILE")"
    info "restoring from ${restore} with label run id ${run_id}"
  else
    run_id="$(date -u +%Y%m%d%H%M%S)"
    printf '%s\n' "$run_id" > "$RUN_ID_FILE"
    log WARN "restore point ${restore} found but ${RUN_ID_FILE} is missing; using a new label run id ${run_id}"
  fi
else
  run_id="$(date -u +%Y%m%d%H%M%S)"
  printf '%s\n' "$run_id" > "$RUN_ID_FILE"
  info "fresh start with new label run id ${run_id}"
fi

# --- 5. Upload and run ------------------------------------------------------
# Program arguments of spec §10.12. Empty values are left out, so the job's
# defaults apply (an empty password stays empty).
args=(
  --kafka.bootstrap "${KAFKA_BOOTSTRAP:-kafka:9092}"
  --kafka.group "${KAFKA_GROUP:-adl-processor}"
  --doris.fenodes "${DORIS_FENODES:-doris-fe:8030}"
  --doris.user "${DORIS_USER:-root}"
  --doris.database "${DORIS_DATABASE:-inventory}"
  --label-run-id "$run_id"
  --iceberg.enabled "${ICEBERG_ENABLED:-true}"
  --iceberg.uri "${ICEBERG_URI:-http://iceberg-rest:8181}"
  --iceberg.warehouse "${ICEBERG_WAREHOUSE:-s3://warehouse/}"
  --s3.endpoint "${S3_ENDPOINT:-http://rustfs:9000}"
  --s3.region "${S3_REGION:-us-east-1}"
)
[ -n "${DORIS_BENODES:-}" ] && args+=(--doris.benodes "$DORIS_BENODES")
[ -n "${DORIS_PASSWORD:-}" ] && args+=(--doris.password "$DORIS_PASSWORD")
[ -n "${S3_ACCESS_KEY:-}" ] && args+=(--s3.access-key "$S3_ACCESS_KEY")
[ -n "${S3_SECRET_KEY:-}" ] && args+=(--s3.secret-key "$S3_SECRET_KEY")

upload="$(rest -H 'Expect:' -X POST -F "jarfile=@${JAR}" "${FLINK_REST}/jars/upload")" \
  || fail "jar upload failed"
jar_id="$(jq -r '.filename // empty' <<< "$upload")"
[ -n "$jar_id" ] || fail "jar upload rejected: ${upload}"
jar_id="${jar_id##*/}"
info "uploaded ${JAR} as ${jar_id}"

# One argument per line (no value contains a newline), then a JSON array.
args_json="$(printf '%s\n' "${args[@]}" | jq -R . | jq -sc .)"
body="$(jq -nc --arg entry "$ENTRY_CLASS" --arg sp "${restore:+file://${restore}}" --argjson args "$args_json" \
  '{entryClass: $entry, programArgsList: $args, allowNonRestoredState: false}
   + (if $sp == "" then {} else {savepointPath: $sp} end)')"
run_resp="$(rest -X POST -H 'Content-Type: application/json' -d "$body" -w '\n%{http_code}' "${FLINK_REST}/jars/${jar_id}/run")" \
  || fail "jar run request failed"
run_code="${run_resp##*$'\n'}"
run_body="${run_resp%$'\n'*}"
rest -X DELETE "${FLINK_REST}/jars/${jar_id}" > /dev/null 2>&1 || true
job_id="$(jq -r '.jobid // empty' <<< "$run_body" 2>/dev/null || true)"
if [ "$run_code" != "200" ] || [ -z "$job_id" ]; then
  fail "job submission failed (HTTP ${run_code}): $(jq -c '.errors // .' <<< "$run_body" 2>/dev/null || echo "$run_body")"
fi
info "submitted job ${job_id}${restore:+ restoring from ${restore}}"

# --- 6. Wait for RUNNING ----------------------------------------------------
deadline=$(( $(date +%s) + WAIT_TIMEOUT_S ))
while :; do
  state="$(rest -f "${FLINK_REST}/jobs/${job_id}" | jq -r '.state' 2>/dev/null || echo UNKNOWN)"
  case "$state" in
    RUNNING) info "job ${job_id} is RUNNING"; break ;;
    FAILED|CANCELED|FINISHED|SUSPENDED)
      log ERROR "job ${job_id} ended in state ${state}; exceptions: $(rest "${FLINK_REST}/jobs/${job_id}/exceptions" | jq -c '.' 2>/dev/null)"
      exit 1 ;;
  esac
  if [ "$(date +%s)" -ge "$deadline" ]; then
    log ERROR "job ${job_id} not RUNNING within ${WAIT_TIMEOUT_S}s (state ${state}); exceptions: $(rest "${FLINK_REST}/jobs/${job_id}/exceptions" | jq -c '.' 2>/dev/null)"
    exit 1
  fi
  sleep 2
done

# --- 7. Keep the newest savepoints ------------------------------------------
find "${FLINK_DATA}/savepoints" -mindepth 1 -maxdepth 1 -type d -printf '%T@ %p\n' 2>/dev/null \
  | sort -rn | tail -n "+$(( KEEP_SAVEPOINTS + 1 ))" | cut -d' ' -f2- \
  | while IFS= read -r old; do
      rm -rf -- "$old" && info "deleted old savepoint ${old}"
    done || true
exit 0
