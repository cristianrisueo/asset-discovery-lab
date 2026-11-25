package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cristianrisueo/asset-discovery-lab/labkit"
)

// waitPollInterval is how often wait re-checks the stack.
const waitPollInterval = 5 * time.Second

func (a *app) wait(ctx context.Context, args []string) error {
	fs := newFlags("wait", a.stderr)
	core := fs.Bool("core", false, "only the core profile (make up-core)")
	timeout := fs.Duration("timeout", 15*time.Minute, "how long to wait")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return usageErr("wait takes no positional arguments")
	}
	root, err := a.cfg.ResolveRepoRoot()
	if err != nil {
		return err
	}
	compose := labkit.NewCompose(root)
	if *core {
		compose.Profiles = []string{"core"}
	}
	l, err := a.lab()
	if err != nil {
		return err
	}
	defer l.Close()

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	last := ""
	for {
		rows, done, err := a.waitOnce(ctx, compose, l)
		if err != nil {
			return err
		}
		table := labkit.FormatWaitTable(rows)
		if done {
			if len(rows) == 0 {
				_, _ = fmt.Fprintln(a.stdout, "wait: no services defined; nothing to wait for")
			} else {
				_, _ = fmt.Fprint(a.stdout, table)
				_, _ = fmt.Fprintln(a.stdout, "wait: ready")
			}
			return nil
		}
		if failed := labkit.FailedOneShot(rows); len(failed) > 0 {
			_, _ = fmt.Fprint(a.stdout, table)
			return fmt.Errorf("wait: one-shot service(s) failed: %s (see make logs SERVICE=%s)", strings.Join(failed, ", "), failed[0])
		}
		if table != last {
			_, _ = fmt.Fprintf(a.stdout, "%s\n%s", time.Now().UTC().Format(time.RFC3339), table)
			last = table
		}
		select {
		case <-ctx.Done():
			_, _ = fmt.Fprint(a.stdout, table)
			return fmt.Errorf("wait: stack not ready within %s", *timeout)
		case <-time.After(waitPollInterval):
		}
	}
}

// waitOnce evaluates the defined services, plus the BE and the Flink job when
// their services are defined (sprints plan §3.6).
func (a *app) waitOnce(ctx context.Context, compose *labkit.Compose, l *labkit.Lab) ([]labkit.WaitRow, bool, error) {
	services, err := compose.Services(ctx)
	if err != nil {
		return nil, false, err
	}
	if len(services) == 0 {
		return nil, true, nil
	}
	containers, err := compose.Status(ctx)
	if err != nil {
		return nil, false, err
	}
	rows := labkit.EvaluateServices(services, containers)
	defined := map[string]bool{}
	for _, s := range services {
		defined[s.Name] = true
	}
	if defined[labkit.CompDorisBE] {
		rows = append(rows, beRow(ctx, l))
	}
	if defined[labkit.CompFlinkJobManager] {
		rows = append(rows, jobRow(ctx, l))
	}
	return rows, labkit.AllReady(rows), nil
}

func beRow(ctx context.Context, l *labkit.Lab) labkit.WaitRow {
	row := labkit.WaitRow{Component: "doris-be (SHOW BACKENDS)"}
	qctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	bes, err := l.Doris.Backends(qctx)
	switch {
	case err != nil:
		row.Status = "unknown: " + firstLine(err.Error())
	case len(bes) == 0:
		row.Status = "no backend registered"
	default:
		var parts []string
		for _, b := range bes {
			parts = append(parts, fmt.Sprintf("%s alive=%v", b.Host, b.Alive))
			if b.Alive {
				row.Ready = true
			}
		}
		row.Status = strings.Join(parts, ", ")
	}
	return row
}

func jobRow(ctx context.Context, l *labkit.Lab) labkit.WaitRow {
	row := labkit.WaitRow{Component: "flink job " + labkit.JobName}
	qctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	job, err := l.Flink.JobByName(qctx, labkit.JobName)
	switch {
	case err != nil:
		row.Status = "unknown: " + firstLine(err.Error())
	case job == nil:
		row.Status = "not submitted"
	default:
		row.Status = job.State
		row.Ready = job.State == "RUNNING"
	}
	return row
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}
