package rules

import (
	"errors"

	rl "github.com/envoyproxy/go-control-plane/envoy/api/v2/ratelimit"
)

var ErrNoMatchedRule = errors.New("no matched rules")

type RulesService interface {
	GetRatelimitRule(domain string, requestDescriptor *rl.RateLimitDescriptor) (*Rule, error)
}

type RulesConfig struct {
	Domains []*DomainRules `yaml:"domains"`
}

type DomainRules struct {
	Domain string `yaml:"domain"`
	Rules  []*Rule `yaml:"rules"`
}

type Rule struct {
	Name      string            `yaml:"name"`
	Labels    []DescriptorLabel `yaml:"labels"`
	Limit     Limit             `yaml:"limit"`
	InnerRank int
}

type DescriptorLabel struct {
	Key   string `yaml:"key"`
	Value string `yaml:"value"`
}

type Limit struct {
	Requests uint32 `yaml:"requests"`
	Unit     string `yaml:"unit"`
}
