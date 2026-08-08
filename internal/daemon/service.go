package daemon

import (
	"context"
	"sync"
	"time"

	"github.com/carellano/herdr-apps/internal/model"
)

// ActionExecutor performs only daemon-validated action intents.
type ActionExecutor interface {
	Execute(context.Context, model.ActionRequest, model.Application) (model.ActionResult, error)
}

const rebuildUnavailable = "application state refresh is unavailable"

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

// MarkStale fail-closes existing applications after a post-start rebuild failure.
// The reason is intentionally stable so repeated failures do not churn revisions.
func (s *Service) MarkStale() model.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.snapshot.Applications) == 0 {
		return cloneSnapshot(s.snapshot)
	}
	next := cloneSnapshot(s.snapshot)
	changed := false
	for i := range next.Applications {
		app := &next.Applications[i]
		if !app.Association.Stale {
			app.Association.Stale = true
			changed = true
		}
		found := false
		for _, evidence := range app.Evidence {
			if evidence.Source == "runtime" && evidence.Unavailable == rebuildUnavailable {
				found = true
				break
			}
		}
		if !found {
			app.Evidence = append(app.Evidence, model.Evidence{Source: "runtime", ObservedAt: time.Now().UTC(), Unavailable: rebuildUnavailable})
			changed = true
		}
	}
	if !changed {
		return cloneSnapshot(s.snapshot)
	}
	next.Revision = s.snapshot.Revision + 1
	s.snapshot = next
	return cloneSnapshot(s.snapshot)
}

// ExecuteAction validates and dispatches while holding the snapshot read lock.
// Replace may wait for this bounded action, preventing validation from racing publication.
func (s *Service) ExecuteAction(ctx context.Context, request model.IPCRequest) (model.ActionResult, uint64, *model.IPCError) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !supportedAction(request.Action) {
		return model.ActionResult{}, s.snapshot.Revision, &model.IPCError{Code: "unsupported_action", Message: "action is unavailable"}
	}
	if request.ObservedRevision != s.snapshot.Revision {
		return model.ActionResult{}, s.snapshot.Revision, &model.IPCError{Code: "stale_revision", Message: "refresh applications before requesting an action"}
	}
	var application *model.Application
	for i := range s.snapshot.Applications {
		if s.snapshot.Applications[i].ID == request.Target {
			application = &s.snapshot.Applications[i]
			break
		}
	}
	if application == nil || application.Identity != request.Identity {
		return model.ActionResult{}, s.snapshot.Revision, &model.IPCError{Code: "invalid_target", Message: "application identity changed; refresh before requesting an action"}
	}
	if s.Actions == nil {
		return model.ActionResult{}, s.snapshot.Revision, &model.IPCError{Code: "action_unavailable", Message: "daemon action executor is unavailable"}
	}
	result, err := s.Actions.Execute(ctx, model.ActionRequest{Action: request.Action, Confirmed: request.Confirmed}, *application)
	if err != nil {
		return model.ActionResult{}, s.snapshot.Revision, actionError(err)
	}
	return result, s.snapshot.Revision, nil
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
