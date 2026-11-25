package io.github.cristianrisueo.adl.processor.decode;

/**
 * One record read from an {@code entities.*} topic (spec §10.3).
 *
 * <p>A valid envelope carries the entity type, id, version, {@code changed_at} in milliseconds, the
 * decoded entity fields and the Kafka coordinates. An invalid envelope also keeps the raw key and
 * value, the reason and a message, so the DLQ can reproduce the original record (spec §8.3).
 *
 * <p>This is a Flink POJO: public fields and a public no-argument constructor. It never holds
 * Protobuf objects, so Flink serializes it with its POJO serializer and needs no Kryo setup.
 */
public class Envelope {

  /** Entity type names, as used in the VersionGate key and in {@code change_rate_1m}. */
  public static final String ASSET = "asset";

  public static final String USER = "user";
  public static final String GROUP = "group";

  /** DLQ reasons the processor can produce (spec §8.3). */
  public static final String DECODE_ERROR = "DECODE_ERROR";

  public static final String VALIDATION_ERROR = "VALIDATION_ERROR";

  public boolean valid;

  /** {@link #ASSET}, {@link #USER} or {@link #GROUP}; null when the topic is unknown. */
  public String entityType;

  public String id;
  public long version;
  public long changedAtMillis;

  // Asset fields. kind is LAPTOP, DESKTOP or SERVER; ownerUserId is empty when unassigned.
  public String hostname;
  public String kind;
  public String os;
  public String site;
  public String ownerUserId;

  // User fields. groupId is empty when the user has no group.
  public String displayName;
  public String email;
  public String groupId;

  // Group fields.
  public String name;

  // Kafka coordinates of the source record.
  public String topic;
  public int partition;
  public long offset;

  // Only set on invalid envelopes.
  public byte[] rawKey;
  public byte[] rawValue;
  public String reason;
  public String message;
  public long failedAtMillis;

  public Envelope() {}

  /** The VersionGate key, for example {@code asset:LT-0042} (spec §10.5). */
  public String gateKey() {
    return entityType + ":" + id;
  }
}
