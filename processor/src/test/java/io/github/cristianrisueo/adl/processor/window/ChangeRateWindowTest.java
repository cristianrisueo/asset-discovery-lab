package io.github.cristianrisueo.adl.processor.window;

import static org.junit.jupiter.api.Assertions.assertEquals;

import io.github.cristianrisueo.adl.processor.decode.Envelope;
import java.time.Duration;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.List;
import org.apache.flink.api.common.eventtime.WatermarkStrategy;
import org.apache.flink.streaming.api.environment.StreamExecutionEnvironment;
import org.apache.flink.util.CloseableIterator;
import org.junit.jupiter.api.Test;

class ChangeRateWindowTest {

  private static final long T0 = 1_795_601_640_000L; // a minute boundary, 2026-11-25T10:14:00Z

  private static Envelope change(String type, long at) {
    Envelope e = new Envelope();
    e.valid = true;
    e.entityType = type;
    e.id = "x";
    e.version = 1;
    e.changedAtMillis = at;
    return e;
  }

  @Test
  void countsPerTypeAndMinute() throws Exception {
    StreamExecutionEnvironment env = StreamExecutionEnvironment.getExecutionEnvironment();
    env.setParallelism(1);
    List<Envelope> input =
        List.of(
            change("asset", T0 + 1_000),
            change("asset", T0 + 59_999),
            change("user", T0 + 30_000),
            change("asset", T0 + 60_000),
            change("asset", T0 + 61_000),
            change("asset", T0 + 62_000),
            change("group", T0 + 125_000));
    List<ChangeRate> out = new ArrayList<>();
    try (CloseableIterator<ChangeRate> it =
        ChangeRateWindow.apply(
                env.fromData(input)
                    .assignTimestampsAndWatermarks(
                        WatermarkStrategy.<Envelope>forBoundedOutOfOrderness(Duration.ofSeconds(5))
                            .withTimestampAssigner((e, ts) -> e.changedAtMillis)))
            .executeAndCollect()) {
      it.forEachRemaining(out::add);
    }
    out.sort(
        Comparator.comparingLong((ChangeRate r) -> r.windowStart).thenComparing(r -> r.entityType));
    assertEquals(
        List.of(
                new ChangeRate("asset", T0, T0 + 60_000, 2),
                new ChangeRate("user", T0, T0 + 60_000, 1),
                new ChangeRate("asset", T0 + 60_000, T0 + 120_000, 3),
                new ChangeRate("group", T0 + 120_000, T0 + 180_000, 1))
            .toString(),
        out.toString());
  }
}
