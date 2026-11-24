package tvui

import (
	"bytes"
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/player"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type uiPlaybackPrefsStore struct {
	mu    sync.Mutex
	has   bool
	prefs app.PlaybackPreferences
}

func (s *uiPlaybackPrefsStore) Save(app.Preferences) error { return nil }
func (s *uiPlaybackPrefsStore) PlaybackPreferences() (app.PlaybackPreferences, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.prefs, s.has
}
func (s *uiPlaybackPrefsStore) SavePlaybackPreferences(p app.PlaybackPreferences) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prefs, s.has = p, true
	return nil
}

type uiPlaybackTestTransport []byte

func (d uiPlaybackTestTransport) OpenResource(ctx context.Context, res provider.PlaybackResource, method, br string) (*http.Response, error) {
	req, _ := http.NewRequestWithContext(ctx, method, res.URLs[0], nil)
	req.Header.Set("Range", br)
	rec := httptest.NewRecorder()
	http.ServeContent(rec, req, "m.mp4", time.Time{}, bytes.NewReader(d))
	return rec.Result(), nil
}

func makeUIDASHBytes() []byte {
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

type uiAudioPlaybackSession struct {
	uiAuthSource
	fixture    []byte
	multiAudio bool
	subLoaded  []string
}

func (s *uiAudioPlaybackSession) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{}, nil
}
func (s *uiAudioPlaybackSession) Close() {}
func (s *uiAudioPlaybackSession) ResolvePlayback(_ context.Context, req provider.PlaybackRequest) (provider.Playback, error) {
	initR := &provider.PlaybackRange{First: 0, Last: 18}
	idxR := &provider.PlaybackRange{First: 19, Last: 70}
	res := func(name string) provider.PlaybackResource {
		return provider.PlaybackResource{URLs: []string{"https://media.example.com/" + name}}
	}
	tracks := []provider.PlaybackTrack{
		{ID: "aac-192", Label: "AAC 192k", Default: true, Codec: "mp4a.40.2", Bandwidth: 192000, Resource: res("a192.m4s"), Init: initR, Index: idxR},
	}
	if s.multiAudio {
		tracks = append(tracks, provider.PlaybackTrack{
			ID: "aac-132", Label: "AAC 132k", Codec: "mp4a.40.2", Bandwidth: 132000, Resource: res("a132.m4s"), Init: initR, Index: idxR,
		})
	}
	return provider.Playback{
		Transport: uiPlaybackTestTransport(s.fixture),
		Plan: provider.PlaybackPlan{
			PartID:   req.PartID,
			Duration: 60,
			Quality:  "80",
			Qualities: []provider.PlaybackQuality{{ID: "80", Label: "1080P"}},
			Subtitles: []provider.PlaybackTrack{
				{ID: "sub-zh", Label: "中文（简体）", Language: "zh-CN", Resource: res("zh.json")},
				{ID: "sub-en", Label: "English", Language: "en-US", Resource: res("en.json")},
			},
			Streams: []provider.PlaybackStream{{
				Format:      provider.PlaybackDASH,
				Video:       provider.PlaybackTrack{Codec: "avc1.640028", Bandwidth: 2000000, Width: 1920, Height: 1080, Resource: res("v.m4s"), Init: initR, Index: idxR},
				Audio:       &tracks[0],
				AudioTracks: tracks,
			}},
		},
	}, nil
}
func (s *uiAudioPlaybackSession) PlaybackSubtitles(_ context.Context, query provider.PlaybackSubtitleQuery) ([]provider.SubtitleCue, error) {
	s.subLoaded = append(s.subLoaded, query.Track.ID)
	return []provider.SubtitleCue{{Start: 0, End: 10, Text: "Hello"}}, nil
}

