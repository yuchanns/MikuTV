package platform

import (
	"cmp"
	"encoding/json"
	"slices"
)

func unmarshalJSONOrZero[T any](raw string) T {
	var out T
	if raw == "" {
		return out
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		var zero T
		return zero
	}
	return out
}

func marshalJSONString(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func trimRecentByOrder[T any](items []T, limit int, order func(T) int64) []T {
	if len(items) <= limit {
		return items
	}
	slices.SortFunc(items, func(a, b T) int {
		return cmp.Compare(order(a), order(b))
	})
	return slices.Clone(items[len(items)-limit:])
}

func deleteMatching[T any](items *[]T, match func(T) bool) bool {
	before := len(*items)
	*items = slices.DeleteFunc(*items, match)
	return len(*items) != before
}
