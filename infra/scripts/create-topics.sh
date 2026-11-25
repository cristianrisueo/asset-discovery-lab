#!/bin/sh
# Creates every Kafka topic of spec §8.2. Run by the one-shot kafka-init
# container (spec §8.4). It is idempotent: existing topics are left alone.
set -eu

BOOTSTRAP="${BOOTSTRAP:-kafka:9092}"
TOPICS_BIN="${TOPICS_BIN:-/opt/kafka/bin/kafka-topics.sh}"

DAY_MS=86400000

create() { # name partitions config...
  name="$1"
  partitions="$2"
  shift 2
  configs=""
  for c in "$@"; do configs="$configs --config $c"; done
  # shellcheck disable=SC2086 # configs is a list of flags on purpose
  "$TOPICS_BIN" --bootstrap-server "$BOOTSTRAP" --create --if-not-exists \
    --topic "$name" --partitions "$partitions" --replication-factor 1 $configs
  echo "topic ready: $name"
}

create triggers 3 cleanup.policy=delete "retention.ms=$((7 * DAY_MS))"

for t in entities.assets entities.users entities.groups; do
  create "$t" 3 cleanup.policy=compact min.cleanable.dirty.ratio=0.1 segment.ms=3600000
done

for t in dlq.adapter dlq.processor; do
  create "$t" 1 cleanup.policy=delete "retention.ms=$((14 * DAY_MS))"
done

"$TOPICS_BIN" --bootstrap-server "$BOOTSTRAP" --list
