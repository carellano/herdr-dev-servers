# Herdr Dev Servers Plugin

`herdr-dev-servers` discovers listening development servers and presents them through a daemon-owned, evidence-backed model for Herdr 0.8+ (protocol 19). It supports macOS and Linux; it does not require persistent plugin registration to build, validate, or run the CLI.

## Quick path

1. Build the binary: `go build ./cmd/herdr-dev-servers`.
2. When installed or linked through Herdr, startup and `pane.created` hooks automatically keep the daemon available; the daemon also reconciles independently every configured scan interval (five seconds by default).
3. Use `herdr-dev-servers list`, `herdr-dev-servers inspect <port>`, `herdr-dev-servers doctor`, or `herdr-dev-servers tui` in another terminal.

The daemon owns current development-server revisions. CLI and TUI clients only query that local authority.

## Install and manifest use

Requirements: Go 1.24+, Herdr 0.8+ / protocol 19, and macOS or Linux. The manifest is [`herdr-plugin.toml`](herdr-plugin.toml); it builds `./herdr-dev-servers`, starts it through one-shot startup and pane-created hooks, and declares relative action and pane commands. No LaunchAgent or systemd unit is required.

For local validation, keep the manifest in this checkout and invoke the binary directly. Do **not** copy it into a user or system plugin directory unless the host's installation process has been explicitly approved. This project never persistently registers itself.

```sh
go test ./...
go run ./cmd/herdr-dev-servers help
go run ./cmd/herdr-dev-servers doctor
```

## Commands

| Command | Purpose |
|---|---|
| `herdr-dev-servers daemon` | Starts the local authority, reconciles Herdr and listener observations, and publishes bounded workspace `$dev_servers` metadata as compact `:port` entries. |
| `herdr-dev-servers ensure-watch` | Checks the local IPC authority and, when unavailable, starts `daemon` and waits briefly for readiness. Herdr invokes this one-shot command automatically. |
| `herdr-dev-servers list [--json]` | Lists the latest daemon snapshot, including external listeners for diagnostics. |
| `herdr-dev-servers open` | Opens the configured `dev-servers` popup through Herdr. The visible `carellano.dev-servers.open` workspace action (bindable to `prefix+a`) invokes it. |
| `herdr-dev-servers inspect <port>` | Shows evidence for the development server that owns a listed port. |
| `herdr-dev-servers doctor` | Reports local daemon availability and Herdr socket reachability only; it does not check API compatibility. |
| `herdr-dev-servers tui` | Opens the interactive client against the existing daemon. |
| `herdr-dev-servers help` | Prints command usage. |

Set `HERDR_SOCKET_PATH` only when connecting the daemon to an isolated or alternate Herdr socket. Direct CLI commands use the same plugin state as Herdr: `$XDG_STATE_HOME/herdr/plugins/carellano.dev-servers`, or `$HOME/.local/state/herdr/plugins/carellano.dev-servers` when `XDG_STATE_HOME` is unset. Set `HERDR_PLUGIN_STATE_DIR` only to override that base exactly, such as for isolated tests.

## Configuration

`config.toml` supports only these active keys: `scan_interval_seconds`, `ignored_ports`, `opener`, and `clipboard`. Ignored listener ports are excluded before correlation and never reach the daemon snapshot, CLI, or TUI. The TUI hides external listeners; `list` and `list --json` retain them for diagnostics.

## Action safety

Actions are daemon-validated and resolve against the latest revision. Open and copy use fixed arguments; unsafe URLs, credentials, control bytes, and unavailable tools are refused. Focus reports either exact pane success, an explicit workspace/tab fallback warning, or unavailable.

TERM and force-kill are unavailable without high-confidence, owned process evidence and explicit confirmation. Before signaling, the daemon revalidates PID, start time, identity, and that the listener itself leads its process group; a changed, missing, or shared-group process receives no signal. Force-kill additionally requires a bounded grace expiry and a second confirmation/revalidation.

## Troubleshooting

| Symptom | Check |
|---|---|
| `doctor` reports Herdr unavailable | Start a compatible Herdr server or point `HERDR_SOCKET_PATH` at an isolated server socket. |
| `list` cannot reach the daemon | Run `herdr-dev-servers ensure-watch` or start `herdr-dev-servers daemon` for direct local validation; direct commands and Herdr share the plugin-state default, while an explicit `HERDR_PLUGIN_STATE_DIR` must match in both processes. |
| No development server is listed | Confirm a supported TCP listener and current Herdr pane/process evidence; unavailable or ambiguous evidence is shown as such rather than guessed. |
| `$dev_servers` is absent or unchanged | Metadata is bounded, stable-sorted, and suppressed when its digest has not changed; inspect daemon/Herdr diagnostics first. |

## Clean rollback and uninstall

1. Stop only the `herdr-dev-servers daemon` process you started.
2. Remove the local binary and any temporary checkout or approved manifest copy.
3. Remove only the plugin state directory selected by `HERDR_PLUGIN_STATE_DIR`, or the plugin-owned default state directory if it was created for this plugin.

The daemon cleans up only its nonce-matching socket, lock, and cache entries. Rollback does not terminate discovered applications, modify unrelated Herdr metadata, or remove host-managed plugin registrations.
