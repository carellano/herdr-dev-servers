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

func TestDurableDarwinRunnerBindsDynamicIdentitiesBeforeEvidence(t *testing.T) {
	fake := newFakeDarwin()
	cfg := runnerConfig()
	cfg.DynamicBinding, cfg.Control.PID, cfg.Replacement.PID, cfg.ParentPID = true, 0, 0, 0
	cfg.Binder = binderFunc(func(_ context.Context, got LiveConfig) (IdentityBinding, error) {
		fake.order = append(fake.order, "bind")
		if got.Control.PID != 0 || got.Replacement.PID != 0 || got.ParentPID != 0 {
			t.Fatalf("binder received caller PIDs: %#v", got)
		}
		c := controlled()
		r := c
		r.PID = 102
		return IdentityBinding{ParentPID: 100, Initial: c, Replacement: r}, nil
	})

	got, err := RunDarwin(context.Background(), cfg, DurableDarwinRunner{Ops: fake})
	if err != nil {
		t.Fatal(err)
	}
	if got.ParentPID != 100 || got.IDs.PID != 101 {
		t.Fatalf("receipt = %#v", got)
	}
	if want := []string{"parent", "bind", "fake", "daemon", "poll"}; !sameStrings(fake.order[:len(want)], want) {
		t.Fatalf("lifecycle = %v, want prefix %v", fake.order, want)
	}
	if fake.fakeConfig.ParentPID != 100 || fake.daemonConfig.Control.PID != 101 || fake.daemonConfig.Replacement.PID != 102 {
		t.Fatalf("bound identities did not reach fake Herdr and daemon: fake=%#v daemon=%#v", fake.fakeConfig, fake.daemonConfig)
	}
}

func TestDynamicIdentityBindingFailsClosed(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*LiveConfig)
		bind   IdentityBinder
	}{
		{"missing binder", func(*LiveConfig) {}, nil},
		{"caller PID", func(c *LiveConfig) { c.ParentPID = 100 }, binderFunc(validBinding)},
		{"binder error", func(*LiveConfig) {}, binderFunc(func(context.Context, LiveConfig) (IdentityBinding, error) {
			return IdentityBinding{}, errors.New("bind failed")
		})},
		{"zero PID", func(*LiveConfig) {}, binderFunc(func(_ context.Context, c LiveConfig) (IdentityBinding, error) {
			b, _ := validBinding(context.Background(), c)
			b.Initial.PID = 0
			return b, nil
		})},
		{"negative PID", func(*LiveConfig) {}, binderFunc(func(_ context.Context, c LiveConfig) (IdentityBinding, error) {
			b, _ := validBinding(context.Background(), c)
			b.Replacement.PID = -1
			return b, nil
		})},
		{"duplicate PID", func(*LiveConfig) {}, binderFunc(func(_ context.Context, c LiveConfig) (IdentityBinding, error) {
			b, _ := validBinding(context.Background(), c)
			b.Replacement.PID = b.Initial.PID
			return b, nil
		})},
		{"listener is parent", func(*LiveConfig) {}, binderFunc(func(_ context.Context, c LiveConfig) (IdentityBinding, error) {
			b, _ := validBinding(context.Background(), c)
			b.ParentPID = b.Initial.PID
			return b, nil
		})},
		{"endpoint drift", func(*LiveConfig) {}, binderFunc(func(_ context.Context, c LiveConfig) (IdentityBinding, error) {
			b, _ := validBinding(context.Background(), c)
			b.Initial.Endpoint = "http://127.0.0.1:9"
			return b, nil
		})},
		{"workspace drift", func(*LiveConfig) {}, binderFunc(func(_ context.Context, c LiveConfig) (IdentityBinding, error) {
			b, _ := validBinding(context.Background(), c)
			b.Replacement.WorkspaceID = "other"
			return b, nil
		})},
		{"tab drift", func(*LiveConfig) {}, binderFunc(func(_ context.Context, c LiveConfig) (IdentityBinding, error) {
			b, _ := validBinding(context.Background(), c)
			b.Initial.TabID = "other"
			return b, nil
		})},
		{"pane drift", func(*LiveConfig) {}, binderFunc(func(_ context.Context, c LiveConfig) (IdentityBinding, error) {
			b, _ := validBinding(context.Background(), c)
			b.Replacement.PaneID = "other"
			return b, nil
		})},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := runnerConfig()
			cfg.DynamicBinding, cfg.Control.PID, cfg.Replacement.PID, cfg.ParentPID, cfg.Binder = true, 0, 0, 0, tt.bind
			tt.mutate(&cfg)
			fake := newFakeDarwin()
			if _, err := RunDarwin(context.Background(), cfg, DurableDarwinRunner{Ops: fake}); err == nil {
				t.Fatalf("RunDarwin() error = %v, want fail closed", err)
			}
			for _, step := range fake.order {
				if step == "poll" {
					t.Fatal("evidence collection started after an invalid binding")
				}
			}
		})
	}
}

func validBinding(_ context.Context, c LiveConfig) (IdentityBinding, error) {
	initial := c.Control
	initial.PID = 101
	replacement := initial
	replacement.PID = 102
	return IdentityBinding{ParentPID: 100, Initial: initial, Replacement: replacement}, nil
}

type binderFunc func(context.Context, LiveConfig) (IdentityBinding, error)

func (f binderFunc) Bind(ctx context.Context, cfg LiveConfig) (IdentityBinding, error) {
	return f(ctx, cfg)
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
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
	order                []string
	fakeConfig           LiveConfig
	daemonConfig         LiveConfig
}

func newFakeDarwin() *fakeDarwin {
	c := controlled()
	r := c
	r.PID = 102
	return &fakeDarwin{polls: []model.Snapshot{{}, {Revision: 1, Applications: []model.Application{application(c)}}, {Revision: 3}}, events: []model.Snapshot{{Revision: 2, Applications: []model.Application{application(r)}}, {Revision: 3}}, metadata: []MetadataCapture{{WorkspaceID: c.WorkspaceID, Values: map[string]string{"ports": "initial"}}, {WorkspaceID: c.WorkspaceID, Values: map[string]string{"ports": "update"}}, {WorkspaceID: c.WorkspaceID, Values: map[string]string{"ports": ""}}}}
}

func (f *fakeDarwin) EnsureStopped(context.Context) error { return nil }
func (f *fakeDarwin) StartFakeHerdr(_ context.Context, cfg LiveConfig) error {
	f.order, f.fakeConfig = append(f.order, "fake"), cfg
	return nil
}
func (f *fakeDarwin) StartParent(context.Context, LiveConfig) error {
	f.order = append(f.order, "parent")
	return nil
}
func (f *fakeDarwin) StartDaemon(_ context.Context, cfg LiveConfig) error {
	f.order, f.daemonConfig = append(f.order, "daemon"), cfg
	return nil
}
func (f *fakeDarwin) Poll(context.Context) (model.Snapshot, error) {
	f.order = append(f.order, "poll")
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
