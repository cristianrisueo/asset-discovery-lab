package main

import (
	"context"
	"fmt"
	"time"

	"github.com/cristianrisueo/asset-discovery-lab/labkit"
)

func (a *app) reconcile(ctx context.Context, args []string) error {
	fs := newFlags("reconcile", a.stderr)
	timeout := fs.Duration("timeout", 3*time.Minute, "how long to wait for convergence")
	noPause := fs.Bool("no-pause", false, "do not pause the generator")
	noLake := fs.Bool("no-lake", false, "skip the lake comparison")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return usageErr("reconcile takes no positional arguments")
	}
	l, err := a.lab()
	if err != nil {
		return err
	}
	defer l.Close()
	if !*noLake {
		if skip, why := a.lakeSkip(ctx); skip {
			_, _ = fmt.Fprintf(a.stderr, "reconcile: lake comparison skipped: %s\n", why)
			*noLake = true
		}
	}
	rep, err := l.Reconcile(ctx, labkit.ReconcileOptions{
		Timeout: *timeout,
		NoPause: *noPause,
		NoLake:  *noLake,
		Logf: func(format string, args ...any) {
			_, _ = fmt.Fprintf(a.stderr, format+"\n", args...)
		},
	})
	if rep.Counts != nil {
		_, _ = fmt.Fprint(a.stdout, rep.String())
	}
	return err
}

// lakeSkip reports whether the lake comparison must be skipped because the
// stack runs without the lake: core-only mode (spec §15.2), where the lake
// profile is not running and the job has no Iceberg branch. When the lake
// profile runs, the lake is always compared, so a missing catalog still fails.
// If docker compose cannot be read, the lake is compared.
func (a *app) lakeSkip(ctx context.Context) (bool, string) {
	root, err := a.cfg.ResolveRepoRoot()
	if err != nil {
		return false, ""
	}
	containers, err := labkit.NewCompose(root).Status(ctx)
	if err != nil {
		return false, ""
	}
	if lakeRunning(containers) {
		return false, ""
	}
	return true, "the lake profile is not running (core-only mode, make up-core); use make up for the full stack"
}

// lakeRunning reports whether a container of the iceberg-rest service runs.
func lakeRunning(containers []labkit.ContainerStatus) bool {
	for _, c := range containers {
		if c.Service == labkit.CompIcebergREST && c.State == "running" {
			return true
		}
	}
	return false
}
