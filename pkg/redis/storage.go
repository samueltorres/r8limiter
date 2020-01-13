package redis

import (
	"context"
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

type metrics struct {
	addDuration prometheus.Histogram
	getDuration prometheus.Histogram
}

func newMetrics(r prometheus.Registerer) *metrics {
	var m metrics

	m.addDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "redis_add_duration_seconds",
		Help:    "Duration of add operation",
		Buckets: prometheus.ExponentialBuckets(0.00005, 2, 12),
	})

	m.getDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "redis_get_duration_seconds",
		Help:    "Duration of get operation",
		Buckets: prometheus.ExponentialBuckets(0.00005, 2, 12),
	})

	r.MustRegister(m.addDuration, m.getDuration)
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

func (s Storage) Add(ctx context.Context, key string, n uint32, ttl int64) error {
	start := time.Now()
	defer func() {
		s.metrics.addDuration.Observe(time.Since(start).Seconds())
	}()

	pipe := s.client.Pipeline()
	pipe.IncrBy(key, int64(n))
	pipe.ExpireAt(key, time.Unix(ttl+2, 0))

	_, err := pipe.Exec()
	if err != nil {
		return errors.Wrap(err, "redis storage pipeline failure")
	}

	return nil
}

func (s Storage) Get(ctx context.Context, key string) (uint32, error) {
	start := time.Now()
	defer func() {
		s.metrics.getDuration.Observe(time.Since(start).Seconds())
	}()

	c, err := s.client.Get(key).Int64()
	if err != nil {
		return 0, errors.Wrap(err, "redis storage get failure "+key)
	}

	return uint32(c), nil
}
