package labkit

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Dragonfly is a client for the adapter's cache (spec §9.3).
type Dragonfly struct {
	addr string
	rdb  *redis.Client
}

type quietLogger struct{}

func (quietLogger) Printf(context.Context, string, ...interface{}) {}

var silenceRedis sync.Once

// NewDragonfly builds a client. It does not connect until the first call.
func NewDragonfly(addr string) *Dragonfly {
	// go-redis logs dial failures itself; labkit returns them as errors instead.
	silenceRedis.Do(func() { redis.SetLogger(quietLogger{}) })
	return &Dragonfly{addr: addr, rdb: redis.NewClient(&redis.Options{
		Addr:         addr,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		MaxRetries:   1,
	})}
}

// Close releases the client.
func (d *Dragonfly) Close() error { return d.rdb.Close() }

func (d *Dragonfly) wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("dragonfly at %s: %w", d.addr, err)
}

// Ping checks that the cache answers.
func (d *Dragonfly) Ping(ctx context.Context) error {
	return d.wrap(d.rdb.Ping(ctx).Err())
}

// Get returns the value of key and whether it exists.
func (d *Dragonfly) Get(ctx context.Context, key string) ([]byte, bool, error) {
	v, err := d.rdb.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, d.wrap(err)
	}
	return v, true, nil
}

// TTL returns the remaining time to live of key. Following Redis, it is -2
// (as a duration of -2 ns) when the key does not exist and -1 when it has no
// expiry.
func (d *Dragonfly) TTL(ctx context.Context, key string) (time.Duration, error) {
	v, err := d.rdb.TTL(ctx, key).Result()
	if err != nil {
		return 0, d.wrap(err)
	}
	return v, nil
}

// CacheKey is the adapter's cache key: entity:<type>:<id>:v<version> (spec §9.3).
func CacheKey(entityType, id string, version int64) string {
	return fmt.Sprintf("entity:%s:%s:v%d", entityType, id, version)
}
