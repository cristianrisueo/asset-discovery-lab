package io.github.cristianrisueo.adl.processor.window;

/**
 * One closed one-minute window of accepted changes for one entity type (spec §10.8). A Flink POJO.
 */
public class ChangeRate {

  /** {@code asset}, {@code user} or {@code group}. */
  public String entityType;

  /** Window bounds in epoch milliseconds, UTC. */
  public long windowStart;

  public long windowEnd;
  public long changes;

  public ChangeRate() {}

  public ChangeRate(String entityType, long windowStart, long windowEnd, long changes) {
    this.entityType = entityType;
    this.windowStart = windowStart;
    this.windowEnd = windowEnd;
    this.changes = changes;
  }

  @Override
  public String toString() {
    return entityType + "[" + windowStart + "," + windowEnd + ")=" + changes;
  }
}
