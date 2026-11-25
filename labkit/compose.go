package labkit

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// AllProfiles are the profiles of the full stack (spec §15.2).
var AllProfiles = []string{"core", "lake", "app"}

// Compose runs the docker compose CLI against the repository's compose file.
type Compose struct {
	Root     string
	Profiles []string
}

// NewCompose returns a runner for the repository at root with every profile
// active, so that any defined service can be addressed.
func NewCompose(root string) *Compose {
	return &Compose{Root: root, Profiles: append([]string(nil), AllProfiles...)}
}

func (c *Compose) args(extra ...string) []string {
	a := []string{"compose", "--project-directory", c.Root}
	for _, p := range c.Profiles {
		a = append(a, "--profile", p)
	}
	return append(a, extra...)
}

// Run executes docker compose with args and returns its combined output.
func (c *Compose) Run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", c.args(args...)...)
	cmd.Dir = c.Root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("docker compose %s: %w: %s", strings.Join(args, " "), err, truncate(strings.TrimSpace(string(out)), 2000))
	}
	return string(out), nil
}

// PS returns `docker compose ps -a` as text.
func (c *Compose) PS(ctx context.Context) (string, error) { return c.Run(ctx, "ps", "-a") }

// Logs returns the last tail lines of the services' logs.
func (c *Compose) Logs(ctx context.Context, tail int, services ...string) (string, error) {
	return c.Run(ctx, append([]string{"logs", "--no-color", "--tail", fmt.Sprint(tail)}, services...)...)
}

// Kill sends SIGKILL to a service's containers.
func (c *Compose) Kill(ctx context.Context, service string) error {
	_, err := c.Run(ctx, "kill", service)
	return err
}

// Start starts a stopped service.
func (c *Compose) Start(ctx context.Context, service string) error {
	_, err := c.Run(ctx, "start", service)
	return err
}

// Stop stops a service.
func (c *Compose) Stop(ctx context.Context, service string) error {
	_, err := c.Run(ctx, "stop", service)
	return err
}

// Restart restarts a service.
func (c *Compose) Restart(ctx context.Context, service string) error {
	_, err := c.Run(ctx, "restart", service)
	return err
}

// Exec runs a command inside a running service container, without a TTY.
func (c *Compose) Exec(ctx context.Context, service string, cmd ...string) (string, error) {
	return c.Run(ctx, append([]string{"exec", "-T", service}, cmd...)...)
}

// ContainerStatus is one entry of `docker compose ps -a --format json`.
type ContainerStatus struct {
	Name     string `json:"Name"`
	Service  string `json:"Service"`
	State    string `json:"State"`
	Health   string `json:"Health"`
	ExitCode int    `json:"ExitCode"`
}

// Status lists the containers of the project.
func (c *Compose) Status(ctx context.Context) ([]ContainerStatus, error) {
	out, err := c.Run(ctx, "ps", "-a", "--format", "json")
	if err != nil {
		return nil, err
	}
	return ParsePSJSON([]byte(out))
}

// ParsePSJSON accepts both a JSON array and one JSON object per line, since
// compose versions differ.
func ParsePSJSON(b []byte) ([]ContainerStatus, error) {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return nil, nil
	}
	if b[0] == '[' {
		var out []ContainerStatus
		if err := json.Unmarshal(b, &out); err != nil {
			return nil, fmt.Errorf("parse compose ps: %w", err)
		}
		return out, nil
	}
	var out []ContainerStatus
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var cs ContainerStatus
		if err := json.Unmarshal(line, &cs); err != nil {
			return nil, fmt.Errorf("parse compose ps line %q: %w", truncate(string(line), 200), err)
		}
		out = append(out, cs)
	}
	return out, sc.Err()
}

// ServiceConfig is what wait needs from a service definition.
type ServiceConfig struct {
	Name           string
	Restart        string
	HasHealthcheck bool
}

// OneShot reports whether the service is a one-shot container: no restart
// policy, or "no" (spec §15.2).
func (s ServiceConfig) OneShot() bool { return s.Restart == "" || s.Restart == "no" }

// Services lists the services defined for the active profiles.
func (c *Compose) Services(ctx context.Context) ([]ServiceConfig, error) {
	out, err := c.Run(ctx, "config", "--format", "json")
	if err != nil {
		return nil, err
	}
	return ParseConfigJSON([]byte(out))
}

// ParseConfigJSON parses `docker compose config --format json`.
func ParseConfigJSON(b []byte) ([]ServiceConfig, error) {
	var cfg struct {
		Services map[string]struct {
			Restart     string `json:"restart"`
			Healthcheck *struct {
				Disable bool     `json:"disable"`
				Test    []string `json:"test"`
			} `json:"healthcheck"`
		} `json:"services"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("parse compose config: %w", err)
	}
	out := make([]ServiceConfig, 0, len(cfg.Services))
	for name, s := range cfg.Services {
		hc := s.Healthcheck != nil && !s.Healthcheck.Disable && len(s.Healthcheck.Test) > 0 &&
			(len(s.Healthcheck.Test) != 1 || s.Healthcheck.Test[0] != "NONE")
		out = append(out, ServiceConfig{Name: name, Restart: s.Restart, HasHealthcheck: hc})
	}
	sortServices(out)
	return out, nil
}

func sortServices(s []ServiceConfig) {
	sort.Slice(s, func(i, j int) bool { return s[i].Name < s[j].Name })
}

// Make runs `make -C root target vars...` and returns its combined output.
func Make(ctx context.Context, root, target string, vars ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "make", append([]string{"-C", root, target}, vars...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("make %s: %w", target, err)
	}
	return string(out), nil
}
