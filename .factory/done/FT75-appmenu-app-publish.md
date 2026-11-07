---
source: pr #73
branch: factory/FT75-appmenu-app-publish
round: 3
---
# feat(appmenu): inua applications publish their menus for the bar

## factory · 2026-11-07 09:51
Imported https://github.com/lyricat/inua/pull/73 directly into review at the owner's request.

Original PR description:

## Problem

The bar's application menus (FT66) read only the Canonical AppMenu Registrar, so they appear for applications that register a window there. inua's own applications publish no dbusmenu, so they never show a menu in the bar.

## Change

- `internal/dbusmenu`: publish a menu tree on the session bus under `io.github.lyricat.Inua.Menu.p<PID>.w<N>`, with the standard `com.canonical.dbusmenu` interface.
- `internal/appui`: `MenuBar` builds the menu's `GAction`s and `GMenu`, keeps a local menu bar out of the window, and publishes the same tree.
- Files and the thirteen light apps (Clock, Calendar, Calculator, Color Picker, GPG, Music, Pass, Stocks, System Monitor, Tailscale, Video, Weather, Workshop) declare their menus; the bar finds one by the name the focused process published, after the registrar finds none.
- A window closes its published menu with itself.

## Known limit

One menu per process: with several windows of the same application (Files can have many), more than one name belongs to the process and the bar shows none rather than the wrong window's commands.

## Verification

- `go build ./...`
- `go test ./internal/dbusmenu ./internal/appmenu`
- `make i18n`


## factory · 2026-11-07 09:51
Status: review (pending; no review performed by this move).
Source branch: iamcheyan/inua:feat/appmenu-app-publish
head: 3550be6b4fc09cdaa2dec8e58788a22bba3a6a2d
The factory branch preserves the exact PR head. Verification above is reported by the PR author, not rerun during this status change.


## factory · review · 2026-11-07 10:05
head: 5b2048dcaf45a473d5bb099d42130eeedf81414d
verdict: fix
fix:
- [P1] internal/dbusmenu/dbusmenu.go:70: Direct Conn.Export calls at lines 70, 73, 222 and 223 fail mandatory TestNoBareExports. Use internal/dbusexport for export and unexport. Example: make test on this head fails the AST rule; the same rule passes all 936 Go files on base e97ebcc.
notes:
- [P2] internal/appui/menu.go:93: Per-action callback registration exhausts purego callback slots. Reuse a long-lived dispatcher and remove closed-window action mappings. Example: a probe using appendAction and releasing each menu/action group panics on invocation 2001; Files adds 25 callbacks per window/file chooser.
- [P2] internal/dbusmenu/dbusmenu.go:114: GetGroupProperties returns aav instead of protocol a(ia{sv}); return a slice of structs containing int32 ID and properties. Example: decoding the emitted rows into the standard property-row type fails dbus.Store.
- [P2] internal/lightui/tailscale.go:225: This device selects nonexistent page this-device; use device, as named in the Blueprint and generated layout. Example: selecting the action from Devices does not switch to the device page.
- [P2] internal/lightui/calculator.go:219: Copy result uses the active input rather than the result. Copy the result explicitly. Example: input 1+2 with preview 3 copies 1+2 (or the selected input substring).
- [P2] internal/filesui/menubar.go:89: Menu literals are not marked for translation extraction. Mark declarations with i18n.M and regenerate catalogs. Example: Keyboard Shortcuts and Find a stock remain English under Chinese locale and are absent after catalog extraction.

Merged origin/master to resolve catalog conflicts, preserving both sides, and pushed merge 5b2048d before review. Reviewed all 28 changed files against origin/master with Codex; no review source edits or commits. The coder could not locate its review-code skill; concrete-example review standards were provided in its task.
Validation: make generate and make i18n passed during merge preparation. Full make test ran in an isolated throwaway HOME/runtime: 18 Python tests and go vet passed; Go race tests failed only TestNoBareExports. Callback, D-Bus signature and translation probes reproduced the findings. Catalog regeneration matched committed output; diff whitespace and Go formatting checks passed.
Installs: none. UI was not visually checked or exercised on the live desktop; UI action findings were checked against source and layouts. Item remains in review/ for the owner to decide; add a note and move to new/ for fixes.

