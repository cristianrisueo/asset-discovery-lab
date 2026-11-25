package io.github.cristianrisueo.adl.processor.sink;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;
import io.github.cristianrisueo.adl.processor.decode.Envelope;
import io.github.cristianrisueo.adl.processor.window.ChangeRate;
import java.time.Instant;
import java.time.ZoneOffset;
import java.time.format.DateTimeFormatter;

/**
 * Maps accepted entities and change-rate windows to the JSON rows of the Doris Stream Loads (spec
 * §10.6, Appendix A). Keys are exactly the Doris column names (spec §11.3); datetimes are UTC wall
 * clock; empty optional references become null.
 */
public final class DorisRows {

  /** {@code DATETIME(3)} columns. */
  static final DateTimeFormatter MILLIS =
      DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm:ss.SSS").withZone(ZoneOffset.UTC);

  /** The {@code DATETIME} window bounds of {@code change_rate_1m} (Appendix A, item 7). */
  static final DateTimeFormatter SECONDS =
      DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm:ss").withZone(ZoneOffset.UTC);

  private static final ObjectMapper MAPPER = new ObjectMapper();

  private DorisRows() {}

  /** The {@code inventory.assets}, {@code users} or {@code groups} row of an accepted entity. */
  public static String entity(Envelope e, long ingestedAtMillis) {
    ObjectNode n = MAPPER.createObjectNode();
    n.put("id", e.id);
    switch (e.entityType) {
      case Envelope.ASSET -> {
        n.put("hostname", e.hostname);
        n.put("kind", e.kind);
        n.put("os", e.os);
        n.put("site", e.site);
        putNullable(n, "owner_user_id", e.ownerUserId);
      }
      case Envelope.USER -> {
        n.put("display_name", e.displayName);
        n.put("email", e.email);
        putNullable(n, "group_id", e.groupId);
      }
      case Envelope.GROUP -> n.put("name", e.name);
      default -> throw new IllegalArgumentException("unknown entity type " + e.entityType);
    }
    n.put("version", e.version);
    n.put("changed_at", millis(e.changedAtMillis));
    n.put("ingested_at", millis(ingestedAtMillis));
    return n.toString();
  }

  /** The {@code inventory.change_rate_1m} row of a closed window. */
  public static String changeRate(ChangeRate r) {
    ObjectNode n = MAPPER.createObjectNode();
    n.put("window_start", SECONDS.format(Instant.ofEpochMilli(r.windowStart)));
    n.put("window_end", SECONDS.format(Instant.ofEpochMilli(r.windowEnd)));
    n.put("entity_type", r.entityType);
    n.put("changes", r.changes);
    return n.toString();
  }

  static String millis(long epochMillis) {
    return MILLIS.format(Instant.ofEpochMilli(epochMillis));
  }

  private static void putNullable(ObjectNode n, String field, String value) {
    if (value == null || value.isEmpty()) {
      n.putNull(field);
    } else {
      n.put(field, value);
    }
  }
}
