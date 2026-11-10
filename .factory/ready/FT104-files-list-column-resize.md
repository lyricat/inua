---
branch: factory/FT104-files-list-column-resize
round: 2
---
# Files: drag to resize list-view columns

## factory · 2026-11-10 16:26
Registered from the owner's Files UI request. Backlog only; no build started.

User request: "on list view, people can adjust the column width by dragging."

- Users can drag column-header boundaries to resize list-view columns, with appropriate pointer feedback and usable minimum widths.
- Resizing must not accidentally sort, navigate, select a file or start a file drag; normal header sorting continues to work.
- Check narrow windows, long names, horizontal overflow and display scaling. Decide width persistence and sizing behavior explicitly in the build plan.

## factory · 2026-11-10 16:28
Moved from backlog to new at the owner’s request: "ok. move them to new". Queued for build; no development started in this board update.

## factory · plan · 2026-11-10 17:45
Behaviour: retain default widths on first run; drag a header boundary with resize feedback to change its width live, keeping headers and rows aligned without sorting or file actions. Clamp minima and allow horizontal overflow. Empty/error directory headers behave the same; widths survive navigation, view switches and recycled rows.
Scope: list view, its settings and documentation only; not the Finder-style column view.
Decisions: persist global widths in existing Files settings in GTK logical pixels; validate loaded values and fall back to defaults. Save at drag completion. Initially planned to keep the name flexible until explicitly resized; implementation adjustment is explained below.
Checking: settings validation and persistence regression tests, make test with throwaway HOME/XDG, make i18n and make generate for changed layouts. Manual checks: sorting vs resizing, all columns, hide/show, recycled rows, empty/error folders, narrow-window overflow, long names, zoom/scaling and persistence.

## factory · 2026-11-10 17:45
Built with Claude Code at 24021ac9; pushed factory/FT104-files-list-column-resize. Moved new/ → review/, round 1.
Added separate 10-pixel header grips, resize cursors and hover/drag feedback; live updates use shared callbacks and do not rebuild the model. Widths persist globally in files.json, with invalid/unknown values discarded. Name minimum 160, other columns 48, maximum 1600 logical pixels.
Implementation decision: any column drag freezes the flexible name at its displayed width so the dragged boundary follows the pointer rather than expanding leftwards. Double-clicking a grip resets that column; resetting the name restores flexible sizing. These are accepted sizing/reset details within FT104 scope.
Tests: coder and manager independently passed make test (Python session tests, go vet, Go race tests) with throwaway HOME/XDG paths. Added settings limit/reset and persistence tests. Layout and translation catalogs regenerated. No unpinned installs or dependency changes.
Not checked: UI was not viewed or exercised in a graphical session. All manual cases in the plan remain unverified; grips are not keyboard reachable. Review should inspect drag coordinates, header/row alignment, reset and sizing behaviour in particular.
Branch push initially failed because the credential helper needed a repository path; retry with command-local credential.useHttpPath=true succeeded. Temporary verification HOME cleanup needed writable module-cache directories and then completed.


## factory · review · 2026-11-10 17:57
head: 24021ac968d357197fd39460eed2604add10e517
verdict: fix
fix:
- [P1] internal/filesui/columns.go:169: The rule's hexpand propagates to its grip, distributing spare header width among all grips while rows expand only the name/end spacer. A headless GTK stand-in at 1000px reproduced name header width 227 versus row width 546 and size-header x=346 versus cell x=582. Explicitly set g.SetHexpand(false) (or prevent inherited expansion) so headers and rows align and freezing the name does not jump.
notes:
- [P2] internal/filesui/columns.go:203: Freezing an automatically expanded name clamps it to 1600 via settings.go:131. With a displayed name around 1880px, a 3px drag snaps it to 1600; dragging right can shrink it. Preserve displayed widths when freezing/starting a drag instead of clamping below the displayed width.
- [P3] internal/filesui/columns.go:161: Grips cannot take keyboard focus and no keyboard resize/reset action exists, contrary to design/ui.md's keyboard-first requirement. Provide keyboard actions or obtain an explicit exception.

