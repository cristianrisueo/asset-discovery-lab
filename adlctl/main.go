// Command adlctl is the lab's control CLI (spec §15.10). It talks to the
// running stack through labkit and fails with a clear message when its target
// is not running.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/cristianrisueo/asset-discovery-lab/labkit"
)

const usage = `usage: adlctl <command> [arguments]

commands:
  wait [--core] [--timeout D]                  wait until the stack is ready
  generator pause | resume | rate N            control the datacore generator
  chaos on | off | show                        control datacore chaos
  burst N                                      apply N generator changes now
  move ID SITE                                 move an asset to another site
  reconcile [--timeout D] [--no-pause] [--no-lake]
                                               compare Doris and the lake with datacore
  flink stop-with-savepoint [--timeout D]      stop the Flink job with a savepoint
  stats                                        datacore stats, consumer lag, job state, Doris rows

Endpoints come from the ADL_* environment variables (see labkit.ConfigFromEnv).
`

// errUsage marks a wrong invocation: exit code 2.
var errUsage = errors.New("usage")

func usageErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errUsage, fmt.Sprintf(format, args...))
}

// app carries the I/O and configuration of one invocation.
type app struct {
	cfg    labkit.Config
	stdout io.Writer
	stderr io.Writer
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], labkit.ConfigFromEnv(), os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run executes one command and returns the process exit code.
func run(ctx context.Context, args []string, cfg labkit.Config, stdout, stderr io.Writer) int {
	a := &app{cfg: cfg, stdout: stdout, stderr: stderr}
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		_, _ = fmt.Fprint(stdout, usage)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	var err error
	switch cmd, rest := args[0], args[1:]; cmd {
	case "wait":
		err = a.wait(ctx, rest)
	case "generator":
		err = a.generator(ctx, rest)
	case "chaos":
		err = a.chaos(ctx, rest)
	case "burst":
		err = a.burst(ctx, rest)
	case "move":
		err = a.move(ctx, rest)
	case "reconcile":
		err = a.reconcile(ctx, rest)
	case "flink":
		err = a.flink(ctx, rest)
	case "stats":
		err = a.stats(ctx, rest)
	default:
		err = usageErr("unknown command %q", cmd)
	}
	switch {
	case err == nil:
		return 0
	case errors.Is(err, errUsage):
		_, _ = fmt.Fprintf(stderr, "adlctl: %v\n\n%s", err, usage)
		return 2
	default:
		_, _ = fmt.Fprintf(stderr, "adlctl: %v\n", err)
		return 1
	}
}

// newFlags returns a flag set that reports errors instead of exiting.
func newFlags(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return usageErr("%s: %v", fs.Name(), err)
	}
	return nil
}

// lab builds the labkit clients. Building never connects.
func (a *app) lab() (*labkit.Lab, error) { return labkit.NewLab(a.cfg) }
