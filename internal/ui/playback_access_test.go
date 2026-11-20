package tvui

import (
	"strings"
	"testing"

	"github.com/yuchanns/MikuTV/internal/app"
	"github.com/yuchanns/MikuTV/internal/provider"
)

func TestRegionDialogReturnsToRecommendations(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	entries := testEntries()
	entries[0].Capabilities = append(entries[0].Capabilities, provider.GetCatalogItem, provider.ResolvePlayback)
	state, err := app.NewState(entries, entries[0].ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Push(app.Route{Page: app.VideoDetail}) || !state.Push(app.Route{Page: app.Player}) {
		t.Fatal("could not open the player from detail")
	}
	view := &View{State: state}
	page := &PageContext{Assets: assets, State: state, host: view}
	dialog := page.playbackAccessDialogFor(app.PlaybackState{Limit: provider.PlaybackRegion}, playbackItem{})
	dialog.actions[0].activate()
	if state.Page != app.Home || view.notice != assets.Text.Detail.Related {
		t.Fatalf("regional fallback did not return to recommendations: %s, %q", state.Page, view.notice)
	}
	state.Back()
	if state.Page != app.Home {
		t.Fatalf("Back reopened the previous route: %s", state.Page)
	}
}

func TestDeviceDialogOffers4KSDROnlyForHDRWith4KFallback(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	view := &View{}
	page := &PageContext{Assets: assets, host: view}
	labels := assets.Text.Playback.Access
	state := app.PlaybackState{
		Limit: provider.PlaybackDevice,
		Resolution: provider.Playback{Plan: provider.PlaybackPlan{Qualities: []provider.PlaybackQuality{
			{ID: "127", Label: "8K", Badge: "8K", Limit: provider.PlaybackDevice},
			{ID: "126", Label: "Dolby Vision", Badge: "HDR", HDR: true, Limit: provider.PlaybackDevice},
			{ID: "125", Label: "HDR", Badge: "HDR", HDR: true, Limit: provider.PlaybackDevice},
			{ID: "120", Label: "4K", Badge: "4K"},
			{ID: "80", Label: "1080P", Badge: "1080P"},
		}}},
	}

	cases := []struct {
		name, requested, resolved string
		title, action             string
	}{
		{"HDR can fall back to 4K", "125", "120", labels.DeviceTitle, labels.UseSDR},
		{"Dolby Vision can fall back to 4K", "126", "120", labels.DeviceTitle, labels.UseSDR},
		{"Advertised 4K was unavailable", "125", "80", labels.DeviceGeneric, labels.UseBest},
		{"8K is unavailable", "127", "120", labels.DeviceGeneric, labels.UseBest},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			view.player.content.requestQuality = test.requested
			state.Fallback = test.resolved
			dialog := page.playbackAccessDialogFor(state, playbackItem{})
			if dialog.title != test.title || len(dialog.actions) != 2 || dialog.actions[0].label != test.action {
				t.Fatalf("unexpected device dialog title or actions: %q %+v", dialog.title, dialog.actions)
			}
		})
	}
}

