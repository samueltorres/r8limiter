package counters

import "context"

type RemoteCounterStorage interface {
	Add(ctx context.Context, key string, n uint32, ttl int64) error
	Get(ctx context.Context, key string) (uint32, error)
}
