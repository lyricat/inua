---
branch: factory/FT106-files-rename-select-basename
round: 1
---
# Files: select the filename without its extension in the rename dialog

## factory · 2026-11-13 00:38
Owner request: In inua Files, when opening the rename dialog, select the filename by default without selecting the extension. For example, `abc.jpg` should initially select only `abc`, leaving `.jpg` unselected.

The owner requested this item be placed in `new`; no implementation is requested in this board update.

Acceptance criteria:
- The rename entry receives focus with the basename selected, excluding the final extension and its separating dot (`abc.jpg` → `abc`).
- Names without an extension and directory names are selected in full.
- A leading dot alone does not count as an extension (`.gitignore` is selected in full); for multiple extensions, exclude only the last extension (`archive.tar.gz` → `archive.tar`).
- Selection uses character positions correctly for non-ASCII filenames.
- Rename submission, cancellation and validation retain their existing behaviour.
- Add regression coverage for the selection boundaries.

## factory · plan · 2026-11-13 01:15

### Behaviour
- Opening a single-item Rename card focuses the entry and selects the name except its final extension and dot for files; no-extension files, leading-dot-only names and directories are selected in full.
- Multi-extension names retain only the last extension outside selection; positions count Unicode characters, not bytes.
- An empty name produces an empty selection safely; submission, validation, cancellation and first-run behaviour are unchanged. If directory metadata cannot be read, preserve the existing file-stem fallback rather than blocking rename.
### Scope
- Adjust the existing cards.go selectStem path and directory distinction only as needed; reuse existing SplitExt semantics where correct. Add focused regression coverage of boundaries. No batch rename, styling, operation or validation changes.
### Decisions
- Existing openCard already selects a stem after GrabFocus: inspect whether GTK focus/default selection overwrites it, and fix ordering only if needed. Ensure directory names containing dots are not treated as extensions.
- Use os.Stat for directory classification (including directory symlinks) unless the existing model supplies equivalent data; failure falls back to file handling. A final trailing dot with no extension selects the full name.
- Keep selection computation toolkit-free/testable without adding architecture layers.
### Checking
- Table tests: abc.jpg, archive.tar.gz, no extension, .gitignore, .config.json, dotted directories, trailing dots, empty names, non-ASCII and combining/emoji names.
- Run make i18n before each Go commit; make generate only if Blueprint changes. Run make test under a throwaway HOME, with caches supplied explicitly if needed.
- UI is not checked by eye in this factory; explicitly record this. Do not touch the user's running session.

## factory · 2026-11-13 01:15
Built and pushed commits 268c4164 and 02fb7fe7; head: 02fb7fe7. Moved new/ → review/.

- Added toolkit-free RenameSelection and wired the Rename card to character-count selection. Files exclude only their final extension, unlike the previous compound-extension SplitExt; dotted directories and symlinks to directories select their full names.
- Leading-dot-only names and trailing-dot names select in full. Directory classification uses os.Stat; errors fall back to file handling. Extensions containing whitespace still count as extensions; a speculative sentence-name heuristic was removed before completion.
- Entry focus uses GrabFocusWithoutSelecting before applying the explicit range. New-folder cursor behaviour, validation and operations remain unchanged. Updated Files documentation and regenerated translation references with make i18n; no Blueprint edits.
- Added table regression coverage for extension boundaries, empty names, directories and Unicode character positions. Broadway integration covers focus/selection persistence, F2/context-menu entry, cancellation and rename submission.
- Installs: none (only repository-pinned module restoration).
- Manager independently ran make test successfully with throwaway HOME/XDG directories: Python session tests, go vet, and Go race tests pass. git diff --check passes; protected paths unchanged.
- Not checked by eye: UI appearance and real keyboard/mouse input; automated Broadway callbacks are not visual inspection. Remote/rclone directory classification was not checked. Existing GTK context-menu finalization warnings were observed by the coder and were not changed.
- First push could not supply credentials without an HTTP repository path; the one retry succeeded with command-local credential.useHttpPath=true. No credential or user settings changed.

## factory · review · 2026-11-13 01:21
head: 02fb7fe7ee60b26be5a1e0f0939ecc9440ea3cc6
verdict: pass
notes:
- Claude Code reviewed origin/master...HEAD against the request and plan; base/merge base: 624b5edb515837932214529cd5a3c65abfbaae81. All 13 changed files inspected; no qualifying P0/P1/P2/P3 findings.
- make test passed under throwaway HOME/XDG/TMPDIR: Python session tests, go vet and Go race tests. TestRenameSelection and TestRenameCardSelectsStem also passed uncached; Broadway was available and the GTK integration test ran, not skipped.
- Unicode boundaries, final-extension selection, full directory selection including directory symlinks, focus persistence, F2/context-menu entry, cancellation and submission covered. Catalog differences are source references only and were checked against source; make i18n was not rerun during read-only review.
- Installs: none. Tracked worktree stayed clean; git diff --check passed. Branch merges cleanly with current master. Manager additionally read docs/architecture.md and design/ui.md, which the coder omitted.
- Not checked by eye: UI appearance or real keyboard/mouse input. Remote/rclone/FUSE directory classification and slow-mount behavior were not exercised; broken symlinks use the planned file fallback.
- Moved review/ → ready/. Owner should check the rename interaction visually before shipping.

## factory · 2026-11-13 07:44
Shipped on the owner's request after confirming the ready head still matches its passing review.
- Merged into master without conflicts: 1080a04039ec6d1b2a577d201e1b16eab83cfe5c; release tag: v0.1.37. Both pushed atomically.
- make generate and make i18n passed and left tracked files unchanged. make test passed under throwaway HOME/XDG directories (Python session tests, go vet and Go race tests).
- Uncached race-enabled TestRenameSelection and TestRenameCardSelectsStem passed on merged master; Broadway GTK integration ran, not skipped.
- No linked GitHub issues or PRs to close. Factory branch retained (clean is not configured). Installs: none.
- UI was not checked by eye; no real desktop input, deployment or restart. Remote/rclone/FUSE classification and slow mounts were not exercised.
- Moved ready/ → done/.
