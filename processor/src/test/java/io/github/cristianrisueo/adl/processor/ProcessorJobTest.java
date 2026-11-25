package io.github.cristianrisueo.adl.processor;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import io.github.cristianrisueo.adl.processor.lake.LakeTable;
import java.nio.file.Path;
import java.util.HashMap;
import java.util.Map;
import org.apache.flink.streaming.api.environment.StreamExecutionEnvironment;
import org.apache.flink.streaming.api.graph.StreamGraph;
import org.apache.flink.streaming.api.graph.StreamNode;
import org.apache.hadoop.conf.Configuration;
import org.apache.iceberg.flink.TableLoader;
import org.apache.iceberg.hadoop.HadoopTables;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

class ProcessorJobTest {

  private static final String[] CORE_ONLY = {
    "--label-run-id", "20261125090000", "--iceberg.enabled", "false"
  };

  /** Operator name to uid, for every node of the stream graph. */
  private static Map<String, String> uidByName(StreamExecutionEnvironment env) {
    StreamGraph graph = env.getStreamGraph();
    Map<String, String> uidByName = new HashMap<>();
    for (StreamNode n : graph.getStreamNodes()) {
      uidByName.put(n.getOperatorName(), n.getTransformationUID());
    }
    return uidByName;
  }

  private static void assertHas(Map<String, String> uidByName, String name, String uid) {
    boolean found =
        uidByName.entrySet().stream()
            .anyMatch(e -> e.getKey().contains(name) && uid.equals(e.getValue()));
    assertTrue(found, name + " with uid " + uid + " in " + uidByName);
  }

  private static void assertCoreOperators(Map<String, String> uidByName) {
    // Spec §10.10. Sources get a "Source: " prefix and Sink V2 operators a ": Writer" suffix.
    Map<String, String> want =
        Map.of(
            "EntitiesSource", "entities-source",
            "DecodeValidate", "decode-validate",
            "Watermarks", "watermarks",
            "VersionGate", "version-gate",
            "ChangeRateWindow", "change-rate-window",
            "DorisAssets", "doris-sink-assets",
            "DorisUsers", "doris-sink-users",
            "DorisGroups", "doris-sink-groups",
            "DorisChangeRate", "doris-sink-change-rate",
            "DlqSink", "dlq-sink");
    want.forEach((name, uid) -> assertHas(uidByName, name, uid));
    assertEquals("version-gate", uidByName.get("VersionGate"));
  }

  private static long icebergOperators(Map<String, String> uidByName) {
    return uidByName.values().stream()
        .filter(uid -> uid != null && uid.startsWith("iceberg-sink"))
        .count();
  }

  @Test
  void coreOnlyHasTheNamesAndUidsOfTheSpecAndNoLakeBranch() {
    StreamExecutionEnvironment env = StreamExecutionEnvironment.getExecutionEnvironment();
    ProcessorJob.build(env, JobOptions.parse(CORE_ONLY), null);
    Map<String, String> uidByName = uidByName(env);
    assertCoreOperators(uidByName);
    assertEquals(0, icebergOperators(uidByName), "no Iceberg operator in " + uidByName);
    assertTrue(uidByName.keySet().stream().noneMatch(n -> n.contains("IcebergAssetChanges")));
  }

  @Test
  void lakeBranchHasTheIcebergSink(@TempDir Path dir) {
    // A local Hadoop table with the lake definition stands in for the REST catalog.
    String location = dir.resolve("asset_changes").toUri().toString();
    new HadoopTables(new Configuration())
        .create(LakeTable.SCHEMA, LakeTable.SPEC, LakeTable.PROPERTIES, location);
    StreamExecutionEnvironment env = StreamExecutionEnvironment.getExecutionEnvironment();
    ProcessorJob.build(
        env,
        JobOptions.parse(new String[] {"--label-run-id", "20261125090000"}),
        TableLoader.fromHadoopTable(location));
    Map<String, String> uidByName = uidByName(env);
    assertCoreOperators(uidByName);
    assertHas(uidByName, "OnlyAssetChanges", "filter-asset-changes");
    assertHas(uidByName, "RowDataAssetChanges", "rowdata-asset-changes");
    assertTrue(
        uidByName.keySet().stream().anyMatch(n -> n.contains("IcebergAssetChanges")),
        "IcebergAssetChanges in " + uidByName);
    // The writer and the committer carry the uid prefix of spec §10.10.
    assertTrue(icebergOperators(uidByName) >= 2, "iceberg-sink operators in " + uidByName);
  }
}
