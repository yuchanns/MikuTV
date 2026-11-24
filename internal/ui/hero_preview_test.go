package tvui

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/player"
	"github.com/yuchanns/MikuTV/internal/plugin"
	"github.com/yuchanns/MikuTV/internal/provider"
	"github.com/yuchanns/MikuTV/internal/theme"
)

type heroPreviewSession struct {
	items    []provider.MediaItem
	resolved chan provider.PlaybackTarget
}

func (s *heroPreviewSession) Close() {}

func (s *heroPreviewSession) Browse(context.Context, provider.BrowseRequest) (provider.CollectionPage, error) {
	return provider.CollectionPage{
		Collections: []provider.Collection{{ID: "picks", Title: "Picks", Items: s.items}},
	}, nil
}

func (s *heroPreviewSession) Item(context.Context, provider.MediaRef) (provider.MediaItem, error) {
	return provider.MediaItem{}, nil
}

func (s *heroPreviewSession) ResolvePlayback(_ context.Context, req provider.PlaybackRequest) (provider.Playback, error) {
	s.resolved <- req.PlaybackTarget
	return provider.Playback{
		Plan: provider.PlaybackPlan{
			Duration: 60,
			Streams:  []provider.PlaybackStream{{Format: provider.PlaybackDirect}},
		},
		Transport: heroPreviewTransport{},
	}, nil
}

type heroPreviewTransport struct{}

func (heroPreviewTransport) OpenResource(context.Context, provider.PlaybackResource, string, string) (*http.Response, error) {
	return nil, fmt.Errorf("fake transport")
}

func TestHomeHeroHoverAutoPreviewLifecycle(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entries := []app.Entry{{
		ID:           "demo",
		SourceID:     "demo",
		Name:         "Demo",
		Capabilities: provider.Capabilities{provider.BrowseCatalog, provider.ResolvePlayback},
	}}
	state, err := app.NewState(entries, "demo", pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	items := []provider.MediaItem{
		{Ref: provider.MediaRef{EntryID: "demo", Kind: provider.Video, ID: "vid-1"}, Title: "First Pick"},
		{Ref: provider.MediaRef{EntryID: "demo", Kind: provider.Video, ID: "vid-2"}, Title: "Second Pick"},
	}
	sess := &heroPreviewSession{items: items, resolved: make(chan provider.PlaybackTarget, 8)}
	queue := make(chan func(), 16)
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
	deliver := func() {
		t.Helper()
		select {
		case fn := <-queue:
			fn()
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for async delivery")
		}
		drain()
	}
	waitResolve := func() provider.PlaybackTarget {
		t.Helper()
		select {
		case target := <-sess.resolved:
			return target
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for hero preview resolve")
			return provider.PlaybackTarget{}
		}
	}

	catalog := app.NewCatalog(func(string) provider.Session { return sess }, func(fn func()) { queue <- fn }, func() {})
	catalog.Open(entries[0])
	catalog.EnsureLoaded()
	time.Sleep(30 * time.Millisecond)
	drain()

	var (
		previewOn  bool
		lastRect   player.ViewportRect
		clock      player.Status
		stopCalled int
	)
	pb := app.NewPlayback(catalog, player.Engine{
		Open:         func(string, float64) bool { return true },
		SetRate:      func(float64) {},
		Stop:         func() { stopCalled++ },
		Snapshot:     func() player.Status { return clock },
		Preview:      func(on bool) { previewOn = on },
		ViewportRect: func(r player.ViewportRect) { lastRect = r },
	}, nil, func(fn func()) { queue <- fn }, func() {})
	defer pb.Close()

	store := plugin.NewPackageStore(filepath.Join(t.TempDir(), "plugins"))
	appearance := app.NewAppearanceSettings(store, nil, nil, nil, nil)

	view := newTestView(t, Options{
		State:      state,
		Catalog:    catalog,
		Appearance: appearance,
		Pages:      pages,
		Assets:     assets,
		Theme:      theme.Default(),
	})
	view.Playback = pb

	tester := ui.NewTester(view.Render, 1920, 1080)
	// Initial frame lands on home/play; dwell timer hasn't elapsed yet.
	tester.Frame()
	if pb.InlinePreviewState().Active {
		t.Fatal("hero preview must wait for dwell timer before starting")
	}

	// Advance past 1.2s dwell -> preview resolves and starts for vid-1.
	tester.Advance(1300 * time.Millisecond)
	if got := waitResolve(); got.Ref.ID != "vid-1" {
		t.Fatalf("resolved preview ref = %q, want vid-1", got.Ref.ID)
	}
	deliver()
	if !previewOn {
		t.Fatal("expected native engine Preview(true)")
	}
	if lastRect.X != float64(collapsedRailWidth) || lastRect.Y != 0 || lastRect.H != float64(homeArtHeight) || !lastRect.Cover {
		t.Fatalf("unexpected hero preview rect: %+v", lastRect)
	}

	// Once first decoded frame is ready, Home activates transparent backdrop hole.
	clock = player.Status{HasFrame: true}
	tester.Advance(150 * time.Millisecond)
	tester.Frame()
	if !view.homePreviewShowing(state.Layers()) {
		t.Fatalf("expected homePreviewShowing=true once HasFrame is true; ps=%+v area=%v featured=%d",
			pb.InlinePreviewState(), view.State.Area, len(view.home.featured))
	}
	// Verify left navigation rail paints ABOVE home-preview-underlay (brand logo visible, not covered by canvas fill).
	frame := tester.Image()
	canvas := view.Theme.Colors.Surface.Canvas.NRGBA()
	railHasNonCanvas := false
	for y := 40; y < 88 && !railHasNonCanvas; y++ {
		for x := 34; x < 82; x++ {
			px := frame.RGBAAt(x, y)
			if max(px.R, canvas.R)-min(px.R, canvas.R) > 25 ||
				max(px.G, canvas.G)-min(px.G, canvas.G) > 25 ||
				max(px.B, canvas.B)-min(px.B, canvas.B) > 25 {
				railHasNonCanvas = true
				break
			}
		}
	}
	if !railHasNonCanvas {
		t.Fatal("left navigation rail was occluded by home-preview-underlay during hero preview")
	}

	// Moving focus to left navigation rail immediately stops preview.
	view.State.Area = app.NavigationArea
	view.State.Focus = app.RailFocus(app.Home)
	tester.Frame()
	if previewOn || pb.InlinePreviewState().Active {
		t.Fatal("moving into navigation rail must stop hero preview immediately")
	}

	// Disabling HoverPreview prevents restart even back on home/play.
	if err := appearance.SetHoverPreview(false); err != nil {
		t.Fatal(err)
	}
	view.State.Area = app.ContentArea
	view.State.Focus = homePlayAction
	tester.Frame()
	tester.Advance(1500 * time.Millisecond)
	if pb.InlinePreviewState().Active {
		t.Fatal("hero preview must not start when HoverPreview setting is off")
	}

	// Re-enable HoverPreview, verify ReduceMotion also suppresses preview.
	if err := appearance.SetHoverPreview(true); err != nil {
		t.Fatal(err)
	}
	if err := appearance.SetReduceMotion(true); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	tester.Advance(1500 * time.Millisecond)
	if pb.InlinePreviewState().Active {
		t.Fatal("hero preview must not start when ReduceMotion is enabled")
	}
	if stopCalled == 0 {
		t.Fatal("expected engine.Stop to be invoked on preview teardown")
	}
}
