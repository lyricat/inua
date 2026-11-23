---
source: pr #106
branch: factory/FT118-dock-move-workspace
round: 0
---
# feat(dock): move windows to a workspace from the app menu

## factory · 2026-11-23 15:10
Owner requested moving the open non-draft PRs to review/; this is PR #106.
Source: https://github.com/lyricat/inua/pull/106
PR head: 83b20223b575df59bdaad80ec414a25de89a3621

Imported the PR head unchanged to the factory branch above for review. No code review or tests performed during intake; the UI has not been checked by eye here.

PR description (author-reported behavior and verification, not independently verified):

> The Dock app menu offers a workspace picker for running windows. For apps with multiple windows, choose a window by title first. The current view stays in place.
> 
> Verified with targeted Dock tests. The feature was also tested in the live session before the changes were split.
> 

## factory · review · 2026-11-23 16:05
head: 83b20223b575df59bdaad80ec414a25de89a3621
verdict: pass
notes:
- [P3] internal/ui/dock_move.go:65: GtkExpander receives menu-item, but the menu styling selectors match button.menu-item only. Example: the Move to Workspace group lacks the menu row height, padding and hover/focus styling specified in design/ui.md. Add explicit expander-header styling.
- [P3] internal/ui/dock_move.go:36: workspace buttons retain their default centered labels while existing dock menu buttons explicitly left-align theirs. Example: Workspace 2 is centered next to left-aligned New Window and Quit. Left-align the new button labels.
- [P3] internal/ui/dock_move.go:64: window titles enter expander labels without an ellipsization or width bound. Example: a multi-window app with a very long page title can widen the dock menu beyond the display; the drawer disables horizontal scrolling. Bound and ellipsize these labels.

Reviewed by Claude Code on origin/master...HEAD against the imported PR request. No P0/P1 findings. make test passed: 18 Python tests, go vet, and all 117 Go packages with the race detector. Initial test invocation lacked XDG_RUNTIME_DIR and failed Hyprland config checks; a rerun with a private scratch runtime directory passed. make i18n in a scratch copy matched committed catalogs; gofmt reported no changed Go files. No tracked code changes or commits; no unpinned installations (only pinned Go module restoration).

The UI was not checked by eye. No nested session, compositor interaction, live Wi-Fi or user-service manipulation. Wayfire and niri behavior was inspected statically only; the new GTK menu logic has no dedicated tests. Same-title windows remain indistinguishable by title. Moved review/ → ready/; no merge or shipping performed.
