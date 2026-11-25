package io.github.cristianrisueo.adl.processor;

import io.github.cristianrisueo.adl.processor.decode.DecodeValidate;
import io.github.cristianrisueo.adl.processor.decode.EntityDecoder;
import io.github.cristianrisueo.adl.processor.decode.EntityDeserializer;
import io.github.cristianrisueo.adl.processor.decode.Envelope;
import io.github.cristianrisueo.adl.processor.gate.VersionGate;
import io.github.cristianrisueo.adl.processor.sink.DlqSerializer;
import io.github.cristianrisueo.adl.processor.sink.DorisRows;
import io.github.cristianrisueo.adl.processor.sink.DorisSinks;
import io.github.cristianrisueo.adl.processor.window.ChangeRate;
import io.github.cristianrisueo.adl.processor.window.ChangeRateWindow;
import java.time.Duration;
import java.util.List;
import org.apache.flink.api.common.eventtime.WatermarkStrategy;
import org.apache.flink.api.common.functions.FilterFunction;
import org.apache.flink.api.common.functions.MapFunction;
import org.apache.flink.connector.base.DeliveryGuarantee;
import org.apache.flink.connector.kafka.sink.KafkaSink;
import org.apache.flink.connector.kafka.source.KafkaSource;
import org.apache.flink.connector.kafka.source.enumerator.initializer.OffsetsInitializer;
import org.apache.flink.streaming.api.datastream.DataStream;
import org.apache.flink.streaming.api.datastream.SingleOutputStreamOperator;
import org.apache.flink.streaming.api.environment.StreamExecutionEnvironment;
import org.apache.kafka.clients.consumer.OffsetResetStrategy;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

/**
 * The adl-processor Flink job (spec §10.1). It reads the three {@code entities.*} topics, sends
 * malformed records to {@code dlq.processor}, drops stale and duplicate versions, and writes the
 * current state and the per-minute change counts to Doris with two-phase commit.
 *
 * <p>Checkpointing, the state backend and the restart strategy come from the cluster configuration
 * (spec §10.9); the job does not override them.
 */
public final class ProcessorJob {

  private static final Logger LOG = LoggerFactory.getLogger(ProcessorJob.class);

  /** The job name the submitter and the tests look for (spec §10.1). */
  public static final String JOB_NAME = "adl-processor";

  public static final List<String> TOPICS =
      List.of(EntityDecoder.TOPIC_ASSETS, EntityDecoder.TOPIC_USERS, EntityDecoder.TOPIC_GROUPS);

  public static final String DLQ_TOPIC = "dlq.processor";

  private ProcessorJob() {}

  public static void main(String[] args) throws Exception {
    JobOptions o = JobOptions.parse(args);
    if (o.icebergEnabled) {
      // The lake branch (spec §10.7) is built in sprint C; until then only core-only mode runs.
      throw new IllegalArgumentException(
          "--iceberg.enabled=true needs the Iceberg branch, which this build does not contain yet;"
              + " start the stack with make up-core (ICEBERG_ENABLED=false)");
    }
    LOG.info(
        "starting {}: kafka={} group={} fenodes={} benodes={} label-run-id={} iceberg={}",
        JOB_NAME,
        o.kafkaBootstrap,
        o.kafkaGroup,
        o.dorisFenodes,
        o.dorisBenodes.isEmpty() ? "(redirect)" : o.dorisBenodes,
        o.labelRunId,
        o.icebergEnabled);
    StreamExecutionEnvironment env = StreamExecutionEnvironment.getExecutionEnvironment();
    build(env, o);
    env.execute(JOB_NAME);
  }

