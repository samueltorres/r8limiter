package configs

type Config struct {
	GrpcAddr  string
	HttpAddr  string
	DebugAddr string
	Datastore string
	Cassandra CassandraConfig
	Redis     RedisConfig
	RulesFile string
}

type CassandraConfig struct {
	Hosts    string
	Keyspace string
}

type RedisConfig struct {
	Address  string
	Database string
}
