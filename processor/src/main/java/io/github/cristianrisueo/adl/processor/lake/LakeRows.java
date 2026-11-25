package io.github.cristianrisueo.adl.processor.lake;

import io.github.cristianrisueo.adl.processor.decode.Envelope;
import org.apache.flink.api.common.functions.MapFunction;
import org.apache.flink.api.common.typeinfo.TypeInformation;
import org.apache.flink.table.data.GenericRowData;
import org.apache.flink.table.data.RowData;
import org.apache.flink.table.data.StringData;
import org.apache.flink.table.data.TimestampData;
import org.apache.flink.table.runtime.typeutils.InternalTypeInfo;
import org.apache.flink.table.types.logical.RowType;
import org.apache.iceberg.flink.FlinkSchemaUtil;

/**
 * Maps an accepted asset to its {@code asset_changes} row (spec §10.7, §12.3). It follows the Doris
 * rows: an empty {@code owner_user_id} becomes null; {@code changed_at} and {@code ingested_at}
 * become timestamptz values, and {@code ingested_at} is the mapping time.
 */
public final class LakeRows implements MapFunction<Envelope, RowData> {

  private static final long serialVersionUID = 1L;

  /** The Flink row type, derived from the table schema so the two cannot diverge. */
  public static final RowType ROW_TYPE = FlinkSchemaUtil.convert(LakeTable.SCHEMA);

  public static final TypeInformation<RowData> TYPE_INFO = InternalTypeInfo.of(ROW_TYPE);

  @Override
  public RowData map(Envelope e) {
    return row(e, System.currentTimeMillis());
  }

  /** The row of an accepted asset, in schema order. */
  public static RowData row(Envelope e, long ingestedAtMillis) {
    if (!Envelope.ASSET.equals(e.entityType)) {
      throw new IllegalArgumentException("only assets go to the lake, got " + e.entityType);
    }
    GenericRowData r = new GenericRowData(9);
    r.setField(0, StringData.fromString(e.id));
    r.setField(1, StringData.fromString(e.hostname));
    r.setField(2, StringData.fromString(e.kind));
    r.setField(3, StringData.fromString(e.os));
    r.setField(4, StringData.fromString(e.site));
    r.setField(
        5,
        e.ownerUserId == null || e.ownerUserId.isEmpty()
            ? null
            : StringData.fromString(e.ownerUserId));
    r.setField(6, e.version);
    r.setField(7, TimestampData.fromEpochMillis(e.changedAtMillis));
    r.setField(8, TimestampData.fromEpochMillis(ingestedAtMillis));
    return r;
  }
}
