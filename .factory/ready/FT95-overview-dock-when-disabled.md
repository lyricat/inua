---
source: pr #91
branch: factory/FT95-overview-dock-when-disabled
round: 1
---
# fix(dock): show the dock in the overview while it is off the desktop

## factory · 2026-11-10 08:36
Imported GitHub PR #91 directly into review at the owner's request: “全部放到 review”.
Source: https://github.com/lyricat/inua/pull/91
Upstream head at import: 8a6e94ecab43648495f042281d135a17c788c996
Uses the existing PR branch; no code changes made during import.

Review the PR against the current base, including its requested behavior, regressions and tests.
No review, tests or visual UI checks run during import. Awaiting review; not approved for merge.

## Lyric · 2026-11-10 09:53
先把 FT92~FT100移动到 backlog

## factory · 2026-11-10 13:44
Moved from backlog to new at the owner’s request. Confirmed source PR #91 is open and its head is iamcheyan/inua:pr/overview-dock-when-disabled.
Removed the imported fork branch and round headers: no factory build has run for this item. Keep source: pr #91; the next build must fetch origin pull/91/head and create factory/FT95-overview-dock-when-disabled from that PR head, not look for the fork branch in the main repository.
This update changes the queue only; no build, tests, review or merge performed.

## factory · plan · 2026-11-10 15:07
Behaviour: build a dock when desktop or overview requests it; keep the overview-only dock hidden at startup and after close, and show it on overview monitors without resizing tiled windows. Preserve desktop/autohide behavior and reserve overview layout footprint independently of desktop enable. Repeated opens, config reload, monitor changes, missing layer-shell and empty app lists must remain safe.
Scope: integrate PR #91 with current master; dock lifecycle, exclusive zones, overview insets, related regression tests and docs. No new settings or unrelated changes.
Decisions: reuse ShowDock; reserve the desktop edge only when desktop dock is enabled without autohide. Overview spans all monitors, so show the dock on all overview monitors, matching existing behavior.
Checking: logic matrix for enable/show_dock/autohide/layer-shell and overview insets; trace startup, config rebuild and close paths. Run make test in isolated HOME/runtime and make i18n; manual nested-session checks remain required.

## factory · 2026-11-10 15:07
Built with Claude Code from PR #91 head 8a6e94ecab43648495f042281d135a17c788c996; merged origin/master without conflicts. Pushed d78d687d on factory/FT95-overview-dock-when-disabled. Moved new → review, round 1.
Preserved the PR's overview-only dock with zero exclusive zone. Prevented startup mapping/flash, closed its drawer/menu and cleared hover state on overview close, and prevented drawer IPC opening against a hidden dock. Shared build/exclusive predicates, added regression coverage, and updated config comments and docs.
Tests: coder and manager independently passed make test (18 Python tests, go vet, race-enabled Go suite) with throwaway HOME/XDG/runtime and live session variables unset. make i18n run before Go commits; generated catalog changes are source references. No Blueprint changes; make generate unnecessary. Worktree clean and no forbidden paths changed against PR start point. Installs: none.
Not checked: UI and animation behavior were not checked by eye; nested-session verification remains for startup hiding, repeated overview open/close, stable tiled-window geometry, drawer closing, settings toggles and multi-monitor behavior. The PR author's earlier live check is not this build's verification.
First push lacked the credential repository path; retried successfully with command-local credential.useHttpPath=true. No user git settings changed.


## factory · review · 2026-11-10 15:20
head: d78d687d9e71550d0281fd1d1fb9722c0a130fb6
verdict: pass
notes:
- Claude Code reviewed origin/master...HEAD against the request and plan; no confirmed P0–P3 findings. Base merges cleanly; no code changes or commits made during review.
- make test passed in isolated HOME/XDG/runtime with live session variables absent: 18 Python tests, go vet, and race-enabled Go tests. Manager checked the clean worktree, reviewed head and successful test log.
- Catalog source references were checked against code and all six locales. make i18n was not rerun: preparing a scratch copy failed before generation; no full regeneration claim is made.
- Installs: none. UI was not checked by eye; startup hiding, repeated open/close, stable tiled geometry, drawer/menu closing, autohide animations, settings toggles and monitor hot-plug still need nested-session verification.
- Moved review → ready. No merge to master or release.
