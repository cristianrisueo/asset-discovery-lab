package io.github.cristianrisueo.adl.processor.sink;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;

import io.github.cristianrisueo.adl.processor.decode.Envelope;
import java.nio.charset.StandardCharsets;
import java.util.HashMap;
import java.util.Map;
import org.apache.kafka.clients.producer.ProducerRecord;
import org.apache.kafka.common.header.Header;
import org.junit.jupiter.api.Test;

class DlqSerializerTest {

  @Test
  void keepsTheOriginalBytesAndAddsTheHeaders() {
    Envelope e = new Envelope();
    e.topic = "entities.assets";
    e.partition = 2;
    e.offset = 18342L;
    e.rawKey = "LT-9103".getBytes(StandardCharsets.UTF_8);
    e.rawValue = new byte[] {1, 2, 3, (byte) 0xff};
    e.reason = Envelope.DECODE_ERROR;
    e.message = "proto: cannot parse";
    e.failedAtMillis = 1_795_602_011_004L; // 2026-11-25T10:20:11.004Z

    ProducerRecord<byte[], byte[]> r = new DlqSerializer("dlq.processor").serialize(e, null, null);

    assertEquals("dlq.processor", r.topic());
    assertArrayEquals(e.rawKey, r.key());
    assertArrayEquals(e.rawValue, r.value());
    Map<String, String> h = new HashMap<>();
    for (Header header : r.headers()) {
      h.put(header.key(), new String(header.value(), StandardCharsets.UTF_8));
    }
    assertEquals(
        Map.of(
            "error.reason", "DECODE_ERROR",
            "error.message", "proto: cannot parse",
            "source.topic", "entities.assets",
            "source.partition", "2",
            "source.offset", "18342",
            "failed.at", "2026-11-25T10:20:11.004Z",
            "service", "processor"),
        h);
  }
}
