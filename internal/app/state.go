package app

import (
	"fmt"
	"slices"
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
	s := &State{routes: routes, positions: make(map[viewKey]Location), sessions: make(map[string]session)}
	if err := s.SetEntries(entries); err != nil {
		return nil, err
	}
	if s.available(recent) {
		s.CurrentEntry = recent
		spec, _ := routes.Resolve(routes.Root)
		s.show(routes.initial(s, spec))
	}
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
	if previous, ok := s.positions[key]; ok && location.Area == NavigationArea {
		location.Focus, location.Area = previous.Focus, previous.Area
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
