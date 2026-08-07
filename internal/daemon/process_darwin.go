//go:build darwin

package daemon

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type systemInspector struct{}

func NewSystemInspector() ProcessInspector { return systemInspector{} }
func (systemInspector) Identity(pid int) (ProcessIdentity, error) {
	out, err := exec.Command("ps", "-o", "pid=,lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ProcessIdentity{}, ErrProcessNotFound
	}
	fields := strings.Fields(string(out))
	if len(fields) < 6 {
		return ProcessIdentity{}, fmt.Errorf("parse process %d", pid)
	}
	got, _ := strconv.Atoi(fields[0])
	return ProcessIdentity{PID: got, StartTime: strings.Join(fields[1:], " ")}, nil
}
