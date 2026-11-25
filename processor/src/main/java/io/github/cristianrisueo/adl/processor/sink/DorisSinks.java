package io.github.cristianrisueo.adl.processor.sink;

import io.github.cristianrisueo.adl.processor.JobOptions;
import java.util.Properties;
import org.apache.doris.flink.cfg.DorisExecutionOptions;
import org.apache.doris.flink.cfg.DorisOptions;
import org.apache.doris.flink.cfg.DorisReadOptions;
import org.apache.doris.flink.sink.DorisSink;
import org.apache.doris.flink.sink.writer.serializer.SimpleStringSerializer;

/**
 * Builds the four Doris sinks (spec §10.6): two-phase commit, JSON lines (spec §3 R5) and a label
 * prefix {@code adl-<run-id>-<table>} that is new on every fresh start (spec §3 R14).
 */
public final class DorisSinks {

  public static final String ASSETS = "assets";
  public static final String USERS = "users";
  public static final String GROUPS = "groups";
  public static final String CHANGE_RATE = "change_rate_1m";

  private DorisSinks() {}

  /** The label prefix of a table's Stream Loads. */
  public static String labelPrefix(String runId, String table) {
    return "adl-" + runId + "-" + table;
  }

  /** The Stream Load properties: JSON, one object per line. */
  static Properties streamLoadProperties() {
    Properties p = new Properties();
    p.setProperty("format", "json");
    p.setProperty("read_json_by_line", "true");
    return p;
  }

  /** A sink of JSON rows into {@code <database>.<table>}. */
  public static DorisSink<String> forTable(JobOptions o, String table) {
    DorisOptions.Builder doris =
        DorisOptions.builder()
            .setFenodes(o.dorisFenodes)
            .setTableIdentifier(o.dorisDatabase + "." + table)
            .setUsername(o.dorisUser)
            .setPassword(o.dorisPassword);
    if (!o.dorisBenodes.isEmpty()) {
      // Spec §3 R2: load straight into the BE when the FE redirect cannot be followed.
      doris.setBenodes(o.dorisBenodes).setAutoRedirect(false);
    }
    DorisExecutionOptions execution =
        DorisExecutionOptions.builder()
            .enable2PC()
            .setDeletable(false)
            .setStreamLoadProp(streamLoadProperties())
            .setLabelPrefix(labelPrefix(o.labelRunId, table))
            .build();
    return DorisSink.<String>builder()
        .setDorisReadOptions(DorisReadOptions.builder().build())
        .setDorisExecutionOptions(execution)
        .setDorisOptions(doris.build())
        .setSerializer(new SimpleStringSerializer())
        .build();
  }
}
