---
branch: factory/FT102-files-location-segment-context-menu
round: 1
---
# Files: context menus for location-bar folder segments

## factory · 2026-11-10 16:26
Registered from the owner's Files UI request. Backlog only; no build started.

User request: "allow using context menu on the folder segment at the location bar."

- Each folder segment in the location bar supports a context menu targeting that segment's folder, not the currently selected file or a different directory.
- Reuse applicable existing folder actions and their permission/error handling. Opening the context menu must not navigate as a side effect.
- Check root, ancestors, the current folder, paths containing spaces/non-ASCII characters, and unavailable folders. Preserve normal click-to-navigate behavior.

## factory · 2026-11-10 16:28
Moved from backlog to new at the owner’s request: "ok. move them to new". Queued for build; no development started in this board update.

## factory · plan · 2026-11-10 16:44
Behaviour: Right-click each real folder segment to open the existing styled menu at the pointer without navigating or changing selection. Actions capture that segment's exact path and pane. Root, home, ancestors and current folder support spaces and Unicode; unavailable folders use existing error handling. Preserve normal click navigation. Empty and first-run folders need no special state; Trash stays virtual.

Scope: Path-bar gesture wiring, explicit-path action helpers, menu cleanup, relevant tests and docs. No sidebar redesign, column resizing or previews.

Decisions: Reuse safe explicit-target folder operations, never change selection to dispatch. Exclude destructive ancestor/root actions. Use shared callback hub and menu lifecycle. The owner's “move them to new” concerns board scheduling only, not new UI requirements.

Checking: make test with throwaway HOME/XDG directories; regression coverage for paths and action targets. Manual checks: root/home/ancestors/current, Unicode/spaces, missing folders, unrelated selected files, dual panes, repeated rebuilding and normal navigation. UI is not checked by eye here.

## factory · 2026-11-10 16:44
Built and pushed 4ad265da on factory/FT102-files-location-segment-context-menu, from a6787817.

Folder menus target the clicked path: new tab/window/other pane, create file/folder, paste, terminal, copy path, bookmarks and tidy (not root). Omitted rename/delete/trash/cut of ancestors. Trash and picker retain separate menus. Shared helpers reuse existing operation/error handling; creation dialogs show noncurrent destinations. Keyboard menu access and middle-click background tabs support path-bar interaction.

The coder misinterpreted the scheduling quote in its report; that interpretation is not a requirement. Implementation was checked against the plan instead.

Tests: make i18n completed. make test passed both in coder execution and independent manager verification (Python session tests, go vet, Go race tests). Regression tests cover path segments, unavailable folders and target-specific actions on a hidden GTK display. One item-scoped commit; clean worktree; no forbidden path changed.

Installs: none. UI changed but was not checked by eye; no nested session run. Pointer placement, repeated interactions and dual-pane behavior need manual inspection.

Initial branch push failed because the credential helper required a repository path; retry with command-local credential.useHttpPath=true succeeded. No settings changed.

Moved new/ → review/; round 1.

## factory · review · 2026-11-10 16:56
head: 4ad265dabc2440bbdd17e3dff8da31dc8474cb37
verdict: pass
notes:
- [P3] internal/filesui/pathbar.go:90: In the file picker, Ctrl+L → trash: → right-click the Trash segment exposes a no-op New tab and Empty Trash with permanent-deletion confirmation. The trash case precedes the picker guard. Check picker mode first and suppress the trash menu there, preserving the documented restricted picker actions.

Claude Code reviewed origin/master...HEAD, all 14 changed files and relevant callers, against the request and plan. No P0/P1 findings; no merge conflicts. Manager confirmed unchanged head and clean worktree.

Validation: coder ran make test successfully with throwaway HOME/XDG/TMPDIR and live session variables unset (Python session tests, go vet, Go race tests). Focused GTK/filesui tests passed, including TestCrumbMenuActsOnItsFolder. Catalog regeneration on a throwaway copy matched committed catalogs; gofmt check clean.

Installs: none. UI not checked by eye; no nested session. Pointer/keyboard menu placement, repeated rebuild interactions and dual-pane behavior remain manual checks. Real-folder Terminal, new window, Tidy and Paste effects were not exercised. No linked issue or PR. Moved review/ → ready/ under the no-P0/P1 review policy; P3 remains for owner consideration.

## factory · 2026-11-10 19:48
Shipped at owner request: ready → done. Merge: e9f914fc8a185299cf49b38e7d0e45d7ba481996. Tag: v0.1.31. Master and tag pushed atomically.
No linked issue or PR.
Merged without conflicts.
Validation: make generate and make i18n completed; make test passed after this integration (18 Python tests, go vet, Go race suite), under isolated throwaway HOME/XDG with no live session. Final generation reproduced cleanly. Installs: none. UI was not checked by eye; no deployment or process restart. Existing reviewed P3 findings remain accepted.
