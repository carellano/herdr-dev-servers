package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/carellano/herdr-apps/internal/model"
)

const (
	ensureWatchAttempts = 20
	ensureWatchRetry    = 100 * time.Millisecond
)

// WatchProcess reports whether a process started for readiness has exited.
type WatchProcess interface {
	Exited() <-chan error
}

// WatchStarter starts the daemon without shell interpretation.
type WatchStarter interface {
	Start(context.Context, string, []string, []string) (WatchProcess, error)
}

// WatchClock keeps readiness polling deterministic in tests.
type WatchClock interface {
	After(time.Duration) <-chan time.Time
}

// WatchEnsurer starts a daemon only when its IPC authority is unavailable.
type WatchEnsurer struct {
	StatePaths  func() (Paths, error)
	Healthy     func(context.Context, Paths) error
	Executable  func() (string, error)
	Environment func() []string
	Starter     WatchStarter
	Clock       WatchClock
	Attempts    int
	Retry       time.Duration
}

// EnsureWatch resolves plugin state using StatePaths and makes the local daemon ready.
func EnsureWatch(ctx context.Context) error {
	return defaultWatchEnsurer().Ensure(ctx)
}

func (e WatchEnsurer) Ensure(ctx context.Context) error {
	paths, err := e.StatePaths()
	if err != nil {
		return fmt.Errorf("resolve daemon state paths: %w", err)
	}
	attempts, retry := e.Attempts, e.Retry
	if attempts <= 0 {
		attempts = ensureWatchAttempts
	}
	if retry <= 0 {
		retry = ensureWatchRetry
	}
	if err := e.health(ctx, paths, retry); err == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("wait for daemon readiness: %w", err)
	}

	executable, err := e.Executable()
	if err != nil {
		return fmt.Errorf("resolve daemon executable: %w", err)
	}
	process, err := e.Starter.Start(ctx, executable, []string{"daemon"}, e.Environment())
	if err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	exited := process.Exited()
	var earlyExit error
	recordExit := func(err error, ok bool) {
		exited = nil
		if !ok || err == nil {
			err = fmt.Errorf("exited successfully before becoming ready")
		}
		earlyExit = err
	}
	for attempt := 0; attempt < attempts; attempt++ {
		if err := e.health(ctx, paths, retry); err == nil {
			return nil
		}
		if exited != nil {
			select {
			case err, ok := <-exited:
				recordExit(err, ok)
			default:
			}
		}
		select {
		case err, ok := <-exited:
			recordExit(err, ok)
		case <-ctx.Done():
			return fmt.Errorf("wait for daemon readiness: %w", ctx.Err())
		case <-e.Clock.After(retry):
		}
	}
	if earlyExit != nil {
		return fmt.Errorf("daemon exited before readiness: %w", earlyExit)
	}
	return fmt.Errorf("daemon readiness timed out after %d attempts", attempts)
}

func (e WatchEnsurer) health(ctx context.Context, paths Paths, timeout time.Duration) error {
	healthCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return e.Healthy(healthCtx, paths)
}

func defaultWatchEnsurer() WatchEnsurer {
	return WatchEnsurer{
		StatePaths:  StatePaths,
		Healthy:     healthyDaemon,
		Executable:  os.Executable,
		Environment: os.Environ,
		Starter:     execWatchStarter{},
		Clock:       realWatchClock{},
		Attempts:    ensureWatchAttempts,
		Retry:       ensureWatchRetry,
	}
}

func healthyDaemon(ctx context.Context, paths Paths) error {
	_, err := (Client{Socket: paths.Socket}).Request(ctx, model.IPCRequest{Version: IPCVersion, RequestID: "ensure-watch", Method: "list"})
	return err
}

type realWatchClock struct{}

func (realWatchClock) After(delay time.Duration) <-chan time.Time { return time.After(delay) }

type execWatchStarter struct{}

func (execWatchStarter) Start(_ context.Context, executable string, argv []string, environment []string) (WatchProcess, error) {
	command := exec.Command(executable, argv...)
	command.Env = append([]string(nil), environment...)
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return nil, err
	}
	exited := make(chan error, 1)
	go func() {
		exited <- command.Wait()
		close(exited)
	}()
	return watchProcess{exited: exited}, nil
}

type watchProcess struct{ exited <-chan error }

func (p watchProcess) Exited() <-chan error { return p.exited }
