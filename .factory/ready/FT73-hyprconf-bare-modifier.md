---
source: pr #71
branch: fix/hyprconf-bare-modifier
round: 1
---
# fix(hyprconf): skip bare modifier Lua binds

## factory · 2026-11-06 22:40
Owner requested: factory: move github issue #69 #71 #72 to review

GitHub #71 is a pull request: https://github.com/lyricat/inua/pull/71
Assigned FT73 and added directly to review using the existing PR branch.
PR head at intake: 33940991a269e80e75c94bbe982432cf863abc8c
This is a fork PR; fetch its head via pull/71/head for review.

Scope from the PR: Skip bare modifier shortcuts in unbind generation and Lua binds; retain classic SUPER_L/SUPER_R release handling and add renderer regression coverage.

Author-reported tests have not been independently verified at intake. Full make test and code review remain pending. No implementation changes or merge performed. UI was not checked by eye.

## factory · review · 2026-11-06 23:25
head: 33940991a269e80e75c94bbe982432cf863abc8c
verdict: pass
notes:
- Codex reviewed origin/master...HEAD; no introduced P0/P1 defects found. Branch merges cleanly with origin/master.
- make test passed (exit 0): 18 Python tests, go vet, and 111 Go race-test packages. Hyprconf tests: HEAD 23/23, master 22/22; the new regression test fails on the merge-base as expected.
- Initial isolated-environment AppArmor failure reproduced on master; after correcting the test environment, the full suite passed.
- Existing limitation, not an introduced finding: internal/hyprconf/keys.go:353 voice submap still emits a bare SUPER Lua bind when voice is enabled and its shortcut is SUPER; the same behavior exists on master. Consider follow-up coverage/filtering for this path.
- Installed Hyprland 0.53.3 passed classic validation; the author's Hyprland 0.57 compatibility report was not independently verified. UI was not checked by eye.
- No unpinned installs, dependency manifest changes, source edits or code commits. Fork PR head fetched through pull/71/head; header branch retained.
