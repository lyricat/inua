# niri and Wayfire: the vertical-slice experiments

This is the third step of #58 (issue #61). Two disposable programs in
[experiments/compositor](../experiments/compositor/README.md) talk to niri
and to Wayfire directly and do the slice the issue asks for: list windows and
workspaces, watch changes, focus a window and switch workspace, and see what
a disconnect does. This document is how they were run, what each step was
expected to do and what it did, the comparison with Hyprland, the
constraints that fell out for the contracts (#62), and a go/no-go per target.

**What was run.** Everything below was observed on 2026-11-05 on
**niri 26.04 (8ed0da4)** and **Wayfire 0.11.0 (wlroots 0.20.2)**, from Arch
Linux's packages, in a throwaway container with no display: Wayfire on
wlroots' headless backend, niri nested in a second headless Wayfire (niri
has no headless backend of its own). The probes first, then the two
backends built on them (`inua compositor dump|watch`), then `inua-shell`
itself with keys and clicks from a virtual keyboard and pointer, and for
Wayfire real key events from its `stipc` test plugin. Hyprland 0.57.1 was
run the same way (nested, on the host's render node, since it has no
software path) for the regression check of #63. What a headless nested
session cannot show is at the end.

The prototypes are evidence, not compositor support; the support is the
backends in `internal/niri` and `internal/wayfire`.

## Running the experiments

Inside a session of the compositor, from `experiments/compositor`:

```sh
niri --version && go run ./niri 2>&1 | tee niri.log
wayfire --version && go run ./wayfire 2>&1 | tee wayfire.log   # needs the ipc and ipc-rules plugins
```

Each run prints every request, reply and event with a timestamp.
`-request JSON` (niri) and `-call METHOD -data JSON` (Wayfire) send one
request; `-reconnect` keeps the event connection across a compositor
restart. To run headless as here:

```sh
# Wayfire
WLR_BACKENDS=headless WLR_RENDERER=pixman WLR_LIBINPUT_NO_DEVICES=1 wayfire -c wayfire.ini
# niri, as a client of that Wayfire, software GL
WAYLAND_DISPLAY=wayland-1 LIBGL_ALWAYS_SOFTWARE=1 niri -c config.kdl
```

with `plugins = ipc ipc-rules vswitch wsets command wm-actions` in the
Wayfire config. Sway cannot be the headless parent in an unprivileged
container (its binary carries a capability), and Hyprland needs a GPU
render node.

## niri 26.04

| # | Step | Expected from the docs | Observed | Status |
| --- | --- | --- | --- | --- |
| n1 | `Version`, `FocusedOutput`, `Outputs`, `Workspaces`, `Windows`, `FocusedWindow` | one line each, `{"Ok":{"<Name>":…}}` | as documented. Nested, the output is `winit` with transform `Flipped180`. `tile_pos_in_workspace_view` is **null for every tiled window**: only `pos_in_scrolling_layout` (column, row) and the sizes are given | confirmed, one difference |
| n2 | event stream start | the full state first | `{"Ok":"Handled"}`, then `WorkspacesChanged`, `WindowsChanged`, then `KeyboardLayoutsChanged`, `OverviewOpenedOrClosed`, `ConfigLoaded`, `CastsChanged` | confirmed |
| n3 | open a window | `WindowOpenedOrChanged`, then `WindowFocusChanged` | `WorkspaceActiveWindowChanged`, then `WindowOpenedOrChanged` with `is_focused: true`. **No `WindowFocusChanged`**: the focus is carried by the window object | differs |
| n4 | change a title | `WindowOpenedOrChanged` | as expected | confirmed |
| n5 | `FocusWindow` of a window on another workspace | focus, and the workspace shown | `WorkspaceActivated{id, focused:true}`, then `WindowFocusChanged{id}` | confirmed |
| n6 | `FocusWorkspace{Index}` | `WorkspaceActivated`, focus to its window | as expected | confirmed |
| n7 | `MoveWindowToWorkspace{focus:false}` | the window moves, focus stays | `WorkspacesChanged` (a new empty workspace appears at the end), `WindowOpenedOrChanged` with the new `workspace_id` and `is_focused:false`, `WindowLayoutsChanged`; no focus or activation event | confirmed |
| n8 | errors | `Err` on the same socket | `ReturnError` → `{"Err":"example compositor error"}`; an unknown request → `{"Err":"error parsing request"}`; **an action on a window id that does not exist → `{"Ok":"Handled"}`**, silently | differs |
| n9 | close a window | `WindowClosed`, then focus | `WorkspaceActiveWindowChanged`, `WindowFocusChanged`, **then** `WindowClosed`; `WindowFocusTimestampChanged` about 0.75 s later | differs in order |
| n10 | restart niri | a new socket path | the path carries niri's pid (`niri.wayland-2.360.sock` → `…894.sock`): a client must be started again in the new session, which a shell spawned by niri is | confirmed |
| n11 | output off | workspaces move | not run: a nested niri has one output | untested |
| n12 | layout positions | positions in the workspace view | none for tiled windows (n1) | differs |

