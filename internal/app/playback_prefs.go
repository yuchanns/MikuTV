package app

import (
	"math"
	"strings"
	"sync"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type PlaybackQualityPref string

const (
	PlaybackQualityAuto  PlaybackQualityPref = ""
	PlaybackQuality4KHDR PlaybackQualityPref = "4k-hdr"
	PlaybackQuality4K    PlaybackQualityPref = "4k"
	PlaybackQuality1080P PlaybackQualityPref = "1080p"
	PlaybackQuality720P  PlaybackQualityPref = "720p"
)

type PlaybackSubtitlePref string

const (
	PlaybackSubtitleOff     PlaybackSubtitlePref = "off"
	PlaybackSubtitleChinese PlaybackSubtitlePref = "zh"
	PlaybackSubtitleEnglish PlaybackSubtitlePref = "en"
)

var supportedPlaybackSpeeds = [...]float64{0.75, 1.0, 1.25, 1.5, 2.0}

type PlaybackPreferences struct {
	Quality  PlaybackQualityPref  `json:"quality"`
	Speed    float64              `json:"speed"`
	Subtitle PlaybackSubtitlePref `json:"subtitle"`
	Autoplay bool                 `json:"autoplay"`
}

func DefaultPlaybackPreferences() PlaybackPreferences {
	return PlaybackPreferences{
		Quality:  PlaybackQualityAuto,
		Speed:    1.0,
		Subtitle: PlaybackSubtitleOff,
		Autoplay: true,
	}
}

func (p PlaybackPreferences) Normalize() PlaybackPreferences {
	switch p.Quality {
	case PlaybackQualityAuto, PlaybackQuality4KHDR, PlaybackQuality4K, PlaybackQuality1080P, PlaybackQuality720P:
	default:
		p.Quality = PlaybackQualityAuto
	}
	if p.Speed <= 0 || math.IsNaN(p.Speed) || math.IsInf(p.Speed, 0) {
		p.Speed = 1.0
	} else {
		best := supportedPlaybackSpeeds[0]
		bestDiff := math.Abs(p.Speed - best)
		for _, candidate := range supportedPlaybackSpeeds[1:] {
			if diff := math.Abs(p.Speed - candidate); diff < bestDiff {
				best, bestDiff = candidate, diff
			}
		}
		p.Speed = best
	}
	switch p.Subtitle {
	case PlaybackSubtitleOff, PlaybackSubtitleChinese, PlaybackSubtitleEnglish:
	default:
		p.Subtitle = PlaybackSubtitleOff
	}
	return p
}

type PlaybackPreferencesStore interface {
	PlaybackPreferences() (PlaybackPreferences, bool)
	SavePlaybackPreferences(PlaybackPreferences) error
}

type PlaybackSettings struct {
	mu     sync.RWMutex
	store  PlaybackPreferencesStore
	notify func()
	prefs  PlaybackPreferences
}

func NewPlaybackSettings(store any, notify func()) *PlaybackSettings {
	if notify == nil {
		notify = func() {}
	}
	prefStore, _ := store.(PlaybackPreferencesStore)
	prefs := DefaultPlaybackPreferences()
	if prefStore != nil {
		if loaded, ok := prefStore.PlaybackPreferences(); ok {
			prefs = loaded.Normalize()
		}
	}
	return &PlaybackSettings{
		store:  prefStore,
		notify: notify,
		prefs:  prefs,
	}
}

func (s *PlaybackSettings) Preferences() PlaybackPreferences {
	if s == nil {
		return DefaultPlaybackPreferences()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.prefs
}

func (s *PlaybackSettings) Update(update func(*PlaybackPreferences)) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	next := s.prefs
	update(&next)
	next = next.Normalize()
	if next == s.prefs {
		s.mu.Unlock()
		return nil
	}
	s.prefs = next
	store := s.store
	s.mu.Unlock()
	var err error
	if store != nil {
		err = store.SavePlaybackPreferences(next)
	}
	s.notify()
	return err
}

func MatchPreferredSubtitle(pref PlaybackSubtitlePref, tracks []provider.PlaybackTrack) (provider.PlaybackTrack, bool) {
	if pref == PlaybackSubtitleOff || len(tracks) == 0 {
		return provider.PlaybackTrack{}, false
	}
	var aiFallback *provider.PlaybackTrack
	for i := range tracks {
		t := tracks[i]
		if !subtitleMatchesPref(pref, t) {
			continue
		}
		if isAISubtitle(t) {
			if aiFallback == nil {
				aiFallback = &tracks[i]
			}
			continue
		}
		return t, true
	}
	if aiFallback != nil {
		return *aiFallback, true
	}
	return provider.PlaybackTrack{}, false
}

func isAISubtitle(t provider.PlaybackTrack) bool {
	id := strings.ToLower(t.ID)
	lang := strings.ToLower(t.Language)
	label := strings.ToLower(t.Label)
	return strings.HasPrefix(id, "ai-") || strings.HasPrefix(lang, "ai-") || strings.Contains(label, "ai")
}

func subtitleMatchesPref(pref PlaybackSubtitlePref, t provider.PlaybackTrack) bool {
	id := strings.ToLower(strings.TrimSpace(t.ID))
	lang := strings.ToLower(strings.TrimSpace(t.Language))
	label := strings.ToLower(strings.TrimSpace(t.Label))
	switch pref {
	case PlaybackSubtitleChinese:
		for _, token := range []string{"zh", "cn", "chs", "cht", "\u4e2d\u6587", "\u7b80\u4f53", "\u7e41\u4f53", "\u6c49\u8bed", "\u56fd\u8bed"} {
			if strings.Contains(lang, token) || strings.Contains(label, token) || strings.HasPrefix(id, token) || strings.Contains(id, "-"+token) {
				return true
			}
		}
	case PlaybackSubtitleEnglish:
		for _, token := range []string{"english", "\u82f1\u6587", "\u82f1\u8bed"} {
			if strings.Contains(label, token) || strings.Contains(lang, token) {
				return true
			}
		}
		if lang == "en" || strings.HasPrefix(lang, "en-") || strings.HasPrefix(lang, "en_") || strings.HasPrefix(lang, "ai-en") ||
			id == "en" || strings.HasPrefix(id, "en-") || strings.HasPrefix(id, "en_") || strings.HasPrefix(id, "ai-en") {
			return true
		}
	}
	return false
}
