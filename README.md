# Herdr Dev Servers

Herdr Dev Servers discovers local listening development servers and presents them in a safe, evidence-backed Herdr popup. It keeps discovery in a plugin-owned daemon so the CLI and TUI see one current view.

## Install

Requirements: Herdr 0.8.0 or later, macOS or Linux, and permission to run this unsandboxed plugin. Marketplace installs show an interactive trust preview: review it before accepting because plugins run with your user permissions.

```sh
herdr plugin install carellano/herdr-dev-servers
```

For the initial version, pin the install explicitly:

```sh
herdr plugin install carellano/herdr-dev-servers --ref v0.1.0
```

The installer first downloads the matching verified release archive. If that archive is unavailable, it builds locally only when Go 1.24.0 or newer is available. Herdr does not install Go for plugins. This repository is not listed in the marketplace and has no published release until its GitHub repository is made public, receives the `herdr-plugin` topic, and a maintainer publishes a release.

## Use

Open the popup from Herdr with the plugin action, or run the command directly:

```sh
herdr-dev-servers open
herdr-dev-servers list
herdr-dev-servers inspect 3000
herdr-dev-servers doctor
```

Bind the workspace action in your Herdr configuration, for example:

```toml
key = "prefix+a"
action = "carellano.dev-servers.open"
```

The popup is also available through the `carellano.dev-servers.open` workspace action. The daemon is started by the plugin startup and `pane.created` hooks as needed.

## Configuration

Find the plugin-owned configuration directory with:

```sh
herdr plugin config-dir carellano.dev-servers
```

Create or edit `config.toml` there. These are the active keys:

| Key | Default | Meaning |
|---|---|---|
| `scan_interval_seconds` | `5` | Reconciliation interval, from 1 to 3600 seconds. |
| `ignored_ports` | `[]` | Unique TCP ports to exclude from discovery. |
| `opener` | `"system"` | Set to `"disabled"` to disable URL opening. |
| `clipboard` | `"system"` | Set to `"disabled"` to disable copying. |

`HERDR_SOCKET_PATH` selects an alternate Herdr socket. `HERDR_PLUGIN_STATE_DIR` is useful for isolated tests; both the daemon and client must use the same value.

## Local Development

Link the checkout for development instead of installing a release:

```sh
herdr plugin link .
go test ./...
go run ./cmd/herdr-dev-servers help
```

Unlink it when finished:

```sh
herdr plugin unlink carellano.dev-servers
```

Use a separate state directory for live tests. Do not start, stop, relink, or otherwise disturb a daemon you did not start.

## Safety

Open and copy actions use fixed command arguments and reject unsafe URLs, credentials, control bytes, and unavailable tools. Focus reports an exact result, a workspace/tab fallback warning, or unavailability.

TERM and force-kill require high-confidence, owned process evidence and explicit confirmation. Before signaling, the daemon revalidates PID, start time, identity, and process-group ownership. Force-kill requires a separate confirmation after the TERM grace period. No action guesses at ambiguous or stale evidence.

## Troubleshooting

| Symptom | Command or check |
|---|---|
| Herdr is unavailable | `herdr-dev-servers doctor` and verify the Herdr socket. |
| The daemon is unavailable | `herdr-dev-servers ensure-watch`, then `herdr-dev-servers list`. |
| No server is listed | Confirm a supported TCP listener and current Herdr pane/process evidence. |
| Need diagnostics | `herdr-dev-servers list --json` or `herdr-dev-servers inspect <port>`. |

## Architecture

The daemon owns listener discovery, correlation, revisions, and the plugin-local JSONL IPC endpoint. The CLI and Bubble Tea popup are clients of that endpoint. Correlation combines listener, process, and Herdr pane evidence; only high-confidence ownership can enable destructive process actions.

Supported production platforms are macOS and Linux on `amd64` and `arm64`. Other platforms can compile the CLI where dependencies permit, but process signaling is intentionally unavailable.

## Build And Test

```sh
gofmt -w .
go mod tidy
go vet ./...
go test ./...
go test -race ./...
go build ./cmd/herdr-dev-servers
sh -n scripts/install.sh scripts/check-release.sh
scripts/check-release.sh
```

Set `HERDR_DEV_SERVERS_FORCE_BUILD=1` to make the installer build locally. It is intended for CI and isolated local tests.

## Updating And Removing

Updates are reinstalls; install the desired ref again after reviewing its trust preview:

```sh
herdr plugin install carellano/herdr-dev-servers --ref v0.1.0
herdr plugin uninstall carellano.dev-servers
```

Use `herdr plugin unlink carellano.dev-servers` for a linked checkout instead of uninstalling it.

## Releases

Manifest versions and release tags move together: manifest `0.1.0` is released as tag `v0.1.0`. GoReleaser creates verified `darwin` and `linux` archives plus `checksums.txt`; the installer consumes that contract. Changes are recorded in [CHANGELOG.md](CHANGELOG.md). Releases are never version-bumped or created automatically by this project.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Security reports belong in [SECURITY.md](SECURITY.md).
