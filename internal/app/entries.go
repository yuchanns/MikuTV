package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/yuchanns/MikuTV/internal/provider"
)

type Entry struct {
	ID           string                    `json:"id"`
	SourceID     string                    `json:"sourceId"`
	Name         string                    `json:"name"`
	Description  string                    `json:"description"`
	Artwork      string                    `json:"artwork"`
	Available    bool                      `json:"available"`
	Capabilities provider.Capabilities     `json:"capabilities,omitempty"`
	Auth         provider.AuthPresentation `json:"auth,omitzero"`
	Generation   uint64                    `json:"-"`
}

func (s *State) SetEntries(entries []Entry) error {
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.ID) == "" || strings.TrimSpace(entry.SourceID) == "" || strings.TrimSpace(entry.Name) == "" {
			return fmt.Errorf("entry identity, source and name are required")
		}
		if seen[entry.ID] {
			return fmt.Errorf("duplicate entry identity %q", entry.ID)
		}
		seen[entry.ID] = true
	}
	s.entries = slices.Clone(entries)
	for index := range s.entries {
		s.entries[index].Capabilities = slices.Clone(s.entries[index].Capabilities)
	}
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
	} else {
		s.restoreSession(s.Location, s.history)
	}
	return nil
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
		s.restoreSession(saved.Location, saved.history)
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
