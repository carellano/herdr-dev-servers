// Package cli renders explicit daemon and dependency diagnostics.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/carellano/herdr-apps/internal/config"
	"github.com/carellano/herdr-apps/internal/daemon"
	"github.com/carellano/herdr-apps/internal/model"
)

type Client interface {
	Request(context.Context, model.IPCRequest) (model.IPCResponse, error)
}

type socketProbe func(string) error

// ExecuteAction sends a daemon-owned action intent, then refreshes from that authority.
func ExecuteAction(ctx context.Context, client Client, key string, app model.Application, revision uint64, confirmed bool) (model.ActionResult, model.Snapshot, error) {
	action := map[string]string{"enter": "focus", "f": "focus", "o": "open", "c": "copy", "t": "terminate", "K": "kill"}[key]
	if action == "" {
		return model.ActionResult{}, model.Snapshot{}, fmt.Errorf("unsupported action %q", key)
	}
	response, err := client.Request(ctx, model.IPCRequest{Version: daemon.IPCVersion, RequestID: "action", ObservedRevision: revision, Method: "action", Action: action, Target: app.ID, Identity: app.Identity, Confirmed: confirmed})
	if err != nil {
		return model.ActionResult{}, model.Snapshot{}, err
	}
	data, _ := json.Marshal(response.Result)
	var result model.ActionResult
	if err := json.Unmarshal(data, &result); err != nil {
		return model.ActionResult{}, model.Snapshot{}, err
	}
	snapshot, err := List(ctx, client)
	return result, snapshot, err
}

func RenderList(snapshot model.Snapshot, jsonOutput bool) (string, error) {
	if jsonOutput {
		data, err := json.MarshalIndent(snapshot.Applications, "", "  ")
		return string(data) + "\n", err
	}
	var lines []string
	for _, app := range snapshot.Applications {
		lines = append(lines, fmt.Sprintf("%s\t%s\t%d endpoints", app.ID, app.Association.Confidence, len(app.Endpoints)))
	}
	return strings.Join(lines, "\n") + "\n", nil
}
func Inspect(ctx context.Context, client Client, id string) (model.Application, error) {
	response, err := client.Request(ctx, model.IPCRequest{Version: daemon.IPCVersion, RequestID: "inspect", Method: "inspect", Target: id})
	if err != nil {
		return model.Application{}, err
	}
	data, _ := json.Marshal(response.Result)
	var app model.Application
	return app, json.Unmarshal(data, &app)
}
func List(ctx context.Context, client Client) (model.Snapshot, error) {
	response, err := client.Request(ctx, model.IPCRequest{Version: daemon.IPCVersion, RequestID: "list", Method: "list"})
	if err != nil {
		return model.Snapshot{}, err
	}
	data, _ := json.Marshal(response.Result)
	var snapshot model.Snapshot
	return snapshot, json.Unmarshal(data, &snapshot)
}
func Doctor(paths daemon.Paths, cfg config.Config) string {
	return doctor(paths, cfg, herdrSocket(), probeHerdrSocket)
}

func doctor(paths daemon.Paths, cfg config.Config, socket string, probe socketProbe) string {
	checks := []string{"plugin config: valid", "scanner: platform adapter configured", "clipboard: " + cfg.Clipboard, "opener: " + cfg.Opener, fmt.Sprintf("sidebar: %d..%d", cfg.SidebarMin, cfg.SidebarMax)}
	if _, err := os.Stat(paths.Socket); err == nil {
		checks = append(checks, "daemon socket: available")
	} else {
		checks = append(checks, "daemon socket: unavailable (start `herdr-apps daemon`)")
	}
	if err := probe(socket); err == nil {
		checks = append(checks, "Herdr API: reachable; live schema validation pending")
	} else {
		checks = append(checks, "Herdr API: unavailable; live checks not claimed")
	}
	return strings.Join(checks, "\n") + "\n"
}

func herdrSocket() string {
	if socket := os.Getenv("HERDR_SOCKET_PATH"); socket != "" {
		return socket
	}
	return filepath.Join(os.Getenv("HOME"), ".config", "herdr", "herdr.sock")
}

func probeHerdrSocket(socket string) error {
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return err
	}
	return conn.Close()
}

func Run(ctx context.Context, args []string, out io.Writer) error {
	cfg, _, err := config.Load()
	if err != nil {
		return err
	}
	paths, err := daemon.StatePaths()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: daemon | list [--json] | inspect <port> | doctor")
	}
	if args[0] == "doctor" {
		_, err = io.WriteString(out, Doctor(paths, cfg))
		return err
	}
	if args[0] == "daemon" {
		return fmt.Errorf("daemon must be started by the command entrypoint")
	}
	client := daemon.Client{Socket: paths.Socket}
	switch args[0] {
	case "list":
		snapshot, err := List(ctx, client)
		if err != nil {
			return err
		}
		text, err := RenderList(snapshot, len(args) > 1 && args[1] == "--json")
		if err != nil {
			return err
		}
		_, err = io.WriteString(out, text)
		return err
	case "inspect":
		if len(args) != 2 {
			return fmt.Errorf("inspect requires a port")
		}
		port, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("inspect port: %w", err)
		}
		snapshot, err := List(ctx, client)
		if err != nil {
			return err
		}
		id := ""
		for _, candidate := range snapshot.Applications {
			for _, endpoint := range candidate.Endpoints {
				if endpoint.Port == port {
					id = candidate.ID
					break
				}
			}
		}
		if id == "" {
			return fmt.Errorf("port %d is unavailable", port)
		}
		app, err := Inspect(ctx, client, id)
		if err != nil {
			return err
		}
		data, _ := json.MarshalIndent(app, "", "  ")
		_, err = fmt.Fprintln(out, string(data))
		return err
	}
	return fmt.Errorf("unknown command %q", args[0])
}
