package fetcher

import (
	"math/rand/v2"
	"sync"
	"time"
)

// Backoff is the retry schedule of spec §9.4: FAST_RETRIES fast retries of
// RETRY_BASE_MS doubling each time (100, 200 and 400 ms) with ±20 % jitter,
// then min(2 × previous, RETRY_MAX_MS) without limit.
type Backoff struct {
	Fast   int
	Base   time.Duration
	Max    time.Duration
	Jitter float64 // 0.2 means ±20 %

	mu  sync.Mutex
	rnd *rand.Rand
}

// NewBackoff returns the schedule with a random jitter source.
func NewBackoff(fast int, base, maxDelay time.Duration, rnd *rand.Rand) *Backoff {
	return &Backoff{Fast: fast, Base: base, Max: maxDelay, Jitter: 0.2, rnd: rnd}
}

// Nominal returns the delay before retry n (1-based) without jitter.
func (b *Backoff) Nominal(n int) time.Duration {
	d := b.Base
	for i := 1; i < n; i++ {
		if d >= b.Max {
			return b.Max
		}
		d *= 2
	}
	if n > b.Fast {
		return min(d, b.Max)
	}
	return d
}

// Delay returns the delay before retry n: fast retries carry the jitter,
// later ones follow the doubling schedule capped at Max.
func (b *Backoff) Delay(n int) time.Duration {
	d := b.Nominal(n)
	if n > b.Fast || b.Jitter <= 0 {
		return d
	}
	b.mu.Lock()
	f := 1 + b.Jitter*(2*b.rnd.Float64()-1)
	b.mu.Unlock()
	return time.Duration(float64(d) * f)
}
