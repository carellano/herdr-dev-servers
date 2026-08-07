package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/carellano/herdr-apps/internal/config"
	"github.com/carellano/herdr-apps/internal/daemon"
	"github.com/carellano/herdr-apps/internal/model"
)

type fakeClient struct{ response model.IPCResponse }

func (f fakeClient) Request(context.Context, model.IPCRequest) (model.IPCResponse, error) {
	return f.response, nil
}
func TestListAndInspectRendering(t *testing.T) {
	snapshot := model.Snapshot{Applications: []model.Application{{ID: "app", Endpoints: []model.Endpoint{{Port: 3000}}}}}
	text, err := RenderList(snapshot, false)
	if err != nil || !strings.Contains(text, "app") {
		t.Fatalf("%q %v", text, err)
	}
	jsonText, _ := RenderList(snapshot, true)
	if !strings.Contains(jsonText, "3000") {
		t.Fatal("JSON list missing endpoint")
	}
	app, err := Inspect(context.Background(), fakeClient{model.IPCResponse{Result: map[string]any{"id": "app"}}}, "app")
	if err != nil || app.ID != "app" {
		t.Fatalf("%#v %v", app, err)
	}
}
func TestDoctorDoesNotClaimUnavailableLiveChecks(t *testing.T) {
	report := Doctor(daemon.Paths{Socket: t.TempDir() + "/missing"}, config.Defaults())
	if !strings.Contains(report, "live checks not claimed") {
		t.Fatalf("doctor = %q", report)
	}
}
