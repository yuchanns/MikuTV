package app

import (
	"fmt"
	"slices"
	"strings"
)

type Entry struct {
	ID          string `json:"id"`
	SourceID    string `json:"sourceId"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Artwork     string `json:"artwork"`
	Available   bool   `json:"available"`
	Generation  uint64 `json:"-"`
}

func (s *State) SetEntries(entries []Entry) error {
	seen := make(map[string]bool, len(entries))
	previous := make(map[string]Entry, len(s.entries))
	for _, entry := range s.entries {
		previous[entry.ID] = entry
	}
	next := make([]Entry, len(entries))
	for i, entry := range entries {
		if strings.TrimSpace(entry.ID) == "" || strings.TrimSpace(entry.SourceID) == "" || strings.TrimSpace(entry.Name) == "" {
			return fmt.Errorf("entry identity, source and name are required")
		}
		if seen[entry.ID] {
			return fmt.Errorf("duplicate entry identity %q", entry.ID)
		}
		seen[entry.ID] = true
		if old, ok := previous[entry.ID]; ok {
			if entry.Generation == 0 {
				entry.Generation = old.Generation
			}
			if old.SourceID != entry.SourceID || old.Name != entry.Name ||
				old.Description != entry.Description || old.Artwork != entry.Artwork ||
				old.Available != entry.Available {
				entry.Generation = max(entry.Generation, old.Generation) + 1
			}
		}
		next[i] = entry
	}
	s.entries = next
	if !s.available(s.CurrentEntry) {
		s.CurrentEntry = ""
		s.history = nil
		spec, _ := s.routes.Resolve(s.routes.Picker)
		s.show(s.routes.initial(s, spec))
	} else if s.Route == s.routes.Picker {
		valid := slices.ContainsFunc(s.entries, func(entry Entry) bool { return entry.Available && EntryFocus(entry.ID) == s.Focus })
		if !valid {
			s.Focus = s.PickerFocus()
		}
	}
	return nil
}

// UpdateEntries reconciles hot-added, updated, or removed entries while keeping navigation consistent.
func (s *State) UpdateEntries(entries []Entry) {
	previous := make(map[string]Entry, len(s.entries))
	for _, entry := range s.entries {
		previous[entry.ID] = entry
	}
	next := make([]Entry, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.ID) == "" || strings.TrimSpace(entry.SourceID) == "" || strings.TrimSpace(entry.Name) == "" {
			continue
		}
		if seen[entry.ID] {
			continue
		}
		seen[entry.ID] = true
		if old, ok := previous[entry.ID]; ok {
			if entry.Generation == 0 {
				entry.Generation = old.Generation
			}
			if old.SourceID != entry.SourceID || old.Name != entry.Name ||
				old.Description != entry.Description || old.Artwork != entry.Artwork ||
				old.Available != entry.Available {
				entry.Generation = max(entry.Generation, old.Generation) + 1
			}
		}
		next = append(next, entry)
	}
	s.entries = next
	if !s.available(s.CurrentEntry) {
		s.CurrentEntry = ""
		s.history = nil
		spec, _ := s.routes.Resolve(s.routes.Picker)
		s.show(s.routes.initial(s, spec))
		return
	}
	if s.Route == s.routes.Picker {
		valid := slices.ContainsFunc(s.entries, func(entry Entry) bool { return entry.Available && EntryFocus(entry.ID) == s.Focus })
		if !valid {
			s.Focus = s.PickerFocus()
			s.Revision++
		}
	}
}

func (s *State) AvailableEntries() []Entry {
	var entries []Entry
	for _, entry := range s.entries {
		if entry.Available {
			entries = append(entries, entry)
		}
	}
	return entries
}

func (s *State) Current() (Entry, bool) {
	for _, entry := range s.entries {
		if entry.ID == s.CurrentEntry && entry.Available {
			return entry, true
		}
	}
	return Entry{}, false
}

func (s *State) OpenEntries(preferred string) {
	s.Navigate(s.routes.Picker)
	if s.available(preferred) {
		s.Focus = EntryFocus(preferred)
	}
}

func (s *State) SelectEntry(id string) bool {
	if !s.available(id) {
		return false
	}
	if id == s.CurrentEntry && len(s.history) > 0 {
		s.Back()
		return true
	}
	s.remember()
	s.CurrentEntry = id
	if saved, ok := s.sessions[id]; ok {
		s.history = slices.Clone(saved.history)
		s.show(saved.Location)
	} else {
		s.history = nil
		spec, _ := s.routes.Resolve(s.routes.Root)
		s.show(s.routes.initial(s, spec))
	}
	return true
}

func (s *State) PickerFocus() FocusID {
	if s.available(s.CurrentEntry) {
		return EntryFocus(s.CurrentEntry)
	}
	for _, entry := range s.entries {
		if entry.Available {
			return EntryFocus(entry.ID)
		}
	}
	return ManagePlugins
}

func (s *State) available(id string) bool {
	return id != "" && slices.ContainsFunc(s.entries, func(entry Entry) bool { return entry.ID == id && entry.Available })
}
