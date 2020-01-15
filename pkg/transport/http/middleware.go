package http

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/urfave/negroni"
)

type metricsMiddleware struct {
	requestCounter *prometheus.CounterVec
	requestLatency *prometheus.HistogramVec
}

func NewMetricsMiddleware(registerer prometheus.Registerer) *metricsMiddleware {
	// metrics
	requestCounter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_request_total",
			Help: "Total http requests counter",
		},
		[]string{"handler", "method", "status"})

	requestLatency := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Duration of the http requests",
			Buckets: prometheus.ExponentialBuckets(0.00005, 2, 12),
		},
		[]string{"handler", "method", "status"})

	registerer.MustRegister(requestCounter, requestLatency)

	return &metricsMiddleware{
		requestCounter: requestCounter,
		requestLatency: requestLatency,
	}
}

func (m *metricsMiddleware) Handler(handler string, next http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		ww := negroni.NewResponseWriter(w)
		next.ServeHTTP(ww, r)

		status := strconv.Itoa(ww.Status())
		m.requestCounter.WithLabelValues(handler, r.Method, status).Inc()
		m.requestLatency.WithLabelValues(handler, r.Method, status).Observe(time.Since(start).Seconds())
	})
}
