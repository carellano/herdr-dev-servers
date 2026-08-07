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

type Client struct{ Socket string }

func (c Client) Request(ctx context.Context, request model.IPCRequest) (model.IPCResponse, error) {
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", c.Socket)
	if err != nil {
		return model.IPCResponse{}, fmt.Errorf("connect to daemon: %w", err)
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return model.IPCResponse{}, err
	}
	var response model.IPCResponse
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return model.IPCResponse{}, fmt.Errorf("read daemon response: %w", err)
	}
	if response.Error != nil {
		return response, fmt.Errorf("daemon %s: %s", response.Error.Code, response.Error.Message)
	}
	return response, nil
}
