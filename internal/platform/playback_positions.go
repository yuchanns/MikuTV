package platform

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"slices"

	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	playbackPositionsDefaultsKey = "playbackPositions.v1"
	maxPlaybackPositions         = 200
)

type playbackPositionEntry struct {
	Key     string  `json:"k"`
	Entry   string  `json:"e"`
	Owner   string  `json:"o"`
	Seconds float64 `json:"s"`
	Order   int64   `json:"u"`
}

type playbackPositionStore struct {
	NextOrder int64                   `json:"n"`
	Items     []playbackPositionEntry `json:"i"`
}

func playbackPositionItemKey(owner string, target provider.PlaybackTarget) string {
	ref := target.Ref
	return ref.EntryID + "\x00" + owner + "\x00" + string(ref.Kind) + "\x00" + ref.ID + "\x00" + target.PartID
}

func legacyPlaybackPositionKeyName(owner string, target provider.PlaybackTarget) string {
	hash := sha256.Sum256([]byte(playbackPositionItemKey(owner, target)))
	return fmt.Sprintf("playbackPosition.%x", hash)
}

func parsePlaybackPositionStore(raw string) playbackPositionStore {
	if raw == "" {
		return playbackPositionStore{}
	}
	var store playbackPositionStore
	if err := json.Unmarshal([]byte(raw), &store); err != nil {
		return playbackPositionStore{}
	}
	return store
}

func (s *playbackPositionStore) get(owner string, target provider.PlaybackTarget) (float64, bool) {
	key := playbackPositionItemKey(owner, target)
	for _, item := range s.Items {
		if item.Key == key {
			return item.Seconds, item.Seconds >= 0
		}
	}
	return 0, false
}

func (s *playbackPositionStore) put(owner string, target provider.PlaybackTarget, seconds float64) {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 {
		seconds = 0
	}
	seconds = math.Floor(seconds)
	key := playbackPositionItemKey(owner, target)
	s.NextOrder++
	for i := range s.Items {
		if s.Items[i].Key == key {
			s.Items[i].Seconds = seconds
			s.Items[i].Order = s.NextOrder
			return
		}
	}
	s.Items = append(s.Items, playbackPositionEntry{
		Key:     key,
		Entry:   target.Ref.EntryID,
		Owner:   owner,
		Seconds: seconds,
		Order:   s.NextOrder,
	})
	if len(s.Items) > maxPlaybackPositions {
		slices.SortFunc(s.Items, func(a, b playbackPositionEntry) int {
			switch {
			case a.Order < b.Order:
				return -1
			case a.Order > b.Order:
				return 1
			default:
				return 0
			}
		})
		s.Items = slices.Clone(s.Items[len(s.Items)-maxPlaybackPositions:])
	}
}

func (s *playbackPositionStore) clearEntry(entryID string) bool {
	before := len(s.Items)
	s.Items = slices.DeleteFunc(s.Items, func(item playbackPositionEntry) bool {
		return item.Entry == entryID
	})
	return len(s.Items) != before
}

func (s *playbackPositionStore) clearAccount(entryID, owner string) bool {
	before := len(s.Items)
	s.Items = slices.DeleteFunc(s.Items, func(item playbackPositionEntry) bool {
		return item.Entry == entryID && item.Owner == owner
	})
	return len(s.Items) != before
}

func (s *playbackPositionStore) encode() (string, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
