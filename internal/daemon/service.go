package daemon

import (
	"context"
	"sync"

	"github.com/carellano/herdr-apps/internal/model"
)

// ActionExecutor performs only daemon-validated action intents.
type ActionExecutor interface {
	Execute(context.Context, model.ActionRequest, model.Application) (model.ActionResult, error)
}

// Service is the single daemon authority for complete application revisions.
type Service struct {
	mu       sync.RWMutex
	snapshot model.Snapshot
	Actions  ActionExecutor
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
	if s.snapshot.SemanticallyEqual(next) {
		return cloneSnapshot(s.snapshot)
	}
	next.Revision = s.snapshot.Revision + 1
	s.snapshot = cloneSnapshot(next)
	return cloneSnapshot(s.snapshot)
}

func cloneSnapshot(snapshot model.Snapshot) model.Snapshot {
	copySnapshot := snapshot
	copySnapshot.Applications = append([]model.Application(nil), snapshot.Applications...)
	for i := range copySnapshot.Applications {
		app := &copySnapshot.Applications[i]
		app.Endpoints = append([]model.Endpoint(nil), app.Endpoints...)
		app.Evidence = append([]model.Evidence(nil), app.Evidence...)
		for j := range app.Evidence {
			app.Evidence[j].Argv = append([]string(nil), app.Evidence[j].Argv...)
			app.Evidence[j].Ancestry = append([]int(nil), app.Evidence[j].Ancestry...)
		}
	}
	return copySnapshot
}
