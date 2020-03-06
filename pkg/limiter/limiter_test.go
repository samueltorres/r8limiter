package limiter

import (
	"context"
	"io/ioutil"
	"testing"

	rl "github.com/envoyproxy/go-control-plane/envoy/api/v2/ratelimit"
	pb "github.com/envoyproxy/go-control-plane/envoy/service/ratelimit/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/mock"
)

func BenchmarkLimiterService_GenerateKey(b *testing.B) {
	descriptor := &rl.RateLimitDescriptor{
		Entries: []*rl.RateLimitDescriptor_Entry{
			{
				Key:   "authorized",
				Value: "true",
			},
			{
				Key:   "tenant",
				Value: "10000",
			},
		},
	}

	rule := &Rule{
		Labels: []DescriptorLabel{
			{
				Key:   "authorized",
				Value: "true",
			},
		},
		Limit: Limit{
			Requests: 20,
			Unit:     "second",
		},
	}

	var time int64 = 20
	for i := 0; i < b.N; i++ {
		generateKey(descriptor, rule, time)
	}
}

func TestLimiterService_GenerateKey(t *testing.T) {
	testCases := []struct {
		desc       string
		descriptor *rl.RateLimitDescriptor
		rule       *Rule
		time       int64
		want       string
	}{
		{
			desc: "with all parameters",
			descriptor: &rl.RateLimitDescriptor{
				Entries: []*rl.RateLimitDescriptor_Entry{
					{
						Key:   "authorized",
						Value: "true",
					},
				},
			},
			rule: &Rule{
				Labels: []DescriptorLabel{
					{
						Key: "authorized",
					},
				},
				Limit: Limit{
					Requests: 20,
					Unit:     "second",
				},
			},
			time: 1,
			want: "authorized.true:second:1",
		},
		{
			desc: "with missing parameters",
			descriptor: &rl.RateLimitDescriptor{
				Entries: []*rl.RateLimitDescriptor_Entry{
					{
						Key:   "authorized",
						Value: "true",
					},
					{
						Key:   "tenant",
						Value: "10",
					},
				},
			},
			rule: &Rule{
				Labels: []DescriptorLabel{
					{
						Key: "authorized",
					},
				},
				Limit: Limit{
					Requests: 20,
					Unit:     "second",
				},
			},
			time: 1,
			want: "authorized.true:second:1",
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got := generateKey(tC.descriptor, tC.rule, tC.time)
			if tC.want != got {
				t.Errorf("got %v, want %v", got, tC.want)
			}
		})
	}
}

