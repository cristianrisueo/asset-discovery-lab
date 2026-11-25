package labkit

import (
	"math"
	"sort"
	"time"
)

// Percentile returns the nearest-rank p-th percentile (0 < p <= 100) of ds.
// For 20 samples, p95 is the 19th smallest value.
func Percentile(ds []time.Duration, p float64) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	rank := int(math.Ceil(p / 100 * float64(len(s))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(s) {
		rank = len(s)
	}
	return s[rank-1]
}

// MaxDuration returns the largest value of ds, or 0.
func MaxDuration(ds []time.Duration) time.Duration {
	var m time.Duration
	for _, d := range ds {
		if d > m {
			m = d
		}
	}
	return m
}
