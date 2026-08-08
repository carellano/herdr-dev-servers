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

type fakeTicker struct {
	ticks   chan time.Time
	stopped chan struct{}
}

func (t *fakeTicker) Chan() <-chan time.Time { return t.ticks }
func (t *fakeTicker) Stop()                  { close(t.stopped) }

func TestRuntimeReconcilesCoalescedEventsAndPeriodicTicks(t *testing.T) {
	events := make(chan struct{}, 8)
	rebuilds := 0
	published := 0
	started := make(chan int)
	release := make(chan struct{})
	ticker := &fakeTicker{ticks: make(chan time.Time, 2), stopped: make(chan struct{})}
	runtime := Runtime{Service: &Service{}, Subscribe: func(context.Context) (<-chan struct{}, error) {
		return events, nil
	}, Rebuild: func(context.Context, model.Snapshot) (model.Snapshot, error) {
		rebuilds++
		started <- rebuilds
		<-release
		return model.Snapshot{Applications: []model.Application{{ID: string(rune('a' + rebuilds))}}}, nil
	}, NewTicker: func(time.Duration) Ticker { return ticker }, Interval: time.Second, Backoff: time.Millisecond, Retries: 1}
	runtime.Publish = func(context.Context, []model.Application) error { published++; return nil }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	if got := <-started; got != 1 {
		t.Fatalf("baseline rebuild = %d", got)
	}
	release <- struct{}{}
	if got := <-started; got != 2 {
		t.Fatalf("confirming rebuild = %d", got)
	}
	for i := 0; i < 5; i++ {
		events <- struct{}{}
	}
	release <- struct{}{}
	if got := <-started; got != 3 {
		t.Fatalf("event rebuild = %d", got)
	}
	release <- struct{}{}
	ticker.ticks <- time.Time{}
	if got := <-started; got != 4 {
		t.Fatalf("tick rebuild = %d", got)
	}
	release <- struct{}{}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if rebuilds != 4 || published != 3 || runtime.Service.Snapshot().Revision != 3 {
		t.Fatalf("rebuilds=%d published=%d snapshot=%#v", rebuilds, published, runtime.Service.Snapshot())
	}
	<-ticker.stopped
}

func TestRuntimePreservesSnapshotAndBacksOffOnFailureAndCancellation(t *testing.T) {
	service := &Service{}
	service.Replace(model.Snapshot{Applications: []model.Application{{ID: "stable"}}})
	attempts, rebuilds := 0, 0
	started := make(chan struct{})
	runtime := Runtime{Service: service, Subscribe: func(context.Context) (<-chan struct{}, error) { attempts++; return nil, errors.New("offline") }, Rebuild: func(ctx context.Context, _ model.Snapshot) (model.Snapshot, error) {
		rebuilds++
		started <- struct{}{}
		<-ctx.Done()
		return model.Snapshot{}, errors.New("scanner failed")
	}, Backoff: 100 * time.Millisecond, Retries: 2}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	<-started
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if attempts != 0 || rebuilds != 1 || service.Snapshot().Applications[0].ID != "stable" {
		t.Fatalf("attempts=%d rebuilds=%d snapshot=%#v", attempts, rebuilds, service.Snapshot())
	}
}

func TestRuntimeStopsTickerWhenEventPublishFails(t *testing.T) {
	events := make(chan struct{}, 1)
	ticker := &fakeTicker{ticks: make(chan time.Time), stopped: make(chan struct{})}
	tickerReady := make(chan struct{})
	published := 0
	runtime := Runtime{Service: &Service{}, Subscribe: func(context.Context) (<-chan struct{}, error) {
		return events, nil
	}, Rebuild: func(context.Context, model.Snapshot) (model.Snapshot, error) {
		return model.Snapshot{Applications: []model.Application{{ID: "app"}}}, nil
	}, NewTicker: func(time.Duration) Ticker {
		close(tickerReady)
		return ticker
	}, Publish: func(context.Context, []model.Application) error {
		published++
		if published == 2 {
			return errors.New("publish failed")
		}
		return nil
	}}
	done := make(chan error, 1)
	go func() { done <- runtime.Run(context.Background()) }()
	<-tickerReady
	events <- struct{}{}
	if err := <-done; err == nil || err.Error() != "publish failed" {
		t.Fatalf("error = %v", err)
	}
	<-ticker.stopped
}

func TestRuntimeReconnectsAfterEventStreamCloses(t *testing.T) {
	first := make(chan struct{})
	close(first)
	follow := make(chan struct{})
	firstTicker := &fakeTicker{ticks: make(chan time.Time), stopped: make(chan struct{})}
	secondTicker := &fakeTicker{ticks: make(chan time.Time), stopped: make(chan struct{})}
	tickers := []*fakeTicker{firstTicker, secondTicker}
	started := make(chan int)
	release := make(chan struct{})
	waits := make(chan time.Duration)
	continueWait := make(chan struct{})
	subscribed := 0
	runtime := Runtime{Service: &Service{}, Subscribe: func(context.Context) (<-chan struct{}, error) {
		subscribed++
		if subscribed == 1 {
			return first, nil
		}
		return follow, nil
	}, Rebuild: func(context.Context, model.Snapshot) (model.Snapshot, error) {
		started <- subscribed
		<-release
		return model.Snapshot{Applications: []model.Application{{ID: "app"}}}, nil
	}, NewTicker: func(time.Duration) Ticker {
		ticker := tickers[0]
		tickers = tickers[1:]
		return ticker
	}, Wait: func(_ context.Context, duration time.Duration) error {
		waits <- duration
		<-continueWait
		return nil
	}, Backoff: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	if got := <-started; got != 0 {
		t.Fatalf("baseline subscription = %d", got)
	}
	release <- struct{}{}
	if got := <-started; got != 1 {
		t.Fatalf("confirming subscription = %d", got)
	}
	release <- struct{}{}
	if got := <-waits; got != time.Second {
		t.Fatalf("backoff = %s", got)
	}
	<-firstTicker.stopped
	continueWait <- struct{}{}
	if got := <-started; got != 1 {
		t.Fatalf("reconnect baseline subscription = %d", got)
	}
	release <- struct{}{}
	if got := <-started; got != 2 {
		t.Fatalf("reconnect confirm subscription = %d", got)
	}
	release <- struct{}{}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	<-secondTicker.stopped
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
