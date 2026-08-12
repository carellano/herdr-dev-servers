// Package ui presents daemon-owned development-server snapshots without rescanning.
package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/carellano/herdr-dev-servers/internal/model"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type SnapshotMsg struct{ Snapshot model.Snapshot }
type Action func(context.Context, string, model.Application, uint64, bool) (model.ActionResult, model.Snapshot, error)
type Refresh func(context.Context) (model.Snapshot, error)

const keyLegend = "Keys: enter/f focus | o open | c copy | t TERM (confirm) | K force kill (after TERM) | j/k move | Esc/q quit"

type actionMsg struct {
	target   actionTarget
	result   model.ActionResult
	snapshot model.Snapshot
	err      error
}
type refreshTickMsg struct{}
type refreshMsg struct {
	snapshot model.Snapshot
	err      error
}

type actionTarget struct {
	key       string
	app       model.Application
	revision  uint64
	confirmed bool
}

type Option func(*Model)

// WithRefresh enables bounded, non-overlapping snapshot refreshes.
func WithRefresh(refresh Refresh, interval, timeout time.Duration) Option {
	return func(m *Model) {
		if refresh != nil && interval > 0 && timeout > 0 {
			m.refresh, m.refreshInterval, m.refreshTimeout = refresh, interval, timeout
		}
	}
}

// WithTick replaces the refresh scheduler, primarily for deterministic tests.
func WithTick(tick func(time.Duration) tea.Cmd) Option {
	return func(m *Model) { m.tick = tick }
}

type Model struct {
	snapshot              model.Snapshot
	cursor, width, height int
	action                Action
	refresh               Refresh
	refreshInterval       time.Duration
	refreshTimeout        time.Duration
	tick                  func(time.Duration) tea.Cmd
	refreshing            bool
	status, syncStatus    string
	pending               *actionTarget
	forceEligible         *actionTarget
}

func New(snapshot model.Snapshot, action Action, options ...Option) Model {
	m := Model{snapshot: snapshot, action: action}
	for _, option := range options {
		option(&m)
	}
	return m
}
func (m Model) Init() tea.Cmd { return m.nextRefresh() }
func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case SnapshotMsg:
		if msg.Snapshot.Revision >= m.snapshot.Revision {
			m.replaceSnapshot(msg.Snapshot)
			m.invalidateDestructiveState()
		}
	case refreshTickMsg:
		if m.refresh != nil && !m.refreshing {
			m.refreshing = true
			return m, m.refreshSnapshot()
		}
	case refreshMsg:
		m.refreshing = false
		if msg.err != nil {
			m.syncStatus = "Sync error: " + msg.err.Error()
		} else if msg.snapshot.Revision >= m.snapshot.Revision {
			m.replaceSnapshot(msg.snapshot)
			m.invalidateDestructiveState()
			m.syncStatus = ""
		}
		return m, m.nextRefresh()
	case actionMsg:
		if msg.err != nil {
			m.status = msg.err.Error()
			if msg.target.key == "t" || msg.target.key == "K" {
				m.forceEligible = nil
			}
		} else {
			m.status = msg.result.Outcome
			if msg.result.Warning != "" {
				m.status += ": " + msg.result.Warning
			}
			if msg.snapshot.Revision >= m.snapshot.Revision {
				m.replaceSnapshot(msg.snapshot)
			}
			m.recordForceEligibility(msg)
			if msg.result.Outcome == "exact-pane" || msg.result.Outcome == "fallback-workspace-tab" {
				return m, tea.Quit
			}
		}
	case tea.KeyMsg:
		if msg.Type == tea.KeyEsc {
			if m.pending != nil {
				m.cancelPending("Confirmation canceled.")
				return m, nil
			}
			return m, tea.Quit
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			if m.cursor < len(m.apps())-1 {
				m.cancelPending("Confirmation canceled: selection changed.")
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cancelPending("Confirmation canceled: selection changed.")
				m.cursor--
			}
		case "t":
			return m.handleTerminate()
		case "K":
			return m.handleKill()
		case "enter", "o", "c", "f":
			m.cancelPending("Confirmation canceled: another action was selected.")
			return m.dispatch(msg.String(), false)
		}
	}
	return m, nil
}

func (m Model) handleTerminate() (tea.Model, tea.Cmd) {
	target, ok := m.selectedTarget("t", false)
	if !ok {
		return m, nil
	}
	if m.pending != nil && sameTarget(*m.pending, target) {
		m.pending = nil
		return m.dispatch("t", true)
	}
	m.pending = &target
	m.status = "Confirm TERM for " + appName(target.app) + " " + endpointSummary(target.app) + ": press t again; Esc cancels"
	return m, nil
}

