package adapter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/carellano/herdr-dev-servers/internal/correlation"
	"github.com/carellano/herdr-dev-servers/internal/discovery"
)

type processTableFake struct {
	processes []discovery.Process
	errs      []error
	index     int
}

func (f *processTableFake) Lookup(_ context.Context, _ int) (discovery.Process, error) {
	index := f.index
	if index < len(f.processes)-1 {
		f.index++
	}
	if index < len(f.errs) && f.errs[index] != nil {
		return discovery.Process{}, f.errs[index]
	}
	return f.processes[index], nil
}

type immediateClock struct{}

func (immediateClock) After(time.Duration) <-chan time.Time {
	channel := make(chan time.Time, 1)
	channel <- time.Time{}
	return channel
}

func testProcess() discovery.Process {
	return discovery.Process{PID: 42, ParentPID: 7, PGID: 42, StartTime: "start", Executable: "/usr/bin/node", Args: []string{"node", "server.js"}, CWD: "/work/app"}
}

func TestProcessInspectorMatchesCorrelationIdentity(t *testing.T) {
	process := testProcess()
	got, err := NewProcessInspector(&processTableFake{processes: []discovery.Process{process}}).Inspect(context.Background(), process.PID)
	result := correlation.Build(correlation.Input{
		Listeners: []discovery.Listener{{PID: process.PID, Port: 3000}},
		Processes: map[int]discovery.Process{
			process.PID: process,
			7:           {PID: 7, Executable: "/bin/sh", CWD: "/work"},
		},
	})
	if err != nil || len(result.All()) != 1 || got != result.All()[0].Identity {
		t.Fatalf("Inspect() = %#v, %v", got, err)
	}
}

type blockingTable struct{}

func (blockingTable) Lookup(ctx context.Context, _ int) (discovery.Process, error) {
	<-ctx.Done()
	return discovery.Process{}, ctx.Err()
}

func TestProcessInspectorBoundsBlockedLookup(t *testing.T) {
	inspector := ProcessInspector{Table: blockingTable{}, Timeout: time.Millisecond}
	if _, err := inspector.Inspect(context.Background(), 42); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Inspect() = %v, want deadline exceeded", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := inspector.Wait(ctx, correlation.LogicalIdentity(testProcess())); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait() = %v, want context cancellation", err)
	}

	grace, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	inspector.Timeout = time.Second
	if err := inspector.Wait(grace, correlation.LogicalIdentity(testProcess())); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait() = %v, want grace deadline", err)
	}
}

func TestProcessInspectorWaitsForChangedIncarnation(t *testing.T) {
	identity := correlation.LogicalIdentity(testProcess())
	for _, test := range []struct {
		name      string
		processes []discovery.Process
		errs      []error
	}{
		{name: "disappears", processes: []discovery.Process{testProcess(), {}}, errs: []error{nil, errors.New("gone")}},
		{name: "PID reused", processes: []discovery.Process{testProcess(), func() discovery.Process { p := testProcess(); p.StartTime = "new"; return p }()}},
		{name: "PGID changed", processes: []discovery.Process{testProcess(), func() discovery.Process { p := testProcess(); p.PGID = 99; return p }()}},
		{name: "key changed", processes: []discovery.Process{testProcess(), func() discovery.Process { p := testProcess(); p.Args = []string{"node", "other.js"}; return p }()}},
	} {
		t.Run(test.name, func(t *testing.T) {
			inspector := ProcessInspector{Table: &processTableFake{processes: test.processes, errs: test.errs}, Clock: immediateClock{}}
			if err := inspector.Wait(context.Background(), identity); err != nil {
				t.Fatalf("Wait() = %v", err)
			}
		})
	}
}

func TestProcessInspectorWaitReturnsDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	inspector := ProcessInspector{Table: &processTableFake{processes: []discovery.Process{testProcess()}}, Clock: immediateClock{}}
	if err := inspector.Wait(ctx, correlation.LogicalIdentity(testProcess())); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait() = %v, want context cancellation", err)
	}
}
