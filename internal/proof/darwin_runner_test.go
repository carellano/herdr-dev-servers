package proof

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/carellano/herdr-apps/internal/model"
)

func TestDurableDarwinRunner(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*fakeDarwin)
		want   error
	}{
		{"full sequence", func(*fakeDarwin) {}, nil},
		{"nil readiness times out", func(f *fakeDarwin) { f.polls = []model.Snapshot{{}} }, context.DeadlineExceeded},
		{"missing controlled app", func(f *fakeDarwin) { f.polls = []model.Snapshot{{Applications: []model.Application{{ID: "other"}}}} }, ErrEvidence},
		{"metadata mismatch", func(f *fakeDarwin) { f.metadata[1].Values["ports"] = "wrong" }, ErrEvidence},
		{"non increasing revisions", func(f *fakeDarwin) { f.events[0].Revision = 1 }, ErrEvidence},
		{"event failure", func(f *fakeDarwin) { f.eventErr = errors.New("event failed") }, fakesError("event failed")},
		{"cleanup failure", func(f *fakeDarwin) { f.cleanupErr = errors.New("cleanup failed") }, fakesError("cleanup failed")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeDarwin()
			tt.mutate(fake)
			_, err := RunDarwin(context.Background(), runnerConfig(), DurableDarwinRunner{Ops: fake})
			if tt.want == nil && err != nil || tt.want != nil && !errors.Is(err, tt.want) && (err == nil || err.Error() != tt.want.Error()) {
				t.Fatalf("RunDarwin() error = %v, want %v", err, tt.want)
			}
			if !fake.cleaned {
				t.Fatal("cleanup was not attempted")
			}
		})
	}
}

type fakesError string

func (e fakesError) Error() string        { return string(e) }
func (e fakesError) Is(target error) bool { return target != nil && e.Error() == target.Error() }

func runnerConfig() LiveConfig {
	c := controlled()
	return LiveConfig{Invoke: true, FakeHerdr: true, TempRoot: "/tmp/proof", FakeHerdrSocket: "/tmp/proof/fake.sock", PluginSocket: "/tmp/proof/plugin.sock", Control: c, Replacement: Control{Endpoint: c.Endpoint, PID: 102, WorkspaceID: c.WorkspaceID, TabID: c.TabID, PaneID: c.PaneID}, ParentPID: 100, PollTimeout: time.Millisecond, EventTimeout: time.Millisecond, MetadataKey: "ports", MetadataValues: []string{"initial", "update", ""}}
}

type fakeDarwin struct {
	polls                []model.Snapshot
	events               []model.Snapshot
	metadata             []MetadataCapture
	eventErr, cleanupErr error
	cleaned              bool
}

func newFakeDarwin() *fakeDarwin {
	c := controlled()
	r := c
	r.PID = 102
	return &fakeDarwin{polls: []model.Snapshot{{}, {Revision: 1, Applications: []model.Application{application(c)}}, {Revision: 3}}, events: []model.Snapshot{{Revision: 2, Applications: []model.Application{application(r)}}, {Revision: 3}}, metadata: []MetadataCapture{{WorkspaceID: c.WorkspaceID, Values: map[string]string{"ports": "initial"}}, {WorkspaceID: c.WorkspaceID, Values: map[string]string{"ports": "update"}}, {WorkspaceID: c.WorkspaceID, Values: map[string]string{"ports": ""}}}}
}

func (f *fakeDarwin) EnsureStopped(context.Context) error              { return nil }
func (f *fakeDarwin) StartFakeHerdr(context.Context, LiveConfig) error { return nil }
func (f *fakeDarwin) StartParent(context.Context, LiveConfig) error    { return nil }
func (f *fakeDarwin) StartDaemon(context.Context, LiveConfig) error    { return nil }
func (f *fakeDarwin) Poll(context.Context) (model.Snapshot, error) {
	if len(f.polls) == 0 {
		return model.Snapshot{}, ErrNotReady
	}
	next := f.polls[0]
	f.polls = f.polls[1:]
	return next, nil
}
func (f *fakeDarwin) Subscribe(context.Context) (<-chan model.Snapshot, error) {
	if f.eventErr != nil {
		return nil, f.eventErr
	}
	ch := make(chan model.Snapshot, len(f.events))
	for _, event := range f.events {
		ch <- event
	}
	close(ch)
	return ch, nil
}
func (f *fakeDarwin) Metadata(context.Context) ([]MetadataCapture, error) { return f.metadata, nil }
func (f *fakeDarwin) Lsof(context.Context) (string, error)                { return "tcp", nil }
func (f *fakeDarwin) Cleanup(context.Context) ([]string, error) {
	f.cleaned = true
	return []string{"daemon"}, f.cleanupErr
}
func (f *fakeDarwin) Status(context.Context) (string, error) { return "not running", nil }
