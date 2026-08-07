// Package model defines the daemon-owned, immutable application graph.
package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// Confidence describes how strongly the current evidence supports an association.
type Confidence string

const (
	ConfidenceHigh    Confidence = "high"
	ConfidencePartial Confidence = "partial"
	ConfidenceUnknown Confidence = "unknown"
)

// Evidence records an observation and any reason it cannot be fully relied upon.
type Evidence struct {
	Source      string    `json:"source"`
	ObservedAt  time.Time `json:"observedAt"`
	Fresh       bool      `json:"fresh"`
	Unavailable string    `json:"unavailable,omitempty"`
	ShellPID    int       `json:"shellPid,omitempty"`
	PGID        int       `json:"pgid,omitempty"`
	Argv        []string  `json:"argv,omitempty"`
	CWD         string    `json:"cwd,omitempty"`
	Ancestry    []int     `json:"ancestry,omitempty"`
}

// ProcessIdentity distinguishes a process incarnation from a recycled PID.
type ProcessIdentity struct {
	PID       int    `json:"pid"`
	StartTime string `json:"startTime"`
	PGID      int    `json:"pgid"`
	Key       string `json:"key"`
}

// Endpoint is a listener exposed by an application.
type Endpoint struct {
	Port int    `json:"port"`
	URL  string `json:"url,omitempty"`
}

// Association is the evidence-backed Herdr location of an application.
type Association struct {
	WorkspaceID    string     `json:"workspaceId,omitempty"`
	WorkspaceLabel string     `json:"workspaceLabel,omitempty"`
	TabID          string     `json:"tabId,omitempty"`
	TabLabel       string     `json:"tabLabel,omitempty"`
	PaneID         string     `json:"paneId,omitempty"`
	PaneLabel      string     `json:"paneLabel,omitempty"`
	Confidence     Confidence `json:"confidence"`
	Stale          bool       `json:"stale"`
}

// Application is one revisioned, daemon-owned listening application.
type Application struct {
	ID          string          `json:"id"`
	Identity    ProcessIdentity `json:"identity"`
	Endpoints   []Endpoint      `json:"endpoints"`
	Association Association     `json:"association"`
	Evidence    []Evidence      `json:"evidence"`
	External    bool            `json:"external"`
}

// Snapshot is a complete immutable view of the graph at a monotonically increasing revision.
type Snapshot struct {
	Revision     uint64        `json:"revision"`
	Applications []Application `json:"applications"`
	ObservedAt   time.Time     `json:"observedAt"`
}

// SemanticDigest excludes observation timestamps and revision so unchanged applications do not churn.
func (s Snapshot) SemanticDigest() string {
	s.Revision, s.ObservedAt = 0, time.Time{}
	s.Applications = append([]Application(nil), s.Applications...)
	for i := range s.Applications {
		s.Applications[i].Endpoints = append([]Endpoint(nil), s.Applications[i].Endpoints...)
		s.Applications[i].Evidence = append([]Evidence(nil), s.Applications[i].Evidence...)
		for j := range s.Applications[i].Evidence {
			s.Applications[i].Evidence[j].ObservedAt = time.Time{}
			s.Applications[i].Evidence[j].Argv = append([]string(nil), s.Applications[i].Evidence[j].Argv...)
			s.Applications[i].Evidence[j].Ancestry = append([]int(nil), s.Applications[i].Evidence[j].Ancestry...)
		}
	}
	encoded, _ := json.Marshal(s)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// SemanticallyEqual reports whether snapshots differ only by sampling timestamps or revision.
func (s Snapshot) SemanticallyEqual(other Snapshot) bool {
	return s.SemanticDigest() == other.SemanticDigest()
}

// IPCRequest is the versioned request envelope used by plugin-local JSONL IPC.
type IPCRequest struct {
	Version          int    `json:"version"`
	RequestID        string `json:"requestId"`
	ObservedRevision uint64 `json:"observedRevision"`
	Method           string `json:"method"`
	Target           string `json:"target,omitempty"`
}

// IPCError is a typed IPC error.
type IPCError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// IPCResponse is the versioned response envelope used by plugin-local JSONL IPC.
type IPCResponse struct {
	Version   int       `json:"version"`
	RequestID string    `json:"requestId"`
	Revision  uint64    `json:"revision"`
	Result    any       `json:"result,omitempty"`
	Error     *IPCError `json:"error,omitempty"`
}
