package labkit

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

// Default endpoints, as seen from the host (spec §16.3).
const (
	DefaultKafka        = "localhost:29092"
	DefaultDragonfly    = "localhost:6379"
	DefaultDatacore     = "localhost:50051"
	DefaultDorisDSN     = "root:@tcp(localhost:9030)/inventory?parseTime=true&loc=UTC"
	DefaultIcebergREST  = "http://localhost:8181"
	DefaultS3Endpoint   = "http://localhost:9000"
	DefaultFlink        = "http://localhost:8081"
	DefaultExplorer     = "http://localhost:8080"
	DefaultWeb          = "http://localhost:3000"
	DefaultAdapterHTTP  = "http://localhost:8091"
	DefaultDatacoreHTTP = "http://localhost:8090"

	// Lab credentials default to the non-secret values of .env.example (spec §17.2).
	DefaultS3AccessKey = "adl-admin"
	DefaultS3SecretKey = "adl-admin-secret"
	DefaultS3Region    = "us-east-1"
)

// Config holds every endpoint and setting labkit needs.
type Config struct {
	Kafka        string
	Dragonfly    string
	Datacore     string
	DorisDSN     string
	IcebergREST  string
	S3Endpoint   string
	Flink        string
	Explorer     string
	Web          string
	AdapterHTTP  string
	DatacoreHTTP string

	S3AccessKey string
	S3SecretKey string
	S3Region    string

	// AllowMySQLFallback is ADL_ALLOW_MYSQL_FALLBACK=1 (spec §3 R4, test D06).
	AllowMySQLFallback bool

	// RepoRoot is ADL_REPO_ROOT. When empty, ResolveRepoRoot searches upwards
	// from the working directory.
	RepoRoot string
}

// ConfigFromEnv reads the configuration from the process environment.
func ConfigFromEnv() Config {
	return ConfigFromLookup(os.LookupEnv)
}

// ConfigFromLookup reads the configuration through lookup, which makes it testable.
func ConfigFromLookup(lookup func(string) (string, bool)) Config {
	get := func(key, def string) string {
		if v, ok := lookup(key); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		return def
	}
	return Config{
		Kafka:              get("ADL_KAFKA", DefaultKafka),
		Dragonfly:          get("ADL_DRAGONFLY", DefaultDragonfly),
		Datacore:           get("ADL_DATACORE", DefaultDatacore),
		DorisDSN:           get("ADL_DORIS_DSN", DefaultDorisDSN),
		IcebergREST:        strings.TrimRight(get("ADL_ICEBERG_REST", DefaultIcebergREST), "/"),
		S3Endpoint:         strings.TrimRight(get("ADL_S3_ENDPOINT", DefaultS3Endpoint), "/"),
		Flink:              strings.TrimRight(get("ADL_FLINK", DefaultFlink), "/"),
		Explorer:           strings.TrimRight(get("ADL_EXPLORER", DefaultExplorer), "/"),
		Web:                strings.TrimRight(get("ADL_WEB", DefaultWeb), "/"),
		AdapterHTTP:        strings.TrimRight(get("ADL_ADAPTER_HTTP", DefaultAdapterHTTP), "/"),
		DatacoreHTTP:       strings.TrimRight(get("ADL_DATACORE_HTTP", DefaultDatacoreHTTP), "/"),
		S3AccessKey:        get("S3_ACCESS_KEY", DefaultS3AccessKey),
		S3SecretKey:        get("S3_SECRET_KEY", DefaultS3SecretKey),
		S3Region:           get("S3_REGION", DefaultS3Region),
		AllowMySQLFallback: get("ADL_ALLOW_MYSQL_FALLBACK", "") == "1",
		RepoRoot:           get("ADL_REPO_ROOT", ""),
	}
}

