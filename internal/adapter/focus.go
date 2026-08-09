package adapter

import (
	"context"
	"errors"
	"time"

	"github.com/carellano/herdr-dev-servers/internal/actions"
	"github.com/carellano/herdr-dev-servers/internal/herdr"
)

const defaultFocusTimeout = 2 * time.Second

type focusTransport interface {
	FocusPane(context.Context, string, string) error
	FocusWorkspace(context.Context, string, string) error
	FocusTab(context.Context, string, string) error
	CurrentFocus(context.Context, string) (herdr.FocusSnapshotResponse, error)
}

// FocusClient invokes only the documented JSONL focus methods with a deadline per request.
type FocusClient struct {
	Transport focusTransport
	Timeout   time.Duration
}

func newFocusClient(transport focusTransport, timeout time.Duration) FocusClient {
	return FocusClient{Transport: transport, Timeout: timeout}
}

func (c FocusClient) FocusPane(_, _, paneID string) error {
	if c.Transport == nil {
		return errors.New("Herdr focus transport is unavailable")
	}
	ctx, cancel := c.context()
	defer cancel()
	return c.Transport.FocusPane(ctx, "herdr-dev-servers-focus-pane", paneID)
}

func (c FocusClient) FocusWorkspaceTab(workspaceID, tabID string) error {
	if c.Transport == nil {
		return errors.New("Herdr focus transport is unavailable")
	}
	ctx, cancel := c.context()
	defer cancel()
	if err := c.Transport.FocusWorkspace(ctx, "herdr-dev-servers-focus-workspace", workspaceID); err != nil {
		return err
	}
	return c.Transport.FocusTab(ctx, "herdr-dev-servers-focus-tab", tabID)
}

func (c FocusClient) CurrentFocus() (actions.FocusLocation, error) {
	if c.Transport == nil {
		return actions.FocusLocation{}, errors.New("Herdr focus transport is unavailable")
	}
	ctx, cancel := c.context()
	defer cancel()
	focus, err := c.Transport.CurrentFocus(ctx, "herdr-dev-servers-current-focus")
	return actions.FocusLocation{
		WorkspaceID: focus.FocusedWorkspaceID,
		TabID:       focus.FocusedTabID,
		PaneID:      focus.FocusedPaneID,
	}, err
}

func (c FocusClient) context() (context.Context, context.CancelFunc) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = defaultFocusTimeout
	}
	return context.WithTimeout(context.Background(), timeout)
}
