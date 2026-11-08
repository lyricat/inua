---
source: pr #88
branch: factory/FT87-appmenu-toml-packs
round: 1
---
# feat(appmenu): per-application menus from TOML packs

## factory · 2026-11-08 22:42
Imported GitHub PR #88 directly into review at the owner's request: “那把 #88 纳入 review”.
Source: https://github.com/lyricat/inua/pull/88
Upstream head: 8912c4630dc29b1a8722c3df7a5495c3fd00961d
Factory branch preserves the upstream head without changes.

Adds per-application TOML menu packs loaded from embedded files, XDG data directories and user configuration. Matches window class and desktop ID; supports shortcuts, key sequences, shell actions, submenus and disabled items. Provides fallback bar menus for applications that do not publish menus, shortcut keycap labels, shipped application packs and docs/appmenus.md describing user overrides.

Review should check pack parsing and precedence, application matching, fallback behavior versus published menus, shortcut and sequence delivery, disabled items and the compatibility of existing accessibility exceptions.

The author reports go build ./..., go vet ./internal/..., appmenu/UI package tests, and live Zed/Chromium checks; these claims have not been independently repeated by the factory.
No code review, tests, or visual UI checks were run during import. Awaiting review; not approved for merge.
