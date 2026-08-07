package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/carellano/herdr-apps/internal/adapter"
	"github.com/carellano/herdr-apps/internal/cli"
	"github.com/carellano/herdr-apps/internal/config"
	"github.com/carellano/herdr-apps/internal/daemon"
	"github.com/carellano/herdr-apps/internal/model"
	"github.com/carellano/herdr-apps/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

const usage = `herdr-apps is the Herdr application discovery plugin.

Usage:
	  herdr-apps daemon
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
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := fmt.Fprint(out, usage)
		return err
	}
	ctx := context.Background()
	if args[0] == "daemon" {
		cfg, _, err := config.Load()
		if err != nil {
			return err
		}
		paths, err := daemon.StatePaths()
		if err != nil {
			return err
		}
		_ = cfg
		ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
		service := &daemon.Service{}
		runtime := adapter.NewFactory(adapter.DefaultSocket()).Runtime(service)
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
		snapshot, err := cli.List(ctx, daemon.Client{Socket: paths.Socket})
		if err != nil {
			return err
		}
		_, err = tea.NewProgram(ui.New(snapshot, func(string, model.Application) string {
			return "action unavailable: daemon action bridge is not configured"
		})).Run()
		return err
	}
	return cli.Run(ctx, args, out)
}
