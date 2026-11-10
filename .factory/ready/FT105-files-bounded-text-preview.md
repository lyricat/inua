---
branch: factory/FT105-files-bounded-text-preview
round: 1
---
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

## factory · plan · 2026-11-10 18:11
Behaviour: A single regular plain-text selection shows a read-only sidebar preview asynchronously, stopping actual reads at 200 lines or 64 KiB, whichever comes first. Clear old content on selection changes and ignore stale results, including after close. Show truncation and empty states; handle invalid UTF-8, binary data, errors and deleted files inline. Avoid blocking on special files. Preserve folder, multi-selection and image details; no first-run setup.

Scope: Replace textHead with toolkit-free bounded reading in internal/apps/files and wire the existing details sidebar. Document limits. No editing, spreadsheet renderer, full-file loading, settings or unrelated UI work.

Decisions: Fixed 200-line / 65536-byte defaults; UTF-8 with incomplete final rune trimmed only at a byte cutoff. MIME plus conservative CSV/log/text extension fallback. Plain text only, never markup. File size determines truncation without reading past either limit; unknown sizes use a conservative indication. Atomic generation guards prevent stale updates and the prior folder-size counter race.

Checking: Count actual reader bytes for independent limits, exact boundaries, long lines, multibyte splits, invalid UTF-8/binary, empty/deleted/error files and special files. Run make test under a throwaway HOME. Manually check CSV/log/image transitions, rapid selection changes and closing during a preview; UI is not checked by eye here.

## factory · 2026-11-10 18:11
Built and pushed at 506d3234 (initial implementation 49fd9951). Bounded plain-text reader, translated sidebar states, atomic stale-result guards, documentation and regression tests implemented. Manager required a follow-up fix to eliminate 4 KiB read-ahead past the line limit; actual consumption now stops exactly at the 200th newline or 64 KiB.

Tests: make test passed in the coder run and independently in manager verification with a throwaway HOME (Python session tests, go vet and Go race tests). make i18n ran before Go commits; no Blueprint changes. No unpinned installs or dependency changes. A preliminary test invocation failed due to a non-canonical temporary HOME; rerunning with a canonical path passed.

Not checked: UI appearance, rapid selection/close interaction and existing image previews by eye; no nested session run. Stale-selection GTK wiring and file-type swap races were inspected, not exercised by automated tests. Special-file timeout and bounded-reader regression tests passed.

The initial branch push failed because the credential request omitted its repository path; the single retry with per-command credential.useHttpPath=true succeeded. No global credential settings changed.

Moved new/ → review/ for round 1. Owner/reviewer should inspect sidebar presentation and selection transitions.

## factory · review · 2026-11-10 18:20
head: 506d3234b7b57004fc1a73c439934a1cf5ac38ba
verdict: pass
notes:
- Claude Code reviewed origin/master...HEAD against the request and plan; no verified introduced defects found. Base merges cleanly; no code changes or commits made during review.
- Validation: make test passed under an isolated canonical throwaway HOME/runtime environment (18 Python session tests, go vet, Go race tests). Additional counted-reader probes covered independent limits, exact boundaries, a long line, BOM-only input and a split UTF-8 character. make i18n in a disposable tracked-files copy reproduced catalogs without changes. Manager confirmed unchanged head and clean worktree.
- No installations or downloads. No linked issues or PRs to comment on.
- UI not checked by eye. GTK stale-result wiring and file-type swap races were inspected, not exercised. Remote-mount performance and 64 KiB GTK label layout were not measured; in-flight slow FUSE reads are ignored when stale rather than cancelled.
- Moved review/ → ready/. Owner should inspect sidebar appearance and CSV/log/image selection transitions before shipping.
