package io.github.cristianrisueo.adl.processor.decode;

import org.apache.flink.api.common.typeinfo.TypeInformation;
import org.apache.flink.streaming.api.functions.ProcessFunction;
import org.apache.flink.util.Collector;
import org.apache.flink.util.OutputTag;

/**
 * Routes envelopes: valid ones continue downstream, invalid ones go to the {@link #INVALID} side
 * output, which feeds the DLQ sink (spec §10.3, §10.4). A malformed message never fails the job.
 */
public class DecodeValidate extends ProcessFunction<Envelope, Envelope> {

  private static final long serialVersionUID = 1L;

  /** Side output of invalid envelopes. */
  public static final OutputTag<Envelope> INVALID =
      new OutputTag<>("invalid-entities", TypeInformation.of(Envelope.class));

  @Override
  public void processElement(Envelope e, Context ctx, Collector<Envelope> out) {
    if (e.valid) {
      out.collect(e);
    } else {
      ctx.output(INVALID, e);
    }
  }
}
