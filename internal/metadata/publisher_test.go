package metadata

import (
	"strings"
	"testing"

	"github.com/carellano/herdr-apps/internal/herdr"
	"github.com/carellano/herdr-apps/internal/model"
)

func TestPublisherBoundsStableOutputAndSuppressesUnchangedWrites(t *testing.T) {
	app := model.Application{Endpoints: []model.Endpoint{{URL: "http://127.0.0.1:3000"}, {URL: "http://127.0.0.1:3001"}, {URL: "http://127.0.0.1:3002"}, {URL: "http://127.0.0.1:3003"}, {URL: "http://127.0.0.1:3004"}, {URL: "http://127.0.0.1:3005"}, {URL: "http://127.0.0.1:3006"}}}
	publisher := Publisher{}
	first := publisher.Prepare([]model.Application{app})
	if !first.Changed || len(first.Value) > 80 || !strings.Contains(first.Value, "+") {
		t.Fatalf("first publication = %#v, want bounded changed +N", first)
	}
	if second := publisher.Prepare([]model.Application{app}); second.Changed {
		t.Fatalf("unchanged publication = %#v, want suppression", second)
	}
}

func TestCompatibilityGuidanceExplainsUnsupportedHerdr(t *testing.T) {
	guidance := CompatibilityGuidance(herdr.Capabilities{Protocol: 18, Schema: 1})
	if !strings.Contains(guidance, "protocol") || !strings.Contains(guidance, "19") {
		t.Fatalf("guidance = %q, want protocol requirement", guidance)
	}
}
