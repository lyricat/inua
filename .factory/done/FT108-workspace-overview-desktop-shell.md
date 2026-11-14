---
source: pr #97
branch: factory/FT108-workspace-overview-desktop-shell
round: 1
---
# Improve workspace overview and desktop shell

## factory · 2026-11-14 23:50
Imported https://github.com/lyricat/inua/pull/97 directly into review/ at the owner's request, superseding the earlier request for new/.
PR head: 31499073b12d07d6a0af758693bb23c1c8912bdb. Preserved on the dedicated factory branch; the PR's head branch is named master in its source repository.

Scope reported by the PR: unified app/window search in overview, per-monitor preview sizing, wallpaper-backed new-workspace tile, blurred backdrop, constant-width preview borders, desktop areas filling cards, and configurable other-monitor focus outlines hidden during overview. Includes icons-only dock configuration, hover switching of open top-level application menus, per-monitor menus that focus the owning window before opening, and adjacent submenus. The PR also reports dock, bar, application menu, icon-shape and screenshot changes already on master; review should distinguish new changes from existing base code.

PR-author validation (not independently rerun during import): live two-display Hyprland overview check; targeted UI/uikit/shell and Hyprland tests, make i18n, and git diff --check reported passing. Full make test reported failing on mounted-filesystem trash tests, legacy-config Lua parsing, and terminal integration timeouts/permission errors. No nested-session validation or screenshots were provided.

Pending factory review, including comparison with current master and verification of the reported test failures. This import performs no code review, tests, merge or release.


## factory · review · 2026-11-15 00:00
head: c5fab0ac11f311cc78b3769fa2d8cc6a03c460d5
verdict: pass
notes:
- [P2] internal/ui/appmenu.go:465: hovering a top-level leaf calls the full click handler and activates it without a click. Restrict hover switching to submenu entries. Example: open File, then hover a leaf Close for window 0x1; a Broadway scratch test records close-window 0x1.
- [P2] internal/uikit/cascade.go:119: non-autohide cascade popovers have no replacement outside-click/Esc dismissal. Restore autohide or explicitly implement dismissal. Example: open File and click inside the already-focused window; no focus change closes it, and the KeyboardNone bar cannot receive Esc. GetAutohide() verified false.
- [P2] internal/ui/overview.go:1103: only eight app results are rendered but keyboard selection includes every match. Cap or scroll the results. Example: twelve matching apps, query app, then Up selects invisible App 11 and Enter launches it; verified with a scratch state test.
- [P3] internal/shell/overview.go:130: SetSnapshot does not recompute active-query window matches. Refresh matches and clamp selection. Example: query fire, then remove Firefox window 0x1 in a snapshot; selected result still points to closed 0x1. Adding a second Firefox also leaves the list stale; verified with a scratch state test.

Claude Code reviewed all 39 changed files in origin/master...HEAD against the imported PR scope. No P0/P1 findings; pass follows the factory severity gate, not absence of defects. Owner should inspect the three P2 findings before shipping.

Merged origin/master c1fac939 into the item branch and resolved seven catalog conflicts, preserving both source references for New workspace and regenerating catalogs. Pushed merge c5fab0ac. No never-path changes. Initial push could not supply repository-scoped credentials; retry with per-command credential.useHttpPath=true succeeded.

Validation: make test passed independently for merge preparation, manager verification, and Claude review, with throwaway HOME. Python session suite: 18 tests OK; go vet clean; race-enabled Go suite: 117 packages OK. The earlier PR-reported failures did not reproduce in this isolated environment. Uncached Broadway TestAppMenuCascade and scratch search-state regressions verified the examples above. Catalog regeneration in the scratch copy matched committed catalogs. Installs: none.

UI was not checked by eye, live or nested. Blur, preview geometry, wallpaper, focus outlines, dock styling and submenu placement still require visual validation. Residual risks: compositor focus-change dismissal of newly opened popovers; pending other-monitor menu click dropped on entry changes or two-second expiry; search placeholder mentions workspaces but no workspace-name matching. These were not promoted to verified findings.

Runner deviations: merge-preparation coder wrote a temporary checker and test log under /tmp, outside the run directory; subsequent review was explicitly constrained to run-local artifacts. Review scratch archive failed with sandbox write errors, then was replaced by a source-directory copy; actual repository never-paths and worktree remained unchanged.

## factory · 2026-11-15 00:11
Shipped at the owner's request: “Ready items are fine, If so merge to master and move on.”
Merge: 666724129ece836212f3f9e5d21503be877397d4
Release: v0.1.39 (pushed atomically with master).
Review head unchanged and passing; merged without conflicts. make generate, make i18n and isolated make test passed after each item; generated files and catalogs unchanged. Final merged worktree clean; git diff --check passed.
Linked PR #97 was automatically marked merged by GitHub after the push; explicit close found it already merged. Factory branch retained (clean action not configured).
Existing P2/P3 review notes remain unresolved; no additional bug fixes in this ship run. No UI visual checks, live weather-provider checks, deployment or restart. Installs: none.
Moved ready/ → done/.
