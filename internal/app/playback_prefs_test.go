package app

import (
	"bytes"
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yuchanns/MikuTV/internal/player"
	"github.com/yuchanns/MikuTV/internal/provider"
)

type memoryPlaybackPrefsStore struct {
	has   bool
	prefs PlaybackPreferences
}

func (m *memoryPlaybackPrefsStore) PlaybackPreferences() (PlaybackPreferences, bool) {
	return m.prefs, m.has
}

func (m *memoryPlaybackPrefsStore) SavePlaybackPreferences(p PlaybackPreferences) error {
	m.prefs, m.has = p, true
	return nil
}

func makeMinimalDASHFixture() []byte {
	initial := []byte{0, 0, 0, 19, 'c', 'o', 'l', 'r', 'n', 'c', 'l', 'x', 0, 1, 0, 1, 0, 1, 0}
	box := make([]byte, 52)
	binary.BigEndian.PutUint32(box, uint32(len(box)))
	copy(box[4:], "sidx")
	box[8] = 1
	binary.BigEndian.PutUint32(box[16:], 1000)
	binary.BigEndian.PutUint64(box[28:], 4)
	binary.BigEndian.PutUint16(box[38:], 1)
	binary.BigEndian.PutUint32(box[40:], 12)
	binary.BigEndian.PutUint32(box[44:], 3000)
	return append(append(append(bytes.Clone(initial), box...), make([]byte, 4)...), []byte("abcdefghijkl")...)
}

type fixtureTransport []byte

func (data fixtureTransport) OpenResource(ctx context.Context, resource provider.PlaybackResource, method, byteRange string) (*http.Response, error) {
	req, _ := http.NewRequestWithContext(ctx, method, resource.URLs[0], nil)
	req.Header.Set("Range", byteRange)
	rec := httptest.NewRecorder()
	http.ServeContent(rec, req, "media.mp4", time.Time{}, bytes.NewReader(data))
	return rec.Result(), nil
}

type fakeMultiAudioSession struct {
	requests []provider.PlaybackRequest
	fixture  []byte
}

func (s *fakeMultiAudioSession) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{}, nil
}
func (s *fakeMultiAudioSession) Item(context.Context, provider.MediaRef) (provider.MediaItem, error) {
	return provider.MediaItem{}, nil
}
func (s *fakeMultiAudioSession) Close() {}
func (s *fakeMultiAudioSession) ResolvePlayback(_ context.Context, req provider.PlaybackRequest) (provider.Playback, error) {
	s.requests = append(s.requests, req)
	initRange := &provider.PlaybackRange{First: 0, Last: 18}
	indexRange := &provider.PlaybackRange{First: 19, Last: 70}
	res := func(name string) provider.PlaybackResource {
		return provider.PlaybackResource{URLs: []string{"https://media.example.com/" + name}}
	}
	audioTracks := []provider.PlaybackTrack{
		{ID: "aac-192", Label: "AAC 192k", Default: true, Codec: "mp4a.40.2", Bandwidth: 192000, Resource: res("a192.m4s"), Init: initRange, Index: indexRange},
		{ID: "dolby", Label: "Dolby Atmos", Codec: "ec-3", Bandwidth: 448000, Resource: res("dolby.m4s"), Init: initRange, Index: indexRange},
		{ID: "aac-132", Label: "AAC 132k", Codec: "mp4a.40.2", Bandwidth: 132000, Resource: res("a132.m4s"), Init: initRange, Index: indexRange},
	}
	return provider.Playback{
		Transport: fixtureTransport(s.fixture),
		Plan: provider.PlaybackPlan{
			PartID:   "p1",
			Duration: 120,
			Quality:  "80",
			Qualities: []provider.PlaybackQuality{
				{ID: "120", Label: "4K"},
				{ID: "80", Label: "1080P"},
			},
			Streams: []provider.PlaybackStream{{
				Format:      provider.PlaybackDASH,
				Video:       provider.PlaybackTrack{Codec: "avc1.640028", Bandwidth: 2000000, Width: 1920, Height: 1080, Resource: res("v.m4s"), Init: initRange, Index: indexRange},
				Audio:       &audioTracks[0],
				AudioTracks: audioTracks,
			}},
		},
	}, nil
}

