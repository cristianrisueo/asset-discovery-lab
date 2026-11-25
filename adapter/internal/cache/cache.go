// Package cache is the adapter's Dragonfly cache of entities by type, id and
// version (spec §9.3). It is an optimisation only: every error is logged and
// treated as a miss, and every operation fails fast.
package cache

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Timeout bounds every cache operation (spec §9.3).
const Timeout = 100 * time.Millisecond

// Key is "entity:<type>:<id>:v<version>", for example entity:asset:LT-0042:v7.
func Key(entityType, id string, version int64) string {
	return "entity:" + entityType + ":" + id + ":v" + strconv.FormatInt(version, 10)
}

// Cache is a Dragonfly client that never blocks the pipeline.
type Cache struct {
	rdb     *redis.Client
	ttl     time.Duration
	timeout time.Duration
	log     *slog.Logger
}

type quietLogger struct{}

func (quietLogger) Printf(context.Context, string, ...interface{}) {}

// New returns a cache on addr with the given TTL.
func New(addr string, ttl time.Duration, log *slog.Logger) *Cache {
	// go-redis prints connection errors on its own; ours are logged as misses.
	redis.SetLogger(quietLogger{})
	rdb := redis.NewClient(&redis.Options{
		Addr:            addr,
		Protocol:        2,
		DisableIdentity: true,
		DialTimeout:     Timeout,
		ReadTimeout:     Timeout,
		WriteTimeout:    Timeout,
		PoolTimeout:     Timeout,
		MaxRetries:      -1, // fail fast: no retries
		PoolSize:        8,
	})
	return &Cache{rdb: rdb, ttl: ttl, timeout: Timeout, log: log}
}

// Close releases the connections.
func (c *Cache) Close() error { return c.rdb.Close() }

// Get returns the cached value and whether it was a hit. Any error, including
// a timeout or Dragonfly being down, is logged and reported as a miss.
func (c *Cache) Get(ctx context.Context, key string) ([]byte, bool) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	b, err := c.rdb.Get(ctx, key).Bytes()
	switch {
	case errors.Is(err, redis.Nil):
		return nil, false
	case err != nil:
		c.log.Warn("cache get failed; treated as a miss", "key", key, "error", err.Error())
		return nil, false
	}
	return b, true
}

// Set stores value under key with the TTL. Errors are logged, never returned.
func (c *Cache) Set(ctx context.Context, key string, value []byte) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if err := c.rdb.Set(ctx, key, value, c.ttl).Err(); err != nil {
		c.log.Warn("cache set failed", "key", key, "error", err.Error())
	}
}
