package app

import (
	"fmt"
	"slices"
	"strings"
)

type viewKey struct {
	EntryID string
	Route
}

type session struct {
	Location
	history []Location
}

// State belongs to the UI thread. Route policy is supplied by registration.
type State struct {
	Location
	CurrentEntry string
	Revision     uint64
	routes       *Routes
	entries      []Entry
	positions    map[viewKey]Location
	sessions     map[string]session
	history      []Location
}

func NewState(entries []Entry, recent string, routes *Routes) (*State, error) {
	if routes == nil {
		return nil, fmt.Errorf("registered routes are required")
	}
	s := &State{routes: routes, entries: slices.Clone(entries), positions: make(map[viewKey]Location), sessions: make(map[string]session)}
	initial := routes.Picker
	if _, ok := s.entry(recent); ok {
		s.CurrentEntry = recent
		initial = routes.Root
	}
	spec, _ := routes.Resolve(initial)
	s.show(routes.initial(s, spec))
	return s, nil
}

func (s *State) RecordPosition(position Position) {
	if position.Focus == "" {
		position.Focus, position.Area = s.Focus, s.Area
	}
	s.Position = position
	s.remember()
}

func (s *State) ContentFocus() FocusID {
	if saved, ok := s.positions[viewKey{s.CurrentEntry, s.Route}]; ok && saved.Area != NavigationArea {
		return saved.Focus
	}
	spec, _ := s.routes.Resolve(s.Route)
	if spec.ContentFocus != "" {
		return spec.ContentFocus
	}
	return s.routes.initial(s, spec).Focus
}

func (s *State) Navigate(route Route) bool { return s.navigate(route, false) }
func (s *State) Push(route Route) bool     { return s.navigate(route, true) }

func (s *State) navigate(route Route, push bool) bool {
	spec, ok := s.routes.Resolve(route)
	if !ok {
		return false
	}
	if spec.RequiresEntry && s.CurrentEntry == "" {
		s.OpenEntries("")
		return true
	}
	if !s.Allows(spec) {
		return false
	}
	if spec.Route == s.Route {
		return true
	}
	s.remember()
	if push || spec.Modal || spec.Transient {
		s.history = append(s.history, s.Location)
		s.show(s.routes.initial(s, spec))
		return true
	}
	s.history = nil
	s.show(s.location(spec))
	return true
}

// Layers retains the actual caller beneath any number of registered overlays.
func (s *State) Layers() []Location {
	layers := append(slices.Clone(s.history), s.Location)
	first := len(layers) - 1
	for first > 0 {
		spec, _ := s.routes.Spec(layers[first].Route)
		if !spec.Modal {
			break
		}
		first--
	}
	return layers[first:]
}

// RouteCount reports how many times page appears in the active navigation stack.
func (s *State) RouteCount(page Page) int {
	count := 0
	for _, loc := range s.history {
		if loc.Page == page {
			count++
		}
	}
	if s.Page == page {
		count++
	}
	return count
}

// Back returns false only when the system should handle leaving an app root.
func (s *State) Back() bool {
	if len(s.history) > 0 {
		s.remember()
		last := len(s.history) - 1
		destination := s.history[last]
		s.history = s.history[:last]
		s.show(destination)
		return true
	}
	root, _ := s.routes.Resolve(s.root())
	if s.Route != root.Route {
		s.remember()
		s.history = nil
		s.show(s.location(root))
		return true
	}
	if root.ExitFocus != "" && s.Focus != root.ExitFocus {
		s.Focus, s.Area = root.ExitFocus, NavigationArea
		return true
	}
	return false
}

func (s *State) CanGoBack() bool {
	root, _ := s.routes.Resolve(s.root())
	return len(s.history) > 0 || s.Route != root.Route || root.ExitFocus != "" && s.Focus != root.ExitFocus
}

func (s *State) root() Route {
	if s.CurrentEntry == "" {
		return s.routes.Picker
	}
	return s.routes.Root
}

func (s *State) Allows(spec RouteSpec) bool {
	if !spec.RequiresEntry && spec.Capability == "" && len(spec.Capabilities) == 0 {
		return true
	}
	entry, ok := s.Current()
	if !ok {
		return false
	}
	if len(spec.Capabilities) > 0 {
		for _, cap := range spec.Capabilities {
			if cap != "" && !entry.Capabilities.Has(cap) {
				return false
			}
		}
		return true
	}
	return spec.Capability == "" || entry.Capabilities.Has(spec.Capability)
}

func (s *State) canRestore(location Location) bool {
	spec, ok := s.routes.Resolve(location.Route)
	return ok && s.Allows(spec)
}

func (s *State) restoreSession(location Location, history []Location) {
	if !s.canRestore(location) {
		root, _ := s.routes.Resolve(s.root())
		location, history = s.routes.initial(s, root), nil
	}
	s.history = slices.DeleteFunc(slices.Clone(history), func(previous Location) bool {
		return !s.canRestore(previous)
	})
	if page, rail := strings.CutPrefix(string(location.Focus), "rail/"); rail &&
		!s.canRestore(Location{Route: Route{Page: Page(page)}}) {
		spec, _ := s.routes.Resolve(location.Route)
		location.Focus, location.Area = s.routes.initial(s, spec).Focus, ContentArea
	}
	s.show(location)
}

func (s *State) location(spec RouteSpec) Location {
	if saved, ok := s.positions[viewKey{s.CurrentEntry, spec.Route}]; ok {
		return saved
	}
	return s.routes.initial(s, spec)
}

func (s *State) remember() {
	spec, ok := s.routes.Spec(s.Route)
	if !ok || spec.Transient || spec.Modal {
		return
	}
	key := viewKey{s.CurrentEntry, s.Route}
	location := s.Location
	if location.Area == NavigationArea {
		if previous, ok := s.positions[key]; ok {
			location.Focus, location.Area = previous.Focus, previous.Area
		} else {
			location = s.routes.initial(s, spec)
		}
	}
	s.positions[key] = location
	if s.CurrentEntry != "" {
		s.sessions[s.CurrentEntry] = session{Location: location, history: slices.Clone(s.history)}
	}
}

func (s *State) show(location Location) {
	s.Location = location
	s.Revision++
}
