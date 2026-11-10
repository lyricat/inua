---
branch: factory/FT104-files-list-column-resize
round: 1
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
