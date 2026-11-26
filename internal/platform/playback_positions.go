package platform

import (
	"crypto/sha256"
	"fmt"
	"math"

	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

const (
	playbackPositionsDefaultsKey = "playbackPositions.v1"
	maxPlaybackPositions         = 200
)

type playbackPositionEntry struct {
	Key         string  `json:"k"`
	Entry       string  `json:"e"`
	Owner       string  `json:"o"`
	Seconds     float64 `json:"s"`
	Order       int64   `json:"u"`
	Subtitle    string  `json:"sub,omitempty"`
	SubtitleSet bool    `json:"subSet,omitempty"`
	Audio       string  `json:"aud,omitempty"`
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
	return unmarshalJSONOrZero[playbackPositionStore](raw)
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
	s.Items = trimRecentByOrder(s.Items, maxPlaybackPositions, func(item playbackPositionEntry) int64 { return item.Order })
}

func (s *playbackPositionStore) getTracks(owner string, target provider.PlaybackTarget) (app.PlaybackTrackSelection, bool) {
	key := playbackPositionItemKey(owner, target)
	for _, item := range s.Items {
		if item.Key == key {
			if !item.SubtitleSet && item.Audio == "" {
				return app.PlaybackTrackSelection{}, false
			}
			return app.PlaybackTrackSelection{
				Subtitle:    item.Subtitle,
				SubtitleSet: item.SubtitleSet,
				Audio:       item.Audio,
			}, true
		}
	}
	return app.PlaybackTrackSelection{}, false
}

func (s *playbackPositionStore) putTracks(owner string, target provider.PlaybackTarget, sel app.PlaybackTrackSelection) {
	key := playbackPositionItemKey(owner, target)
	s.NextOrder++
	for i := range s.Items {
		if s.Items[i].Key == key {
			if sel.SubtitleSet {
				s.Items[i].Subtitle = sel.Subtitle
				s.Items[i].SubtitleSet = true
			}
			if sel.Audio != "" {
				s.Items[i].Audio = sel.Audio
			}
			s.Items[i].Order = s.NextOrder
			return
		}
	}
	s.Items = append(s.Items, playbackPositionEntry{
		Key:         key,
		Entry:       target.Ref.EntryID,
		Owner:       owner,
		Seconds:     -1,
		Order:       s.NextOrder,
		Subtitle:    sel.Subtitle,
		SubtitleSet: sel.SubtitleSet,
		Audio:       sel.Audio,
	})
	s.Items = trimRecentByOrder(s.Items, maxPlaybackPositions, func(item playbackPositionEntry) int64 { return item.Order })
}

func (s *playbackPositionStore) clearEntry(entryID string) bool {
	return deleteMatching(&s.Items, func(item playbackPositionEntry) bool {
		return item.Entry == entryID
	})
}

func (s *playbackPositionStore) clearAccount(entryID, owner string) bool {
	return deleteMatching(&s.Items, func(item playbackPositionEntry) bool {
		return item.Entry == entryID && item.Owner == owner
	})
}

func (s *playbackPositionStore) encode() (string, error) {
	return marshalJSONString(s)
}