  /** Builds the topology of spec §10.1 with the operator names and uids of spec §10.10. */
  public static void build(StreamExecutionEnvironment env, JobOptions o) {
    KafkaSource<Envelope> source =
        KafkaSource.<Envelope>builder()
            .setBootstrapServers(o.kafkaBootstrap)
            .setTopics(TOPICS)
            .setGroupId(o.kafkaGroup)
            .setStartingOffsets(OffsetsInitializer.committedOffsets(OffsetResetStrategy.EARLIEST))
            .setDeserializer(new EntityDeserializer())
            .setProperty("commit.offsets.on.checkpoint", "true")
            .build();

    SingleOutputStreamOperator<Envelope> decoded =
        env.fromSource(source, WatermarkStrategy.noWatermarks(), "EntitiesSource")
            .uid("entities-source")
            .process(new DecodeValidate())
            .name("DecodeValidate")
            .uid("decode-validate");

    KafkaSink<Envelope> dlq =
        KafkaSink.<Envelope>builder()
            .setBootstrapServers(o.kafkaBootstrap)
            .setRecordSerializer(new DlqSerializer(DLQ_TOPIC))
            .setDeliveryGuarantee(DeliveryGuarantee.AT_LEAST_ONCE)
            .build();
    decoded.getSideOutput(DecodeValidate.INVALID).sinkTo(dlq).name("DlqSink").uid("dlq-sink");

    WatermarkStrategy<Envelope> watermarks =
        WatermarkStrategy.<Envelope>forBoundedOutOfOrderness(Duration.ofSeconds(5))
            .withTimestampAssigner((e, ts) -> e.changedAtMillis)
            .withIdleness(Duration.ofSeconds(30));
    SingleOutputStreamOperator<Envelope> accepted =
        decoded
            .assignTimestampsAndWatermarks(watermarks)
            .name("Watermarks")
            .uid("watermarks")
            .keyBy(VersionGate.KEY)
            .process(new VersionGate())
            .name("VersionGate")
            .uid("version-gate");

    entitySink(accepted, o, Envelope.ASSET, DorisSinks.ASSETS, "DorisAssets", "doris-sink-assets");
    entitySink(accepted, o, Envelope.USER, DorisSinks.USERS, "DorisUsers", "doris-sink-users");
    entitySink(accepted, o, Envelope.GROUP, DorisSinks.GROUPS, "DorisGroups", "doris-sink-groups");

    DataStream<ChangeRate> rates = ChangeRateWindow.apply(accepted);
    rates
        .map(new ChangeRateJson())
        .name("ChangeRateJson")
        .uid("change-rate-json")
        .sinkTo(DorisSinks.forTable(o, DorisSinks.CHANGE_RATE))
        .name("DorisChangeRate")
        .uid("doris-sink-change-rate");
  }

  private static void entitySink(
      DataStream<Envelope> accepted,
      JobOptions o,
      String type,
      String table,
      String name,
      String uid) {
    accepted
        .filter(new OfType(type))
        .name("Only" + table)
        .uid("filter-" + table)
        .map(new EntityJson())
        .name("Json" + table)
        .uid("json-" + table)
        .sinkTo(DorisSinks.forTable(o, table))
        .name(name)
        .uid(uid);
  }

  /** Keeps the envelopes of one entity type. */
  static final class OfType implements FilterFunction<Envelope> {
    private static final long serialVersionUID = 1L;
    private final String type;

    OfType(String type) {
      this.type = type;
    }

    @Override
    public boolean filter(Envelope e) {
      return type.equals(e.entityType);
    }
  }

  /** Maps an accepted entity to its Doris JSON row; ingested_at is the mapping time. */
  static final class EntityJson implements MapFunction<Envelope, String> {
    private static final long serialVersionUID = 1L;

    @Override
    public String map(Envelope e) {
      return DorisRows.entity(e, System.currentTimeMillis());
    }
  }

  /** Maps a closed window to its {@code change_rate_1m} JSON row. */
  static final class ChangeRateJson implements MapFunction<ChangeRate, String> {
    private static final long serialVersionUID = 1L;

    @Override
    public String map(ChangeRate r) {
      return DorisRows.changeRate(r);
    }
  }
}
