package io.github.cristianrisueo.adl.processor.decode;

import static org.junit.jupiter.api.Assertions.assertEquals;

import org.apache.flink.streaming.api.operators.ProcessOperator;
import org.apache.flink.streaming.util.OneInputStreamOperatorTestHarness;
import org.junit.jupiter.api.Test;

class DecodeValidateTest {

  @Test
  void routesInvalidEnvelopesToTheSideOutput() throws Exception {
    try (OneInputStreamOperatorTestHarness<Envelope, Envelope> h =
        new OneInputStreamOperatorTestHarness<>(new ProcessOperator<>(new DecodeValidate()))) {
      h.open();
      Envelope ok = new Envelope();
      ok.valid = true;
      Envelope bad = new Envelope();
      bad.reason = Envelope.DECODE_ERROR;
      h.processElement(ok, 1L);
      h.processElement(bad, 2L);
      assertEquals(1, h.extractOutputValues().size());
      assertEquals(1, h.getSideOutput(DecodeValidate.INVALID).size());
    }
  }
}
