package platform

import (
	"encoding/json"
	"slices"
	"strings"
)

const (
	searchHistoryDefaultsKey = "searchHistory.v1"
	maxSearchHistoryQueries  = 10
	maxSearchHistoryBuckets  = 64
	maxSearchHistoryLength   = 256
)

type searchHistoryBucket struct {
	Entry   string   `json:"e"`
	Owner   string   `json:"o"`
	Queries []string `json:"q"`
	Order   int64    `json:"u"`
}

type searchHistoryStore struct {
	NextOrder int64                 `json:"n"`
	Buckets   []searchHistoryBucket `json:"b"`
}

func parseSearchHistoryStore(raw string) searchHistoryStore {
	if raw == "" {
		return searchHistoryStore{}
	}
	var store searchHistoryStore
	if err := json.Unmarshal([]byte(raw), &store); err != nil {
		return searchHistoryStore{}
	}
	return store
}

func normalizeSearchQueries(queries []string) []string {
	if len(queries) == 0 {
		return nil
	}
	out := make([]string, 0, min(len(queries), maxSearchHistoryQueries))
	seen := make(map[string]bool, min(len(queries), maxSearchHistoryQueries))
	for _, raw := range queries {
		term := strings.TrimSpace(raw)
		if term == "" || len(term) > maxSearchHistoryLength {
			continue
		}
		if seen[term] {
			continue
		}
		seen[term] = true
		out = append(out, term)
		if len(out) >= maxSearchHistoryQueries {
			break
		}
	}
	return out
}

func (s *searchHistoryStore) get(entryID, owner string) []string {
	for _, bucket := range s.Buckets {
		if bucket.Entry == entryID && bucket.Owner == owner {
			return slices.Clone(normalizeSearchQueries(bucket.Queries))
		}
	}
	return nil
}

func (s *searchHistoryStore) put(entryID, owner string, queries []string) {
	cleaned := normalizeSearchQueries(queries)
	if len(cleaned) == 0 {
		s.clearAccount(entryID, owner)
		return
	}
	s.NextOrder++
	for i := range s.Buckets {
		if s.Buckets[i].Entry == entryID && s.Buckets[i].Owner == owner {
			s.Buckets[i].Queries = cleaned
			s.Buckets[i].Order = s.NextOrder
			return
		}
	}
	s.Buckets = append(s.Buckets, searchHistoryBucket{
		Entry: entryID, Owner: owner, Queries: cleaned, Order: s.NextOrder,
	})
	if len(s.Buckets) > maxSearchHistoryBuckets {
		slices.SortFunc(s.Buckets, func(a, b searchHistoryBucket) int {
			switch {
			case a.Order < b.Order:
				return -1
			case a.Order > b.Order:
				return 1
			default:
				return 0
			}
		})
		s.Buckets = slices.Clone(s.Buckets[len(s.Buckets)-maxSearchHistoryBuckets:])
	}
}

func (s *searchHistoryStore) clearAccount(entryID, owner string) bool {
	before := len(s.Buckets)
	s.Buckets = slices.DeleteFunc(s.Buckets, func(bucket searchHistoryBucket) bool {
		return bucket.Entry == entryID && bucket.Owner == owner
	})
	return len(s.Buckets) != before
}

func (s *searchHistoryStore) clearEntry(entryID string) bool {
	before := len(s.Buckets)
	s.Buckets = slices.DeleteFunc(s.Buckets, func(bucket searchHistoryBucket) bool {
		return bucket.Entry == entryID
	})
	return len(s.Buckets) != before
}

func (s *searchHistoryStore) encode() (string, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
