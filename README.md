# Herdr Apps Plugin

`herdr-apps` discovers listening development applications and presents them through a daemon-owned, evidence-backed model for Herdr 0.8+ (protocol 19). It supports macOS and Linux; it does not require persistent plugin registration to build, validate, or run the CLI.

## Quick path

1. Build the binary: `go build ./cmd/herdr-apps`.
2. When installed or linked through Herdr, startup and `pane.created` hooks automatically keep the daemon available; the daemon also reconciles independently every configured scan interval (five seconds by default).
3. Use `herdr-apps list`, `herdr-apps inspect <port>`, `herdr-apps doctor`, or `herdr-apps tui` in another terminal.

The daemon owns current application revisions. CLI and TUI clients only query that local authority.

## Install and manifest use

Requirements: Go 1.24+, Herdr 0.8+ / protocol 19, and macOS or Linux. The manifest is [`herdr-plugin.toml`](herdr-plugin.toml); it builds `./herdr-apps`, starts it through one-shot startup and pane-created hooks, and declares relative action and pane commands. No LaunchAgent or systemd unit is required.

For local validation, keep the manifest in this checkout and invoke the binary directly. Do **not** copy it into a user or system plugin directory unless the host's installation process has been explicitly approved. This project never persistently registers itself.

```sh
go test ./...
go run ./cmd/herdr-apps help
go run ./cmd/herdr-apps doctor
```

## Commands

| Command | Purpose |
|---|---|
| `herdr-apps daemon` | Starts the local authority, reconciles Herdr and listener observations, and publishes bounded workspace `$apps` metadata as compact `:port` entries. |
| `herdr-apps ensure-watch` | Checks the local IPC authority and, when unavailable, starts `daemon` and waits briefly for readiness. Herdr invokes this one-shot command automatically. |
| `herdr-apps list [--json]` | Lists the latest daemon snapshot; external listeners remain hidden unless the model explicitly includes them. |
| `herdr-apps open` | Opens the configured `apps` popup through Herdr. The visible `carellano.apps.apps` workspace action (bindable to `prefix+a`) invokes it. |
| `herdr-apps inspect <port>` | Shows evidence for the application that owns a listed port. |
| `herdr-apps doctor` | Reports local daemon availability and Herdr compatibility/reachability guidance without claiming unavailable live validation. |
| `herdr-apps tui` | Opens the interactive client against the existing daemon. |
| `herdr-apps help` | Prints command usage. |

Set `HERDR_SOCKET_PATH` only when connecting the daemon to an isolated or alternate Herdr socket. Set `HERDR_PLUGIN_STATE_DIR` only when isolating plugin socket and lock state, such as in tests.

## Action safety

Actions are daemon-validated and resolve against the latest revision. Open and copy use fixed arguments; unsafe URLs, credentials, control bytes, and unavailable tools are refused. Focus reports either exact pane success, an explicit workspace/tab fallback warning, or unavailable.

TERM and force-kill are unavailable without high-confidence, owned process evidence and explicit confirmation. Before signaling, the daemon revalidates PID, start time, process group, and identity; a changed or missing process receives no signal. Force-kill additionally requires a bounded grace expiry and a second confirmation/revalidation.

## Troubleshooting

| Symptom | Check |
|---|---|
| `doctor` reports Herdr unavailable | Start a compatible Herdr server or point `HERDR_SOCKET_PATH` at an isolated server socket. |
| `list` cannot reach the daemon | Run `herdr-apps ensure-watch` or start `herdr-apps daemon` for direct local validation; use the same isolated `HERDR_PLUGIN_STATE_DIR` for both processes when set. |
| No application is listed | Confirm a supported TCP listener and current Herdr pane/process evidence; unavailable or ambiguous evidence is shown as such rather than guessed. |
| `$apps` is absent or unchanged | Metadata is bounded, stable-sorted, and suppressed when its digest has not changed; inspect daemon/Herdr diagnostics first. |

## Clean rollback and uninstall

1. Stop only the `herdr-apps daemon` process you started.
2. Remove the local binary and any temporary checkout or approved manifest copy.
3. Remove only the plugin state directory selected by `HERDR_PLUGIN_STATE_DIR`, or the plugin-owned default state directory if it was created for this plugin.

The daemon cleans up only its nonce-matching socket, lock, and cache entries. Rollback does not terminate discovered applications, modify unrelated Herdr metadata, or remove host-managed plugin registrations.
