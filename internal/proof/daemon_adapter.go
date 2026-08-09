package proof

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/carellano/herdr-dev-servers/internal/model"
)

// DaemonProcess is the narrow, injected lifecycle boundary for the proof daemon.
type DaemonProcess interface {
	PID() int
	Signal(context.Context, string) error
	Wait(context.Context) error
}

// DaemonStarter starts one fixed argv command without shell interpretation.
type DaemonStarter interface {
	Start(context.Context, []string) (DaemonProcess, error)
}

// DaemonIdentity is the process incarnation returned by the injected ps boundary.
type DaemonIdentity struct {
	PID   int
	Token string
}

type DaemonInspector interface {
	Inspect(context.Context, int) (DaemonIdentity, error)
}

type SnapshotPoller interface {
	Snapshot(context.Context) (*model.Snapshot, error)
}

type PollClock interface {
	After(time.Duration) <-chan time.Time
}

type LsofBoundary interface {
	Lsof(context.Context, int) (string, error)
}

// DaemonAdapter owns only the process and socket it starts through injected boundaries.
// It is intentionally not wired to a command or live entry point.
type DaemonAdapter struct {
	host            *DarwinHostSafety
	starter         DaemonStarter
	poller          SnapshotPoller
	inspector       DaemonInspector
	lsof            LsofBoundary
	retry, shutdown time.Duration
	process         DaemonProcess
	identity        DaemonIdentity
	socket          string
}

func NewDaemonAdapter(host *DarwinHostSafety, starter DaemonStarter, poller SnapshotPoller, inspector DaemonInspector, lsof LsofBoundary, retry, shutdown time.Duration) *DaemonAdapter {
	return &DaemonAdapter{host: host, starter: starter, poller: poller, inspector: inspector, lsof: lsof, retry: retry, shutdown: shutdown}
}

// Start reserves the plugin endpoint, starts a literal argv, and records its verified owner.
func (a *DaemonAdapter) Start(ctx context.Context, binary, endpoint string) error {
	if a.host == nil || a.starter == nil || a.inspector == nil || a.process != nil {
		return fmt.Errorf("%w: daemon adapter dependencies are incomplete", ErrLiveSafety)
	}
	socket, err := a.host.FakeEndpoint(endpoint)
	if err != nil {
		return err
	}
	argv, err := a.host.FixedArgv(binary, "daemon", "--socket", socket)
	if err != nil {
		return err
	}
	process, err := a.starter.Start(ctx, argv)
	if err != nil {
		return err
	}
	identity, err := a.inspector.Inspect(ctx, process.PID())
	if err != nil || identity.PID != process.PID() || identity.Token == "" {
		return fmt.Errorf("%w: verify daemon ownership", ErrLiveSafety)
	}
	a.process, a.identity, a.socket = process, identity, socket
	return nil
}

// WaitReady polls a bounded IPC boundary; null snapshots are retryable, not evidence.
func (a *DaemonAdapter) WaitReady(ctx context.Context, timeout time.Duration, want Control) (model.Snapshot, error) {
	if a.poller == nil || a.retry <= 0 || timeout <= 0 {
		return model.Snapshot{}, fmt.Errorf("%w: polling boundaries require positive deadlines", ErrLiveSafety)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		snapshot, err := a.poller.Snapshot(ctx)
		if err == nil && snapshot != nil {
			_, err = SelectControlled(snapshot.Applications, want)
		}
		if err == nil && snapshot != nil {
			return *snapshot, nil
		}
		if err != nil && !errors.Is(err, ErrNotReady) {
			return model.Snapshot{}, err
		}
		select {
		case <-ctx.Done():
			return model.Snapshot{}, ctx.Err()
		case <-a.pollAfter(a.retry):
		}
	}
}

func (a *DaemonAdapter) pollAfter(d time.Duration) <-chan time.Time {
	if clock, ok := a.poller.(PollClock); ok {
		return clock.After(d)
	}
	return time.After(d)
}

func (a *DaemonAdapter) Lsof(ctx context.Context) (string, error) {
	if a.lsof == nil || a.process == nil {
		return "", fmt.Errorf("%w: lsof requires an owned daemon", ErrLiveSafety)
	}
	return a.lsof.Lsof(ctx, a.process.PID())
}

// Cleanup refuses foreign sockets or changed PIDs before a bounded graceful TERM wait.
func (a *DaemonAdapter) Cleanup(ctx context.Context) error {
	if a.process == nil || !a.host.Owns(a.socket) || a.shutdown <= 0 {
		return fmt.Errorf("%w: refuse cleanup of unowned daemon resource", ErrLiveSafety)
	}
	identity, err := a.inspector.Inspect(ctx, a.process.PID())
	if err != nil || identity != a.identity {
		return fmt.Errorf("%w: daemon ownership changed", ErrLiveSafety)
	}
	if err := a.process.Signal(ctx, "TERM"); err != nil {
		return err
	}
	waitCtx, cancel := context.WithTimeout(ctx, a.shutdown)
	defer cancel()
	if err := a.process.Wait(waitCtx); err != nil {
		return err
	}
	if err := a.host.RemoveOwned(a.socket); err != nil {
		return err
	}
	a.process = nil
	return nil
}
