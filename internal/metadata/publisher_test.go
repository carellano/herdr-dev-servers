package metadata

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/carellano/herdr-dev-servers/internal/herdr"
	"github.com/carellano/herdr-dev-servers/internal/model"
)

type reportCall struct{ workspace, source, key, value string }
type reporterFake struct {
	calls []reportCall
	fail  string
}

func (f *reporterFake) ReportMetadata(_ context.Context, workspace, source string, tokens map[string]*string) error {
	if workspace == f.fail {
		return errors.New("stopped")
	}
	value := ""
	if tokens["dev_servers"] != nil {
		value = *tokens["dev_servers"]
	}
	for key := range tokens {
		f.calls = append(f.calls, reportCall{workspace, source, key, value})
	}
	return nil
}

func TestPublisherPrepareUsesCompactPositivePorts(t *testing.T) {
	for _, tt := range []struct {
		name string
		apps []model.Application
		want string
	}{
		{"one port without URL", []model.Application{{Endpoints: []model.Endpoint{{Port: 8081}}}}, ":8081"},
		{"sorted and deduplicated", []model.Application{{Endpoints: []model.Endpoint{{Port: 8081}, {Port: 3000}, {Port: 8081}, {Port: 0}, {Port: -1}}}}, ":3000 :8081"},
		{"endpoint limit", []model.Application{{Endpoints: []model.Endpoint{{Port: 3006}, {Port: 3000}, {Port: 3005}, {Port: 3001}, {Port: 3004}, {Port: 3002}, {Port: 3003}}}}, ":3000 :3001 :3002 :3003 :3004 :3005 +1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			publication := (&Publisher{}).Prepare(tt.apps)
			if !publication.Changed || publication.Value != tt.want {
				t.Fatalf("publication = %#v, want changed %q", publication, tt.want)
			}
		})
	}

	ports := []model.Endpoint{}
	maxInt := int(^uint(0) >> 1)
	for _, port := range []int{maxInt - 5, maxInt - 4, maxInt - 3, maxInt - 2, maxInt - 1, maxInt} {
		ports = append(ports, model.Endpoint{Port: port})
	}
	publication := (&Publisher{}).Prepare([]model.Application{{Endpoints: ports}})
	if len(publication.Value) > maxBytes || !strings.HasSuffix(publication.Value, "+3") {
		t.Fatalf("byte-bounded publication = %#v", publication)
	}

	publisher := Publisher{}
	if publisher.Prepare([]model.Application{{Endpoints: []model.Endpoint{{Port: 8081}}}}).Changed == false {
		t.Fatal("first publication was suppressed")
	}
	if second := publisher.Prepare([]model.Application{{Endpoints: []model.Endpoint{{Port: 8081}}}}); second.Changed {
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
		{Association: model.Association{WorkspaceID: "a"}, Endpoints: []model.Endpoint{{Port: 3000}}},
		{Association: model.Association{WorkspaceID: "b"}, Endpoints: []model.Endpoint{{Port: 3001}}},
	}
	if err := publisher.Publish(context.Background(), apps, reporter); err != nil {
		t.Fatal(err)
	}
	if len(reporter.calls) != 2 {
		t.Fatalf("calls = %#v", reporter.calls)
	}
	if reporter.calls[0] != (reportCall{workspace: "a", source: "herdr-dev-servers", key: "dev_servers", value: ":3000"}) || reporter.calls[1] != (reportCall{workspace: "b", source: "herdr-dev-servers", key: "dev_servers", value: ":3001"}) {
		t.Fatalf("metadata calls = %#v", reporter.calls)
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
	if got := reporter.calls[2]; got.workspace != "b" || got.source != "herdr-dev-servers" || got.key != "dev_servers" || got.value != "" {
		t.Fatalf("removed workspace clear = %#v", got)
	}
}

func TestPublishIncludesPortWithoutURL(t *testing.T) {
	publisher, reporter := Publisher{}, &reporterFake{}
	apps := []model.Application{{Association: model.Association{WorkspaceID: "wC"}, Endpoints: []model.Endpoint{{Port: 8081}}}}
	if err := publisher.Publish(context.Background(), apps, reporter); err != nil {
		t.Fatal(err)
	}
	if got := reporter.calls; len(got) != 1 || got[0] != (reportCall{workspace: "wC", source: "herdr-dev-servers", key: "dev_servers", value: ":8081"}) {
		t.Fatalf("calls = %#v", got)
	}
}

func TestPublishReturnsTypedReportErrorWithoutPartialState(t *testing.T) {
	publisher, reporter := Publisher{}, &reporterFake{fail: "b"}
	apps := []model.Application{
		{Association: model.Association{WorkspaceID: "a"}, Endpoints: []model.Endpoint{{Port: 3000}}},
		{Association: model.Association{WorkspaceID: "b"}, Endpoints: []model.Endpoint{{Port: 3001}}},
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
	value := ":3000"
	reporter := HerdrReporter{Transport: transport, RequestID: func() string { return "request" }}
	if err := reporter.ReportMetadata(context.Background(), "workspace", "herdr-dev-servers", map[string]*string{"dev_servers": &value}); err != nil {
		t.Fatal(err)
	}
	if transport.id != "request" || transport.metadata.WorkspaceID != "workspace" || transport.metadata.Source != "herdr-dev-servers" || transport.metadata.Tokens["dev_servers"] != &value {
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
