package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/carellano/herdr-apps/internal/model"
)

type recordingClient struct {
	requests  []model.IPCRequest
	responses []model.IPCResponse
}

func (f *recordingClient) Request(_ context.Context, request model.IPCRequest) (model.IPCResponse, error) {
	f.requests = append(f.requests, request)
	if len(f.responses) == 0 {
		return model.IPCResponse{}, errors.New("missing response")
	}
	response := f.responses[0]
	f.responses = f.responses[1:]
	return response, nil
}

func TestExecuteActionCarriesSafetyContextAndRefreshes(t *testing.T) {
	app := model.Application{ID: "app", Identity: model.ProcessIdentity{PID: 7, StartTime: "one", PGID: 7, Key: "app"}}
	client := &recordingClient{responses: []model.IPCResponse{
		{Result: model.ActionResult{Outcome: "fallback-workspace-tab", Warning: "exact pane evidence is unavailable"}},
		{Result: model.Snapshot{Revision: 2, Applications: []model.Application{app}}},
	}}
	result, snapshot, err := ExecuteAction(context.Background(), client, "f", app, 1, false)
	if err != nil || result.Warning == "" || snapshot.Revision != 2 {
		t.Fatalf("result=%#v snapshot=%#v err=%v", result, snapshot, err)
	}
	if got := client.requests; len(got) != 2 || got[0].Action != "focus" || got[0].Confirmed || got[0].ObservedRevision != 1 || got[0].Identity != app.Identity || got[1].Method != "list" {
		t.Fatalf("requests=%#v", got)
	}
}

func TestExecuteActionCarriesDestructiveConfirmation(t *testing.T) {
	app := model.Application{ID: "app", Identity: model.ProcessIdentity{PID: 7, StartTime: "one", PGID: 7, Key: "app"}}
	client := &recordingClient{responses: []model.IPCResponse{{Result: model.ActionResult{Outcome: "term-sent"}}, {Result: model.Snapshot{}}}}
	if _, _, err := ExecuteAction(context.Background(), client, "t", app, 1, true); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 2 || client.requests[0].Action != "terminate" || !client.requests[0].Confirmed {
		t.Fatalf("requests=%#v", client.requests)
	}
}