Reviewed by Claude Code against origin/master...HEAD, all 17 changed files. make test passed with isolated HOME/XDG (Python session tests, go vet, Go race tests; 115 packages). make generate and make i18n in a throwaway copy reproduced committed outputs. No installations or dependency changes; worktree clean, no code commits.
Validation limit: alignment was reproduced in a Python/GTK stand-in using headless Broadway, not the actual Files window. UI not visually checked; actual dragging/sorting, empty/error directories, zoom/scaling, restart persistence and multiple panes/windows remain unverified. The coder used and removed a short temporary socket directory outside the run directory despite the directory constraint; no retained external artifacts reported.
Moved review/ → new/ for fix round 2 (round 1 of max 3). No linked issue or PR.

## factory · plan · 2026-11-10 18:36
Fix round 2 supplement. Behaviour: grips retain their intended width; spare header width belongs to the name/end spacer consistently with rows, including empty/error directory headers. Scope: latest review P1 inherited expansion only; P2 wide-name clamping and P3 keyboard access remain known notes. Decision: explicitly disable expansion on the grip, keeping its inner rule expanding to centre the hairline. Checking: isolated make test and a safe headless GTK geometry stand-in; actual interaction and visual checks remain outstanding.

## factory · 2026-11-10 18:36
Built fix round 2 with Claude Code at 18fd7084; pushed factory/FT104-files-list-column-resize. Moved new/ → review/.
Fixed the P1 header/row misalignment with g.SetHexpand(false). Translation line references regenerated before commit; no Blueprint changes. No installations or dependency changes. Manager verified item-only history and unchanged protected paths.
Tests: coder and manager independently passed make test with throwaway HOME/XDG/TMPDIR (18 Python session tests, go vet, Go race tests). Coder's isolated Python/GTK Broadway stand-in reproduced misalignment before and matching header/cell positions after at requested widths 700, 1000 and 1600; the largest may have been limited by the headless display. This was not the actual Files window.
Not checked: UI was not viewed or exercised by hand. Dragging/sorting, empty/error folders, scaling/zoom, narrow windows, long names and restart persistence remain unverified. Known review notes unchanged: P2 names displayed wider than 1600px can snap when frozen; P3 grips lack keyboard resize/reset access. No linked issue or PR.

## factory · review · 2026-11-10 18:55
head: 18fd70847bf516af1af221203cbf0bb568d53713
verdict: pass
notes:
- [P2] internal/apps/files/settings.go:131: The 1600px cap still clamps an automatically expanded name when frozen by columns.go:206 or resized. With a displayed name around 1900px, a 3px SIZE-edge drag pins it to 1600 and shifts the remaining columns left. Preserve the displayed name width and accept it on reload. Code-path evidence only; the headless display was too narrow for a runtime reproduction.
- [P3] internal/filesui/columns.go:165: Grips cannot take keyboard focus and no keyboard resize/reset action exists, contrary to design/ui.md's keyboard-first requirement. Add keyboard actions or obtain an explicit owner exception.

Claude Code reviewed origin/master...HEAD, all 17 changed files, against the request and plan. No P0/P1 findings. Prior P1 fixed: a Python/GTK Broadway stand-in verified matching header/row positions at 1000 and 1024px, both flexible and fixed name sizing; grips remained 10px. Actual Files UI was not run or visually checked.
Validation: make test passed with throwaway HOME/XDG/TMPDIR (18 Python session tests, go vet, 115 passing Go race-test packages). make i18n and make generate in a throwaway copy reproduced committed outputs. No installs, dependency changes, code edits or commits. Manager verified clean worktree and unchanged reviewed head. Temporary artifacts remained within the review directory and were removed.
Unverified: actual drag/sort interactions, double-click reset, empty/error folders, narrow-window horizontal scrolling, long names, zoom/scaling, restart persistence, and widths over 1024px. The P2 and P3 notes remain nonblocking under the factory's P0/P1 gate.
Moved review/ → ready/. No linked issue or PR. FT89 and FT91 skipped because their current heads already have reviews.
