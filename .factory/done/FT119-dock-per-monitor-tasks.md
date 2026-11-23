---
source: pr #107
branch: factory/FT119-dock-per-monitor-tasks
round: 0
---
# feat(dock): optionally show tasks on their own display

## factory · 2026-11-23 15:10
Owner requested moving the open non-draft PRs to review/; this is PR #107.
Source: https://github.com/lyricat/inua/pull/107
PR head: 412240419a3d3054fa543c228c37233b4d28ce18

Imported the PR head unchanged to the factory branch above for review. No code review or tests performed during intake; the UI has not been checked by eye here.

PR description (author-reported behavior and verification, not independently verified):

> Adds an opt-in Dock setting to show running tasks only on the display that owns their windows. Pinned apps remain available on every display. Dock clicks, scrolling, and the context menu use the local window list when enabled.
> 
> Targeted config, shell, and Dock tests passed. The feature was tested in the live dual-display session before the patches were split.

## factory · review · 2026-11-23 16:23
head: 412240419a3d3054fa543c228c37233b4d28ce18
verdict: pass
notes:
- [P3] internal/ui/dock.go:672-683: per-monitor clicks can launch an app with existing windows on another display, but renderDock clears its launching mark from the all-display item list (lines 381-385). Example: Firefox pinned on DP-1 with its only window on HDMI-A-1; clicking it on DP-1 launches and marks it, then the immediate render deletes the mark because Firefox already has a remote window, so the launch bounce never appears. Track newly created windows rather than clearing on any existing window. Confirmed by code tracing, not an interactive run.

Claude Code reviewed origin/master...HEAD and all changed files against PR #107. No P0/P1 findings; moved review/ → ready/.
Validation: make test passed (18 Python session tests, go vet ./..., go test -race ./... across 117 packages with tests). Changed Go files passed gofmt. Isolated translation extraction produced byte-identical po/inua.pot and all six catalogs. Tests used a throwaway HOME and caches under this run's TMPDIR. No installs, downloads, manifest changes, or tracked worktree changes. Branch merges cleanly with the fetched master.
UI was not checked by eye; no nested session or live interaction. Per-monitor filtering lacks direct regression coverage; disabled-output behavior and late connector names remain unverified risks. Reviewer also suggested documentation and label changes; subjective wording suggestions were not accepted as confirmed defects. An initial temporary source-copy attempt failed with Function not implemented; subsequent isolated extraction succeeded, and temporary copies were removed.

## factory · 2026-11-23 19:28
Shipped into master: 88a99286c7e0b783f8a84bc7d5cce943775f785f. Release: v0.1.43; master and annotated tag pushed atomically.
Claude Code resolved Dock state/menu conflicts while preserving workspace moves and per-monitor filtering; catalogs regenerated without losing messages/translations. Latest passing Claude review matched the branch head before merge. make generate and make i18n passed after each merge; final regeneration left no diff. Targeted package race tests and final make test passed (18 Python tests, go vet and Go race tests).
PR #107 already marked MERGED after push; no close needed. Branch retained (clean not configured). No new installs.
Existing P2/P3 findings remain unchanged. UI not checked by eye; no nested compositor session, deployment or restart.
Test harness initially failed due to sandbox AppArmor and PID/proc restrictions; correcting harness access and PID visibility made tests pass without source changes. /tmp was privately mapped to the ship run scratch directory and host /storage hidden.
