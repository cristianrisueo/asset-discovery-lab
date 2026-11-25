package io.github.cristianrisueo.adl.processor;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import java.util.HashMap;
import java.util.Map;
import org.apache.flink.streaming.api.environment.StreamExecutionEnvironment;
import org.apache.flink.streaming.api.graph.StreamGraph;
import org.apache.flink.streaming.api.graph.StreamNode;
import org.junit.jupiter.api.Test;

class ProcessorJobTest {

  @Test
  void operatorsHaveTheNamesAndUidsOfTheSpec() {
    StreamExecutionEnvironment env = StreamExecutionEnvironment.getExecutionEnvironment();
    ProcessorJob.build(env, JobOptions.parse(new String[] {"--label-run-id", "20261125090000"}));
    StreamGraph graph = env.getStreamGraph();
    Map<String, String> uidByName = new HashMap<>();
    for (StreamNode n : graph.getStreamNodes()) {
      uidByName.put(n.getOperatorName(), n.getTransformationUID());
    }
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
    for (Map.Entry<String, String> w : want.entrySet()) {
      boolean found =
          uidByName.entrySet().stream()
              .anyMatch(
                  e ->
                      e.getKey().contains(w.getKey())
                          && e.getValue() != null
                          && e.getValue().equals(w.getValue()));
      assertTrue(found, w.getKey() + " with uid " + w.getValue() + " in " + uidByName);
    }
    assertEquals("version-gate", uidByName.get("VersionGate"));
  }
}
