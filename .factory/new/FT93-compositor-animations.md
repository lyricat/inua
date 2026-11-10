---
source: pr #78
branch: factory/FT93-compositor-animations
round: 1
---
# feat(compositor): apply the animation setting through each backend

## factory · 2026-11-10 08:36
Imported GitHub PR #78 directly into review at the owner's request: “全部放到 review”.
Source: https://github.com/lyricat/inua/pull/78
Upstream head at import: fb273f188aa7daea8211091a472e01b78b27ea10
Uses the existing PR branch; no code changes made during import.

Review the PR against the current base, including its requested behavior, regressions and tests.
No review, tests or visual UI checks run during import. Awaiting review; not approved for merge.

## Lyric · 2026-11-10 09:53
先把 FT92~FT100移动到 backlog

## factory · 2026-11-10 13:44
Moved from backlog to new at the owner’s request. Confirmed source PR #78 is open and its head is iamcheyan/inua:feat/compositor-animations.
Removed the imported fork branch and round headers: no factory build has run for this item. Keep source: pr #78; the next build must fetch origin pull/78/head and create factory/FT93-compositor-animations from that PR head, not look for the fork branch in the main repository.
This update changes the queue only; no build, tests, review or merge performed.

## factory · plan · 2026-11-10 14:08
Behaviour: pass the global animation preference to each supported compositor; explicit false disables supported motion, true restores it, and an unset backend value leaves compositor settings alone. First writes work without a running compositor. Unchanged files avoid writes and reloads; changed Lua configuration explicitly reloads. Return file and IPC failures without preventing unrelated Wayfire settings from applying.
Scope: integrate PR #78 with current master; animation plumbing, backend rendering/application, tests and relevant docs only. No new UI or unrelated features.
Decisions: reuse style.animations and optional backend bool; set only animation options Wayfire advertises, without enabling the magnifier. Preserve existing master changes and its intentionally empty generated sample.
Checking: nil/true/false propagation and render cases; first/unchanged Lua writes, reload failures; supported/missing Wayfire options and input isolation. Run make test in a throwaway HOME, make i18n for Go changes, make generate only if layouts change. Visual behavior requires a separate nested-session check.

## factory · 2026-11-10 14:08
Built from PR #78 head fb273f188aa7daea8211091a472e01b78b27ea10; merged origin/master without conflicts. Pushed head ebb85a7e on factory/FT93-compositor-animations. Moved new → review, round 1.
Hardened Hyprland empty-config comments and Lua reload regressions; retained master's empty sample; fake socket uses sockdir. Preserved unset animation values in compsettings. Wayfire applies animation options separately after unrelated settings, and excludes zoom (screen magnification). Added regression tests and backend documentation.
Installs: none. make i18n left catalogs unchanged; no Blueprint edits, so make generate not needed.
Tests: coder make test passed (18 Python tests, go vet, race-enabled Go suite); manager independently reran make test with throwaway HOME/runtime and no session bus: exit 0. Worktree clean; no never paths changed against the PR start point. No baseline failures observed.
Not checked: visual UI/animation behavior was not checked by eye; niri and Wayfire are not installed, and installed Hyprland only verifies classic config, not Lua. IMPORTANT for review: actual Wayfire animation option availability is unverified; stock Wayfire may not advertise these enabled options, in which case this implementation safely skips them but does not disable its animations. Check this functional limitation before approving. Hyprland explicit/default true overrides an off setting in the user's compositor config; niri emits only the disabled block.
