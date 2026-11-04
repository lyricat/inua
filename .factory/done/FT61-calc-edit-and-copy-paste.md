---
branch: factory/FT61-calc-edit-and-copy-paste
round: 1
---
# Calculator: edit the value and the formula, copy and paste from a context menu

## lyric · 2026-11-04 22:24
I can't edit the value and the formula in the calculator. I also want to copy and paste via a context menu; please take care of the menu design (follow design/ui.md and the shared menu pieces in internal/uikit).

## factory · plan · 2026-11-05 00:00
Behaviour: Make formula and value editable with caret and selection. Formula edits update the preview; value edits become the active expression, without altering history. Keypad inserts at the selection. Enter evaluates and records valid calculations. Right-click menus offer selection-aware copy/paste and editing through shared uikit. Empty first run shows zero; incomplete or invalid text remains editable, and errors appear beside the fields. Empty/nontext clipboard does nothing.

Scope: Calculator view, layout and styles, small toolkit-free editing helpers and regression tests. No unrelated apps or evaluator redesign.

Decisions: Prefer standard GTK editable controls. Copy selection, otherwise the whole target field; paste replaces selection or inserts at caret. Preserve answer continuation, scientific modes and history. Editing a value starts an expression rather than rewriting history.

Checking: Regression tests for Unicode selection replacement, bounds and answer continuation; generate layouts and catalogs; make test with a throwaway HOME. Manually inspect both fields, menus, clipboard, invalid input, history, scientific mode and long text under light/dark themes in an isolated session.

## factory · 2026-11-05 00:00
Built and pushed commit 047670ff. Formula and value use editable fields; shared uikit context menus provide Cut, Copy, Paste, Delete and Select All. Added editing helper regression tests. Invalid pasted text remains editable rather than being silently discarded.

make generate and make i18n completed. Coder reports application build and full make test passed; an initial mail test failure passed on retry without mail changes. Manager independently reran make test with a throwaway HOME: passed. No unpinned installs or dependency manifest changes. No forbidden paths changed.

UI changed and was not checked by eye. Manual checks listed above remain outstanding. Moved new/ → review/; not merged into master.

## factory · review · 2026-11-05 00:29
head: 047670fff829e8c1be3beaa0a5894d8e6cb18160
verdict: pass
notes:
- [P2] internal/lightui/calculator.go:326: dismissing the context menu does not restore the saved caret/selection. GTK/Broadway reproduction: select `2` in `2+3`, open and dismiss the menu without an action; selection changes from [0,1] to [0,3], then keypad `9` replaces the whole expression instead of producing `9+3`. Restore selection on cancellation while preserving explicit menu actions.
- [P2] internal/lightui/calculator.go:365: replacing field text with SetText clears native undo history. Reproduction in both fields: select `2` in `123`, paste `9` to obtain `193`, invoke GTK text.undo; text stays `193`, whereas ordinary typing is undoable. Use undo-preserving editable delete/insert operations; keypad replacement shares this path.

Validation: Codex reviewed all 15 changed files in origin/master...HEAD. No merge conflicts. Final make test exited 0: 15 Python tests, go vet, and race tests (103 packages passed, 33 without tests). Initial test attempts failed due to isolated temporary-path length and path safety checks; final run used a go test -exec wrapper giving seven socket-related packages short temporary aliases, with real paths for other packages. No tests were changed or excluded. make generate and make i18n in a disposable HEAD copy succeeded; all 51 generated files matched HEAD. Worktree remains clean; no code commits. Installs: none; existing pinned dependency caches copied only.
Limits: GTK/Broadway state probes were run, but UI was not checked by eye. Real Hyprland gestures, external clipboard, long text and light/dark themes remain unverified.
No P0/P1 findings remain; moved review/ → ready/ under the configured review gate, with both P2 findings retained for owner consideration. No linked issues or PRs. Not merged into master.

## factory · 2026-11-05 08:05
Owner authorized shipping ready items without blockers. Merged into master at f1acc7a; pushed integrated master f9cecde. Moved ready/ → done/. No linked issues or PRs.

Integration: resolved catalog conflicts through regeneration and preserved both terminal session drag and context-menu handlers. make generate and make i18n passed with no remaining generated changes. Final make test passed in isolated HOME/XDG/runtime and D-Bus (15 Python tests, go vet, race tests across 104 Go packages); compositor experiment suite passed 17 tests. Initial temporary-path failures were corrected in test isolation, not business code. 50 skip records include unavailable terminal engine, libturbojpeg, fish, bun and conditional/helper tests. No installs. No protected paths changed.
Limits: no visual or real-compositor validation; existing review P2 notes remain unresolved and nonblocking under the configured gate.
