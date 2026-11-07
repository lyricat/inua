---
source: pr #81
branch: factory/FT79-appmenu-chromium-xwayland
round: 2
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

## factory · 2026-11-07 21:38
Fix round 2, at the owner's request in chat: "can you fix FT79?". The item stays in review/ for the next review run.

Fixed:
- [P1] appmenu.go:75, discovered menus failing the popup and click guards: Chromium's menu is now part of `Registrar.Focused` (registered, then an inua application's own, then Chromium's), and the bar keeps the menu found for the focused window. `appMenuCurrent` compares against that on the main thread with no D-Bus call; `showAppMenu` and `activateAppMenu` check `Registrar.Current` off the main thread before showing or clicking. A move to another window drops the found menu at once.
- [P1] appmenu.go:74, synchronous scan on the main thread at every snapshot: `findAppMenu` runs `Focused` on a goroutine only when the focused window changes or the registrar says menus changed, and applies the result through `gtkx.Post`, dropped when a newer search started. Snapshots for the same window (title changes and so on) no longer scan. This also takes FT75's `Native` scan off the main thread.
- [P2] registrar.go:347, first child chosen without window identity: Chromium's menus are rejected as ambiguous when the process serves more than one window's (under one connection or several), as `Native` already does; the compositor does not report the X window id. A scan that ran out of time also returns none.

Also: the registrar now tells of a change when a name under `io.github.lyricat.Inua.Menu.` comes or goes, since the bar no longer re-scans at every snapshot. Chromium discovery moved to `internal/appmenu/native.go`.

Decisions: a browser with several windows shows no menu rather than a guessed one (same rule as inua's own). Chromium's own LayoutUpdated signals are still not relayed (only registered menus' are); its first level is static in practice.

Commits: 06358115 merges origin/master (conflict in internal/ui/appmenu.go with FT75's `Focused`, resolved to master's side there; the fix commit then builds on it); 07d5e72e is the fix. Head: 07d5e72e57801f92e9a0c20ba693598bbe8dd9c1.

Tests: new `TestFocusedChromiumMenu` (found, read, clicked; registered menu first; two windows and two connections show none; replacement found) and `TestNativeNameTellsOfChange`. `make generate` and `make i18n` changed nothing. `make test` passed with a throwaway HOME (Python tests, go vet, Go race tests).

Not checked: the UI was not looked at, and no Chromium over XWayland was run here; the unresponsive-endpoint timing was not re-measured. Installs: none.

## factory · review · 2026-11-07 22:00
head: 07d5e72e57801f92e9a0c20ba693598bbe8dd9c1
verdict: pass
notes:
- [P2] internal/ui/appmenu.go:84: A missing Chromium menu remains cached until focus changes. Reproduction: same-PID endpoints /4000001 and /4000002 are rejected as ambiguous; removing the second, emitting LayoutUpdated and updating 20 snapshots still shows no menu, although Focused now returns /4000001. Invalidate discovery on relevant endpoint/window changes and provide bounded retries after no result.
- [P2] internal/ui/appmenu.go:357: Activation does not recheck focus after asynchronous Current validation. Reproduction: start item 17 on window A, block Current on D-Bus, focus B and clear A's cached menu, then release validation; A still receives clicked(17). Recheck window/request generation on the main thread after validation before sending the click.
- No P0/P1 product findings. Factory gate passes with P2 notes; the coder's generic fix recommendation is not the factory verdict.
- Codex reviewed all four files in origin/master (8db50f3)...HEAD. Merge check clean. Previous blocking guard and synchronous-discovery defects verified fixed: discovery returns asynchronously (~15 microseconds), repeated snapshots do not rescan, stale results are discarded, ambiguity rejected, unresponsive endpoint times out (~2.002 seconds).
- Final make test passed with isolated HOME/runtime: Python 18 tests, go vet, Go race tests (113 packages passed, 97 cached). Temporary overlay probes reproduced the P2 findings. Initial AppArmor read-only query failure also reproduced on base and resolved by correcting isolation; a probe synchronization error was corrected. No remaining product test failure.
- HEAD and tracked files unchanged. Installs: none. UI not visually checked; real Chromium/XWayland and browser endpoint lifecycle not integration-tested.
