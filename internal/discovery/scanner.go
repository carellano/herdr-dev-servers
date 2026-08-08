// Package discovery collects typed TCP listener and process observations.
package discovery

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
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

// ParseLinuxProcessStat extracts only stable process identity fields from /proc/<pid>/stat.
func ParseLinuxProcessStat(pid int, data []byte) (Process, error) {
	record := string(data)
	open, close := strings.IndexByte(record, '('), strings.LastIndexByte(record, ')')
	if pid <= 0 || open <= 0 || close <= open {
		return Process{}, fmt.Errorf("parse /proc/%d/stat: malformed record", pid)
	}
	parsedPID, err := strconv.Atoi(strings.TrimSpace(record[:open]))
	if err != nil || parsedPID != pid {
		return Process{}, fmt.Errorf("parse /proc/%d/stat: PID mismatch", pid)
	}
	fields := strings.Fields(record[close+1:])
	if len(fields) < 20 {
		return Process{}, fmt.Errorf("parse /proc/%d/stat: incomplete record", pid)
	}
	parent, err := strconv.Atoi(fields[1])
	if err != nil {
		return Process{}, fmt.Errorf("parse /proc/%d/stat: parent PID: %w", pid, err)
	}
	pgid, err := strconv.Atoi(fields[2])
	if err != nil || pgid <= 0 {
		return Process{}, fmt.Errorf("parse /proc/%d/stat: process group", pid)
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return Process{}, fmt.Errorf("parse /proc/%d/stat: start time: %w", pid, err)
	}
	return Process{PID: pid, ParentPID: parent, PGID: pgid, StartTime: fields[19]}, nil
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
	out, err := t.Runner.Run(ctx, "ps", "-o", "pid=,ppid=,pgid=,lstart=,comm=,args=", "-p", strconv.Itoa(pid))
	if err != nil {
		return Process{}, fmt.Errorf("read process %d: %w", pid, err)
	}
	process, err := ParseDarwinProcess(out)
	if err != nil {
		return Process{}, fmt.Errorf("parse process %d: %w", pid, err)
	}
	if process.PID != pid {
		return Process{}, fmt.Errorf("parse process %d: PID mismatch %d", pid, process.PID)
	}
	return process, nil
}

// ParseDarwinProcess parses the single unheaded record emitted by Darwin ps.
// lstart is normalized so discovery and destructive revalidation compare one token.
func ParseDarwinProcess(data []byte) (Process, error) {
	fields := strings.Fields(strings.TrimSpace(string(data)))
	if len(fields) < 10 {
		return Process{}, fmt.Errorf("incomplete ps record")
	}
	values := make([]int, 3)
	for index := range values {
		value, err := strconv.Atoi(fields[index])
		if err != nil {
			return Process{}, err
		}
		values[index] = value
	}
	start := strings.Join(fields[3:8], " ")
	if _, err := time.Parse("Mon Jan 2 15:04:05 2006", start); err != nil {
		return Process{}, fmt.Errorf("invalid start time: %w", err)
	}
	return Process{PID: values[0], ParentPID: values[1], PGID: values[2], StartTime: start, Executable: fields[8], Args: fields[9:]}, nil
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

// MergeLinuxTCPListeners parses available IPv4 and IPv6 proc tables, deduplicating shared sockets.
func MergeLinuxTCPListeners(tcp, tcp6 []byte, inodePID map[string]int) ([]Listener, error) {
	var result []Listener
	seen := map[Listener]bool{}
	for _, data := range [][]byte{tcp, tcp6} {
		if len(data) == 0 {
			continue
		}
		listeners, err := ParseLinuxTCPListeners(data, inodePID)
		if err != nil {
			return nil, err
		}
		for _, listener := range listeners {
			if !seen[listener] {
				seen[listener] = true
				result = append(result, listener)
			}
		}
	}
	return result, nil
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
	if len(parts) != 2 || (len(parts[0]) != 8 && len(parts[0]) != 32) {
		return "", 0, false
	}
	port64, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return "", 0, false
	}
	if len(parts[0]) == 32 {
		bytes := make(net.IP, net.IPv6len)
		for index := range bytes {
			part, err := strconv.ParseUint(parts[0][index*2:index*2+2], 16, 8)
			if err != nil {
				return "", 0, false
			}
			bytes[index^3] = byte(part)
		}
		return bytes.String(), int(port64), true
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
