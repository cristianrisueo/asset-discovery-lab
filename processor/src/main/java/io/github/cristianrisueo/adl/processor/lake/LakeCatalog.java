package io.github.cristianrisueo.adl.processor.lake;

import io.github.cristianrisueo.adl.processor.JobOptions;
import java.io.IOException;
import java.io.UncheckedIOException;
import java.util.LinkedHashMap;
import java.util.Map;
import org.apache.hadoop.conf.Configuration;
import org.apache.iceberg.Table;
import org.apache.iceberg.catalog.Catalog;
import org.apache.iceberg.catalog.SupportsNamespaces;
import org.apache.iceberg.exceptions.AlreadyExistsException;
import org.apache.iceberg.flink.CatalogLoader;
import org.apache.iceberg.flink.TableLoader;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

/**
 * The REST catalog {@code lake} of the job (spec §10.7). {@link #ensureTable} runs in {@code
 * main()}, on the JobManager, and creates the namespace and the table when they are missing.
 */
public final class LakeCatalog {

  private static final Logger LOG = LoggerFactory.getLogger(LakeCatalog.class);

  public static final String NAME = "lake";

  private LakeCatalog() {}

  /** The catalog properties of spec §10.7. */
  public static Map<String, String> properties(JobOptions o) {
    if (o.s3AccessKey.isEmpty() || o.s3SecretKey.isEmpty()) {
      throw new IllegalArgumentException(
          "--s3.access-key and --s3.secret-key are required when --iceberg.enabled=true"
              + " (the flink-job submitter passes them from .env, spec §10.12)");
    }
    Map<String, String> p = new LinkedHashMap<>();
    p.put("uri", o.icebergUri);
    p.put("warehouse", o.icebergWarehouse);
    p.put("io-impl", "org.apache.iceberg.aws.s3.S3FileIO");
    p.put("s3.endpoint", o.s3Endpoint);
    p.put("s3.path-style-access", "true");
    p.put("s3.access-key-id", o.s3AccessKey);
    p.put("s3.secret-access-key", o.s3SecretKey);
    p.put("client.region", o.s3Region);
    return p;
  }

  /** A serializable loader of the catalog, used by the job and by the Iceberg sink. */
  public static CatalogLoader loader(JobOptions o) {
    return CatalogLoader.rest(NAME, new Configuration(), properties(o));
  }

  /**
   * Creates the namespace {@code inventory} and the table {@code asset_changes} if they do not
   * exist, and returns a loader of the table. A concurrent creation counts as success.
   */
  public static TableLoader ensureTable(JobOptions o) {
    CatalogLoader loader = loader(o);
    Catalog catalog = loader.loadCatalog();
    try {
      SupportsNamespaces namespaces = (SupportsNamespaces) catalog;
      if (!namespaces.namespaceExists(LakeTable.NAMESPACE)) {
        try {
          namespaces.createNamespace(LakeTable.NAMESPACE);
          LOG.info("created namespace {}.{}", NAME, LakeTable.NAMESPACE);
        } catch (AlreadyExistsException e) {
          LOG.info("namespace {}.{} already exists", NAME, LakeTable.NAMESPACE);
        }
      }
      Table table;
      if (catalog.tableExists(LakeTable.IDENTIFIER)) {
        table = catalog.loadTable(LakeTable.IDENTIFIER);
        LOG.info("table {}.{} exists at {}", NAME, LakeTable.IDENTIFIER, table.location());
      } else {
        try {
          table =
              catalog.createTable(
                  LakeTable.IDENTIFIER, LakeTable.SCHEMA, LakeTable.SPEC, LakeTable.PROPERTIES);
          LOG.info("created table {}.{} at {}", NAME, LakeTable.IDENTIFIER, table.location());
        } catch (AlreadyExistsException e) {
          table = catalog.loadTable(LakeTable.IDENTIFIER);
          LOG.info("table {}.{} was created concurrently", NAME, LakeTable.IDENTIFIER);
        }
      }
    } finally {
      closeQuietly(catalog);
    }
    return TableLoader.fromCatalog(loader, LakeTable.IDENTIFIER);
  }

  private static void closeQuietly(Catalog catalog) {
    if (catalog instanceof AutoCloseable c) {
      try {
        c.close();
      } catch (IOException e) {
        throw new UncheckedIOException(e);
      } catch (Exception e) {
        LOG.warn("closing the catalog failed", e);
      }
    }
  }
}
