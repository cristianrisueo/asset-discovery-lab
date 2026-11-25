package io.github.cristianrisueo.adl.processor.lake;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;

import io.github.cristianrisueo.adl.processor.JobOptions;
import java.util.Map;
import org.junit.jupiter.api.Test;

class LakeCatalogTest {

  @Test
  void propertiesFollowTheSpec() {
    JobOptions o =
        JobOptions.parse(
            new String[] {
              "--label-run-id", "20261125090000", "--s3.access-key", "ak", "--s3.secret-key", "sk"
            });
    // Spec §10.7, with the defaults of §10.12.
    assertEquals(
        Map.of(
            "uri", "http://iceberg-rest:8181",
            "warehouse", "s3://warehouse/",
            "io-impl", "org.apache.iceberg.aws.s3.S3FileIO",
            "s3.endpoint", "http://rustfs:9000",
            "s3.path-style-access", "true",
            "s3.access-key-id", "ak",
            "s3.secret-access-key", "sk",
            "client.region", "us-east-1"),
        LakeCatalog.properties(o));
  }

  @Test
  void argumentsOverrideTheDefaults() {
    JobOptions o =
        JobOptions.parse(
            new String[] {
              "--label-run-id", "20261125090000",
              "--iceberg.uri", "http://cat:1",
              "--iceberg.warehouse", "s3://w/",
              "--s3.endpoint", "http://s3:2",
              "--s3.region", "eu-west-1",
              "--s3.access-key", "ak",
              "--s3.secret-key", "sk"
            });
    Map<String, String> p = LakeCatalog.properties(o);
    assertEquals("http://cat:1", p.get("uri"));
    assertEquals("s3://w/", p.get("warehouse"));
    assertEquals("http://s3:2", p.get("s3.endpoint"));
    assertEquals("eu-west-1", p.get("client.region"));
  }

  @Test
  void missingCredentialsAreRejected() {
    JobOptions o = JobOptions.parse(new String[] {"--label-run-id", "20261125090000"});
    assertThrows(IllegalArgumentException.class, () -> LakeCatalog.properties(o));
  }
}