func TestPlaybackSettingsUIAndAudioTrackMenuAndAutoplay(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entry := app.Entry{
		ID: "sample-source", SourceID: "sample-source", Name: "SampleSource",
		Capabilities: provider.Capabilities{provider.BrowseCatalog, provider.ResolvePlayback, provider.ReadSubtitles},
	}
	state, err := app.NewState([]app.Entry{entry}, entry.ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	store := &uiPlaybackPrefsStore{}
	queue := make(chan func(), 64)
	post := func(fn func()) { queue <- fn }
	drain := func() {
		for {
			select {
			case fn := <-queue:
				fn()
			default:
				return
			}
		}
	}
	waitDrain := func() {
		select {
		case fn := <-queue:
			fn()
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for async UI playback callback")
		}
		drain()
	}

	sess := &uiAudioPlaybackSession{fixture: makeUIDASHBytes(), multiAudio: true}
	catalog := app.NewCatalog(func(string) provider.Session { return sess }, post, func() {})
	t.Cleanup(catalog.Close)
	prefsMgr := app.NewPlaybackSettings(store, nil)

	status := player.Status{Position: 5, Duration: 60, HasFrame: true, AudioActive: true}
	var selectedAudioNames []string
	engine := player.Engine{
		Open:        func(string, float64) bool { return true },
		SetRate:     func(float64) {},
		SelectAudio: func(name string) bool { selectedAudioNames = append(selectedAudioNames, name); return true },
		Snapshot:    func() player.Status { return status },
		Stop:        func() {},
	}
	playback := app.NewPlayback(catalog, engine, nil, post, func() {})
	playback.SetPreferences(prefsMgr)
	t.Cleanup(playback.Close)

	view := newTestView(t, Options{
		State: state, Catalog: catalog, PlaybackSettings: prefsMgr,
		Pages: pages, Assets: assets, Preferences: store, Theme: theme.Default(),
	})
	view.Playback = playback
	view.SyncEntry()
	drain()

	tester := ui.NewTester(view.Render, 1920, 1080)
	state.Navigate(app.Route{Page: app.Settings, Section: "playback"})
	tester.Frame()

	// 1. Open Quality picker -> default focus on choice 0 (Auto), choose 1080P (index 3).
	state.Focus = playbackPrefQuality
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.PlaybackSettingPicker || state.Focus != playbackPickerOptionID(0) {
		t.Fatalf("expected quality picker focused on 0, got page=%s focus=%q", state.Page, state.Focus)
	}
	state.Focus = playbackPickerOptionID(3)
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Page != app.Settings || prefsMgr.Preferences().Quality != app.PlaybackQuality1080P {
		t.Fatalf("expected quality saved as 1080p, got page=%s prefs=%+v", state.Page, prefsMgr.Preferences())
	}

	// Re-open quality picker -> MUST default focus on index 3 (current selection)!
	state.Focus = playbackPrefQuality
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if state.Focus != playbackPickerOptionID(3) {
		t.Fatalf("expected reopened quality picker to focus current choice 3, got %q", state.Focus)
	}
	state.Back()
	tester.Frame()

	// 2. Set default subtitle to Chinese (index 1) and toggle Autoplay OFF.
	state.Focus = playbackPrefSubtitles
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	state.Focus = playbackPickerOptionID(1)
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if prefsMgr.Preferences().Subtitle != app.PlaybackSubtitleChinese {
		t.Fatalf("expected default subtitle zh, got %+v", prefsMgr.Preferences())
	}

	state.Focus = playbackPrefAutoplay
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if prefsMgr.Preferences().Autoplay {
		t.Fatal("expected autoplay toggled OFF")
	}

	// 3. Start multi-part video with multi-audio -> default Chinese subtitle auto-loads, [音轨] button is visible & switches tracks, Autoplay=OFF prevents auto-advance on Ended!
	item := provider.MediaItem{Ref: provider.MediaRef{EntryID: "sample-source", Kind: provider.Video, ID: "BV1Test"}, Title: "Audio Video"}
	p := view.context(state.Location, true, 1920, 1080)
	p.openPlayback([]playbackItem{{item: item, partID: "p1"}, {item: item, partID: "p2"}}, 0, false, nil)
	waitDrain() // ResolvePlayback + OpenMedia
	tester.Frame()
	waitDrain() // Auto-loaded Chinese subtitle
	tester.Frame()

	if len(sess.subLoaded) != 1 || sess.subLoaded[0] != "sub-zh" {
		t.Fatalf("expected default Chinese subtitle auto-loaded, got %v", sess.subLoaded)
	}
	if view.focus.item(playerAudio) == nil {
		t.Fatal("expected player/audio control visible when 2 audio tracks exist")
	}

	// Open [音轨] popover -> default focus on active track aac-192 -> select aac-132.
	state.Focus = playerAudio
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if view.player.panel != playerAudio || state.Focus != playbackOptionID(playerAudio, "aac-192") {
		t.Fatalf("expected audio panel focused on default aac-192, got panel=%q focus=%q", view.player.panel, state.Focus)
	}
	state.Focus = playbackOptionID(playerAudio, "aac-132")
	view.reveal = true
	tester.Frame()
	tester.Key(0, ui.KeyEnter)
	tester.Frame()
	if playback.State().ActiveAudio != "aac-132" || len(selectedAudioNames) != 1 || selectedAudioNames[0] != "AAC 132k" {
		t.Fatalf("expected audio switched to aac-132, got active=%q names=%v", playback.State().ActiveAudio, selectedAudioNames)
	}
	if view.player.panel != "" {
		t.Fatalf("expected audio panel closed after selection, got %q", view.player.panel)
	}

	// End playback with Autoplay=OFF -> MUST stay on index 0 (not auto-advance to p2)!
	status.Ended = true
	tester.Frame()
	if view.player.index != 0 {
		t.Fatalf("expected Autoplay=OFF to stop auto-advance, got index=%d", view.player.index)
	}

	// 4. Single-audio video -> [音轨] button MUST be completely hidden!
	status.Ended = false
	sess.multiAudio = false
	view.startPlaybackItem(1, nil)
	waitDrain()
	tester.Frame()
	drain()
	tester.Frame()
	if view.focus.item(playerAudio) != nil {
		t.Fatal("expected player/audio button hidden when only 1 audio track exists")
	}
}
