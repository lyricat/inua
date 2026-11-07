---
source: pr #73
branch: factory/FT75-appmenu-app-publish
round: 2
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
