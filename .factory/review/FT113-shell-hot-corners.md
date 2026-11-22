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
