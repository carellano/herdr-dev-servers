package ui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/carellano/herdr-dev-servers/internal/model"
	tea "github.com/charmbracelet/bubbletea"
)

func TestResponsiveViewFiltersExternalApplicationsAndIgnoresA(t *testing.T) {
	snapshot := model.Snapshot{Revision: 2, Applications: []model.Application{{ID: "owned", Endpoints: []model.Endpoint{{Port: 3000}}, Evidence: []model.Evidence{{Argv: []string{"node"}}}, Association: model.Association{WorkspaceLabel: "Project", Confidence: model.ConfidenceHigh}}, {ID: "external", External: true, Evidence: []model.Evidence{{Argv: []string{"nginx"}}}}}}
	m := New(snapshot, func(_ context.Context, key string, _ model.Application, _ uint64, confirmed bool) (model.ActionResult, model.Snapshot, error) {
		if confirmed {
			t.Fatal("non-destructive action was unexpectedly confirmed")
		}
		return model.ActionResult{Outcome: key + " executed"}, snapshot, nil
	})
	for _, width := range []int{70, 90, 130} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		m = next.(Model)
		view := m.View()
		normalizedView := strings.Join(strings.Fields(view), " ")
		if !strings.Contains(view, "node") || !strings.Contains(view, "Keys:") || !strings.Contains(normalizedView, "K force kill (after TERM)") {
			t.Fatalf("width %d did not retain commands and owned app: %q", width, view)
		}
		if width >= 80 && !strings.Contains(view, "Association:") {
			t.Fatalf("width %d did not render detail pane: %q", width, view)
		}
	}
	before := m.View()
	next, command := m.Update(key("a"))
	m = next.(Model)
	if command != nil || m.View() != before || strings.Contains(m.View(), "nginx") {
		t.Fatalf("a changed popup state: command=%v view=%q", command, m.View())
	}
	next, command = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(command())
	m = next.(Model)
	if !strings.Contains(m.View(), "enter executed") {
		t.Fatal("action was not routed")
	}
}

func TestRefreshLifecycleAndSnapshotSelection(t *testing.T) {
	initial := model.Snapshot{Revision: 1, Applications: []model.Application{{ID: "first"}, {ID: "second"}}}
	var calls int
	m := New(initial, nil, WithRefresh(func(ctx context.Context) (model.Snapshot, error) {
		calls++
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("refresh context has no deadline")
		}
		return model.Snapshot{Revision: 2, Applications: []model.Application{{ID: "second"}, {ID: "third"}, {ID: "first"}}}, nil
	}, 1, 1), WithTick(func(time.Duration) tea.Cmd { return func() tea.Msg { return refreshTickMsg{} } }))
	if m.Init() == nil {
		t.Fatal("configured refresh did not schedule its first tick")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	next, refresh := m.Update(refreshTickMsg{})
	m = next.(Model)
	if refresh == nil || !m.refreshing {
		t.Fatal("tick did not start refresh")
	}
	_, overlap := m.Update(refreshTickMsg{})
	if overlap != nil || calls != 0 {
		t.Fatal("overlapping tick started another refresh")
	}
	next, nextTick := m.Update(refresh())
	m = next.(Model)
	if nextTick == nil || calls != 1 || m.snapshot.Revision != 2 || m.apps()[m.cursor].ID != "second" {
		t.Fatalf("refresh result = calls %d revision %d selected %q", calls, m.snapshot.Revision, m.apps()[m.cursor].ID)
	}
	next, _ = m.Update(SnapshotMsg{Snapshot: model.Snapshot{Revision: 1, Applications: []model.Application{{ID: "old"}}}})
	if got := next.(Model); got.snapshot.Revision != 2 || got.apps()[got.cursor].ID != "second" {
		t.Fatal("older snapshot replaced a newer refresh")
	}
	next, _ = m.Update(SnapshotMsg{Snapshot: model.Snapshot{Revision: 3, Applications: []model.Application{{ID: "third"}}}})
	m = next.(Model)
	if m.cursor != 0 || m.apps()[m.cursor].ID != "third" {
		t.Fatal("removed selection did not clamp predictably")
	}
}

