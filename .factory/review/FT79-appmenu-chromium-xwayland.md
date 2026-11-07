---
source: pr #81
branch: factory/FT79-appmenu-chromium-xwayland
round: 1
---
# feat(appmenu): read Chromium's own menu over XWayland

## factory · 2026-11-07 12:03
Added directly to review at the owner's request: “Move GitHub issue 79, 80, 81,82,83 to review status”. GitHub #81 is an open pull request.

Source: https://github.com/lyricat/inua/pull/81
PR head: 2ae2c1403e28caaeae60a87fc61d752aa81e29b9
Original branch: pr/chromium-menu
The exact PR head is preserved on `factory/FT79-appmenu-chromium-xwayland`. No code changes, review, or tests were performed during this status change. Verification statements below are the PR author's claims, not independently checked here.

PR description (quoted):
> Chromium/Chrome exposes a complete com.canonical.dbusmenu at /com/canonical/menu/<window> on its own bus name when it runs through XWayland, but it does not always register that endpoint with com.canonical.AppMenu.Registrar (the session's desktop name is not Unity's). The bar now looks for that menu on a bus name owned by the focused PID, so the real File/Edit/View/… sections appear.
> 
> Native Wayland Chromium does not publish this menu (its AT-SPI tree has empty frames), so the menu needs Chromium started on XWayland to be available.
> 
> Verified against Chromium 156 running with --ozone-platform=x11: seven top-level menus were read and shown; the menu also opened its items.
