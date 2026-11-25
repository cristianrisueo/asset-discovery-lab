package labkit

import (
	"context"
	"errors"
)

// Lab bundles every client, built from one Config. Building it never
// connects; each client fails on its first call if its target is down.
type Lab struct {
	Cfg       Config
	Kafka     *Kafka
	Dragonfly *Dragonfly
	Datacore  *Datacore
	Doris     *Doris
	S3        *S3
	Catalog   *Catalog
	Flink     *Flink

	root    string
	rootErr error
}

// NewLab builds every client from cfg.
func NewLab(cfg Config) (*Lab, error) {
	l := &Lab{Cfg: cfg}
	var err error
	if l.Kafka, err = NewKafka(cfg.Kafka); err != nil {
		return nil, err
	}
	if l.Datacore, err = NewDatacore(cfg.Datacore); err != nil {
		l.Close()
		return nil, err
	}
	if l.Doris, err = NewDoris(cfg.DorisDSN); err != nil {
		l.Close()
		return nil, err
	}
	l.Dragonfly = NewDragonfly(cfg.Dragonfly)
	l.S3 = NewS3(cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Region)
	l.Catalog = NewCatalog(cfg.IcebergREST)
	l.Flink = NewFlink(cfg.Flink)
	l.root, l.rootErr = cfg.ResolveRepoRoot()
	return l, nil
}

// Close releases every client.
func (l *Lab) Close() {
	if l.Kafka != nil {
		l.Kafka.Close()
	}
	if l.Datacore != nil {
		_ = l.Datacore.Close()
	}
	if l.Doris != nil {
		_ = l.Doris.Close()
	}
	if l.Dragonfly != nil {
		_ = l.Dragonfly.Close()
	}
}

// RepoRoot returns the repository root, where docker-compose.yml lives.
func (l *Lab) RepoRoot() (string, error) {
	if l.rootErr != nil {
		return "", l.rootErr
	}
	if l.root == "" {
		return "", errors.New("repository root unknown")
	}
	return l.root, nil
}

// Compose returns a compose runner with every profile active.
func (l *Lab) Compose() (*Compose, error) {
	root, err := l.RepoRoot()
	if err != nil {
		return nil, err
	}
	return NewCompose(root), nil
}

// Make runs a Makefile target at the repository root.
func (l *Lab) Make(ctx context.Context, target string, vars ...string) (string, error) {
	root, err := l.RepoRoot()
	if err != nil {
		return "", err
	}
	return Make(ctx, root, target, vars...)
}

// Sources returns the reconciliation sources backed by this lab.
func (l *Lab) Sources() LabSources { return LabSources{Datacore: l.Datacore, Doris: l.Doris} }

// Reconcile runs spec §16.5 against the lab.
func (l *Lab) Reconcile(ctx context.Context, opts ReconcileOptions) (Report, error) {
	return Reconcile(ctx, l.Sources(), opts)
}

// HTTPGetURL is a convenience for GET base+path.
func HTTPGetURL(ctx context.Context, base, path string) (*HTTPResponse, error) {
	return HTTPGet(ctx, base+path)
}
