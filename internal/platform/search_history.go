package platform

import (
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
	return unmarshalJSONOrZero[searchHistoryStore](raw)
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
	s.Buckets = trimRecentByOrder(s.Buckets, maxSearchHistoryBuckets, func(b searchHistoryBucket) int64 { return b.Order })
}

func (s *searchHistoryStore) clearAccount(entryID, owner string) bool {
	return deleteMatching(&s.Buckets, func(bucket searchHistoryBucket) bool {
		return bucket.Entry == entryID && bucket.Owner == owner
	})
}

func (s *searchHistoryStore) clearEntry(entryID string) bool {
	return deleteMatching(&s.Buckets, func(bucket searchHistoryBucket) bool {
		return bucket.Entry == entryID
	})
}

func (s *searchHistoryStore) encode() (string, error) {
	return marshalJSONString(s)
}
