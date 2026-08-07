// Package proof provides a fail-closed, opt-in boundary for release evidence.
package proof

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/carellano/herdr-apps/internal/model"
)

var (
	ErrNotReady   = errors.New("proof evidence is not ready")
	ErrEvidence   = errors.New("proof evidence is incomplete or inconsistent")
	ErrLiveSafety = errors.New("live proof isolation is incomplete")
)

// Control identifies exactly the application expected from a controlled fixture.
type Control struct {
	Endpoint, WorkspaceID, TabID, PaneID string
	PID                                  int
}

// SelectControlled tolerates a transient null application list and rejects partial evidence.
func SelectControlled(apps []model.Application, want Control) (model.Application, error) {
	if apps == nil {
		return model.Application{}, ErrNotReady
	}
	for _, app := range apps {
		if matches(app, want) {
			return app, nil
		}
	}
	return model.Application{}, fmt.Errorf("%w: controlled endpoint=%q pid=%d workspace=%q tab=%q pane=%q was not found", ErrEvidence, want.Endpoint, want.PID, want.WorkspaceID, want.TabID, want.PaneID)
}

func matches(app model.Application, want Control) bool {
	if app.External || app.Identity.PID != want.PID || app.Association.WorkspaceID != want.WorkspaceID || app.Association.TabID != want.TabID || app.Association.PaneID != want.PaneID || app.Association.Confidence != model.ConfidenceHigh {
		return false
	}
	for _, endpoint := range app.Endpoints {
		if endpoint.URL == want.Endpoint {
			return true
		}
	}
	return false
}

// MetadataCapture is one captured workspace.report_metadata request.
type MetadataCapture struct {
	WorkspaceID string
	Values      map[string]string
}

// MatchMetadata preserves request order and requires every expected value for one workspace/key.
func MatchMetadata(captures []MetadataCapture, workspace, key string, want []string) error {
	got := make([]string, 0, len(want))
	for _, capture := range captures {
		if capture.WorkspaceID == workspace {
			if value, ok := capture.Values[key]; ok {
				got = append(got, value)
			}
		}
	}
	if len(got) != len(want) {
		return fmt.Errorf("%w: metadata workspace=%q key=%q captures=%v want=%v", ErrEvidence, workspace, key, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			return fmt.Errorf("%w: metadata workspace=%q key=%q capture %d=%q want %q", ErrEvidence, workspace, key, i, got[i], want[i])
		}
	}
	return nil
}

// ValidateTransitions accepts exactly semantic R1, R2, and R3 transitions.
func ValidateTransitions(snapshots []model.Snapshot) error {
	if len(snapshots) != 3 {
		return fmt.Errorf("%w: require R1, R2, and R3; got %d snapshots", ErrEvidence, len(snapshots))
	}
	for i := 1; i < len(snapshots); i++ {
		if snapshots[i-1].Revision >= snapshots[i].Revision || snapshots[i-1].SemanticallyEqual(snapshots[i]) {
			return fmt.Errorf("%w: R%d=%d to R%d=%d is not a semantic revision", ErrEvidence, i, snapshots[i-1].Revision, i+1, snapshots[i].Revision)
		}
	}
	return nil
}

// MatchExpected verifies controlled listener replacement and removal without accepting extras as proof.
func MatchExpected(apps []model.Application, present, absent []Control) error {
	for _, want := range present {
		if _, err := SelectControlled(apps, want); err != nil {
			return err
		}
	}
	for _, forbidden := range absent {
		for _, app := range apps {
			if matches(app, forbidden) {
				return fmt.Errorf("%w: removed controlled PID %d is still present", ErrEvidence, forbidden.PID)
			}
		}
	}
	return nil
}

// Cleanup records successful teardown once, so repeated defer paths remain bounded and auditable.
type Cleanup struct{ completed map[string]bool }

func (c *Cleanup) Record(name string) bool {
	if c.completed == nil {
		c.completed = map[string]bool{}
	}
	if c.completed[name] {
		return false
	}
	c.completed[name] = true
	return true
}
func (c Cleanup) Done(name string) bool { return c.completed[name] }

// LiveConfig contains every explicit isolation input required before an opt-in Darwin proof.
type LiveConfig struct {
	Invoke, FakeHerdr                       bool
	TempRoot, FakeHerdrSocket, PluginSocket string
}

// LiveEvidence is the durable receipt required from an explicitly invoked fake-Herdr runner.
type LiveEvidence struct {
	SocketBytes               map[string]int
	IDs                       Control
	ParentPID                 int
	Lsof                      string
	Polls                     int
	States                    []model.Snapshot
	Metadata, Events, Cleanup []string
	FinalStatus               string
}

// DarwinRunner is deliberately injected: go test never has a live runner to invoke.
type DarwinRunner interface {
	Run(context.Context, LiveConfig) (LiveEvidence, error)
}

// RunDarwin validates isolation and receipt completeness before returning runner evidence.
func RunDarwin(ctx context.Context, cfg LiveConfig, runner DarwinRunner) (LiveEvidence, error) {
	if !cfg.Invoke || !cfg.FakeHerdr || runner == nil || !tempSocket(cfg.TempRoot, cfg.FakeHerdrSocket) || !tempSocket(cfg.TempRoot, cfg.PluginSocket) || len([]byte(cfg.FakeHerdrSocket)) >= 104 || len([]byte(cfg.PluginSocket)) >= 104 {
		return LiveEvidence{}, fmt.Errorf("%w: require explicit fake Herdr, temp root, and sockets below 104 bytes", ErrLiveSafety)
	}
	evidence, err := runner.Run(ctx, cfg)
	if err != nil {
		return LiveEvidence{}, err
	}
	if len(evidence.SocketBytes) < 2 || evidence.IDs.PID <= 0 || evidence.ParentPID <= 0 || evidence.Lsof == "" || evidence.Polls == 0 || ValidateTransitions(evidence.States) != nil || len(evidence.Metadata) != 3 || len(evidence.Events) == 0 || len(evidence.Cleanup) == 0 || evidence.FinalStatus != "not running" {
		return LiveEvidence{}, fmt.Errorf("%w: receipt lacks socket, process, poll, revision, metadata, event, cleanup, or final status evidence", ErrEvidence)
	}
	return evidence, nil
}

func tempSocket(root, path string) bool {
	if root == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && rel != "." && rel != ".." && !filepath.IsAbs(rel)
}
