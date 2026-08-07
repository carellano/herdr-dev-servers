//go:build linux

package daemon

import (
	"os"
	"strconv"
	"strings"
)

type systemInspector struct{}

func NewSystemInspector() ProcessInspector { return systemInspector{} }
func (systemInspector) Identity(pid int) (ProcessIdentity, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return ProcessIdentity{}, ErrProcessNotFound
	}
	close := strings.LastIndexByte(string(data), ')')
	if close < 0 {
		return ProcessIdentity{}, ErrProcessNotFound
	}
	fields := strings.Fields(string(data)[close+1:])
	if len(fields) < 20 {
		return ProcessIdentity{}, ErrProcessNotFound
	}
	return ProcessIdentity{PID: pid, StartTime: fields[19]}, nil
}
