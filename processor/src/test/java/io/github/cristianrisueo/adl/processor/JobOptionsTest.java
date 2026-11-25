package io.github.cristianrisueo.adl.processor;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import org.junit.jupiter.api.Test;

class JobOptionsTest {

  @Test
  void defaultsFollowTheSpec() {
    JobOptions o = JobOptions.parse(new String[] {"--label-run-id", "20261125090000"});
    assertEquals("kafka:9092", o.kafkaBootstrap);
    assertEquals("adl-processor", o.kafkaGroup);
    assertEquals("doris-fe:8030", o.dorisFenodes);
    assertEquals("", o.dorisBenodes);
    assertEquals("root", o.dorisUser);
    assertEquals("", o.dorisPassword);
    assertEquals("inventory", o.dorisDatabase);
    assertTrue(o.icebergEnabled);
    assertEquals("us-east-1", o.s3Region);
  }

  @Test
  void parsesOverridesAndEmptyValues() {
    JobOptions o =
        JobOptions.parse(
            new String[] {
              "--label-run-id",
              "20261125090000",
              "--iceberg.enabled",
              "false",
              "--doris.password",
              "--doris.benodes",
              "172.30.0.3:8040"
            });
    assertFalse(o.icebergEnabled);
    assertEquals("", o.dorisPassword);
    assertEquals("172.30.0.3:8040", o.dorisBenodes);
  }

  @Test
  void labelRunIdIsRequired() {
    assertThrows(IllegalArgumentException.class, () -> JobOptions.parse(new String[] {}));
    assertThrows(
        IllegalArgumentException.class,
        () -> JobOptions.parse(new String[] {"--label-run-id", "abc"}));
  }
}
