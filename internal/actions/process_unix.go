//go:build darwin || linux

package actions

import (
	"syscall"
)

const (
	SignalTERM Signal = Signal(syscall.SIGTERM)
	SignalKILL Signal = Signal(syscall.SIGKILL)
)

// UnixSignaler is the production exact-process and process-group signal adapter.
type UnixSignaler struct{}

func (UnixSignaler) SignalPID(pid int, signal Signal) error {
	return syscall.Kill(pid, syscall.Signal(signal))
}

func (UnixSignaler) SignalProcessGroup(pgid int, signal Signal) error {
	return syscall.Kill(-pgid, syscall.Signal(signal))
}
