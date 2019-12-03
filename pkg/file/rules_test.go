package file

import (
	"testing"

	"github.com/samueltorres/r8limiter/pkg/rules"

	rl "github.com/envoyproxy/go-control-plane/envoy/api/v2/ratelimit"
	"github.com/stretchr/testify/assert"
)

func BenchmarkGetRatelimitRule(b *testing.B) {
	rs, _ := NewRuleService("testdata/generic.yaml")

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

	for i := 0; i < b.N; i++ {
		rs.GetRatelimitRule("api-gateway", descriptor)
	}
}

func TestGetRatelimitRule(t *testing.T) {
	testCases := []struct {
		desc       string
		file       string
		domain     string
		descriptor rl.RateLimitDescriptor
		limit      rules.Limit
		error      error
	}{
		{
			desc:   "tc1",
			file:   "testdata/generic.yaml",
			domain: "api-gateway",
			descriptor: rl.RateLimitDescriptor{
				Entries: []*rl.RateLimitDescriptor_Entry{
					{
						Key:   "authorized",
						Value: "true",
					},
					{
						Key:   "user_id",
						Value: "1234",
					},
				},
			},
			limit: rules.Limit{
				Unit:     "hour",
				Requests: 1,
			},
			error: nil,
		},
		{
			desc:   "tc2",
			file:   "testdata/generic.yaml",
			domain: "api-gateway",
			descriptor: rl.RateLimitDescriptor{
				Entries: []*rl.RateLimitDescriptor_Entry{
					{
						Key:   "authorized",
						Value: "false",
					},
					{
						Key:   "user_id",
						Value: "1234",
					},
				},
			},
			limit: rules.Limit{
				Unit:     "hour",
				Requests: 2,
			},
			error: nil,
		},
		{
			desc:   "tc3",
			file:   "testdata/generic.yaml",
			domain: "api-gateway",
			descriptor: rl.RateLimitDescriptor{
				Entries: []*rl.RateLimitDescriptor_Entry{
					{
						Key:   "user_id",
						Value: "1234",
					},
				},
			},
			limit: rules.Limit{
				Unit:     "hour",
				Requests: 3,
			},
			error: nil,
		},
		{
			desc:   "tc4",
			file:   "testdata/generic.yaml",
			domain: "api-gateway",
			descriptor: rl.RateLimitDescriptor{
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
			},
			limit: rules.Limit{
				Unit:     "hour",
				Requests: 4,
			},
			error: nil,
		},
		{
			desc:   "tc5",
			file:   "testdata/generic.yaml",
			domain: "api-gateway",
			descriptor: rl.RateLimitDescriptor{
				Entries: []*rl.RateLimitDescriptor_Entry{
					{
						Key:   "authorized",
						Value: "false",
					},
					{
						Key:   "tenant",
						Value: "10000",
					},
				},
			},
			limit: rules.Limit{
				Unit:     "hour",
				Requests: 5,
			},
			error: nil,
		},
		{
			desc:   "tc6",
			file:   "testdata/generic.yaml",
			domain: "api-gateway",
			descriptor: rl.RateLimitDescriptor{
				Entries: []*rl.RateLimitDescriptor_Entry{
					{
						Key:   "random",
						Value: "value",
					},
				},
			},
			limit: rules.Limit{},
			error: rules.ErrNoMatchedRule,
		},
		{
			desc:   "tc7",
			file:   "testdata/generic.yaml",
			domain: "api-gateway",
			descriptor: rl.RateLimitDescriptor{
				Entries: []*rl.RateLimitDescriptor_Entry{
					{
						Key:   "authorized",
						Value: "true",
					},
				},
			},
			limit: rules.Limit{},
			error: rules.ErrNoMatchedRule,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {

			rs, err := NewRuleService(tC.file)
			if err != nil {
				assert.FailNow(t, "could not initialize rule service", err)
			}

			descriptor, err := rs.GetRatelimitRule(tC.domain, &tC.descriptor)

			if !assert.Equal(t, tC.error, err) {
				assert.FailNow(t, "")
			}

			if tC.error == nil {
				assert.Equal(t, tC.limit.Unit, descriptor.Limit.Unit, "invalid limit unit")
				assert.Equal(t, tC.limit.Requests, descriptor.Limit.Requests, "invalid request limit number")
			}
		})
	}
}
