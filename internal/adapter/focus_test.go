package adapter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/carellano/herdr-apps/internal/herdr"
)

func TestFocusClientUsesDocumentedCallsWithDeadlines(t *testing.T) {
	transport := &fakeFocusTransport{current: herdr.FocusSnapshotResponse{FocusedWorkspaceID: "w1", FocusedTabID: "w1:t1", FocusedPaneID: "w1:p1"}}
	client := newFocusClient(transport, time.Second)

	if err := client.FocusPane("w1", "w1:t1", "w1:p1"); err != nil {
		t.Fatalf("FocusPane() error = %v", err)
	}
	if err := client.FocusWorkspaceTab("w1", "w1:t1"); err != nil {
		t.Fatalf("FocusWorkspaceTab() error = %v", err)
	}
	current, err := client.CurrentFocus()
	if err != nil {
		t.Fatalf("CurrentFocus() error = %v", err)
	}
	if current.WorkspaceID != "w1" || current.TabID != "w1:t1" || current.PaneID != "w1:p1" {
		t.Fatalf("CurrentFocus() = %#v", current)
	}
	want := []focusCall{
		{method: "pane.focus", id: "herdr-apps-focus-pane", target: "w1:p1"},
		{method: "workspace.focus", id: "herdr-apps-focus-workspace", target: "w1"},
		{method: "tab.focus", id: "herdr-apps-focus-tab", target: "w1:t1"},
		{method: "session.snapshot", id: "herdr-apps-current-focus"},
	}
	if len(transport.calls) != len(want) {
		t.Fatalf("calls = %#v, want %#v", transport.calls, want)
	}
	for i, call := range transport.calls {
		if call.method != want[i].method || call.id != want[i].id || call.target != want[i].target || !call.hasDeadline {
			t.Fatalf("call %d = %#v, want %#v with deadline", i, call, want[i])
		}
	}
}

func TestFocusClientPreservesFailuresAndStopsFallback(t *testing.T) {
	want := errors.New("Herdr unavailable")
	transport := &fakeFocusTransport{workspaceErr: want}
	client := newFocusClient(transport, time.Second)
	if err := client.FocusWorkspaceTab("w1", "w1:t1"); !errors.Is(err, want) {
		t.Fatalf("FocusWorkspaceTab() error = %v, want %v", err, want)
	}
	if len(transport.calls) != 1 || transport.calls[0].method != "workspace.focus" {
		t.Fatalf("calls = %#v, want only workspace.focus", transport.calls)
	}
}

func TestFocusClientBoundsTransportCall(t *testing.T) {
	transport := &fakeFocusTransport{waitForCancel: true}
	client := newFocusClient(transport, 10*time.Millisecond)
	if err := client.FocusPane("w1", "w1:t1", "w1:p1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("FocusPane() error = %v, want deadline exceeded", err)
	}
	if len(transport.calls) != 1 || !transport.calls[0].hasDeadline {
		t.Fatalf("calls = %#v, want bounded call", transport.calls)
	}
}

func TestNewFactorySharesSocketWithFocusClient(t *testing.T) {
	factory := NewFactory("/tmp/herdr-test.sock")
	client, ok := factory.Focus.(FocusClient)
	if !ok {
		t.Fatalf("Focus = %T, want FocusClient", factory.Focus)
	}
	transport, ok := client.Transport.(herdr.JSONLTransport)
	if !ok || transport.Socket != "/tmp/herdr-test.sock" {
		t.Fatalf("focus transport = %#v, want factory socket", client.Transport)
	}
}

type focusCall struct {
	method, id, target string
	hasDeadline        bool
}

type fakeFocusTransport struct {
	calls         []focusCall
	current       herdr.FocusSnapshotResponse
	workspaceErr  error
	waitForCancel bool
}

func (f *fakeFocusTransport) FocusPane(ctx context.Context, id, paneID string) error {
	return f.call(ctx, "pane.focus", id, paneID, nil)
}

func (f *fakeFocusTransport) FocusWorkspace(ctx context.Context, id, workspaceID string) error {
	return f.call(ctx, "workspace.focus", id, workspaceID, f.workspaceErr)
}

func (f *fakeFocusTransport) FocusTab(ctx context.Context, id, tabID string) error {
	return f.call(ctx, "tab.focus", id, tabID, nil)
}

func (f *fakeFocusTransport) CurrentFocus(ctx context.Context, id string) (herdr.FocusSnapshotResponse, error) {
	err := f.call(ctx, "session.snapshot", id, "", nil)
	return f.current, err
}

func (f *fakeFocusTransport) call(ctx context.Context, method, id, target string, err error) error {
	_, hasDeadline := ctx.Deadline()
	f.calls = append(f.calls, focusCall{method: method, id: id, target: target, hasDeadline: hasDeadline})
	if f.waitForCancel {
		<-ctx.Done()
		return ctx.Err()
	}
	return err
}
