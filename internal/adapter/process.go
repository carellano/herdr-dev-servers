package adapter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/carellano/herdr-apps/internal/correlation"
	"github.com/carellano/herdr-apps/internal/discovery"
	"github.com/carellano/herdr-apps/internal/model"
)

const (
	processPollInterval  = 100 * time.Millisecond
	processLookupTimeout = time.Second
)

// ProcessClock is injectable to make the bounded graceful wait deterministic.
type ProcessClock interface {
	After(time.Duration) <-chan time.Time
}

// ProcessInspector reuses ProcessTable records and correlation identity semantics.
type ProcessInspector struct {
	Table    discovery.ProcessTable
	Clock    ProcessClock
	Interval time.Duration
	Timeout  time.Duration
}

func NewProcessInspector(table discovery.ProcessTable) ProcessInspector {
	return ProcessInspector{Table: table, Interval: processPollInterval, Timeout: processLookupTimeout}
}

func (i ProcessInspector) Inspect(ctx context.Context, pid int) (model.ProcessIdentity, error) {
	if i.Table == nil || pid <= 0 {
		return model.ProcessIdentity{}, fmt.Errorf("process inspection is unavailable")
	}
	timeout := i.Timeout
	if timeout <= 0 {
		timeout = processLookupTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	process, err := i.Table.Lookup(ctx, pid)
	if err != nil {
		return model.ProcessIdentity{}, err
	}
	if process.PID != pid {
		return model.ProcessIdentity{}, fmt.Errorf("process %d returned PID %d", pid, process.PID)
	}
	return correlation.LogicalIdentity(process), nil
}

// Wait returns when the original process incarnation disappears or changes.
func (i ProcessInspector) Wait(ctx context.Context, identity model.ProcessIdentity) error {
	interval := i.Interval
	if interval <= 0 {
		interval = processPollInterval
	}
	for {
		current, err := i.Inspect(ctx, identity.PID)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			return nil
		}
		if current != identity {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-i.after(interval):
		}
	}
}

func (i ProcessInspector) after(interval time.Duration) <-chan time.Time {
	if i.Clock != nil {
		return i.Clock.After(interval)
	}
	return time.After(interval)
}