func TestLimiterService_ShouldRateLimit(t *testing.T) {
	testCases := []struct {
		desc string

		rule    *Rule
		ruleErr error

		getCounterResponse            uint32
		getCounterResponseErr         error
		getCounterFromStorageResponse uint32
		getCounterFromStorageErr      error

		incrementCounterResponse          uint32
		incrementCounterResponseErr       error
		incrementCounterOnStorageResponse uint32
		incrementCounterOnStorageErr      error

		request *pb.RateLimitRequest

		want    *pb.RateLimitResponse
		wantErr error
	}{
		{
			desc:    "No Ratelimit rule for descriptor returns unkown response",
			ruleErr: ErrNoMatchedRule,
			request: &pb.RateLimitRequest{
				Domain: "domain",
				Descriptors: []*rl.RateLimitDescriptor{
					{
						Entries: []*rl.RateLimitDescriptor_Entry{
							{
								Key:   "tenant",
								Value: "10000",
							},
						},
					},
				},
				HitsAddend: 1,
			},
			want: &pb.RateLimitResponse{
				OverallCode: pb.RateLimitResponse_UNKNOWN,
				Statuses: []*pb.RateLimitResponse_DescriptorStatus{
					{
						Code: pb.RateLimitResponse_UNKNOWN,
					},
				},
			},
		},
		{
			desc: "Ratelimit rule has sync rate 0, increments on storage and returns ok result",
			rule: &Rule{
				Name: "rule_name",
				Labels: []DescriptorLabel{
					{
						Key: "tenant",
					},
				},
				SyncRate: 0,
				Limit: Limit{
					Requests: 1000,
					Unit:     "minute",
				},
			},
			getCounterFromStorageResponse:     100,
			incrementCounterOnStorageResponse: 100,
			request: &pb.RateLimitRequest{
				Domain: "domain",
				Descriptors: []*rl.RateLimitDescriptor{
					{
						Entries: []*rl.RateLimitDescriptor_Entry{
							{
								Key:   "tenant",
								Value: "10000",
							},
						},
					},
				},
				HitsAddend: 1,
			},
			want: &pb.RateLimitResponse{
				OverallCode: pb.RateLimitResponse_OK,
				Statuses: []*pb.RateLimitResponse_DescriptorStatus{
					{
						Code: pb.RateLimitResponse_OK,
						CurrentLimit: &pb.RateLimitResponse_RateLimit{
							RequestsPerUnit: 1000,
							Unit:            pb.RateLimitResponse_RateLimit_MINUTE,
						},
						LimitRemaining: 900,
					},
				},
			},
		},
		{
			desc: "Ratelimit rule has sync rate bigger than 0, increments on counter service and returns ok result",
			rule: &Rule{
				Name: "rule_name",
				Labels: []DescriptorLabel{
					{
						Key: "tenant",
					},
				},
				SyncRate: 5,
				Limit: Limit{
					Requests: 1000,
					Unit:     "minute",
				},
			},
			getCounterResponse:       100,
			incrementCounterResponse: 100,
			request: &pb.RateLimitRequest{
				Domain: "domain",
				Descriptors: []*rl.RateLimitDescriptor{
					{
						Entries: []*rl.RateLimitDescriptor_Entry{
							{
								Key:   "tenant",
								Value: "10000",
							},
						},
					},
				},
				HitsAddend: 1,
			},
			want: &pb.RateLimitResponse{
				OverallCode: pb.RateLimitResponse_OK,
				Statuses: []*pb.RateLimitResponse_DescriptorStatus{
					{
						Code: pb.RateLimitResponse_OK,
						CurrentLimit: &pb.RateLimitResponse_RateLimit{
							RequestsPerUnit: 1000,
							Unit:            pb.RateLimitResponse_RateLimit_MINUTE,
						},
						LimitRemaining: 900,
					},
				},
			},
		},
		{
			desc: "Current minute is going to be rate limited using predicted request rate algorithm",
			rule: &Rule{
				Name: "rule_name",
				Labels: []DescriptorLabel{
					{
						Key: "tenant",
					},
				},
				SyncRate: 5,
				Limit: Limit{
					Requests: 1000,
					Unit:     "minute",
				},
			},
			getCounterResponse:       1000,
			incrementCounterResponse: 999,
			request: &pb.RateLimitRequest{
				Domain: "domain",
				Descriptors: []*rl.RateLimitDescriptor{
					{
						Entries: []*rl.RateLimitDescriptor_Entry{
							{
								Key:   "tenant",
								Value: "10000",
							},
						},
					},
				},
				HitsAddend: 1,
			},
			want: &pb.RateLimitResponse{
				OverallCode: pb.RateLimitResponse_OVER_LIMIT,
				Statuses: []*pb.RateLimitResponse_DescriptorStatus{
					{
						Code: pb.RateLimitResponse_OVER_LIMIT,
						CurrentLimit: &pb.RateLimitResponse_RateLimit{
							RequestsPerUnit: 1000,
							Unit:            pb.RateLimitResponse_RateLimit_MINUTE,
						},
						LimitRemaining: 0,
					},
				},
			},
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			// arrange
			rulesServiceMock := new(ruleServiceMock)
			rulesServiceMock.On("GetRatelimitRule", tC.request.Domain, tC.request.Descriptors[0]).Return(tC.rule, tC.ruleErr)

			counterServiceMock := new(counterServiceMock)
			if tC.rule != nil {
				counterServiceMock.On("Increment", mock.Anything, mock.Anything, tC.request.HitsAddend, mock.Anything, tC.rule.SyncRate).Return(tC.incrementCounterResponse, tC.incrementCounterResponseErr)
				counterServiceMock.On("IncrementOnStorage", mock.Anything, mock.Anything, tC.request.HitsAddend, mock.Anything).Return(tC.incrementCounterOnStorageResponse, tC.incrementCounterOnStorageErr)
				counterServiceMock.On("Get", mock.Anything, mock.Anything).Return(tC.getCounterResponse, tC.getCounterResponseErr)
				counterServiceMock.On("GetFromStorage", mock.Anything, mock.Anything).Return(tC.getCounterFromStorageResponse, tC.getCounterFromStorageErr)
			}

			limiterService := NewLimiterService(rulesServiceMock, counterServiceMock, newNullLogger(), prometheus.NewRegistry())

			// act
			got, _ := limiterService.ShouldRateLimit(context.Background(), tC.request)

			// assert
			if tC.want.OverallCode != got.OverallCode {
				t.Errorf("got overall code %v, want %v", got.OverallCode, tC.want.OverallCode)
			}

			for i := 0; i < len(tC.want.Statuses); i++ {
				if tC.want.Statuses[i].LimitRemaining != got.Statuses[i].LimitRemaining {
					t.Errorf("got limit remaining %v on status index %v, want %v", got.Statuses[i].LimitRemaining, i, tC.want.Statuses[i].LimitRemaining)
				}

				if tC.want.Statuses[i].CurrentLimit != nil {
					if tC.want.Statuses[i].CurrentLimit.RequestsPerUnit != got.Statuses[i].CurrentLimit.RequestsPerUnit {
						t.Errorf("got requests per unit %v on status index %v, want %v", got.Statuses[i].CurrentLimit.RequestsPerUnit, i, tC.want.Statuses[i].CurrentLimit.RequestsPerUnit)
					}

					if tC.want.Statuses[i].CurrentLimit.Unit != got.Statuses[i].CurrentLimit.Unit {
						t.Errorf("got unit %v on status index %v, want %v", got.Statuses[i].CurrentLimit.Unit, i, tC.want.Statuses[i].CurrentLimit.Unit)
					}
				}
			}
		})
	}
}

