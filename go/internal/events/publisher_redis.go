package events

import (
	"time"

	"github.com/redis/go-redis/v9"
)

const publishTimeout = 5 * time.Second

// NewPublisherRedisClient creates a dedicated outbox connection pool. The caller
// owns the client and must close it when the publisher stops.
// XADD is retried only by the durable outbox, not inside go-redis. Context
// deadlines cover connection setup, pool acquisition and socket I/O.
func NewPublisherRedisClient(opts *redis.Options) *redis.Client {
	bounded := *opts
	bounded.MaxRetries = -1
	bounded.ContextTimeoutEnabled = true
	bounded.DialTimeout = boundedRedisTimeout(bounded.DialTimeout)
	bounded.ReadTimeout = boundedRedisTimeout(bounded.ReadTimeout)
	bounded.WriteTimeout = boundedRedisTimeout(bounded.WriteTimeout)
	bounded.PoolTimeout = boundedRedisTimeout(bounded.PoolTimeout)
	return redis.NewClient(&bounded)
}

func boundedRedisTimeout(d time.Duration) time.Duration {
	if d <= 0 || d > publishTimeout {
		return publishTimeout
	}
	return d
}
