package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/carellano/herdr-dev-servers/internal/daemon"
	"github.com/carellano/herdr-dev-servers/internal/discovery"
	"github.com/carellano/herdr-dev-servers/internal/herdr"
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

func TestFactoryRuntimeRecoversOnlyKnownPaneListenersAfterDarwinGlobalOmission(t *testing.T) {
	topology := json.RawMessage(`{"workspaces":[{"workspace_id":"w","label":"Work"}],"tabs":[{"tab_id":"t","workspace_id":"w","label":"Tab"}],"panes":[{"pane_id":"p","tab_id":"t","workspace_id":"w","label":"Pane"}]}`)
	for _, tt := range []struct {
		name      string
		global    []discovery.Listener
		targeted  []discovery.Listener
		targetErr error
		wantPorts []int
		wantPIDs  []int
	}{
		{
			name:      "recovers omitted known foreground listener",
			targeted:  []discovery.Listener{{PID: 6, Port: 3000, Address: "127.0.0.1"}},
			wantPorts: []int{3000}, wantPIDs: []int{6},
		},
		{
			name:      "deduplicates targeted result already in global scan",
			global:    []discovery.Listener{{PID: 6, Port: 3000, Address: "127.0.0.1"}},
			targeted:  []discovery.Listener{{PID: 6, Port: 3000, Address: "127.0.0.1"}, {PID: 6, Port: 4000, Address: "127.0.0.1"}},
			wantPorts: []int{3000, 4000}, wantPIDs: []int{6},
		},
		{
			name:      "keeps global result when targeted lookup fails",
			global:    []discovery.Listener{{PID: 6, Port: 3000, Address: "127.0.0.1"}},
			targetErr: errors.New("malformed targeted lsof output"),
			wantPorts: []int{3000}, wantPIDs: []int{6},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			scanner := &knownPaneScanner{global: tt.global, targeted: tt.targeted, targetErr: tt.targetErr}
			factory := Factory{
				Scanner: scanner, Processes: knownPaneProcesses{},
				Snapshot: func(context.Context) (json.RawMessage, error) { return topology, nil },
				ProcessInfo: func(context.Context, string) (herdr.ProcessInfoResponse, error) {
					return herdr.ProcessInfoResponse{PaneID: "p", ShellPID: 4, ForegroundProcesses: []herdr.ProcessInfo{{PID: 6, Command: "node app.js", CWD: "/work"}}}, nil
				},
			}
			service := &daemon.Service{}
			snapshot, err := factory.Runtime(service).Rebuild(context.Background(), service.Snapshot())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(scanner.pids, tt.wantPIDs) {
				t.Fatalf("targeted PIDs = %#v, want %#v", scanner.pids, tt.wantPIDs)
			}
			if len(snapshot.Applications) != 1 || snapshot.Applications[0].Association.Confidence != "high" {
				t.Fatalf("applications = %#v", snapshot.Applications)
			}
			var ports []int
			for _, endpoint := range snapshot.Applications[0].Endpoints {
				ports = append(ports, endpoint.Port)
			}
			if !reflect.DeepEqual(ports, tt.wantPorts) {
				t.Fatalf("ports = %#v, want %#v", ports, tt.wantPorts)
			}
		})
	}
}

func TestFactoryRuntimeFiltersIgnoredPortsBeforeCorrelation(t *testing.T) {
	topology := json.RawMessage(`{"workspaces":[{"workspace_id":"w","label":"Work"}],"tabs":[{"tab_id":"t","workspace_id":"w","label":"Tab"}],"panes":[{"pane_id":"p","tab_id":"t","workspace_id":"w","label":"Pane"}]}`)
	factory := Factory{
		Ignored: func(port int) bool { return port == 3000 },
		Scanner: emptyScanner{}, Processes: knownPaneProcesses{},
		Snapshot: func(context.Context) (json.RawMessage, error) { return topology, nil },
		ProcessInfo: func(context.Context, string) (herdr.ProcessInfoResponse, error) {
			return herdr.ProcessInfoResponse{PaneID: "p", ForegroundProcesses: []herdr.ProcessInfo{{PID: 6, Command: "node app.js", CWD: "/work"}}}, nil
		},
	}
	scanner := &knownPaneScanner{global: []discovery.Listener{{PID: 6, Port: 3000, Address: "127.0.0.1"}, {PID: 6, Port: 4000, Address: "127.0.0.1"}}}
	factory.Scanner = scanner
	service := &daemon.Service{}
	snapshot, err := factory.Runtime(service).Rebuild(context.Background(), service.Snapshot())
	if err != nil || len(snapshot.Applications) != 1 || len(snapshot.Applications[0].Endpoints) != 1 || snapshot.Applications[0].Endpoints[0].Port != 4000 {
		t.Fatalf("snapshot=%#v err=%v", snapshot, err)
	}
}

type emptyScanner struct{}

func (emptyScanner) Scan(context.Context) ([]discovery.Listener, error) { return nil, nil }

type emptyProcesses struct{}

func (emptyProcesses) Lookup(context.Context, int) (discovery.Process, error) {
	return discovery.Process{}, nil
}

type knownPaneScanner struct {
	global, targeted []discovery.Listener
	targetErr        error
	pids             []int
}

func (s *knownPaneScanner) Scan(context.Context) ([]discovery.Listener, error) { return s.global, nil }
func (s *knownPaneScanner) ScanPIDs(_ context.Context, pids []int) ([]discovery.Listener, error) {
	s.pids = append([]int(nil), pids...)
	return s.targeted, s.targetErr
}

type knownPaneProcesses struct{}

func (knownPaneProcesses) Lookup(_ context.Context, pid int) (discovery.Process, error) {
	return discovery.Process{PID: pid, StartTime: "start", PGID: pid, Executable: "node", Args: []string{"node", "app.js"}, CWD: "/work"}, nil
}
