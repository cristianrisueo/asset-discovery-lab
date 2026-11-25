package io.github.cristianrisueo.adl.processor.decode;

import com.google.protobuf.InvalidProtocolBufferException;
import com.google.protobuf.Timestamp;
import io.github.cristianrisueo.adl.proto.v1.Asset;
import io.github.cristianrisueo.adl.proto.v1.AssetKind;
import io.github.cristianrisueo.adl.proto.v1.Group;
import io.github.cristianrisueo.adl.proto.v1.User;
import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import java.util.regex.Pattern;

/**
 * Decodes and validates one {@code entities.*} record (spec §6.7, §10.3). It never throws: every
 * failure becomes an invalid {@link Envelope}.
 */
public final class EntityDecoder {

  public static final String TOPIC_ASSETS = "entities.assets";
  public static final String TOPIC_USERS = "entities.users";
  public static final String TOPIC_GROUPS = "entities.groups";

  private static final Pattern ASSET_ID = Pattern.compile("^(LT|DT|SV)-\\d{4}$");
  private static final Pattern USER_ID = Pattern.compile("^U-\\d{4}$");
  private static final Pattern GROUP_ID = Pattern.compile("^G-\\d{2}$");

  /** Maximum size of the DLQ {@code error.message} header (spec §8.3). */
  static final int MAX_MESSAGE_BYTES = 1024;

  private EntityDecoder() {}

  /** Decodes the record at the given coordinates. {@code nowMillis} stamps invalid envelopes. */
  public static Envelope decode(
      String topic, int partition, long offset, byte[] key, byte[] value, long nowMillis) {
    Envelope e = new Envelope();
    e.topic = topic;
    e.partition = partition;
    e.offset = offset;
    byte[] bytes = value == null ? new byte[0] : value;
    String keyText = key == null ? null : new String(key, StandardCharsets.UTF_8);
    String problem;
    try {
      switch (topic == null ? "" : topic) {
        case TOPIC_ASSETS -> problem = fillAsset(e, Asset.parseFrom(bytes));
        case TOPIC_USERS -> problem = fillUser(e, User.parseFrom(bytes));
        case TOPIC_GROUPS -> problem = fillGroup(e, Group.parseFrom(bytes));
        default -> {
          return invalid(
              e, key, value, Envelope.DECODE_ERROR, "unexpected topic " + topic, nowMillis);
        }
      }
    } catch (InvalidProtocolBufferException ex) {
      return invalid(e, key, value, Envelope.DECODE_ERROR, "proto: " + ex.getMessage(), nowMillis);
    } catch (RuntimeException ex) {
      return invalid(e, key, value, Envelope.DECODE_ERROR, String.valueOf(ex), nowMillis);
    }
    if (problem == null && !e.id.equals(keyText)) {
      problem = "key " + quote(keyText) + " differs from id " + quote(e.id);
    }
    if (problem != null) {
      return invalid(e, key, value, Envelope.VALIDATION_ERROR, problem, nowMillis);
    }
    e.valid = true;
    return e;
  }

  private static String fillAsset(Envelope e, Asset a) {
    e.entityType = Envelope.ASSET;
    e.id = a.getId();
    e.version = a.getVersion();
    e.hostname = a.getHostname();
    e.os = a.getOs();
    e.site = a.getSite();
    e.ownerUserId = a.getOwnerUserId();
    e.kind = kindName(a.getKind());
    if (!ASSET_ID.matcher(e.id).matches()) {
      return "asset id " + quote(e.id) + " does not match " + ASSET_ID.pattern();
    }
    if (e.hostname.isEmpty()) {
      return "asset hostname is empty";
    }
    if (e.os.isEmpty()) {
      return "asset os is empty";
    }
    if (e.site.isEmpty()) {
      return "asset site is empty";
    }
    if (e.kind == null) {
      return "asset kind " + a.getKindValue() + " is not LAPTOP, DESKTOP or SERVER";
    }
    if (!e.kind.equals(kindForPrefix(e.id))) {
      return "asset kind " + e.kind + " does not match the id prefix of " + e.id;
    }
    if (!e.ownerUserId.isEmpty() && !USER_ID.matcher(e.ownerUserId).matches()) {
      return "asset owner_user_id " + quote(e.ownerUserId) + " does not match " + USER_ID.pattern();
    }
    return common(e, a.getVersion(), a.hasChangedAt(), a.getChangedAt());
  }

