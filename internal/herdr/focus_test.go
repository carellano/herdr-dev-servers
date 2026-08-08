package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestFocusMethodsSendDocumentedRequests(t *testing.T) {
	for _, tt := range []struct {
		name, id, method, target, reply string
		call                            func(JSONLTransport) error
	}{
		{name: "pane", id: "pane", method: "pane.focus", target: `{"pane_id":"w1:p1"}`, call: func(t JSONLTransport) error { return t.FocusPane(context.Background(), "pane", "w1:p1") }},
		{name: "workspace", id: "workspace", method: "workspace.focus", target: `{"workspace_id":"w1"}`, call: func(t JSONLTransport) error { return t.FocusWorkspace(context.Background(), "workspace", "w1") }},
		{name: "tab", id: "tab", method: "tab.focus", target: `{"tab_id":"w1:t1"}`, call: func(t JSONLTransport) error { return t.FocusTab(context.Background(), "tab", "w1:t1") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client, server := net.Pipe()
			defer server.Close()
			transport := JSONLTransport{Dial: func(context.Context, string) (net.Conn, error) { return client, nil }}
			requests := make(chan request, 1)
			go func() {
				defer close(requests)
				var got request
				_ = json.NewDecoder(server).Decode(&got)
				requests <- got
				_, _ = server.Write([]byte(`{"id":"` + tt.id + `","result":{}}` + "\n"))
			}()
			if err := tt.call(transport); err != nil {
				t.Fatalf("focus call error = %v", err)
			}
			got := <-requests
			params, _ := json.Marshal(got.Params)
			if got.ID != tt.id || got.Method != tt.method || string(params) != tt.target {
				t.Fatalf("request = %#v params=%s", got, params)
			}
		})
	}
}

func TestCurrentFocusParsesSnapshotResponse(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	transport := JSONLTransport{Dial: func(context.Context, string) (net.Conn, error) { return client, nil }}
	go func() {
		var got request
		_ = json.NewDecoder(server).Decode(&got)
		_, _ = server.Write([]byte(`{"id":"focus","result":{"type":"session_snapshot","snapshot":{"focused_workspace_id":"w1","focused_tab_id":"w1:t1","focused_pane_id":"w1:p1"}}}` + "\n"))
	}()
	focus, err := transport.CurrentFocus(context.Background(), "focus")
	if err != nil || focus != (FocusSnapshotResponse{FocusedWorkspaceID: "w1", FocusedTabID: "w1:t1", FocusedPaneID: "w1:p1"}) {
		t.Fatalf("CurrentFocus() = %#v, %v", focus, err)
	}
}

func TestCurrentFocusRejectsInvalidSnapshots(t *testing.T) {
	for _, tt := range []struct {
		name, result string
	}{
		{name: "missing", result: `{"type":"session_snapshot"}`},
		{name: "null", result: `{"type":"session_snapshot","snapshot":null}`},
		{name: "wrong type", result: `{"type":"session_snapshot","snapshot":"not an object"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client, server := net.Pipe()
			defer server.Close()
			transport := JSONLTransport{Dial: func(context.Context, string) (net.Conn, error) { return client, nil }}
			go func() {
				var got request
				_ = json.NewDecoder(server).Decode(&got)
				_, _ = server.Write([]byte(`{"id":"focus","result":` + tt.result + "}\n"))
			}()
			if _, err := transport.CurrentFocus(context.Background(), "focus"); !errors.Is(err, ErrMalformedEnvelope) {
				t.Fatalf("CurrentFocus() error = %v, want malformed envelope", err)
			}
		})
	}
}

func TestFocusMethodsRejectMissingIDsAndPreserveTypedFailures(t *testing.T) {
	transport := JSONLTransport{Dial: func(context.Context, string) (net.Conn, error) { t.Fatal("Dial called"); return nil, nil }}
	for _, call := range []func() error{
		func() error { return transport.FocusPane(context.Background(), "pane", "") },
		func() error { return transport.FocusWorkspace(context.Background(), "workspace", "") },
		func() error { return transport.FocusTab(context.Background(), "tab", "") },
	} {
		if err := call(); err == nil || !strings.Contains(err.Error(), "ID is required") {
			t.Fatalf("error = %v, want required ID", err)
		}
	}

	client, server := net.Pipe()
	defer server.Close()
	transport = JSONLTransport{Dial: func(context.Context, string) (net.Conn, error) { return client, nil }}
	go func() {
		var got request
		_ = json.NewDecoder(server).Decode(&got)
		_, _ = server.Write([]byte(`{"id":"pane","error":{"code":"unavailable","message":"stopped"}}` + "\n"))
	}()
	err := transport.FocusPane(context.Background(), "pane", "w1:p1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "unavailable" {
		t.Fatalf("error = %#v, want APIError", err)
	}
}

func TestFocusMethodsRejectMismatchedResponseID(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	transport := JSONLTransport{Dial: func(context.Context, string) (net.Conn, error) { return client, nil }}
	go func() {
		var got request
		_ = json.NewDecoder(server).Decode(&got)
		_, _ = server.Write([]byte(`{"id":"other","result":{}}` + "\n"))
	}()
	if err := transport.FocusPane(context.Background(), "pane", "w1:p1"); err == nil || !strings.Contains(err.Error(), "response ID mismatch") {
		t.Fatalf("error = %v, want response ID mismatch", err)
	}
}

func TestJSONLTransportCallHonorsContextAfterConnect(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	transport := JSONLTransport{Dial: func(context.Context, string) (net.Conn, error) { return client, nil }}
	go func() {
		var got request
		_ = json.NewDecoder(server).Decode(&got)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := transport.FocusPane(ctx, "pane", "w1:p1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("FocusPane() error = %v, want deadline exceeded", err)
	}
}
