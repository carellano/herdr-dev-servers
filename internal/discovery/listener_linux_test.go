//go:build linux

package discovery

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestLinuxScannerUsesAvailableTCPTable(t *testing.T) {
	fixture := []byte("  0: 00000000000000000000000001000000:0BB8 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 42 1\n")
	scanner := LinuxScanner{
		ReadTCP:  func() ([]byte, error) { return nil, errors.New("denied") },
		ReadTCP6: func() ([]byte, error) { return fixture, nil },
		InodePID: func() (map[string]int, error) { return map[string]int{"1": 7}, nil },
	}
	listeners, err := scanner.Scan(context.Background())
	if err != nil || !reflect.DeepEqual(listeners, []Listener{{PID: 7, Address: "::1", Port: 3000}}) {
		t.Fatalf("listeners=%#v err=%v", listeners, err)
	}
}
