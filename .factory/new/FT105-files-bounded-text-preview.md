# Files: bounded plain-text preview in the right sidebar

## factory · 2026-11-10 16:26
Registered from the owner's Files UI request. Backlog only; no build started.

User request: "preview plain text file(csv, log, etc) at the right sidebar, only preview for limited rows and limited bytes."

- Preview plain-text files, including CSV and log files, in the right sidebar as read-only text; CSV does not require a spreadsheet renderer.
- Enforce BOTH a line limit and a byte-read limit, stopping at whichever is reached first. Do not read the whole file and truncate afterward. A single extremely long line must remain bounded.
- Proposed defaults (not user-mandated): 200 lines and 64 KiB read per preview. Confirm exact constants in the build plan and visibly indicate truncated previews.
- Keep the UI responsive; cancel or ignore stale reads when the selection changes so the previous file's preview cannot replace the current one.
- Handle empty files, invalid text encoding, binary files, read errors and deleted files gracefully; do not execute or interpret file contents as markup. Avoid blocking on special files such as FIFOs/devices.
- Check both limits independently, very long lines, multi-byte characters at the byte boundary, rapid selection changes and existing non-text previews.

## factory · 2026-11-10 16:28
Moved from backlog to new at the owner’s request: "ok. move them to new". Queued for build; no development started in this board update.
