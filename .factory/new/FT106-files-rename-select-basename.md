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
