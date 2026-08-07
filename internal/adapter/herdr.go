// Package adapter connects Herdr's raw JSONL API to daemon reconciliation.
package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/carellano/herdr-apps/internal/daemon"
	"github.com/carellano/herdr-apps/internal/discovery"
	"github.com/carellano/herdr-apps/internal/herdr"
	"github.com/carellano/herdr-apps/internal/metadata"
	"github.com/carellano/herdr-apps/internal/model"
)

// PaneUnavailableError identifies a pane whose process evidence is unavailable.
type PaneUnavailableError struct {
	PaneID string
	Err    error
}

func (e *PaneUnavailableError) Error() string {
	return fmt.Sprintf("pane %s unavailable: %v", e.PaneID, e.Err)
}
func (e *PaneUnavailableError) Unwrap() error { return e.Err }

// Topology converts the supported flat session snapshot fields without discarding IDs or labels.
func Topology(raw json.RawMessage) (daemon.HerdrSnapshot, error) {
	var source struct {
		Workspaces []struct {
			ID    string `json:"workspace_id"`
			Label string `json:"label"`
		} `json:"workspaces"`
		Tabs []struct {
			ID          string `json:"tab_id"`
			WorkspaceID string `json:"workspace_id"`
			Label       string `json:"label"`
		} `json:"tabs"`
		Panes []struct {
			ID          string `json:"pane_id"`
			TabID       string `json:"tab_id"`
			WorkspaceID string `json:"workspace_id"`
			Label       string `json:"label"`
		} `json:"panes"`
	}
	if err := json.Unmarshal(raw, &source); err != nil {
		return daemon.HerdrSnapshot{}, fmt.Errorf("decode Herdr snapshot: %w", err)
	}
	workspaces := make(map[string]*daemon.Workspace, len(source.Workspaces))
	for _, workspace := range source.Workspaces {
		workspaces[workspace.ID] = &daemon.Workspace{ID: workspace.ID, Label: workspace.Label}
	}
	for _, tab := range source.Tabs {
		if workspace := workspaces[tab.WorkspaceID]; workspace != nil {
			workspace.Tabs = append(workspace.Tabs, daemon.Tab{ID: tab.ID, Label: tab.Label})
		}
	}
	for _, pane := range source.Panes {
		if workspace := workspaces[pane.WorkspaceID]; workspace != nil {
			for i := range workspace.Tabs {
				if workspace.Tabs[i].ID == pane.TabID {
					workspace.Tabs[i].Panes = append(workspace.Tabs[i].Panes, daemon.Pane{ID: pane.ID, Label: pane.Label})
				}
			}
		}
	}
	result := daemon.HerdrSnapshot{}
	for _, workspace := range source.Workspaces {
		if value := workspaces[workspace.ID]; value != nil {
			result.Workspaces = append(result.Workspaces, *value)
		}
	}
	return result, nil
}

// PaneProcesses reads process evidence for every committed pane.
func PaneProcesses(ctx context.Context, panes []daemon.Pane, lookup func(context.Context, string) (herdr.ProcessInfoResponse, error)) ([]daemon.PaneProcess, error) {
	result := make([]daemon.PaneProcess, 0, len(panes))
	for _, pane := range panes {
		info, err := lookup(ctx, pane.ID)
		if err != nil {
			return nil, &PaneUnavailableError{PaneID: pane.ID, Err: err}
		}
		record := daemon.PaneProcess{PaneID: pane.ID, ShellPID: info.ShellPID, PGID: info.ForegroundProcessGroupID}
		for _, process := range info.ForegroundProcesses {
			record.Foreground = append(record.Foreground, daemon.ForegroundProcess{PID: process.PID, Argv: strings.Fields(process.Command), CWD: process.CWD})
		}
		result = append(result, record)
	}
	return result, nil
}

type Factory struct {
	Scanner     discovery.Scanner
	Processes   discovery.ProcessTable
	ProcessInfo func(context.Context, string) (herdr.ProcessInfoResponse, error)
	Snapshot    func(context.Context) (json.RawMessage, error)
	Events      func(context.Context) (<-chan herdr.Event, error)
	Reporter    metadata.Reporter
}

