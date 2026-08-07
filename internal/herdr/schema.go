// Package herdr defines conservative contracts for the unvalidated Herdr 0.8+ protocol.
package herdr

import "encoding/json"

const (
	RequiredProtocol = 19
	RequiredSchema   = 1
)

// Capabilities is the minimally required portion of a Herdr compatibility reply.
// Unknown fields remain in Raw so newer servers do not fail decoding by default.
type Capabilities struct {
	Protocol int             `json:"protocol"`
	Schema   int             `json:"schema"`
	Raw      json.RawMessage `json:"-"`
}

// SnapshotResponse preserves the server snapshot without guessing at evolving fields.
type SnapshotResponse struct {
	Snapshot json.RawMessage `json:"snapshot"`
}
type ProcessInfoRequest struct {
	PaneID string `json:"pane_id,omitempty"`
}
type ProcessInfoResponse struct {
	PaneID                   string        `json:"pane_id"`
	ShellPID                 int           `json:"shell_pid"`
	ForegroundProcessGroupID int           `json:"foreground_process_group_id"`
	ForegroundProcesses      []ProcessInfo `json:"foreground_processes"`
}
type ProcessInfo struct {
	PID     int    `json:"pid"`
	Command string `json:"command,omitempty"`
	CWD     string `json:"cwd,omitempty"`
}
type Subscription struct {
	Type   string `json:"type"`
	PaneID string `json:"pane_id,omitempty"`
}
type SubscriptionRequest struct {
	Subscriptions []Subscription `json:"subscriptions"`
}
type MetadataRequest struct {
	WorkspaceID string             `json:"workspace_id"`
	Source      string             `json:"source"`
	Tokens      map[string]*string `json:"tokens"`
	TTLMS       int                `json:"ttl_ms,omitempty"`
}

// Event retains the raw envelope so unknown event types remain forward-compatible.
type Event struct {
	Type string          `json:"type"`
	Raw  json.RawMessage `json:"-"`
}

// Compatible reports only the protocol/schema gate inferred from static 0.8 evidence.
func (c Capabilities) Compatible() bool {
	return c.Protocol >= RequiredProtocol && c.Schema == RequiredSchema
}
