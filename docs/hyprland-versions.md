# Hyprland 0.53 and 0.55

inua runs on both. Hyprland 0.55 replaced its config language and its
dispatcher syntax with Lua, and inua uses that where it is there; on 0.53
it uses the classic forms. This page lists what differs for the user and in
the code.

## How inua tells them apart

When the shell connects to Hyprland it sends `eval _G.inuaOverviewProbe=true`
(`probeLua` in `internal/ui/overview_keys.go`). 0.55 answers `ok`; 0.53
answers "unknown request". The result is `shellUI.hyprLua`, and the shell
logs it:

```
hyprland: runs Lua: true (the overview is on with it)
```

The command line does the same with `hyprconf.RunsLua` when it needs to
know (`inua config apply`, `inua config hyprland`). Nothing reads
`hyprctl version`: what matters is whether Lua answers, not the number.

## What the user sees

| | Hyprland 0.53 | Hyprland 0.55 and later |
| --- | --- | --- |
| Workspace overview | Off. Super+Tab opens the launcher's window list; tapping Super does nothing; the bar's workspace click does not open it | On: tap Super, or Super+Tab / Super+Shift+Tab, release Super to commit |
| Super+1 … Super+0 | Whatever `hyprland.conf` binds (usually workspaces 1–10) | The first to tenth workspace **on the focused monitor**, in the overview's order; the number right after the last opens a new workspace there, higher ones do nothing |
| Hyprland config | `hyprland.conf` (hyprlang) | `hyprland.lua`, or `hyprland.conf`, which still works but is deprecated |
| Settings inua writes (keyboard, touchpad, shortcuts) | `~/.config/hypr/inua.conf`, loaded with `source = ./inua.conf` | `inua.lua` next to `hyprland.lua`, loaded with `pcall(require, "inua")`; `inua.conf` if the config is still `hyprland.conf` |
| Starting the shell | `exec-once = inua-shell` | `hl.on("hyprland.start", function() hl.exec_cmd("inua-shell") end)` |
| Everything else | Same | Same |

Which file inua writes: `inua.lua` when Hyprland runs Lua and
`hyprland.lua` exists, or when there is no `hyprland.conf` (so `inua init`
from a TTY, with nothing running, still picks the right one); otherwise
`inua.conf`. A `hyprland.lua` lying around on 0.53 is ignored.

## In the code

Each place that talks to Hyprland picks one form; none tries Lua first and
falls back, so 0.53 never sees a request it rejects.

| What | 0.53 | 0.55 | Where |
| --- | --- | --- | --- |
| Dispatchers | `dispatch focuswindow address:0x…`, `dispatch workspace 3` | `dispatch hl.dsp.focus({ window = "address:0x…" })`, `hl.dsp.focus({ workspace = 3 })` | `dispatchEither` in `internal/ui/shell.go`; `hypr.Client.LuaDispatch` |
| Focusing from the dock without moving the pointer (`focusQuietly`) | One `[[BATCH]]`: `keyword cursor:no_warps 1`, the dispatch, `keyword cursor:no_warps 0` | One `eval` script that sets `cursor.no_warps`, focuses, and restores it | `focusQuietly` in `internal/ui/dock.go` |
| Pasting from the clipboard history | The quiet focus above, then `dispatch sendshortcut CTRL, V, address:0x…` | The quiet focus above, then `hl.dsp.send_shortcut({ mods = "CTRL", key = "V", window = "address:0x…" })` | `pasteClipboard` in `internal/ui/clipboard.go` |
| Moving the pointer back after a surface opens | `dispatch movecursor x y` | `hl.dsp.cursor.move({ x = …, y = … })` | `refocusPointer` in `internal/ui/shell.go` |
| Recording a shortcut (the empty submap) | `dispatch submap inua-record` | `hl.dsp.submap("inua-record")` | `dispatchSubmap` in `internal/ui/shell.go` |
| The overview's keys | Not installed | `eval` scripts: a raw keyboard listener for the Super tap, `hl.bind` for Super+Tab and Super+1…0, `hl.dsp.event("inua-overview,…")` back to the shell | `internal/ui/overview_keys.go` |
| Moving windows and workspaces in the overview | (no overview) | `hl.dsp.window.move`, `hl.dsp.workspace.move` | `internal/ui/overview_manage.go` |
| Settings file | `Render`: `input { kb_options = … }`, `device { … }`, `monitor = …` (`, disable`, `, mirror, …`), `bindl = , switch:…`, `unbind =` / `bind =`, `submap =` | `RenderLua`: `hl.config({ input = … })`, `hl.device({ … })`, `hl.monitor({ … })` (`disabled = true`, `mirror = …`), `hl.bind("switch:…", …, { locked = true })`, `hl.unbind` / `hl.bind(…, { description = … })`, `hl.define_submap` | `internal/hyprconf` |
| Reading the user's config | `source =` lines, `#` comments | `require` (relative to `hyprland.lua`, `.` or `/` between folders), `--` comments outside strings | `internal/bootstrap/hyprland.go` |
| Applying settings | Write the file; Hyprland reloads by itself, or `reload` if `misc:disable_autoreload` is set | Write `inua.lua` and always `reload`: Hyprland only watches a required file once it has loaded it | `hyprconf.Apply` |

A new feature that dispatches should go through `dispatchEither` (or check
`s.hyprLua` and capture it before starting a goroutine) with both forms.

## Things to know on 0.55

- `hl.unbind` matches the key's case exactly ("SUPER + TAB" does not
  unbind "SUPER + Tab"). `inua.lua` unbinds each replaced key as written,
  in capitals and in lower case.
- A `require` of a module that does not exist stops the rest of
  `hyprland.lua`; that is why the line is `pcall(require, "inua")`.
- Binds made in Lua with a function show in `hyprctl binds` with the
  dispatcher `__lua` and no command. The Shortcuts page matches inua's own
  binds by their description, and lists a function bind as "Lua function".
- `inua-session` (the login session) checks only `hyprland.conf` before it
  starts Hyprland. With only a `hyprland.lua` it skips the check; with both
  files it checks `hyprland.conf`, which may not be the one Hyprland reads.

## Testing

The machine inua is developed on runs 0.53, so the 0.55 paths are covered
by unit tests of the generated text (`RenderLua`, the Lua config reader)
and by trying them on a 0.55 machine. A nested session (`make nested`) runs
whatever Hyprland is installed. Changes to version-dependent code should
say which version they were run on.
