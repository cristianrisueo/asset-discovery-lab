#!/bin/sh
# Raises vm.max_map_count inside the Docker VM (Docker Desktop) or on the Linux
# host, as the Doris BE needs (spec §3 R3, §15.5). The setting is lost when
# Docker Desktop restarts, so `make up` runs this every time. It is idempotent.
set -eu

ALPINE_IMAGE="${ALPINE_IMAGE:-alpine:3.24.4}"

docker run --rm --privileged --pid=host "$ALPINE_IMAGE" \
  nsenter -t 1 -m -u -n -i sh -c 'sysctl -w vm.max_map_count=2000000'
