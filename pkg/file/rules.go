package file

import (
	"fmt"
	"github.com/fsnotify/fsnotify"
	"sort"
	"strings"
	"sync"

	"github.com/pkg/errors"

	"github.com/samueltorres/r8limiter/pkg/rules"
	"github.com/spf13/viper"

	rl "github.com/envoyproxy/go-control-plane/envoy/api/v2/ratelimit"
)

type RuleService struct {
	viper       *viper.Viper
	rulesConfig *rules.RulesConfig
	ruleIndex   map[string][]*rules.Rule
	mux         *sync.RWMutex
	ruleCount   int
}

func NewRuleService(file string) (*RuleService, error) {
	v := viper.New()
	v.SetConfigFile(file)
	err := v.ReadInConfig()
	if err != nil {
		return nil, errors.Wrap(err, "error reading in rule file config")
	}

	fc := &RuleService{
		viper: v,
		mux:   &sync.RWMutex{},
	}

	err = fc.loadRules()
	if err != nil {
		return nil, errors.Wrap(err, "error loading rules")
	}

	v.WatchConfig()
	v.OnConfigChange(func(e fsnotify.Event) {
		fmt.Println("Config file changed:", e.Name)
		err := fc.loadRules()
		if err != nil {
			fmt.Println(err)
		}
	})

	return fc, nil
}

func (rs *RuleService) GetRatelimitRule(domain string, requestDescriptor *rl.RateLimitDescriptor) (*rules.Rule, error) {
	rs.mux.RLock()
	defer rs.mux.RUnlock()

	ruleMatchCount := make(map[*rules.Rule]int, rs.ruleCount)

	// 1. find possible matches
	for _, ee := range requestDescriptor.Entries {
		// 1.1 descriptors that contain a key
		key := domain + "." + ee.Key
		if descriptors, ok := rs.ruleIndex[key]; ok {
			for _, desc := range descriptors {
				ruleMatchCount[desc]++
			}
		}

		// 1.2 descriptors that contain a key & value
		key = domain + "." + ee.Key + "." + ee.Value
		if descriptors, ok := rs.ruleIndex[key]; ok {
			for _, desc := range descriptors {
				ruleMatchCount[desc]++
			}
		}
	}

	if len(ruleMatchCount) == 0 {
		return nil, rules.ErrNoMatchedRule
	}

	// 2. filter out matches
	type rankedMatch struct {
		rule  *rules.Rule
		count int
	}
	// todo: #performance rankedMatches is escaping to the heap, please review later
	rankedMatches := make([]rankedMatch, 0, len(ruleMatchCount))
	requestDescriptorLabels := make(map[string]bool)
	for _, label := range requestDescriptor.Entries {
		requestDescriptorLabels[label.Key] = true
		requestDescriptorLabels[label.Key+"."+label.Value] = true
	}

	for k, v := range ruleMatchCount {
		// filter out non existing labels
		if len(requestDescriptor.Entries) >= len(k.Labels) {
			descriptorEntriesValid := true
			for _, label := range k.Labels {
				// if there's a label key not present
				if _, exists := requestDescriptorLabels[label.Key]; !exists {
					descriptorEntriesValid = false
					break
				}

				// if label value is specified, it must match descriptor's
				if label.Value != "" {
					if _, exists := requestDescriptorLabels[label.Key+"."+label.Value]; !exists {
						descriptorEntriesValid = false
						break
					}
				}
			}

			if descriptorEntriesValid {
				rankedMatches = append(rankedMatches, rankedMatch{k, v})
			}
		}
	}

	if len(rankedMatches) == 0 {
		return nil, rules.ErrNoMatchedRule
	}

	// 2.1 sort matches by count descending
	sort.Slice(rankedMatches, func(i, j int) bool {
		return rankedMatches[i].count > rankedMatches[j].count
	})

	// 2.2 return descriptor with matches
	selectedDescriptor := rankedMatches[0]
	maxInnerRank := rankedMatches[0].rule.InnerRank

	// check for ties in matches
	for j := 1; j < len(rankedMatches); j++ {
		// if there's a tie we need to find the one with the biggest rank
		if selectedDescriptor.count == rankedMatches[j].count {
			if rankedMatches[j].rule.InnerRank > maxInnerRank {
				selectedDescriptor = rankedMatches[j]
				maxInnerRank = rankedMatches[j].rule.InnerRank
			}
		} else {
			return selectedDescriptor.rule, nil
		}
	}

	return selectedDescriptor.rule, nil
}

