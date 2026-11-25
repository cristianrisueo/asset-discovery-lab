package main

import (
	"context"
	"fmt"
	"time"

	"github.com/cristianrisueo/asset-discovery-lab/labkit"
)

// SavepointDir is where graceful stops write savepoints (spec §10.11).
const SavepointDir = "file:///flink-data/savepoints"

func (a *app) flink(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "stop-with-savepoint" {
		return usageErr("flink: expected stop-with-savepoint")
	}
	fs := newFlags("flink stop-with-savepoint", a.stderr)
	timeout := fs.Duration("timeout", 3*time.Minute, "how long to wait for the savepoint")
	if err := parseFlags(fs, args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return usageErr("flink stop-with-savepoint takes no positional arguments")
	}
	f := labkit.NewFlink(a.cfg.Flink)
	job, err := f.JobByName(ctx, labkit.JobName)
	if err != nil {
		return err
	}
	if job == nil || job.State != "RUNNING" {
		state := "absent"
		if job != nil {
			state = job.State
		}
		_, _ = fmt.Fprintf(a.stdout, "flink: job %s is %s; nothing to stop\n", labkit.JobName, state)
		return nil
	}
	sctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	_, _ = fmt.Fprintf(a.stdout, "flink: stopping job %s (%s) with a savepoint in %s\n", labkit.JobName, job.ID, SavepointDir)
	loc, err := f.StopWithSavepoint(sctx, job.ID, SavepointDir, time.Second)
	if err != nil {
		return fmt.Errorf("stop-with-savepoint: %w", err)
	}
	_, _ = fmt.Fprintf(a.stdout, "flink: savepoint written to %s\n", loc)
	return nil
}
