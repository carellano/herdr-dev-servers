package actions

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/carellano/herdr-dev-servers/internal/model"
)

func TestOpenRejectsUnsafeURLsWithoutLaunching(t *testing.T) {
	runner := &fakeRunner{}
	service := Service{Runner: runner, Platform: "linux", OpenTimeout: time.Second}
	for _, raw := range []string{"javascript:alert(1)", "https://user@example.test", "http://localhost\n--new-window", "http://example.test", "http://"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := service.Open(context.Background(), raw); err == nil {
				t.Fatal("Open() error = nil, want unsafe URL rejection")
			}
		})
	}
	if len(runner.commands) != 0 {
		t.Fatalf("unsafe URL launched commands: %#v", runner.commands)
	}
}

func TestOpenUsesFixedArgvAndBoundedDetachedRunner(t *testing.T) {
	runner := &fakeRunner{}
	service := Service{Runner: runner, Platform: "linux", OpenTimeout: time.Second}
	if _, err := service.Open(context.Background(), "http://localhost:3000"); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("commands = %d, want 1", len(runner.commands))
	}
	got := runner.commands[0]
	if got.Path != "xdg-open" || len(got.Args) != 1 || got.Args[0] != "http://localhost:3000" || !got.Detach {
		t.Fatalf("command = %#v, want fixed detached xdg-open argv", got)
	}
	if !got.HasDeadline {
		t.Fatal("detached launch did not receive a bounded context")
	}
}

func TestOpenReportsMissingBinaryWithoutFallback(t *testing.T) {
	runner := &fakeRunner{err: exec.ErrNotFound}
	service := Service{Runner: runner, Platform: "darwin", OpenTimeout: time.Second}
	if _, err := service.Open(context.Background(), "http://localhost:3000"); err == nil {
		t.Fatal("Open() error = nil, want missing binary error")
	}
	if len(runner.commands) != 1 || runner.commands[0].Path != "open" {
		t.Fatalf("commands = %#v, want one fixed open command", runner.commands)
	}
}

func TestCopyRefusesTerminalFallback(t *testing.T) {
	clipboard := &fakeClipboard{err: errors.New("clipboard tool missing")}
	service := Service{Clipboard: clipboard}
	result, err := service.Copy("http://localhost:3000")
	if err != nil {
		t.Fatalf("Copy() error = %v", err)
	}
	if result.Outcome != OutcomeUnavailable || result.Warning == "" {
		t.Fatalf("Copy() = %#v, want explicit unavailable result", result)
	}
	if result.TerminalOutput {
		t.Fatal("Copy() permitted terminal fallback")
	}
}

func TestCopyRejectsTerminalControlData(t *testing.T) {
	clipboard := &fakeClipboard{}
	service := Service{Clipboard: clipboard}
	if _, err := service.Copy("http://localhost:3000\x1b]52;unsafe"); err == nil {
		t.Fatal("Copy() error = nil, want control-data rejection")
	}
	if clipboard.writes != 0 {
		t.Fatalf("clipboard writes = %d, want 0", clipboard.writes)
	}
}

func TestFocusVerifiesExactPaneAndExplicitFallback(t *testing.T) {
	t.Run("exact pane", func(t *testing.T) {
		focus := &fakeFocus{current: FocusLocation{WorkspaceID: "w", TabID: "t", PaneID: "p"}}
		result := (Service{Focus: focus}).FocusApplication(model.Application{Association: model.Association{WorkspaceID: "w", TabID: "t", PaneID: "p", Confidence: model.ConfidenceHigh}})
		if result.Outcome != OutcomeExactPane || result.Warning != "" {
			t.Fatalf("FocusApplication() = %#v, want exact-pane", result)
		}
	})
	t.Run("workspace tab fallback", func(t *testing.T) {
		focus := &fakeFocus{current: FocusLocation{WorkspaceID: "w", TabID: "t"}}
		result := (Service{Focus: focus}).FocusApplication(model.Application{Association: model.Association{WorkspaceID: "w", TabID: "t", Confidence: model.ConfidencePartial}})
		if result.Outcome != OutcomeFallbackWorkspaceTab || result.Warning == "" {
			t.Fatalf("FocusApplication() = %#v, want explicit partial fallback", result)
		}
	})
	t.Run("failed verification never claims exact success", func(t *testing.T) {
		focus := &fakeFocus{current: FocusLocation{WorkspaceID: "other"}}
		result := (Service{Focus: focus}).FocusApplication(model.Application{Association: model.Association{WorkspaceID: "w", TabID: "t", PaneID: "p", Confidence: model.ConfidenceHigh}})
		if result.Outcome == OutcomeExactPane {
			t.Fatalf("FocusApplication() = %#v, claimed exact success", result)
		}
	})
}

type recordedCommand struct {
	Command
	HasDeadline bool
}

type fakeRunner struct {
	commands []recordedCommand
	err      error
}

func (f *fakeRunner) Start(ctx context.Context, command Command) error {
	_, hasDeadline := ctx.Deadline()
	f.commands = append(f.commands, recordedCommand{Command: command, HasDeadline: hasDeadline})
	return f.err
}

type fakeClipboard struct {
	err    error
	writes int
}

func (f *fakeClipboard) WriteText(string) error {
	f.writes++
	return f.err
}

type fakeFocus struct{ current FocusLocation }

func (f *fakeFocus) FocusPane(string, string, string) error { return nil }
func (f *fakeFocus) FocusWorkspaceTab(string, string) error { return nil }
func (f *fakeFocus) CurrentFocus() (FocusLocation, error)   { return f.current, nil }
