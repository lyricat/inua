---
source: pr #82
branch: factory/FT80-bar-monitor-focus-outline
round: 1
---
# feat(bar): outline each monitor's last focused window

## factory · 2026-11-07 12:03
Added directly to review at the owner's request: “Move GitHub issue 79, 80, 81,82,83 to review status”. GitHub #82 is an open pull request.

Source: https://github.com/lyricat/inua/pull/82
PR head: a98bc3ff30416ea79ddc7ae145f1d45cc334dffa
Original branch: pr/monitor-focus-outline
The exact PR head is preserved on `factory/FT80-bar-monitor-focus-outline`. No code changes, review, or tests were performed during this status change. Verification statements below are the PR author's claims, not independently checked here.

PR description (quoted):
> Hyprland draws its blue active border only around the one globally focused window, so every other monitor looked inactive. Each bar now keeps its own monitor's last focused window outlined while the focus is on another monitor; the focused window keeps the compositor's own border.
> 
> The outline is a transparent, input-free overlay layer surface (empty input region via gtkx.PassInput) sized to the window's geometry and clipped to its monitor. It is used only on Hyprland, where Window.Geometry is available.
> 
> Verified in a nested Hyprland with three outputs: with focus on the second output, the first output's window got the blue ring at its exact rectangle (pixel-checked at the expected bounds), and the ring followed the window when focus moved.

## factory · review · 2026-11-07 13:40
head: a98bc3ff30416ea79ddc7ae145f1d45cc334dffa
verdict: pass
notes:
- [P2] internal/ui/focusring.go:38: clipping uses raw monitor mode dimensions rather than rotated/scaled logical dimensions. Reproduction: a rotated 1920x1080 monitor with a visible 800x600 window at (50,1200) loses its outline. Use GDK logical dimensions or transform the mode dimensions.
- [P2] internal/ui/bar.go:412: BarWindow selects only from ActiveWorkspace, ignoring a visible SpecialWorkspace. After focusing a special-workspace window on monitor A and moving focus to B, the outline targets the underlying normal-workspace window. Include the visible special workspace in selection.
- [P2] internal/ui/focusring.go:47: intersecting the window rectangle before stroking invents borders inside partially off-screen windows. A window whose left edge is -100 gets a false left edge at 0. Stroke the original rectangle and clip the drawing to the monitor.
- No P0/P1 findings; moved to ready under the factory review gate. The P2 defects remain for owner consideration.
- Codex reviewed all 10 changed files in origin/master...HEAD (e97ebcc...a98bc3f); branch merges cleanly. HEAD and tracked files unchanged.
- Validation: make test exited 0: Python 18/18, go vet passed, Go race tests passed for 112 packages (96 cached), 34 packages without tests. Initial isolation-only device, PID /proc and AppArmor failures were resolved without product changes. Temporary programs reproduced the three data-path defects.
- Installs: none. UI, input pass-through and three-monitor pixels were not visually checked; author's manual validation remains unverified.
