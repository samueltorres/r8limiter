package redis

import (
	"context"
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/samueltorres/r8limiter/pkg/counter"
	"github.com/sirupsen/logrus"
)

type metrics struct {
	incDuration      prometheus.Histogram
	batchIncDuration prometheus.Histogram
	getDuration      prometheus.Histogram
	incCount         prometheus.Counter
}

func newMetrics(r prometheus.Registerer) *metrics {
	var m metrics

	m.incDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "redis_inc_duration_seconds",
		Help: "Duration of add operation",
	})

	m.batchIncDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "redis_batchinc_duration_seconds",
		Help: "Duration of add operation",
	})

	m.getDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "redis_get_duration_seconds",
		Help: "Duration of get operation",
	})

	m.incCount = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "redis_inc_total",
		Help: "Number of counter increments in total, including batch",
	})

	r.MustRegister(m.incDuration, m.batchIncDuration, m.getDuration, m.incCount)
	return &m
}

type Storage struct {
	client  *redis.Client
	logger  *logrus.Logger
	metrics *metrics
}

func NewStorage(client *redis.Client, logger *logrus.Logger, registerer prometheus.Registerer) *Storage {
	metrics := newMetrics(registerer)

	return &Storage{
		client:  client,
		logger:  logger,
		metrics: metrics,
	}
}

func (s *Storage) BatchIncrement(ctx context.Context, incrs []counter.CounterInc) ([]counter.CounterIncResponse, error) {
	timer := prometheus.NewTimer(s.metrics.batchIncDuration)
	defer func() {
		timer.ObserveDuration()
	}()

	incrCmds := make([]*redis.IntCmd, len(incrs))

	pipe := s.client.Pipeline()
	for i, increment := range incrs {
		incrCmds[i] = pipe.IncrBy(increment.Key, int64(increment.Inc))
		pipe.ExpireAt(increment.Key, time.Unix(increment.TTL+2, 0))
	}

	c := make(chan error, 1)
	go func() {
		_, err := pipe.Exec()
		c <- err
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-c:
		if err != nil {
			return nil, err
		}
	}

	incrResponses := make([]counter.CounterIncResponse, len(incrs))
	for i, cmd := range incrCmds {

		incrResponses[i] = counter.CounterIncResponse{
			Key: incrs[i].Key,
		}

		res, err := cmd.Result()
		if err != nil {
			incrResponses[i].Err = errors.Wrap(err, "redis storage pipeline command failure")
		}

		incrResponses[i].Curr = uint32(res)
	}

	s.metrics.incCount.Add(float64(len(incrs)))

	return incrResponses, nil
}

func (s *Storage) Increment(ctx context.Context, key string, n uint32, ttl int64) (uint32, error) {
	timer := prometheus.NewTimer(s.metrics.incDuration)
	defer func() {
		timer.ObserveDuration()
	}()

	pipe := s.client.Pipeline()
	incrCmd := pipe.IncrBy(key, int64(n))
	pipe.ExpireAt(key, time.Unix(ttl+2, 0))

	_, err := pipe.Exec()
	if err != nil {
		return 0, errors.Wrap(err, "redis storage pipeline failure")
	}

	res, err := incrCmd.Result()
	if err != nil {
		return 0, errors.Wrap(err, "redis storage pipeline command failure")
	}

	s.metrics.incCount.Add(1)

	return uint32(res), nil
}

func (s *Storage) Get(ctx context.Context, key string) (uint32, error) {
	timer := prometheus.NewTimer(s.metrics.getDuration)
	defer func() {
		timer.ObserveDuration()
	}()

	c, err := s.client.Get(key).Int64()
	if err != nil {
		return 0, errors.Wrap(err, "redis storage get failure "+key)
	}

	return uint32(c), nil
}