func (m Model) handleKill() (tea.Model, tea.Cmd) {
	target, ok := m.selectedTarget("K", false)
	if !ok {
		return m, nil
	}
	if m.forceEligible == nil || !sameApp(*m.forceEligible, target) {
		m.cancelPending("")
		m.status = "Force KILL unavailable: send confirmed TERM first and wait for grace."
		return m, nil
	}
	if m.pending != nil && sameTarget(*m.pending, target) {
		m.pending = nil
		return m.dispatch("K", true)
	}
	m.pending = &target
	m.status = "Confirm KILL for " + appName(target.app) + " " + endpointSummary(target.app) + ": press K again; Esc cancels"
	return m, nil
}

func (m Model) dispatch(key string, confirmed bool) (tea.Model, tea.Cmd) {
	target, ok := m.selectedTarget(key, confirmed)
	if !ok {
		return m, nil
	}
	return m, func() tea.Msg {
		result, snapshot, err := m.action(context.Background(), key, target.app, target.revision, confirmed)
		return actionMsg{target: target, result: result, snapshot: snapshot, err: err}
	}
}

func (m Model) selectedTarget(key string, confirmed bool) (actionTarget, bool) {
	apps := m.apps()
	if len(apps) == 0 || m.cursor >= len(apps) || m.action == nil {
		return actionTarget{}, false
	}
	return actionTarget{key: key, app: apps[m.cursor], revision: m.snapshot.Revision, confirmed: confirmed}, true
}

func (m *Model) cancelPending(status string) {
	if m.pending != nil {
		m.pending = nil
		if status != "" {
			m.status = status
		}
	}
}

func (m *Model) invalidateDestructiveState() {
	if m.pending != nil && (m.pending.revision != m.snapshot.Revision || !m.contains(*m.pending)) {
		m.cancelPending("Confirmation canceled: application changed.")
	}
	if m.forceEligible != nil && !m.contains(*m.forceEligible) {
		m.forceEligible = nil
	}
}

func (m *Model) recordForceEligibility(msg actionMsg) {
	if msg.target.key == "K" {
		m.forceEligible = nil
		return
	}
	if msg.target.key == "t" {
		m.forceEligible = nil
		if msg.target.confirmed && msg.result.ForceEligible && m.contains(msg.target) {
			m.forceEligible = &msg.target
		}
	}
}

func (m Model) contains(target actionTarget) bool {
	for _, app := range m.snapshot.Applications {
		if sameApp(actionTarget{app: app}, target) {
			return true
		}
	}
	return false
}

func sameApp(left, right actionTarget) bool {
	return left.app.ID == right.app.ID && left.app.Identity == right.app.Identity
}

func sameTarget(left, right actionTarget) bool {
	return left.key == right.key && left.revision == right.revision && left.confirmed == right.confirmed && sameApp(left, right)
}
func (m Model) nextRefresh() tea.Cmd {
	if m.refresh == nil || m.refreshInterval <= 0 {
		return nil
	}
	if m.tick != nil {
		return m.tick(m.refreshInterval)
	}
	return tea.Tick(m.refreshInterval, func(time.Time) tea.Msg { return refreshTickMsg{} })
}

func (m Model) refreshSnapshot() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), m.refreshTimeout)
		defer cancel()
		snapshot, err := m.refresh(ctx)
		return refreshMsg{snapshot: snapshot, err: err}
	}
}

func (m *Model) replaceSnapshot(snapshot model.Snapshot) {
	selectedID := ""
	if apps := m.apps(); m.cursor < len(apps) {
		selectedID = apps[m.cursor].ID
	}
	m.snapshot = snapshot
	apps := m.apps()
	for i, app := range apps {
		if app.ID == selectedID {
			m.cursor = i
			return
		}
	}
	m.cursor = min(m.cursor, max(0, len(apps)-1))
}
func (m Model) apps() []model.Application {
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
		return "Loading Herdr Dev Servers..."
	}
	width := max(1, m.width)
	apps := m.apps()
	title := truncate("Herdr Dev Servers | development servers", width)
	title = lipgloss.NewStyle().Bold(true).Render(title)

	listWidth, detailWidth := width, width
	wide := width >= 80
	if wide {
		listWidth = min(46, max(30, width*2/5))
		detailWidth = width - listWidth - 3
	}
	list := m.renderList(apps, listWidth)
	detail := m.renderDetail(apps, detailWidth)
	content := list
	if wide {
		content = lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(listWidth).Render(list),
			"   ",
			lipgloss.NewStyle().Width(detailWidth).Render(detail),
		)
	} else if len(apps) > 0 {
		content += "\n\n" + detail
	}

	parts := []string{title, content}
	if m.status != "" {
		parts = append(parts, wrapText("Status: "+m.status, width))
	}
	if m.syncStatus != "" {
		parts = append(parts, wrapText(m.syncStatus, width))
	}
	parts = append(parts, wrapText(keyLegend, width))
	return strings.Join(parts, "\n\n")
}

