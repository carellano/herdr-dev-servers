package proof

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/carellano/herdr-dev-servers/internal/model"
)

func TestDaemonAdapterLifecycle(t *testing.T) {
	for _, tt := range []struct {
		name    string
		mutate  func(*adapterFake)
		wantErr error
	}{
		{"starts, polls, and cleans owned daemon", func(*adapterFake) {}, nil},
		{"start failure records nothing", func(f *adapterFake) { f.startErr = errors.New("start failed") }, fakesError("start failed")},
		{"null readiness retries", func(f *adapterFake) { f.snapshots = append([]*model.Snapshot{nil}, f.snapshots...) }, nil},
		{"readiness timeout is bounded", func(f *adapterFake) { f.snapshots = []*model.Snapshot{nil, nil} }, context.DeadlineExceeded},
		{"ownership drift refuses signal", func(f *adapterFake) { f.drift = true }, ErrLiveSafety},
		{"foreign socket refuses cleanup", func(f *adapterFake) { f.foreign = true }, ErrLiveSafety},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fake := newAdapterFake(t)
			tt.mutate(fake)
			adapter := NewDaemonAdapter(fake.host, fake, fake, fake, fake, time.Millisecond, time.Millisecond)
			err := adapter.Start(context.Background(), "herdr-dev-servers", "plugin.sock")
			if fake.startErr != nil {
				if err == nil || err.Error() != tt.wantErr.Error() {
					t.Fatalf("Start() error = %v, want %v", err, tt.wantErr)
				}
				if fake.signals != 0 {
					t.Fatal("failed start must not clean up unowned resources")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = adapter.WaitReady(context.Background(), time.Millisecond, Control{PID: 7})
			if tt.wantErr == context.DeadlineExceeded && !errors.Is(err, tt.wantErr) {
				t.Fatalf("WaitReady() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil || tt.wantErr == ErrLiveSafety {
				if err != nil {
					t.Fatal(err)
				}
			}
			if fake.foreign {
				delete(fake.host.owned, fake.socket)
			}
			cleanupErr := adapter.Cleanup(context.Background())
			if tt.wantErr == nil && cleanupErr != nil {
				t.Fatal(cleanupErr)
			}
			if tt.wantErr == ErrLiveSafety && cleanupErr == nil {
				t.Fatal("Cleanup() accepted unsafe ownership")
			}
			if tt.wantErr == ErrLiveSafety && fake.signals != 0 {
				t.Fatal("unsafe cleanup sent a signal")
			}
		})
	}
}

func TestDaemonAdapterShutdownWaitIsBounded(t *testing.T) {
	fake := newAdapterFake(t)
	fake.waitErr = context.DeadlineExceeded
	adapter := NewDaemonAdapter(fake.host, fake, fake, fake, fake, time.Millisecond, time.Millisecond)
	if err := adapter.Start(context.Background(), "herdr-dev-servers", "plugin.sock"); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Cleanup(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Cleanup() error = %v, want deadline", err)
	}
}

func TestDaemonAdapterUsesFixedBoundaries(t *testing.T) {
	fake := newAdapterFake(t)
	adapter := NewDaemonAdapter(fake.host, fake, fake, fake, fake, time.Millisecond, time.Millisecond)
	if err := adapter.Start(context.Background(), "herdr-dev-servers", "plugin.sock"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"herdr-dev-servers", "daemon", "--socket", fake.socket}; !sameStrings(fake.argv, want) {
		t.Fatalf("argv = %q, want %q", fake.argv, want)
	}
	if _, err := adapter.Lsof(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fake.lsofCalls != 1 || fake.psCalls == 0 {
		t.Fatalf("boundary calls lsof=%d ps=%d, want lsof=1 ps>0", fake.lsofCalls, fake.psCalls)
	}
}

type adapterFake struct {
	host               *DarwinHostSafety
	socket             string
	argv               []string
	snapshots          []*model.Snapshot
	startErr           error
	drift, foreign     bool
	waitErr            error
	signals            int
	lsofCalls, psCalls int
}

func newAdapterFake(t *testing.T) *adapterFake {
	t.Helper()
	host, err := NewDarwinHostSafety(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	socket, err := host.FakeEndpoint("plugin.sock")
	if err != nil {
		t.Fatal(err)
	}
	ready := model.Application{Identity: model.ProcessIdentity{PID: 7}, Endpoints: []model.Endpoint{{}}, Association: model.Association{Confidence: model.ConfidenceHigh}}
	return &adapterFake{host: host, socket: socket, snapshots: []*model.Snapshot{{Applications: []model.Application{ready}}}}
}

func (f *adapterFake) Start(_ context.Context, argv []string) (DaemonProcess, error) {
	f.argv = append([]string(nil), argv...)
	if f.startErr != nil {
		return nil, f.startErr
	}
	return f, nil
}
func (f *adapterFake) PID() int                             { return 7 }
func (f *adapterFake) Signal(context.Context, string) error { f.signals++; return nil }
func (f *adapterFake) Wait(context.Context) error           { return f.waitErr }
func (f *adapterFake) Inspect(context.Context, int) (DaemonIdentity, error) {
	f.psCalls++
	if f.drift && f.psCalls > 1 {
		return DaemonIdentity{PID: 7, Token: "other"}, nil
	}
	return DaemonIdentity{PID: 7, Token: "owned"}, nil
}
func (f *adapterFake) Snapshot(context.Context) (*model.Snapshot, error) {
	if len(f.snapshots) == 0 {
		return nil, nil
	}
	next := f.snapshots[0]
	f.snapshots = f.snapshots[1:]
	return next, nil
}
func (f *adapterFake) After(time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- time.Now()
	return ch
}
func (f *adapterFake) Lsof(context.Context, int) (string, error) { f.lsofCalls++; return "lsof", nil }
