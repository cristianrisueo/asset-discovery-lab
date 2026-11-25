package io.github.cristianrisueo.adl.processor.gate;

import io.github.cristianrisueo.adl.processor.decode.Envelope;
import org.apache.flink.api.common.functions.OpenContext;
import org.apache.flink.api.common.state.ValueState;
import org.apache.flink.api.common.state.ValueStateDescriptor;
import org.apache.flink.api.common.typeinfo.Types;
import org.apache.flink.api.java.functions.KeySelector;
import org.apache.flink.metrics.Counter;
import org.apache.flink.streaming.api.functions.KeyedProcessFunction;
import org.apache.flink.util.Collector;

/**
 * Drops stale and duplicate versions (spec §10.5). Keyed by {@code <type>:<id>}, it keeps the last
 * accepted version per key, with no TTL. A greater version, or no state yet, passes and updates the
 * state; an equal or older one is dropped.
 */
public class VersionGate extends KeyedProcessFunction<String, Envelope, Envelope> {

  private static final long serialVersionUID = 1L;

  /** Metric names read by the acceptance tests through the Flink REST API (spec §10.10). */
  public static final String ACCEPTED_METRIC = "versionGateAccepted";

  public static final String DROPPED_METRIC = "versionGateDropped";

  /** The key selector: {@code <type>:<id>}, for example {@code asset:LT-0042}. */
  public static final KeySelector<Envelope, String> KEY = new GateKey();

  private transient ValueState<Long> lastVersion;
  private transient Counter accepted;
  private transient Counter dropped;

  @Override
  public void open(OpenContext openContext) {
    lastVersion =
        getRuntimeContext().getState(new ValueStateDescriptor<>("lastVersion", Types.LONG));
    accepted = getRuntimeContext().getMetricGroup().counter(ACCEPTED_METRIC);
    dropped = getRuntimeContext().getMetricGroup().counter(DROPPED_METRIC);
  }

  @Override
  public void processElement(Envelope e, Context ctx, Collector<Envelope> out) throws Exception {
    Long last = lastVersion.value();
    if (last == null || e.version > last) {
      lastVersion.update(e.version);
      accepted.inc();
      out.collect(e);
    } else {
      dropped.inc();
    }
  }

  private static final class GateKey implements KeySelector<Envelope, String> {
    private static final long serialVersionUID = 1L;

    @Override
    public String getKey(Envelope e) {
      return e.gateKey();
    }
  }
}
