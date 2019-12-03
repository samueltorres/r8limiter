package http

import (
	"context"
	"encoding/json"
	"github.com/sirupsen/logrus"
	"net/http"

	pb "github.com/envoyproxy/go-control-plane/envoy/service/ratelimit/v2"
	"github.com/gorilla/mux"
	"github.com/samueltorres/r8limiter/pkg/limiter"
)

type Server struct {
	limiter *limiter.LimiterService
	router  *mux.Router
	logger  *logrus.Logger
}

func NewHttpServer(limiter *limiter.LimiterService, logger *logrus.Logger) *Server {
	server := &Server{
		router:  mux.NewRouter(),
		limiter: limiter,
		logger:  logger,
	}
	server.registerRoutes()
	return server
}

func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	s.router.ServeHTTP(w, req)
}

func (s *Server) registerRoutes() {
	s.router.HandleFunc("/ratelimit", s.handleRateLimit).Methods("POST")
}

func (s *Server) handleRateLimit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req pb.RateLimitRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		s.logger.Info("could not decode request", err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	s.logger.Info("request", req)

	res, err := s.limiter.ShouldRateLimit(context.Background(), &req)
	if err != nil {
		s.logger.Info("could not get rate limit", err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	s.logger.Info("response", res)

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(res); err != nil {
		panic(err)
	}
}
