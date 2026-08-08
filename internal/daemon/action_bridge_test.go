package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/carellano/herdr-apps/internal/actions"
	"github.com/carellano/herdr-apps/internal/herdr"
	"github.com/carellano/herdr-apps/internal/model"
)

type fakeActionExecutor struct {
	result model.ActionResult
	err    error
	calls  int
}

type ipcProcessInspector struct{ identity model.ProcessIdentity }

func (i ipcProcessInspector) Inspect(context.Context, int) (model.ProcessIdentity, error) {
	return i.identity, nil
}
func (ipcProcessInspector) Wait(context.Context, model.ProcessIdentity) error {
	return context.DeadlineExceeded
}

type ipcSignaler struct{ calls int }

func (s *ipcSignaler) SignalPGID(int, actions.Signal) error {
	s.calls++
	return nil
}

func (f *fakeActionExecutor) Execute(_ context.Context, _ model.ActionRequest, _ model.Application) (model.ActionResult, error) {
	f.calls++
	return f.result, f.err
}

func TestServeJSONLDispatchesVerifiedActions(t *testing.T) {
	app := model.Application{ID: "app", Identity: model.ProcessIdentity{PID: 7, StartTime: "one", PGID: 7, Key: "app"}}
	executor := &fakeActionExecutor{result: model.ActionResult{Outcome: "exact-pane"}}
	service := &Service{Actions: executor}
	snapshot := service.Replace(model.Snapshot{Applications: []model.Application{app}})
	request := model.IPCRequest{Version: IPCVersion, RequestID: "focus", ObservedRevision: snapshot.Revision, Method: "action", Action: "focus", Target: app.ID, Identity: app.Identity}
	response := serveAction(t, service, request)
	if response.Error != nil || executor.calls != 1 {
		t.Fatalf("response=%#v calls=%d", response, executor.calls)
	}
	if result := decodeActionResult(t, response); result.Outcome != "exact-pane" {
		t.Fatalf("result=%#v", result)
	}
}

func TestServeJSONLRequiresDaemonForceEligibility(t *testing.T) {
	app := model.Application{ID: "app", Identity: model.ProcessIdentity{PID: 7, StartTime: "one", PGID: 7, Key: "app"}, Association: model.Association{Confidence: model.ConfidenceHigh}}
	signaler := &ipcSignaler{}
	executor := actions.NewExecutor(actions.Service{Processes: ipcProcessInspector{identity: app.Identity}, Signaler: signaler, Grace: time.Millisecond})
	service := &Service{Actions: executor}
	snapshot := service.Replace(model.Snapshot{Applications: []model.Application{app}})
	request := func(action string) model.IPCRequest {
		return model.IPCRequest{Version: IPCVersion, RequestID: action, ObservedRevision: snapshot.Revision, Method: "action", Action: action, Target: app.ID, Identity: app.Identity, Confirmed: true}
	}
	if result := decodeActionResult(t, serveAction(t, service, request("kill"))); result.Outcome != "unavailable" || signaler.calls != 0 {
		t.Fatalf("unapproved KILL result=%#v calls=%d", result, signaler.calls)
	}
	if result := decodeActionResult(t, serveAction(t, service, request("terminate"))); !result.ForceEligible || signaler.calls != 1 {
		t.Fatalf("TERM result=%#v calls=%d", result, signaler.calls)
	}
	if result := decodeActionResult(t, serveAction(t, service, request("kill"))); result.Outcome != "kill-sent" || signaler.calls != 2 {
		t.Fatalf("eligible KILL result=%#v calls=%d", result, signaler.calls)
	}
}

