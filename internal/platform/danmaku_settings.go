package platform

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	danmakuDisplayDefaultsKey = "danmakuDisplay.v1"
	danmakuRulesDefaultsKey   = "danmakuRules.v1"
	maxDanmakuRuleBuckets     = 64
	maxDanmakuRulesPerBucket  = 200
)

func parseDanmakuDisplay(raw string) (app.DanmakuDisplay, bool) {
	if raw == "" {
		return app.DefaultDanmakuDisplay(), false
	}
	var display app.DanmakuDisplay
	if err := json.Unmarshal([]byte(raw), &display); err != nil {
		return app.DefaultDanmakuDisplay(), false
	}
	return display.Normalize(), true
}

func encodeDanmakuDisplay(display app.DanmakuDisplay) (string, error) {
	data, err := json.Marshal(display.Normalize())
	if err != nil {
		return "", err
	}
	return string(data), nil
}

type danmakuRulesBucket struct {
	Entry string                 `json:"e"`
	Owner string                 `json:"o"`
	Rules []provider.DanmakuRule `json:"r"`
	Order int64                  `json:"u"`
}

type danmakuRulesStore struct {
	NextOrder int64                `json:"n"`
	Buckets   []danmakuRulesBucket `json:"b"`
}

func parseDanmakuRulesStore(raw string) danmakuRulesStore {
	if raw == "" {
		return danmakuRulesStore{}
	}
	var store danmakuRulesStore
	if err := json.Unmarshal([]byte(raw), &store); err != nil {
		return danmakuRulesStore{}
	}
	return store
}

func normalizeDanmakuRuleSlice(rules []provider.DanmakuRule) []provider.DanmakuRule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]provider.DanmakuRule, 0, min(len(rules), maxDanmakuRulesPerBucket))
	for _, rule := range rules {
		pattern := strings.TrimSpace(rule.Pattern)
		if !provider.ValidDanmakuRuleKind(rule.Kind) || pattern == "" {
			continue
		}
		rule.Pattern = pattern
		out = append(out, rule)
		if len(out) >= maxDanmakuRulesPerBucket {
			break
		}
	}
	return out
}

func (s *danmakuRulesStore) get(entryID, owner string) []provider.DanmakuRule {
	for _, bucket := range s.Buckets {
		if bucket.Entry == entryID && bucket.Owner == owner {
			return slices.Clone(normalizeDanmakuRuleSlice(bucket.Rules))
		}
	}
	return nil
}

func (s *danmakuRulesStore) put(entryID, owner string, rules []provider.DanmakuRule) {
	cleaned := normalizeDanmakuRuleSlice(rules)
	if len(cleaned) == 0 {
		s.clearAccount(entryID, owner)
		return
	}
	s.NextOrder++
	for i := range s.Buckets {
		if s.Buckets[i].Entry == entryID && s.Buckets[i].Owner == owner {
			s.Buckets[i].Rules = cleaned
			s.Buckets[i].Order = s.NextOrder
			return
		}
	}
	s.Buckets = append(s.Buckets, danmakuRulesBucket{
		Entry: entryID, Owner: owner, Rules: cleaned, Order: s.NextOrder,
	})
	if len(s.Buckets) > maxDanmakuRuleBuckets {
		slices.SortFunc(s.Buckets, func(a, b danmakuRulesBucket) int {
			switch {
			case a.Order < b.Order:
				return -1
			case a.Order > b.Order:
				return 1
			default:
				return 0
			}
		})
		s.Buckets = slices.Clone(s.Buckets[len(s.Buckets)-maxDanmakuRuleBuckets:])
	}
}

func (s *danmakuRulesStore) clearAccount(entryID, owner string) bool {
	before := len(s.Buckets)
	s.Buckets = slices.DeleteFunc(s.Buckets, func(bucket danmakuRulesBucket) bool {
		return bucket.Entry == entryID && bucket.Owner == owner
	})
	return len(s.Buckets) != before
}

func (s *danmakuRulesStore) clearEntry(entryID string) bool {
	before := len(s.Buckets)
	s.Buckets = slices.DeleteFunc(s.Buckets, func(bucket danmakuRulesBucket) bool {
		return bucket.Entry == entryID
	})
	return len(s.Buckets) != before
}

func (s *danmakuRulesStore) encode() (string, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
