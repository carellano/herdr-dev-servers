package actions

import (
	"context"
	"fmt"

	"github.com/carellano/herdr-apps/internal/model"
)

// Executor adapts the existing evidence-backed action Service to daemon IPC.
type Executor struct{ Service Service }

func (e Executor) Execute(ctx context.Context, request model.ActionRequest, app model.Application) (model.ActionResult, error) {
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
		result = e.Service.Terminate(ctx, app, request.Confirmed)
	case "kill":
		result = e.Service.ForceKill(ctx, app, request.Confirmed)
	default:
		return model.ActionResult{}, fmt.Errorf("unsupported action %q", request.Action)
	}
	return model.ActionResult{Outcome: string(result.Outcome), Warning: result.Warning, ForceEligible: result.ForceEligible}, err
}

func appURL(app model.Application) string {
	for _, endpoint := range app.Endpoints {
		if endpoint.URL != "" {
			return endpoint.URL
		}
	}
	return ""
}
