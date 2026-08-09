package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/carellano/herdr-dev-servers/internal/model"
)

type Paths struct{ StateDir, Socket, Lock string }

// Ticker provides a fakeable periodic reconciliation clock.
type Ticker interface {
	Chan() <-chan time.Time
	Stop()
}

type systemTicker struct{ *time.Ticker }

func (t systemTicker) Chan() <-chan time.Time { return t.C }

// Runtime rebuilds complete reconciled snapshots around the persistent Herdr event stream.
type Runtime struct {
	Service   *Service
	Subscribe func(context.Context) (<-chan struct{}, error)
	Rebuild   func(context.Context, model.Snapshot) (model.Snapshot, error)
	Publish   func(context.Context, []model.Application) error
	NewTicker func(time.Duration) Ticker
	Wait      func(context.Context, time.Duration) error
	Interval  time.Duration
	Backoff   time.Duration
	Retries   int
}

// RuntimeError identifies an exhausted reconnect loop while preserving the last complete Service snapshot.
type RuntimeError struct{ Err error }

func (e *RuntimeError) Error() string { return "Herdr runtime unavailable: " + e.Err.Error() }
func (e *RuntimeError) Unwrap() error { return e.Err }

// Run establishes baseline→subscribe→confirm, coalesces event bursts, and fully rebuilds after loss.
func (r Runtime) Run(ctx context.Context) error {
	if r.Service == nil || r.Subscribe == nil || r.Rebuild == nil {
		return &RuntimeError{fmt.Errorf("runtime dependencies are incomplete")}
	}
	backoff := r.Backoff
	if backoff <= 0 {
		backoff = time.Second
	}
	retries := r.Retries
	if retries < 1 {
		retries = 3
	}
	interval := r.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	newTicker := r.NewTicker
	if newTicker == nil {
		newTicker = func(interval time.Duration) Ticker { return systemTicker{time.NewTicker(interval)} }
	}
	wait := r.Wait
	if wait == nil {
		wait = func(ctx context.Context, duration time.Duration) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(duration):
				return nil
			}
		}
	}
	for failures := 0; ; {
		if _, err := r.Rebuild(ctx, r.Service.Snapshot()); err == nil {
			events, err := r.Subscribe(ctx)
			if err == nil {
				if snapshot, err := r.Rebuild(ctx, r.Service.Snapshot()); err == nil {
					published := r.Service.Replace(snapshot)
					if r.Publish != nil {
						if err := r.Publish(ctx, published.Applications); err != nil {
							return err
						}
					}
					failures = 0
					err := r.runEvents(ctx, events, interval, newTicker)
					if err != nil {
						return err
					}
					if ctx.Err() != nil {
						return nil
					}
					if err := wait(ctx, backoff); err != nil {
						if ctx.Err() != nil {
							return nil
						}
						return err
					}
					continue
				}
			}
		}
		failures++
		if failures >= retries {
			return &RuntimeError{fmt.Errorf("reconnect failed after %d attempts", failures)}
		}
		if err := wait(ctx, backoff); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
}

func (r Runtime) runEvents(ctx context.Context, events <-chan struct{}, interval time.Duration, newTicker func(time.Duration) Ticker) error {
	ticker := newTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case _, ok := <-events:
			if !ok {
				return nil
			}
			for len(events) > 0 {
				<-events
			}
			if snapshot, err := r.Rebuild(ctx, r.Service.Snapshot()); err == nil {
				published := r.Service.Replace(snapshot)
				if r.Publish != nil {
					if err := r.Publish(ctx, published.Applications); err != nil {
						return err
					}
				}
			} else {
				r.Service.MarkStale()
			}
		case <-ticker.Chan():
			for len(ticker.Chan()) > 0 {
				<-ticker.Chan()
			}
			if snapshot, err := r.Rebuild(ctx, r.Service.Snapshot()); err == nil {
				published := r.Service.Replace(snapshot)
				if r.Publish != nil {
					if err := r.Publish(ctx, published.Applications); err != nil {
						return err
					}
				}
			} else {
				r.Service.MarkStale()
			}
		}
	}
}

func StatePaths() (Paths, error) {
	return statePaths(os.Getenv, os.UserHomeDir)
}

func statePaths(getenv func(string) string, userHomeDir func() (string, error)) (Paths, error) {
	base := getenv("HERDR_PLUGIN_STATE_DIR")
	if base == "" {
		if stateHome := getenv("XDG_STATE_HOME"); stateHome != "" {
			base = filepath.Join(stateHome, "herdr", "plugins", "carellano.dev-servers")
		} else {
			home, err := userHomeDir()
			if err != nil {
				return Paths{}, fmt.Errorf("resolve user home for plugin state: %w", err)
			}
			base = filepath.Join(home, ".local", "state", "herdr", "plugins", "carellano.dev-servers")
		}
	}
	return Paths{StateDir: base, Socket: filepath.Join(base, "herdr-dev-servers.sock"), Lock: filepath.Join(base, "herdr-dev-servers.lock")}, nil
}

// Serve exposes the supplied daemon authority over its private IPC socket.
func Serve(ctx context.Context, paths Paths, service *Service, inspector ProcessInspector) error {
	if service == nil || inspector == nil {
		return fmt.Errorf("daemon dependencies are incomplete")
	}
	lock, err := AcquireLock(paths.Lock, inspector)
	if err != nil {
		return err
	}
	defer lock.Release()
	if err := os.MkdirAll(paths.StateDir, 0o700); err != nil {
		return err
	}
	if err := os.Remove(paths.Socket); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale daemon socket: %w", err)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: paths.Socket, Net: "unix"})
	if err != nil {
		return fmt.Errorf("listen on daemon socket: %w", err)
	}
	defer func() { listener.Close(); os.Remove(paths.Socket) }()
	if err := os.Chmod(paths.Socket, 0o600); err != nil {
		return err
	}
	for {
		listener.SetDeadline(time.Now().Add(100 * time.Millisecond))
		conn, err := listener.AcceptUnix()
		if err == nil {
			go func() { defer conn.Close(); _ = service.ServeJSONL(conn, conn) }()
			continue
		}
		if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		default:
		}
	}
}

type Client struct{ Socket string }

// IPCClientError retains the daemon's typed protocol code for actionable clients.
type IPCClientError struct{ Payload *model.IPCError }

func (e *IPCClientError) Error() string { return "daemon " + e.Payload.Code + ": " + e.Payload.Message }

func (c Client) Request(ctx context.Context, request model.IPCRequest) (model.IPCResponse, error) {
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", c.Socket)
	if err != nil {
		return model.IPCResponse{}, fmt.Errorf("connect to daemon: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return model.IPCResponse{}, fmt.Errorf("set daemon deadline: %w", err)
		}
	}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return model.IPCResponse{}, err
	}
	var response model.IPCResponse
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return model.IPCResponse{}, fmt.Errorf("read daemon response: %w", err)
	}
	if response.Error != nil {
		return response, &IPCClientError{Payload: response.Error}
	}
	return response, nil
}
