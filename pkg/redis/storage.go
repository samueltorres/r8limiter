package redis

import (
	"context"
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type RemoteStorage struct {
	client *redis.Client
	logger *logrus.Logger
}

func NewRemoteStorage(client *redis.Client, logger *logrus.Logger) *RemoteStorage {
	return &RemoteStorage{
		client: client,
		logger: logger,
	}
}

func (s RemoteStorage) Add(ctx context.Context, key string, n uint32, ttl int64) error {
	pipe := s.client.Pipeline()
	pipe.IncrBy(key, int64(n))
	pipe.ExpireAt(key, time.Unix(ttl, 0))

	_, err := pipe.Exec()
	if err != nil {
		return errors.Wrap(err, "redis storage pipeline failure")
	}

	return nil
}

func (s RemoteStorage) Get(ctx context.Context, key string) (uint32, error) {
	c, err := s.client.Get(key).Int64()
	if err != nil {
		return 0, errors.Wrap(err, "redis storage get failure")
	}

	return uint32(c), nil
}
