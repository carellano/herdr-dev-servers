package proof

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/carellano/herdr-dev-servers/internal/herdr"
)

func TestFakeHerdrSnapshotProcessAndMetadata(t *testing.T) {
	f := NewFakeHerdr()
	state := json.RawMessage(`{"workspaces":[{"workspace_id":"w"}]}`)
	if err := f.SetState(state, map[string]herdr.ProcessInfoResponse{"p": {PaneID: "p", ShellPID: 7}}); err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tr := transport(f)
	got, err := tr.Snapshot(context.Background(), "snapshot")
	if err != nil || string(got.Snapshot) != string(state) {
		t.Fatalf("Snapshot() = %s, %v", got.Snapshot, err)
	}
	info, err := tr.ProcessInfo(context.Background(), "process", "p")
	if err != nil || info.ShellPID != 7 {
		t.Fatalf("ProcessInfo() = %#v, %v", info, err)
	}
	if err := tr.ReportMetadata(context.Background(), "metadata", herdr.MetadataRequest{WorkspaceID: "w", Source: "herdr-dev-servers", Tokens: map[string]*string{"dev_servers": ptr("one")}}); err != nil {
		t.Fatal(err)
	}
	if err := tr.ReportMetadata(context.Background(), "metadata-2", herdr.MetadataRequest{WorkspaceID: "w", Source: "herdr-dev-servers", Tokens: map[string]*string{"dev_servers": ptr("two")}}); err != nil {
		t.Fatal(err)
	}
	if got := f.Metadata(); len(got) != 2 || got[0].WorkspaceID != "w" || got[0].Tokens["dev_servers"] != "one" || got[1].Tokens["dev_servers"] != "two" {
		t.Fatalf("Metadata() = %#v", got)
	}
}

func TestFakeHerdrSubscriptionOrdersEventsAndCloses(t *testing.T) {
	f := NewFakeHerdr()
	defer f.Close()
	tr := transport(f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := tr.Subscribe(ctx, "events", []herdr.Subscription{{Type: "pane.updated"}})
	if err != nil {
		t.Fatal(err)
	}
	if !f.SubscriptionReady() {
		t.Fatal("subscription was not ready")
	}
	if err := f.Emit(map[string]string{"type": "replacement"}); err != nil {
		t.Fatal(err)
	}
	if err := f.Emit(map[string]string{"type": "removal"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"replacement", "removal"} {
		select {
		case got := <-events:
			if got.Type != want {
				t.Fatalf("event = %q, want %q", got.Type, want)
			}
		case <-time.After(time.Second):
			t.Fatal("event timeout")
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := <-events; ok {
		t.Fatal("events remain open after Close")
	}
}

func TestFakeHerdrRejectsBadRequestsAndHandlesConcurrency(t *testing.T) {
	f := NewFakeHerdr()
	defer f.Close()
	if err := f.SetState(json.RawMessage(`{}`), map[string]herdr.ProcessInfoResponse{}); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"not json\n", `{"id":"x","method":"nope","params":{}}` + "\n", `{"id":"x","method":"pane.process_info","params":{}}` + "\n"} {
		client, server := net.Pipe()
		go f.Serve(server)
		if _, err := client.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
		var reply struct {
			Error *herdr.APIError `json:"error"`
		}
		if err := json.NewDecoder(client).Decode(&reply); err != nil || reply.Error == nil || reply.Error.Code == "" {
			t.Fatalf("reply = %#v, %v", reply, err)
		}
		client.Close()
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = transport(f).Snapshot(context.Background(), "x") }()
	}
	wg.Wait()
}

func transport(f *FakeHerdr) herdr.JSONLTransport {
	return herdr.JSONLTransport{Dial: func(context.Context, string) (net.Conn, error) {
		client, server := net.Pipe()
		go f.Serve(server)
		return client, nil
	}}
}
func ptr(s string) *string { return &s }