// ResolveRepoRoot returns RepoRoot, or the nearest ancestor of the working
// directory that holds both docker-compose.yml and go.mod.
func (c Config) ResolveRepoRoot() (string, error) {
	if c.RepoRoot != "" {
		return filepath.Abs(c.RepoRoot)
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return FindRepoRoot(wd)
}

// FindRepoRoot walks up from start until it finds a directory holding both
// docker-compose.yml and go.mod.
func FindRepoRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if fileExists(filepath.Join(dir, "docker-compose.yml")) && fileExists(filepath.Join(dir, "go.mod")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("repository root not found above %s (set ADL_REPO_ROOT)", start)
		}
		dir = parent
	}
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// Component names match the docker compose service names (spec §15.2).
const (
	CompKafka            = "kafka"
	CompDragonfly        = "dragonfly"
	CompDatacore         = "datacore"
	CompAdapter          = "adapter"
	CompDorisFE          = "doris-fe"
	CompDorisBE          = "doris-be"
	CompFlinkJobManager  = "flink-jobmanager"
	CompFlinkTaskManager = "flink-taskmanager"
	CompIcebergREST      = "iceberg-rest"
	CompRustFS           = "rustfs"
	CompExplorer         = "explorer"
	CompWeb              = "web"
)

// Endpoint is a host-reachable address of a component.
type Endpoint struct {
	Component string
	Name      string
	Addr      string // host:port
}

// Endpoints returns the host-reachable addresses of a component. Components
// without a host port (doris-be, flink-taskmanager) return none.
func (c Config) Endpoints(component string) ([]Endpoint, error) {
	type raw struct{ name, value string }
	var raws []raw
	switch component {
	case CompKafka:
		for _, b := range strings.Split(c.Kafka, ",") {
			raws = append(raws, raw{"Kafka", "tcp://" + strings.TrimSpace(b)})
		}
	case CompDragonfly:
		raws = []raw{{"Dragonfly", "tcp://" + c.Dragonfly}}
	case CompDatacore:
		raws = []raw{{"datacore gRPC", "tcp://" + c.Datacore}, {"datacore HTTP", c.DatacoreHTTP}}
	case CompAdapter:
		raws = []raw{{"adapter HTTP", c.AdapterHTTP}}
	case CompDorisFE:
		addr, err := DorisAddr(c.DorisDSN)
		if err != nil {
			return nil, err
		}
		raws = []raw{{"Doris FE (MySQL protocol)", "tcp://" + addr}}
	case CompFlinkJobManager:
		raws = []raw{{"Flink REST", c.Flink}}
	case CompIcebergREST:
		raws = []raw{{"Iceberg REST catalog", c.IcebergREST}}
	case CompRustFS:
		raws = []raw{{"RustFS S3", c.S3Endpoint}}
	case CompExplorer:
		raws = []raw{{"explorer", c.Explorer}}
	case CompWeb:
		raws = []raw{{"web", c.Web}}
	case CompDorisBE, CompFlinkTaskManager:
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown component %q", component)
	}
	out := make([]Endpoint, 0, len(raws))
	for _, r := range raws {
		addr, err := hostPort(r.value)
		if err != nil {
			return nil, fmt.Errorf("%s endpoint %q: %w", r.name, r.value, err)
		}
		out = append(out, Endpoint{Component: component, Name: r.name, Addr: addr})
	}
	return out, nil
}

// CheckReachable opens a TCP connection to every host endpoint of the given
// components, once, and reports the first one that does not answer.
func (c Config) CheckReachable(ctx context.Context, components ...string) error {
	var errs []error
	for _, comp := range components {
		eps, err := c.Endpoints(comp)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, ep := range eps {
			if err := CheckTCP(ctx, ep.Addr); err != nil {
				errs = append(errs, fmt.Errorf("%s (%s) not reachable at %s: %w", ep.Name, comp, ep.Addr, err))
			}
		}
	}
	return errors.Join(errs...)
}

// CheckTCP dials addr once with a 5 s timeout.
func CheckTCP(ctx context.Context, addr string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return conn.Close()
}

// DorisAddr extracts host:port from a go-sql-driver DSN.
func DorisAddr(dsn string) (string, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "", fmt.Errorf("parse Doris DSN: %w", err)
	}
	return cfg.Addr, nil
}

// hostPort turns "http://h:p/x", "tcp://h:p" or "h:p" into "h:p", filling in
// the scheme's default port.
func hostPort(v string) (string, error) {
	if !strings.Contains(v, "://") {
		v = "tcp://" + v
	}
	u, err := url.Parse(v)
	if err != nil {
		return "", err
	}
	host, port := u.Hostname(), u.Port()
	if host == "" {
		return "", errors.New("missing host")
	}
	if port == "" {
		switch u.Scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		default:
			return "", errors.New("missing port")
		}
	}
	return net.JoinHostPort(host, port), nil
}
