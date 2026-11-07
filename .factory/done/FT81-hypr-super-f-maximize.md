---
source: pr #83
branch: factory/FT81-hypr-super-f-maximize
round: 1
---
# feat(hypr): maximize with Super+F instead of fullscreen

## factory · 2026-11-07 12:03
Added directly to review at the owner's request: “Move GitHub issue 79, 80, 81,82,83 to review status”. GitHub #83 is an open pull request.

Source: https://github.com/lyricat/inua/pull/83
PR head: a85ab17f0c0608555344cd9e8aefa297db7917f5
Original branch: pr/super-f-maximize
The exact PR head is preserved on `factory/FT81-hypr-super-f-maximize`. No code changes, review, or tests were performed during this status change. Verification statements below are the PR author's claims, not independently checked here.

PR description (quoted):
> The sample Hyprland config bound Super+F to fullscreen, which covers the bar and dock. Bind it to fullscreen state 1 (maximized): the window fills its workspace while panels stay visible, and pressing it again restores the window.
> 
> Verified by loading the Lua binding in the running Hyprland (no config errors) and by the classic dispatcher's documented state 1.

## factory · review · 2026-11-07 13:57
head: a85ab17f0c0608555344cd9e8aefa297db7917f5
verdict: pass
notes:
- No qualifying introduced defects found. Codex reviewed the complete origin/master...HEAD diff: one line in config/hypr/hyprland.conf, changing Super+F to fullscreen state 1. Branch merges cleanly with origin/master; HEAD and tracked files unchanged.
- Validation: final make test exited 0: Python 18 tests passed, go vet passed, Go race tests passed for 112 packages (96 cached); 34 packages had no tests. Initial isolation-environment failures involving /dev/null and AppArmor queries were resolved without product changes. Hyprland 0.53.3 --verify-config returned config ok, exit 0.
- Non-pinned installs: none. UI not visually checked; panel preservation and repeated keypress behavior were not independently exercised on a desktop.
- The diff changes only the classic sample configuration, not a Lua binding. The PR author's Lua verification claim was not independently verified; no broader Lua support is certified by this review.

## factory · 2026-11-07 16:03
Shipped at the owner's request after verifying the latest reviewed head. Merged into master at 8db50f34; release v0.1.27 points to 8db50f34. Linked PR #83: merged automatically by GitHub.
Validation: make generate and make i18n passed with clean generated output; full make test passed first run with throwaway HOME/runtime and uncached Go tests (18 Python tests, go vet, 113 race-tested Go packages). FT76 catalog conflicts resolved preserving both sides. Existing P2/P3 notes remain unchanged. No installs. UI was not visually checked or exercised on the desktop.
Moved ready/ → done/.
