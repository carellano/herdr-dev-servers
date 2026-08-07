package daemon

import (
	"fmt"
	"time"

	"github.com/carellano/herdr-apps/internal/correlation"
	"github.com/carellano/herdr-apps/internal/discovery"
	"github.com/carellano/herdr-apps/internal/model"
)

// HerdrSnapshot is the committed, minimal session topology needed for correlation.
type HerdrSnapshot struct{ Workspaces []Workspace }
type Workspace struct {
	ID, Label string
	Tabs      []Tab
}
type Tab struct {
	ID, Label string
	Panes     []Pane
}
type Pane struct{ ID, Label string }
type ForegroundProcess struct {
	PID  int
	Argv []string
	CWD  string
}
type PaneProcess struct {
	PaneID         string
	ShellPID, PGID int
	Foreground     []ForegroundProcess
}

// ReconcileInput contains completed observations; callers own scanning and Herdr I/O.
type ReconcileInput struct {
	Committed     HerdrSnapshot
	PaneProcesses []PaneProcess
	Listeners     []discovery.Listener
	Processes     map[int]discovery.Process
	ScanErr       error
	ProcessErr    error
	HerdrErr      error
	ObservedAt    time.Time
}

// UnavailableError means no complete state exists to serve after an input failure.
type UnavailableError struct {
	Operation string
	Err       error
}

func (e *UnavailableError) Error() string {
	return fmt.Sprintf("%s unavailable: %v", e.Operation, e.Err)
}
func (e *UnavailableError) Unwrap() error { return e.Err }

// Reconcile converts one committed Herdr snapshot and discovery pass into a complete semantic snapshot.
func Reconcile(previous model.Snapshot, in ReconcileInput) (model.Snapshot, error) {
	if operation, err := unavailable(in); err != nil {
		if len(previous.Applications) == 0 {
			return model.Snapshot{}, &UnavailableError{operation, err}
		}
		stale := cloneSnapshot(previous)
		stale.ObservedAt = in.ObservedAt
		for i := range stale.Applications {
			stale.Applications[i].Association.Stale = true
			stale.Applications[i].Evidence = append(stale.Applications[i].Evidence, model.Evidence{Source: operation, ObservedAt: in.ObservedAt, Unavailable: err.Error()})
		}
		return stale, nil
	}
	panes := flatten(in.Committed, in.PaneProcesses)
	input := correlation.Input{Listeners: in.Listeners, Processes: in.Processes, Panes: panes, ObservedAt: in.ObservedAt}
	result := correlation.Build(input).All()
	for i := range result {
		enrich(&result[i], in.Processes, in.PaneProcesses)
	}
	return model.Snapshot{Applications: result, ObservedAt: in.ObservedAt}, nil
}

func unavailable(in ReconcileInput) (string, error) {
	if in.ScanErr != nil {
		return "scanner", in.ScanErr
	}
	if in.ProcessErr != nil {
		return "process", in.ProcessErr
	}
	if in.HerdrErr != nil {
		return "herdr", in.HerdrErr
	}
	return "", nil
}

func flatten(snapshot HerdrSnapshot, records []PaneProcess) []correlation.PaneEvidence {
	byID := map[string]PaneProcess{}
	for _, record := range records {
		byID[record.PaneID] = record
	}
	var panes []correlation.PaneEvidence
	for _, workspace := range snapshot.Workspaces {
		for _, tab := range workspace.Tabs {
			for _, pane := range tab.Panes {
				record := byID[pane.ID]
				for _, foreground := range record.Foreground {
					panes = append(panes, correlation.PaneEvidence{WorkspaceID: workspace.ID, WorkspaceLabel: workspace.Label, TabID: tab.ID, TabLabel: tab.Label, PaneID: pane.ID, PaneLabel: pane.Label, PID: foreground.PID, CWD: foreground.CWD})
				}
			}
		}
	}
	return panes
}

func enrich(app *model.Application, processes map[int]discovery.Process, records []PaneProcess) {
	var record *PaneProcess
	for i := range records {
		if records[i].PaneID == app.Association.PaneID {
			record = &records[i]
			break
		}
	}
	if record == nil {
		return
	}
	process := processes[app.Identity.PID]
	evidence := model.Evidence{Source: "herdr-process", Fresh: true, ShellPID: record.ShellPID, PGID: record.PGID, Ancestry: ancestry(process, processes)}
	for _, foreground := range record.Foreground {
		if foreground.PID == app.Identity.PID {
			evidence.Argv, evidence.CWD = foreground.Argv, foreground.CWD
			break
		}
	}
	app.Evidence = append(app.Evidence, evidence)
}

func ancestry(process discovery.Process, processes map[int]discovery.Process) []int {
	var result []int
	seen := map[int]bool{}
	for process.PID != 0 && !seen[process.PID] {
		result, seen[process.PID] = append(result, process.PID), true
		process = processes[process.ParentPID]
	}
	return result
}
