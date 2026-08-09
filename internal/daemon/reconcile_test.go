package daemon

import (
	"errors"
	"testing"
	"time"

	"github.com/carellano/herdr-dev-servers/internal/discovery"
	"github.com/carellano/herdr-dev-servers/internal/model"
)

func TestReconcile(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	base := ReconcileInput{ObservedAt: now, Listeners: []discovery.Listener{{PID: 42, Port: 3000, Address: "127.0.0.1"}}, Processes: map[int]discovery.Process{42: {PID: 42, ParentPID: 7, StartTime: "a", PGID: 42, Executable: "node", Args: []string{"node", "server.js"}, CWD: "/work/app"}, 7: {PID: 7, Executable: "zsh", CWD: "/work"}}, Committed: HerdrSnapshot{Workspaces: []Workspace{{ID: "w", Label: "Work", Tabs: []Tab{{ID: "t", Label: "Tab", Panes: []Pane{{ID: "p", Label: "Pane"}}}}}}}, PaneProcesses: []PaneProcess{{PaneID: "p", ShellPID: 9, PGID: 42, Foreground: []ForegroundProcess{{PID: 42, Argv: []string{"node", "server.js"}, CWD: "/work/app"}}}}}
	first, err := Reconcile(model.Snapshot{}, base)
	if err != nil {
		t.Fatal(err)
	}
	app := first.Applications[0]
	if app.Association.WorkspaceLabel != "Work" || app.Association.TabLabel != "Tab" || app.Association.PaneLabel != "Pane" || app.Association.PaneID != "p" {
		t.Fatalf("labels/ids lost: %#v", app.Association)
	}
	if app.Identity.PGID != 42 || len(app.Evidence) < 3 || app.Evidence[len(app.Evidence)-1].ShellPID != 9 || len(app.Evidence[len(app.Evidence)-1].Argv) != 2 || len(app.Evidence[len(app.Evidence)-1].Ancestry) != 2 {
		t.Fatalf("process evidence lost: %#v", app.Evidence)
	}

	t.Run("CWD fallback is segment aware", func(t *testing.T) {
		in := base
		in.PaneProcesses = []PaneProcess{{PaneID: "p", Foreground: []ForegroundProcess{{CWD: "/work/application"}}}}
		got, _ := Reconcile(model.Snapshot{}, in)
		if !got.Applications[0].External {
			t.Fatal("prefix CWD was associated")
		}
	})
	t.Run("ambiguous pane evidence remains partial", func(t *testing.T) {
		in := base
		in.Committed.Workspaces[0].Tabs[0].Panes = append(in.Committed.Workspaces[0].Tabs[0].Panes, Pane{ID: "p2"})
		in.PaneProcesses = append(in.PaneProcesses, PaneProcess{PaneID: "p2", Foreground: []ForegroundProcess{{PID: 42}}})
		got, _ := Reconcile(model.Snapshot{}, in)
		if got.Applications[0].External || got.Applications[0].Association.Confidence != model.ConfidencePartial {
			t.Fatalf("ambiguous = %#v", got.Applications[0])
		}
	})
	t.Run("unmatched listener is external", func(t *testing.T) {
		in := base
		in.Committed = HerdrSnapshot{}
		got, _ := Reconcile(model.Snapshot{}, in)
		if !got.Applications[0].External {
			t.Fatal("unmatched listener was not external")
		}
	})
	t.Run("Herdr foreground parent owns controlled listener", func(t *testing.T) {
		in := base
		in.Listeners = []discovery.Listener{{PID: 666, Port: 38181, Address: "127.0.0.1"}}
		in.Processes = map[int]discovery.Process{
			666: {PID: 666, ParentPID: 42, StartTime: "listener", PGID: 42, Executable: "python", Args: []string{"python", "server.py"}, CWD: "/work/app"},
			42:  {PID: 42, ParentPID: 7, StartTime: "pane", PGID: 42, Executable: "zsh", CWD: "/work/app"},
			7:   {PID: 7, Executable: "login", CWD: "/work"},
		}
		got, err := Reconcile(model.Snapshot{}, in)
		if err != nil {
			t.Fatal(err)
		}
		app := got.Applications[0]
		if app.External || app.Association.Confidence != model.ConfidenceHigh || app.Association.WorkspaceID != "w" || app.Association.TabID != "t" || app.Association.PaneID != "p" {
			t.Fatalf("association = %#v, external = %t", app.Association, app.External)
		}
	})
	t.Run("unrelated shared PGID and CWD remains external", func(t *testing.T) {
		in := base
		in.Listeners = []discovery.Listener{{PID: 666, Port: 38181, Address: "127.0.0.1"}}
		in.Processes = map[int]discovery.Process{666: {PID: 666, ParentPID: 7, StartTime: "listener", PGID: 42, Executable: "python", Args: []string{"python", "server.py"}, CWD: "/work/app"}, 7: {PID: 7, Executable: "login", CWD: "/work"}}
		got, err := Reconcile(model.Snapshot{}, in)
		if err != nil {
			t.Fatal(err)
		}
		app := got.Applications[0]
		if !app.External || app.Association.Confidence != model.ConfidenceUnknown {
			t.Fatalf("association = %#v, external = %t", app.Association, app.External)
		}
	})
	t.Run("ports share launch identity", func(t *testing.T) {
		in := base
		in.Listeners = append(in.Listeners, discovery.Listener{PID: 42, Port: 5173, Address: "127.0.0.1"})
		got, _ := Reconcile(model.Snapshot{}, in)
		if len(got.Applications) != 1 || len(got.Applications[0].Endpoints) != 2 {
			t.Fatalf("apps = %#v", got.Applications)
		}
	})
	t.Run("unchanged data suppresses revisions", func(t *testing.T) {
		s := &Service{}
		one := s.Replace(first)
		two := s.Replace(first)
		if one.Revision != two.Revision {
			t.Fatalf("revisions = %d, %d", one.Revision, two.Revision)
		}
	})
	t.Run("changed PID or port evidence publishes revision", func(t *testing.T) {
		s := &Service{}
		one := s.Replace(first)
		changed := cloneSnapshot(first)
		changed.Applications[0].Identity.PID = 99
		changed.Applications[0].Endpoints[0].Port = 4000
		two := s.Replace(changed)
		if two.Revision != one.Revision+1 {
			t.Fatalf("revision = %d", two.Revision)
		}
	})
	t.Run("scanner failure retains stale baseline", func(t *testing.T) {
		got, err := Reconcile(first, ReconcileInput{ScanErr: errors.New("denied"), ObservedAt: now})
		if err != nil || !got.Applications[0].Association.Stale || got.Applications[0].Evidence[len(got.Applications[0].Evidence)-1].Unavailable == "" {
			t.Fatalf("got=%#v err=%v", got, err)
		}
	})
	t.Run("no baseline returns unavailable", func(t *testing.T) {
		_, err := Reconcile(model.Snapshot{}, ReconcileInput{ScanErr: errors.New("denied")})
		var unavailable *UnavailableError
		if !errors.As(err, &unavailable) {
			t.Fatalf("error = %v", err)
		}
	})
}
