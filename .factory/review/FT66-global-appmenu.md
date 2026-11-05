---
source: pr #66
branch: factory/FT66-global-appmenu
round: 2
---
# Global appmenu (DBusMenu) for bar

## factory · 2026-11-04 22:57
Added directly to review at the owner's request: PR 66也加进去

PR: https://github.com/lyricat/inua/pull/66
Base: master
Head at intake: 087cd11b4e6ab9cc50736a134f0a1171b1f15191
Original branch: iamcheyan/inua feat/appmenu-global-menu
The PR head is mirrored unchanged to factory/FT66-global-appmenu in the base repository for the review workflow.

PR description: Adds a global appmenu to the bar using DBusMenu Registrar. Includes internal/appmenu/registrar, ui/appmenu, bar integration, tests.

This intake did not run tests or perform a code review. Review is pending; UI behaviour has not been checked by eye.

## factory · review · 2026-11-04 23:27
head: 087cd11b4e6ab9cc50736a134f0a1171b1f15191
verdict: fix
fix:
- [P1] internal/ui/appmenu.go:61: bar lacks appMenuGen, appMenu and appMenuReg fields and widget binding; make test fails go vet with b.appMenuGen undefined. Complete bar integration.
- [P1] internal/ui/appmenu.go:199: each rebuilt action allocates a new native callback that cannot be reclaimed. Rebuilding 100 actions crashes on round 21 with purego callback limit exhaustion even when GIO objects are released. Share long-lived handlers and resolve targets by action identity.
notes:
- [P2] internal/appmenu/registrar.go:123: unchecked signal sender allows a directed forged NameOwnerChanged to unregister an online victim (reproduced for window 42). Validate bus sender, path and argument types.
- [P2] internal/appmenu/registrar.go:150: WindowRegistered emits (window,path), signature uo, instead of (window,service,path); standard three-field decoding fails. Fix emission and introspection XML.
- [P2] internal/appmenu/registrar.go:197: registering windows 42 and 43 from one PID at the same /Menu endpoint makes ForPID return false and hides the menu. Deduplicate identical endpoints and map distinct endpoints reliably.
- [P2] internal/appmenu/registrar.go:211: only AboutToShow(0) is called and children-display is discarded; a lazy submenu 7 never gets AboutToShow(7) and becomes a plain item. Preserve submenu metadata and load on opening.
- [P2] internal/ui/appmenu.go:37: focus snapshot updates never call syncAppMenu; switching from registered A to registered B without a registration event leaves A's menu displayed and actions rejected. Synchronize on focus/monitor changes.
- [P2] internal/ui/appmenu.go:124: cached menu ignores LayoutUpdated/ItemsPropertiesUpdated and opening does not refresh it; private-service LayoutUpdated(2,0) produces zero update callbacks. Subscribe with sender validation or reload on opening.

Validation: Codex reviewed all 13 changed files in origin/master...HEAD, merge base 4ad7126c07cc81346cec017610d1ca3bd4508935. No merge conflicts. make test failed (exit 2): 15 Python tests passed, vet failed compilation, full race suite was not reached. Targeted go test -race -count=1 -v ./internal/appmenu passed both existing tests. Seven isolated checks covered six defect reproductions and correct cross-client unregister rejection. git diff --check passed; worktree unchanged. Tests used throwaway HOME and private services. Installs: none.
Limits: no manual visual UI check or live compositor integration. Native object lifecycle on monitor hotplug and resource exhaustion from large DBus messages/registration floods were not runtime-verified.
Remains in review/ for the owner's decision; add a note and move to new/ for fixes.

## factory · 2026-11-05 07:54
Author comment copied at the owner's request.

Author: iamcheyan
Source: https://github.com/lyricat/inua/pull/66#issuecomment-6643993871
Posted: 2026-11-04T22:40:46Z

> I updated  to merge the latest upstream/master and fixed the compile issue from the factory review ( now uses  correctly). The branch builds and , ok  	github.com/lyricat/inua/internal/ui	(cached)
> ok  	github.com/lyricat/inua/internal/appmenu	(cached)
> ok  	github.com/lyricat/inua/internal/style	(cached) pass locally. However, the PR CI / failures are still pre-existing in upstream's test environment ( Unix socket path-length issue on macOS, plus  failures), not introduced by this PR.

The author reports merging latest upstream/master, fixing compilation and passing local tests, and attributes the remaining PR CI failures to upstream test-environment issues. The source comment contains missing identifiers; the quote above preserves it as returned by GitHub without reconstruction. These are the author’s claims, not newly verified test results. This update only records the comment; no re-review was performed and the existing verdict and review/ status are unchanged.

## factory · 2026-11-05 10:05
Fix round 2, done at the owner's request ("just fix at the PR's br") on the PR's own branch.

Checking the author's comment first: PR #66's head had not moved from 087cd11; the update it describes went to another fork branch, feat/global-app-menu (26434dc), a larger and separate line of work that does not contain the PR head. That branch compiles but fails make test (internal/config TestSampleIsFormat, internal/dbusexport TestNoBareExports) and conflicts with master in internal/lightui/calculator.go. It was not used.

Built on factory/FT66-global-appmenu, after a merge of origin/master (f9cecde):
- 687c052 fix(appmenu): trust signals by sender, read submenus when they open
- 7a695a9 fix(bar): bind the application menu and draw it as the shell's menus

Against the review of 087cd11:
- [P1] internal/ui/appmenu.go:61: the bar has the app_menu binding and its fields; the shell compiles.
- [P1] internal/ui/appmenu.go:199: the first level is bar buttons with one shared handler, and a section opens as a uikit menu, as the tray's; no callback is made per item.
- [P2] internal/appmenu/registrar.go:123: NameOwnerChanged counts only from the bus, with its path and argument types checked.
- [P2] internal/appmenu/registrar.go:150: WindowRegistered carries window, service and path; the introspection XML too.
- [P2] internal/appmenu/registrar.go:197: a process with several registered windows keeps its menu, the latest registered.
- [P2] internal/appmenu/registrar.go:211: a section is read when it opens, with AboutToShow for it and for each empty submenu inside it.
- [P2] internal/ui/appmenu.go:37: the menu is synchronised with every compositor snapshot.
- [P2] internal/ui/appmenu.go:124: LayoutUpdated and ItemsPropertiesUpdated from the connection that registered a menu reload the bar when they touch the first level.

Also: the registrar's exports go through internal/dbusexport, which master's TestNoBareExports now requires.

Decisions:
- Distinct menus registered by one process cannot be matched to a window, as the compositor does not give the registered window ID; the latest registration stands for the process.
- Menu contents are read at each opening rather than tracked; only the first level follows the application's signals.
- The registrar still starts unconditionally, as in the PR. Qt applications hide their in-window menu bar while a registrar is present, so this changes them for everyone; a setting is the owner's call.

Head: 7a695a9a68ec577d8c8cb246743dd59914ab6071, pushed to the mirror and, fast-forward, to the PR's branch iamcheyan/inua feat/appmenu-global-menu.
Tests: make test passes with a throwaway HOME. Each registrar finding has a regression test in internal/appmenu/registrar_test.go. PR CI on the new head: checks and term-engine pass; macos fails in internal/term/host with the same tests as on master (f9cecde).
Installs: none.
Not checked: the UI was not looked at, and nothing ran against a live compositor or a real application's menu. By hand in a nested session: focus a Qt application, see its menu titles in the bar, open a section, activate an item, switch to another application and see the bar follow.
The verdict above is for 087cd11; this head has no review yet. Stays in review/ for the next review run.