func TestRefreshErrorRetainsSnapshot(t *testing.T) {
	m := New(model.Snapshot{Revision: 4, Applications: []model.Application{{ID: "app"}}}, nil,
		WithRefresh(func(context.Context) (model.Snapshot, error) {
			return model.Snapshot{}, errors.New("socket unavailable")
		}, 1, 1),
		WithTick(func(time.Duration) tea.Cmd { return func() tea.Msg { return refreshTickMsg{} } }))
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(Model)
	next, refresh := m.Update(refreshTickMsg{})
	next, nextTick := next.Update(refresh())
	result := next.(Model)
	if nextTick == nil || result.snapshot.Revision != 4 || !strings.Contains(result.View(), "Sync error: socket unavailable") {
		t.Fatalf("refresh error did not remain visible with last snapshot: %q", result.View())
	}
}

func TestRefreshRequiresConfiguration(t *testing.T) {
	if New(model.Snapshot{}, nil).Init() != nil {
		t.Fatal("unconfigured model scheduled refresh")
	}
}

func TestDiscardsObsoleteSnapshot(t *testing.T) {
	m := New(model.Snapshot{Revision: 3, Applications: []model.Application{{ID: "new", Evidence: []model.Evidence{{Argv: []string{"new"}}}}}}, nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 24})
	m = next.(Model)
	next, _ = m.Update(SnapshotMsg{Snapshot: model.Snapshot{Revision: 2, Applications: []model.Application{{ID: "old", Evidence: []model.Evidence{{Argv: []string{"old"}}}}}}})
	if strings.Contains(next.(Model).View(), "old") {
		t.Fatal("obsolete revision replaced current snapshot")
	}
}

func TestSelectionAndUnavailableAction(t *testing.T) {
	snapshot := model.Snapshot{Applications: []model.Application{{ID: "first", Evidence: []model.Evidence{{Argv: []string{"first"}}}}, {ID: "second", Evidence: []model.Evidence{{Argv: []string{"second"}}}}}}
	m := New(snapshot, func(context.Context, string, model.Application, uint64, bool) (model.ActionResult, model.Snapshot, error) {
		return model.ActionResult{}, model.Snapshot{}, errors.New("action unavailable: daemon action bridge is not configured")
	})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 24})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	if !strings.Contains(m.View(), "> second") || !strings.Contains(m.View(), "Association:") {
		t.Fatalf("selection did not update detail: %q", m.View())
	}
	next, command := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	next, _ = next.Update(command())
	if !strings.Contains(next.(Model).View(), "action unavailable: daemon action bridge is not configured") {
		t.Fatal("unavailable action was not explicit")
	}
}

func TestFocusActionQuitsAfterVerifiedFocus(t *testing.T) {
	for _, tt := range []struct {
		name    string
		key     tea.KeyMsg
		outcome string
	}{
		{name: "enter exact pane", key: tea.KeyMsg{Type: tea.KeyEnter}, outcome: "exact-pane"},
		{name: "f workspace tab fallback", key: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")}, outcome: "fallback-workspace-tab"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			updated := model.Snapshot{Revision: 2, Applications: []model.Application{{ID: "updated"}}}
			m := New(model.Snapshot{Revision: 1, Applications: []model.Application{{ID: "app"}}}, func(_ context.Context, key string, _ model.Application, _ uint64, confirmed bool) (model.ActionResult, model.Snapshot, error) {
				if confirmed {
					t.Fatal("focus action was unexpectedly confirmed")
				}
				if key != tt.key.String() {
					t.Fatalf("action key = %q, want %q", key, tt.key.String())
				}
				return model.ActionResult{Outcome: tt.outcome}, updated, nil
			})

			next, command := m.Update(tt.key)
			next, quit := next.Update(command())
			if quit == nil {
				t.Fatal("verified focus did not return a quit command")
			}
			if _, ok := quit().(tea.QuitMsg); !ok {
				t.Fatalf("quit command message = %T, want tea.QuitMsg", quit())
			}
			result := next.(Model)
			if result.status != tt.outcome || result.snapshot.Revision != updated.Revision {
				t.Fatalf("result = status %q revision %d, want status %q revision %d", result.status, result.snapshot.Revision, tt.outcome, updated.Revision)
			}
		})
	}
}

