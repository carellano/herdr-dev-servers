package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/carellano/herdr-dev-servers/internal/config"
	"github.com/carellano/herdr-dev-servers/internal/daemon"
	"github.com/carellano/herdr-dev-servers/internal/model"
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
func TestDoctorUsesInjectedProbe(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{name: "unavailable", err: context.DeadlineExceeded, want: "API compatibility was not checked"},
		{name: "reachable", want: "Herdr socket: reachable; API compatibility was not checked"},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := ""
			report := doctor(daemon.Paths{Socket: t.TempDir() + "/missing"}, config.Defaults(), "/isolated/herdr.sock", func(socket string) error {
				called = socket
				return test.err
			})
			if called != "/isolated/herdr.sock" {
				t.Fatalf("probe socket = %q", called)
			}
			if !strings.Contains(report, test.want) {
				t.Fatalf("doctor = %q", report)
			}
		})
	}
}

func TestHerdrSocketHonorsEnvironmentOverride(t *testing.T) {
	t.Setenv("HERDR_SOCKET_PATH", "/isolated/herdr.sock")
	if socket := herdrSocket(); socket != "/isolated/herdr.sock" {
		t.Fatalf("socket = %q", socket)
	}
}
