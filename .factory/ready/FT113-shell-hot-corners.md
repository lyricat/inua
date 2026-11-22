---
source: pr #101
branch: factory/FT113-shell-hot-corners
round: 0
---
# feat(shell): hot corners for the overview and the desk

## factory · 2026-11-22 16:37
Owner requested adding non-draft PR #101 directly to review/.
Source: https://github.com/lyricat/inua/pull/101
PR head: 01d800cd7f6fb2a308cb358e27c94df9db9b6178

Imported the PR head unchanged to the factory branch above for review. No code review or tests performed during intake; the UI has not been checked by eye here.

PR description (author-reported behavior and verification, not independently verified):

> ## What changed
> 
> - Adds hot corners to Settings → Bar & Dock, with an action per corner (`off`, `overview`, `show-desktop`) and one shared dwell delay (0–2000 ms, 25 ms steps; default 500 ms). Defaults: overview at top left, show desktop at top right.
> - Each display has its own four corners (within `hotCornerSize`, 24 logical px of a corner). The pointer is polled at 20 ms while at least one corner has an action; only Hyprland, via `j/cursorpos` (the same query the app menu already uses). No layer surface takes input — the HUD passes it through with `gtkx.PassInput`.
> - While the pointer dwells in a corner, a small glass HUD at that corner crossfades in with an icon and a progress bar that fills to the configured delay, then it pulses and the action runs. Leaving early crossfades it out; re-entering starts over.
> - As on a Mac, each entry in a corner alternates: the overview opens and closes, and show desktop puts the windows of the current workspaces into a hidden `special:inua-desktop` workspace and the next entry brings them back to the workspaces they came from. The workspaces themselves never change.
> - Show desktop's window list is kept in the shell's store, so windows left in the hidden workspace by a shell that stopped are brought back on the next start.
> 
> ## Notes
> 
> - `internal/ui/hotcorners_test.go` covers corner detection (single/multi monitor, the hot-corner-disabled case) and the dwell-progress/trigger path with a test action hook.
> - Hyprland's `j/monitors` reports the mode's physical size while `cursorpos` is in logical pixels, so an output's size for corner math divides by `scale` (and swaps turned outputs).
> - Settings copy and sample config are updated; `make generate` and `make i18n` were run.
> - This is separate from the Shift-square screenshot PR (#100) and touches no screenshot code.
> 
> ## Verification
> 
> - `go build ./...`, `go vet ./internal/ui`, and `go test ./internal/config ./internal/shell ./internal/ui -run HotCorner` pass.
> - On HX90 (Hyprland 0.56, two displays) with the new shell running in the session: entering the configured corner showed the HUD, ran the action after the delay, and leaving/re-entering alternated (overview closed; show desktop's windows returned to their original workspaces). The show-desktop state survives a shell restart (windows recovered from the hidden workspace).

## factory · review · 2026-11-22 16:56
head: 01d800cd7f6fb2a308cb358e27c94df9db9b6178
verdict: pass
notes:
- [P2] internal/config/config.go:996: Default dwell is 150 ms, while the imported PR description specifies 500 ms. With no hot_corners configuration, the top-right action hides windows after 150 ms. Align the default, sample and documentation with the intended delay. Reviewer classified this P1; manager classifies it P2 because this is a configurable timing mismatch, not verified severe failure or data loss.
- [P2] internal/ui/hotcorners.go:95: Polling does not check locked/locking state. Entering the top-right corner while locked can hide the active workspaces' windows; top-left can open the overview behind the lock. Reset dwell and suppress actions while locking or locked.
- [P2] ui/settings.blp:526: The added Replace inua app icons switch persists app_icons_inua but no icon resolver consumes it in this branch; toggling it redraws unchanged icons. Implement its behavior separately or omit the non-functional switch.
- [P3] internal/ui/hotcorners.go:181: max(scale, 1) miscalculates outputs scaled below 1. A 1920x1080 output at 0.8 is 2400x1350 logical: (2390,10) is rejected and (1900,10) incorrectly triggers top-right. Divide by any positive scale.
- [P3] internal/ui/hotcorners.go:195: Inclusive output bounds attribute a shared boundary to the previous display. With an output spanning x=-1280..-1 followed by one at x=0, (0,10) selects the first output's top-right instead of the second's top-left. Use exclusive upper bounds.
- [P3] ui/settings.blp:1731: Settings describes corners of all displays together, but detection is per display. Update the wording; the config comment describing an empty-workspace switch is also stale.
- [P3] ui/style.css:697: HUD radius is hard-coded to 24 px rather than following style.radius. With style.radius=0 the HUD remains rounded; include it in configurable corner styling.

Reviewed by Claude Code on origin/master...HEAD (base 10802f2873b1a95b21af180f465fe8e95a733643; merge-base be1f9004). All changed hunks reviewed; branch merges cleanly with base. No P0/P1 findings remain after manager severity assessment, so moved to ready/ under the factory's P0/P1 gate; lower-priority findings remain for owner consideration.

Validation: make test passed (18 Python session tests, go vet ./..., go test -race ./...). Worktree unchanged. No unpinned installs or dependency changes. Generated layout and translation output were checked by the reviewer.

Not checked: UI by eye or in a nested session; live lock-screen interaction, HUD animation/input pass-through, tiled order/focus after restore, visible scratchpads and fullscreen interaction. Lock-state and coordinate findings are code-path examples, not live-session reproduction. Runtime recovery and show/restore integration lack dedicated tests; polling idle cost was not measured.
