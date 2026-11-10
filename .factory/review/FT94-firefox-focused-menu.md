---
source: pr #90
branch: factory/FT94-firefox-focused-menu
round: 1
---
# feat(appmenu): read Firefox's own menu in the focused lookup

## factory · 2026-11-10 08:36
Imported GitHub PR #90 directly into review at the owner's request: “全部放到 review”.
Source: https://github.com/lyricat/inua/pull/90
Upstream head at import: 403460e12e0d84d713a298c5411419f3de22c527
Uses the existing PR branch; no code changes made during import.

Review the PR against the current base, including its requested behavior, regressions and tests.
No review, tests or visual UI checks run during import. Awaiting review; not approved for merge.

## Lyric · 2026-11-10 09:53
先把 FT92~FT100移动到 backlog

## factory · 2026-11-10 13:44
Moved from backlog to new at the owner’s request. Confirmed source PR #90 is open and its head is iamcheyan/inua:pr/firefox-focused-menu.
Removed the imported fork branch and round headers: no factory build has run for this item. Keep source: pr #90; the next build must fetch origin pull/90/head and create factory/FT94-firefox-focused-menu from that PR head, not look for the fork branch in the main repository.
This update changes the queue only; no build, tests, review or merge performed.

## factory · plan · 2026-11-10 14:46
Behaviour: resolve the focused Firefox process's unambiguous menu after registered/native menus and before Chromium. No PID, missing bridge/process, malformed data and bus errors fail safely. Read a bounded complete tree once, opening and activating entries from that snapshot; discard stale entries on focus/refresh.
Scope: Firefox discovery, full-tree handling, bar integration, documentation and regression tests. No unrelated features or configuration.
Decisions: ambiguous multi-window menus are not guessed; non-Firefox providers retain lazy submenus; matching is restricted to the process itself or a same-program ancestor, not arbitrary relatives.
Checking: private D-Bus and process fixtures for discovery, ambiguity, failures, precedence and changing item IDs; make i18n and make test with a throwaway HOME. UI requires later visual/live Firefox checking.

## factory · 2026-11-10 14:46
Built from PR #90 head 403460e12e0d84d713a298c5411419f3de22c527. Merged origin/master without rebase, preserving newer bar-menu handling. Head: bbd82b14; implementation: 35d07410; merge: 116ac3d4.
Firefox aliases on one connection are deduplicated; multiple window menus/connections fail closed. Matching excludes terminals/shells and unrelated launched programs. Full-tree menus refuse separate submenu reads; clicks and reads are serialized and open menus close before refresh. Firefox name appearance/disappearance triggers lookup. Updated docs/appmenus.md and added fake-bus/process regression coverage.
Tests: coder and manager independently passed make test with throwaway HOME (18 Python tests, go vet, Go race tests). make i18n completed; no layout changes. No unpinned installs or manifest changes. Protected paths unchanged. Code branch pushed.
Not checked: UI was not checked visually and real Firefox was not exercised. Bar GTK behavior lacks automated coverage. Multi-window Firefox deliberately shows no menu; empty full-tree submenus stay empty; layout signals are not followed (refresh on refocus/click); trees beyond 4096 entries fail; proxy-owned Flatpak bridges may not match.
Initial push needed command-local credential.useHttpPath=true; retry succeeded without changing user settings.
Moved new/ → review/ for round 1. Not reviewed or approved for merge.
