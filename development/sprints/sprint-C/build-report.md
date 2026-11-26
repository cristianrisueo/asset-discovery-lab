# Sprint C build report: Lake

## What was built

| Area | Spec | Files |
|---|---|---|
| RustFS | §12.1, §15.4–§15.8 | `docker-compose.yml` service `rustfs`: `rustfs/rustfs:1.0.6`, ports 9000 and 9001, volume `rustfs-data:/data` (the image owns `/data` as uid 10001), `curl /health` check, 384M, profile `lake` |
| Bucket | §12.1 | `rustfs-init` (`amazon/aws-cli:2.42.4`, one-shot) runs the new `infra/scripts/create-bucket.sh`. It uses path-style addressing, runs `head-bucket` then `create-bucket warehouse`, treats "already owned" as success, and logs JSON lines |
| REST catalog | §12.2, R8 | `iceberg-rest`: `apache/iceberg-rest-fixture:1.10.1`, the §12.2 environment verbatim, volume `iceberg-rest-data:/tmp` (the SQLite catalog persists), `curl /v1/config` check, 512M |
| Doris lake catalog | §11.6–§11.8, R6 | `infra/doris/init-lake.sql`: catalog `lake` and `GRANT SELECT_PRIV ON lake.*.*`. `doris-init-lake` (one-shot, `mysql:8.4.13`) applies it through the existing `infra/scripts/doris-init.sh`, which now takes its log service name from `ADL_SERVICE` |
| Job dependencies | §2.4, R7 | `processor/pom.xml`: `iceberg-flink-runtime-2.1` and `iceberg-aws-bundle` 1.12.0, `hadoop-client-api` and `hadoop-client-runtime` 3.4.3 (all shaded), `flink-table-api-java-bridge` and `flink-table-runtime` 2.1.3 (`provided`). The shaded jar is 222 MiB |
| Iceberg branch | §10.1, §10.7, §10.10, §12.3 | New package `processor/.../lake/`: `LakeTable` (schema, `day(changed_at)`, properties), `LakeCatalog` (§10.7 properties, `ensureTable`), `LakeRows` (RowData mapping). `ProcessorJob`: the D-029 fail-fast is gone; `main()` creates the table on the JobManager; `build(env, o, tableLoader)` adds `OnlyAssetChanges` → `RowDataAssetChanges` → `FlinkSink.forRowData` with uid prefix `iceberg-sink`, write parallelism 1, `append()`, and the sink named `IcebergAssetChanges` |
| `flink-job` dependencies | §15.2 | `iceberg-rest` healthy and `rustfs-init` completed, both `required: false`. `make up-core` still resolves without them |
| Unit tests | §16.2 | Up from 25 to 37 Maven tests: `LakeRowsTest` (row type follows the schema, Appendix A values, empty owner becomes null, `ingested_at` is the mapping time, assets only), `LakeTableTest` (§12.3 schema, partition spec, properties), `LakeCatalogTest` (the §10.7 properties, overrides, missing keys rejected), and `ProcessorJobTest` (the core-only graph has no Iceberg operator; the lake graph built on a local Hadoop table has the `IcebergAssetChanges` sink and the `iceberg-sink` operators) |
| Doris FE memory | §15.4, R9 | `MALLOC_ARENA_MAX=2` on `doris-fe` (D-038) |

No Go or Makefile change was needed:
- `adlctl wait` evaluates the new services (one-shot means `restart: "no"`);
- `adlctl reconcile` compares the lake as soon as an `iceberg-rest` container exists;
- labkit already had the catalog, S3 and lake queries.

## How to verify

```sh
make build && make clean && make up      # ready about 82 s after clean (images built)
make test lint
make acceptance-gate GATE=A && make acceptance-gate GATE=B && make acceptance-gate GATE=C
make reconcile                           # "lake_assets=…" in the PASS line: the lake was compared
```

### Results (first full gate, from `make build && make clean && make up`)

- **`make up`:** `wait: ready` 82 s after `make clean` (21:35:28 → 21:36:50 UTC).
  - The table shows rustfs and iceberg-rest healthy; rustfs-init and doris-init-lake completed; the job RUNNING.
  - The submitter uploaded the 222 MiB jar in 8 s, and `/run` (which creates the table in `main()`) returned in 11 s.
