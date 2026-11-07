---
source: pr #73
branch: factory/FT75-appmenu-app-publish
round: 1
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
