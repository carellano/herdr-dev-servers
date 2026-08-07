package correlation

import (
	"testing"
	"time"

	"github.com/carellano/herdr-apps/internal/discovery"
	"github.com/carellano/herdr-apps/internal/model"
)

func process(pid, parent int, cwd string) discovery.Process {
	return discovery.Process{PID: pid, ParentPID: parent, StartTime: "start", PGID: pid, Executable: "/usr/bin/node", Args: []string{"node", "server.js", "--port", "3000"}, CWD: cwd}
}

func TestBuildGroupsMultiPortApplicationWithExactPaneEvidence(t *testing.T) {
	result := Build(Input{Listeners: []discovery.Listener{{PID: 42, Port: 3000}, {PID: 42, Port: 5173}}, Processes: map[int]discovery.Process{42: process(42, 1, "/work/app")}, Panes: []PaneEvidence{{PaneID: "p1", TabID: "t1", WorkspaceID: "w1", PID: 42, CWD: "/work/app", ObservedAt: time.Now()}}})
	if len(result.Applications) != 1 {
		t.Fatalf("applications = %d, want 1", len(result.Applications))
	}
	app := result.Applications[0]
	if got, want := app.Association, (model.Association{WorkspaceID: "w1", TabID: "t1", PaneID: "p1", Confidence: model.ConfidenceHigh}); got != want {
		t.Fatalf("association = %#v, want %#v", got, want)
	}
	if len(app.Endpoints) != 2 || app.External {
		t.Fatalf("application = %#v, want grouped non-external endpoints", app)
	}
}

func TestBuildRequiresSegmentBoundaryAndMarksAmbiguousOrUnavailableEvidence(t *testing.T) {
	tests := []struct {
		name      string
		panes     []PaneEvidence
		available string
		want      model.Confidence
		stale     bool
	}{
		{name: "cwd prefix is not a segment match", panes: []PaneEvidence{{PaneID: "wrong", CWD: "/work/application", ObservedAt: time.Now()}}, want: model.ConfidenceUnknown},
		{name: "multiple matching panes are ambiguous", panes: []PaneEvidence{{PaneID: "one", CWD: "/work/app", ObservedAt: time.Now()}, {PaneID: "two", CWD: "/work/app", ObservedAt: time.Now()}}, want: model.ConfidencePartial, stale: true},
		{name: "permission limit remains evidence", available: "process details unavailable: permission denied", want: model.ConfidenceUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Build(Input{Listeners: []discovery.Listener{{PID: 42, Port: 3000}}, Processes: map[int]discovery.Process{42: process(42, 1, "/work/app")}, Panes: tt.panes, ProcessUnavailable: tt.available})
			app := result.All()[0]
			if app.Association.Confidence != tt.want || app.Association.Stale != tt.stale {
				t.Fatalf("association = %#v", app.Association)
			}
			if tt.available != "" && len(app.Evidence) == 0 || tt.available != "" && app.Evidence[len(app.Evidence)-1].Unavailable != tt.available {
				t.Fatalf("evidence = %#v, want unavailable %q", app.Evidence, tt.available)
			}
		})
	}
}

func TestBuildTreatsPIDChurnAsNewIdentity(t *testing.T) {
	processes := map[int]discovery.Process{42: process(42, 1, "/work/app"), 43: process(43, 1, "/work/app")}
	first := Build(Input{Listeners: []discovery.Listener{{PID: 42, Port: 3000}}, Processes: processes})
	second := Build(Input{Listeners: []discovery.Listener{{PID: 43, Port: 3000}}, Processes: processes})
	firstApp, secondApp := first.All()[0], second.All()[0]
	if firstApp.ID == secondApp.ID || firstApp.Identity.PID == secondApp.Identity.PID {
		t.Fatalf("PID churn reused identity: first=%#v second=%#v", firstApp, secondApp)
	}
}

func TestBuildKeepsUnresolvedListenerInExplicitAllView(t *testing.T) {
	result := Build(Input{Listeners: []discovery.Listener{{PID: 99, Port: 8080}}})
	if len(result.Applications) != 0 || len(result.External) != 1 || !result.External[0].External {
		t.Fatalf("result = %#v, want one external unresolved listener", result)
	}
	if result.External[0].Evidence[0].Unavailable == "" {
		t.Fatalf("external listener evidence = %#v, want unavailable reason", result.External[0].Evidence)
	}
}

func TestLogicalIdentityIncludesAncestryRoot(t *testing.T) {
	child := process(42, 10, "/work/app")
	first := LogicalIdentityWithAncestry(child, map[int]discovery.Process{42: child, 10: {PID: 10, Executable: "/bin/sh", CWD: "/work"}})
	second := LogicalIdentityWithAncestry(child, map[int]discovery.Process{42: child, 10: {PID: 10, Executable: "/bin/bash", CWD: "/work"}})
	if first.Key == second.Key {
		t.Fatalf("logical identity omitted ancestry root: first=%#v second=%#v", first, second)
	}
}
