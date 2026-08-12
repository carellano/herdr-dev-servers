//go:build !darwin && !linux

package actions

import "fmt"

const (
	SignalTERM Signal = 15
	SignalKILL Signal = 9
)

// UnsupportedSignaler makes unsupported-platform action wiring explicit.
type UnsupportedSignaler struct{}

func (UnsupportedSignaler) SignalPID(int, Signal) error {
	return fmt.Errorf("process signaling is unavailable on this platform")
}

func (UnsupportedSignaler) SignalProcessGroup(int, Signal) error {
	return fmt.Errorf("process signaling is unavailable on this platform")
}
