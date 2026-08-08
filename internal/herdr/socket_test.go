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

func TestJSONLTransportHandlesFragmentedReply(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	transport := JSONLTransport{Socket: "ignored", Dial: func(context.Context, string) (net.Conn, error) { return client, nil }}
	go func() {
		buf := make([]byte, 128)
		_, _ = server.Read(buf)
		_, _ = server.Write([]byte(`{"id":"one","result":`))
		_, _ = server.Write([]byte(`{"ok":true}}` + "\n"))
	}()
	var result struct {
		OK bool `json:"ok"`
	}
	if err := transport.Call(context.Background(), "one", "session.snapshot", map[string]any{}, &result); err != nil || !result.OK {
		t.Fatalf("%#v %v", result, err)
	}
}

func TestProcessInfoUnwrapsLiveEnvelope(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	transport := JSONLTransport{Dial: func(context.Context, string) (net.Conn, error) { return client, nil }}
	go func() {
		buf := make([]byte, 256)
		_, _ = server.Read(buf)
		_, _ = server.Write([]byte(`{"id":"process","result":{"type":"pane_process_info","process_info":{"pane_id":"p1","shell_pid":4,"foreground_process_group_id":5,"foreground_processes":[{"pid":6,"cmdline":"node app.js","cwd":"/work"}]}}}` + "\n"))
	}()
	info, err := transport.ProcessInfo(context.Background(), "process", "p1")
	if err != nil || info.PaneID != "p1" || info.ForegroundProcessGroupID != 5 || len(info.ForegroundProcesses) != 1 || info.ForegroundProcesses[0].Command != "node app.js" {
		t.Fatalf("ProcessInfo() = %#v, %v", info, err)
	}
}

func TestSubscriptionCorrelatesAckAndStreamsEvents(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	transport := JSONLTransport{Dial: func(context.Context, string) (net.Conn, error) { return client, nil }}
	go func() {
		buf := make([]byte, 256)
		_, _ = server.Read(buf)
		_, _ = server.Write([]byte(`{"id":"sub","result":{"type":"subscription_`))
		_, _ = server.Write([]byte(`started"}}` + "\n" + `{"event":{"type":"pane.created","pane_id":"p1"}}` + "\n"))
	}()
	events, err := transport.Subscribe(context.Background(), "sub", []Subscription{{Type: "pane.created"}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event.Type != "pane.created" {
			t.Fatalf("event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("event not received")
	}
}

func TestSubscriptionRejectsBadFramesAndStopsOnCancel(t *testing.T) {
	for _, tt := range []struct {
		name, reply string
		want        error
	}{
		{"malformed", "{\n", ErrMalformedEnvelope},
		{"oversized", strings.Repeat("x", maxJSONLMessage+1) + "\n", ErrMessageTooLarge},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client, server := net.Pipe()
			defer server.Close()
			transport := JSONLTransport{Dial: func(context.Context, string) (net.Conn, error) { return client, nil }}
			go func() { buf := make([]byte, 256); _, _ = server.Read(buf); _, _ = server.Write([]byte(tt.reply)) }()
			_, err := transport.Subscribe(context.Background(), "sub", nil)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
	client, server := net.Pipe()
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	transport := JSONLTransport{Dial: func(context.Context, string) (net.Conn, error) { return client, nil }}
	go func() {
		buf := make([]byte, 256)
		_, _ = server.Read(buf)
		_, _ = server.Write([]byte(`{"id":"sub","result":{"type":"subscription_started"}}` + "\n"))
		<-ctx.Done()
	}()
	if _, err := transport.Subscribe(ctx, "sub", nil); err != nil {
		t.Fatal(err)
	}
	cancel()
}

func TestSubscriptionClosesAfterEOF(t *testing.T) {
	client, server := net.Pipe()
	transport := JSONLTransport{Dial: func(context.Context, string) (net.Conn, error) { return client, nil }}
	go func() {
		buf := make([]byte, 256)
		_, _ = server.Read(buf)
		_, _ = server.Write([]byte(`{"id":"sub","result":{"type":"subscription_started"}}` + "\n"))
		_ = server.Close()
	}()
	events, err := transport.Subscribe(context.Background(), "sub", nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("events remained open")
		}
	case <-time.After(time.Second):
		t.Fatal("EOF was not observed")
	}
}

func TestProtocolRequestShapes(t *testing.T) {
	for _, tt := range []struct {
		value any
		want  string
	}{
		{request{ID: "1", Method: "session.snapshot", Params: map[string]any{}}, `{"id":"1","method":"session.snapshot","params":{}}`},
		{request{ID: "2", Method: "pane.process_info", Params: ProcessInfoRequest{}}, `{"id":"2","method":"pane.process_info","params":{}}`},
		{request{ID: "3", Method: "workspace.report_metadata", Params: MetadataRequest{WorkspaceID: "w", Source: "herdr-apps", Tokens: map[string]*string{"apps": nil}}}, `{"id":"3","method":"workspace.report_metadata","params":{"workspace_id":"w","source":"herdr-apps","tokens":{"apps":null}}}`},
	} {
		got, _ := json.Marshal(tt.value)
		if string(got) != tt.want {
			t.Fatalf("request = %s", got)
		}
	}
}

func TestJSONLTransportReportsTypedError(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	transport := JSONLTransport{Dial: func(context.Context, string) (net.Conn, error) { return client, nil }}
	go func() {
		buf := make([]byte, 128)
		_, _ = server.Read(buf)
		_, _ = server.Write([]byte(`{"id":"one","error":{"code":"unavailable","message":"stopped"}}` + "\n"))
	}()
	err := transport.Call(context.Background(), "one", "session.snapshot", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "Herdr unavailable: stopped") {
		t.Fatalf("error = %v", err)
	}
}