func TestNonVerifiedActionResultsStayOpen(t *testing.T) {
	for _, tt := range []struct {
		name       string
		key        tea.KeyMsg
		result     model.ActionResult
		err        error
		wantStatus string
	}{
		{name: "stale focus error", key: tea.KeyMsg{Type: tea.KeyEnter}, err: errors.New("focus unavailable"), wantStatus: "focus unavailable"},
		{name: "focus unavailable", key: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")}, result: model.ActionResult{Outcome: "unavailable"}, wantStatus: "unavailable"},
		{name: "empty outcome", key: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")}, wantStatus: ""},
		{name: "open warning", key: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")}, result: model.ActionResult{Outcome: "opened", Warning: "browser unavailable"}, wantStatus: "opened: browser unavailable"},
		{name: "copy success", key: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")}, result: model.ActionResult{Outcome: "copied"}, wantStatus: "copied"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := New(model.Snapshot{Applications: []model.Application{{ID: "app"}}}, func(context.Context, string, model.Application, uint64, bool) (model.ActionResult, model.Snapshot, error) {
				return tt.result, model.Snapshot{}, tt.err
			})
			next, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 24})
			m = next.(Model)

			next, command := m.Update(tt.key)
			next, quit := next.Update(command())
			if quit != nil {
				t.Fatal("non-verified action result returned a quit command")
			}
			result := next.(Model)
			if result.status != tt.wantStatus || tt.wantStatus != "" && !strings.Contains(result.View(), "Status: "+tt.wantStatus) {
				t.Fatalf("status = %q, view = %q, want visible status %q", result.status, result.View(), tt.wantStatus)
			}
		})
	}
}

func TestReadableApplicationRendering(t *testing.T) {
	const rawID = "d0f42a6ad1e9604dd9eea4454bc83a4c4db0944d7b9b4b01e5b50d4b7a8fc1d1"
	snapshot := model.Snapshot{Revision: 7, Applications: []model.Application{{
		ID:          rawID,
		Identity:    model.ProcessIdentity{PID: 4242},
		Endpoints:   []model.Endpoint{{URL: "http://127.0.0.1:8081", Port: 8081}},
		Evidence:    []model.Evidence{{Argv: []string{"/usr/bin/Python", "-m", "http.server", "8081"}}},
		Association: model.Association{WorkspaceLabel: "Website", TabID: "tab-42", PaneLabel: "Server", Confidence: model.ConfidenceHigh},
	}}}
	m := New(snapshot, nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	view := next.(Model).View()
	for _, want := range []string{"http.server", "http://127.0.0.1:8081", "Workspace Website", "Tab tab-42", "Pane Server", "High confidence", "PID 4242"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q: %q", want, view)
		}
	}
	if strings.Contains(view, rawID) {
		t.Fatalf("view leaked raw application ID: %q", view)
	}
	if got := strings.Count(view, "http.server"); got != 2 {
		t.Fatalf("display name count = %d, want list and detail once each: %q", got, view)
	}
	if !strings.Contains(view, "Dev Servers (1)") || !strings.Contains(view, "Selected development server") {
		t.Fatalf("wide view did not render stable columns: %q", view)
	}
}

func TestNarrowViewStacksAndBoundsLines(t *testing.T) {
	snapshot := model.Snapshot{Applications: []model.Application{{
		ID:          "opaque-id-that-must-not-render",
		Endpoints:   []model.Endpoint{{URL: "http://127.0.0.1:8081/a-very-long-path-that-needs-truncation"}},
		Evidence:    []model.Evidence{{Argv: []string{"/very/long/path/to/a-very-long-command-name-that-needs-truncation"}}},
		Association: model.Association{WorkspaceID: "workspace-with-a-long-identifier", Confidence: model.ConfidencePartial},
	}}}
	m := New(snapshot, nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 32, Height: 24})
	view := next.(Model).View()
	if strings.Contains(view, snapshot.Applications[0].ID) || !strings.Contains(view, "Selected development server") {
		t.Fatalf("narrow view did not stack readable detail: %q", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if len([]rune(line)) > 32 {
			t.Fatalf("unbounded line (%d columns): %q", len([]rune(line)), line)
		}
	}
}

