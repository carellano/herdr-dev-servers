//go:build darwin || linux

package actions

import (
	"syscall"
)

const (
	SignalTERM Signal = Signal(syscall.SIGTERM)
	SignalKILL Signal = Signal(syscall.SIGKILL)
)

// UnixSignaler is the production process-group signal adapter.
type UnixSignaler struct{}

func (UnixSignaler) SignalPGID(pgid int, signal Signal) error {
	return syscall.Kill(-pgid, syscall.Signal(signal))
}
