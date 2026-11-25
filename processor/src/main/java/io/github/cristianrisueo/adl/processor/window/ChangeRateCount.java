package io.github.cristianrisueo.adl.processor.window;

import org.apache.flink.api.common.functions.AggregateFunction;

/** Counts the records of a window incrementally (spec §10.8). */
public class ChangeRateCount implements AggregateFunction<String, Long, Long> {

  private static final long serialVersionUID = 1L;

  @Override
  public Long createAccumulator() {
    return 0L;
  }

  @Override
  public Long add(String entityType, Long acc) {
    return acc + 1;
  }

  @Override
  public Long getResult(Long acc) {
    return acc;
  }

  @Override
  public Long merge(Long a, Long b) {
    return a + b;
  }
}
