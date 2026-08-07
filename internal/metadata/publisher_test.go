package metadata

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/carellano/herdr-apps/internal/herdr"
	"github.com/carellano/herdr-apps/internal/model"
)

type reportCall struct{ workspace, value string }
type reporterFake struct {
	calls []reportCall
	fail  string
}

func (f *reporterFake) ReportMetadata(_ context.Context, workspace, _ string, tokens map[string]*string) error {
	if workspace == f.fail {
		return errors.New("stopped")
	}
	value := ""
	if tokens["ports"] != nil {
		value = *tokens["ports"]
	}
	f.calls = append(f.calls, reportCall{workspace, value})
	return nil
}

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

func TestPublishWritesOnlyChangedWorkspacesAndClearsRemoved(t *testing.T) {
	publisher, reporter := Publisher{}, &reporterFake{}
	apps := []model.Application{
		{Association: model.Association{WorkspaceID: "a"}, Endpoints: []model.Endpoint{{URL: "http://127.0.0.1:3000"}}},
		{Association: model.Association{WorkspaceID: "b"}, Endpoints: []model.Endpoint{{URL: "http://127.0.0.1:3001"}}},
	}
	if err := publisher.Publish(context.Background(), apps, reporter); err != nil {
		t.Fatal(err)
	}
	if len(reporter.calls) != 2 {
		t.Fatalf("calls = %#v", reporter.calls)
	}
	if reporter.calls[0].value == "" || reporter.calls[1].value == "" {
		t.Fatalf("ports token missing from calls = %#v", reporter.calls)
	}
	if err := publisher.Publish(context.Background(), apps, reporter); err != nil {
		t.Fatal(err)
	}
	if len(reporter.calls) != 2 {
		t.Fatalf("unchanged calls = %#v", reporter.calls)
	}
	if err := publisher.Publish(context.Background(), apps[:1], reporter); err != nil {
		t.Fatal(err)
	}
	if got := reporter.calls[2]; got.workspace != "b" || got.value != "" {
		t.Fatalf("removed workspace clear = %#v", got)
	}
}

func TestPublishReturnsTypedReportErrorWithoutPartialState(t *testing.T) {
	publisher, reporter := Publisher{}, &reporterFake{fail: "b"}
	apps := []model.Application{
		{Association: model.Association{WorkspaceID: "a"}, Endpoints: []model.Endpoint{{URL: "http://127.0.0.1:3000"}}},
		{Association: model.Association{WorkspaceID: "b"}, Endpoints: []model.Endpoint{{URL: "http://127.0.0.1:3001"}}},
	}
	var reportErr *ReportError
	if err := publisher.Publish(context.Background(), apps, reporter); !errors.As(err, &reportErr) || reportErr.WorkspaceID != "b" {
		t.Fatalf("error = %v", err)
	}
	if err := publisher.Publish(context.Background(), apps[:1], reporter); err != nil {
		t.Fatal(err)
	}
	if len(reporter.calls) != 2 || reporter.calls[1].workspace != "a" {
		t.Fatalf("failed publication advanced state: %#v", reporter.calls)
	}
}

func TestHerdrReporterUsesMetadataTransport(t *testing.T) {
	transport := &metadataTransportFake{}
	value := "http://127.0.0.1:3000"
	reporter := HerdrReporter{Transport: transport, RequestID: func() string { return "request" }}
	if err := reporter.ReportMetadata(context.Background(), "workspace", "herdr-apps", map[string]*string{"ports": &value}); err != nil {
		t.Fatal(err)
	}
	if transport.id != "request" || transport.metadata.WorkspaceID != "workspace" || transport.metadata.Tokens["ports"] != &value {
		t.Fatalf("transport = %#v", transport)
	}
}

type metadataTransportFake struct {
	id       string
	metadata herdr.MetadataRequest
}

func (f *metadataTransportFake) ReportMetadata(_ context.Context, id string, metadata herdr.MetadataRequest) error {
	f.id, f.metadata = id, metadata
	return nil
}
