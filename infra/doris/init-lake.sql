-- Doris lake catalog (spec §11.6) and the explorer's read grant on it (spec §11.7).
-- Applied by doris-init-lake through infra/scripts/doris-init.sh, which fills in
-- the S3 credentials from the environment. Every statement is idempotent.
--
-- Path-style addressing uses "use_path_style" (spec §3 R6, decision D-037):
-- Doris 4.1.5 binds it, or "s3.path-style-access", to the Iceberg FileIO of the
-- catalog; it ignores "s3.use_path_style", and then resolves warehouse.rustfs.

CREATE CATALOG IF NOT EXISTS lake PROPERTIES (
  "type" = "iceberg",
  "iceberg.catalog.type" = "rest",
  "iceberg.rest.uri" = "http://iceberg-rest:8181",
  "warehouse" = "s3://warehouse/",
  "s3.endpoint" = "http://rustfs:9000",
  "s3.region" = "us-east-1",
  "s3.access_key" = "<S3_ACCESS_KEY>",
  "s3.secret_key" = "<S3_SECRET_KEY>",
  "use_path_style" = "true",
  "meta.cache.iceberg.table.ttl-second" = "0"
);

GRANT SELECT_PRIV ON lake.*.* TO 'explorer'@'%';