func (rs *RuleService) loadRules() error {
	var rulesConfig rules.RulesConfig
	err := rs.viper.Unmarshal(&rulesConfig)
	if err != nil {
		return errors.Wrap(err, "error on rule config unmarshal")
	}

	err = validateRules(rulesConfig)
	if err != nil {
		return errors.Wrap(err, "rules file is invalid")
	}

	rs.mux.Lock()
	rs.mux.Unlock()
	rs.rulesConfig = &rulesConfig
	rs.ruleIndex, rs.ruleCount = createSearchIndex(&rulesConfig)

	return nil
}

func createSearchIndex(rc *rules.RulesConfig) (map[string][]*rules.Rule, int) {
	ruleMap := make(map[string][]*rules.Rule)
	ruleCount := 0
	for _, domain := range rc.Domains {
		for _, rule := range domain.Rules {
			ruleCount++

			for _, k := range rule.Labels {
				var key string
				if k.Value == "" {
					key = domain.Domain + "." + k.Key
					rule.InnerRank = 10
				} else {
					key = domain.Domain + "." + k.Key + "." + k.Value
					rule.InnerRank = 1000
				}

				_, exist := ruleMap[key]
				if !exist {
					ruleMap[key] = []*rules.Rule{}
				}
				ruleMap[key] = append(ruleMap[key], rule)
			}
		}
	}

	return ruleMap, ruleCount
}

func validateRules(rulesConfig rules.RulesConfig) error {

	// validate that there is at least one domain config
	if len(rulesConfig.Domains) == 0 {
		return errors.Errorf("there are no rule domain configs")
	}

	// validate domain rule configs
	domainMap := make(map[string]bool, len(rulesConfig.Domains))

	for i, d := range rulesConfig.Domains {
		// validate domain name
		if d.Domain == "" {
			return errors.Errorf("invalid domain name (%d)", i)
		}

		// validate that there are no duplicated domains
		if _, exists := domainMap[d.Domain]; exists {
			return errors.Errorf("duplicated domain name (%d)", i)
		}
		domainMap[d.Domain] = true

		// validate that the domain has at least one rule
		if len(d.Rules) == 0 {
			return errors.Errorf("domain with no rules (%s)", d.Domain)
		}

		labelsMap := make(map[string]bool)
		for j, r := range d.Rules {

			// validate that there are no rules with the same labels
			labelKeyValues := make([]string, 0, len(r.Labels))
			for _, label := range r.Labels {
				labelKeyValues = append(labelKeyValues, label.Key+"."+label.Value)
			}
			sort.Strings(labelKeyValues)
			labelSummary := strings.Join(labelKeyValues, ":")

			if _, exists := labelsMap[labelSummary]; exists {
				return errors.Errorf("duplicated rule labels - domain (%s) rule (%d)", d.Domain, j)
			}
			labelsMap[labelSummary] = true

			// validate rule limit
			if !validateLimitUnit(r.Limit.Unit) {
				return errors.Errorf("invalid rule limit unit - domain (%s) rule (%d)", d.Domain, j)
			}
			if r.Limit.Requests < 0 {
				return errors.Errorf("invalid rule limit request - domain (%s), rule (%d)", d.Domain, j)
			}
		}
	}

	return nil
}

func validateLimitUnit(unit string) bool {
	switch unit {
	case "second":
		return true
	case "minute":
		return true
	case "hour":
		return true
	case "day":
		return true
	default:
		return false
	}
}
