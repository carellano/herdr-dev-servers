package actions

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/carellano/herdr-dev-servers/internal/model"
)

type bridgeSignaler struct {
	mu    sync.Mutex
	calls []signalCall
}

func (s *bridgeSignaler) SignalPID(pid int, signal Signal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, signalCall{PID: pid, Signal: signal})
	return nil
}

func (s *bridgeSignaler) SignalProcessGroup(pgid int, signal Signal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, signalCall{ProcessGroup: pgid, Signal: signal})
	return nil
}

func (s *bridgeSignaler) count(signal Signal) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, call := range s.calls {
		if call.Signal == signal {
			count++
		}
	}
	return count
}

func newBridgeExecutor(waitErr error) (*Executor, *bridgeSignaler, model.Application) {
	identity := testIdentity()
	signaler := &bridgeSignaler{}
	service := Service{Processes: fakeInspector{identity: identity, waitErr: waitErr}, Signaler: signaler, Grace: time.Millisecond}
	return NewExecutor(service), signaler, testApp(identity)
}

func execute(executor *Executor, action string, confirmed bool, app model.Application) model.ActionResult {
	result, err := executor.Execute(context.Background(), model.ActionRequest{Action: action, Confirmed: confirmed}, app)
	if err != nil {
		panic(err)
	}
	return result
}

func TestExecutorForceKillEligibility(t *testing.T) {
	for _, test := range []struct {
		name      string
		prepare   func(*Executor, model.Application)
		confirmed bool
		app       func(model.Application) model.Application
		want      Outcome
		kills     int
	}{
		{name: "confirmed kill without TERM", confirmed: true, app: func(app model.Application) model.Application { return app }, want: OutcomeUnavailable},
		{name: "unconfirmed kill", confirmed: false, app: func(app model.Application) model.Application { return app }, want: OutcomeUnavailable},
		{name: "matching eligible kill", prepare: func(e *Executor, app model.Application) { execute(e, "terminate", true, app) }, confirmed: true, app: func(app model.Application) model.Application { return app }, want: OutcomeKillSent, kills: 1},
		{name: "different app", prepare: func(e *Executor, app model.Application) { execute(e, "terminate", true, app) }, confirmed: true, app: func(app model.Application) model.Application { app.ID = "other"; return app }, want: OutcomeUnavailable},
		{name: "reused identity", prepare: func(e *Executor, app model.Application) { execute(e, "terminate", true, app) }, confirmed: true, app: func(app model.Application) model.Application { app.Identity.StartTime = "new"; return app }, want: OutcomeUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor, signaler, app := newBridgeExecutor(context.DeadlineExceeded)
			if test.prepare != nil {
				test.prepare(executor, app)
			}
			result := execute(executor, "kill", test.confirmed, test.app(app))
			if result.Outcome != string(test.want) || signaler.count(SignalKILL) != test.kills {
				t.Fatalf("result=%#v kills=%d", result, signaler.count(SignalKILL))
			}
		})
	}
}

func TestExecutorConsumesForceEligibility(t *testing.T) {
	executor, signaler, app := newBridgeExecutor(context.DeadlineExceeded)
	if result := execute(executor, "terminate", true, app); !result.ForceEligible {
		t.Fatalf("TERM result=%#v", result)
	}
	if result := execute(executor, "kill", true, app); result.Outcome != string(OutcomeKillSent) {
		t.Fatalf("KILL result=%#v", result)
	}
	if result := execute(executor, "kill", true, app); result.Outcome != string(OutcomeUnavailable) || signaler.count(SignalKILL) != 1 {
		t.Fatalf("replay result=%#v kills=%d", result, signaler.count(SignalKILL))
	}
}

func TestExecutorClearsForceEligibilityAfterGracefulTerm(t *testing.T) {
	executor, _, app := newBridgeExecutor(context.DeadlineExceeded)
	execute(executor, "terminate", true, app)
	executor.Service.Processes = fakeInspector{identity: app.Identity}
	if result := execute(executor, "terminate", true, app); result.ForceEligible {
		t.Fatalf("graceful TERM result=%#v", result)
	}
	if result := execute(executor, "kill", true, app); result.Outcome != string(OutcomeUnavailable) {
		t.Fatalf("KILL result=%#v", result)
	}
}

func TestExecutorConcurrentKillsConsumeEligibilityOnce(t *testing.T) {
	executor, signaler, app := newBridgeExecutor(context.DeadlineExceeded)
	execute(executor, "terminate", true, app)
	var group sync.WaitGroup
	for range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			execute(executor, "kill", true, app)
		}()
	}
	group.Wait()
	if signaler.count(SignalKILL) != 1 {
		t.Fatalf("KILL signals=%d, want one", signaler.count(SignalKILL))
	}
}
