package labkit

import (
	"fmt"
	"strings"
	"text/tabwriter"
)

// WaitRow is one line of the adlctl wait status table.
type WaitRow struct {
	Component string
	Status    string
	Ready     bool
}

// EvaluateServices decides, for every defined service, whether it is ready:
// long-running services must be healthy (or running when they have no
// healthcheck) and one-shot services must have exited 0 (spec §15.10).
func EvaluateServices(services []ServiceConfig, containers []ContainerStatus) []WaitRow {
	by := map[string]ContainerStatus{}
	for _, c := range containers {
		by[c.Service] = c
	}
	rows := make([]WaitRow, 0, len(services))
	for _, s := range services {
		c, ok := by[s.Name]
		row := WaitRow{Component: s.Name}
		switch {
		case !ok:
			row.Status = "not created"
		case s.OneShot():
			switch {
			case c.State == "exited" && c.ExitCode == 0:
				row.Status, row.Ready = "completed", true
			case c.State == "exited":
				row.Status = fmt.Sprintf("failed (exit %d)", c.ExitCode)
			default:
				row.Status = c.State
			}
		case c.State != "running":
			row.Status = c.State
		case s.HasHealthcheck && c.Health != "healthy":
			row.Status = "running, " + orDefault(c.Health, "health unknown")
		case s.HasHealthcheck:
			row.Status, row.Ready = "healthy", true
		default:
			row.Status, row.Ready = "running", true
		}
		rows = append(rows, row)
	}
	return rows
}

// FailedOneShot reports whether a one-shot service exited with an error,
// which no amount of waiting will fix.
func FailedOneShot(rows []WaitRow) []string {
	var out []string
	for _, r := range rows {
		if strings.HasPrefix(r.Status, "failed (exit") {
			out = append(out, r.Component)
		}
	}
	return out
}

// AllReady reports whether every row is ready.
func AllReady(rows []WaitRow) bool {
	for _, r := range rows {
		if !r.Ready {
			return false
		}
	}
	return true
}

// FormatWaitTable renders the rows as an aligned table.
func FormatWaitTable(rows []WaitRow) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "COMPONENT\tSTATUS\tREADY")
	for _, r := range rows {
		ready := "no"
		if r.Ready {
			ready = "yes"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", r.Component, r.Status, ready)
	}
	_ = w.Flush()
	return b.String()
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
