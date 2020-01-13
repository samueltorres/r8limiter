package http

import (
	"context"
	"net/http"
	"net/http/pprof"

	"github.com/julienschmidt/httprouter"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/samueltorres/r8limiter/pkg/limiter"
	"github.com/sirupsen/logrus"
)

type Server struct {
	handler         *rateLimitingHandler
	srv             *http.Server
	router          *httprouter.Router
	opts            options
	metricsRegistry *prometheus.Registry
	logger          *logrus.Logger
}

func New(
	limiter *limiter.LimiterService,
	logger *logrus.Logger,
	metricsRegistry *prometheus.Registry,
	opts ...Option) *Server {

	options := options{}
	for _, o := range opts {
		o.apply(&options)
	}

	handler := &rateLimitingHandler{limiter: limiter, logger: logger}
	router := httprouter.New()

	server := &Server{
		handler:         handler,
		router:          router,
		metricsRegistry: metricsRegistry,
		opts:            options,
		logger:          logger,
		srv:             &http.Server{Addr: options.listen, Handler: router},
	}

	server.registerRoutes()

	return server
}

func (s *Server) registerRoutes() {

	// middlewares
	mm := NewMetricsMiddleware(s.metricsRegistry)

	// rate limiting
	s.router.HandlerFunc("POST", "/ratelimit", mm.Handler("/rateLimit", s.handler.handleRateLimit))

	// metrics
	s.router.Handler("GET", "/metrics", promhttp.HandlerFor(s.metricsRegistry, promhttp.HandlerOpts{}))

	// profiling
	s.router.HandlerFunc("GET", "/debug/pprof/", pprof.Index)
	s.router.HandlerFunc("GET", "/debug/pprof/cmdline", pprof.Cmdline)
	s.router.HandlerFunc("GET", "/debug/pprof/profile", pprof.Profile)
	s.router.HandlerFunc("GET", "/debug/pprof/symbol", pprof.Symbol)
	s.router.HandlerFunc("GET", "/debug/pprof/trace", pprof.Trace)

	// healthcheck
	// todo: add healthcheck handlers
}

func (s *Server) Start() error {
	s.logger.Info("listening for http address ", s.opts.listen)
	return errors.Wrap(s.srv.ListenAndServe(), "serve http")
}

func (s *Server) Stop(err error) {
	if err == http.ErrServerClosed {
		s.logger.Info("internal server closed unexpectedly")
		return
	}

	defer s.logger.Info("internal server shutdown", "err", err)

	if s.opts.gracePeriod == 0 {
		s.srv.Close()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.opts.gracePeriod)
	defer cancel()

	if err := s.srv.Shutdown(ctx); err != nil {
		s.logger.Error("internal server shut down failed", "err", err)
	}
}
