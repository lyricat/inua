---
branch: factory/FT110-files-new-folder-with-selection
round: 1
---
# Files: select the default folder name and create a folder from selected items

## factory · 2026-11-15 20:31
Registered the owner's UX request for inua-file in `new/`; implementation has not started.

Requested behavior:
- When the create-folder dialog opens, focus the name field and select the entire default name so typing replaces it immediately.
- When multiple files and/or folders are selected, include a New Folder action in the selection context menu.
- Invoking that action opens the create-folder dialog and explicitly tells the user that the selected items will be moved into the new folder after it is created.
- Show the selected files/folders in that dialog so the user can see what will move.
- On confirmation, create the new folder in the current directory and move the selected items into it.

Acceptance checks:
- Ordinary folder creation still works; its default name is selected on opening.
- Multi-selection works for files, folders, and mixed selections.
- Cancelling the dialog creates nothing and moves nothing.
- Invalid names or folder-creation failure move nothing; move failures are reported accurately without deleting, overwriting, or hiding unmoved items.
- Keep the existing visual design and include regression coverage for default-name selection, multi-selection creation/moves, cancellation, and errors.

## factory · plan · 2026-11-15 20:43

Behaviour:
- Ordinary new-folder dialogs focus their name entry and select the whole suggested name, including collision suffixes; ordinary file creation remains unchanged.
- In a normal directory with multiple selected items, offer New folder… in the selection context menu. Snapshot directory and selection when invoked. Show all selected names in the dialog and state that confirmation creates the folder and moves those items into it.
- Cancel has no filesystem effects. Validate the name before starting; create the destination exclusively before moving anything. Creation errors move nothing. Use existing job/progress/error mechanisms to move files, directories and mixed selections without overwriting; failures preserve unmoved sources and accurately report partial results. Refresh the affected pane.
Scope:
- Files creation card, selection context action, minimal filesystem/job support if needed, regression tests and translations. No redesign, unrelated operations, workflows or factory settings.
Decisions:
- Selection grouping is a dedicated action; existing toolbar/keyboard new-folder behavior remains ordinary creation. Limit grouping to a normal directory, not trash/search/picker. Display names in a scrollable list using existing card conventions when needed. Keep an already-created folder on partial move failure; never roll back by deleting data. Capture paths before asynchronous execution.
Checking:
- Cover full default-name selection, ordinary folder creation, mixed/multiple file/directory moves, cancel/no-op, invalid/taken names, mkdir failure, and move failures/partial results with temporary fixtures and existing seams. Run make i18n before each Go/Blueprint commit; make generate for layout changes; run make test with a throwaway HOME and preserved tool caches. UI is not checked by eye here; explicitly record it.

## factory · 2026-11-15 20:43

Built and pushed e5f32b9a on factory/FT110-files-new-folder-with-selection (round 1).

- Ordinary folder creation focuses the entry and selects the complete suggested name, including collision suffixes.
- Normal-directory multi-selection offers a dedicated New folder with the selection action. The dialog snapshots paths, explains the move, and shows selected names in a scrollable list (200 visible plus a remaining-count notice).
- New Gather job exclusively creates the folder before moving anything, uses existing move/progress/undo support, refuses existing destination entries, reports folder-creation and partial-move failures, and leaves unmoved sources intact.
- Existing keyboard/toolbar creation remains ordinary creation; grouping is excluded for single selection, trash, search and picker. Existing undo behavior remains: moves back first, then trashes the created folder, with any leftover content recoverable in trash.
- Added filesystem and Broadway interaction regression tests for name selection, ordinary creation, selection eligibility, mixed/folder moves, cancellation, invalid/taken names, mkdir failure, vanished sources and changed selection. Updated docs and regenerated/translated catalogs.
- Installs: none; no dependency manifests changed.
- Checking: coder make test passed; manager independently reran make test with a throwaway HOME: passed (Python session tests, go vet, race-enabled Go tests). Forbidden paths unchanged; one topical commit only. Initial code push needed per-command credential.useHttpPath=true; retry succeeded without changing settings.
- Not checked: UI was not checked by eye in a nested session; long-name layout/wrapping and translations need human review. Search/picker exclusion and midway job cancellation lack direct regression coverage. Broadway automated interaction coverage ran according to coder report.

## factory · review · 2026-11-15 20:55
head: e5f32b9a54787d776baf95070cb26e0832a47b75
verdict: pass
notes:
- [P2] internal/apps/files/ops.go:284: Gather records its folder in Undo.Created, so undo trashes it even when move-backs fail or new files remain. Verified example: gather a.txt/b.txt, recreate a.txt in the parent and add added.txt inside the folder, then undo; b.txt returns but original a.txt and added.txt are silently sent to trash. Prefer removing the folder only if empty after move-backs, retaining it and reporting failures otherwise; add regression coverage.
- [P3] internal/apps/files/ops.go:281: An already-cancelled Gather still creates its folder before checking cancellation. Verified example: cancelled context, sources a.txt/b.txt and destination NF yields context canceled but leaves NF beside both sources. Check cancellation before Mkdir and add coverage.

Validation: Claude Code reviewed origin/master...HEAD against the request and plan. No P0/P1 findings. make test passed with a throwaway HOME/runtime directory (Python session tests, go vet, race-enabled Go tests). Three throwaway overlay probes verified partial failure/undo and cancellation behavior; exported-copy make i18n matched committed catalogs. Worktree remained clean at the reviewed head. No installs. Branch merges cleanly with origin/master.

Not checked: UI was not checked by eye; long-name layout, rendering, focus and translations need human review. Midway job cancellation and search/picker exclusion lack direct regression tests. The destination Lstat-to-move race was not reproduced; slow-filesystem cost of dialog Lstat calls was not measured. P2/P3 notes are non-blocking under factory policy and should be considered before ship. No linked issues or PRs were declared.

## factory · 2026-11-15 21:15
Shipped to master by the interrupted ship run; completing board bookkeeping after owner-requested fresh Claude Code review.
Merge: c103e6af70910e604a357f13e3fdb7a75c00ee67
Release: v0.1.40 (master c103e6af70910e604a357f13e3fdb7a75c00ee67); remote master and tag verified.
Claude Code independently reviewed combined range 66672412..c103e6af, all 16 changed files, and found no P0/P1. Isolated make test passed (Python session tests, go vet, race-enabled Go tests, 117 packages); gofmt and diff checks clean. No installs, edits or commits by reviewer.
No linked issues or PRs. Known non-blocking P2 Gather undo may trash remaining content after move-back failures; P3 cancellation during counting can leave an empty folder. Both retained following fresh review; no fixes made.
UI not checked visually or interactively; no deployment or restart. Factory branches retained (clean not configured).
