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

## factory · review · 2026-11-07 13:23
head: 2ae2c1403e28caaeae60a87fc61d752aa81e29b9
verdict: fix
fix:
- [P1] internal/ui/appmenu.go:75: Chromium discovery returns an endpoint absent from registrar items, while appMenuCurrent checks only ForPID. Discovered menus therefore fail popup/activation guards. Private D-Bus reproduction read File and its submenu but recorded zero UI clicks versus one direct protocol click. Include discovered endpoints in current-menu validation and invalidate them on focus changes.
- [P1] internal/ui/appmenu.go:74: Chromium discovery synchronously scans D-Bus and introspects the focused process on the GTK main thread on every snapshot, before the equality guard. An unresponsive endpoint blocked each of two calls for approximately 2.001 seconds. Discover asynchronously, apply results through gtkx.Post with a focus check, and avoid repeated scans.
notes:
- [P2] internal/appmenu/registrar.go:347: the first child endpoint is selected without identifying the focused browser window. With two same-PID windows, reversing XML child order switched selection from /100 to /200. Match window identity or reject ambiguous endpoints.
- Codex reviewed the complete origin/master...HEAD diff (two files, 55 additions and one deletion). Branch merges cleanly with origin/master. Tracked files and HEAD unchanged.
- Validation: final make test passed with isolated HOME and runtime directory: Python 18 tests, go vet, and Go race tests (112 packages passed, including 96 cached; 34 without tests). Initial environment-only AppArmor/read-only-filesystem failure was resolved by correcting the isolated environment; no product changes. Temporary overlay reproductions confirmed the two menu defects and repeated timeout.
- Non-pinned installs: none. UI not visually checked; author's Chromium 156 manual test not independently verified. Native Wayland is outside scope.
