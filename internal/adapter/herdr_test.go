package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/carellano/herdr-apps/internal/daemon"
	"github.com/carellano/herdr-apps/internal/discovery"
	"github.com/carellano/herdr-apps/internal/herdr"
)

func TestTopologyConvertsFlattenedHerdrSnapshot(t *testing.T) {
	raw := json.RawMessage(`{"workspaces":[{"workspace_id":"w","label":"Work"}],"tabs":[{"tab_id":"t","workspace_id":"w","label":"Tab"}],"panes":[{"pane_id":"p","tab_id":"t","workspace_id":"w","label":"Pane"}]}`)
	got, err := Topology(raw)
	if err != nil {
		t.Fatal(err)
	}
	pane := got.Workspaces[0].Tabs[0].Panes[0]
	if pane.ID != "p" || got.Workspaces[0].Label != "Work" || got.Workspaces[0].Tabs[0].Label != "Tab" {
		t.Fatalf("topology = %#v", got)
	}
}

func TestPaneProcessesRejectsMissingPane(t *testing.T) {
	_, err := PaneProcesses(context.Background(), []daemon.Pane{{ID: "missing"}}, func(context.Context, string) (herdr.ProcessInfoResponse, error) {
		return herdr.ProcessInfoResponse{}, errors.New("not found")
	})
	var unavailable *PaneUnavailableError
	if !errors.As(err, &unavailable) || unavailable.PaneID != "missing" {
		t.Fatalf("error = %v", err)
	}
}

func TestFactorySharesServiceAndConvertsProcessInfo(t *testing.T) {
	service := &daemon.Service{}
	factory := Factory{Scanner: emptyScanner{}, Processes: emptyProcesses{}, ProcessInfo: func(context.Context, string) (herdr.ProcessInfoResponse, error) {
		return herdr.ProcessInfoResponse{PaneID: "p", ShellPID: 4, ForegroundProcessGroupID: 5, ForegroundProcesses: []herdr.ProcessInfo{{PID: 6, Command: "node app.js", CWD: "/work"}}}, nil
	}}
	runtime := factory.Runtime(service)
	if runtime.Service != service {
		t.Fatal("runtime does not use the IPC service")
	}
	processes, err := PaneProcesses(context.Background(), []daemon.Pane{{ID: "p"}}, factory.ProcessInfo)
	if err != nil || processes[0].PGID != 5 || processes[0].Foreground[0].Argv[1] != "app.js" {
		t.Fatalf("processes=%#v err=%v", processes, err)
	}
}

func TestNewFactorySubscribesToSupportedTopologyEvents(t *testing.T) {
	subscriptions := topologySubscriptions()
	for _, subscription := range subscriptions {
		if subscription.Type == "session.changed" {
			t.Fatal("subscribed to unsupported session.changed")
		}
	}
	if len(subscriptions) == 0 || subscriptions[0].Type != "workspace.created" {
		t.Fatalf("subscriptions = %#v", subscriptions)
	}
}

func TestDefaultSocketPrefersHerdrSocketOverride(t *testing.T) {
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/herdr.sock")
	if got := DefaultSocket(); got != "/tmp/herdr.sock" {
		t.Fatalf("DefaultSocket() = %q", got)
	}
}

type emptyScanner struct{}

func (emptyScanner) Scan(context.Context) ([]discovery.Listener, error) { return nil, nil }

type emptyProcesses struct{}

func (emptyProcesses) Lookup(context.Context, int) (discovery.Process, error) {
	return discovery.Process{}, nil
}
