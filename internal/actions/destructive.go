package actions

import (
	"context"
	"fmt"
	"time"

	"github.com/carellano/herdr-dev-servers/internal/model"
)

// Terminate sends TERM only after confidence, confirmation, and immediate identity revalidation.
func (s Service) Terminate(ctx context.Context, application model.Application, confirmed bool) Result {
	if !confirmed {
		return Result{Outcome: OutcomeUnavailable, Warning: "termination requires confirmation"}
	}
	target, err := s.validateSignalTarget(ctx, application)
	if err != nil {
		return Result{Outcome: OutcomeUnavailable, Warning: err.Error()}
	}
	if err := target.signal(s.Signaler, SignalTERM); err != nil {
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
	target, err := s.validateSignalTarget(ctx, application)
	if err != nil {
		return Result{Outcome: OutcomeUnavailable, Warning: err.Error()}
	}
	if err := target.signal(s.Signaler, SignalKILL); err != nil {
		return Result{Outcome: OutcomeUnavailable, Warning: fmt.Sprintf("KILL failed: %v", err)}
	}
	return Result{Outcome: OutcomeKillSent}
}

type signalTarget struct {
	pid  int
	pgid int
}

func (t signalTarget) signal(signaler Signaler, signal Signal) error {
	if t.pid != 0 {
		return signaler.SignalPID(t.pid, signal)
	}
	return signaler.SignalProcessGroup(t.pgid, signal)
}

func (s Service) validateSignalTarget(ctx context.Context, application model.Application) (signalTarget, error) {
	identity := application.Identity
	if application.External || application.Association.Stale || application.Association.Confidence != model.ConfidenceHigh {
		return signalTarget{}, fmt.Errorf("TERM requires an exact, current pane association and verified process ownership")
	}
	if identity.PID <= 0 || identity.PGID <= 0 || identity.StartTime == "" || identity.Key == "" {
		return signalTarget{}, fmt.Errorf("TERM requires complete process-incarnation evidence")
	}
	if s.Processes == nil || s.Signaler == nil {
		return signalTarget{}, fmt.Errorf("process signaling is unavailable")
	}
	current, err := s.Processes.Inspect(ctx, identity.PID)
	if err != nil {
		return signalTarget{}, fmt.Errorf("process could not be revalidated: %w", err)
	}
	if current != identity {
		return signalTarget{}, fmt.Errorf("process identity changed since observation")
	}
	if identity.PID == identity.PGID {
		return signalTarget{pgid: identity.PGID}, nil
	}
	return signalTarget{pid: identity.PID}, nil
}
