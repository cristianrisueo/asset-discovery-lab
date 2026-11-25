#!/usr/bin/env bash
# One-shot Doris initialisation (spec §11.8), run by doris-init (and, from
# sprint C, doris-init-lake) in a mysql client container.
#   1. wait until SHOW BACKENDS shows a backend with Alive=true;
#   2. substitute the placeholders of the SQL file (an upper-case NAME in
#      angle brackets) from the environment variable NAME;
#   3. run it, failing on the first error.
# Usage: doris-init.sh <sql-file>
set -euo pipefail

SQL_FILE="${1:?usage: doris-init.sh <sql-file>}"
DORIS_HOST="${DORIS_HOST:-doris-fe}"
DORIS_PORT="${DORIS_PORT:-9030}"
WAIT_TIMEOUT_S="${WAIT_TIMEOUT_S:-600}"

log() {
  printf '{"time":"%s","level":"%s","service":"doris-init","msg":"%s"}\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$1" "$(printf '%s' "$2" | sed 's/\\/\\\\/g; s/"/\\"/g' | tr '\n' ' ')"
}

mysql_cmd=(mysql -h "$DORIS_HOST" -P "$DORIS_PORT" -uroot --connect-timeout=5)

deadline=$(( $(date +%s) + WAIT_TIMEOUT_S ))
# Prints "host alive" per backend, using the Host and Alive columns of the header.
backends() {
  "${mysql_cmd[@]}" -B -e 'SHOW BACKENDS' 2>/dev/null | awk -F'\t' '
    NR == 1 { for (i = 1; i <= NF; i++) { if ($i == "Host") h = i; if ($i == "Alive") a = i } next }
    h && a { print $h, $a }'
}

until backends | grep -q ' true$'; do
  if [ "$(date +%s)" -ge "$deadline" ]; then
    log ERROR "no alive backend in SHOW BACKENDS within ${WAIT_TIMEOUT_S}s"
    exit 1
  fi
  sleep 3
done
log INFO "backend alive: $(backends | tr '\n' ' ')"

# Substitute every <NAME> placeholder from the environment variable NAME.
sql="$(cat "$SQL_FILE")"
for name in $(grep -oE '<[A-Z][A-Z0-9_]*>' "$SQL_FILE" | sort -u | tr -d '<>'); do
  value="${!name:-}"
  [ -n "$value" ] || { log ERROR "placeholder <${name}> has no value in the environment"; exit 1; }
  sql="${sql//<${name}>/${value}}"
done

if ! out="$("${mysql_cmd[@]}" -v <<< "$sql" 2>&1)"; then
  log ERROR "init SQL ${SQL_FILE} failed: ${out}"
  exit 1
fi
log INFO "applied ${SQL_FILE}"
