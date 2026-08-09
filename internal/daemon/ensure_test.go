package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWatchEnsurer(t *testing.T) {
	unavailable := errors.New("unavailable")
	for _, tc := range []struct {
		name      string
		health    []error
		startErr  error
		exit      error
		attempts  int
		wantErr   string
		wantStart bool
	}{
		{name: "already running", health: []error{nil}},
		{name: "becomes ready after spawn", health: []error{unavailable, unavailable, nil}, wantStart: true},
		{name: "contender exits while winner becomes ready", health: []error{unavailable, unavailable, nil}, exit: errors.New("lock is held"), wantStart: true},
		{name: "spawn failure", health: []error{unavailable}, startErr: errors.New("permission denied"), wantErr: "start daemon: permission denied", wantStart: true},
		{name: "early child exit", health: []error{unavailable, unavailable}, exit: errors.New("lock is held"), wantErr: "daemon exited before readiness: lock is held", wantStart: true},
		{name: "readiness timeout", health: []error{unavailable}, attempts: 2, wantErr: "daemon readiness timed out after 2 attempts", wantStart: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			starter := &fakeWatchStarter{err: tc.startErr, process: fakeWatchProcess{exit: tc.exit}}
			ensurer := testWatchEnsurer(tc.health, starter)
			ensurer.Attempts = tc.attempts
			err := ensurer.Ensure(context.Background())
			if tc.wantErr == "" && err != nil {
				t.Fatal(err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
			if got := len(starter.calls) > 0; got != tc.wantStart {
				t.Fatalf("started = %v, want %v", got, tc.wantStart)
			}
		})
	}
}

func TestWatchEnsurerDoesNotSpawnWithCanceledContext(t *testing.T) {
	starter := &fakeWatchStarter{process: fakeWatchProcess{}}
	ensurer := testWatchEnsurer([]error{errors.New("unavailable")}, starter)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := ensurer.Ensure(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if len(starter.calls) != 0 {
		t.Fatalf("starts = %d, want 0", len(starter.calls))
	}
}

func TestWatchEnsurerStartsLiteralDaemonWithInheritedEnvironment(t *testing.T) {
	starter := &fakeWatchStarter{process: fakeWatchProcess{}}
	ensurer := testWatchEnsurer([]error{errors.New("unavailable"), nil}, starter)
	ensurer.Executable = func() (string, error) { return "/plugin root/herdr-dev-servers", nil }
	ensurer.Environment = func() []string {
		return []string{"HERDR_SOCKET_PATH=/tmp/herdr.sock", "HERDR_PLUGIN_STATE_DIR=/tmp/state"}
	}
	if err := ensurer.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(starter.calls) != 1 {
		t.Fatalf("starts = %d", len(starter.calls))
	}
	call := starter.calls[0]
	if call.executable != "/plugin root/herdr-dev-servers" || len(call.argv) != 1 || call.argv[0] != "daemon" {
		t.Fatalf("command = %#v", call)
	}
	if strings.Join(call.environment, ",") != "HERDR_SOCKET_PATH=/tmp/herdr.sock,HERDR_PLUGIN_STATE_DIR=/tmp/state" {
		t.Fatalf("environment = %#v", call.environment)
	}
}

func testWatchEnsurer(health []error, starter WatchStarter) WatchEnsurer {
	index := 0
	return WatchEnsurer{
		StatePaths: func() (Paths, error) { return Paths{Socket: "/tmp/daemon.sock"}, nil },
		Healthy: func(context.Context, Paths) error {
			if index >= len(health) {
				return health[len(health)-1]
			}
			err := health[index]
			index++
			return err
		},
		Executable:  func() (string, error) { return "/plugin/herdr-dev-servers", nil },
		Environment: func() []string { return nil },
		Starter:     starter,
		Clock:       immediateWatchClock{},
		Attempts:    3,
		Retry:       time.Millisecond,
	}
}

type fakeWatchStarter struct {
	err     error
	process WatchProcess
	calls   []watchStart
}

func (s *fakeWatchStarter) Start(_ context.Context, executable string, argv, environment []string) (WatchProcess, error) {
	s.calls = append(s.calls, watchStart{executable, append([]string(nil), argv...), append([]string(nil), environment...)})
	return s.process, s.err
}

type watchStart struct {
	executable        string
	argv, environment []string
}

type fakeWatchProcess struct{ exit error }

func (p fakeWatchProcess) Exited() <-chan error {
	if p.exit == nil {
		return nil
	}
	exited := make(chan error, 1)
	exited <- p.exit
	return exited
}

type immediateWatchClock struct{}

func (immediateWatchClock) After(time.Duration) <-chan time.Time {
	tick := make(chan time.Time, 1)
	tick <- time.Time{}
	return tick
}
