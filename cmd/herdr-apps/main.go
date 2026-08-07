package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/carellano/herdr-apps/internal/cli"
	"github.com/carellano/herdr-apps/internal/config"
	"github.com/carellano/herdr-apps/internal/daemon"
	"github.com/carellano/herdr-apps/internal/discovery"
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
		return daemon.Run(ctx, paths, cfg, discovery.NewSystemScanner(), discovery.NewSystemProcessTable(), daemon.NewSystemInspector())
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
