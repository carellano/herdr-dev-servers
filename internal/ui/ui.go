// Package ui presents daemon-owned application snapshots without rescanning.
package ui

import (
	"fmt"
	"strings"

	"github.com/carellano/herdr-apps/internal/model"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type SnapshotMsg struct{ Snapshot model.Snapshot }
type Action func(string, model.Application) string

type Model struct {
	snapshot              model.Snapshot
	all, help             bool
	cursor, width, height int
	action                Action
	status                string
}

func New(snapshot model.Snapshot, action Action) Model {
	return Model{snapshot: snapshot, action: action}
}
func (m Model) Init() tea.Cmd { return nil }
func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case SnapshotMsg:
		if msg.Snapshot.Revision >= m.snapshot.Revision {
			m.snapshot = msg.Snapshot
			if m.cursor >= len(m.apps()) {
				m.cursor = max(0, len(m.apps())-1)
			}
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			if m.cursor < len(m.apps())-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "a":
			m.all = !m.all
			m.cursor = 0
		case "?":
			m.help = !m.help
		case "enter", "o", "c", "f", "t", "K":
			if apps := m.apps(); len(apps) > 0 && m.action != nil {
				m.status = m.action(msg.String(), apps[m.cursor])
			}
		}
	}
	return m, nil
}
func (m Model) apps() []model.Application {
	if m.all {
		return m.snapshot.Applications
	}
	var visible []model.Application
	for _, app := range m.snapshot.Applications {
		if !app.External {
			visible = append(visible, app)
		}
	}
	return visible
}
func (m Model) View() string {
	if m.width == 0 {
		return "Loading Herdr Apps…"
	}
	apps := m.apps()
	title := lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("Herdr Apps · rev %d · %s", m.snapshot.Revision, map[bool]string{true: "all", false: "Herdr only"}[m.all]))
	rows := make([]string, 0, len(apps))
	for i, app := range apps {
		mark := " "
		if i == m.cursor {
			mark = ">"
		}
		ports := make([]string, 0, len(app.Endpoints))
		for _, endpoint := range app.Endpoints {
			ports = append(ports, fmt.Sprint(endpoint.Port))
		}
		rows = append(rows, fmt.Sprintf("%s %s  :%s", mark, app.ID, strings.Join(ports, ",")))
	}
	if len(rows) == 0 {
		rows = []string{"No matching applications."}
	}
	list := strings.Join(rows, "\n")
	detail := ""
	if len(apps) > 0 {
		app := apps[m.cursor]
		detail = fmt.Sprintf("%s\nconfidence: %s\nworkspace: %s\nkeys: enter focus · o open · c copy · t TERM · K KILL", app.ID, app.Association.Confidence, app.Association.WorkspaceID)
	}
	content := list
	if m.width >= 80 {
		content = lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(max(28, m.width/2)).Render(list), lipgloss.NewStyle().Width(max(24, m.width/2-2)).Render(detail))
	}
	footer := "j/k move · a all · ? help · q quit"
	if m.width < 80 {
		footer = "j/k · a all · ? help · q"
	}
	if m.help {
		footer = "enter focus; o open; c copy; f focus; t TERM; K force KILL; a all; q quit"
	}
	return title + "\n\n" + content + "\n\n" + m.status + "\n" + footer
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