type ruleServiceMock struct {
	mock.Mock
}

func (r *ruleServiceMock) GetRatelimitRule(domain string, requestDescriptor *rl.RateLimitDescriptor) (*Rule, error) {
	args := r.Called(domain, requestDescriptor)
	return args.Get(0).(*Rule), args.Error(1)
}

type counterServiceMock struct {
	mock.Mock
}

func (c *counterServiceMock) Increment(ctx context.Context, key string, n uint32, ttl int64, syncRate int) (uint32, error) {
	args := c.Called(ctx, key, n, ttl, syncRate)
	return args.Get(0).(uint32), args.Error(1)
}

func (c *counterServiceMock) IncrementOnStorage(ctx context.Context, key string, n uint32, ttl int64) (uint32, error) {
	args := c.Called(ctx, key, n, ttl)
	return args.Get(0).(uint32), args.Error(1)
}

func (c *counterServiceMock) Get(ctx context.Context, key string) (uint32, error) {
	args := c.Called(ctx, key)
	return args.Get(0).(uint32), args.Error(1)
}

func (c *counterServiceMock) GetFromStorage(ctx context.Context, key string) (uint32, error) {
	args := c.Called(ctx, key)
	return args.Get(0).(uint32), args.Error(1)
}

func newNullLogger() *logrus.Logger {
	logger := logrus.New()
	logger.Out = ioutil.Discard

	return logger
}
