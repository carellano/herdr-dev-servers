package actions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/carellano/herdr-dev-servers/internal/model"
)

func TestTerminateRevalidatesOwnedProcessGroupBeforeSignaling(t *testing.T) {
	identity := testIdentity()
	for _, tc := range []struct {
		name       string
		app        model.Application
		inspected  model.ProcessIdentity
		inspectErr error
		confirmed  bool
	}{
		{name: "PID reused", app: testApp(identity), inspected: model.ProcessIdentity{PID: identity.PID, StartTime: "new", PGID: identity.PGID, Key: identity.Key}, confirmed: true},
		{name: "PGID changed", app: testApp(identity), inspected: model.ProcessIdentity{PID: identity.PID, StartTime: identity.StartTime, PGID: 99, Key: identity.Key}, confirmed: true},
		{name: "process vanished", app: testApp(identity), inspectErr: errors.New("not found"), confirmed: true},
		{name: "confirmation absent", app: testApp(identity), inspected: identity},
		{name: "external identity", app: model.Application{Identity: identity, External: true, Association: model.Association{Confidence: model.ConfidenceHigh}}, inspected: identity, confirmed: true},
		{name: "stale identity", app: model.Application{Identity: identity, Association: model.Association{Confidence: model.ConfidenceHigh, Stale: true}}, inspected: identity, confirmed: true},
		{name: "ambiguous identity", app: model.Application{Identity: identity, Association: model.Association{Confidence: model.ConfidencePartial}}, inspected: identity, confirmed: true},
		{name: "listener is not process group leader", app: testApp(model.ProcessIdentity{PID: identity.PID, StartTime: identity.StartTime, PGID: 99, Key: identity.Key}), inspected: identity, confirmed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			signaler := &fakeSignaler{}
			service := Service{Processes: fakeInspector{identity: tc.inspected, err: tc.inspectErr}, Signaler: signaler, Grace: time.Millisecond}
			result := service.Terminate(context.Background(), tc.app, tc.confirmed)
			if result.Outcome == OutcomeTermSent || len(signaler.calls) != 0 {
				t.Fatalf("Terminate() = %#v, signals = %#v; want refusal without signal", result, signaler.calls)
			}
		})
	}
}

func TestTerminateSignalsValidatedProcessGroupAndLeavesForceSeparate(t *testing.T) {
	identity := testIdentity()
	signaler := &fakeSignaler{}
	service := Service{Processes: fakeInspector{identity: identity, waitErr: context.DeadlineExceeded}, Signaler: signaler, Grace: time.Millisecond}
	result := service.Terminate(context.Background(), testApp(identity), true)
	if result.Outcome != OutcomeTermSent || !result.ForceEligible {
		t.Fatalf("Terminate() = %#v, want TERM result with explicit force eligibility", result)
	}
	if got, want := signaler.calls, []signalCall{{PGID: identity.PGID, Signal: SignalTERM}}; !sameCalls(got, want) {
		t.Fatalf("signals = %#v, want %#v", got, want)
	}
}

func TestForceKillNeedsSeparateConfirmationAndRevalidation(t *testing.T) {
	identity := testIdentity()
	t.Run("confirmation absent", func(t *testing.T) {
		signaler := &fakeSignaler{}
		result := (Service{Processes: fakeInspector{identity: identity}, Signaler: signaler}).ForceKill(context.Background(), testApp(identity), false)
		if result.Outcome == OutcomeKillSent || len(signaler.calls) != 0 {
			t.Fatalf("ForceKill() = %#v, signals = %#v", result, signaler.calls)
		}
	})
	t.Run("identity changed after TERM", func(t *testing.T) {
		signaler := &fakeSignaler{}
		inspector := fakeInspector{identity: model.ProcessIdentity{PID: identity.PID, StartTime: "reused", PGID: identity.PGID, Key: identity.Key}}
		result := (Service{Processes: inspector, Signaler: signaler}).ForceKill(context.Background(), testApp(identity), true)
		if result.Outcome == OutcomeKillSent || len(signaler.calls) != 0 {
			t.Fatalf("ForceKill() = %#v, signals = %#v", result, signaler.calls)
		}
	})
}

type fakeInspector struct {
	identity model.ProcessIdentity
	err      error
	waitErr  error
}

func (f fakeInspector) Inspect(context.Context, int) (model.ProcessIdentity, error) {
	return f.identity, f.err
}
func (f fakeInspector) Wait(context.Context, model.ProcessIdentity) error { return f.waitErr }

type signalCall struct {
	PGID   int
	Signal Signal
}

type fakeSignaler struct{ calls []signalCall }

func (f *fakeSignaler) SignalPGID(pgid int, signal Signal) error {
	f.calls = append(f.calls, signalCall{PGID: pgid, Signal: signal})
	return nil
}

func testIdentity() model.ProcessIdentity {
	return model.ProcessIdentity{PID: 42, StartTime: "100", PGID: 42, Key: "owned-key"}
}

func testApp(identity model.ProcessIdentity) model.Application {
	return model.Application{Identity: identity, Association: model.Association{Confidence: model.ConfidenceHigh}}
}

func sameCalls(got, want []signalCall) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
