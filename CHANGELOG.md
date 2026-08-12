# Changelog

All notable changes to Herdr Dev Servers are documented here.

## Unreleased

No unreleased changes.

## 0.1.2

Release contents for `v0.1.2`.

- Send TERM and eligible force KILL to the exact verified listener PID when it is a child of an npm-managed process group, while retaining process-group signals for verified group leaders.
- Make the popup distinguish navigation association from the separate, stricter safety evidence required before TERM or force KILL is available.
- Remove the internal snapshot revision from the visible popup header while retaining revision-based action safety.

## 0.1.1

Release automation correction for the next publication attempt. This entry does not indicate that a GitHub release has been published.

- Fix the release workflow's tag-to-manifest version gate.
- Prepare release artifacts for the `v0.1.1` publication tag.

## 0.1.0

Initial release candidate. The `v0.1.0` release workflow failed before publication, so no GitHub release or release assets exist for this tag.

- Discover and correlate local development-server listeners with Herdr pane evidence.
- Provide a popup, direct CLI, configuration, and safe open, copy, focus, TERM, and force-kill actions.
- Distribute verified macOS and Linux release archives with a local Go build fallback.