## lyric · 2026-11-07 10:11
Move ft75 back to new

## factory · plan · 2026-11-07 10:37
Fix round 2.
Behaviour: publish menu and properties with per-connection serialized exports; close removes both through the same helper before releasing the name and closing the connection. Preserve empty-menu publication, replacement and error propagation.
Scope: the mandatory P1 export fix and lifecycle regression coverage; optional P2 notes remain outside this round.
Decisions: the owner's return-to-new note adds no scope; follow the latest mandatory fix list, preserving the existing feature and its multi-window limit.
Checking: private-bus lifecycle tests, TestNoBareExports, targeted race tests, full make test in a throwaway HOME/runtime, and make i18n before the Go commit.

## factory · 2026-11-07 10:37
Built fix round 2 with Claude Code; pushed ef73d16e on factory/FT75-appmenu-app-publish. Replaced all four bare Conn.Export calls with dbusexport.Export. Added TestPublishAgainAndClose for replacement, empty publication and name release on close.
Validation: targeted race tests passed; full make test passed in the coder run and independently in manager verification with isolated HOME/runtime (Python session tests, go vet, Go race tests). make i18n left catalogs unchanged; no Blueprint changes, so make generate was not needed. Commit and protected-path checks passed.
Installs: none. No recovery was required. UI was not visually checked or exercised on the live desktop.
Remaining P2 notes: callback-slot exhaustion, GetGroupProperties wire signature, Tailscale page target, Calculator Copy result, and translation extraction. These remain for review/owner consideration; this round fixes only the mandatory P1.
Moved new/ → review/ for another review; not merged into master.


## factory · review · 2026-11-07 10:57
head: ef73d16e49f9cba31cfefe15c774ebc0ea7dcc71
verdict: fix
fix:
- [P1] internal/ui/appmenu.go:75: Native discovery is not accepted by appMenuCurrent, which still checks only ForPID; use consistent native fallback in discovery and validity checks. Example: a private-bus native publisher is discovered and its File menu read, but appMenuCurrent returns false, so opening and activation return early. The feature's native menus cannot be used.
notes:
- [P2] internal/appui/menu.go:93: New per-action callback registrations exhaust permanent purego slots; reuse a dispatcher and remove closed-window mappings. Example: calling appendAction and releasing menus/actions still panics on invocation 2001; Files adds 25 per window or chooser.
- [P2] internal/dbusmenu/dbusmenu.go:116: GetGroupProperties returns aav instead of a(ia{sv}); return structured ID/property rows. Example: private-bus GetGroupProperties([1], []) fails decoding into standard rows.
- [P2] internal/lightui/tailscale.go:225: This device targets this-device while the layout names it device; use device. Example: activating it from Devices does not switch to the device page.
- [P2] internal/lightui/calculator.go:219: Copy result copies the active input; explicitly copy the result. Example: active input 1+2 with preview 3 copies 1+2 or its selected substring.
- [P2] internal/filesui/menubar.go:89: Mark menu declaration labels with i18n.M and regenerate catalogs. Example: Keyboard Shortcuts and Find a stock are absent from POT and remain English in Chinese locale.

Codex reviewed all 28 changed files in origin/master...HEAD. The previous bare-export P1 is fixed; five P2 notes remain independently verified. No merge conflict with origin/master. Review source files were unchanged and no code commits made.
Validation: make test passed in an isolated throwaway HOME/runtime after correcting temporary-path, device and AppArmor isolation configuration: 18 Python tests, go vet, and race tests across 113 Go packages passed. Export audit and publication lifecycle tests passed. Private-bus and callback probes confirmed the findings; diff whitespace and Go formatting checks passed.
Installs: none. UI was not visually checked or exercised on the live desktop. The coder could not locate its review-code skill; concrete-example review standards were supplied in its task. D-Bus boundaries were inspected with no additional confirmed security finding. Source PR #73 has separate upstream changes, which were not incorporated or reviewed here.
Item stays in review/ for the owner to decide; add a note and move to new/ for fixes. No recovery was needed.

## Lyric · 2026-11-07 11:44
Move ft75 back to new