Also observed: niri gives an on-demand layer surface the keyboard only on
a click, not when it appears; a virtual keyboard's own keymap is honoured
(text typed into a terminal arrived intact); the top-left hot corner opens
niri's own overview, which is where inua's bar has its workspace marks.

Not in niri, recorded as unavailable: a window image stream, run-time
bindings, a headless output, raw key observation, window geometry for
tiled windows.

## Wayfire 0.11

| # | Step | Expected from the docs | Observed | Status |
| --- | --- | --- | --- | --- |
| w1 | `list-methods`, `wayfire/configuration`, `list-outputs`, `list-wsets`, `list-views`, focused view and output | objects | **the three list methods answer with a JSON array**, the others with an object carrying `"result":"ok"`. A view's `geometry` is relative to the cell its output shows: a view on the next cell to the right has `x` one output width larger (292 → 1572 on a 1280 wide output). The shell's own layer surfaces are listed as views with `role: "desktop-environment"` | confirmed, with the array |
| w2 | `events/watch` with no list | one reply, then events | `{"result":"ok"}`, then events; no initial state | confirmed |
| w3 | open a window | `view-mapped`, then `view-focused` | `view-set-output`, `view-title-changed` (title `nil` first), `view-app-id-changed`, `view-mapped`, `view-focused`, `view-geometry-changed` | confirmed |
| w4 | change a title | `view-title-changed` | as expected | confirmed |
| w5 | `focus-view` of a view on another cell | unknown | Wayfire **switches the cell itself**: the views' geometries shift, `view-focused`, `wset-workspace-changed` | resolved |
| w6 | `vswitch/set-workspace` | `wset-workspace-changed` | as expected; with no window on the cell, `view-focused` carries no view | confirmed |
| w7 | `vswitch/send-view` | unknown whether focus follows | `view-geometry-changed`, `view-workspace-changed{from,to}`; **the focus stays** | resolved |
| w8 | errors | an object with `error`, connection kept | `{"error":"No such method found!","method":…}` and `{"error":"Error during execution of the handler …: No view with id=…"}`; the connection is kept | confirmed |
| w9 | close a window | `view-unmapped`, then focus | as expected | confirmed |
| w10 | restart Wayfire | the same socket path | `wayfire-wayland-1-.socket` again; the client reconnects once it is back | confirmed |
| w11 | unload `vswitch` at run time (`wayfire/set-config-options`) | its methods go | `vswitch/set-workspace` → "No such method found!"; loading the plugin again brings it back | confirmed |
| w12 | `wayfire/create-headless-output`, then destroy | `output-added`, `output-removed` | the output appears (`HEADLESS-2`, a workspace set of its own) with **`output-layout-changed`**, not `output-added`; destroying it sends `output-removed` and `output-layout-changed` | differs |

Also observed: `focus-view` leaves the pointer where it is; an on-demand
layer surface gets the keyboard when it appears; the `command` plugin's
bindings from the sample config (`binding_x` and `command_x` pairs) run
`inua call …` for real key events.

Not in Wayfire, recorded as unavailable: a window image stream, a
workspace move between outputs (cells belong to their set), the standard
workspace protocol.

## The same slice on the three compositors

| Aspect | Hyprland | niri | Wayfire |
| --- | --- | --- | --- |
| Window identity | hex address | `u64` id | integer id |
| Workspace identity | positive integer, chosen by the client on create | `u64` id chosen by niri; `idx` positional | a grid cell `(set, x, y)`; never created or destroyed |
| Workspace ↔ output | one output per workspace; movable | one output per workspace; movable | a set per output; cells stay with it |
| Initial state | four queries after the event socket is up | the event stream's first events | lists after the watch is up |
| Events | names with text; a refresh re-reads | typed, carrying objects | typed, carrying objects |
| Focus a window elsewhere | switches workspace | switches workspace | switches cell |
| Move without following | Lua `follow=false`, classic `movetoworkspacesilent` | `focus:false` | `vswitch/send-view` |
| New workspace | the client picks a free integer | the trailing empty workspace | any empty cell |
| Window geometry | global, every window | floating windows only | every window, relative to the shown cell |
| Pointer on focus | warps unless told not to | does not unless configured | does not |
| On-demand layer surface | focused when it appears | focused on a click | focused when it appears |
| An unknown window id | an error | accepted silently | an error |
| Restart | same socket path | a new path | same socket path |

