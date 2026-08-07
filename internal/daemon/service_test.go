package daemon

import (
	"testing"

	"github.com/carellano/herdr-apps/internal/model"
)

func TestServiceReplacePublishesCompleteRevision(t *testing.T) {
	service := &Service{}
	published := service.Replace(model.Snapshot{Applications: []model.Application{{ID: "app-a"}}})
	if published.Revision != 1 {
		t.Fatalf("revision = %d, want 1", published.Revision)
	}
	if got := service.Snapshot(); len(got.Applications) != 1 || got.Applications[0].ID != "app-a" {
		t.Fatalf("snapshot = %#v, want app-a", got)
	}
}
