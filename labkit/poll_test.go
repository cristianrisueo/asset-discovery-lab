package labkit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPollSucceeds(t *testing.T) {
	n := 0
	err := Poll(context.Background(), time.Second, 5*time.Millisecond, func(context.Context) (bool, string, error) {
		n++
		return n == 3, "n", nil
	})
	if err != nil || n != 3 {
		t.Fatalf("err=%v n=%d", err, n)
	}
}

func TestPollTimeoutReportsLastState(t *testing.T) {
	n := 0
	err := Poll(context.Background(), 50*time.Millisecond, 5*time.Millisecond, func(context.Context) (bool, string, error) {
		n++
		if n%2 == 0 {
			return false, "", errors.New("boom")
		}
		return false, "seen " + string(rune('0'+n%10)), nil
	})
	if err == nil || !strings.Contains(err.Error(), "last state") {
		t.Fatalf("expected a timeout with the last state, got %v", err)
	}
}

func TestPollStop(t *testing.T) {
	want := errors.New("fatal")
	start := time.Now()
	err := Poll(context.Background(), time.Minute, time.Millisecond, func(context.Context) (bool, string, error) {
		return false, "", Stop(want)
	})
	if !errors.Is(err, want) || time.Since(start) > time.Second {
		t.Fatalf("Stop should end the poll immediately, got %v", err)
	}
}

func TestPercentile(t *testing.T) {
	var ds []time.Duration
	for i := 20; i >= 1; i-- {
		ds = append(ds, time.Duration(i)*time.Second)
	}
	if got := Percentile(ds, 95); got != 19*time.Second {
		t.Errorf("p95 of 1..20 s = %v, want 19s", got)
	}
	if got := Percentile(ds, 100); got != 20*time.Second {
		t.Errorf("p100 = %v", got)
	}
	if MaxDuration(ds) != 20*time.Second || Percentile(nil, 95) != 0 {
		t.Error("MaxDuration or empty Percentile")
	}
}
