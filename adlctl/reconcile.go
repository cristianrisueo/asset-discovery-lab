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
