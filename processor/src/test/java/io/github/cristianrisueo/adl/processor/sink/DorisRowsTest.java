package io.github.cristianrisueo.adl.processor.sink;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import io.github.cristianrisueo.adl.processor.decode.Envelope;
import io.github.cristianrisueo.adl.processor.window.ChangeRate;
import java.util.ArrayList;
import java.util.List;
import org.junit.jupiter.api.Test;

class DorisRowsTest {

  private static final ObjectMapper M = new ObjectMapper();
  // 2026-11-25T10:14:03.120Z and 2026-11-25T10:14:04.031Z (Appendix A).
  private static final long CHANGED = 1_795_601_643_120L;
  private static final long INGESTED = 1_795_601_644_031L;

  private static List<String> fields(JsonNode n) {
    List<String> out = new ArrayList<>();
    n.fieldNames().forEachRemaining(out::add);
    return out;
  }

  @Test
  void assetRowMatchesAppendixA() throws Exception {
    Envelope e = new Envelope();
    e.entityType = Envelope.ASSET;
    e.id = "LT-0042";
    e.hostname = "lt-0042.acme.local";
    e.kind = "LAPTOP";
    e.os = "Windows 11";
    e.site = "Madrid";
    e.ownerUserId = "U-0117";
    e.version = 7;
    e.changedAtMillis = CHANGED;
    JsonNode n = M.readTree(DorisRows.entity(e, INGESTED));
    assertEquals(
        List.of(
            "id",
            "hostname",
            "kind",
            "os",
            "site",
            "owner_user_id",
            "version",
            "changed_at",
            "ingested_at"),
        fields(n));
    assertEquals(
        M.readTree(
            "{\"id\": \"LT-0042\", \"hostname\": \"lt-0042.acme.local\", \"kind\": \"LAPTOP\","
                + " \"os\": \"Windows 11\", \"site\": \"Madrid\", \"owner_user_id\": \"U-0117\","
                + " \"version\": 7, \"changed_at\": \"2026-11-25 10:14:03.120\","
                + " \"ingested_at\": \"2026-11-25 10:14:04.031\"}"),
        n);
  }

  @Test
  void emptyOwnerBecomesNull() throws Exception {
    Envelope e = new Envelope();
    e.entityType = Envelope.ASSET;
    e.id = "SV-0001";
    e.hostname = "sv-0001.acme.local";
    e.kind = "SERVER";
    e.os = "Ubuntu 24.04";
    e.site = "Bilbao";
    e.ownerUserId = "";
    e.version = 1;
    e.changedAtMillis = 0L;
    JsonNode n = M.readTree(DorisRows.entity(e, 5L));
    assertTrue(n.get("owner_user_id").isNull());
    assertEquals("1970-01-01 00:00:00.000", n.get("changed_at").asText());
    assertEquals("1970-01-01 00:00:00.005", n.get("ingested_at").asText());
  }

  @Test
  void userAndGroupRows() throws Exception {
    Envelope u = new Envelope();
    u.entityType = Envelope.USER;
    u.id = "U-0117";
    u.displayName = "Ana Ruiz";
    u.email = "u-0117@acme.example";
    u.groupId = "";
    u.version = 4;
    u.changedAtMillis = CHANGED;
    JsonNode un = M.readTree(DorisRows.entity(u, INGESTED));
    assertEquals(
        List.of("id", "display_name", "email", "group_id", "version", "changed_at", "ingested_at"),
        fields(un));
    assertTrue(un.get("group_id").isNull());

    Envelope g = new Envelope();
    g.entityType = Envelope.GROUP;
    g.id = "G-03";
    g.name = "Dirección";
    g.version = 2;
    g.changedAtMillis = CHANGED;
    JsonNode gn = M.readTree(DorisRows.entity(g, INGESTED));
    assertEquals(List.of("id", "name", "version", "changed_at", "ingested_at"), fields(gn));
    assertEquals("Dirección", gn.get("name").asText());
  }

  @Test
  void changeRateRowMatchesAppendixA() throws Exception {
    long start = 1_795_601_640_000L; // 2026-11-25T10:14:00Z
    JsonNode n =
        M.readTree(DorisRows.changeRate(new ChangeRate("asset", start, start + 60_000, 312)));
    assertEquals(
        M.readTree(
            "{\"window_start\": \"2026-11-25 10:14:00\", \"window_end\": \"2026-11-25 10:15:00\","
                + " \"entity_type\": \"asset\", \"changes\": 312}"),
        n);
  }

  @Test
  void labelPrefixAndStreamLoadProperties() {
    assertEquals("adl-20261125090000-assets", DorisSinks.labelPrefix("20261125090000", "assets"));
    assertEquals("json", DorisSinks.streamLoadProperties().getProperty("format"));
    assertEquals("true", DorisSinks.streamLoadProperties().getProperty("read_json_by_line"));
  }
}
