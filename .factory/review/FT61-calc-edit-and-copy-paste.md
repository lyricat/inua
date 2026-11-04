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
