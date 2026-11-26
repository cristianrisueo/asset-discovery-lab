package main

import (
	"context"
	"fmt"
	"time"

	"github.com/cristianrisueo/asset-discovery-lab/labkit"
)

// SavepointDir is where graceful stops write savepoints (spec §10.11).
const SavepointDir = "file:///flink-data/savepoints"

// settleWait is how long stop-with-savepoint waits for a job in a transient
// state (RESTARTING, INITIALIZING, or RUNNING with tasks still starting) to
// become ready before it tries the stop anyway.
const settleWait = 90 * time.Second

// retryPause separates two stop-with-savepoint attempts. A variable so tests
// can shorten it.
var retryPause = 2 * time.Second

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
	if job != nil && !job.Ready() && labkit.ActiveJobStates[job.State] {
		// A job that is failing over cannot take a savepoint yet, even once
		// Flink reports it RUNNING while its tasks still start; wait for it,
		// or the next start would restore from an older checkpoint.
		_, _ = fmt.Fprintf(a.stdout, "flink: job %s is %s (%d of %d tasks running); waiting up to %s for every task\n",
			labkit.JobName, job.State, job.Tasks.Running, job.Tasks.Total, min(settleWait, *timeout))
		wctx, wcancel := context.WithTimeout(ctx, min(settleWait, *timeout))
		settled, werr := f.SettledJob(wctx, labkit.JobName, time.Second)
		wcancel()
		if settled != nil {
			job = settled
		}
		if werr != nil && ctx.Err() != nil {
			return werr
		}
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
	for attempt := 1; ; attempt++ {
		_, _ = fmt.Fprintf(a.stdout, "flink: stopping job %s (%s) with a savepoint in %s\n", labkit.JobName, job.ID, SavepointDir)
		loc, err := f.StopWithSavepoint(sctx, job.ID, SavepointDir, time.Second)
		if err == nil {
			_, _ = fmt.Fprintf(a.stdout, "flink: savepoint written to %s\n", loc)
			return nil
		}
		// A failed stop-with-savepoint leaves the job running. If a failover
		// started between the check and the trigger, wait until the same job is
		// ready again and retry, within the same timeout (spec §10.11).
		next, ok := a.retryableJob(sctx, f, job.ID)
		if !ok {
			return fmt.Errorf("stop-with-savepoint: %w", err)
		}
		_, _ = fmt.Fprintf(a.stdout, "flink: savepoint attempt %d failed (%v); retrying once every task runs\n", attempt, err)
		job = next
	}
}

// retryableJob waits retryPause, then until the job jid is ready again, and
// returns it. It reports false when the timeout ends first, or when the job
// is gone, replaced or no longer active.
func (a *app) retryableJob(ctx context.Context, f *labkit.Flink, jid string) (*labkit.JobSummary, bool) {
	select {
	case <-ctx.Done():
		return nil, false
	case <-time.After(retryPause):
	}
	job, err := f.SettledJob(ctx, labkit.JobName, time.Second)
	if err != nil || job == nil || job.ID != jid || job.State != "RUNNING" {
		return nil, false
	}
	return job, true
}
