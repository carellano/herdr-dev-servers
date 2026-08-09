# Contributing

Contributions should preserve Herdr Dev Servers' central boundary: the daemon owns discovery, correlation, and action validation; CLI and TUI clients consume its snapshot through local IPC.

## Contributor Workflow

1. Create a focused change with tests at the smallest behavioral boundary.
2. Format and verify it locally.
3. Describe behavior, safety impact, and verification in the pull request.

```sh
gofmt -w .
go mod tidy
go vet ./...
go test ./...
go test -race ./...
scripts/check-release.sh
```

## Testing

Use test-driven development for behavior changes: add or adjust a focused test, implement the smallest change that passes it, then run the full suite. Use temporary directories and fake process or Herdr boundaries in tests; never target host processes.

Live tests are opt-in and isolated. Use a separate `HERDR_PLUGIN_STATE_DIR` and, when needed, `HERDR_SOCKET_PATH`. Do not link, unlink, restart, stop, or signal a live daemon or application you did not start yourself. Do not use a development checkout to overwrite a production plugin installation.

## Safety And Scope

Destructive actions require high-confidence ownership, explicit confirmation, and immediate process identity revalidation. Preserve those checks and add tests for both allowed and refused paths. Keep changes narrow; avoid unrelated refactors and generated artifacts.

## Releases

Run `scripts/check-release.sh` before proposing a versioned release. The manifest version, changelog entry, installer build argument, release tag, and GoReleaser archive names are one release contract. Maintainers publish tags and releases; contributors must not create tags or modify repository visibility, topics, or remotes.
