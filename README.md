# Herdr Apps Plugin

`herdr-apps` is a foundation for a Herdr plugin that will present listening applications as evidence-backed entities.

## Foundation scope

- Defines the Go module, plugin manifest, command help, versioned model, and JSONL IPC envelopes.
- Owns complete model revisions in one daemon service.
- Recovers only stale regular lock files, preserves live owners and symlinks, and releases only nonce-matching state.
- Gates the static Herdr 0.8 compatibility evidence at protocol 19 and schema 1, then rebuilds from a baseline, subscription, and confirming snapshot.

## Explicit uncertainty

No live Herdr server was available while this foundation was created. The manifest fields and protocol contract are intentionally minimal and must be validated against a running Herdr 0.8+ server before a release claims runtime compatibility. Discovery, correlation, actions, metadata, and UI are outside this work unit.

## Local verification

```sh
go test ./internal/model ./internal/daemon ./internal/herdr
go run ./cmd/herdr-apps help
```
