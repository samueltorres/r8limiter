package limiter

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	rl "github.com/envoyproxy/go-control-plane/envoy/api/v2/ratelimit"
	pb "github.com/envoyproxy/go-control-plane/envoy/service/ratelimit/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

type metrics struct {
	okResp      prometheus.Counter
	limitedResp prometheus.Counter
	unkownResp  prometheus.Counter
}

func newMetrics(r prometheus.Registerer) *metrics {
	var m metrics

	m.okResp = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ratelimit_ok_responses",
		Help: "Total ok responses",
	})

	m.limitedResp = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ratelimit_limited_requests",
		Help: "Total limited responses",
	})

	m.unkownResp = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ratelimit_unknown_requests",
		Help: "Total unknown responses",
	})

	r.MustRegister(m.okResp, m.limitedResp, m.unkownResp)
	return &m
}

type LimiterService struct {
	rulesService   RulesService
	counterService CounterService
	logger         *logrus.Logger
	metrics        *metrics
}

func NewLimiterService(
	rulesService RulesService,
	counterService CounterService,
	logger *logrus.Logger,
	registerer prometheus.Registerer) *LimiterService {

	metrics := newMetrics(registerer)

	return &LimiterService{
		rulesService:   rulesService,
		counterService: counterService,
		logger:         logger,
		metrics:        metrics,
	}
}

func (l *LimiterService) ShouldRateLimit(ctx context.Context, req *pb.RateLimitRequest) (response *pb.RateLimitResponse, err error) {
	defer func() {
		if err != nil {
			return
		}

		switch response.OverallCode {
		case pb.RateLimitResponse_OVER_LIMIT:
			l.metrics.limitedResp.Inc()
		case pb.RateLimitResponse_OK:
			l.metrics.okResp.Inc()
		case pb.RateLimitResponse_UNKNOWN:
			l.metrics.unkownResp.Inc()
		}
	}()

	response = &pb.RateLimitResponse{
		Statuses: make([]*pb.RateLimitResponse_DescriptorStatus, len(req.Descriptors)),
	}

	for i, desc := range req.Descriptors {
		response.Statuses[i] = &pb.RateLimitResponse_DescriptorStatus{
			CurrentLimit: &pb.RateLimitResponse_RateLimit{},
		}

		rule, err := l.rulesService.GetRatelimitRule(req.Domain, desc)
		if err != nil {
			if err == ErrNoMatchedRule {
				continue
			}
		}

		windowSize := timeUnitToWindowSize(rule.Limit.Unit)
		now := time.Now().Unix()

		// service does not allow 0 hits addend
		if req.HitsAddend == 0 {
			req.HitsAddend = 1
		}

		// get current bucket usage
		var currUsage uint32
		{
			key := generateKey(desc, rule, now/windowSize)

			// current usage must be available 2 buckets later for interpolation
			ttl := now + (windowSize * 2)

			if rule.SyncRate == 0 {
				currUsage, err = l.counterService.IncrementOnStorage(ctx, key, req.HitsAddend, ttl)
				if err != nil {
					return nil, err
				}
			} else {
				currUsage, err = l.counterService.Increment(ctx, key, req.HitsAddend, ttl, rule.SyncRate)
				if err != nil {
					return nil, err
				}
			}
		}

		// get previous bucket usage
		var previousUsage uint32
		{
			key := generateKey(desc, rule, (now/windowSize)-1)
			if rule.SyncRate == 0 {
				previousUsage, _ = l.counterService.GetFromStorage(ctx, key)
			} else {
				previousUsage, _ = l.counterService.Get(ctx, key)
			}
		}

		// calculate rate
		weight := float64(windowSize-(now%windowSize)) / float64(windowSize)
		rate := currUsage + uint32(float64(previousUsage)*weight)

		response.Statuses[i].CurrentLimit.RequestsPerUnit = rule.Limit.Requests
		response.Statuses[i].CurrentLimit.Unit = timeUnitToPb(rule.Limit.Unit)

		if rate > rule.Limit.Requests {
			response.OverallCode = pb.RateLimitResponse_OVER_LIMIT
			response.Statuses[i].Code = pb.RateLimitResponse_OVER_LIMIT
			response.Statuses[i].LimitRemaining = 0
		} else {
			// if its already over-limit, we shouldnt tag it as ok
			if response.OverallCode != pb.RateLimitResponse_OVER_LIMIT {
				response.OverallCode = pb.RateLimitResponse_OK
			}

			response.Statuses[i].Code = pb.RateLimitResponse_OK
			response.Statuses[i].LimitRemaining = rule.Limit.Requests - currUsage
		}
	}

	return response, nil
}

func generateKey(desc *rl.RateLimitDescriptor, rule *Rule, timeValue int64) string {
	usedLimitDescriptorLabels := make(map[string]bool)
	for _, label := range rule.Labels {
		usedLimitDescriptorLabels[label.Key] = true
	}

	descriptorKeyValues := make([]string, 0, len(desc.Entries))
	for _, entry := range desc.Entries {
		if _, exists := usedLimitDescriptorLabels[entry.Key]; exists {
			descriptorKeyValues = append(descriptorKeyValues, entry.Key+"."+entry.Value)
		}
	}
	sort.Strings(descriptorKeyValues)

	return strings.Join(descriptorKeyValues, "_") + ":" + rule.Limit.Unit + ":" + strconv.FormatInt(timeValue, 10)
}
