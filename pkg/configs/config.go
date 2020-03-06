package configs

import "time"

type Config struct {
	GrpcAddr            string
	HttpAddr            string
	DebugAddr           string
	Datastore           string
	Redis               RedisConfig
	RulesFile           string
	LogLevel            string
	SyncBatchSize       int
	ShutdownGracePeriod time.Duration
}

type RedisConfig struct {
	Address  string
	Database int
	Password string
}