func TestPlaybackPreferencesAndAudioTrackSwitching(t *testing.T) {
	store := &memoryPlaybackPrefsStore{}
	settings := NewPlaybackSettings(store, nil)
	if err := settings.Update(func(p *PlaybackPreferences) {
		p.Quality = PlaybackQuality1080P
		p.Speed = 1.5
		p.Subtitle = PlaybackSubtitleEnglish
		p.Autoplay = false
	}); err != nil {
		t.Fatal(err)
	}
	reloaded := NewPlaybackSettings(store, nil)
	if got := reloaded.Preferences(); got.Quality != PlaybackQuality1080P || got.Speed != 1.5 || got.Subtitle != PlaybackSubtitleEnglish || got.Autoplay {
		t.Fatalf("persisted prefs mismatch: %+v", got)
	}

	// Subtitle matching prefers non-AI track and respects language codes.
	subs := []provider.PlaybackTrack{
		{ID: "ai-zh", Label: "中文（自动生成）", Language: "ai-zh"},
		{ID: "s-zh", Label: "中文（简体）", Language: "zh-CN"},
		{ID: "s-en", Label: "English", Language: "en-US"},
	}
	if m, ok := MatchPreferredSubtitle(PlaybackSubtitleChinese, subs); !ok || m.ID != "s-zh" {
		t.Fatalf("expected non-AI zh subtitle s-zh, got %+v ok=%v", m, ok)
	}
	if m, ok := MatchPreferredSubtitle(PlaybackSubtitleEnglish, subs); !ok || m.ID != "s-en" {
		t.Fatalf("expected en subtitle s-en, got %+v ok=%v", m, ok)
	}
	if _, ok := MatchPreferredSubtitle(PlaybackSubtitleOff, subs); ok {
		t.Fatal("expected off preference to match no subtitle")
	}

	// Wire Playback with PlaybackSettings and verify default speed, preferredQuality, in-place & cross-codec audio switches!
	queue := make(chan func(), 16)
	post := func(fn func()) { queue <- fn }
	drainOne := func() {
		select {
		case fn := <-queue:
			fn()
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for playback callback")
		}
		for len(queue) > 0 {
			(<-queue)()
		}
	}

	sess := &fakeMultiAudioSession{fixture: makeMinimalDASHFixture()}
	catalog := NewCatalog(func(string) provider.Session { return sess }, post, func() {})
	defer catalog.Close()
	catalog.Open(Entry{ID: "src", SourceID: "src", Capabilities: provider.Capabilities{provider.ResolvePlayback}})

	var openCount int
	var lastOpenPos float64
	var lastRate float64
	var inPlaceAudio []string
	engine := player.Engine{
		Open: func(_ string, pos float64) bool {
			openCount++
			lastOpenPos = pos
			return true
		},
		SetRate: func(rate float64) { lastRate = rate },
		SelectAudio: func(name string) bool {
			inPlaceAudio = append(inPlaceAudio, name)
			return true
		},
		Snapshot: func() player.Status {
			return player.Status{Position: 42.5, Duration: 120, HasFrame: true, AudioActive: true}
		},
		Stop: func() {},
	}
	pb := NewPlayback(catalog, engine, nil, post, func() {})
	pb.SetPreferences(reloaded)
	defer pb.Close()

	pb.Start(provider.PlaybackTarget{Ref: provider.MediaRef{EntryID: "src", Kind: provider.Video, ID: "v1"}}, "", false, nil)
	drainOne()

	if len(sess.requests) != 1 || sess.requests[0].PreferredQuality != "1080p" {
		t.Fatalf("expected PreferredQuality=1080p, got %+v", sess.requests)
	}
	if pb.Rate() != 1.5 || lastRate != 1.5 {
		t.Fatalf("expected default speed 1.5x applied, got rate=%v engine=%v", pb.Rate(), lastRate)
	}
	st := pb.State()
	if len(st.AudioTracks) != 3 || st.DefaultAudio != "aac-192" || st.ActiveAudio != "aac-192" {
		t.Fatalf("unexpected audio tracks state: %+v", st)
	}
	if openCount != 1 {
		t.Fatalf("expected 1 open call, got %d", openCount)
	}

	// Same-codec switch (AAC 192k -> AAC 132k) switches in-place WITHOUT re-opening AVPlayer!
	pb.Tick(time.Now())
	if !pb.ChooseAudioTrack("aac-132") {
		t.Fatal("expected in-place audio switch to succeed")
	}
	if openCount != 1 || len(inPlaceAudio) != 1 || inPlaceAudio[0] != "AAC 132k" || pb.State().ActiveAudio != "aac-132" {
		t.Fatalf("expected in-place switch to AAC 132k without reopen: openCount=%d inPlace=%v active=%q", openCount, inPlaceAudio, pb.State().ActiveAudio)
	}

	// Cross-codec switch (AAC 132k -> Dolby ec-3) re-opens Media at current position (42.5s) WITHOUT re-calling ResolvePlayback!
	if !pb.ChooseAudioTrack("dolby") {
		t.Fatal("expected cross-codec audio switch to start")
	}
	drainOne()
	if openCount != 2 || lastOpenPos != 42.5 || len(sess.requests) != 1 || pb.State().ActiveAudio != "dolby" {
		t.Fatalf("expected cross-codec reopen at 42.5s with no plugin roundtrip: openCount=%d pos=%v reqs=%d active=%q",
			openCount, lastOpenPos, len(sess.requests), pb.State().ActiveAudio)
	}
}
