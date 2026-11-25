package io.github.cristianrisueo.adl.processor.sink;

import io.github.cristianrisueo.adl.processor.decode.Envelope;
import java.nio.charset.StandardCharsets;
import java.time.Instant;
import java.time.ZoneOffset;
import java.time.format.DateTimeFormatter;
import org.apache.flink.connector.kafka.sink.KafkaRecordSerializationSchema;
import org.apache.kafka.clients.producer.ProducerRecord;
import org.apache.kafka.common.header.internals.RecordHeaders;

/**
 * Writes an invalid envelope to {@code dlq.processor}: the original key and bytes, with the reason
 * and the source coordinates in headers (spec §8.3, §10.4).
 */
public class DlqSerializer implements KafkaRecordSerializationSchema<Envelope> {

  private static final long serialVersionUID = 1L;

  public static final String SERVICE = "processor";

  static final DateTimeFormatter RFC3339_MILLIS =
      DateTimeFormatter.ofPattern("yyyy-MM-dd'T'HH:mm:ss.SSS'Z'").withZone(ZoneOffset.UTC);

  private final String topic;

  public DlqSerializer(String topic) {
    this.topic = topic;
  }

  @Override
  public ProducerRecord<byte[], byte[]> serialize(
      Envelope e, KafkaSinkContext context, Long timestamp) {
    RecordHeaders h = new RecordHeaders();
    add(h, "error.reason", e.reason);
    add(h, "error.message", e.message);
    add(h, "source.topic", e.topic);
    add(h, "source.partition", Integer.toString(e.partition));
    add(h, "source.offset", Long.toString(e.offset));
    add(h, "failed.at", RFC3339_MILLIS.format(Instant.ofEpochMilli(e.failedAtMillis)));
    add(h, "service", SERVICE);
    return new ProducerRecord<>(topic, null, null, e.rawKey, e.rawValue, h);
  }

  private static void add(RecordHeaders h, String name, String value) {
    h.add(name, (value == null ? "" : value).getBytes(StandardCharsets.UTF_8));
  }
}
