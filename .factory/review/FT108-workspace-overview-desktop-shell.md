---
source: pr #97
branch: factory/FT108-workspace-overview-desktop-shell
round: 1
---
# Improve workspace overview and desktop shell

## factory · 2026-11-14 23:50
Imported https://github.com/lyricat/inua/pull/97 directly into review/ at the owner's request, superseding the earlier request for new/.
PR head: 31499073b12d07d6a0af758693bb23c1c8912bdb. Preserved on the dedicated factory branch; the PR's head branch is named master in its source repository.

Scope reported by the PR: unified app/window search in overview, per-monitor preview sizing, wallpaper-backed new-workspace tile, blurred backdrop, constant-width preview borders, desktop areas filling cards, and configurable other-monitor focus outlines hidden during overview. Includes icons-only dock configuration, hover switching of open top-level application menus, per-monitor menus that focus the owning window before opening, and adjacent submenus. The PR also reports dock, bar, application menu, icon-shape and screenshot changes already on master; review should distinguish new changes from existing base code.

PR-author validation (not independently rerun during import): live two-display Hyprland overview check; targeted UI/uikit/shell and Hyprland tests, make i18n, and git diff --check reported passing. Full make test reported failing on mounted-filesystem trash tests, legacy-config Lua parsing, and terminal integration timeouts/permission errors. No nested-session validation or screenshots were provided.

Pending factory review, including comparison with current master and verification of the reported test failures. This import performs no code review, tests, merge or release.
