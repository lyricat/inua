---
source: pr #80
branch: factory/FT78-launcher-right-click-actions
round: 1
---
# feat(launcher): open a result's actions by right-click

## factory · 2026-11-07 12:03
Added directly to review at the owner's request: “Move GitHub issue 79, 80, 81,82,83 to review status”. GitHub #80 is an open pull request.

Source: https://github.com/lyricat/inua/pull/80
PR head: 8a590bf2154cdef5be0d974f1f67233046badbcd
Original branch: pr/launcher-right-click
The exact PR head is preserved on `factory/FT78-launcher-right-click-actions`. No code changes, review, or tests were performed during this status change. Verification statements below are the PR author's claims, not independently checked here.

PR description (quoted):
> Right-clicking a launcher result now opens the same actions menu Tab does: open, new window, the app's desktop actions, keep in/remove from the dock, quit; windows can be switched to, closed, floated, maximized or centred.
> 
> The result cells' primary gesture is limited to button 1 so the right-click does not also launch the result.
> 
> Verified by building the shell and exercising the menu on a result in the launcher.

## factory · review · 2026-11-07 13:06
head: 8a590bf2154cdef5be0d974f1f67233046badbcd
verdict: fix
fix:
- [P1] internal/hyprconf/displays_test.go:122; internal/hyprconf/hyprconf_test.go:59,215,284: four Hyprland config tests fail with `XDG_RUNTIME_DIR is not set!`. Supply an isolated runtime directory and rerun make test. Environment-dependent failure in unchanged tests, not an established FT78 regression; classified P1 under the factory test gate.
notes:
- [P2] internal/ui/launcher.go:230: right-click changes selection without refreshing the highlight. Select Screenshot Area, then right-click Screenshot Screen (no additional actions): highlight stays on Area but Enter executes Screen. Render the launcher after Move before showing actions.
- [P2] internal/ui/launcher.go:218: GetWidget adds a reference that the callback never releases; each right-click retains another reference. Add defer w.Unref() after the nil guard.
- Codex reviewed all nine changed files in origin/master...HEAD. Branch merges cleanly with origin/master. No tracked files changed.
- Validation: make test with throwaway HOME exited 2; Python 18 tests passed, go vet passed, Go race tests passed for 111 packages and failed for one. git diff --check passed.
- Non-pinned installs: none. UI was not visually or interactively checked; interaction findings were verified through code paths. Author's manual test claims remain unverified.
