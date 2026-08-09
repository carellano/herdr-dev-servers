package daemon

import (
	"testing"

	"github.com/carellano/herdr-dev-servers/internal/model"
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

func TestServiceMarkStaleIsBoundedAndDoesNotChurnRevisions(t *testing.T) {
	service := &Service{}
	first := service.Replace(model.Snapshot{Applications: []model.Application{{ID: "app"}}})
	stale := service.MarkStale()
	again := service.MarkStale()
	if stale.Revision != first.Revision+1 || again.Revision != stale.Revision || !stale.Applications[0].Association.Stale {
		t.Fatalf("first=%#v stale=%#v again=%#v", first, stale, again)
	}
	if evidence := stale.Applications[0].Evidence; len(evidence) != 1 || evidence[0].Unavailable != rebuildUnavailable {
		t.Fatalf("evidence=%#v", evidence)
	}
}
