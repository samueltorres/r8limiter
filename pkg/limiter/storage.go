package limiter

import "context"

type CounterStorage interface {
	Add(ctx context.Context, key string, n uint32, ttl int64) error
	Get(ctx context.Context, key string) (uint32, error)
}
