// Package proof provides a fail-closed, opt-in boundary for release evidence.
package proof

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/carellano/herdr-dev-servers/internal/model"
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
	DynamicBinding                          bool
	TempRoot, FakeHerdrSocket, PluginSocket string
	Control, Replacement                    Control
	ParentPID                               int
	Binder                                  IdentityBinder
	PollTimeout, EventTimeout               time.Duration
	MetadataKey                             string
	MetadataValues                          []string
}

// IdentityBinding contains the actual identities created by a controlled fixture.
type IdentityBinding struct {
	ParentPID            int
	Initial, Replacement Control
}

// IdentityBinder binds fixture-created PIDs before fake Herdr or daemon evidence begins.
type IdentityBinder interface {
	Bind(context.Context, LiveConfig) (IdentityBinding, error)
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

// DarwinOps isolates all process, socket, IPC, and cleanup boundaries for an opt-in proof.
type DarwinOps interface {
	EnsureStopped(context.Context) error
	StartFakeHerdr(context.Context, LiveConfig) error
	StartParent(context.Context, LiveConfig) error
	StartDaemon(context.Context, LiveConfig) error
	Poll(context.Context) (model.Snapshot, error)
	Subscribe(context.Context) (<-chan model.Snapshot, error)
	Metadata(context.Context) ([]MetadataCapture, error)
	Lsof(context.Context) (string, error)
	Cleanup(context.Context) ([]string, error)
	Status(context.Context) (string, error)
}

// DurableDarwinRunner orchestrates a fake-Herdr proof through injected boundaries only.
type DurableDarwinRunner struct{ Ops DarwinOps }

func (r DurableDarwinRunner) Run(ctx context.Context, cfg LiveConfig) (evidence LiveEvidence, err error) {
	if r.Ops == nil {
		return LiveEvidence{}, fmt.Errorf("%w: Darwin operations are required", ErrLiveSafety)
	}
	var cleanup Cleanup
	defer func() {
		if !cleanup.Record("runner") {
			return
		}
		receipt, cleanupErr := r.Ops.Cleanup(context.Background())
		if cleanupErr != nil {
			if err == nil {
				err = cleanupErr
			}
			return
		}
		evidence.Cleanup = receipt
		if status, statusErr := r.Ops.Status(context.Background()); statusErr != nil {
			if err == nil {
				err = statusErr
			}
		} else {
			evidence.FinalStatus = status
		}
	}()
	if err = r.Ops.EnsureStopped(ctx); err != nil {
		return LiveEvidence{}, err
	}
	if cfg.DynamicBinding {
		if err = r.Ops.StartParent(ctx, cfg); err != nil {
			return LiveEvidence{}, err
		}
		if cfg, err = bindIdentities(ctx, cfg); err != nil {
			return LiveEvidence{}, err
		}
		for _, start := range []func(context.Context, LiveConfig) error{r.Ops.StartFakeHerdr, r.Ops.StartDaemon} {
			if err = start(ctx, cfg); err != nil {
				return LiveEvidence{}, err
			}
		}
	} else {
		for _, start := range []func(context.Context, LiveConfig) error{r.Ops.StartFakeHerdr, r.Ops.StartParent, r.Ops.StartDaemon} {
			if err = start(ctx, cfg); err != nil {
				return LiveEvidence{}, err
			}
		}
	}
	var first model.Snapshot
	pollCtx, cancel := context.WithTimeout(ctx, cfg.PollTimeout)
	defer cancel()
	for {
		first, err = r.Ops.Poll(pollCtx)
		if err == nil {
			_, err = SelectControlled(first.Applications, cfg.Control)
		}
		if err == nil {
			break
		}
		if !errors.Is(err, ErrNotReady) {
			return LiveEvidence{}, err
		}
		if pollCtx.Err() != nil {
			return LiveEvidence{}, pollCtx.Err()
		}
	}
	events, err := r.Ops.Subscribe(ctx)
	if err != nil {
		return LiveEvidence{}, err
	}
	eventCtx, stopEvents := context.WithTimeout(ctx, cfg.EventTimeout)
	defer stopEvents()
	states := []model.Snapshot{first}
	for len(states) < 3 {
		select {
		case snapshot, ok := <-events:
			if !ok {
				return LiveEvidence{}, fmt.Errorf("%w: subscription ended early", ErrEvidence)
			}
			states = append(states, snapshot)
		case <-eventCtx.Done():
			return LiveEvidence{}, eventCtx.Err()
		}
	}
	if err = MatchExpected(states[1].Applications, []Control{cfg.Replacement}, []Control{cfg.Control}); err != nil {
		return LiveEvidence{}, err
	}
	if err = MatchExpected(states[2].Applications, nil, []Control{cfg.Replacement}); err != nil {
		return LiveEvidence{}, err
	}
	stable, err := r.Ops.Poll(ctx)
	if err != nil || stable.Revision != states[2].Revision || !stable.SemanticallyEqual(states[2]) {
		if err == nil {
			err = fmt.Errorf("%w: removal revision was not stable", ErrEvidence)
		}
		return LiveEvidence{}, err
	}
	captures, err := r.Ops.Metadata(ctx)
	if err != nil {
		return LiveEvidence{}, err
	}
	if err = MatchMetadata(captures, cfg.Control.WorkspaceID, cfg.MetadataKey, cfg.MetadataValues); err != nil {
		return LiveEvidence{}, err
	}
	lsof, err := r.Ops.Lsof(ctx)
	if err != nil {
		return LiveEvidence{}, err
	}
	return LiveEvidence{SocketBytes: map[string]int{"fake": len(cfg.FakeHerdrSocket), "plugin": len(cfg.PluginSocket)}, IDs: cfg.Control, ParentPID: cfg.ParentPID, Lsof: lsof, Polls: 2, States: states, Metadata: cfg.MetadataValues, Events: []string{"replacement", "removal"}}, nil
}

// RunDarwin validates isolation and receipt completeness before returning runner evidence.
func RunDarwin(ctx context.Context, cfg LiveConfig, runner DarwinRunner) (LiveEvidence, error) {
	if !cfg.Invoke || !cfg.FakeHerdr || runner == nil || !tempSocket(cfg.TempRoot, cfg.FakeHerdrSocket) || !tempSocket(cfg.TempRoot, cfg.PluginSocket) || len([]byte(cfg.FakeHerdrSocket)) >= 104 || len([]byte(cfg.PluginSocket)) >= 104 || !hasIdentity(cfg.Control) || cfg.PollTimeout <= 0 || cfg.EventTimeout <= 0 || cfg.MetadataKey == "" || len(cfg.MetadataValues) != 3 || (cfg.DynamicBinding && (cfg.Binder == nil || cfg.Control.PID != 0 || cfg.Replacement.PID != 0 || cfg.ParentPID != 0)) || (!cfg.DynamicBinding && (cfg.Control.PID <= 0 || cfg.Replacement.PID <= 0 || cfg.ParentPID <= 0)) {
		return LiveEvidence{}, fmt.Errorf("%w: require fake Herdr, temp sockets, controlled identities, metadata, and deadlines", ErrLiveSafety)
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

func bindIdentities(ctx context.Context, cfg LiveConfig) (LiveConfig, error) {
	binding, err := cfg.Binder.Bind(ctx, cfg)
	if err != nil {
		return LiveConfig{}, fmt.Errorf("%w: bind controlled identities: %v", ErrLiveSafety, err)
	}
	if !sameIdentity(cfg.Control, binding.Initial) || !sameIdentity(cfg.Control, binding.Replacement) || !distinctPositive(binding.ParentPID, binding.Initial.PID, binding.Replacement.PID) {
		return LiveConfig{}, fmt.Errorf("%w: bound identities drift or conflict", ErrLiveSafety)
	}
	cfg.ParentPID, cfg.Control, cfg.Replacement = binding.ParentPID, binding.Initial, binding.Replacement
	return cfg, nil
}

func sameIdentity(want, got Control) bool {
	return hasIdentity(want) && want.Endpoint == got.Endpoint && want.WorkspaceID == got.WorkspaceID && want.TabID == got.TabID && want.PaneID == got.PaneID
}

func hasIdentity(control Control) bool {
	return control.Endpoint != "" && control.WorkspaceID != "" && control.TabID != "" && control.PaneID != ""
}

func distinctPositive(pids ...int) bool {
	seen := map[int]bool{}
	for _, pid := range pids {
		if pid <= 0 || seen[pid] {
			return false
		}
		seen[pid] = true
	}
	return true
}

// RunDarwinInvocation is an explicit opt-in surface for a later injected live-proof runner.
// It deliberately has no command registration, so ordinary binary use and go test cannot launch it.
func RunDarwinInvocation(ctx context.Context, args []string, runner DarwinRunner) (LiveEvidence, error) {
	fs := flag.NewFlagSet("proof-darwin", flag.ContinueOnError)
	fs.SetOutput(new(strings.Builder))
	var cfg LiveConfig
	fs.BoolVar(&cfg.Invoke, "invoke", false, "permit proof execution")
	fs.BoolVar(&cfg.FakeHerdr, "fake-herdr", false, "require fake Herdr")
	fs.StringVar(&cfg.TempRoot, "temp-root", "", "temporary root")
	fs.StringVar(&cfg.FakeHerdrSocket, "fake-socket", "", "fake Herdr socket")
	fs.StringVar(&cfg.PluginSocket, "plugin-socket", "", "plugin socket")
	fs.StringVar(&cfg.Control.Endpoint, "endpoint", "", "controlled endpoint")
	fs.IntVar(&cfg.Control.PID, "pid", 0, "controlled listener PID")
	fs.StringVar(&cfg.Control.WorkspaceID, "workspace", "", "controlled workspace")
	fs.StringVar(&cfg.Control.TabID, "tab", "", "controlled tab")
	fs.StringVar(&cfg.Control.PaneID, "pane", "", "controlled pane")
	fs.IntVar(&cfg.Replacement.PID, "replacement-pid", 0, "replacement listener PID")
	fs.IntVar(&cfg.ParentPID, "parent-pid", 0, "controlled parent PID")
	fs.DurationVar(&cfg.PollTimeout, "poll-timeout", 0, "readiness deadline")
	fs.DurationVar(&cfg.EventTimeout, "event-timeout", 0, "event deadline")
	fs.StringVar(&cfg.MetadataKey, "metadata-key", "dev_servers", "metadata key")
	metadata := fs.String("metadata-values", "", "three comma-separated metadata values")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		if err == nil {
			err = fmt.Errorf("unexpected proof-darwin arguments")
		}
		return LiveEvidence{}, err
	}
	cfg.Replacement.Endpoint, cfg.Replacement.WorkspaceID, cfg.Replacement.TabID, cfg.Replacement.PaneID = cfg.Control.Endpoint, cfg.Control.WorkspaceID, cfg.Control.TabID, cfg.Control.PaneID
	cfg.MetadataValues = strings.Split(*metadata, ",")
	return RunDarwin(ctx, cfg, runner)
}

func tempSocket(root, path string) bool {
	if root == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && rel != "." && rel != ".." && !filepath.IsAbs(rel)
}
