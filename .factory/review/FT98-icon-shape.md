---
source: pr #94
branch: factory/FT98-icon-shape
round: 1
---
# feat(style): choose the shape of app icons in the dock and launcher

## factory · 2026-11-10 08:36
Imported GitHub PR #94 directly into review at the owner's request: “全部放到 review”.
Source: https://github.com/lyricat/inua/pull/94
Upstream head at import: 59cbffafcf104dc626dc21da32a0503bc7deca30
Uses the existing PR branch; no code changes made during import.

Review the PR against the current base, including its requested behavior, regressions and tests.
No review, tests or visual UI checks run during import. Awaiting review; not approved for merge.

## Lyric · 2026-11-10 09:53
先把 FT92~FT100移动到 backlog

## factory · 2026-11-10 13:44
Moved from backlog to new at the owner’s request. Confirmed source PR #94 is open and its head is iamcheyan/inua:pr/icon-shape.
Removed the imported fork branch and round headers: no factory build has run for this item. Keep source: pr #94; the next build must fetch origin pull/94/head and create factory/FT98-icon-shape from that PR head, not look for the fork branch in the main repository.
This update changes the queue only; no build, tests, review or merge performed.

## factory · plan · 2026-11-10 15:42
Behaviour: missing icon_shape keeps squircle at first run; squircle/circle/pebble/none persist and apply to existing and new dock, drawer and launcher icons without hover. Invalid or empty values fall back with the normal warning. None bypasses shape, plate, light and clipping; empty icons remain safe.
Scope: integrate PR #94, correct outline and redraw issues, configuration/geometry regression tests, docs and generated-file consistency. No unrelated UI or light-app propagation.
Decisions: reuse shared AppIcon and configuration mechanism; GTK main-thread invalidation; squircle preserves the existing default. Inspect and correct pebble edge/corner command ordering while retaining the proposed radii.
Checking: defaults, valid values, invalid values and round trips; shape-name and outline logic; inspect lifetime and cache invalidation. Run make generate, make i18n before Go/Blueprint commits, and make test in isolated HOME/XDG/runtime. UI is not checked by eye here.

## factory · 2026-11-10 15:42
Built from current PR #94 head cc97106c503ee763d5a3ba3e7076aa0a6b378639 (already contains origin/master). Pushed head 80a94efa on factory/FT98-icon-shape; moved new → review, round 1.
Fixed pebble geometry (straight sides, quarter-circle corners), avoided stylesheet/theme reloads on shape-only changes, made repeated shape assignment a no-op, removed duplicate shape names, added configuration and path regression coverage, and documented the four shapes. Manager requested reverting the coder's out-of-scope light-app propagation; committed that correction without rewriting history.
Installs: none; no dependency changes. make generate and make i18n left generated files/catalogs unchanged.
Tests: coder and manager independently passed make test on final head with throwaway HOME/XDG/runtime and session/compositor addresses unset: 18 Python tests, go vet and race-enabled Go suite. No baseline failures. Worktree clean; no forbidden paths changed against PR start point.
Not checked: UI was not checked by eye; Appearance dropdown and immediate redraw inspected in code only. Pebble radii retain the PR choices but visual suitability after geometry correction remains to check.
Push initially failed because gh credential helper required repository path; retried once with command-local credential.useHttpPath=true and succeeded. No persistent Git settings changed. No review or merge performed.
