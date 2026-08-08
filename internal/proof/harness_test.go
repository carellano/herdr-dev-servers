package proof

import (
	"context"
	"errors"
	"testing"

	"github.com/carellano/herdr-apps/internal/model"
)

func controlled() Control {
	return Control{Endpoint: "http://127.0.0.1:38301", PID: 101, WorkspaceID: "w", TabID: "t", PaneID: "p"}
}

func application(c Control) model.Application {
	return model.Application{Identity: model.ProcessIdentity{PID: c.PID}, Endpoints: []model.Endpoint{{URL: c.Endpoint}}, Association: model.Association{WorkspaceID: c.WorkspaceID, TabID: c.TabID, PaneID: c.PaneID, Confidence: model.ConfidenceHigh}}
}

func TestSelectControlled(t *testing.T) {
	c := controlled()
	external := application(c)
	external.External, external.Association.Confidence = true, model.ConfidenceUnknown
	for _, tt := range []struct {
		name string
		apps []model.Application
		want error
	}{
		{"transient null is not ready", nil, ErrNotReady},
		{"selects controlled among multiple", []model.Application{external, application(c)}, nil},
		{"missing evidence fails closed", []model.Application{{}}, ErrEvidence},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SelectControlled(tt.apps, c)
			if !errors.Is(err, tt.want) {
				t.Fatalf("SelectControlled() error = %v, want %v", err, tt.want)
			}
			if err == nil && got.Identity.PID != c.PID {
				t.Fatalf("selected %#v", got)
			}
		})
	}
}

func TestCaptureMetadataAndTransitions(t *testing.T) {
	captures := []MetadataCapture{
		{WorkspaceID: "other", Values: map[string]string{"apps": "wrong"}},
		{WorkspaceID: "w", Values: map[string]string{"apps": "initial"}},
		{WorkspaceID: "w", Values: map[string]string{"apps": "update"}},
		{WorkspaceID: "w", Values: map[string]string{"apps": ""}},
	}
	if err := MatchMetadata(captures, "w", "apps", []string{"initial", "update", ""}); err != nil {
		t.Fatal(err)
	}
	base := model.Snapshot{Applications: []model.Application{{ID: "one"}}}
	changed := model.Snapshot{Applications: []model.Application{{ID: "two"}}}
	removed := model.Snapshot{}
	if err := ValidateTransitions([]model.Snapshot{{Revision: 1, Applications: base.Applications}, {Revision: 2, Applications: changed.Applications}, {Revision: 3}}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTransitions([]model.Snapshot{{Revision: 1, Applications: base.Applications}, {Revision: 2, Applications: base.Applications}, removed}); !errors.Is(err, ErrEvidence) {
		t.Fatalf("unchanged transition error = %v", err)
	}
}

func TestExpectedStateAndCleanup(t *testing.T) {
	c := controlled()
	if err := MatchExpected([]model.Application{application(c)}, []Control{c}, nil); err != nil {
		t.Fatal(err)
	}
	replaced := c
	replaced.PID = 102
	if err := MatchExpected([]model.Application{application(replaced)}, []Control{replaced}, []Control{c}); err != nil {
		t.Fatal(err)
	}
	if err := MatchExpected(nil, nil, []Control{replaced}); err != nil {
		t.Fatal(err)
	}
	var cleanup Cleanup
	if !cleanup.Record("daemon") || cleanup.Record("daemon") || !cleanup.Done("daemon") {
		t.Fatalf("cleanup = %#v", cleanup)
	}
}

func TestLiveRunnerFailsClosedAndValidatesEvidence(t *testing.T) {
	cfg := runnerConfig()
	runner := runnerFunc(func(context.Context, LiveConfig) (LiveEvidence, error) {
		return validEvidence(), nil
	})
	if _, err := RunDarwin(context.Background(), cfg, runner); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []LiveConfig{{}, {Invoke: true, FakeHerdrSocket: cfg.FakeHerdrSocket}, {Invoke: true, FakeHerdrSocket: string(make([]byte, 104)), PluginSocket: cfg.PluginSocket}} {
		if _, err := RunDarwin(context.Background(), bad, runner); !errors.Is(err, ErrLiveSafety) {
			t.Fatalf("RunDarwin(%#v) error = %v", bad, err)
		}
	}
	bad := runnerFunc(func(context.Context, LiveConfig) (LiveEvidence, error) { return LiveEvidence{}, nil })
	if _, err := RunDarwin(context.Background(), cfg, bad); !errors.Is(err, ErrEvidence) {
		t.Fatalf("incomplete evidence error = %v", err)
	}
}

func TestRunDarwinInvocationRequiresEveryOptInGate(t *testing.T) {
	args := []string{"--invoke", "--fake-herdr", "--temp-root=/tmp/proof", "--fake-socket=/tmp/proof/fake.sock", "--plugin-socket=/tmp/proof/plugin.sock", "--endpoint=http://127.0.0.1:38301", "--pid=101", "--workspace=w", "--tab=t", "--pane=p", "--replacement-pid=102", "--parent-pid=100", "--poll-timeout=1s", "--event-timeout=1s", "--metadata-values=initial,update,"}
	runner := runnerFunc(func(context.Context, LiveConfig) (LiveEvidence, error) { return validEvidence(), nil })
	if _, err := RunDarwinInvocation(context.Background(), args, runner); err != nil {
		t.Fatal(err)
	}
	if _, err := RunDarwinInvocation(context.Background(), args[1:], runner); !errors.Is(err, ErrLiveSafety) {
		t.Fatalf("missing invoke error = %v", err)
	}
}

type runnerFunc func(context.Context, LiveConfig) (LiveEvidence, error)

func (f runnerFunc) Run(ctx context.Context, cfg LiveConfig) (LiveEvidence, error) {
	return f(ctx, cfg)
}

func validEvidence() LiveEvidence {
	return LiveEvidence{SocketBytes: map[string]int{"fake": 14, "plugin": 16}, IDs: controlled(), ParentPID: 100, Lsof: "tcp", Polls: 3, States: []model.Snapshot{{Revision: 1, Applications: []model.Application{{ID: "one"}}}, {Revision: 2, Applications: []model.Application{{ID: "two"}}}, {Revision: 3}}, Metadata: []string{"initial", "update", ""}, Events: []string{"replacement", "removal"}, Cleanup: []string{"daemon"}, FinalStatus: "not running"}
}
