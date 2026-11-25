package io.github.cristianrisueo.adl.processor.window;

import io.github.cristianrisueo.adl.processor.decode.Envelope;
import java.time.Duration;
import org.apache.flink.api.common.typeinfo.Types;
import org.apache.flink.api.java.functions.KeySelector;
import org.apache.flink.streaming.api.datastream.DataStream;
import org.apache.flink.streaming.api.datastream.SingleOutputStreamOperator;
import org.apache.flink.streaming.api.functions.windowing.ProcessWindowFunction;
import org.apache.flink.streaming.api.windowing.assigners.TumblingEventTimeWindows;
import org.apache.flink.streaming.api.windowing.windows.TimeWindow;
import org.apache.flink.util.Collector;

/**
 * Emits one {@link ChangeRate} per entity type and closed event-time minute (spec §10.8). The count
 * comes from {@link ChangeRateCount}; this function only adds the window bounds. Allowed lateness
 * is 0: records whose window has already closed are dropped.
 */
public class ChangeRateWindow extends ProcessWindowFunction<Long, ChangeRate, String, TimeWindow> {

  private static final long serialVersionUID = 1L;

  /** Window length (spec §10.8). */
  public static final Duration SIZE = Duration.ofMinutes(1);

  @Override
  public void process(
      String entityType, Context ctx, Iterable<Long> counts, Collector<ChangeRate> out) {
    long changes = counts.iterator().next();
    TimeWindow w = ctx.window();
    out.collect(new ChangeRate(entityType, w.getStart(), w.getEnd(), changes));
  }

  /**
   * Builds the change-rate branch from the VersionGate output: entity type, keyBy, one-minute
   * tumbling event-time window, count. Operator names and uids follow spec §10.10.
   */
  public static SingleOutputStreamOperator<ChangeRate> apply(DataStream<Envelope> accepted) {
    return accepted
        .map(e -> e.entityType)
        .returns(Types.STRING)
        .name("EntityType")
        .uid("change-rate-entity-type")
        .keyBy(new Identity())
        .window(TumblingEventTimeWindows.of(SIZE))
        .aggregate(new ChangeRateCount(), new ChangeRateWindow())
        .name("ChangeRateWindow")
        .uid("change-rate-window");
  }

  private static final class Identity implements KeySelector<String, String> {
    private static final long serialVersionUID = 1L;

    @Override
    public String getKey(String s) {
      return s;
    }
  }
}