## Constraints for the contracts

What these observations put on the shared types:

1. **Window and workspace ids are opaque strings** chosen by the backend.
2. **A workspace's position is the backend's**; the shell sorts by a
   backend-given order key.
3. **"New workspace" is a backend function** (the lowest free integer, the
   trailing empty workspace, an empty cell).
4. **A move without following focus is optional.** All three have it.
5. **The shell sees snapshots, not events**: niri's focus rides on window
   objects, Wayfire's cell on geometry, and neither can be expressed as
   Hyprland's event names.
6. **Capabilities are probed**: Wayfire's by `list-methods`, again when
   asked to refresh, since a plugin can be unloaded while running.
7. **Geometry is optional per window**, not per compositor: niri has it
   for floating windows only.
8. **Errors are values**, and a backend cannot rely on one: niri accepts
   an action on a window that is gone.
9. **Whether an on-demand layer surface is focused as it appears is a
   capability** (`CapOnDemandFocus`): a surface that must take keys at
   once asks for the keyboard exclusively where it is not.

## Go/no-go

- **niri: go.** The event stream, the move without focus and the restart
  behaviour are as needed; the backend is in and the shell runs on it. Its
  limits are the optional capabilities: no previews, no geometry for tiled
  windows (the overview's cards show titles and icons), no shortcut or
  display pages, no virtual screen.
- **Wayfire: go.** Focusing switches the cell, a sent view does not take
  the focus, a missing plugin is an error on a kept connection. The
  backend is in and the shell runs on it, with the grid as nine workspaces
  per output.
- **Baseline revision**: none. The grid as a list of cells and an overview
  without previews both work in the shell as built; whether they are good
  enough to call supported is the owner's call.

## What the shell did on each (2026-11-05)

`inua-shell` from this branch, with its layouts read from the repository:

| Check | Hyprland 0.57.1 | niri 26.04 | Wayfire 0.11 |
| --- | --- | --- | --- |
| Starts, bar with workspaces and the focused title, wallpaper, dock with the running apps | yes | yes | yes (nine marks) |
| Launcher's window list, Enter switches to the previous window | yes | yes | yes |
| Overview opens with a card per workspace in use and a New card | yes, with live previews | yes, titles and icons | yes, titles and icons |
| Overview: arrow and Enter go to another workspace | yes | yes (after the keyboard fix below) | yes |
| Overview: New goes to a new workspace | yes (workspace 3 made) | yes (the trailing workspace) | yes (the first empty cell) |
| Dock click focuses the app's window on its workspace, pointer left alone | yes | yes | yes |
| Lock and unlock (`inua call lock now`, `unlock`) | yes | yes | yes |
| Settings window; Displays and Shortcuts say what is not available | all pages work | the line shows | the line shows |
| The sample config starts the shell and its shortcuts run | not run (the nested Hyprland used its generated config) | yes (`niri validate`; started by `spawn-at-startup`; a bind's command opens the launcher) | yes (started by `autostart`; Super+Space and Super+Tab by real key events) |

Found and fixed on the way: the overview could not be driven by keys on
niri (the on-demand keyboard mode, and GTK focusing the search entry by
itself, which the shell took for a search); the overview kept retrying
Hyprland's preview protocol on other compositors; the shell wrote
`hypr/inua.conf` while on niri; niri's hot corner collided with the bar.
The same key steps on master's build of the shell on Hyprland did not
move the selection at all, so the keyboard fix helps there too.

## Not checked

- Anything that needs a real screen, GPU output or input device: frame
  timing, blur, fractional scale, a second monitor on niri, hot-plugging,
  the lid.
- The overview's drag with a pointer (the move it sends was checked through
  the IPC), the control center, notifications, the tray, the clipboard
  panel's paste, dictation, screenshots, screen sharing, Mobile Link, on
  niri and Wayfire.
- A login through a display manager with `inua-session niri` or
  `inua-session wayfire`; the script's checks are covered by its tests and
  the sample configs were started by hand.
- Wayfire 0.9 or 0.10 (Debian's and Ubuntu's): only 0.11 was run.
- Sway and KWin: not run at all.
