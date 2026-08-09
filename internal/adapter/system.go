package adapter

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/carellano/herdr-dev-servers/internal/actions"
)

const systemActionTimeout = 2 * time.Second

type systemCommand interface {
	Start() error
	Run() error
	Release() error
	SetStdin(io.Reader)
	DiscardStdio()
}

type execCommand struct{ *exec.Cmd }

func (c execCommand) Release() error       { return c.Process.Release() }
func (c execCommand) SetStdin(r io.Reader) { c.Stdin = r }
func (c execCommand) DiscardStdio()        { c.Stdout, c.Stderr = io.Discard, io.Discard }
func newSystemCommand(ctx context.Context, path string, args ...string) systemCommand {
	return execCommand{exec.CommandContext(ctx, path, args...)}
}

// SystemRunner executes fixed desktop commands without a shell.
type SystemRunner struct {
	Command func(context.Context, string, ...string) systemCommand
}

func NewSystemRunner() *SystemRunner { return &SystemRunner{} }

func (r SystemRunner) Start(ctx context.Context, command actions.Command) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if invalidCommandPart(command.Path) {
		return fmt.Errorf("invalid command path")
	}
	for _, arg := range command.Args {
		if invalidCommandPart(arg) {
			return fmt.Errorf("invalid command argument")
		}
	}
	build := r.Command
	if build == nil {
		build = newSystemCommand
	}
	if command.Detach {
		// Do not let the action deadline terminate the desktop application.
		cmd := build(context.Background(), command.Path, command.Args...)
		cmd.DiscardStdio()
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Release()
	}
	cmd := build(ctx, command.Path, command.Args...)
	cmd.DiscardStdio()
	return cmd.Run()
}

func invalidCommandPart(value string) bool {
	if value == "" {
		return true
	}
	return strings.IndexFunc(value, func(r rune) bool { return r == 0 || r < 0x20 || r == 0x7f }) >= 0
}

// SystemClipboard writes through fixed system clipboard commands without terminal fallback.
type SystemClipboard struct {
	Platform string
	Timeout  time.Duration
	LookPath func(string) (string, error)
	Command  func(context.Context, string, ...string) systemCommand
	Getenv   func(string) string
}

func NewSystemClipboard(platform string) *SystemClipboard {
	return &SystemClipboard{Platform: platform}
}

func (c SystemClipboard) WriteText(text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout())
	defer cancel()
	path, args, err := c.commandFor()
	if err != nil {
		return err
	}
	build := c.Command
	if build == nil {
		build = newSystemCommand
	}
	cmd := build(ctx, path, args...)
	cmd.SetStdin(strings.NewReader(text))
	cmd.DiscardStdio()
	return cmd.Run()
}

func (c SystemClipboard) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return systemActionTimeout
}

func (c SystemClipboard) commandFor() (string, []string, error) {
	if c.Platform == "darwin" {
		return "pbcopy", nil, nil
	}
	if c.Platform != "linux" {
		return "", nil, fmt.Errorf("clipboard is unsupported on %q", c.Platform)
	}
	lookup := c.LookPath
	if lookup == nil {
		lookup = exec.LookPath
	}
	getenv := c.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	candidates := []struct {
		name string
		args []string
	}{{"xclip", []string{"-selection", "clipboard"}}, {"xsel", []string{"--clipboard", "--input"}}}
	if getenv("WAYLAND_DISPLAY") != "" {
		candidates = append([]struct {
			name string
			args []string
		}{{"wl-copy", nil}}, candidates...)
	}
	for _, candidate := range candidates {
		if path, err := lookup(candidate.name); err == nil {
			return path, candidate.args, nil
		}
	}
	return "", nil, fmt.Errorf("system clipboard command is unavailable")
}
