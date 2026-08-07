package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/carellano/herdr-apps/internal/config"
	"github.com/carellano/herdr-apps/internal/discovery"
	"github.com/carellano/herdr-apps/internal/model"
)

func TestRuntimeCoalescesBurstsAndRebuildsAfterReconnect(t *testing.T) {
	events := make(chan struct{}, 8)
	follow := make(chan struct{})
	rebuilds := 0
	published := 0
	subscribed := 0
	runtime := Runtime{Service: &Service{}, Subscribe: func(context.Context) (<-chan struct{}, error) {
		subscribed++
		if subscribed == 1 {
			return events, nil
		}
		return follow, nil
	}, Rebuild: func(context.Context, model.Snapshot) (model.Snapshot, error) {
		rebuilds++
		return model.Snapshot{Applications: []model.Application{{ID: string(rune('a' + rebuilds))}}}, nil
	}, Backoff: time.Millisecond, Retries: 1}
	runtime.Publish = func(context.Context, []model.Application) error { published++; return nil }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	for i := 0; i < 5; i++ {
		events <- struct{}{}
	}
	time.Sleep(20 * time.Millisecond)
	close(events)
	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if rebuilds < 3 || published < 2 || runtime.Service.Snapshot().Revision < 2 {
		t.Fatalf("rebuilds=%d published=%d snapshot=%#v", rebuilds, published, runtime.Service.Snapshot())
	}
}

func TestRuntimePreservesSnapshotAndBacksOffOnFailureAndCancellation(t *testing.T) {
	service := &Service{}
	service.Replace(model.Snapshot{Applications: []model.Application{{ID: "stable"}}})
	attempts, rebuilds := 0, 0
	runtime := Runtime{Service: service, Subscribe: func(context.Context) (<-chan struct{}, error) { attempts++; return nil, errors.New("offline") }, Rebuild: func(context.Context, model.Snapshot) (model.Snapshot, error) {
		rebuilds++
		return model.Snapshot{}, errors.New("scanner failed")
	}, Backoff: 100 * time.Millisecond, Retries: 2}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	time.Sleep(15 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if attempts != 0 || rebuilds != 1 || service.Snapshot().Applications[0].ID != "stable" {
		t.Fatalf("attempts=%d rebuilds=%d snapshot=%#v", attempts, rebuilds, service.Snapshot())
	}
}

type runtimeScanner struct{}

func (runtimeScanner) Scan(context.Context) ([]discovery.Listener, error) {
	return []discovery.Listener{{PID: 9, Port: 3000, Address: "127.0.0.1"}}, nil
}

type runtimeProcesses struct{}

func (runtimeProcesses) Lookup(context.Context, int) (discovery.Process, error) {
	return discovery.Process{PID: 9, StartTime: "s", PGID: 9, Executable: "app"}, nil
}

type runtimeInspector struct{}

func (runtimeInspector) Identity(pid int) (ProcessIdentity, error) {
	return ProcessIdentity{PID: pid, StartTime: "self"}, nil
}

func TestRunServesReconciledSnapshot(t *testing.T) {
	dir := t.TempDir()
	socket, err := os.CreateTemp("/tmp", "ha-")
	if err != nil {
		t.Fatal(err)
	}
	socketPath := socket.Name()
	check := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	check(socket.Close())
	check(os.Remove(socketPath))
	check(os.Symlink(dir, socketPath))
	defer os.Remove(socketPath)
	paths := Paths{StateDir: socketPath, Socket: filepath.Join(socketPath, "daemon.sock"), Lock: filepath.Join(socketPath, "daemon.lock")}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, paths, config.Defaults(), runtimeScanner{}, runtimeProcesses{}, runtimeInspector{})
	}()
	deadline := time.Now().Add(time.Second)
	var response model.IPCResponse
	var requestErr error
	for time.Now().Before(deadline) {
		response, requestErr = (Client{Socket: paths.Socket}).Request(context.Background(), model.IPCRequest{Version: IPCVersion, RequestID: "test", Method: "list"})
		if requestErr == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if requestErr != nil {
		t.Fatal(requestErr)
	}
	info, err := os.Stat(paths.Socket)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket permissions = %v, %v", info, err)
	}
	snapshot, ok := response.Result.(map[string]any)
	if !ok || snapshot == nil {
		t.Fatalf("result = %#v", response.Result)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.Socket); !os.IsNotExist(err) {
		t.Fatalf("socket remains after shutdown: %v", err)
	}
}

func TestServeJSONLServesOneResponsePerRequest(t *testing.T) {
	s := &Service{}
	input := strings.NewReader(`{"version":1,"requestId":"list","method":"list"}` + "\n" + `{"version":1,"requestId":"missing","method":"inspect","target":"missing"}` + "\n" + `{"version":1,"requestId":"nope","method":"nope"}` + "\n")
	var output bytes.Buffer
	if err := s.ServeJSONL(input, &output); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("responses = %d, want 3", len(lines))
	}
	var missing, unavailable model.IPCResponse
	_ = json.Unmarshal([]byte(lines[1]), &missing)
	_ = json.Unmarshal([]byte(lines[2]), &unavailable)
	if missing.Error == nil || missing.Error.Code != "not_found" || unavailable.Error == nil || unavailable.Error.Code != "unsupported_method" {
		t.Fatalf("errors = %#v, %#v", missing.Error, unavailable.Error)
	}
}
