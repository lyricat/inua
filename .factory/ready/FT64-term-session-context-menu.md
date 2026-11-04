---
branch: factory/FT64-term-session-context-menu
round: 1
---
# Terminal: context menu for a session with rename and close

## lyric · 2026-11-04 22:24
Add a context menu for a session: rename, and close with a confirmation, because closing a session closes all the panes in it. Workspaces already have a rename and close menu; sessions should get the same.

## factory · plan · 2026-11-05 01:09
Behaviour: right-click a session header or use Menu / Shift+F10 on its focused header for Rename and Close. Actions target that session ID, not the active session. Rename reuses the existing dialog. Close always confirms that all panes and running programs end, with Cancel focused. Cancellation changes nothing; missing state or vanished sessions do not act. Preserve selection and double-click rename.

Scope: native terminal session headers, menu and close confirmation, documentation and catalogs. Reuse workspace menu patterns, uikit and existing host APIs. No workspace redesign or web terminal changes.

Decisions: always confirm even single-pane session destruction; resolve stable IDs and validate current state before actions; use shared styling and translated strings.

Checking: make i18n before Go commits; make generate if layouts change; make test in a throwaway HOME. Manual checks: inactive session targeting, rename, cancel/confirm multi-pane close, keyboard menu and disappeared sessions. Add regression tests if non-UI logic changes.

## factory · 2026-11-05 01:09
Built c1ded70c: session-header context menu with Rename and Close, keyboard access, stable session targeting and confirmation before closing every pane. Updated terminal documentation and translation catalogs. Existing selection and double-click rename remain.

Installs: none; no dependency manifest changes. No Blueprint changes required generation.

Tests: make test passed in isolated HOME, independently rerun by manager. Coder also reports terminal and host builds passed. Only this item's commit is present; protected paths unchanged.

Not checked: UI not verified by eye. Attempted isolated Broadway check aborted on an assertion during first-frame rendering; menu, keyboard and confirmation interactions still need manual verification.

Moved new/ → review/ for review, not merged into master.

## factory · review · 2026-11-05 01:24
head: c1ded70cfeb2d961a498d22be8fc60925b4ba07b
verdict: pass

Validation: Codex reviewed all 10 changed files in origin/master...HEAD, including session ID targeting, confirmation/cancellation, event handling and GTK reference/callback lifetime. No confirmed introduced defects. No merge conflicts. Required make test passed on final rerun: 15 script tests, go vet and full race suite. Initial long Unix socket paths caused failures also reproduced on base (17 failed tests); shortening isolated paths with a /proc alias resolved the environment issue without code changes. Temporary catalog regeneration matched; formatting and diff checks passed. Worktree unchanged; no commits or installs.

Limits: UI was not checked by eye; real menu, keyboard and confirmation interactions remain manual checks. libinua-vt.so was unavailable, so related tests skipped under existing rules. New warning text is not yet translated and falls back to English.
Moved review/ → ready/ for owner approval; not merged into master.
