package limiter

import (
	"testing"

	"github.com/bmizerany/assert"
	rl "github.com/envoyproxy/go-control-plane/envoy/api/v2/ratelimit"
	"github.com/samueltorres/r8limiter/pkg/rules"
)

// GenerateKey tests

func BenchmarkGenerateKey(b *testing.B) {
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

	rule := &rules.Rule{
		Labels: []rules.DescriptorLabel{
			{
				Key:   "authorized",
				Value: "true",
			},
		},
		Limit: rules.Limit{
			Requests: 20,
			Unit:     "second",
		},
	}

	var time int64 = 20
	for i := 0; i < b.N; i++ {
		generateKey(descriptor, rule, time)
	}
}

func TestGenerateKey(t *testing.T) {
	testCases := []struct {
		desc        string
		descriptor  *rl.RateLimitDescriptor
		rule        *rules.Rule
		time        int64
		expectedKey string
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
			rule: &rules.Rule{
				Labels: []rules.DescriptorLabel{
					{
						Key: "authorized",
					},
				},
				Limit: rules.Limit{
					Requests: 20,
					Unit:     "second",
				},
			},
			time:        1,
			expectedKey: "authorized.true:second:1",
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
			rule: &rules.Rule{
				Labels: []rules.DescriptorLabel{
					{
						Key: "authorized",
					},
				},
				Limit: rules.Limit{
					Requests: 20,
					Unit:     "second",
				},
			},
			time:        1,
			expectedKey: "authorized.true:second:1",
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			key := generateKey(tC.descriptor, tC.rule, tC.time)
			assert.Equal(t, tC.expectedKey, key)
		})
	}
}
