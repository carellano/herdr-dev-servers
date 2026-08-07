package correlation

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"

	"github.com/carellano/herdr-apps/internal/discovery"
	"github.com/carellano/herdr-apps/internal/model"
)

// LogicalIdentity creates a stable launch description plus incarnation evidence.
func LogicalIdentity(process discovery.Process) model.ProcessIdentity {
	return LogicalIdentityWithAncestry(process, map[int]discovery.Process{process.PID: process})
}

// LogicalIdentityWithAncestry includes a stable ancestry-root description without treating PIDs as stable identity.
func LogicalIdentityWithAncestry(process discovery.Process, processes map[int]discovery.Process) model.ProcessIdentity {
	normalized := NormalizedCommand(process.Executable, process.Args)
	root := ancestryRoot(process, processes)
	logical := strings.Join([]string{process.Executable, normalized, filepath.Clean(process.CWD), filepath.Clean(process.Workspace), root.Executable, filepath.Clean(root.CWD)}, "\x00")
	digest := sha256.Sum256([]byte(logical))
	return model.ProcessIdentity{PID: process.PID, StartTime: process.StartTime, PGID: process.PGID, Key: hex.EncodeToString(digest[:])}
}

func ancestryRoot(process discovery.Process, processes map[int]discovery.Process) discovery.Process {
	seen := map[int]bool{}
	for process.ParentPID != 0 && !seen[process.PID] {
		seen[process.PID] = true
		parent, exists := processes[process.ParentPID]
		if !exists {
			break
		}
		process = parent
	}
	return process
}

func pathContainsSegment(parent, child string) bool {
	parent, child = filepath.Clean(parent), filepath.Clean(child)
	return child == parent || strings.HasPrefix(child, parent+string(filepath.Separator))
}
