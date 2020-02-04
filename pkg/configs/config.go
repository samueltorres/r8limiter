package configs

type Config struct {
	GrpcAddr  string
	HttpAddr  string
	DebugAddr string
	Datastore string
	Redis     RedisConfig
	RulesFile string
	LogLevel  string
}

type RedisConfig struct {
	Address  string
	Database int
	Password string
}
