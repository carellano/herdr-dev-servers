package actions

import (
	"context"
	"fmt"

	"github.com/carellano/herdr-dev-servers/internal/correlation"
	"github.com/carellano/herdr-dev-servers/internal/model"
)

// Open validates a local HTTP(S) URL and invokes a fixed desktop opener argv.
func (s Service) Open(ctx context.Context, rawURL string) (Result, error) {
	validated, err := (correlation.URLPolicy{}).Validate(rawURL)
	if err != nil {
		return Result{Outcome: OutcomeUnavailable, Warning: err.Error()}, err
	}
	path, err := openerFor(s.Platform)
	if err != nil {
		return Result{Outcome: OutcomeUnavailable, Warning: err.Error()}, err
	}
	if s.Runner == nil {
		err := fmt.Errorf("desktop opener is unavailable")
		return Result{Outcome: OutcomeUnavailable, Warning: err.Error()}, err
	}
	timeout := s.OpenTimeout
	if timeout <= 0 {
		timeout = defaultOpenTimeout
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := s.Runner.Start(bounded, Command{Path: path, Args: []string{validated}, Detach: true}); err != nil {
		return Result{Outcome: OutcomeUnavailable, Warning: fmt.Sprintf("could not launch %s: %v", path, err)}, err
	}
	return Result{Outcome: OutcomeOpened}, nil
}

// Copy has an explicit no-terminal fallback: clipboard failures never print untrusted text.
func (s Service) Copy(rawURL string) (Result, error) {
	validated, err := (correlation.URLPolicy{}).Validate(rawURL)
	if err != nil {
		return Result{Outcome: OutcomeUnavailable, Warning: err.Error()}, err
	}
	if s.Clipboard == nil {
		return Result{Outcome: OutcomeUnavailable, Warning: "clipboard unavailable; URL was not written to terminal"}, nil
	}
	if err := s.Clipboard.WriteText(validated); err != nil {
		return Result{Outcome: OutcomeUnavailable, Warning: "clipboard unavailable; URL was not written to terminal"}, nil
	}
	return Result{Outcome: OutcomeCopied}, nil
}

// FocusApplication verifies the final observed focus and never upgrades a fallback to exact.
func (s Service) FocusApplication(application model.Application) Result {
	association := application.Association
	if s.Focus == nil || association.Stale || association.WorkspaceID == "" || association.TabID == "" {
		return Result{Outcome: OutcomeUnavailable, Warning: "workspace and tab evidence are unavailable"}
	}
	if association.Confidence == model.ConfidenceHigh && association.PaneID != "" {
		if err := s.Focus.FocusPane(association.WorkspaceID, association.TabID, association.PaneID); err == nil {
			if current, currentErr := s.Focus.CurrentFocus(); currentErr == nil && current == (FocusLocation{WorkspaceID: association.WorkspaceID, TabID: association.TabID, PaneID: association.PaneID}) {
				return Result{Outcome: OutcomeExactPane}
			}
		}
		return Result{Outcome: OutcomeUnavailable, Warning: "pane focus could not be verified"}
	}
	if err := s.Focus.FocusWorkspaceTab(association.WorkspaceID, association.TabID); err != nil {
		return Result{Outcome: OutcomeUnavailable, Warning: "workspace/tab focus failed"}
	}
	current, err := s.Focus.CurrentFocus()
	if err != nil || current.WorkspaceID != association.WorkspaceID || current.TabID != association.TabID {
		return Result{Outcome: OutcomeUnavailable, Warning: "workspace/tab focus could not be verified"}
	}
	return Result{Outcome: OutcomeFallbackWorkspaceTab, Warning: "exact pane evidence is unavailable; focused workspace and tab only"}
}

func openerFor(platform string) (string, error) {
	switch platform {
	case "darwin":
		return "open", nil
	case "linux":
		return "xdg-open", nil
	default:
		return "", fmt.Errorf("desktop open is unsupported on %q", platform)
	}
}
