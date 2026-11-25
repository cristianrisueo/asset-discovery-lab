package io.github.cristianrisueo.adl.processor.lake;

import static org.junit.jupiter.api.Assertions.assertEquals;

import java.util.List;
import java.util.Map;
import org.apache.iceberg.PartitionField;
import org.apache.iceberg.types.Type;
import org.apache.iceberg.types.Types;
import org.junit.jupiter.api.Test;

class LakeTableTest {

  private record Field(int id, String name, Type type, boolean required) {}

  @Test
  void schemaMatchesTheSpec() {
    // Spec §12.3.
    List<Field> want =
        List.of(
            new Field(1, "id", Types.StringType.get(), true),
            new Field(2, "hostname", Types.StringType.get(), true),
            new Field(3, "kind", Types.StringType.get(), true),
            new Field(4, "os", Types.StringType.get(), true),
            new Field(5, "site", Types.StringType.get(), true),
            new Field(6, "owner_user_id", Types.StringType.get(), false),
            new Field(7, "version", Types.LongType.get(), true),
            new Field(8, "changed_at", Types.TimestampType.withZone(), true),
            new Field(9, "ingested_at", Types.TimestampType.withZone(), true));
    List<Types.NestedField> got = LakeTable.SCHEMA.columns();
    assertEquals(want.size(), got.size());
    for (int i = 0; i < want.size(); i++) {
      Field w = want.get(i);
      Types.NestedField f = got.get(i);
      assertEquals(w.id(), f.fieldId(), w.name());
      assertEquals(w.name(), f.name());
      assertEquals(w.type(), f.type(), w.name());
      assertEquals(w.required(), f.isRequired(), w.name());
    }
    assertEquals("timestamptz", Types.TimestampType.withZone().toString());
  }

  @Test
  void partitionedByDayOfChangedAt() {
    List<PartitionField> fields = LakeTable.SPEC.fields();
    assertEquals(1, fields.size());
    assertEquals("day", fields.get(0).transform().toString());
    assertEquals(8, fields.get(0).sourceId());
  }

  @Test
  void propertiesAndIdentifier() {
    assertEquals(
        Map.of(
            "format-version", "2",
            "write.format.default", "parquet",
            "write.parquet.compression-codec", "zstd"),
        LakeTable.PROPERTIES);
    assertEquals("inventory.asset_changes", LakeTable.IDENTIFIER.toString());
  }
}
