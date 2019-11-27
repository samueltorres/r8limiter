package grpc

import (
	"context"

	"github.com/sirupsen/logrus"

	pb "github.com/envoyproxy/go-control-plane/envoy/service/ratelimit/v2"
	"github.com/samueltorres/r8limiter/pkg/limiter"
)

type Server struct {
	limiterService *limiter.LimiterService
	logger         *logrus.Logger
}

func NewServer(limiterService *limiter.LimiterService, logger *logrus.Logger) *Server {
	return &Server{limiterService: limiterService, logger: logger}
}

func (s *Server) ShouldRateLimit(ctx context.Context, req *pb.RateLimitRequest) (*pb.RateLimitResponse, error) {
	return s.limiterService.ShouldRateLimit(ctx, req)
}
