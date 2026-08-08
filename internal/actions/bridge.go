package actions

import (
	"context"
	"fmt"
	"sync"

	"github.com/carellano/herdr-apps/internal/model"
)

// Executor adapts the existing evidence-backed action Service to daemon IPC.
// It owns force-KILL eligibility because local IPC clients are not trusted authority.
type Executor struct {
	Service Service
	mu      sync.Mutex
	force   map[string]model.ProcessIdentity
}

func NewExecutor(service Service) *Executor {
	return &Executor{Service: service, force: map[string]model.ProcessIdentity{}}
}

func (e *Executor) Execute(ctx context.Context, request model.ActionRequest, app model.Application) (model.ActionResult, error) {
	var result Result
	var err error
	switch request.Action {
	case "open":
		result, err = e.Service.Open(ctx, appURL(app))
	case "copy":
		result, err = e.Service.Copy(appURL(app))
	case "focus":
		result = e.Service.FocusApplication(app)
	case "terminate":
		e.clearForce(app)
		result = e.Service.Terminate(ctx, app, request.Confirmed)
		if request.Confirmed && result.ForceEligible {
			e.mu.Lock()
			if e.force == nil {
				e.force = map[string]model.ProcessIdentity{}
			}
			e.force[app.ID] = app.Identity
			e.mu.Unlock()
		}
	case "kill":
		if !e.consumeForce(app, request.Confirmed) {
			return model.ActionResult{Outcome: string(OutcomeUnavailable), Warning: "force kill requires a confirmed TERM grace expiry for this process"}, nil
		}
		result = e.Service.ForceKill(ctx, app, request.Confirmed)
	default:
		return model.ActionResult{}, fmt.Errorf("unsupported action %q", request.Action)
	}
	return model.ActionResult{Outcome: string(result.Outcome), Warning: result.Warning, ForceEligible: result.ForceEligible}, err
}

func (e *Executor) clearForce(app model.Application) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.force, app.ID)
}

func (e *Executor) consumeForce(app model.Application, confirmed bool) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	identity, ok := e.force[app.ID]
	delete(e.force, app.ID)
	return confirmed && ok && identity == app.Identity
}

func appURL(app model.Application) string {
	for _, endpoint := range app.Endpoints {
		if endpoint.URL != "" {
			return endpoint.URL
		}
	}
	return ""
}
