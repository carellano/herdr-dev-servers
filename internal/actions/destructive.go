package actions

import (
	"context"
	"fmt"
	"time"

	"github.com/carellano/herdr-apps/internal/model"
)

// Terminate sends TERM only after confidence, confirmation, and immediate identity revalidation.
func (s Service) Terminate(ctx context.Context, application model.Application, confirmed bool) Result {
	if !confirmed {
		return Result{Outcome: OutcomeUnavailable, Warning: "termination requires confirmation"}
	}
	if err := s.validateSignalTarget(ctx, application); err != nil {
		return Result{Outcome: OutcomeUnavailable, Warning: err.Error()}
	}
	if err := s.Signaler.SignalPGID(application.Identity.PGID, SignalTERM); err != nil {
		return Result{Outcome: OutcomeUnavailable, Warning: fmt.Sprintf("TERM failed: %v", err)}
	}
	grace := s.Grace
	if grace <= 0 {
		grace = 3 * time.Second
	}
	bounded, cancel := context.WithTimeout(ctx, grace)
	defer cancel()
	if err := s.Processes.Wait(bounded, application.Identity); err != nil {
		return Result{Outcome: OutcomeTermSent, Warning: "TERM grace elapsed; force kill requires separate confirmation", ForceEligible: true}
	}
	return Result{Outcome: OutcomeTermSent}
}

// ForceKill is deliberately separate from Terminate and revalidates before its own signal.
func (s Service) ForceKill(ctx context.Context, application model.Application, confirmed bool) Result {
	if !confirmed {
		return Result{Outcome: OutcomeUnavailable, Warning: "force kill requires separate confirmation"}
	}
	if err := s.validateSignalTarget(ctx, application); err != nil {
		return Result{Outcome: OutcomeUnavailable, Warning: err.Error()}
	}
	if err := s.Signaler.SignalPGID(application.Identity.PGID, SignalKILL); err != nil {
		return Result{Outcome: OutcomeUnavailable, Warning: fmt.Sprintf("KILL failed: %v", err)}
	}
	return Result{Outcome: OutcomeKillSent}
}

func (s Service) validateSignalTarget(ctx context.Context, application model.Application) error {
	identity := application.Identity
	if application.External || application.Association.Stale || application.Association.Confidence != model.ConfidenceHigh || identity.PID <= 0 || identity.PGID <= 0 || identity.StartTime == "" || identity.Key == "" {
		return fmt.Errorf("process identity is not high-confidence owned evidence")
	}
	if s.Processes == nil || s.Signaler == nil {
		return fmt.Errorf("process signaling is unavailable")
	}
	current, err := s.Processes.Inspect(ctx, identity.PID)
	if err != nil {
		return fmt.Errorf("process could not be revalidated: %w", err)
	}
	if current != identity {
		return fmt.Errorf("process identity changed since observation")
	}
	return nil
}
