package io.github.cristianrisueo.adl.processor.decode;

import org.apache.flink.api.common.typeinfo.TypeInformation;
import org.apache.flink.connector.kafka.source.reader.deserializer.KafkaRecordDeserializationSchema;
import org.apache.flink.util.Collector;
import org.apache.kafka.clients.consumer.ConsumerRecord;

/**
 * Turns every Kafka record of the three {@code entities.*} topics into exactly one {@link
 * Envelope}. It receives the whole record and never throws: bad input becomes an invalid envelope
 * (spec §10.2, §10.3).
 */
public class EntityDeserializer implements KafkaRecordDeserializationSchema<Envelope> {

  private static final long serialVersionUID = 1L;

  @Override
  public void deserialize(ConsumerRecord<byte[], byte[]> record, Collector<Envelope> out) {
    out.collect(
        EntityDecoder.decode(
            record.topic(),
            record.partition(),
            record.offset(),
            record.key(),
            record.value(),
            System.currentTimeMillis()));
  }

  @Override
  public TypeInformation<Envelope> getProducedType() {
    return TypeInformation.of(Envelope.class);
  }
}