func TestCommandLegendIsAlwaysVisible(t *testing.T) {
	const expectedLegend = "Keys: enter/f focus | o open | c copy | t TERM (confirm) | K force kill (after TERM) | j/k move | Esc/q quit"
	if keyLegend != expectedLegend || strings.Contains(keyLegend, "a all") || strings.Contains(keyLegend, "x2") {
		t.Fatalf("key legend = %q", keyLegend)
	}
	for _, width := range []int{32, 120} {
		m := New(model.Snapshot{}, nil)
		next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		view := next.(Model).View()
		if strings.Contains(view, "? help") {
			t.Fatalf("width %d retains help dependency: %q", width, view)
		}
		footer := view[strings.LastIndex(view, "\n\n")+2:]
		if strings.Join(strings.Fields(footer), " ") != keyLegend {
			t.Fatalf("width %d footer lost commands: %q", width, footer)
		}
		if width >= len(keyLegend) && footer != keyLegend {
			t.Fatalf("wide footer = %q, want one line %q", footer, keyLegend)
		}
		if width < len(keyLegend) && !strings.Contains(footer, "\n") {
			t.Fatalf("narrow footer did not wrap: %q", footer)
		}
		for _, line := range strings.Split(footer, "\n") {
			if len([]rune(line)) > width {
				t.Fatalf("footer line exceeds width %d: %q", width, line)
			}
		}
	}
}

func TestEscapeQuitsOrCancelsDestructiveConfirmation(t *testing.T) {
	app := destructiveApp("app", "one")
	newModel := func() Model {
		return New(model.Snapshot{Revision: 1, Applications: []model.Application{app}}, func(context.Context, string, model.Application, uint64, bool) (model.ActionResult, model.Snapshot, error) {
			return model.ActionResult{}, model.Snapshot{}, nil
		})
	}
	assertQuit := func(t *testing.T, command tea.Cmd) {
		t.Helper()
		if command == nil {
			t.Fatal("expected quit command")
		}
		if _, ok := command().(tea.QuitMsg); !ok {
			t.Fatalf("quit command message = %T, want tea.QuitMsg", command())
		}
	}

	for _, tt := range []struct {
		name       string
		key        tea.KeyMsg
		pendingKey string
		wantQuit   bool
	}{
		{name: "escape without confirmation", key: tea.KeyMsg{Type: tea.KeyEsc}, wantQuit: true},
		{name: "q without confirmation", key: key("q"), wantQuit: true},
		{name: "q with TERM confirmation", key: key("q"), pendingKey: "t", wantQuit: true},
		{name: "q with KILL confirmation", key: key("q"), pendingKey: "K", wantQuit: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel()
			if tt.pendingKey != "" {
				if tt.pendingKey == "K" {
					target := actionTarget{key: "t", app: app, revision: 1, confirmed: true}
					m.forceEligible = &target
				}
				m, _ = updateModel(m, key(tt.pendingKey))
			}
			_, command := updateModel(m, tt.key)
			if tt.wantQuit {
				assertQuit(t, command)
			}
		})
	}

	for _, pendingKey := range []string{"t", "K"} {
		t.Run("escape cancels "+pendingKey+" confirmation", func(t *testing.T) {
			m := newModel()
			var eligible *actionTarget
			if pendingKey == "K" {
				target := actionTarget{key: "t", app: app, revision: 1, confirmed: true}
				m.forceEligible, eligible = &target, &target
			}
			m, _ = updateModel(m, key(pendingKey))
			m, command := updateModel(m, tea.KeyMsg{Type: tea.KeyEsc})
			if command != nil || m.pending != nil || m.status != "Confirmation canceled." || (pendingKey == "K" && m.forceEligible != eligible) {
				t.Fatalf("first Escape = command %v pending %#v status %q eligible %#v", command, m.pending, m.status, m.forceEligible)
			}
			_, command = updateModel(m, tea.KeyMsg{Type: tea.KeyEsc})
			assertQuit(t, command)
		})
	}
}

