package proof

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/carellano/herdr-dev-servers/internal/herdr"
)

const (
	maxFakeRequest = 64 << 10
	maxFakeClients = 8
)

// MetadataRequestCapture records a fake workspace.report_metadata request.
type MetadataRequestCapture struct {
	WorkspaceID, Source string
	Tokens              map[string]string
}

// FakeHerdr is a bounded, in-process protocol-19 server for durable proof adapters.
type FakeHerdr struct {
	mu        sync.Mutex
	snapshot  json.RawMessage
	processes map[string]herdr.ProcessInfoResponse
	metadata  []MetadataRequestCapture
	clients   map[*fakeClient]struct{}
	closed    bool
}

type fakeClient struct {
	conn net.Conn
	mu   sync.Mutex
}

// NewFakeHerdr creates an empty server. It never opens a listener.
func NewFakeHerdr() *FakeHerdr {
	return &FakeHerdr{processes: map[string]herdr.ProcessInfoResponse{}, clients: map[*fakeClient]struct{}{}}
}

// SetState atomically replaces the topology snapshot and pane process evidence.
func (f *FakeHerdr) SetState(snapshot json.RawMessage, processes map[string]herdr.ProcessInfoResponse) error {
	if !emptyObject(snapshot) {
		return fmt.Errorf("fake Herdr state: invalid snapshot")
	}
	copyProcesses := make(map[string]herdr.ProcessInfoResponse, len(processes))
	for id, info := range processes {
		copyProcesses[id] = info
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return fmt.Errorf("fake Herdr is closed")
	}
	f.snapshot, f.processes = append(json.RawMessage(nil), snapshot...), copyProcesses
	return nil
}

// Metadata returns ordered, defensive metadata captures.
func (f *FakeHerdr) Metadata() []MetadataRequestCapture {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]MetadataRequestCapture, len(f.metadata))
	for i, capture := range f.metadata {
		result[i] = MetadataRequestCapture{capture.WorkspaceID, capture.Source, copyTokens(capture.Tokens)}
	}
	return result
}

// SubscriptionReady reports whether at least one subscription is ready for events.
func (f *FakeHerdr) SubscriptionReady() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.clients) > 0
}

// Emit broadcasts one typed event to current subscribers in call order.
func (f *FakeHerdr) Emit(event any) error {
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	var typed struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &typed) != nil || typed.Type == "" {
		return fmt.Errorf("fake Herdr event: missing type")
	}
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return fmt.Errorf("fake Herdr is closed")
	}
	clients := make([]*fakeClient, 0, len(f.clients))
	for client := range f.clients {
		clients = append(clients, client)
	}
	f.mu.Unlock()
	for _, client := range clients {
		if err := client.write(map[string]json.RawMessage{"event": raw}); err != nil {
			f.remove(client)
		}
	}
	return nil
}

// Close is idempotent and unblocks every connected client.
func (f *FakeHerdr) Close() error {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return nil
	}
	f.closed = true
	clients := make([]*fakeClient, 0, len(f.clients))
	for client := range f.clients {
		clients = append(clients, client)
	}
	f.clients = map[*fakeClient]struct{}{}
	f.mu.Unlock()
	for _, client := range clients {
		_ = client.conn.Close()
	}
	return nil
}

// Serve handles one caller over an injected connection; it never starts a socket listener.
func (f *FakeHerdr) Serve(conn net.Conn) {
	client := &fakeClient{conn: conn}
	defer func() { f.remove(client); _ = conn.Close() }()
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 1024), maxFakeRequest)
	for scanner.Scan() {
		var request struct {
			ID, Method string
			Params     json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil || request.ID == "" || request.Method == "" {
			_ = client.reply("", nil, "invalid_request", "malformed request")
			continue
		}
		result, code, message, subscribe := f.handle(request.Method, request.Params)
		if code != "" {
			_ = client.reply(request.ID, nil, code, message)
			continue
		}
		if subscribe && !f.add(client) {
			_ = client.reply(request.ID, nil, "busy", "subscription limit reached")
			continue
		}
		if err := client.reply(request.ID, result, "", ""); err != nil {
			return
		}
	}
	if scanner.Err() != nil {
		_ = client.reply("", nil, "invalid_request", "request exceeds limit")
	}
}

func (f *FakeHerdr) handle(method string, params json.RawMessage) (any, string, string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil, "closed", "server is closed", false
	}
	switch method {
	case "session.snapshot":
		if !emptyObject(params) {
			return nil, "invalid_params", "snapshot params must be an object", false
		}
		return herdr.SnapshotResponse{Snapshot: append(json.RawMessage(nil), f.snapshot...)}, "", "", false
	case "pane.process_info":
		var request herdr.ProcessInfoRequest
		if json.Unmarshal(params, &request) != nil || request.PaneID == "" {
			return nil, "invalid_params", "pane_id is required", false
		}
		info, ok := f.processes[request.PaneID]
		if !ok {
			return nil, "not_found", "pane is unavailable", false
		}
		return struct {
			ProcessInfo herdr.ProcessInfoResponse `json:"process_info"`
		}{info}, "", "", false
	case "workspace.report_metadata":
		var request herdr.MetadataRequest
		if json.Unmarshal(params, &request) != nil || request.WorkspaceID == "" || request.Source == "" || request.Tokens == nil {
			return nil, "invalid_params", "workspace_id, source, and tokens are required", false
		}
		tokens := map[string]string{}
		for key, value := range request.Tokens {
			if value == nil {
				return nil, "invalid_params", "metadata token is nil", false
			}
			tokens[key] = *value
		}
		f.metadata = append(f.metadata, MetadataRequestCapture{request.WorkspaceID, request.Source, tokens})
		return map[string]any{}, "", "", false
	case "events.subscribe":
		var request herdr.SubscriptionRequest
		if json.Unmarshal(params, &request) != nil || len(request.Subscriptions) == 0 {
			return nil, "invalid_params", "subscriptions are required", false
		}
		for _, subscription := range request.Subscriptions {
			if subscription.Type == "" {
				return nil, "invalid_params", "subscription type is required", false
			}
		}
		return map[string]string{"type": "subscription_started"}, "", "", true
	default:
		return nil, "unknown_method", "unsupported method", false
	}
}

func (f *FakeHerdr) add(client *fakeClient) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed || len(f.clients) >= maxFakeClients {
		return false
	}
	f.clients[client] = struct{}{}
	return true
}
func (f *FakeHerdr) remove(client *fakeClient) { f.mu.Lock(); delete(f.clients, client); f.mu.Unlock() }
func emptyObject(raw json.RawMessage) bool {
	var object map[string]json.RawMessage
	return json.Unmarshal(raw, &object) == nil && object != nil
}
func copyTokens(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
func (c *fakeClient) reply(id string, result any, code, message string) error {
	reply := map[string]any{"id": id}
	if code != "" {
		reply["error"] = map[string]string{"code": code, "message": message}
	} else {
		reply["result"] = result
	}
	return c.write(reply)
}
func (c *fakeClient) write(value any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(time.Second))
	defer c.conn.SetWriteDeadline(time.Time{})
	return json.NewEncoder(c.conn).Encode(value)
}
