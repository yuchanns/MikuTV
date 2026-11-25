package tvui

import (
	"testing"

	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

func TestPreloadAndFailureHideUnsupportedTabsAcrossPages(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}

	// 1. Search fallback kinds must omit Live when entry lacks BrowseLive.
	audioEntry := app.Entry{
		ID: "audio", SourceID: "audio", Name: "Audio",
		Capabilities: provider.Capabilities{provider.BrowseCatalog, provider.SearchCatalog, provider.ReadCreator},
	}
	gotKinds := searchKindsList(audioEntry, app.SearchSnapshot{})
	wantKinds := []provider.SearchKind{provider.SearchAll, provider.SearchVideo, provider.SearchCreator, provider.SearchSeries}
	if len(gotKinds) != len(wantKinds) {
		t.Fatalf("searchKindsList(audio) = %v, want %v", gotKinds, wantKinds)
	}
	for i := range wantKinds {
		if gotKinds[i] != wantKinds[i] {
			t.Fatalf("searchKindsList(audio)[%d] = %q, want %q", i, gotKinds[i], wantKinds[i])
		}
	}
	liveEntry := app.Entry{
		ID: "live", SourceID: "live", Name: "Live",
		Capabilities: provider.Capabilities{provider.BrowseCatalog, provider.SearchCatalog, provider.BrowseLive},
	}
	if got := searchKindsList(liveEntry, app.SearchSnapshot{}); len(got) != 5 || got[4] != provider.SearchLive {
		t.Fatalf("searchKindsList(live) = %v, want Live tab included", got)
	}

	// 2. Creator tabs before profileLoaded must not expose Posts or Collections.
	loadingCreator := &creatorState{seed: provider.Creator{ID: "c1", Name: "Artist"}}
	gotCreatorTabs := loadingCreator.visibleTabs()
	if len(gotCreatorTabs) != 1 || gotCreatorTabs[0] != provider.CreatorTabVideos {
		t.Fatalf("loading creator tabs = %v, want [videos]", gotCreatorTabs)
	}
	loadedCreatorNoTabs := &creatorState{seed: provider.Creator{ID: "c1", Name: "Artist"}, profileLoaded: true}
	gotLoadedTabs := loadedCreatorNoTabs.visibleTabs()
	if len(gotLoadedTabs) != 2 || gotLoadedTabs[0] != provider.CreatorTabVideos || gotLoadedTabs[1] != provider.CreatorTabAbout {
		t.Fatalf("loaded creator fallback tabs = %v, want [videos about]", gotLoadedTabs)
	}

	// 3. VideoDetail tabs before VideoDetails loads (info == nil) must not expose Episodes or Related.
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	state, err := app.NewState([]app.Entry{audioEntry}, audioEntry.ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	p := PageContext{State: state, Assets: assets}
	detail := &videoDetailState{item: provider.MediaItem{Ref: provider.MediaRef{EntryID: "audio", Kind: provider.Video, ID: "1"}}}
	gotDetailTabs := p.visibleVideoDetailTabs(detail)
	if len(gotDetailTabs) != 1 || gotDetailTabs[0] != videoDetailOverview {
		t.Fatalf("unloaded detail tabs = %v, want [overview]", gotDetailTabs)
	}
}
