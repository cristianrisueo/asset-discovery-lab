package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/cristianrisueo/asset-discovery-lab/labkit"
)

func (a *app) stats(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return usageErr("stats takes no arguments")
	}
	l, err := a.lab()
	if err != nil {
		return err
	}
	defer l.Close()
	var errs []error
	section := func(title string, fn func(context.Context) error) {
		_, _ = fmt.Fprintf(a.stdout, "== %s\n", title)
		sctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		if err := fn(sctx); err != nil {
			_, _ = fmt.Fprintf(a.stdout, "  unavailable: %v\n", err)
			errs = append(errs, fmt.Errorf("%s: %w", title, err))
		}
	}

	section("datacore", func(ctx context.Context) error {
		s, err := l.Datacore.Stats(ctx)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(a.stdout, "  assets %d, users %d, groups %d\n  changes_applied %d, triggers_published %d, outbox_pending %d, get_calls %d\n",
			s.GetAssets(), s.GetUsers(), s.GetGroups(), s.GetChangesApplied(), s.GetTriggersPublished(), s.GetOutboxPending(), s.GetGetCalls())
		return nil
	})
	section("consumer lag", func(ctx context.Context) error {
		groups := []struct {
			group  string
			topics []string
		}{
			{"adl-adapter", []string{labkit.TopicTriggers}},
			{"adl-processor", labkit.EntityTopics},
		}
		for _, g := range groups {
			total, per, err := l.Kafka.GroupLag(ctx, g.group, g.topics...)
			if err != nil {
				return err
			}
			keys := make([]string, 0, len(per))
			for k := range per {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			_, _ = fmt.Fprintf(a.stdout, "  %s: %d\n", g.group, total)
			for _, k := range keys {
				_, _ = fmt.Fprintf(a.stdout, "    %s %d\n", k, per[k])
			}
		}
		return nil
	})
	section("flink", func(ctx context.Context) error {
		job, err := l.Flink.JobByName(ctx, labkit.JobName)
		if err != nil {
			return err
		}
		if job == nil {
			_, _ = fmt.Fprintf(a.stdout, "  job %s: absent\n", labkit.JobName)
			return nil
		}
		_, _ = fmt.Fprintf(a.stdout, "  job %s (%s): %s since %s\n", job.Name, job.ID, job.State, job.Started().Format(time.RFC3339))
		return nil
	})
	section("doris rows", func(ctx context.Context) error {
		for _, table := range []string{"inventory.assets", "inventory.users", "inventory.`groups`", "inventory.change_rate_1m"} {
			n, err := l.Doris.Count(ctx, table)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(a.stdout, "  %s %d\n", table, n)
		}
		return nil
	})
	return errors.Join(errs...)
}