type recordedAction struct {
	key       string
	confirmed bool
}

func destructiveApp(id, start string) model.Application {
	return model.Application{ID: id, Identity: model.ProcessIdentity{PID: 42, StartTime: start, PGID: 42, Key: "key-" + start}, Endpoints: []model.Endpoint{{URL: "http://127.0.0.1:3000"}}, Evidence: []model.Evidence{{Argv: []string{"node"}}}}
}

func key(key string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}

func updateModel(m Model, message tea.Msg) (Model, tea.Cmd) {
	next, command := m.Update(message)
	return next.(Model), command
}

func TestTerminateConfirmationRequiresSecondMatchingKey(t *testing.T) {
	app := destructiveApp("app", "one")
	var calls []recordedAction
	m := New(model.Snapshot{Revision: 4, Applications: []model.Application{app}}, func(_ context.Context, key string, _ model.Application, _ uint64, confirmed bool) (model.ActionResult, model.Snapshot, error) {
		calls = append(calls, recordedAction{key: key, confirmed: confirmed})
		return model.ActionResult{Outcome: "term-sent", ForceEligible: true}, model.Snapshot{Revision: 4, Applications: []model.Application{app}}, nil
	})
	m, command := updateModel(m, key("t"))
	if command != nil || len(calls) != 0 || !strings.Contains(m.status, "Confirm TERM") {
		t.Fatalf("first TERM = command %v calls %#v status %q", command, calls, m.status)
	}
	m, command = updateModel(m, key("t"))
	if command == nil {
		t.Fatal("second TERM did not dispatch")
	}
	m, _ = updateModel(m, command())
	if got, want := calls, []recordedAction{{key: "t", confirmed: true}}; !reflect.DeepEqual(got, want) || m.forceEligible == nil {
		t.Fatalf("TERM calls = %#v, force eligible = %#v", got, m.forceEligible)
	}
}

func TestDestructiveConfirmationCancelsOnStateChanges(t *testing.T) {
	first, second := destructiveApp("first", "one"), destructiveApp("second", "two")
	for _, test := range []struct {
		name  string
		apply func(Model) Model
	}{
		{name: "escape", apply: func(m Model) Model { m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEsc}); return m }},
		{name: "move", apply: func(m Model) Model { m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyDown}); return m }},
		{name: "another action", apply: func(m Model) Model { m, _ = updateModel(m, key("o")); return m }},
		{name: "refresh revision", apply: func(m Model) Model {
			m, _ = updateModel(m, SnapshotMsg{Snapshot: model.Snapshot{Revision: 2, Applications: []model.Application{first, second}}})
			return m
		}},
		{name: "refresh result", apply: func(m Model) Model {
			m, _ = updateModel(m, refreshMsg{snapshot: model.Snapshot{Revision: 2, Applications: []model.Application{first, second}}})
			return m
		}},
		{name: "refresh identity", apply: func(m Model) Model {
			changed := first
			changed.Identity.StartTime = "reused"
			m, _ = updateModel(m, SnapshotMsg{Snapshot: model.Snapshot{Revision: 1, Applications: []model.Application{changed, second}}})
			return m
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := New(model.Snapshot{Revision: 1, Applications: []model.Application{first, second}}, func(context.Context, string, model.Application, uint64, bool) (model.ActionResult, model.Snapshot, error) {
				return model.ActionResult{}, model.Snapshot{}, nil
			})
			m, _ = updateModel(m, key("t"))
			m = test.apply(m)
			if m.pending != nil {
				t.Fatalf("pending confirmation survived %s", test.name)
			}
		})
	}
}

