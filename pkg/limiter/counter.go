package limiter

import (
	"context"
)

type CounterService interface {
	Increment(ctx context.Context, key string, n uint32, ttl int64, syncRate int) (uint32, error)
	IncrementOnStorage(ctx context.Context, key string, n uint32, ttl int64) (uint32, error)
	Get(ctx context.Context, key string) (uint32, error)
	GetFromStorage(ctx context.Context, key string) (uint32, error)
}
