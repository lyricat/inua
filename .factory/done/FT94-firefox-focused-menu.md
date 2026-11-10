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

## factory · review · 2026-11-10 14:56
head: bbd82b145d492058d1939dbeca7d00c84ab8926b
verdict: pass
notes:
- [P2] internal/appmenu/firefox.go:117: A separate Firefox B launched by same-program parent A can receive A's menu when B owns a Firefox bus name but exposes no menu. Reviewer fake-process/bus probe confirmed Focused(B) returns A's menu, so actions target A. Collect all owner PIDs and stop ancestor matching at the nearest Firefox owner; add regression coverage.
- [P2] internal/appmenu/firefox.go:80-90: A matching Firefox owner whose introspection times out reports seen=false, allowing Chromium fallback to rediscover the same bridge as Whole=false. Reviewer probe with a 2.2-second first introspection confirmed this result, violating snapshot-only reads and causing stale IDs or rejected submenu actions. Treat a matched but unreadable owner as seen, or exclude Firefox owners from Chromium discovery.

Claude Code reviewed origin/master...HEAD (six changed files). No P0/P1 findings; pass follows factory severity policy, not absence of defects. make test passed with isolated HOME/XDG/runtime and no live session (18 Python tests, go vet, Go race tests); Firefox D-Bus tests ran. make i18n in a throwaway copy produced no changes. Both P2 cases were reproduced in throwaway-copy probes. Worktree unchanged; no installs or manifest changes. Branch merges cleanly with origin/master.
Residual risks: no visual UI, GTK interaction or real Firefox verification. Failed whole-tree refresh may retain stale entries; not promoted to a confirmed finding without a concrete UI reproduction. Real Firefox ID renumbering, dynamically populated menus, proxy-owned Flatpak bridges and layout-signal behavior remain unverified or documented limitations. Multi-window ambiguity deliberately suppresses menus.
Moved review/ → ready/. Owner should examine the two P2 findings before shipping. No merge to master.

## factory · 2026-11-10 20:03
Owner-authorized ship: merged into master at a0a92061e39600db12c1f60d9fbc8a7650788cf8. Released and pushed atomically in v0.1.32. Moved ready → done.
PR #90 already merged.
Validation: make test passed after this merge (18 Python session tests, go vet, Go race suite); isolated HOME/XDG/runtime. make i18n completed; make generate also verified for FT104. Resolved docs/appmenus.md conflict preserving Firefox and accessibility lookup order.
Owner explicitly accepted the reported P2/P3 findings; they remain unresolved, not fixed by this ship. Prior review notes and limitations remain applicable. No visual UI or live/nested desktop verification. No deployment, unpinned installs or dependency changes.