func NewFactory(socket string) Factory {
	transport := herdr.JSONLTransport{Socket: socket}
	return Factory{
		Scanner: discovery.NewSystemScanner(), Processes: discovery.NewSystemProcessTable(),
		ProcessInfo: func(ctx context.Context, pane string) (herdr.ProcessInfoResponse, error) {
			return transport.ProcessInfo(ctx, "herdr-apps-process", pane)
		},
		Snapshot: func(ctx context.Context) (json.RawMessage, error) {
			response, err := transport.Snapshot(ctx, "herdr-apps-snapshot")
			return response.Snapshot, err
		},
		Events: func(ctx context.Context) (<-chan herdr.Event, error) {
			return transport.Subscribe(ctx, "herdr-apps-events", topologySubscriptions())
		},
		Reporter: metadata.HerdrReporter{Transport: transport},
	}
}

func topologySubscriptions() []herdr.Subscription {
	types := []string{
		"workspace.created", "workspace.updated", "workspace.renamed", "workspace.moved", "workspace.reordered", "workspace.closed", "workspace.focused",
		"tab.created", "tab.closed", "tab.focused", "tab.renamed", "tab.moved",
		"pane.created", "pane.closed", "pane.updated", "pane.focused", "pane.moved", "pane.exited",
	}
	subscriptions := make([]herdr.Subscription, len(types))
	for i, eventType := range types {
		subscriptions[i] = herdr.Subscription{Type: eventType}
	}
	return subscriptions
}

func DefaultSocket() string {
	if socket := os.Getenv("HERDR_SOCKET_PATH"); socket != "" {
		return socket
	}
	return filepath.Join(os.Getenv("HOME"), ".config", "herdr", "herdr.sock")
}

// Runtime composes one Service for reconciliation and IPC, with real Herdr and OS adapters.
func (f Factory) Runtime(service *daemon.Service) daemon.Runtime {
	publisher := &metadata.Publisher{}
	return daemon.Runtime{Service: service, Backoff: time.Second, Subscribe: func(ctx context.Context) (<-chan struct{}, error) {
		events, err := f.Events(ctx)
		if err != nil {
			return nil, err
		}
		signals := make(chan struct{}, 1)
		go func() {
			defer close(signals)
			for range events {
				select {
				case signals <- struct{}{}:
				default:
				}
			}
		}()
		return signals, nil
	}, Rebuild: func(ctx context.Context, previous model.Snapshot) (model.Snapshot, error) {
		raw, err := f.Snapshot(ctx)
		if err != nil {
			return model.Snapshot{}, err
		}
		topology, err := Topology(raw)
		if err != nil {
			return model.Snapshot{}, err
		}
		var panes []daemon.Pane
		for _, workspace := range topology.Workspaces {
			for _, tab := range workspace.Tabs {
				panes = append(panes, tab.Panes...)
			}
		}
		records, err := PaneProcesses(ctx, panes, f.ProcessInfo)
		if err != nil {
			return daemon.Reconcile(previous, daemon.ReconcileInput{HerdrErr: err, ObservedAt: time.Now().UTC()})
		}
		listeners, scanErr := f.Scanner.Scan(ctx)
		processes := map[int]discovery.Process{}
		var processErr error
		for _, listener := range listeners {
			if process, err := f.Processes.Lookup(ctx, listener.PID); err != nil {
				processErr = err
				break
			} else {
				processes[listener.PID] = process
			}
		}
		return daemon.Reconcile(previous, daemon.ReconcileInput{Committed: topology, PaneProcesses: records, Listeners: listeners, Processes: processes, ScanErr: scanErr, ProcessErr: processErr, ObservedAt: time.Now().UTC()})
	}, Publish: func(ctx context.Context, apps []model.Application) error {
		return publisher.Publish(ctx, apps, f.Reporter)
	}}
}
