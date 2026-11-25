package io.github.cristianrisueo.adl.processor.decode;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.google.protobuf.Timestamp;
import io.github.cristianrisueo.adl.proto.v1.Asset;
import io.github.cristianrisueo.adl.proto.v1.AssetKind;
import io.github.cristianrisueo.adl.proto.v1.Group;
import io.github.cristianrisueo.adl.proto.v1.User;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import org.apache.flink.api.common.functions.util.ListCollector;
import org.apache.kafka.clients.consumer.ConsumerRecord;
import org.junit.jupiter.api.Test;

class EntityDeserializerTest {

  private static final Timestamp TS =
      Timestamp.newBuilder().setSeconds(1_795_601_643L).setNanos(120_000_000).build();

  private static Asset.Builder asset() {
    return Asset.newBuilder()
        .setId("LT-0042")
        .setHostname("lt-0042.acme.local")
        .setKind(AssetKind.ASSET_KIND_LAPTOP)
        .setOs("Windows 11")
        .setSite("Madrid")
        .setOwnerUserId("U-0117")
        .setVersion(7)
        .setChangedAt(TS);
  }

  private static Envelope run(String topic, String key, byte[] value) {
    List<Envelope> out = new ArrayList<>();
    new EntityDeserializer()
        .deserialize(
            new ConsumerRecord<>(
                topic, 2, 18342L, key == null ? null : key.getBytes(StandardCharsets.UTF_8), value),
            new ListCollector<>(out));
    assertEquals(1, out.size(), "exactly one envelope per record");
    return out.get(0);
  }

  private static void assertInvalid(Envelope e, String reason) {
    assertFalse(e.valid, "envelope must be invalid: " + e.message);
    assertEquals(reason, e.reason, e.message);
    assertEquals(2, e.partition);
    assertEquals(18342L, e.offset);
    assertTrue(e.failedAtMillis > 0);
  }

  @Test
  void validAssetDecodes() {
    Envelope e = run("entities.assets", "LT-0042", asset().build().toByteArray());
    assertTrue(e.valid, e.message);
    assertEquals("asset", e.entityType);
    assertEquals("asset:LT-0042", e.gateKey());
    assertEquals(7, e.version);
    assertEquals(1_795_601_643_120L, e.changedAtMillis);
    assertEquals("LAPTOP", e.kind);
    assertEquals("U-0117", e.ownerUserId);
    assertEquals("entities.assets", e.topic);
    assertNull(e.rawValue, "valid envelopes do not carry the raw bytes");
  }

  @Test
  void validUserAndGroupDecode() {
    byte[] user =
        User.newBuilder()
            .setId("U-0117")
            .setDisplayName("Ana Ruiz")
            .setEmail("u-0117@acme.example")
            .setGroupId("G-03")
            .setVersion(4)
            .setChangedAt(TS)
            .build()
            .toByteArray();
    Envelope u = run("entities.users", "U-0117", user);
    assertTrue(u.valid, u.message);
    assertEquals("user:U-0117", u.gateKey());
    assertEquals("Ana Ruiz", u.displayName);

    byte[] group =
        Group.newBuilder()
            .setId("G-03")
            .setName("Ventas")
            .setVersion(2)
            .setChangedAt(TS)
            .build()
            .toByteArray();
    Envelope g = run("entities.groups", "G-03", group);
    assertTrue(g.valid, g.message);
    assertEquals("group:G-03", g.gateKey());
    assertEquals("Ventas", g.name);
  }

  @Test
  void badBytesBecomeDecodeError() {
    byte[] garbage = {(byte) 0xff, (byte) 0xff, (byte) 0xff, 0x01, 0x02, 0x03};
    Envelope e = run("entities.assets", "LT-9103", garbage);
    assertInvalid(e, Envelope.DECODE_ERROR);
    assertEquals("LT-9103", new String(e.rawKey, StandardCharsets.UTF_8));
    assertArrayEquals(garbage, e.rawValue);
  }

  @Test
  void keyMismatchIsValidationError() {
    Envelope e = run("entities.assets", "LT-9106", asset().setId("LT-9105").build().toByteArray());
    assertInvalid(e, Envelope.VALIDATION_ERROR);
  }

  @Test
  void missingKeyIsValidationError() {
    assertInvalid(run("entities.assets", null, asset().build().toByteArray()), "VALIDATION_ERROR");
  }

  @Test
  void ruleBreaksAreValidationErrors() {
    List<Asset> bad =
        List.of(
            asset().setHostname("").build(),
            asset().setOs("").build(),
            asset().setSite("").build(),
            asset().setKind(AssetKind.ASSET_KIND_UNSPECIFIED).build(),
            asset().setKind(AssetKind.ASSET_KIND_SERVER).build(),
            asset().setOwnerUserId("bob").build(),
            asset().setVersion(0).build(),
            asset().clearChangedAt().build(),
            asset().setKindValue(42).build());
    for (Asset a : bad) {
      assertInvalid(run("entities.assets", "LT-0042", a.toByteArray()), Envelope.VALIDATION_ERROR);
    }
    assertInvalid(
        run("entities.assets", "LT-42", asset().setId("LT-42").build().toByteArray()),
        Envelope.VALIDATION_ERROR);
    byte[] badUser =
        User.newBuilder()
            .setId("U-0001")
            .setEmail("x")
            .setVersion(1)
            .setChangedAt(TS)
            .build()
            .toByteArray();
    assertInvalid(run("entities.users", "U-0001", badUser), Envelope.VALIDATION_ERROR);
    byte[] badGroup =
        Group.newBuilder()
            .setId("G-1")
            .setName("x")
            .setVersion(1)
            .setChangedAt(TS)
            .build()
            .toByteArray();
    assertInvalid(run("entities.groups", "G-1", badGroup), Envelope.VALIDATION_ERROR);
  }

  @Test
  void nullValueIsInvalidWithoutThrowing() {
    Envelope e = run("entities.assets", "LT-0042", null);
    assertInvalid(e, Envelope.VALIDATION_ERROR);
    assertNull(e.rawValue);
  }

  @Test
  void emptyOptionalReferencesAreValid() {
    Envelope e =
        run(
            "entities.assets",
            "SV-0001",
            asset()
                .setId("SV-0001")
                .setKind(AssetKind.ASSET_KIND_SERVER)
                .setOwnerUserId("")
                .build()
                .toByteArray());
    assertTrue(e.valid, e.message);
    assertEquals("", e.ownerUserId);
  }

  @Test
  void messagesAreCappedAtOneKilobyte() {
    String longText = "é".repeat(2000);
    String cut = EntityDecoder.truncateUtf8(longText, EntityDecoder.MAX_MESSAGE_BYTES);
    assertTrue(cut.getBytes(StandardCharsets.UTF_8).length <= 1024);
    assertEquals(512, cut.length());
  }
}
