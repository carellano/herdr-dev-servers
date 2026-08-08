package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/carellano/herdr-apps/internal/config"
	"github.com/carellano/herdr-apps/internal/correlation"
	"github.com/carellano/herdr-apps/internal/discovery"
	"github.com/carellano/herdr-apps/internal/model"
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
			}
		}
	}
}

func StatePaths() (Paths, error) {
	base := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if base == "" {
		var err error
		base, err = config.Dir()
		if err != nil {
			return Paths{}, err
		}
		base = filepath.Join(base, "state")
	}
	return Paths{StateDir: base, Socket: filepath.Join(base, "daemon.sock"), Lock: filepath.Join(base, "daemon.lock")}, nil
}

// Run owns the Unix socket and periodically publishes complete scanner passes. Herdr correlation is
// deliberately unavailable until its live schema transport reports a compatible snapshot.
func Run(ctx context.Context, paths Paths, cfg config.Config, scanner discovery.Scanner, processes discovery.ProcessTable, inspector ProcessInspector) error {
	if scanner == nil || processes == nil || inspector == nil {
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
	service := &Service{}
	refresh := func() {
		listeners, err := scanner.Scan(ctx)
		if err != nil {
			return
		}
		input := correlation.Input{Processes: map[int]discovery.Process{}, ObservedAt: time.Now().UTC(), ProcessUnavailable: "Herdr live snapshot unavailable"}
		for _, listener := range listeners {
			if cfg.Ignored(listener.Port) {
				continue
			}
			input.Listeners = append(input.Listeners, listener)
			if process, err := processes.Lookup(ctx, listener.PID); err == nil {
				input.Processes[listener.PID] = process
			}
		}
		service.Replace(model.Snapshot{Applications: correlation.Build(input).All(), ObservedAt: input.ObservedAt})
	}
	refresh()
	ticker := time.NewTicker(cfg.Interval())
	defer ticker.Stop()
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
		case <-ticker.C:
			refresh()
		default:
		}
	}
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