- **`make test lint`:** green. Go tests, buf lint and breaking, golangci-lint, Maven 37/37, Spotless.
- **Gate A:** 10/10 in 19 s.
- **Gate B:** 9/9 in 304 s. B09: p95 10.20 s, max 10.30 s, the same as without the lake.
- **Gate C:** 5/5 in 25 s. C02 (lake visible through Doris) took 13.5 s; later runs took 18–19 s. That is well within the §1.5 freshness target (60 s p95).
- **`make reconcile`:** `PASS (checked assets=3013 groups=10 lake_assets=3013 users=302)`.
- **Sprint 0 checks:** buf lint and build, `go vet`, `go build`, `go vet -tags acceptance`, the 38-case count and `go test ./...` are all green.
- **Catalog:** the table is `s3://warehouse/inventory/asset_changes`, format version 2, spec `changed_at_day` (`day`, source 8), with `write.format.default=parquet` and `write.parquet.compression-codec=zstd`.
- **Live vertices:** `iceberg-sink-IcebergStreamWriter -> iceberg-sink-IcebergFilesCommitter -> IcebergAssetChanges: Writer` at parallelism 1. `OnlyAssetChanges -> RowDataAssetChanges` is chained after `VersionGate` at parallelism 2.

### Lifecycle check (manual, not a gate step; an early look at E04)

1. `make down`: savepoint `savepoint-a52a06-…` written.
2. `make up`: the submitter restored from it with the same label run id; `latest.restored.is_savepoint=true`.
3. `make move ID=LT-0042 SITE=Madrid` (v2 → v3) reached the lake.
4. `make reconcile` with the lake: PASS. Gate C: 5/5, no duplicated (`id`, `version`) pair in the lake, no duplicated (`window_start`, `entity_type`) pair in `change_rate_1m`.

### Memory (`docker stats --no-stream`, full stack at default load, after D-038)

| Container | Usage / limit |
|---|---|
| doris-fe | 1.48 GiB / 1.75 GiB |
| doris-be | 1.62 GiB / 3.25 GiB |
| flink-taskmanager | 1.18 GiB / 2 GiB |
| flink-jobmanager | 642 MiB / 1.25 GiB |
| kafka | 709 MiB / 1 GiB |
| iceberg-rest | 196 MiB / 512 MiB |
| rustfs | 117 MiB / 384 MiB |
| dragonfly | 38 MiB / 512 MiB |
| datacore | 13 MiB / 160 MiB |
| adapter | 14 MiB / 160 MiB |

Before D-038, `doris-fe` was at 1.72 GiB / 1.75 GiB, with 7 `memory.max` events (D-038 has the breakdown).

### After the audit fixes

The audit (`audit-report.md`) found no critical or major issue. Two cheap findings were fixed anyway:
- F-02: a close error in `LakeCatalog` can no longer hide the table-creation error.
- F-06: `doris-init.sh` masks secrets in its error line.

The full gate was then re-run from `make down && make build && make clean && make up`:
- gates A 10/10, B 9/9 (B09 p95 10.25 s), C 5/5;
- `make test lint` green;
- `make reconcile` PASS with the lake compared;
- sprint 0 checks green.

## Deviations from the plan

- **R6 applied (D-037):** Doris 4.1.5 silently ignores `s3.use_path_style`. Its Iceberg FileIO then used virtual-hosted addressing (`UnknownHostException: warehouse.rustfs`). `init-lake.sql` uses `use_path_style`, which R6 names as the fallback. `javap` on the FE jar shows that property binds to Iceberg's `s3.path-style-access`. `meta.cache.iceberg.table.ttl-second` exists in 4.1.5 and is kept. The explorer REFRESH fallback (sprint D) is not needed so far.
- **R9 applied preventively (D-038):** `MALLOC_ARENA_MAX=2` on the FE. This was not in the plan. It came from the memory measurement in plan step 8.
- **Operator names (D-036):** Iceberg names its writer and committer from the uid prefix (`iceberg-sink-IcebergStreamWriter`, `iceberg-sink-IcebergFilesCommitter`). The §10.10 name `IcebergAssetChanges` is on the terminal sink.
- **No other fallback was needed:** R1, R7's HTTP-client override, `IcebergSink`, R8's `latest` tag, R11's relocation, R18 and SeaweedFS all stayed unused. The RustFS and fixture images had curl, and RustFS's `/data` already belongs to uid 10001, so no chown init was needed.
- **`make down` before the build:** the core stack left running by the sprint B gate was stopped with a savepoint first. `make clean` then wiped it, as the gate requires.

## Notes for sprint D

- **Doris FE memory is the main risk.** It sits at about 1.5–1.6 GB of 1.75 GiB with the heap not yet fully touched, and Flight SQL will add direct memory. Watch `docker stats` and `/sys/fs/cgroup/memory.events` in `doris-fe`. On an OOM kill, follow R9 in order and record it.
- **Explorer history:** the history query (§13.4) works over the MySQL protocol today (`lake.inventory.asset_changes`, about 13–19 s behind datacore). The explorer user has `Select_priv` on catalog `lake`.
- **Switching modes needs `make clean`** (§15.2). `make up-core` still works (the optional lake dependencies), but a stack created with `make up-core` must not be continued with `make up`.
- **The lake commits once per 10 s checkpoint**, so small files and many snapshots accumulate (§12.4, expected).