func (m Model) renderList(apps []model.Application, width int) string {
	lines := []string{truncate(fmt.Sprintf("Dev Servers (%d)", len(apps)), width)}
	if len(apps) == 0 {
		return strings.Join(append(lines, "", truncate("No Herdr-associated development servers are available.", width)), "\n")
	}
	selected := lipgloss.NewStyle().Bold(true).Reverse(true)
	for i, app := range apps {
		name := truncate(appName(app), max(1, width-2))
		row := truncate("  "+name, width)
		endpoint := truncate("  "+endpointSummary(app), width)
		if i == m.cursor {
			row = "> " + name
			row = selected.Width(width).Render(truncate(row, width))
			endpoint = selected.Width(width).Render(endpoint)
		}
		lines = append(lines, row, endpoint)
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderDetail(apps []model.Application, width int) string {
	if len(apps) == 0 {
		return ""
	}
	app := apps[m.cursor]
	association := app.Association
	lines := []string{
		truncate("Selected development server", width),
		"",
		truncate(appName(app), width),
		truncate("Endpoints: "+endpointSummary(app), width),
		truncate("Location: "+locationSummary(association), width),
		truncate("Association: "+confidenceSummary(association.Confidence, association.Stale)+" (navigation)", width),
		truncate(signalSafetySummary(app), width),
	}
	if app.Identity.PID > 0 {
		lines = append(lines, truncate(fmt.Sprintf("Process: PID %d", app.Identity.PID), width))
	}
	return strings.Join(lines, "\n")
}

func appName(app model.Application) string {
	for _, evidence := range app.Evidence {
		for i, arg := range evidence.Argv {
			if arg == "-m" && i+1 < len(evidence.Argv) && evidence.Argv[i+1] != "" {
				return evidence.Argv[i+1]
			}
		}
		if len(evidence.Argv) > 0 && evidence.Argv[0] != "" {
			return filepath.Base(evidence.Argv[0])
		}
	}
	if endpoints := endpointSummary(app); endpoints != "No listener information" {
		return endpoints
	}
	return "Application"
}

func endpointSummary(app model.Application) string {
	endpoints := make([]string, 0, len(app.Endpoints))
	for _, endpoint := range app.Endpoints {
		if endpoint.URL != "" {
			endpoints = append(endpoints, endpoint.URL)
		} else if endpoint.Port > 0 {
			endpoints = append(endpoints, fmt.Sprintf(":%d", endpoint.Port))
		}
	}
	if len(endpoints) == 0 {
		return "No listener information"
	}
	return strings.Join(endpoints, ", ")
}

func locationSummary(association model.Association) string {
	parts := []string{
		"Workspace " + labelOrID(association.WorkspaceLabel, association.WorkspaceID, "unknown"),
		"Tab " + labelOrID(association.TabLabel, association.TabID, "unknown"),
		"Pane " + labelOrID(association.PaneLabel, association.PaneID, "unknown"),
	}
	return strings.Join(parts, " | ")
}

func labelOrID(label, id, fallback string) string {
	if label != "" {
		return label
	}
	if id != "" {
		return id
	}
	return fallback
}

func confidenceSummary(confidence model.Confidence, stale bool) string {
	value := map[model.Confidence]string{
		model.ConfidenceHigh:    "High confidence",
		model.ConfidencePartial: "Partial confidence",
		model.ConfidenceUnknown: "Unknown confidence",
	}[confidence]
	if value == "" {
		value = "Unknown confidence"
	}
	if stale {
		return value + " (stale)"
	}
	return value
}

func signalSafetySummary(app model.Application) string {
	identity := app.Identity
	switch {
	case app.External || app.Association.Stale || app.Association.Confidence != model.ConfidenceHigh:
		return "TERM/KILL: unavailable; exact current association is required"
	case identity.PID <= 0 || identity.PGID <= 0 || identity.StartTime == "" || identity.Key == "":
		return "TERM/KILL: unavailable; process-incarnation evidence is incomplete"
	case identity.PID != identity.PGID:
		return "TERM/KILL: listener PID only; require final process revalidation"
	default:
		return "TERM/KILL: require final process revalidation"
	}
}

func wrapText(text string, width int) string {
	var lines []string
	var line string
	for _, word := range strings.Fields(text) {
		if lipgloss.Width(word) > width {
			if line != "" {
				lines, line = append(lines, line), ""
			}
			lines = append(lines, truncate(word, width))
			continue
		}
		if line == "" {
			line = word
		} else if lipgloss.Width(line)+1+lipgloss.Width(word) <= width {
			line += " " + word
		} else {
			lines, line = append(lines, line), word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func truncate(value string, width int) string {
	if width <= 0 || lipgloss.Width(value) <= width {
		return value
	}
	if width <= 3 {
		return strings.Repeat(".", width)
	}
	var prefix strings.Builder
	used := 0
	for _, r := range value {
		runeWidth := lipgloss.Width(string(r))
		if used+runeWidth > width-3 {
			break
		}
		prefix.WriteRune(r)
		used += runeWidth
	}
	return prefix.String() + "..."
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
