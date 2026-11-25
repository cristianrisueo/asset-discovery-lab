package labkit

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// PollInterval is the polling period of every wait (spec §16.3).
const PollInterval = time.Second

// PollFunc is one observation. It returns done when the condition holds, and
// a short description of what it saw, which Poll reports on timeout. A
// non-nil error is treated as a transient observation failure, unless it is
// wrapped with Stop.
type PollFunc func(ctx context.Context) (done bool, state string, err error)

type stopError struct{ err error }

func (s stopError) Error() string { return s.err.Error() }
func (s stopError) Unwrap() error { return s.err }

// Stop wraps err so that Poll returns it immediately instead of retrying.
func Stop(err error) error { return stopError{err: err} }

// Poll calls fn immediately and then every interval until it reports done, it
// returns a Stop error, or timeout elapses. On timeout the error contains the
// last state observed.
func Poll(ctx context.Context, timeout, interval time.Duration, fn PollFunc) error {
	if interval <= 0 {
		interval = PollInterval
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	last := "nothing observed yet"
	for {
		done, state, err := fn(ctx)
		var se stopError
		if errors.As(err, &se) {
			return se.err
		}
		if done && err == nil {
			return nil
		}
		switch {
		case err != nil && state != "":
			last = fmt.Sprintf("%s (error: %v)", state, err)
		case err != nil:
			last = fmt.Sprintf("error: %v", err)
		case state != "":
			last = state
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("condition not met within %s; last state: %s", timeout, last)
		case <-time.After(interval):
		}
	}
}
