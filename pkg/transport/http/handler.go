package http

import (
	"context"
	"encoding/json"
	"net/http"

	pb "github.com/envoyproxy/go-control-plane/envoy/service/ratelimit/v2"
	"github.com/samueltorres/r8limiter/pkg/limiter"
	"github.com/sirupsen/logrus"
)

type rateLimitingHandler struct {
	limiter *limiter.LimiterService
	logger  *logrus.Logger
}

func (h *rateLimitingHandler) handleRateLimit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req pb.RateLimitRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		h.logger.Info("could not decode request", err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	res, err := h.limiter.ShouldRateLimit(context.Background(), &req)
	if err != nil {
		h.logger.Info("could not get rate limit", err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(res); err != nil {
		panic(err)
	}
}
