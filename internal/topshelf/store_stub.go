//go:build !(ios && tvos && arm64)

package topshelf

import "sync"

type SharedStore struct {
	mu       sync.Mutex
	snapshot Snapshot
	notified int
}

func NewSharedStore() (*SharedStore, error) {
	return &SharedStore{}, nil
}

func (s *SharedStore) Mode() string { return "memory" }

func (s *SharedStore) Load() (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SanitizeSnapshot(s.snapshot), nil
}

func (s *SharedStore) Save(snapshot Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot = SanitizeSnapshot(snapshot)
	return nil
}

func (s *SharedStore) NotifyChanged() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notified++
}
