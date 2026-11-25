package chaos

import (
	"math"
	"math/rand/v2"
	"regexp"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
)

func seeded() *rand.Rand { return rand.New(rand.NewPCG(42, 7)) }

func mustNew(t *testing.T, cfg *adlv1.ChaosConfig) *Chaos {
	t.Helper()
	c, err := New(cfg, seeded())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// rateOf runs decide n times and returns the observed hit rate.
func rateOf(n int, decide func() bool) float64 {
	hits := 0
	for range n {
		if decide() {
			hits++
		}
	}
	return float64(hits) / float64(n)
}

func TestEveryKnobHonoursItsRate(t *testing.T) {
	const n = 200000
	now := time.Now()
	for _, rate := range []float64{0, 0.01, 0.05, 0.5, 1} {
		c := mustNew(t, &adlv1.ChaosConfig{
			DuplicateRate: rate, MalformedRate: rate, PhantomRate: rate,
			StaleReadRate: rate, StaleWindowMs: 300, UnavailableRate: rate,
		})
		knobs := map[string]func() bool{
			"duplicate":   c.Duplicate,
			"malformed":   c.Malformed,
			"phantom":     c.Phantom,
			"unavailable": c.Unavailable,
			"stale_read":  func() bool { return c.StaleRead(now.Add(-100*time.Millisecond), now, true) },
		}
		for name, decide := range knobs {
			got := rateOf(n, decide)
			tol := 4 * math.Sqrt(rate*(1-rate)/n) // four standard deviations
			if math.Abs(got-rate) > tol {
				t.Errorf("%s at rate %v: observed %v (tolerance %v)", name, rate, got, tol)
			}
			if rate == 0 && got != 0 || rate == 1 && got != 1 {
				t.Errorf("%s at rate %v: observed %v, want exactly %v", name, rate, got, rate)
			}
		}
	}
}

func TestSeededSourceIsReproducible(t *testing.T) {
	cfg := &adlv1.ChaosConfig{UnavailableRate: 0.3}
	a, _ := New(cfg, seeded())
	b, _ := New(cfg, seeded())
	for i := range 1000 {
		if a.Unavailable() != b.Unavailable() {
			t.Fatalf("draw %d differs between two identically seeded sources", i)
		}
	}
}

func TestStaleReadWindow(t *testing.T) {
	c := mustNew(t, &adlv1.ChaosConfig{StaleReadRate: 1, StaleWindowMs: 300})
	now := time.Now()
	if !c.StaleRead(now.Add(-299*time.Millisecond), now, true) {
		t.Error("inside the window with a previous state: want stale")
	}
	if c.StaleRead(now.Add(-300*time.Millisecond), now, true) {
		t.Error("at the window edge: want fresh")
	}
	if c.StaleRead(now, now, false) {
		t.Error("without a previous state: want fresh")
	}
	if _, err := c.Set(&adlv1.ChaosConfig{StaleReadRate: 1, StaleWindowMs: 0}); err != nil {
		t.Fatal(err)
	}
	if c.StaleRead(now, now, true) {
		t.Error("window 0: want fresh")
	}
}

func TestValidateRanges(t *testing.T) {
	bad := []*adlv1.ChaosConfig{
		nil,
		{DuplicateRate: -0.1},
		{MalformedRate: 1.1},
		{PhantomRate: math.NaN()},
		{StaleReadRate: 2},
		{UnavailableRate: -1},
		{StaleWindowMs: -1},
		{StaleWindowMs: 10001},
	}
	for _, cfg := range bad {
		if Validate(cfg) == nil {
			t.Errorf("Validate(%v) accepted an out-of-range config", cfg)
		}
	}
	good := []*adlv1.ChaosConfig{{}, {DuplicateRate: 1, StaleWindowMs: 10000}, FromEnv(0.05, 0.01, 0.01, 0.05, 0.05, 300)}
	for _, cfg := range good {
		if err := Validate(cfg); err != nil {
			t.Errorf("Validate(%v) = %v", cfg, err)
		}
	}
	c := mustNew(t, &adlv1.ChaosConfig{})
	if _, err := c.Set(&adlv1.ChaosConfig{DuplicateRate: 3}); err == nil {
		t.Error("Set accepted an invalid config")
	}
	if c.Config().GetDuplicateRate() != 0 {
		t.Error("a rejected Set changed the config")
	}
}

func TestMalformedAlternatesOnSeq(t *testing.T) {
	c := mustNew(t, &adlv1.ChaosConfig{})
	now := time.Now()
	for seq := int64(1); seq <= 40; seq++ {
		v := c.MalformedValue(seq, now)
		if seq%2 != 0 {
			if len(v) < 16 || len(v) > 64 {
				t.Fatalf("seq %d: %d random bytes, want 16 to 64", seq, len(v))
			}
			continue
		}
		tr := &adlv1.Trigger{}
		if err := proto.Unmarshal(v, tr); err != nil {
			t.Fatalf("seq %d: even seq must be a Trigger: %v", seq, err)
		}
		if tr.GetEntityType() != adlv1.EntityType_ENTITY_TYPE_UNSPECIFIED {
			t.Fatalf("seq %d: entity type %s, want UNSPECIFIED", seq, tr.GetEntityType())
		}
	}
	if MalformedKey(17) != "malformed:17" {
		t.Fatalf("MalformedKey = %q", MalformedKey(17))
	}
}

func TestPhantomTrigger(t *testing.T) {
	c := mustNew(t, &adlv1.ChaosConfig{})
	re := regexp.MustCompile(`^asset:LT-8\d{3}$`)
	for range 200 {
		key, v := c.PhantomTrigger(time.Now())
		if !re.MatchString(key) {
			t.Fatalf("phantom key %q", key)
		}
		tr := &adlv1.Trigger{}
		if err := proto.Unmarshal(v, tr); err != nil {
			t.Fatal(err)
		}
		if "asset:"+tr.GetEntityId() != key || tr.GetVersion() != 1 || tr.GetEntityType() != adlv1.EntityType_ENTITY_TYPE_ASSET || tr.GetChangedAt() == nil {
			t.Fatalf("phantom trigger %v with key %s", tr, key)
		}
	}
}
