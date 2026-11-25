#!/usr/bin/env bash
# One-shot creation of the lake bucket (spec §12.1), run by rustfs-init in an
# aws-cli container. Path-style addressing; idempotent: a bucket that already
# exists and is ours counts as success.
set -euo pipefail

S3_ENDPOINT="${S3_ENDPOINT:-http://rustfs:9000}"
BUCKET="${BUCKET:-warehouse}"

log() {
  printf '{"time":"%s","level":"%s","service":"rustfs-init","msg":"%s"}\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$1" "$(printf '%s' "$2" | sed 's/\\/\\\\/g; s/"/\\"/g' | tr '\n' ' ')"
}

aws configure set default.s3.addressing_style path
s3api() { aws --endpoint-url "$S3_ENDPOINT" s3api "$@"; }

if s3api head-bucket --bucket "$BUCKET" > /dev/null 2>&1; then
  log INFO "bucket ${BUCKET} already exists"
  exit 0
fi
if out="$(s3api create-bucket --bucket "$BUCKET" 2>&1)"; then
  log INFO "created bucket ${BUCKET}"
  exit 0
fi
case "$out" in
  *BucketAlreadyOwnedByYou*|*BucketAlreadyExists*)
    log INFO "bucket ${BUCKET} already exists" ;;
  *)
    log ERROR "cannot create bucket ${BUCKET} at ${S3_ENDPOINT}: ${out}"
    exit 1 ;;
esac
