-- Doris core init (spec §11.3, §11.4, §11.5, §11.7, §11.8). Run by doris-init
-- through infra/scripts/doris-init.sh, which substitutes the placeholders
-- written as an upper-case name in angle brackets.
-- Every statement is idempotent, so re-running make up is safe.

SET GLOBAL time_zone = 'UTC';

CREATE DATABASE IF NOT EXISTS inventory;

CREATE TABLE IF NOT EXISTS inventory.assets (
  id            VARCHAR(16)  NOT NULL,
  hostname      VARCHAR(128) NOT NULL,
  kind          VARCHAR(16)  NOT NULL,
  os            VARCHAR(64)  NOT NULL,
  site          VARCHAR(64)  NOT NULL,
  owner_user_id VARCHAR(16)  NULL,
  version       BIGINT       NOT NULL,
  changed_at    DATETIME(3)  NOT NULL,
  ingested_at   DATETIME(3)  NOT NULL
)
UNIQUE KEY(id)
DISTRIBUTED BY HASH(id) BUCKETS 4
PROPERTIES (
  "replication_num" = "1",
  "enable_unique_key_merge_on_write" = "true",
  "function_column.sequence_col" = "version"
);

CREATE TABLE IF NOT EXISTS inventory.users (
  id           VARCHAR(16)  NOT NULL,
  display_name VARCHAR(256) NOT NULL,
  email        VARCHAR(256) NOT NULL,
  group_id     VARCHAR(16)  NULL,
  version      BIGINT       NOT NULL,
  changed_at   DATETIME(3)  NOT NULL,
  ingested_at  DATETIME(3)  NOT NULL
)
UNIQUE KEY(id)
DISTRIBUTED BY HASH(id) BUCKETS 2
PROPERTIES (
  "replication_num" = "1",
  "enable_unique_key_merge_on_write" = "true",
  "function_column.sequence_col" = "version"
);

CREATE TABLE IF NOT EXISTS inventory.groups (
  id          VARCHAR(16)  NOT NULL,
  name        VARCHAR(256) NOT NULL,
  version     BIGINT       NOT NULL,
  changed_at  DATETIME(3)  NOT NULL,
  ingested_at DATETIME(3)  NOT NULL
)
UNIQUE KEY(id)
DISTRIBUTED BY HASH(id) BUCKETS 1
PROPERTIES (
  "replication_num" = "1",
  "enable_unique_key_merge_on_write" = "true",
  "function_column.sequence_col" = "version"
);

CREATE TABLE IF NOT EXISTS inventory.change_rate_1m (
  window_start DATETIME    NOT NULL,
  entity_type  VARCHAR(16) NOT NULL,
  window_end   DATETIME    NOT NULL,
  changes      BIGINT      NOT NULL
)
DUPLICATE KEY(window_start, entity_type)
DISTRIBUTED BY HASH(window_start) BUCKETS 1
PROPERTIES (
  "replication_num" = "1"
);

CREATE VIEW IF NOT EXISTS inventory.v_assets_enriched AS
SELECT
  a.id,
  a.hostname,
  a.kind,
  a.os,
  a.site,
  a.owner_user_id,
  u.display_name AS owner_name,
  u.group_id,
  g.name AS group_name,
  a.version,
  a.changed_at
FROM inventory.assets a
LEFT JOIN inventory.users u ON a.owner_user_id = u.id
LEFT JOIN inventory.groups g ON u.group_id = g.id;

CREATE MATERIALIZED VIEW IF NOT EXISTS inventory.mv_assets_by_os_site
BUILD IMMEDIATE
REFRESH COMPLETE ON SCHEDULE EVERY 1 MINUTE
DISTRIBUTED BY RANDOM BUCKETS 1
PROPERTIES ("replication_num" = "1")
AS
SELECT os, site, COUNT(*) AS assets
FROM inventory.assets
GROUP BY os, site;

CREATE USER IF NOT EXISTS 'explorer'@'%' IDENTIFIED BY '<DORIS_EXPLORER_PASSWORD>';
GRANT SELECT_PRIV ON internal.inventory.* TO 'explorer'@'%';
