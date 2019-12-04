package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/peterbourgon/ff"
	"github.com/prometheus/client_golang/prometheus"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/samueltorres/r8limiter/pkg/counters"

	pb "github.com/envoyproxy/go-control-plane/envoy/service/ratelimit/v2"
	rediscli "github.com/go-redis/redis/v7"
	"github.com/gocql/gocql"
	"github.com/gorilla/mux"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/oklog/run"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/samueltorres/r8limiter/pkg/cassandra"
	"github.com/samueltorres/r8limiter/pkg/configs"
	"github.com/samueltorres/r8limiter/pkg/file"
	"github.com/samueltorres/r8limiter/pkg/limiter"
	"github.com/samueltorres/r8limiter/pkg/redis"
	limiterGrpc "github.com/samueltorres/r8limiter/pkg/transport/grpc"
	limiterHttp "github.com/samueltorres/r8limiter/pkg/transport/http"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

func main() {
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
		rulesFile         = fs.String("rules-file", "/env/rules.yaml", "rules file")
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
	}

	// todo: add log levels
	logger := logrus.New()
	ctx, cancel := context.WithCancel(context.Background())

	// Rate limiter
	var limiterService *limiter.LimiterService
	{
		ruleService, err := file.NewRuleService(config.RulesFile)
		if err != nil {
			logger.Error("error creating rule service: ", err)
			os.Exit(1)
		}

		remoteCounterStorage, err := CreateRemoteCounterStorage(config, logger)
		if err != nil {
			logger.Error("could not create remote counter storage: ", err)
			os.Exit(1)
		}

		counterService := counters.NewCounterService(remoteCounterStorage, logger, ctx)
		limiterService = limiter.NewLimiterService(ruleService, counterService, logger)
	}

	var g run.Group
	{
		limiterGrpcServer := limiterGrpc.NewServer(limiterService, logger)

		grpcServer := grpc.NewServer(
			grpc.UnaryInterceptor(grpc_prometheus.UnaryServerInterceptor),
		)
		pb.RegisterRateLimitServiceServer(grpcServer, limiterGrpcServer)

		histogramOpts := grpc_prometheus.WithHistogramBuckets(prometheus.ExponentialBuckets(0.00005, 2, 12))
		grpc_prometheus.EnableHandlingTimeHistogram(histogramOpts)
		grpc_prometheus.Register(grpcServer)

		lis, err := net.Listen("tcp", config.GrpcAddr)
		if err != nil {
			logger.Error("could not listen on tcp, error: ", err)
			os.Exit(1)
		}
		g.Add(func() error {
			logger.Info("listening grpc server on addr", config.GrpcAddr)
			return grpcServer.Serve(lis)
		}, func(error) {
			grpcServer.Stop()
		})
	}
	{
		srv := &http.Server{Addr: config.DebugAddr}
		g.Add(func() error {
			logger.Info("listening http debug server on addr", config.DebugAddr)
			r := mux.NewRouter()
			r.Path("/metrics").Handler(promhttp.Handler())
			srv.Handler = r
			return srv.ListenAndServe()
		}, func(error) {
			srv.Close()
		})
	}
	{
		httpTransport := limiterHttp.NewHttpServer(limiterService, logger)
		httpServer := http.Server{Addr: config.HttpAddr}
		httpServer.Handler = httpTransport

		g.Add(func() error {
			logger.Info("listening http server on addr", config.HttpAddr)
			return httpServer.ListenAndServe()
		}, func(error) {
			httpServer.Close()
		})
	}
	{
		g.Add(func() error {
			c := make(chan os.Signal, 1)
			signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
			select {
			case sig := <-c:
				return fmt.Errorf("received signal %s", sig)
			case <-ctx.Done():
				return ctx.Err()
			}
		}, func(error) {
			cancel()
		})
	}

	logger.Info("exit", g.Run())
}

func CreateRemoteCounterStorage(config configs.Config, logger *logrus.Logger) (counters.RemoteCounterStorage, error) {
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

		return redis.NewRemoteStorage(redisClient, logger), nil

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
