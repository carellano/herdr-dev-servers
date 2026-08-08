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

	"github.com/carellano/herdr-apps/internal/actions"
	"github.com/carellano/herdr-apps/internal/adapter"
	"github.com/carellano/herdr-apps/internal/cli"
	"github.com/carellano/herdr-apps/internal/config"
	"github.com/carellano/herdr-apps/internal/daemon"
	"github.com/carellano/herdr-apps/internal/model"
	"github.com/carellano/herdr-apps/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	tuiRefreshInterval = 2 * time.Second
	tuiRefreshTimeout  = time.Second
)

const usage = `herdr-apps is the Herdr application discovery plugin.

	Usage:
	  herdr-apps daemon
	  herdr-apps ensure-watch
	  herdr-apps open
	  herdr-apps list [--json]
	  herdr-apps inspect <port>
	  herdr-apps doctor
	  herdr-apps tui
  herdr-apps help

The daemon owns application state. Live Herdr validation is required before release.
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
		return openApps(ctx, getenv, runCommand)
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
		paths, err := daemon.StatePaths()
		if err != nil {
			return err
		}
		client := daemon.Client{Socket: paths.Socket}
		snapshot, err := cli.List(ctx, client)
		if err != nil {
			return err
		}
		_, err = tea.NewProgram(ui.New(snapshot, func(ctx context.Context, key string, app model.Application, revision uint64, confirmed bool) (model.ActionResult, model.Snapshot, error) {
			return cli.ExecuteAction(ctx, client, key, app, revision, confirmed)
		}, ui.WithRefresh(func(ctx context.Context) (model.Snapshot, error) {
			return cli.List(ctx, client)
		}, tuiRefreshInterval, tuiRefreshTimeout))).Run()
		return err
	}
	return cli.Run(ctx, args, out)
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

func openApps(ctx context.Context, getenv func(string) string, run commandRunner) error {
	if getenv == nil || run == nil {
		return fmt.Errorf("open apps dependencies are incomplete")
	}
	path := getenv("HERDR_BIN_PATH")
	if path == "" {
		return fmt.Errorf("open apps: HERDR_BIN_PATH is required")
	}
	if strings.ContainsAny(path, "\x00\r\n") || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("open apps: HERDR_BIN_PATH is unsafe")
	}
	if err := run(ctx, path, "plugin", "pane", "open", "--plugin", "carellano.apps", "--entrypoint", "apps"); err != nil {
		return fmt.Errorf("open apps pane: %w", err)
	}
	return nil
}

func runCommand(ctx context.Context, path string, args ...string) error {
	return exec.CommandContext(ctx, path, args...).Run()
}
