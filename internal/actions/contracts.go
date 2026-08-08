// Package actions executes evidence-backed application actions without shells.
package actions

import (
	"context"
	"time"

	"github.com/carellano/herdr-apps/internal/model"
)

const defaultOpenTimeout = 2 * time.Second

// Command is a fixed executable invocation. Args are passed directly, never through a shell.
type Command struct {
	Path   string
	Args   []string
	Detach bool
}

// Runner starts a fixed command under the provided bounded context.
type Runner interface {
	Start(context.Context, Command) error
}

// Clipboard writes plain text to a platform clipboard implementation.
type Clipboard interface {
	WriteText(string) error
}

// FocusLocation is the observed Herdr location after a focus request.
type FocusLocation struct {
	WorkspaceID string
	TabID       string
	PaneID      string
}

// FocusClient is a narrow, fakeable Herdr focus boundary.
type FocusClient interface {
	FocusPane(workspaceID, tabID, paneID string) error
	FocusWorkspaceTab(workspaceID, tabID string) error
	CurrentFocus() (FocusLocation, error)
}

// ProcessInspector re-reads the process incarnation immediately before a signal.
type ProcessInspector interface {
	Inspect(context.Context, int) (model.ProcessIdentity, error)
	Wait(context.Context, model.ProcessIdentity) error
}

// Signaler targets process groups, never an inferred child PID.
type Signaler interface {
	SignalPGID(pgid int, signal Signal) error
}

// Signal is intentionally narrow so tests never signal host processes.
type Signal int

// Outcome makes exact, partial, unavailable, and destructive action results explicit.
type Outcome string

const (
	OutcomeOpened               Outcome = "opened"
	OutcomeCopied               Outcome = "copied"
	OutcomeExactPane            Outcome = "exact-pane"
	OutcomeFallbackWorkspaceTab Outcome = "fallback-workspace-tab"
	OutcomeTermSent             Outcome = "term-sent"
	OutcomeKillSent             Outcome = "kill-sent"
	OutcomeUnavailable          Outcome = "unavailable"
)

// Result reports the real action outcome; warnings are required for partial results.
type Result struct {
	Outcome        Outcome
	Warning        string
	TerminalOutput bool
	ForceEligible  bool
}

// Service owns only action boundaries; it never rescans or invents evidence.
type Service struct {
	Runner      Runner
	Clipboard   Clipboard
	Focus       FocusClient
	Processes   ProcessInspector
	Signaler    Signaler
	Platform    string
	OpenTimeout time.Duration
	Grace       time.Duration
}
