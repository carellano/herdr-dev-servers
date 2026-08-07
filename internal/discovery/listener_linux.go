//go:build linux

package discovery

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// LinuxScanner is a typed seam for /proc collection. Inode ownership must be supplied by a process adapter.
type LinuxScanner struct {
	ReadTCP  func() ([]byte, error)
	InodePID func() (map[string]int, error)
}

func (s LinuxScanner) Scan(context.Context) ([]Listener, error) {
	if s.ReadTCP == nil || s.InodePID == nil {
		return nil, fmt.Errorf("Linux scanner is not configured")
	}
	tcp, err := s.ReadTCP()
	if err != nil {
		return nil, err
	}
	inodes, err := s.InodePID()
	if err != nil {
		return nil, err
	}
	return ParseLinuxTCPListeners(tcp, inodes)
}

// NewSystemScanner returns the supported Linux /proc listener adapter.
func NewSystemScanner() Scanner {
	return LinuxScanner{ReadTCP: func() ([]byte, error) { return os.ReadFile("/proc/net/tcp") }, InodePID: inodePID}
}

// LinuxProcessTable is a typed seam for /proc process records.
type LinuxProcessTable struct{ Read func(int) (Process, error) }

func (t LinuxProcessTable) Lookup(_ context.Context, pid int) (Process, error) {
	if t.Read == nil {
		return Process{}, fmt.Errorf("Linux process table is not configured")
	}
	return t.Read(pid)
}

// NewSystemProcessTable returns the supported Linux /proc process adapter.
func NewSystemProcessTable() ProcessTable { return LinuxProcessTable{Read: readLinuxProcess} }

func inodePID() (map[string]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	result := map[string]int{}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || !entry.IsDir() {
			continue
		}
		fds, err := os.ReadDir(filepath.Join("/proc", entry.Name(), "fd"))
		if err != nil {
			continue
		}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join("/proc", entry.Name(), "fd", fd.Name()))
			if err != nil || !strings.HasPrefix(target, "socket:[") || !strings.HasSuffix(target, "]") {
				continue
			}
			result[strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")] = pid
		}
	}
	return result, nil
}

func readLinuxProcess(pid int) (Process, error) {
	base := filepath.Join("/proc", strconv.Itoa(pid))
	stat, err := os.ReadFile(filepath.Join(base, "stat"))
	if err != nil {
		return Process{}, err
	}
	fields := strings.Fields(string(stat))
	if len(fields) < 22 {
		return Process{}, fmt.Errorf("parse /proc/%d/stat: incomplete record", pid)
	}
	parent, err := strconv.Atoi(fields[3])
	if err != nil {
		return Process{}, err
	}
	pgid, err := strconv.Atoi(fields[4])
	if err != nil {
		return Process{}, err
	}
	cwd, err := os.Readlink(filepath.Join(base, "cwd"))
	if err != nil {
		return Process{}, err
	}
	executable, err := os.Readlink(filepath.Join(base, "exe"))
	if err != nil {
		return Process{}, err
	}
	cmdline, _ := os.ReadFile(filepath.Join(base, "cmdline"))
	args := strings.FieldsFunc(string(cmdline), func(r rune) bool { return r == 0 })
	return Process{PID: pid, ParentPID: parent, PGID: pgid, StartTime: fields[21], CWD: cwd, Executable: executable, Args: args}, nil
}
