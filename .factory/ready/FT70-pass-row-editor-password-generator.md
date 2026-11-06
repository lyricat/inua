---
branch: factory/FT70-pass-row-editor-password-generator
round: 1
---
# Pass light app: default row-by-row editor and password generator

## factory · 2026-11-06 17:28
Requested by the owner:
> New factory feature: pass light app, update an alternative edit mode (default), row by row. And provide password generator button (icon button)

Feature requirements:
- Add an alternative row-by-row editing mode to the Pass light app and make it the default editor.
- Keep the existing editing mode available as an alternative.
- Provide a password-generator icon button alongside the password field, with a tooltip and accessible label.

Implementation defaults and acceptance checks:
- Represent the password and subsequent entry lines as editable rows, preserving their order and content; opening and saving an unchanged entry must not lose information.
- Switching editing modes must preserve unsaved changes. Preserve the existing save/cancel behavior and storage format.
- Generate passwords with a cryptographically secure random source. Insert the generated value into the editor without automatically saving or copying it; do not silently overwrite an existing password without an explicit user action.
- Handle empty/new entries, multiline entries, and generation failures without losing edits. Never log password values.
- Add regression coverage for row serialization, mode switching, and password generation. Follow the existing app's UI conventions.

Queued for a factory build; not implemented yet.

## factory · plan · 2026-11-06 18:05
Behaviour: Open in Rows with a password row and ordered subsequent lines, preserving blank lines and line endings. Text mode shares the unsaved draft. Save/Cancel and storage retain existing semantics. Generate only on explicit action; confirm replacing an existing password, never auto-save or copy, and retain edits on errors.
Scope: Pass draft model, editor, generator icon, regression tests, documentation, generated layouts and catalogs; no backend/storage changes.
Decisions: Preserve exact unedited draft bytes, including CRLF and trailing-newline state; retain the existing save-time newline convention. Generate 24 cryptographically random characters with multiple character classes and no look-alikes.
Checking: Round-trip empty/Unicode/CRLF/mixed-ending drafts, edits and mode switches, generator randomness/error handling, private Broadway editor test, generated-file consistency, and make test. Visual checks remain for the owner.

## factory · 2026-11-06 18:05
Recovered the clean item worktree and existing commit from an interrupted build; no changes discarded. Claude Code verified the implementation and required no additional commit.
Built: 051b2de405f2f1d31e1a25e44ae9404090a84da1. Default Rows editor, alternate Text editor, shared lossless draft, add/remove rows, secure generator with replacement confirmation, tooltip/accessibility label, documentation and regression tests. Branch pushed; new/ → review/.
Tests: make test passes independently for both Claude Code and the manager with throwaway HOME, TMPDIR and XDG_RUNTIME_DIR. Python tests, go vet and race-enabled Go tests passed. Real GTK editor automation ran on a private Broadway display. make generate and make i18n were clean according to the coder.
An initial coder test run lacked a private XDG_RUNTIME_DIR and failed Hyprland config tests; rerunning with the private directory passed.
Installs: none.
Not checked: UI was not inspected by eye; owner should check Rows/Text layout, generator icon and replacement menu. Accessibility was disabled in the UI test; generator failure is covered at the model level, not the window error display.

## factory · review · 2026-11-06 18:31
head: 051b2de405f2f1d31e1a25e44ae9404090a84da1
verdict: pass
notes:
- Codex reviewed all 19 changed files in origin/master...HEAD; no qualifying findings. Password randomness, sampling, explicit replacement, secret disclosure and draft preservation paths checked.
- Codex validation: make test passed with isolated HOME, TMPDIR and XDG_RUN2026-11-06 18:31_DIR (18 Python tests, go vet, race-enabled tests across 112 Go packages).
- make generate and make i18n passed in a temporary source copy; 51 generated files matched. Private Broadway overlay checks passed for multiline paste, CRLF, mode switching, cancel, generation failure and simulated save failure.
- Initial direct GTK paste probe did not activate on its first attempt; reproduced on a plain GTK entry and not attributed to this change.
- git diff --check passed; worktree remains clean. Installs: none.
- UI not checked by eye; owner should inspect Rows/Text layout, generator icon and replacement menu. Accessibility definitions checked, but no screen-reader validation.
- review/ → ready/; awaiting owner approval to ship. No linked GitHub issues or PRs.
