package herdr

import (
	"context"
	"net"
	"strings"
	"testing"
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
