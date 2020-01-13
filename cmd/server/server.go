package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	rediscli "github.com/go-redis/redis/v7"
	"github.com/gocql/gocql"
	"github.com/oklog/run"
	"github.com/peterbourgon/ff"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/version"
	"github.com/samueltorres/r8limiter/pkg/cassandra"
	"github.com/samueltorres/r8limiter/pkg/configs"
	"github.com/samueltorres/r8limiter/pkg/file"
	"github.com/samueltorres/r8limiter/pkg/limiter"
	"github.com/samueltorres/r8limiter/pkg/redis"
	"github.com/samueltorres/r8limiter/pkg/transport/grpc"
	"github.com/samueltorres/r8limiter/pkg/transport/http"
	"github.com/sirupsen/logrus"
)

func main() {
	config := parseConfig()
	logger := createLogger(config)

	// metrics
	metrics := prometheus.NewRegistry()
	metrics.MustRegister(
		version.NewCollector("r8limiter"),
		prometheus.NewGoCollector(),
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
	)

	// rate limiter service
	ruleService, err := file.NewRuleService(config.RulesFile)
	if err != nil {
		logger.Fatalf("error creating rule service: ", err)
	}

	remoteCounterStorage, err := createCounterStorage(config, logger, metrics)
	if err != nil {
		logger.Fatalf("could not create remote counter storage: ", err)
	}

	counterService := limiter.NewCounterService(remoteCounterStorage, logger, metrics)
	limiterService := limiter.NewLimiterService(ruleService, counterService, logger, metrics)

	cancel := make(chan struct{})

	var g run.Group
	{
		g.Add(func() error {
			return counterService.Run(cancel)
		}, func(error) {})
	}
	{
		limiterGrpcServer := grpc.NewServer(
			limiterService,
			logger,
			metrics,
			grpc.WithListen(config.GrpcAddr))

		g.Add(func() error {
			return limiterGrpcServer.Start()
		}, func(error) {
			limiterGrpcServer.Stop()
		})
	}
	{
		limiterHTTPServer := http.New(
			limiterService,
			logger,
			metrics,
			http.WithListen(config.HttpAddr))

		g.Add(func() error {
			return limiterHTTPServer.Start()
		}, func(err error) {
			limiterHTTPServer.Stop(err)
		})
	}
	{
		g.Add(func() error {
			c := make(chan os.Signal, 1)
			signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
			sig := <-c
			return fmt.Errorf("received signal %s", sig)

		}, func(error) {
			close(cancel)
		})
	}

	logger.Info("exit", g.Run())
}

func parseConfig() configs.Config {
	fs := flag.NewFlagSet("r8limiter", flag.ExitOnError)
	var (
		grpcAddress       = fs.String("grpc-addr", ":8081", "grpc address")
		httpAddress       = fs.String("http-addr", ":8082", "http address")
		debugAddress      = fs.String("debug-addr", ":8083", "debug address for metrics and healthcheck")
		datastore         = fs.String("datastore", "redis", "datastore type (redis/cassandra)")
		cassandraHost     = fs.String("cassandra-host", "", "cassandra host")
		cassandraKeyspace = fs.String("cassandra-keyspace", "", "cassandra keyspace")
		redisAddress      = fs.String("redis-address", "", "redis address")
		redisDatabase     = fs.String("redis-database", "", "redis database")
		redisPassword     = fs.String("redis-password", "", "redis password")
		rulesFile         = fs.String("rules-file", "./env/rules.yaml", "rules file")
		logLevel          = fs.String("log-level", "info", "log level (panic, fatal, error, warn, info, debug, trace)")
	)
	ff.Parse(fs, os.Args[1:], ff.WithEnvVarPrefix("R8"))

	var config configs.Config
	{
		config.GrpcAddr = *grpcAddress
		config.HttpAddr = *httpAddress
		config.DebugAddr = *debugAddress
		config.Datastore = *datastore
		config.Cassandra.Hosts = *cassandraHost
		config.Cassandra.Keyspace = *cassandraKeyspace
		config.Redis.Address = *redisAddress
		config.Redis.Database = *redisDatabase
		config.Redis.Password = *redisPassword
		config.RulesFile = *rulesFile
		config.LogLevel = *logLevel
	}

	return config
}

func createLogger(config configs.Config) *logrus.Logger {
	logger := logrus.StandardLogger()
	level, err := logrus.ParseLevel(config.LogLevel)
	if err != nil {
		level = logrus.ErrorLevel
	}

	logger.Infof("setting log level to %v", level)
	logger.SetLevel(level)

	return logger
}

func createCounterStorage(config configs.Config, logger *logrus.Logger, metrics prometheus.Registerer) (limiter.CounterStorage, error) {
	switch config.Datastore {
	case "redis":
		redisClient := rediscli.NewClient(&rediscli.Options{
			Addr:     config.Redis.Address,
			Password: config.Redis.Password,
		})

		_, err := redisClient.Ping().Result()
		if err != nil {
			return nil, fmt.Errorf("could not connect to redis : %w", err)
		}

		return redis.NewStorage(redisClient, logger, metrics), nil

	case "cassandra":
		cluster := gocql.NewCluster(config.Cassandra.Hosts)
		cluster.Keyspace = config.Cassandra.Keyspace
		cluster.Consistency = gocql.LocalQuorum
		session, err := cluster.CreateSession()

		if err != nil {
			return nil, fmt.Errorf("could not create cassadra session : %w", err)
		}

		return cassandra.NewRemoteStorage(logger, session), nil
	default:
		return nil, fmt.Errorf("invalid datastore %s", config.Datastore)
	}
}
