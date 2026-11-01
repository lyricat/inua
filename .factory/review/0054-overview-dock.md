---
branch: factory/54-overview-dock
round: 0
---
# Show Dock during workspace Overview

## lyric · 2026-11-01 20:52
Import the author's PR #53 directly into factory review.

## factory · 2026-11-01 20:55
Source: https://github.com/lyricat/inua/pull/53
Author: iamcheyan
PR branch: feat/overview-dock
Imported commit: 6d10352e7d9a860bd02ebd94556b3fb20ba10657

The factory branch preserves the author's commits. Item 0053 already belongs to the completed terminal pane close confirmation task; this separate request uses item 0054.

Requested behaviour: keep the Dock visible above workspace Overview, provide a setting to disable this behaviour, and restore the previous Dock autohide state when Overview closes.

The author reports running `go test ./internal/config`, `go test ./internal/shell`, `CGO_ENABLED=0 go build -buildvcs=false -o /tmp/inua-overview-dock-test ./cmd/inua-shell`, `make generate`, and `make i18n`. These results have not been independently verified during import. The author did not exercise the UI in a nested desktop session; neither has the factory during import.

Pending review: inspect the implementation and regression coverage, verify configuration defaults and persistence, check Dock layering and autohide restoration across Overview open/close and setting changes, and run the configured `make test` against the merge result with current master. Follow repository generation and translation requirements when fixing code. Do not treat this import as review approval or merge authorization.
