package app

import "testing"

func TestRouteRegistrationRejectsInvalidGraphs(t *testing.T) {
	home, picker, other := Route{Page: "root"}, Route{Page: "picker"}, Route{Page: "other"}
	for _, test := range []struct {
		name  string
		extra []RouteSpec
	}{
		{"duplicate", []RouteSpec{{Route: home}}},
		{"missing parent", []RouteSpec{{Route: other, Parent: Route{Page: "missing"}}}},
		{"parent cycle", []RouteSpec{{Route: other, Parent: other}}},
		{"redirect cycle", []RouteSpec{{Route: other, Redirect: other}}},
		{"missing redirect", []RouteSpec{{Route: other, Redirect: Route{Page: "missing"}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			definitions := append([]RouteSpec{{Route: home}, {Route: picker}}, test.extra...)
			if _, err := NewRoutes(home, picker, definitions); err == nil {
				t.Fatal("invalid route graph was accepted")
			}
		})
	}
}

func TestRegisteredOverlaysRestoreTheirActualCaller(t *testing.T) {
	routes := routesForTest(t)
	definitions := make([]RouteSpec, 0, len(routes.specs)+2)
	for _, spec := range routes.specs {
		definitions = append(definitions, spec)
	}
	one, two := Route{Page: "test-overlay-one"}, Route{Page: "test-overlay-two"}
	definitions = append(definitions, RouteSpec{Route: one, Modal: true, Initial: Position{Focus: "one/action"}}, RouteSpec{Route: two, Modal: true, Initial: Position{Focus: "two/action"}})
	registered, err := NewRoutes(routes.Root, routes.Picker, definitions)
	if err != nil {
		t.Fatal(err)
	}
	state, err := NewState(entriesForTest(), "first", registered)
	if err != nil {
		t.Fatal(err)
	}
	state.Navigate(Route{Page: Search})
	origin := Position{Focus: "search/result/7", Y: 420}
	state.RecordPosition(origin)
	state.Push(one)
	state.RecordPosition(Position{Focus: "one/last", Y: 180})
	state.Push(two)
	layers := state.Layers()
	if len(layers) != 3 || layers[0].Route.Page != Search || layers[0].Position != origin {
		t.Fatalf("overlay lost its actual caller: %+v", layers)
	}
	state.Back()
	if state.Route != one || state.Focus != "one/last" || state.Y != 180 {
		t.Fatalf("nested overlay did not restore its parent: %+v", state.Location)
	}
	state.Back()
	if state.Page != Search || state.Position != origin {
		t.Fatalf("overlay returned to an unrelated page: %+v", state.Location)
	}
	before := state.Location
	if state.Navigate(Route{Page: "unregistered"}) || state.Location != before {
		t.Fatal("an unregistered route changed navigation")
	}
}

func TestEntrySwitchPreservesTheRegisteredRouteStack(t *testing.T) {
	state := stateForTest(t, "first")
	state.RecordPosition(Position{Focus: "home/item/3", Y: 160})
	state.Push(Route{Page: Search})
	state.RecordPosition(Position{Focus: "search/result/9", Y: 720})
	state.OpenEntries("")
	state.SelectEntry("second")
	state.OpenEntries("")
	state.SelectEntry("first")
	if state.Page != Search || state.Focus != "search/result/9" || state.Y != 720 {
		t.Fatal("entry switch lost the active registered route")
	}
	state.Back()
	if state.Page != Home || state.Focus != "home/item/3" || state.Y != 160 {
		t.Fatal("entry switch lost the route's parent history")
	}
}
