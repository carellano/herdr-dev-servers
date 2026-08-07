// Package discovery collects typed TCP listener and process observations.
package discovery

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Listener is a TCP listener observation; it deliberately contains no inferred ownership.
type Listener struct {
	PID     int
	Port    int
	Address string
}

// Process is a process-table record used as evidence by correlation.
type Process struct {
	PID        int
	ParentPID  int
	StartTime  string
	PGID       int
	Executable string
	Args       []string
	CWD        string
	Workspace  string
}

// Scanner provides listener observations without exposing shell command strings.
type Scanner interface {
	Scan(context.Context) ([]Listener, error)
}

// PIDScanner can collect listeners for already-established process identities.
// Callers must never supply arbitrary host PIDs.
type PIDScanner interface {
	ScanPIDs(context.Context, []int) ([]Listener, error)
}

// ProcessTable supplies typed process evidence to correlation.
type ProcessTable interface {
	Lookup(context.Context, int) (Process, error)
}

// Runner is the narrow fixed-argv command boundary used by platform adapters.
type Runner interface {
	Run(context.Context, ...string) ([]byte, error)
}

// DarwinScanner invokes lsof with a fixed argv and parses its NUL-delimited fields.
// It is platform-neutral for deterministic parser tests; NewSystemScanner selects it only on Darwin.
type DarwinScanner struct{ Runner Runner }

// DarwinProcessTable invokes ps with fixed argv and parses a single process record.
// It is platform-neutral for deterministic parser tests; NewSystemProcessTable selects it only on Darwin.
type DarwinProcessTable struct{ Runner Runner }

func (t DarwinProcessTable) Lookup(ctx context.Context, pid int) (Process, error) {
	if t.Runner == nil {
		return Process{}, fmt.Errorf("Darwin process table has no command runner")
	}
	out, err := t.Runner.Run(ctx, "ps", "-o", "pid=,ppid=,pgid=,comm=,args=", "-p", strconv.Itoa(pid))
	if err != nil {
		return Process{}, fmt.Errorf("read process %d: %w", pid, err)
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) < 5 {
		return Process{}, fmt.Errorf("parse process %d: incomplete ps record", pid)
	}
	values := make([]int, 3)
	for index := range values {
		values[index], err = strconv.Atoi(fields[index])
		if err != nil {
			return Process{}, fmt.Errorf("parse process %d: %w", pid, err)
		}
	}
	return Process{PID: values[0], ParentPID: values[1], PGID: values[2], Executable: fields[3], Args: fields[4:]}, nil
}

func (s DarwinScanner) Scan(ctx context.Context) ([]Listener, error) {
	if s.Runner == nil {
		return nil, fmt.Errorf("Darwin scanner has no command runner")
	}
	out, err := s.Runner.Run(ctx, "lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-F0pcn")
	if err != nil {
		return nil, fmt.Errorf("run lsof listener scan: %w", err)
	}
	return ParseDarwinLsof(out)
}

// ScanPIDs collects listeners for the supplied process identities using fixed lsof argv.
func (s DarwinScanner) ScanPIDs(ctx context.Context, pids []int) ([]Listener, error) {
	if s.Runner == nil {
		return nil, fmt.Errorf("Darwin scanner has no command runner")
	}
	seen := make(map[int]struct{}, len(pids))
	var listeners []Listener
	for _, pid := range pids {
		if pid <= 0 {
			continue
		}
		if _, exists := seen[pid]; exists {
			continue
		}
		seen[pid] = struct{}{}
		out, err := s.Runner.Run(ctx, "lsof", "-nP", "-a", "-p", strconv.Itoa(pid), "-iTCP", "-sTCP:LISTEN", "-F0pcn")
		if err != nil {
			return nil, fmt.Errorf("run lsof listener scan for PID %d: %w", pid, err)
		}
		parsed, err := ParseDarwinLsof(out)
		if err != nil {
			return nil, err
		}
		listeners = append(listeners, parsed...)
	}
	return listeners, nil
}

// ParseDarwinLsof parses only the lsof fields requested by DarwinScanner.
func ParseDarwinLsof(data []byte) ([]Listener, error) {
	var listeners []Listener
	pid := 0
	for _, raw := range strings.Split(string(data), "\x00") {
		raw = strings.TrimLeft(raw, "\r\n")
		if len(raw) < 2 {
			continue
		}
		switch raw[0] {
		case 'p':
			value, err := strconv.Atoi(raw[1:])
			if err != nil {
				return nil, fmt.Errorf("parse lsof PID %q: %w", raw[1:], err)
			}
			pid = value
		case 'n':
			if pid == 0 {
				continue
			}
			name := strings.TrimSpace(strings.TrimSuffix(raw[1:], " (LISTEN)"))
			name = strings.TrimPrefix(name, "TCP ")
			address, port, ok := splitListenerAddress(name)
			if ok {
				listeners = append(listeners, Listener{PID: pid, Address: address, Port: port})
			}
		}
	}
	return listeners, nil
}

// ParseLinuxTCPListeners parses /proc/net/tcp fixture data after inode-to-PID resolution.
func ParseLinuxTCPListeners(data []byte, inodePID map[string]int) ([]Listener, error) {
	var listeners []Listener
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 10 || fields[3] != "0A" {
			continue
		}
		address, port, ok := parseProcAddress(fields[1])
		if !ok {
			return nil, fmt.Errorf("parse /proc listener %q", fields[1])
		}
		pid, exists := inodePID[fields[9]]
		if !exists {
			continue
		}
		listeners = append(listeners, Listener{PID: pid, Address: address, Port: port})
	}
	return listeners, nil
}

func splitListenerAddress(value string) (string, int, bool) {
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return "", 0, false
	}
	port, err := strconv.Atoi(portText)
	return host, port, err == nil && port > 0 && port <= 65535
}

func parseProcAddress(value string) (string, int, bool) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 || len(parts[0]) != 8 {
		return "", 0, false
	}
	port64, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return "", 0, false
	}
	bytes := make(net.IP, 4)
	for index := range bytes {
		part, err := strconv.ParseUint(parts[0][index*2:index*2+2], 16, 8)
		if err != nil {
			return "", 0, false
		}
		bytes[3-index] = byte(part)
	}
	return bytes.String(), int(port64), true
}
