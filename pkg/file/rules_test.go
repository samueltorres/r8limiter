package file

import (
	"testing"

	rl "github.com/envoyproxy/go-control-plane/envoy/api/v2/ratelimit"
	"github.com/samueltorres/r8limiter/pkg/limiter"
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
		want       *limiter.Limit
		wantErr    error
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
			want: &limiter.Limit{
				Unit:     "hour",
				Requests: 1,
			},
			wantErr: nil,
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
			want: &limiter.Limit{
				Unit:     "hour",
				Requests: 2,
			},
			wantErr: nil,
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
			want: &limiter.Limit{
				Unit:     "hour",
				Requests: 3,
			},
			wantErr: nil,
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
			want: &limiter.Limit{
				Unit:     "hour",
				Requests: 4,
			},
			wantErr: nil,
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
			want: &limiter.Limit{
				Unit:     "hour",
				Requests: 5,
			},
			wantErr: nil,
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
			want:    nil,
			wantErr: limiter.ErrNoMatchedRule,
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
			want:    nil,
			wantErr: limiter.ErrNoMatchedRule,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {

			rs, err := NewRuleService(tC.file)
			if err != nil {
				assert.FailNow(t, "could not initialize rule service", err)
			}

			got, err := rs.GetRatelimitRule(tC.domain, &tC.descriptor)

			if tC.wantErr != err {
				t.Errorf("got %v, want %v", err, tC.wantErr)
			}

			if tC.want == nil {
				if got != nil {
					t.Errorf("got %v, want nil", got)
				}
				return
			}

			if tC.want.Requests != got.Limit.Requests {
				t.Errorf("got %v, want %v", got.Limit.Requests, tC.want.Requests)
			}

			if tC.want.Unit != got.Limit.Unit {
				t.Errorf("got %v, want %v", got.Limit.Unit, tC.want.Unit)
			}
		})
	}
}
