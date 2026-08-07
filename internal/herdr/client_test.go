package herdr

import (
	"context"
	"errors"
	"testing"

	"github.com/carellano/herdr-apps/internal/daemon"
	"github.com/carellano/herdr-apps/internal/model"
)

type fakeTransport struct {
	snapshots  []model.Snapshot
	reads      int
	subscribed bool
}

func (f *fakeTransport) Capabilities(context.Context) (Capabilities, error) {
	return Capabilities{Protocol: RequiredProtocol, Schema: RequiredSchema}, nil
}

func (f *fakeTransport) Snapshot(context.Context) (model.Snapshot, error) {
	snapshot := f.snapshots[f.reads]
	f.reads++
	return snapshot, nil
}

func (f *fakeTransport) Subscribe(context.Context) error {
	f.subscribed = true
	return nil
}

func TestReconnectPublishesConfirmingSnapshot(t *testing.T) {
	transport := &fakeTransport{snapshots: []model.Snapshot{
		{Applications: []model.Application{{ID: "old"}}},
		{Applications: []model.Application{{ID: "current"}}},
	}}
	service := &daemon.Service{}
	cache := &Cache{}
	client := Client{Transport: transport, Service: service, Cache: cache}

	got, err := client.Reconnect(context.Background())
	if err != nil {
		t.Fatalf("Reconnect() error = %v", err)
	}
	if !transport.subscribed || transport.reads != 2 {
		t.Fatalf("reconnect sequence subscribed=%t reads=%d, want true/2", transport.subscribed, transport.reads)
	}
	if got.Revision != 1 || got.Applications[0].ID != "current" {
		t.Fatalf("published snapshot = %#v, want current revision 1", got)
	}
	if cached := cache.Snapshot(); cached.Applications[0].ID != "current" {
		t.Fatalf("cache = %#v, want current snapshot", cached)
	}
}

func TestCapabilitiesRequireProtocolAndSchema(t *testing.T) {
	tests := []struct {
		name  string
		value Capabilities
		want  bool
	}{
		{name: "required capability", value: Capabilities{Protocol: 19, Schema: 1}, want: true},
		{name: "old protocol", value: Capabilities{Protocol: 18, Schema: 1}, want: false},
		{name: "different schema", value: Capabilities{Protocol: 19, Schema: 2}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.Compatible(); got != tt.want {
				t.Fatalf("Compatible() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestReconnectKeepsCompleteCacheWhenConfirmationFails(t *testing.T) {
	cache := &Cache{}
	cache.Replace(model.Snapshot{Applications: []model.Application{{ID: "complete"}}})
	client := Client{Transport: &failingTransport{}, Service: &daemon.Service{}, Cache: cache}
	if _, err := client.Reconnect(context.Background()); !errors.Is(err, errStreamLost) {
		t.Fatalf("error = %v", err)
	}
	if got := cache.Snapshot().Applications[0].ID; got != "complete" {
		t.Fatalf("cache = %q", got)
	}
}

var errStreamLost = errors.New("stream lost")

type failingTransport struct{ fakeTransport }

func (f *failingTransport) Snapshot(context.Context) (model.Snapshot, error) {
	return model.Snapshot{}, errStreamLost
}
