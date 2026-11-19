package app

import "testing"

func entriesForTest() []Entry {
	return []Entry{
		{ID: "first", SourceID: "source", Name: "First"},
		{ID: "second", SourceID: "other", Name: "Second"},
	}
}

func stateForTest(t *testing.T, recent string) *State {
	t.Helper()
	s, err := NewState(entriesForTest(), recent, routesForTest(t))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestEntryBrowsingDoesNotActivate(t *testing.T) {
	s := stateForTest(t, "first")
	origin := Position{Focus: "content/card/4", Y: 640}
	s.RecordPosition(origin)
	s.OpenEntries("second")
	if s.CurrentEntry != "first" || s.Focus != EntryFocus("second") {
		t.Fatalf("browsing changed the active entry: %+v", s.Location)
	}
	if !s.Back() || s.Page != Home || s.Position != origin {
		t.Fatalf("cancel did not restore the original position: %+v", s.Location)
	}
}

func TestEntrySessionsRestoreRoutesAndPositions(t *testing.T) {
	s := stateForTest(t, "first")
	s.Navigate(Route{Page: Library})
	first := Position{Focus: "content/card/12", X: 40, Y: 880}
	s.RecordPosition(first)
	s.OpenEntries("")
	if !s.SelectEntry("second") || s.Page != Home {
		t.Fatal("the new entry did not start at home")
	}
	s.Navigate(Route{Page: Search})
	second := Position{Focus: "search/result/6", Y: 320}
	s.RecordPosition(second)
	s.OpenEntries("")
	s.SelectEntry("first")
	if s.Page != Library || s.Position != first {
		t.Fatalf("first session lost its position: %+v", s.Location)
	}
	s.OpenEntries("")
	s.SelectEntry("second")
	if s.Page != Search || s.Position != second {
		t.Fatalf("second session lost its position: %+v", s.Location)
	}
}

func TestUnavailableRecentEntryKeepsManagementReachable(t *testing.T) {
	for _, recent := range []string{"", "missing"} {
		s := stateForTest(t, recent)
		if s.Page != Entries || s.CurrentEntry != "" {
			t.Fatalf("unavailable entry was restored: %q", recent)
		}
		if s.SelectEntry("missing") {
			t.Fatal("missing entry was activated")
		}
		s.Navigate(Route{Page: Settings, Section: "plugins"})
		if s.Page != Settings || !s.Back() || s.Page != Entries {
			t.Fatal("management cannot return to entry selection")
		}
		if s.CanGoBack() || s.Back() {
			t.Fatal("returning to the application root retained a transient page")
		}
	}
}

func TestNestedBackRestoresFocusAndScroll(t *testing.T) {
	s := stateForTest(t, "first")
	s.Navigate(Route{Page: Settings, Section: "about"})
	origin := Position{Focus: ShowLicenses, Y: 220}
	s.RecordPosition(origin)
	s.Push(Route{Page: Licenses, Section: "about"})
	s.RecordPosition(Position{Focus: LicenseText, Y: 1200})
	if !s.Back() || s.Page != Settings || s.Section != "about" || s.Position != origin {
		t.Fatalf("nested Back did not restore its parent: %+v", s.Location)
	}
	if !s.Back() || s.Page != Home {
		t.Fatal("Back did not return to home")
	}
	s.Focus = RailFocus(Home)
	if s.Back() {
		t.Fatal("app root consumed the system Back action")
	}
}

func TestRailFocusDoesNotOverwriteContentBookmark(t *testing.T) {
	s := stateForTest(t, "first")
	origin := Position{Focus: "home/item/9", Y: 480}
	s.RecordPosition(origin)
	s.RecordPosition(Position{Focus: RailFocus(Entries), Area: NavigationArea, Y: origin.Y})
	s.OpenEntries("")
	s.SelectEntry("second")
	s.OpenEntries("")
	s.SelectEntry("first")
	if s.Position != origin {
		t.Fatalf("rail replaced the content bookmark: %+v", s.Position)
	}
}

func routesForTest(t *testing.T) *Routes {
	t.Helper()
	specs := []RouteSpec{
		{Route: Route{Page: Home}, Initial: Position{Focus: ContentAction}, ContentFocus: ContentAction, ExitFocus: RailFocus(Home), RequiresEntry: true},
		{Route: Route{Page: Entries}, Transient: true, ResolveFocus: func(s *State) FocusID { return s.PickerFocus() }},
		{Route: Route{Page: Search}, Initial: Position{Focus: ContentAction}, RequiresEntry: true},
		{Route: Route{Page: Library}, Initial: Position{Focus: ContentAction}, RequiresEntry: true},
		{Route: Route{Page: Settings}, Redirect: Route{Page: Settings, Section: "playback"}},
		{Route: Route{Page: Settings, Section: "playback"}, Parent: Route{Page: Settings}, Initial: Position{Focus: SectionFocus("playback")}},
		{Route: Route{Page: Settings, Section: "plugins"}, Parent: Route{Page: Settings}, Initial: Position{Focus: SectionFocus("plugins")}},
		{Route: Route{Page: Settings, Section: "about"}, Parent: Route{Page: Settings}, Initial: Position{Focus: SectionFocus("about")}},
		{Route: Route{Page: Licenses, Section: "about"}, Parent: Route{Page: Settings, Section: "about"}, Modal: true, Initial: Position{Focus: LicenseText}},
	}
	routes, err := NewRoutes(Route{Page: Home}, Route{Page: Entries}, specs)
	if err != nil {
		t.Fatal(err)
	}
	return routes
}
