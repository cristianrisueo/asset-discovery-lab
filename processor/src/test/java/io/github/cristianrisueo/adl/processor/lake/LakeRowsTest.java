package io.github.cristianrisueo.adl.processor.lake;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import io.github.cristianrisueo.adl.processor.decode.Envelope;
import java.time.Instant;
import java.util.List;
import org.apache.flink.table.data.RowData;
import org.apache.flink.table.types.logical.LocalZonedTimestampType;
import org.apache.flink.table.types.logical.LogicalType;
import org.apache.flink.table.types.logical.LogicalTypeRoot;
import org.apache.flink.table.types.logical.RowType;
import org.junit.jupiter.api.Test;

class LakeRowsTest {

  // 2026-11-25T10:14:03.120Z and 2026-11-25T10:14:04.031Z (Appendix A).
  private static final long CHANGED = 1_795_601_643_120L;
  private static final long INGESTED = 1_795_601_644_031L;

  private static Envelope asset(String owner) {
    Envelope e = new Envelope();
    e.valid = true;
    e.entityType = Envelope.ASSET;
    e.id = "LT-0042";
    e.hostname = "lt-0042.acme.local";
    e.kind = "LAPTOP";
    e.os = "Windows 11";
    e.site = "Madrid";
    e.ownerUserId = owner;
    e.version = 7;
    e.changedAtMillis = CHANGED;
    return e;
  }

  @Test
  void rowTypeFollowsTheTableSchema() {
    RowType t = LakeRows.ROW_TYPE;
    assertEquals(
        List.of(
            "id",
            "hostname",
            "kind",
            "os",
            "site",
            "owner_user_id",
            "version",
            "changed_at",
            "ingested_at"),
        t.getFieldNames());
    for (int i = 0; i < 5; i++) {
      assertEquals(LogicalTypeRoot.VARCHAR, t.getTypeAt(i).getTypeRoot(), t.getFieldNames().get(i));
      assertTrue(!t.getTypeAt(i).isNullable(), t.getFieldNames().get(i) + " is required");
    }
    assertEquals(LogicalTypeRoot.VARCHAR, t.getTypeAt(5).getTypeRoot());
    assertTrue(t.getTypeAt(5).isNullable(), "owner_user_id is optional");
    assertEquals(LogicalTypeRoot.BIGINT, t.getTypeAt(6).getTypeRoot());
    for (int i = 7; i < 9; i++) {
      LogicalType ts = t.getTypeAt(i);
      assertTrue(ts instanceof LocalZonedTimestampType, ts + " is a timestamptz");
      assertTrue(!ts.isNullable());
    }
  }

  @Test
  void assetRowMatchesAppendixA() {
    RowData r = LakeRows.row(asset("U-0117"), INGESTED);
    assertEquals(9, r.getArity());
    assertEquals("LT-0042", r.getString(0).toString());
    assertEquals("lt-0042.acme.local", r.getString(1).toString());
    assertEquals("LAPTOP", r.getString(2).toString());
    assertEquals("Windows 11", r.getString(3).toString());
    assertEquals("Madrid", r.getString(4).toString());
    assertEquals("U-0117", r.getString(5).toString());
    assertEquals(7L, r.getLong(6));
    assertEquals(Instant.parse("2026-11-25T10:14:03.120Z"), r.getTimestamp(7, 6).toInstant());
    assertEquals(Instant.parse("2026-11-25T10:14:04.031Z"), r.getTimestamp(8, 6).toInstant());
  }

  @Test
  void emptyOwnerBecomesNull() {
    assertTrue(LakeRows.row(asset(""), INGESTED).isNullAt(5));
    assertTrue(LakeRows.row(asset(null), INGESTED).isNullAt(5));
    assertNull(
        ((org.apache.flink.table.data.GenericRowData) LakeRows.row(asset(""), 0)).getField(5));
  }

  @Test
  void ingestedAtIsTheMappingTime() throws Exception {
    long before = System.currentTimeMillis();
    RowData r = new LakeRows().map(asset("U-0117"));
    long after = System.currentTimeMillis();
    long ingested = r.getTimestamp(8, 6).getMillisecond();
    assertTrue(ingested >= before && ingested <= after, "ingested_at " + ingested);
  }

  @Test
  void onlyAssetsAreMapped() {
    Envelope u = new Envelope();
    u.entityType = Envelope.USER;
    u.id = "U-0117";
    assertThrows(IllegalArgumentException.class, () -> LakeRows.row(u, INGESTED));
  }
}
