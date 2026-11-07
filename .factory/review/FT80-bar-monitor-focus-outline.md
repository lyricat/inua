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
