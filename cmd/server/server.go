package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"

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
	"github.com/spf13/viper"
	"google.golang.org/grpc"
)

func main() {
	configFile := flag.String("config-file", "./env/config.yaml", "config file")
	flag.Parse()

	serverConfig, err := CreateServerConfig(*configFile)
	if err != nil {
		panic(err)
	}

	// todo: add log levels
	logger := logrus.New()
	ctx, cancel := context.WithCancel(context.Background())

	// Rate limiter
	var limiterService *limiter.LimiterService
	{
		ruleService, err := file.NewRuleService(serverConfig.RulesFile)
		if err != nil {
			logger.Error("error creating config manager: ", err)
			os.Exit(1)
		}

		remoteCounterStorage, err := CreateRemoteCounterStorage(serverConfig, logger)
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

		lis, err := net.Listen("tcp", serverConfig.GrpcAddr)
		if err != nil {
			logger.Error("could not listen on tcp, error: ", err)
			os.Exit(1)
		}
		g.Add(func() error {
			logger.Info("listening grpc server on addr", serverConfig.GrpcAddr)
			return grpcServer.Serve(lis)
		}, func(error) {
			grpcServer.Stop()
		})
	}
	{
		srv := &http.Server{Addr: serverConfig.DebugAddr}
		g.Add(func() error {
			logger.Info("listening http debug server on addr", serverConfig.DebugAddr)
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
		httpServer := http.Server{Addr: serverConfig.HttpAddr}
		httpServer.Handler = httpTransport

		g.Add(func() error {
			logger.Info("listening http server on addr", serverConfig.HttpAddr)
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

func CreateServerConfig(configFile string) (configs.Config, error) {
	viper.SetConfigFile(configFile)
	viper.SetConfigType("yaml")
	viper.AutomaticEnv()
	viper.SetDefault("grpcAddr", ":8081")
	viper.SetDefault("httpAddr", ":8082")
	viper.SetDefault("debugAddr", ":8083")
	viper.SetDefault("datastore", "redis")

	err := viper.ReadInConfig()
	if err != nil {
		return configs.Config{}, fmt.Errorf("fatal error on config file: %s", err)
	}

	var serverConfig configs.Config
	err = viper.Unmarshal(&serverConfig)
	if err != nil {
		return configs.Config{}, fmt.Errorf("fatal error unmarshalling config file: %s", err)
	}

	return serverConfig, nil
}

func CreateRemoteCounterStorage(config configs.Config, logger *logrus.Logger) (counters.RemoteCounterStorage, error) {
	switch config.Datastore {
	case "redis":
		redisClient := rediscli.NewClient(&rediscli.Options{
			Addr: config.Redis.Address,
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
