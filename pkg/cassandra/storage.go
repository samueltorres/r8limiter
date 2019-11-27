package cassandra

import (
	"context"

	"github.com/gocql/gocql"
	"github.com/sirupsen/logrus"
)

type RemoteStorage struct {
	session *gocql.Session
	logger  *logrus.Logger
}

func NewRemoteStorage(logger *logrus.Logger, session *gocql.Session) *RemoteStorage {
	return &RemoteStorage{
		session: session,
		logger:  logger,
	}
}

func (s RemoteStorage) Add(ctx context.Context, key string, n uint32, ttl int64) error {
	err := s.session.
		Query(`UPDATE svc_ratelimiter.counters SET value = value + ? WHERE key = ?`, n, key).
		Consistency(gocql.LocalQuorum).
		Exec()

	if err != nil {
		return err
	}

	return nil
}

func (s RemoteStorage) Get(ctx context.Context, key string) (uint32, error) {
	var value uint32

	err := s.session.
		Query(`SELECT value FROM svc_ratelimiter.counters WHERE key = ? LIMIT 1`, key).
		Consistency(gocql.LocalQuorum).
		Scan(&value)

	if err != nil {
		return 0, err
	}

	s.logger.Info(value)

	return value, nil
}
