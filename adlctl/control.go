package main

import (
	"context"
	"fmt"
	"strconv"

	"google.golang.org/protobuf/proto"

	adlv1 "github.com/cristianrisueo/asset-discovery-lab/gen/go/adl/v1"
	"github.com/cristianrisueo/asset-discovery-lab/labkit"
)

func (a *app) generator(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageErr("generator: expected pause, resume or rate N")
	}
	var mutate func(*adlv1.GeneratorConfig)
	switch args[0] {
	case "pause":
		if len(args) != 1 {
			return usageErr("generator pause takes no arguments")
		}
		mutate = func(c *adlv1.GeneratorConfig) { c.Paused = true }
	case "resume":
		if len(args) != 1 {
			return usageErr("generator resume takes no arguments")
		}
		mutate = func(c *adlv1.GeneratorConfig) { c.Paused = false }
	case "rate":
		if len(args) != 2 {
			return usageErr("generator rate takes one number")
		}
		r, err := strconv.ParseFloat(args[1], 64)
		if err != nil || r < 0 || r > 500 {
			return usageErr("generator rate: %q is not a number from 0 to 500", args[1])
		}
		mutate = func(c *adlv1.GeneratorConfig) { c.RatePerSecond = r }
	default:
		return usageErr("generator: unknown action %q", args[0])
	}
	l, err := a.lab()
	if err != nil {
		return err
	}
	defer l.Close()
	cur, err := l.Datacore.Generator(ctx)
	if err != nil {
		return err
	}
	next := proto.Clone(cur).(*adlv1.GeneratorConfig)
	mutate(next)
	if _, err := l.Datacore.SetGenerator(ctx, next); err != nil {
		return err
	}
	got, err := l.Datacore.Generator(ctx)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.stdout, "generator: rate %g/s, paused %v\n", got.GetRatePerSecond(), got.GetPaused())
	return nil
}

// chaosConfigFor returns the configuration of chaos on or off.
func chaosConfigFor(action string) (*adlv1.ChaosConfig, bool) {
	switch action {
	case "on":
		return labkit.ChaosOnConfig(), true
	case "off":
		return labkit.ChaosOffConfig(), true
	}
	return nil, false
}

func formatChaos(c *adlv1.ChaosConfig) string {
	return fmt.Sprintf("chaos: duplicate %g, malformed %g, phantom %g, stale_read %g (window %d ms), unavailable %g",
		c.GetDuplicateRate(), c.GetMalformedRate(), c.GetPhantomRate(), c.GetStaleReadRate(), c.GetStaleWindowMs(), c.GetUnavailableRate())
}

func (a *app) chaos(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return usageErr("chaos: expected on, off or show")
	}
	cfg, set := chaosConfigFor(args[0])
	if !set && args[0] != "show" {
		return usageErr("chaos: unknown action %q", args[0])
	}
	l, err := a.lab()
	if err != nil {
		return err
	}
	defer l.Close()
	if set {
		if _, err := l.Datacore.SetChaos(ctx, cfg); err != nil {
			return err
		}
	}
	got, err := l.Datacore.Chaos(ctx)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(a.stdout, formatChaos(got))
	return nil
}

func (a *app) burst(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return usageErr("burst takes one count")
	}
	n, err := strconv.Atoi(args[0])
	if err != nil || n < 1 || n > 10000 {
		return usageErr("burst: %q is not a count from 1 to 10000", args[0])
	}
	l, err := a.lab()
	if err != nil {
		return err
	}
	defer l.Close()
	applied, err := l.Datacore.Burst(ctx, int32(n))
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.stdout, "burst: applied %d changes\n", applied)
	return nil
}

func (a *app) move(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return usageErr("move takes ID and SITE")
	}
	id, site := args[0], args[1]
	if !labkit.AssetIDPattern.MatchString(id) {
		return usageErr("move: %q is not an asset id", id)
	}
	if site == "" {
		return usageErr("move: SITE must not be empty")
	}
	l, err := a.lab()
	if err != nil {
		return err
	}
	defer l.Close()
	// Read through AdminService.ListAssets: chaos never applies to it (spec §7.7).
	cur, err := l.Datacore.FindAsset(ctx, id)
	if err != nil {
		return err
	}
	if cur == nil {
		return fmt.Errorf("asset %s not found in datacore", id)
	}
	req := labkit.AssetRequestFrom(cur)
	req.Site = site
	next, err := l.Datacore.UpsertAsset(ctx, req)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.stdout, "%s: site %s -> %s, version %d -> %d, changed_at %s\n",
		id, cur.GetSite(), next.GetSite(), cur.GetVersion(), next.GetVersion(),
		next.GetChangedAt().AsTime().UTC().Format("2006-01-02T15:04:05.000Z"))
	return nil
}