func TestSourcePresentationOverridesTermsWithoutPollutingNeutralSources(t *testing.T) {
	assets, err := LoadAssets()
	if err != nil {
		t.Fatal(err)
	}
	pages, err := NewPages(assets)
	if err != nil {
		t.Fatal(err)
	}
	neutral := app.Entry{
		ID: "neutral", SourceID: "neutral", Name: "Neutral",
		Capabilities: provider.Capabilities{provider.BrowseCatalog, provider.ResolvePlayback},
	}
	branded := app.Entry{
		ID: "sample-source", SourceID: "sample-source", Name: "SampleSource",
		Capabilities: provider.Capabilities{provider.BrowseCatalog, provider.ResolvePlayback},
		Presentation: provider.SourcePresentation{
			CreatorMark:       "创作者",
			CreatorGroupTitle: "来自你关注的 创作者",
			Membership:        "会员",
			MobileApp:         "示例媒体 App",
			Sections: provider.SectionPresentation{
				Tracking: "我的追番",
				Courses:  "已购课堂",
			},
		},
	}
	state, err := app.NewState([]app.Entry{neutral, branded}, neutral.ID, pages.Routes)
	if err != nil {
		t.Fatal(err)
	}
	view := &View{State: state, Auth: app.NewAuth(uiTestKeychain{}, func(fn func()) { fn() }, func() {}, func() {}, func() {})}
	defer view.Auth.Close()
	page := &PageContext{Assets: assets, State: state, host: view}

	// Neutral entry keeps host defaults.
	if page.sourceCreatorMark() != assets.Text.Detail.CreatorMark ||
		page.sourcePinnedLabel() != assets.Text.Detail.Pinned ||
		page.sourceMembership() != assets.Text.Playback.QualityMember ||
		page.playbackLimitLabel(provider.PlaybackMembership) != assets.Text.Playback.MembershipLimit ||
		page.librarySectionLabel(provider.AccountTracking) != assets.Text.Activity.Tracking ||
		page.librarySectionLabel(provider.AccountCourses) != assets.Text.Activity.Courses {
		t.Fatalf("neutral source lost host defaults")
	}
	neutralVIP := page.playbackAccessDialogFor(app.PlaybackState{
		Limit: provider.PlaybackMembership,
		Resolution: provider.Playback{Plan: provider.PlaybackPlan{
			Access: provider.VideoAccessRestricted,
		}},
	}, playbackItem{})
	if neutralVIP.title != assets.Text.Playback.Access.VIPTitle || neutralVIP.body != assets.Text.Playback.Access.VIPNoPreview {
		t.Fatalf("neutral VIP dialog changed: %+v", neutralVIP)
	}

	// Branded entry applies manifest presentation + clean CJK/ASCII spacing.
	state.SelectEntry(branded.ID)
	view.player.showAccessHelp = true
	if page.sourceCreatorMark() != "创作者" ||
		page.sourcePinnedLabel() != "创作者置顶" ||
		page.sourceMembership() != "会员" ||
		page.playbackLimitLabel(provider.PlaybackMembership) != "会员专享" ||
		page.librarySectionLabel(provider.AccountTracking) != "我的追番" ||
		page.librarySectionLabel(provider.AccountCourses) != "已购课堂" {
		t.Fatalf("branded presentation did not override labels")
	}
	emptyCoursesTitle, _ := page.libraryEmptyText(provider.AccountCourses)
	if emptyCoursesTitle != "暂无已购课堂" {
		t.Fatalf("courses empty title = %q", emptyCoursesTitle)
	}
	brandedVIP := page.playbackAccessDialogFor(app.PlaybackState{
		Limit: provider.PlaybackMembership,
		Resolution: provider.Playback{Plan: provider.PlaybackPlan{
			Access: provider.VideoAccessRestricted,
		}},
	}, playbackItem{})
	if !strings.Contains(brandedVIP.title, "会员") ||
		!strings.Contains(brandedVIP.body, "示例媒体 App 开通") ||
		!strings.Contains(brandedVIP.instructions, "示例媒体 App 手动") ||
		!strings.Contains(brandedVIP.instructions, "开通会员") {
		t.Fatalf("branded VIP dialog = %+v", brandedVIP)
	}
	brandedPurchase := page.playbackAccessDialogFor(app.PlaybackState{
		Limit: provider.PlaybackPurchase,
		Resolution: provider.Playback{Plan: provider.PlaybackPlan{
			Access: provider.VideoAccessRestricted,
		}},
	}, playbackItem{})
	if !strings.Contains(brandedPurchase.body, "会员不直接") ||
		!strings.Contains(brandedPurchase.body, "在示例媒体 App 查看") {
		t.Fatalf("branded purchase dialog = %+v", brandedPurchase)
	}
	shelves := page.homeShelves([]provider.Collection{{
		ID: "following", Title: "Returned Shelf Title",
		Items: []provider.MediaItem{{Ref: provider.MediaRef{EntryID: branded.ID, Kind: provider.Video, ID: "v1"}, Title: "V1"}},
	}}, nil)
	if shelves[2].title != "Returned Shelf Title" {
		t.Fatalf("returned shelf title was ignored: %q", shelves[2].title)
	}
	fallbackShelves := page.homeShelves(nil, nil)
	if fallbackShelves[2].title != branded.Presentation.CreatorGroupTitle {
		t.Fatalf("manifest creatorGroupTitle fallback = %q", fallbackShelves[2].title)
	}
}
