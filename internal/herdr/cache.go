package herdr

import (
	"sync"

	"github.com/carellano/herdr-apps/internal/model"
)

// Cache retains only complete snapshots; callers must rebuild after reconnect.
type Cache struct {
	mu       sync.RWMutex
	snapshot model.Snapshot
}

func (c *Cache) Replace(snapshot model.Snapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snapshot = snapshot
}

func (c *Cache) Snapshot() model.Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshot
}
