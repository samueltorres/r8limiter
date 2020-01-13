package grpc

import (
	"context"
	"math"
	"net"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/envoyproxy/go-control-plane/envoy/service/ratelimit/v2"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/samueltorres/r8limiter/pkg/limiter"
)

type Server struct {
	srv            *grpc.Server
	listener       net.Listener
	opts           options
	limiterService *limiter.LimiterService
	logger         *logrus.Logger
}

func NewServer(limiterService *limiter.LimiterService, logger *logrus.Logger, reg *prometheus.Registry, opts ...Option) *Server {
	options := options{}
	for _, o := range opts {
		o.apply(&options)
	}

	met := grpc_prometheus.NewServerMetrics()
	met.EnableHandlingTimeHistogram(
		grpc_prometheus.WithHistogramBuckets(prometheus.ExponentialBuckets(0.00005, 2, 12)),
	)
	reg.MustRegister(met)

	grpcOpts := []grpc.ServerOption{
		grpc.MaxSendMsgSize(math.MaxInt32),
		grpc.UnaryInterceptor(
			met.UnaryServerInterceptor(),
		),
		grpc.StreamInterceptor(
			met.StreamServerInterceptor(),
		),
	}

	if options.tlsConfig != nil {
		grpcOpts = append(grpcOpts, grpc.Creds(credentials.NewTLS(options.tlsConfig)))
	}

	s := grpc.NewServer(grpcOpts...)

	rateLimtitServer := &Server{
		limiterService: limiterService,
		logger:         logger,
		opts:           options,
		srv:            s,
	}

	pb.RegisterRateLimitServiceServer(s, rateLimtitServer)
	met.InitializeMetrics(s)

	return rateLimtitServer
}

func (s *Server) ShouldRateLimit(ctx context.Context, req *pb.RateLimitRequest) (*pb.RateLimitResponse, error) {
	return s.limiterService.ShouldRateLimit(ctx, req)
}

func (s *Server) Start() error {
	l, err := net.Listen("tcp", s.opts.listen)
	if err != nil {
		return errors.Wrapf(err, "listen grpc on address %s", s.opts.listen)
	}
	s.listener = l

	s.logger.Info("listening for grpc address ", s.opts.listen)
	return errors.Wrap(s.srv.Serve(s.listener), "serve gRPC")
}

func (s *Server) Stop() (err error) {
	defer s.logger.Info("internal server shutdown", "err", err)

	if s.opts.gracePeriod == 0 {
		s.srv.Stop()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.opts.gracePeriod)
	defer cancel()

	stopped := make(chan struct{})
	go func() {
		s.logger.Info("gracefully stopping internal server")
		s.srv.GracefulStop() // Also closes s.listener.
		close(stopped)
	}()

	select {
	case <-ctx.Done():
		s.logger.Info("grace period exceeded enforcing shutdown")
		s.srv.Stop()
	case <-stopped:
		cancel()
	}

	return
}
