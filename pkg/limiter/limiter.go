package limiter

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/samueltorres/r8limiter/pkg/counters"

	"github.com/samueltorres/r8limiter/pkg/rules"

	rl "github.com/envoyproxy/go-control-plane/envoy/api/v2/ratelimit"
	pb "github.com/envoyproxy/go-control-plane/envoy/service/ratelimit/v2"
	"github.com/sirupsen/logrus"
)

type LimiterService struct {
	rulesService   rules.RulesService
	counterService *counters.CounterService
	logger         *logrus.Logger
}

func NewLimiterService(
	rulesService rules.RulesService,
	counterService *counters.CounterService,
	logger *logrus.Logger) *LimiterService {
	return &LimiterService{
		rulesService:   rulesService,
		counterService: counterService,
		logger:         logger,
	}
}

func (l *LimiterService) ShouldRateLimit(ctx context.Context, req *pb.RateLimitRequest) (*pb.RateLimitResponse, error) {
	response := &pb.RateLimitResponse{
		Statuses: make([]*pb.RateLimitResponse_DescriptorStatus, len(req.Descriptors)),
	}

	for i, desc := range req.Descriptors {
		response.Statuses[i] = &pb.RateLimitResponse_DescriptorStatus{
			CurrentLimit: &pb.RateLimitResponse_RateLimit{},
		}

		limitDescriptor, err := l.rulesService.GetRatelimitRule(req.Domain, desc)
		if err != nil {
			if err == rules.ErrNoMatchedRule {
				return response, nil
			} else {
				return nil, err
			}
		}

		windowSize := timeUnitToWindowSize(limitDescriptor.Limit.Unit)
		now := time.Now().Unix()

		// get current and previous keys
		key := generateKey(desc, limitDescriptor, now/windowSize)
		prevKey := generateKey(desc, limitDescriptor, (now/windowSize)-1)

		// get current and previous buckets
		var hitsAddend uint32 = 1
		if req.HitsAddend > 0 {
			hitsAddend = req.HitsAddend
		}
		currUsage, _ := l.counterService.Add(ctx, key, hitsAddend, now+(windowSize*2))
		previousUsage, _ := l.counterService.Peek(ctx, prevKey)

		// calculate rate
		weight := float64(windowSize-(now%windowSize)) / float64(windowSize)
		rate := currUsage + uint32(float64(previousUsage)*weight)

		response.Statuses[i].CurrentLimit.RequestsPerUnit = limitDescriptor.Limit.Requests
		response.Statuses[i].CurrentLimit.Unit = timeUnitToPb(limitDescriptor.Limit.Unit)

		if rate > limitDescriptor.Limit.Requests {
			response.OverallCode = pb.RateLimitResponse_OVER_LIMIT
			response.Statuses[i].Code = pb.RateLimitResponse_OVER_LIMIT
			response.Statuses[i].LimitRemaining = 0
		} else {
			response.OverallCode = pb.RateLimitResponse_OK
			response.Statuses[i].Code = pb.RateLimitResponse_OK
			response.Statuses[i].LimitRemaining = limitDescriptor.Limit.Requests - rate
		}
	}

	return response, nil
}

func generateKey(desc *rl.RateLimitDescriptor, rule *rules.Rule, timeValue int64) string {
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
