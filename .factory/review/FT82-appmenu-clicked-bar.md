---
source: pr #84
branch: factory/FT82-appmenu-clicked-bar
round: 1
---
# fix(appmenu): open the menu of the bar whose button was clicked

## factory · 2026-11-07 16:21
Imported GitHub PR #84 directly into review at the owner's request.
Source: https://github.com/lyricat/inua/pull/84
Upstream head: 1915f73e2b37e319c8d38ca787ae58c04e9582ff
Factory branch preserves the upstream head without changes.

The menu click handler previously searched all bars for the button's item ID, which could select another monitor's menu. The PR identifies the bar owning the button's parent box before looking up its menu item.

The author reports building and checking a two-monitor Kate/terminal setup. These checks have not been independently repeated by the factory.
No code review, tests, or visual UI checks were run during import. Awaiting review; not approved for merge.
