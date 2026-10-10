// Package redis provides the shared Redis client used for the Asynq queue and
// selective caching.
package redis

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// pingTimeout bounds the startup connectivity check.
const pingTimeout = 5 * time.Second

// Client wraps the Redis client so callers depend on this package rather than
// on the driver directly.
type Client struct {
	*goredis.Client
}

// Connect parses the Redis URL and verifies connectivity with a ping. The
// caller decides whether a failure is fatal: the API can serve catalogue and
// auth traffic without Redis, but readiness reports the degradation.
func Connect(url string) (*Client, error) {
	opts, err := goredis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("redis: parse url: %w", err)
	}

	// Keep the pool small: the API shares a 2-core VPS with the worker.
	opts.PoolSize = 10
	opts.MinIdleConns = 1
	opts.DialTimeout = pingTimeout

	client := goredis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis: ping: %w", err)
	}

	return &Client{Client: client}, nil
}

// Ping checks liveness for readiness probes.
func (c *Client) Ping(ctx context.Context) error {
	if c == nil || c.Client == nil {
		return fmt.Errorf("redis: client not configured")
	}
	return c.Client.Ping(ctx).Err()
}

// Close releases the connection pool.
func (c *Client) Close() error {
	if c == nil || c.Client == nil {
		return nil
	}
	return c.Client.Close()
}
