package app

import (
	"slices"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type Entry struct {
	ID           string                      `json:"id"`
	SourceID     string                      `json:"sourceId"`
	Name         string                      `json:"name"`
	Description  string                      `json:"description"`
	Artwork      string                      `json:"artwork"`
	Capabilities provider.Capabilities       `json:"capabilities,omitempty"`
	Auth         provider.AuthPresentation   `json:"auth,omitzero"`
	Presentation provider.SourcePresentation `json:"presentation,omitzero"`
	Generation   uint64                      `json:"-"`
}

// Entries returns a snapshot of the registered entries.
func (s *State) Entries() []Entry { return s.entries }

// UpdateEntries replaces the active entry list (e.g. on plugin install,
// in-place update, disable, or uninstall) and reconciles the current route.
func (s *State) UpdateEntries(entries []Entry) {
	previous := make(map[string]Entry, len(s.entries))
	for _, item := range s.entries {
		previous[item.ID] = item
	}
	next := make([]Entry, len(entries))
	present := make(map[string]bool, len(entries))
	for i, item := range entries {
		present[item.ID] = true
		if old, ok := previous[item.ID]; ok && item.Generation == old.Generation {
			if item.SourceID != old.SourceID || item.Name != old.Name || item.Description != old.Description ||
				item.Artwork != old.Artwork || item.Auth != old.Auth || item.Presentation != old.Presentation ||
				!slices.Equal(item.Capabilities, old.Capabilities) {
				item.Generation = old.Generation + 1
			}
		}
		next[i] = item
	}
	s.entries = next
	for key := range s.positions {
		if key.EntryID != "" && !present[key.EntryID] {
			delete(s.positions, key)
		}
	}
	for id := range s.sessions {
		if !present[id] {
			delete(s.sessions, id)
		}
	}
	if s.CurrentEntry != "" && !present[s.CurrentEntry] {
		s.CurrentEntry = ""
		s.history = nil
		spec, _ := s.routes.Resolve(s.routes.Picker)
		s.show(s.routes.initial(s, spec))
		return
	}
	if s.CurrentEntry != "" {
		if spec, ok := s.routes.Resolve(s.Route); !ok || !s.Allows(spec) {
			s.history = nil
			root, _ := s.routes.Resolve(s.root())
			s.show(s.routes.initial(s, root))
			return
		}
	}
	s.Revision++
}

func (s *State) Current() (Entry, bool) { return s.entry(s.CurrentEntry) }

func (s *State) entry(id string) (Entry, bool) {
	for _, entry := range s.entries {
		if entry.ID == id {
			return entry, true
		}
	}
	return Entry{}, false
}

func (s *State) OpenEntries(preferred string) {
	s.Navigate(s.routes.Picker)
	if _, ok := s.entry(preferred); ok {
		s.Focus = EntryFocus(preferred)
	}
}

func (s *State) SelectEntry(id string) bool {
	if _, ok := s.entry(id); !ok {
		return false
	}
	if id == s.CurrentEntry && len(s.history) > 0 {
		s.Back()
		return true
	}
	s.remember()
	s.CurrentEntry = id
	if saved, ok := s.sessions[id]; ok {
		s.restoreSession(saved.Location, saved.history)
	} else {
		s.history = nil
		spec, _ := s.routes.Resolve(s.routes.Root)
		s.show(s.routes.initial(s, spec))
	}
	return true
}

func (s *State) PickerFocus() FocusID {
	if s.CurrentEntry != "" {
		return EntryFocus(s.CurrentEntry)
	}
	if len(s.entries) != 0 {
		return EntryFocus(s.entries[0].ID)
	}
	return ManagePlugins
}
