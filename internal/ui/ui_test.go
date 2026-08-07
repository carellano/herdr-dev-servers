package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/carellano/herdr-apps/internal/model"
	tea "github.com/charmbracelet/bubbletea"
)

func TestResponsiveViewAndTransitions(t *testing.T) {
	snapshot := model.Snapshot{Revision: 2, Applications: []model.Application{{ID: "owned", Endpoints: []model.Endpoint{{Port: 3000}}}, {ID: "external", External: true}}}
	m := New(snapshot, func(_ context.Context, key string, _ model.Application, _ uint64) (model.ActionResult, model.Snapshot, error) {
		return model.ActionResult{Outcome: key + " executed"}, snapshot, nil
	})
	for _, width := range []int{70, 90, 130} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		m = next.(Model)
		view := m.View()
		if !strings.Contains(view, "owned") || !strings.Contains(view, "a all") || !strings.Contains(view, "? help") {
			t.Fatalf("width %d did not retain commands and owned app: %q", width, view)
		}
		if width >= 80 && !strings.Contains(view, "confidence:") {
			t.Fatalf("width %d did not render detail pane: %q", width, view)
		}
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = next.(Model)
	if !strings.Contains(m.View(), "external") {
		t.Fatal("all toggle did not reveal external app")
	}
	next, command := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(command())
	m = next.(Model)
	if !strings.Contains(m.View(), "enter executed") {
		t.Fatal("action was not routed")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = next.(Model)
	if !strings.Contains(m.View(), "K force KILL") {
		t.Fatal("help did not render action commands")
	}
}

func TestDiscardsObsoleteSnapshot(t *testing.T) {
	m := New(model.Snapshot{Revision: 3, Applications: []model.Application{{ID: "new"}}}, nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 24})
	m = next.(Model)
	next, _ = m.Update(SnapshotMsg{Snapshot: model.Snapshot{Revision: 2, Applications: []model.Application{{ID: "old"}}}})
	if strings.Contains(next.(Model).View(), "old") {
		t.Fatal("obsolete revision replaced current snapshot")
	}
}

func TestSelectionAndUnavailableAction(t *testing.T) {
	snapshot := model.Snapshot{Applications: []model.Application{{ID: "first"}, {ID: "second"}}}
	m := New(snapshot, func(context.Context, string, model.Application, uint64) (model.ActionResult, model.Snapshot, error) {
		return model.ActionResult{}, model.Snapshot{}, errors.New("action unavailable: daemon action bridge is not configured")
	})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 24})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	if !strings.Contains(m.View(), "> second") || !strings.Contains(m.View(), "confidence:") {
		t.Fatalf("selection did not update detail: %q", m.View())
	}
	next, command := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	next, _ = next.Update(command())
	if !strings.Contains(next.(Model).View(), "action unavailable: daemon action bridge is not configured") {
		t.Fatal("unavailable action was not explicit")
	}
}
