package counter

import "context"

type CounterStorage interface {
	BatchIncrement(ctx context.Context, incrs []CounterInc) ([]CounterIncResponse, error)
	Increment(ctx context.Context, key string, n uint32, ttl int64) (uint32, error)
	Get(ctx context.Context, key string) (uint32, error)
}

type CounterInc struct {
	Key string
	Inc uint32
	TTL int64
}

type CounterIncResponse struct {
	Key  string
	Curr uint32
	Err  error
}
