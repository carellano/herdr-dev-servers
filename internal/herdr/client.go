package herdr

import (
	"context"
	"fmt"

	"github.com/carellano/herdr-apps/internal/daemon"
	"github.com/carellano/herdr-apps/internal/model"
)

// Transport is the deliberately narrow, testable surface needed for snapshot reconciliation.
type Transport interface {
	Capabilities(context.Context) (Capabilities, error)
	Snapshot(context.Context) (model.Snapshot, error)
	Subscribe(context.Context) error
}

// Client gates static compatibility and rebuilds state from two complete snapshots.
type Client struct {
	Transport Transport
	Service   *daemon.Service
	Cache     *Cache
}

// Reconnect validates capabilities, obtains a baseline, subscribes, then confirms with a full rebuild.
func (c Client) Reconnect(ctx context.Context) (model.Snapshot, error) {
	if c.Transport == nil || c.Service == nil || c.Cache == nil {
		return model.Snapshot{}, fmt.Errorf("Herdr client is incomplete")
	}
	capabilities, err := c.Transport.Capabilities(ctx)
	if err != nil {
		return model.Snapshot{}, fmt.Errorf("read Herdr capabilities: %w", err)
	}
	if !capabilities.Compatible() {
		return model.Snapshot{}, fmt.Errorf("Herdr protocol %d/schema %d is unsupported; require protocol >= %d and schema %d", capabilities.Protocol, capabilities.Schema, RequiredProtocol, RequiredSchema)
	}
	if _, err := c.Transport.Snapshot(ctx); err != nil {
		return model.Snapshot{}, fmt.Errorf("read initial Herdr snapshot: %w", err)
	}
	if err := c.Transport.Subscribe(ctx); err != nil {
		return model.Snapshot{}, fmt.Errorf("subscribe to Herdr events: %w", err)
	}
	confirmed, err := c.Transport.Snapshot(ctx)
	if err != nil {
		return model.Snapshot{}, fmt.Errorf("read confirming Herdr snapshot: %w", err)
	}
	c.Cache.Replace(confirmed)
	return c.Service.Replace(confirmed), nil
}