## factory · plan · 2026-11-07 12:04
Fix round 3.
Behaviour: discovery and action validity resolve the focused PID consistently, registrar first and native publisher second. Reject absent, ambiguous or stale menus after focus/registration changes; preserve no-bus and empty cases.
Scope: latest mandatory P1 native-menu validity fix and focused private-bus regressions. Optional P2 notes and separate source-PR updates remain excluded.
Decisions: share the resolver; ambiguous registered windows suppress native fallback rather than risk the wrong commands. Preserve conservative native multi-window handling.
Checking: native lookup/read/activation and validity, focus loss, registrar priority/ambiguity, multiple publishers, release/replacement and no-bus tests; full make test in isolated HOME/runtime; make i18n before the Go commit.

## factory · 2026-11-07 12:04
Built fix round 3 with Claude Code; pushed 986428f0 on factory/FT75-appmenu-app-publish. Added Registrar.Focused and Current, used consistently by bar discovery and action validity; added private-bus regression coverage.
Validation: targeted race tests passed. Full make test passed both in the coder run and independent manager verification using throwaway HOME/runtime (Python session tests, go vet, Go race tests). make i18n left catalogs unchanged; no Blueprint changes, so make generate was not needed. Commit history, protected paths and clean worktree checked.
Installs: none. No recovery was required. UI was not visually checked or exercised in a nested/live session. Native validity now performs the same synchronous D-Bus lookup as discovery, so a slow bus can delay an action; this was not latency-tested.
The five existing optional P2 notes remain outside this round. No upstream PR commits incorporated; no merge into master.
Moved new/ → review/ for another review.


## factory · review · 2026-11-07 12:22
head: 986428f0bae51f347e8df40fbc8cf7377e7cf7aa
verdict: pass
notes:
- [P2] internal/appui/menu.go:93: Per-action permanent callbacks exhaust purego slots; reuse a dispatcher and clean up closed-window mappings. Example: releasing each menu/action group still panics at invocation 2001; Files adds 25 per window or chooser.
- [P2] internal/dbusmenu/dbusmenu.go:116: GetGroupProperties emits aav rather than a(ia{sv}); return structured ID/property rows. Example: private-bus GetGroupProperties([1], []) fails standard property-row decoding.
- [P2] internal/lightui/tailscale.go:225: This device targets nonexistent this-device; use device. Example: activation from Devices cannot select the device page defined in both layouts.
- [P2] internal/lightui/calculator.go:219: Copy result copies the focused input; copy the result explicitly. Example: focused input 1+2 with preview 3 copies 1+2 or selected input text.
- [P2] internal/filesui/menubar.go:89: Menu declarations lack translation extraction markers; use i18n.M and update catalogs. Example: Keyboard Shortcuts and Find a stock remain absent after extraction and fall back to English in Chinese locale.

Codex reviewed all 30 changed files in origin/master...HEAD against the full request and plans. Both historical P1 findings are fixed; no P0/P1 found. No conflict with origin/master, no source edits or commits, and no upstream PR changes incorporated. The coder could not locate its review-code skill; concrete-example standards were provided in its task.
Validation: make test passed with isolated throwaway HOME/runtime (18 Python tests, go vet, 113 Go packages with race tests). Targeted lifecycle, signal-security and export tests passed. Temporary-copy make i18n matched committed catalogs. Manager verified unchanged head and clean worktree.
Installs: none. No recovery was required. D-Bus process identity, signal sender and action forwarding boundaries were checked with no additional confirmed security findings. Slow-bus latency and full third-party client compatibility were not tested. UI was not visually checked or exercised on the desktop; page and copy findings were verified against source/layouts.
Moved review/ → ready/ under the factory no-P0/P1 threshold. Five P2 notes remain for owner consideration; not merged into master.

## factory · 2026-11-07 16:03
Shipped at the owner's request after verifying the latest reviewed head. Merged into master at df00305b; release v0.1.27 points to 8db50f34. Linked PR #73: closed; later upstream PR changes excluded.
Validation: make generate and make i18n passed with clean generated output; full make test passed first run with throwaway HOME/runtime and uncached Go tests (18 Python tests, go vet, 113 race-tested Go packages). FT76 catalog conflicts resolved preserving both sides. Existing P2/P3 notes remain unchanged. No installs. UI was not visually checked or exercised on the desktop.
Moved ready/ → done/.
