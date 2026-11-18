---
branch: factory/FT111-settings-default-width
round: 1
---
# Settings: increase the default panel width to prevent sidebar compression

## factory · 2026-11-18 14:58
Registered the owner's UI fix request in `new/`; implementation has not started.

Owner's request:
> the default width of settings panel is too small. need to increase, or the sidebar of settings panel will be compressed.

Requested behavior:
- Increase the settings panel's default width so its sidebar is not compressed on initial opening.
- Choose the width based on the existing sidebar and content layout; preserve usable space for both without changing the visual design unnecessarily.

Acceptance checks:
- Opening settings at its default size shows the sidebar at its intended width, with readable navigation labels and usable content.
- Resizing remains usable and the larger default respects the available screen space on smaller displays.
- Add appropriate regression coverage for the default size/layout; record any visual checks that could not be performed.

## factory · plan · 2026-11-18 15:08
Behaviour:
- Settings opens at a wider preferred size on ordinary desktop monitors, showing its 220px index and useful page area instead of immediately collapsing into the icon rail.
- Preserve the existing narrow-screen monitor clamp, the 900px narrow breakpoint, normal resizing, search and page selection; no configuration or service behavior changes. Missing/empty settings remain governed by existing defaults; no new error cases.
Scope:
- Update ui/settings.blp's 860px default width (below the 900px collapse breakpoint) to 1040px; regenerate ui/settings.ui and translation catalogs as needed. Existing sidebar/content design stays unchanged.
- Avoid unrelated responsive layout redesign, user configuration, compositor changes and new abstractions.
Decisions:
- Prefer 1040px: above the existing breakpoint, allowing roughly 820px beside the 220px sidebar before page padding. Retain height 620px. Keep monitor clamp rather than imposing a larger minimum width.
Checking:
- Verify source and generated default widths agree and remain above narrowSettingsWidth; inspect the unchanged small-monitor clamp and sidebar restoration on resize.
- Follow repository guidance against automated tests for a purely visual size change; document concrete manual regression steps (wide first opening, narrow monitor, shrink/expand).
- Run make generate, make i18n, and make test. UI is not checked by eye in this factory; explicitly record that limitation.

Plan addition after implementation inspection: remove the sample Hyprland rule’s forced 860×620 size, which otherwise overrides the wider default and monitor clamp. Keep floating and centering. This is directly required for the requested default to take effect with the sample config.

## factory · 2026-11-18 15:08
Built and pushed commit `6ce7142c` on `factory/FT111-settings-default-width`.
- Increased the default width from 860px to 1040px in Blueprint and generated UI. Retained 620px height, 220px sidebar, 900px narrow breakpoint and existing small-monitor clamp.
- Removed the sample Hyprland rule’s forced 860×620 size; it now respects each window’s preferred size and the shell’s monitor clamp. Existing installations retaining that size rule must remove it themselves; no user settings were modified. Other normal shell windows matched by this rule now keep their own preferred sizes too.
- No installs. `make generate` and `make i18n` succeeded; catalogs unchanged. `Hyprland --verify-config` reported config ok on isolated sample copies before and after the change.
- `make test` passed in both coder and manager verification (Python session tests, go vet, Go race tests), with throwaway HOME/XDG directories under the task temporary directory.
- No new automated tests: repository guidance excludes purely visual size changes. Source/generated widths were checked for agreement and against the collapse breakpoint.
- UI not checked by eye; no live windows or nested sessions were started. Manual checks remaining: open Settings on a wide monitor and confirm full sidebar/search and usable content; resize below/above 900px and confirm rail/restoration; open on a narrow monitor and confirm width fitting; exercise search and page selection at both sizes.
- First branch push failed because gh’s credential helper required a repository path; retried successfully with command-local credential.useHttpPath=true. No credentials were stored.

## factory · review · 2026-11-18 15:22
head: 6ce7142c88f9525863994883b0030d8a4ee6c243
verdict: pass
notes:
- Claude Code reviewed origin/master...HEAD (merge-base 02a6228d9c0c58385bcd0b0b94387cd247b47c35), all three changed files and affected callers. No verified findings.
- make test passed with isolated HOME/XDG/cache directories: 18 Python session tests, go vet ./..., and Go race tests. Recompiled Blueprint matches the committed settings.ui. Working tree remained clean; no installs.
- Verified 1040px default exceeds the 900px narrow breakpoint; 220px sidebar, monitor clamp and resize behavior remain unchanged. Checked other shell windows affected by removing the shared forced-size rule; no concrete regression found.
- UI not checked by eye. Manually check wide first opening, search/page selection, resizing across 900px, and narrow-monitor fitting (including scaled displays); post-Present monitor clamp behavior was not verified on a live display.
- Existing copied Hyprland configs retaining the forced 860x620 size still require removing that rule. No user configuration was changed.
- No linked GitHub issues or PRs in this item. Moved review/ → ready/; no merge or release performed.

## factory · 2026-11-18 16:24
Shipped on the owner's word. Merged into master as `3a3bc401` (Merge FT111) and tagged `v0.1.41`; `make test` passed on the merged master, and `make generate` and `make i18n` left it unchanged. No linked GitHub issues or PRs to close. The branch `factory/FT111-settings-default-width` was not deleted (`clean` is not in `ship`).
