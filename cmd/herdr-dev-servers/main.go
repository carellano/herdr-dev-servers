package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/carellano/herdr-dev-servers/internal/actions"
	"github.com/carellano/herdr-dev-servers/internal/adapter"
	"github.com/carellano/herdr-dev-servers/internal/cli"
	"github.com/carellano/herdr-dev-servers/internal/config"
	"github.com/carellano/herdr-dev-servers/internal/daemon"
	"github.com/carellano/herdr-dev-servers/internal/model"
	"github.com/carellano/herdr-dev-servers/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	tuiRefreshInterval = 2 * time.Second
	tuiRefreshTimeout  = time.Second
)

const usage = `herdr-dev-servers is the Herdr development-server discovery plugin.

	Usage:
	  herdr-dev-servers daemon
	  herdr-dev-servers ensure-watch
	  herdr-dev-servers open
	  herdr-dev-servers list [--json]
	  herdr-dev-servers inspect <port>
	  herdr-dev-servers doctor
	  herdr-dev-servers tui
	  herdr-dev-servers help

The daemon owns development-server state. Live Herdr validation is required before release.
`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	return runWith(args, out, daemon.EnsureWatch, os.Getenv, runCommand)
}

func runWithEnsure(args []string, out io.Writer, ensureWatch func(context.Context) error) error {
	return runWith(args, out, ensureWatch, os.Getenv, runCommand)
}

type commandRunner func(context.Context, string, ...string) error

func runWith(args []string, out io.Writer, ensureWatch func(context.Context) error, getenv func(string) string, runCommand commandRunner) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := fmt.Fprint(out, usage)
		return err
	}
	ctx := context.Background()
	if args[0] == "ensure-watch" {
		return ensureWatch(ctx)
	}
	if args[0] == "open" {
		return openDevServers(ctx, ensureWatch, getenv, runCommand)
	}
	if args[0] == "daemon" {
		cfg, _, err := config.Load()
		if err != nil {
			return err
		}
		paths, err := daemon.StatePaths()
		if err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
		factory := adapter.NewFactory(adapter.DefaultSocket())
		factory.Ignored = cfg.Ignored
		service := newDaemonService(factory, cfg)
		factory.Interval = cfg.Interval()
		runtime := factory.Runtime(service)
		errs := make(chan error, 2)
		go func() { errs <- daemon.Serve(ctx, paths, service, daemon.NewSystemInspector()) }()
		go func() { errs <- runtime.Run(ctx) }()
		err = <-errs
		stop()
		return err
	}
	if args[0] == "tui" {
		return runTUI(ctx, ensureWatch, daemon.StatePaths, func(ctx context.Context, client daemon.Client) (model.Snapshot, error) {
			return cli.List(ctx, client)
		}, func(snapshot model.Snapshot, client daemon.Client) error {
			_, err := tea.NewProgram(ui.New(snapshot, func(ctx context.Context, key string, app model.Application, revision uint64, confirmed bool) (model.ActionResult, model.Snapshot, error) {
				return cli.ExecuteAction(ctx, client, key, app, revision, confirmed)
			}, ui.WithRefresh(func(ctx context.Context) (model.Snapshot, error) {
				return cli.List(ctx, client)
			}, tuiRefreshInterval, tuiRefreshTimeout))).Run()
			return err
		})
	}
	return cli.Run(ctx, args, out)
}

func runTUI(ctx context.Context, ensureWatch func(context.Context) error, statePaths func() (daemon.Paths, error), list func(context.Context, daemon.Client) (model.Snapshot, error), launch func(model.Snapshot, daemon.Client) error) error {
	if err := ensureWatch(ctx); err != nil {
		return fmt.Errorf("start dev servers daemon for tui: %w", err)
	}
	paths, err := statePaths()
	if err != nil {
		return err
	}
	client := daemon.Client{Socket: paths.Socket}
	snapshot, err := list(ctx, client)
	if err != nil {
		return err
	}
	return launch(snapshot, client)
}

func newDaemonService(factory adapter.Factory, cfg config.Config) *daemon.Service {
	service := actions.Service{Platform: runtime.GOOS, Focus: factory.Focus}
	service.Processes, service.Signaler = productionProcessActions(factory)
	if cfg.Opener == "system" {
		service.Runner = adapter.NewSystemRunner()
	}
	if cfg.Clipboard == "system" {
		service.Clipboard = adapter.NewSystemClipboard(runtime.GOOS)
	}
	return &daemon.Service{Actions: actions.NewExecutor(service)}
}

func openDevServers(ctx context.Context, ensureWatch func(context.Context) error, getenv func(string) string, run commandRunner) error {
	if ensureWatch == nil || getenv == nil || run == nil {
		return fmt.Errorf("open dev servers dependencies are incomplete")
	}
	path := getenv("HERDR_BIN_PATH")
	if path == "" {
		return fmt.Errorf("open dev servers: HERDR_BIN_PATH is required")
	}
	if strings.ContainsAny(path, "\x00\r\n") || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("open dev servers: HERDR_BIN_PATH is unsafe")
	}
	if err := ensureWatch(ctx); err != nil {
		return fmt.Errorf("start dev servers daemon before opening pane: %w", err)
	}
	if err := run(ctx, path, "plugin", "pane", "open", "--plugin", "carellano.dev-servers", "--entrypoint", "dev-servers"); err != nil {
		return fmt.Errorf("open dev servers pane: %w", err)
	}
	return nil
}

func runCommand(ctx context.Context, path string, args ...string) error {
	return exec.CommandContext(ctx, path, args...).Run()
}