func TestKillRequiresEligibleConfirmedTermAndSecondKey(t *testing.T) {
	app := destructiveApp("app", "one")
	var calls []recordedAction
	m := New(model.Snapshot{Revision: 1, Applications: []model.Application{app}}, func(_ context.Context, key string, _ model.Application, _ uint64, confirmed bool) (model.ActionResult, model.Snapshot, error) {
		calls = append(calls, recordedAction{key: key, confirmed: confirmed})
		if key == "t" {
			return model.ActionResult{Outcome: "term-sent", ForceEligible: true}, model.Snapshot{Revision: 1, Applications: []model.Application{app}}, nil
		}
		return model.ActionResult{Outcome: "kill-sent"}, model.Snapshot{Revision: 2}, nil
	})
	m, command := updateModel(m, key("K"))
	if command != nil || len(calls) != 0 || !strings.Contains(m.status, "send confirmed TERM") {
		t.Fatalf("pre-TERM KILL = command %v calls %#v status %q", command, calls, m.status)
	}
	m, _ = updateModel(m, key("t"))
	m, command = updateModel(m, key("t"))
	m, _ = updateModel(m, command())
	m, command = updateModel(m, key("K"))
	if command != nil || m.pending == nil || m.pending.key != "K" {
		t.Fatalf("first KILL = command %v pending %#v", command, m.pending)
	}
	m, command = updateModel(m, key("K"))
	m, _ = updateModel(m, command())
	if got, want := calls, []recordedAction{{key: "t", confirmed: true}, {key: "K", confirmed: true}}; !reflect.DeepEqual(got, want) || m.forceEligible != nil || m.pending != nil {
		t.Fatalf("calls = %#v, force eligible = %#v, pending = %#v", got, m.forceEligible, m.pending)
	}
}

func TestForceEligibilityMatchesTermTargetAndClearsOnError(t *testing.T) {
	app := destructiveApp("app", "one")
	m := New(model.Snapshot{Revision: 1, Applications: []model.Application{app}}, func(context.Context, string, model.Application, uint64, bool) (model.ActionResult, model.Snapshot, error) {
		return model.ActionResult{}, model.Snapshot{}, nil
	})
	target := actionTarget{key: "t", app: app, revision: 1, confirmed: true}
	m, _ = updateModel(m, actionMsg{target: target, result: model.ActionResult{Outcome: "term-sent", ForceEligible: true}, snapshot: model.Snapshot{Revision: 1, Applications: []model.Application{app}}})
	if m.forceEligible == nil {
		t.Fatal("matching confirmed TERM did not enable force KILL")
	}
	m.forceEligible = nil
	m, _ = updateModel(m, actionMsg{target: actionTarget{key: "t", app: app, revision: 1}, result: model.ActionResult{Outcome: "term-sent", ForceEligible: true}, snapshot: model.Snapshot{Revision: 1, Applications: []model.Application{app}}})
	if m.forceEligible != nil {
		t.Fatal("unconfirmed TERM enabled force KILL")
	}
	changed := app
	changed.Identity.PGID = 99
	m, _ = updateModel(m, actionMsg{target: actionTarget{key: "t", app: changed, revision: 1, confirmed: true}, result: model.ActionResult{Outcome: "term-sent", ForceEligible: true}, snapshot: model.Snapshot{Revision: 1, Applications: []model.Application{app}}})
	if m.forceEligible != nil {
		t.Fatal("mismatched TERM target enabled force KILL")
	}
	m, _ = updateModel(m, SnapshotMsg{Snapshot: model.Snapshot{Revision: 2, Applications: []model.Application{changed}}})
	if m.forceEligible != nil {
		t.Fatal("identity change retained force eligibility")
	}
	m.forceEligible = &target
	m, _ = updateModel(m, actionMsg{target: actionTarget{key: "K", app: app, confirmed: true}, err: errors.New("kill unavailable")})
	if m.forceEligible != nil || m.status != "kill unavailable" {
		t.Fatalf("error retained destructive state: %#v %q", m.forceEligible, m.status)
	}
	m.forceEligible = &target
	m, _ = updateModel(m, actionMsg{target: target, result: model.ActionResult{Outcome: "unavailable"}})
	if m.forceEligible != nil {
		t.Fatal("unavailable TERM retained force eligibility")
	}
}

func TestEmptyStateExplainsNoAssociatedApps(t *testing.T) {
	m := New(model.Snapshot{}, nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 70, Height: 24})
	if !strings.Contains(next.(Model).View(), "No Herdr-associated development servers are available.") {
		t.Fatalf("empty state did not explain associated development-server filtering: %q", next.(Model).View())
	}
}