  private static String fillUser(Envelope e, User u) {
    e.entityType = Envelope.USER;
    e.id = u.getId();
    e.version = u.getVersion();
    e.displayName = u.getDisplayName();
    e.email = u.getEmail();
    e.groupId = u.getGroupId();
    if (!USER_ID.matcher(e.id).matches()) {
      return "user id " + quote(e.id) + " does not match " + USER_ID.pattern();
    }
    if (e.displayName.isEmpty()) {
      return "user display_name is empty";
    }
    if (e.email.isEmpty()) {
      return "user email is empty";
    }
    if (!e.groupId.isEmpty() && !GROUP_ID.matcher(e.groupId).matches()) {
      return "user group_id " + quote(e.groupId) + " does not match " + GROUP_ID.pattern();
    }
    return common(e, u.getVersion(), u.hasChangedAt(), u.getChangedAt());
  }

  private static String fillGroup(Envelope e, Group g) {
    e.entityType = Envelope.GROUP;
    e.id = g.getId();
    e.version = g.getVersion();
    e.name = g.getName();
    if (!GROUP_ID.matcher(e.id).matches()) {
      return "group id " + quote(e.id) + " does not match " + GROUP_ID.pattern();
    }
    if (e.name.isEmpty()) {
      return "group name is empty";
    }
    return common(e, g.getVersion(), g.hasChangedAt(), g.getChangedAt());
  }

  private static String common(Envelope e, long version, boolean hasChangedAt, Timestamp ts) {
    if (version < 1) {
      return e.entityType + " version " + version + " is below 1";
    }
    if (!hasChangedAt) {
      return e.entityType + " changed_at is not set";
    }
    e.changedAtMillis = ts.getSeconds() * 1000L + ts.getNanos() / 1_000_000;
    return null;
  }

  /** LAPTOP, DESKTOP or SERVER; null for UNSPECIFIED or an unknown value. */
  static String kindName(AssetKind kind) {
    return switch (kind) {
      case ASSET_KIND_LAPTOP -> "LAPTOP";
      case ASSET_KIND_DESKTOP -> "DESKTOP";
      case ASSET_KIND_SERVER -> "SERVER";
      default -> null;
    };
  }

  private static String kindForPrefix(String id) {
    return switch (id.substring(0, 2)) {
      case "LT" -> "LAPTOP";
      case "DT" -> "DESKTOP";
      default -> "SERVER";
    };
  }

  private static Envelope invalid(
      Envelope e, byte[] key, byte[] value, String reason, String message, long nowMillis) {
    e.valid = false;
    e.rawKey = key;
    e.rawValue = value;
    e.reason = reason;
    e.message = truncateUtf8(message, MAX_MESSAGE_BYTES);
    e.failedAtMillis = nowMillis;
    return e;
  }

  /** Cuts s to at most maxBytes of UTF-8 without splitting a character. */
  static String truncateUtf8(String s, int maxBytes) {
    if (s == null) {
      return "";
    }
    byte[] b = s.getBytes(StandardCharsets.UTF_8);
    if (b.length <= maxBytes) {
      return s;
    }
    int end = maxBytes;
    while (end > 0 && (b[end] & 0xC0) == 0x80) {
      end--;
    }
    return new String(Arrays.copyOf(b, end), StandardCharsets.UTF_8);
  }

  private static String quote(String s) {
    return s == null ? "null" : "\"" + s + "\"";
  }
}