func TestServeJSONLRejectsStaleOrInvalidActionTargets(t *testing.T) {
	app := model.Application{ID: "app", Identity: model.ProcessIdentity{PID: 7, StartTime: "one", PGID: 7, Key: "app"}}
	for _, request := range []model.IPCRequest{
		{Version: IPCVersion, RequestID: "stale", ObservedRevision: 0, Method: "action", Action: "focus", Target: app.ID, Identity: app.Identity},
		{Version: IPCVersion, RequestID: "changed", ObservedRevision: 1, Method: "action", Action: "focus", Target: app.ID, Identity: model.ProcessIdentity{PID: 7, StartTime: "two", PGID: 7, Key: "app"}},
	} {
		t.Run(request.RequestID, func(t *testing.T) {
			executor := &fakeActionExecutor{}
			service := &Service{Actions: executor}
			service.Replace(model.Snapshot{Applications: []model.Application{app}})
			response := serveAction(t, service, request)
			if response.Error == nil || executor.calls != 0 {
				t.Fatalf("response=%#v calls=%d", response, executor.calls)
			}
		})
	}
}

func TestServeJSONLPreservesPartialAndTypedFailures(t *testing.T) {
	app := model.Application{ID: "app", Identity: model.ProcessIdentity{PID: 7, StartTime: "one", PGID: 7, Key: "app"}}
	for _, tc := range []struct {
		name   string
		action string
		result model.ActionResult
		err    error
		code   string
	}{
		{name: "partial focus", action: "focus", result: model.ActionResult{Outcome: "fallback-workspace-tab", Warning: "exact pane evidence is unavailable"}},
		{name: "Herdr failure", action: "focus", err: &herdr.APIError{Code: "denied", Message: "focus unavailable"}, code: "herdr_denied"},
		{name: "unsupported", action: "surprise", code: "unsupported_action"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			executor := &fakeActionExecutor{result: tc.result, err: tc.err}
			service := &Service{Actions: executor}
			snapshot := service.Replace(model.Snapshot{Applications: []model.Application{app}})
			response := serveAction(t, service, model.IPCRequest{Version: IPCVersion, RequestID: tc.name, ObservedRevision: snapshot.Revision, Method: "action", Action: tc.action, Target: app.ID, Identity: app.Identity})
			if tc.code != "" {
				if response.Error == nil || response.Error.Code != tc.code {
					t.Fatalf("error=%#v", response.Error)
				}
				return
			}
			if result := decodeActionResult(t, response); result.Warning == "" || result.Outcome != "fallback-workspace-tab" {
				t.Fatalf("result=%#v", result)
			}
		})
	}
}

func TestIPCClientErrorRetainsProtocolCode(t *testing.T) {
	err := &IPCClientError{Payload: &model.IPCError{Code: "herdr_denied", Message: "focus unavailable"}}
	if err.Payload.Code != "herdr_denied" || err.Error() != "daemon herdr_denied: focus unavailable" {
		t.Fatalf("error=%#v", err)
	}
}

func TestActionBridgeOverFakeSocketHasNoExternalSideEffects(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	app := model.Application{ID: "app", Identity: model.ProcessIdentity{PID: 7, StartTime: "one", PGID: 7, Key: "app"}}
	executor := &fakeActionExecutor{result: model.ActionResult{Outcome: "exact-pane"}}
	service := &Service{Actions: executor}
	snapshot := service.Replace(model.Snapshot{Applications: []model.Application{app}})
	done := make(chan error, 1)
	go func() { done <- service.ServeJSONL(server, server) }()
	request := model.IPCRequest{Version: IPCVersion, RequestID: "fake-socket", ObservedRevision: snapshot.Revision, Method: "action", Action: "focus", Target: app.ID, Identity: app.Identity}
	if err := json.NewEncoder(client).Encode(request); err != nil {
		t.Fatal(err)
	}
	var response model.IPCResponse
	if err := json.NewDecoder(client).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error != nil || executor.calls != 1 {
		t.Fatalf("response=%#v calls=%d", response, executor.calls)
	}
	client.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func serveAction(t *testing.T, service *Service, request model.IPCRequest) model.IPCResponse {
	t.Helper()
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := service.ServeJSONL(strings.NewReader(string(data)+"\n"), &output); err != nil {
		t.Fatal(err)
	}
	var response model.IPCResponse
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func decodeActionResult(t *testing.T, response model.IPCResponse) model.ActionResult {
	t.Helper()
	data, _ := json.Marshal(response.Result)
	var result model.ActionResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
