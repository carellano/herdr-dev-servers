package daemon

import (
	"sync"

	"github.com/carellano/herdr-apps/internal/model"
)

// Service is the single daemon authority for complete application revisions.
type Service struct {
	mu       sync.RWMutex
	snapshot model.Snapshot
}

// Snapshot returns a defensive copy of the latest complete graph.
func (s *Service) Snapshot() model.Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSnapshot(s.snapshot)
}

// Replace atomically publishes a complete graph and advances its revision.
func (s *Service) Replace(next model.Snapshot) model.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	next.Revision = s.snapshot.Revision + 1
	s.snapshot = cloneSnapshot(next)
	return cloneSnapshot(s.snapshot)
}

func cloneSnapshot(snapshot model.Snapshot) model.Snapshot {
	copySnapshot := snapshot
	copySnapshot.Applications = append([]model.Application(nil), snapshot.Applications...)
	return copySnapshot
}
