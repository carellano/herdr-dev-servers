//go:build darwin

package discovery

import (
	"context"
	"os/exec"
)

type execRunner struct{}

func (execRunner) Run(ctx context.Context, argv ...string) ([]byte, error) {
	return exec.CommandContext(ctx, argv[0], argv[1:]...).Output()
}

// NewSystemScanner returns the supported Darwin listener adapter.
func NewSystemScanner() Scanner { return DarwinScanner{Runner: execRunner{}} }

// NewSystemProcessTable returns the supported Darwin process adapter.
func NewSystemProcessTable() ProcessTable { return DarwinProcessTable{Runner: execRunner{}} }
