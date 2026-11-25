package io.github.cristianrisueo.adl.processor.lake;

import java.util.Map;
import org.apache.iceberg.PartitionSpec;
import org.apache.iceberg.Schema;
import org.apache.iceberg.catalog.Namespace;
import org.apache.iceberg.catalog.TableIdentifier;
import org.apache.iceberg.types.Types;

/** The definition of the lake table {@code inventory.asset_changes} (spec §12.3). */
public final class LakeTable {

  public static final Namespace NAMESPACE = Namespace.of("inventory");

  public static final TableIdentifier IDENTIFIER = TableIdentifier.of(NAMESPACE, "asset_changes");

  /** Field ids 1 to 9, in this order; only {@code owner_user_id} is optional. */
  public static final Schema SCHEMA =
      new Schema(
          Types.NestedField.required(1, "id", Types.StringType.get()),
          Types.NestedField.required(2, "hostname", Types.StringType.get()),
          Types.NestedField.required(3, "kind", Types.StringType.get()),
          Types.NestedField.required(4, "os", Types.StringType.get()),
          Types.NestedField.required(5, "site", Types.StringType.get()),
          Types.NestedField.optional(6, "owner_user_id", Types.StringType.get()),
          Types.NestedField.required(7, "version", Types.LongType.get()),
          Types.NestedField.required(8, "changed_at", Types.TimestampType.withZone()),
          Types.NestedField.required(9, "ingested_at", Types.TimestampType.withZone()));

  /** Partitioned by {@code day(changed_at)}. */
  public static final PartitionSpec SPEC =
      PartitionSpec.builderFor(SCHEMA).day("changed_at").build();

  public static final Map<String, String> PROPERTIES =
      Map.of(
          "format-version", "2",
          "write.format.default", "parquet",
          "write.parquet.compression-codec", "zstd");

  private LakeTable() {}
}
