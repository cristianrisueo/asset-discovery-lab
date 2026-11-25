package io.github.cristianrisueo.adl.processor.gate;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.github.cristianrisueo.adl.processor.decode.Envelope;
import java.util.List;
import org.apache.flink.api.common.typeinfo.Types;
import org.apache.flink.runtime.checkpoint.OperatorSubtaskState;
import org.apache.flink.streaming.api.operators.KeyedProcessOperator;
import org.apache.flink.streaming.util.KeyedOneInputStreamOperatorTestHarness;
import org.apache.flink.streaming.util.ProcessFunctionTestHarnesses;
import org.junit.jupiter.api.Test;

class VersionGateTest {

  private static Envelope env(String type, String id, long version) {
    Envelope e = new Envelope();
    e.valid = true;
    e.entityType = type;
    e.id = id;
    e.version = version;
    return e;
  }

  private static KeyedOneInputStreamOperatorTestHarness<String, Envelope, Envelope> harness()
      throws Exception {
    return ProcessFunctionTestHarnesses.forKeyedProcessFunction(
        new VersionGate(), VersionGate.KEY, Types.STRING);
  }

  private static List<Long> versions(
      KeyedOneInputStreamOperatorTestHarness<String, Envelope, Envelope> h) {
    return h.extractOutputValues().stream().map(e -> e.version).toList();
  }

  @Test
  void firstVersionAndNewerVersionsPass() throws Exception {
    try (var h = harness()) {
      h.processElement(env("asset", "LT-0042", 6), 1L);
      h.processElement(env("asset", "LT-0042", 7), 2L);
      h.processElement(env("asset", "LT-0042", 9), 3L);
      assertEquals(List.of(6L, 7L, 9L), versions(h));
    }
  }

  @Test
  void equalAndOlderVersionsAreDropped() throws Exception {
    try (var h = harness()) {
      h.processElement(env("asset", "LT-0042", 7), 1L);
      h.processElement(env("asset", "LT-0042", 7), 2L);
      h.processElement(env("asset", "LT-0042", 6), 3L);
      h.processElement(env("asset", "LT-0042", 1), 4L);
      assertEquals(List.of(7L), versions(h));
    }
  }

  @Test
  void stateIsPerKey() throws Exception {
    try (var h = harness()) {
      h.processElement(env("asset", "LT-0001", 5), 1L);
      h.processElement(env("asset", "LT-0002", 1), 2L);
      h.processElement(env("user", "U-0001", 1), 3L);
      h.processElement(env("asset", "LT-0001", 4), 4L);
      h.processElement(env("asset", "LT-0002", 2), 5L);
      assertEquals(List.of(5L, 1L, 1L, 2L), versions(h));
    }
  }

  @Test
  void stateSurvivesASnapshotAndRestore() throws Exception {
    OperatorSubtaskState snapshot;
    try (var h = harness()) {
      h.processElement(env("group", "G-03", 3), 1L);
      snapshot = h.snapshot(1L, 10L);
    }
    try (var h =
        new KeyedOneInputStreamOperatorTestHarness<>(
            new KeyedProcessOperator<>(new VersionGate()), VersionGate.KEY, Types.STRING)) {
      h.initializeState(snapshot);
      h.open();
      h.processElement(env("group", "G-03", 3), 2L);
      h.processElement(env("group", "G-03", 4), 3L);
      assertEquals(List.of(4L), versions(h));
    }
  }
}
