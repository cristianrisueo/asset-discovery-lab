package io.github.cristianrisueo.adl.processor;

import java.io.Serializable;
import java.util.regex.Pattern;
import org.apache.flink.util.ParameterTool;

/** The program arguments of the job, with the defaults of spec §10.12. */
public final class JobOptions implements Serializable {

  private static final long serialVersionUID = 1L;

  /** What ParameterTool stores for a key given without a value (its NO_VALUE_KEY is protected). */
  private static final String NO_VALUE = "__NO_VALUE_KEY";

  private static final Pattern RUN_ID = Pattern.compile("^\\d{14}$");

  public String kafkaBootstrap;
  public String kafkaGroup;
  public String dorisFenodes;
  public String dorisBenodes;
  public String dorisUser;
  public String dorisPassword;
  public String dorisDatabase;
  public String labelRunId;
  public boolean icebergEnabled;
  public String icebergUri;
  public String icebergWarehouse;
  public String s3Endpoint;
  public String s3AccessKey;
  public String s3SecretKey;
  public String s3Region;

  /** Parses {@code --key value} arguments. {@code --label-run-id} is required. */
  public static JobOptions parse(String[] args) {
    ParameterTool p = ParameterTool.fromArgs(args);
    JobOptions o = new JobOptions();
    o.kafkaBootstrap = get(p, "kafka.bootstrap", "kafka:9092");
    o.kafkaGroup = get(p, "kafka.group", "adl-processor");
    o.dorisFenodes = get(p, "doris.fenodes", "doris-fe:8030");
    o.dorisBenodes = get(p, "doris.benodes", "");
    o.dorisUser = get(p, "doris.user", "root");
    o.dorisPassword = get(p, "doris.password", "");
    o.dorisDatabase = get(p, "doris.database", "inventory");
    o.labelRunId = get(p, "label-run-id", "");
    o.icebergEnabled = Boolean.parseBoolean(get(p, "iceberg.enabled", "true"));
    o.icebergUri = get(p, "iceberg.uri", "http://iceberg-rest:8181");
    o.icebergWarehouse = get(p, "iceberg.warehouse", "s3://warehouse/");
    o.s3Endpoint = get(p, "s3.endpoint", "http://rustfs:9000");
    o.s3AccessKey = get(p, "s3.access-key", "");
    o.s3SecretKey = get(p, "s3.secret-key", "");
    o.s3Region = get(p, "s3.region", "us-east-1");
    if (!RUN_ID.matcher(o.labelRunId).matches()) {
      throw new IllegalArgumentException(
          "--label-run-id is required and must be a UTC timestamp yyyyMMddHHmmss, got \""
              + o.labelRunId
              + "\" (the flink-job submitter sets it, spec §10.6)");
    }
    return o;
  }

  /** A present key without a value (for example an empty password) reads as the empty string. */
  private static String get(ParameterTool p, String key, String def) {
    if (!p.has(key)) {
      return def;
    }
    String v = p.get(key);
    return v == null || NO_VALUE.equals(v) ? "" : v;
  }
}
