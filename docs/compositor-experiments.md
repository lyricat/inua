# niri and Wayfire: the vertical-slice experiments

This is the third step of #58 (issue #61). Two disposable programs in
[experiments/compositor](../experiments/compositor/README.md) talk to niri
and to Wayfire directly and do the slice the issue asks for: list windows and
workspaces, watch changes, focus a window and switch workspace, and see what
a disconnect does. This document is the instructions, the expected and the
observed behaviour per step, the comparison with Hyprland, the constraints
that fall out for the contracts (#62), and a go/no-go per target.

**What was and was not run.** The programs were written against the
documentation and sources named in
[compositor-capabilities.md](compositor-capabilities.md), built and vetted
with Go 1.26, and **not run against either compositor**: neither niri nor
Wayfire is installed on the machine this was written on, and the factory's
rules do not allow system packages or downloaded binaries. Every row of the
observation tables below is therefore **untested** until the owner, or a
build on a machine with the compositor, runs the steps and fills the
"observed" column. Nothing here claims that either prototype is compositor
support.

Versions targeted: niri 26.04 (the `niri-ipc` crate on `main` at the time of
writing; the IPC is versioned and `Version` is the first request the program
sends) and Wayfire 0.9.0 (`plugins/ipc`, `plugins/ipc-rules` on `master`).

## Running the experiments

Both compositors run nested under Hyprland: with `WAYLAND_DISPLAY` set they
pick the Wayland backend and open as a window. Use a throwaway `HOME` as the
repository's rules ask, and run the program from a terminal *inside* the
nested compositor so it sees that compositor's socket.

```sh
# niri
mkdir -p /tmp/niri-home && HOME=/tmp/niri-home niri   # opens a nested window
# inside it: a terminal, then
cd experiments/compositor && niri --version && go run ./niri 2>&1 | tee ../../niri.log

# Wayfire: enable the plugins first
mkdir -p /tmp/wf-home/.config && cat > /tmp/wf-home/.config/wayfire.ini <<'INI'
[core]
plugins = ipc ipc-rules vswitch wsets command wm-actions
INI
HOME=/tmp/wf-home wayfire -c /tmp/wf-home/.config/wayfire.ini
# inside it: a terminal, then
cd experiments/compositor && wayfire --version && go run ./wayfire 2>&1 | tee ../../wayfire.log
```

Each step below is one run of the program with the flag shown, with two or
three windows open (a terminal and any two apps) on two workspaces. The log
has a timestamp per line, every request, every reply and every event, so the
order of an action's reply against the events it causes is in the file.

## niri

Slice steps, with what the documentation says should happen.

| # | Step (flag) | Expected from the docs | Observed | Status |
| --- | --- | --- | --- | --- |
| n1 | start-up: `Version`, `FocusedOutput`, `Outputs`, `Workspaces`, `Windows`, `FocusedWindow` | each a one-line `{"Ok": ...}`; `Workspaces` has one entry per workspace per output with `id`, `idx`, `output`, `is_active`, `is_focused`, `active_window_id`; the last one on each output is empty; `Windows` has `id`, `title`, `app_id`, `pid`, `workspace_id`, `is_focused`, `layout` | | untested |
| n2 | event stream start | before any change, `WorkspacesChanged{workspaces}` and `WindowsChanged{windows}` carrying the full state, then `WindowFocusChanged`, `KeyboardLayoutsChanged` and the others once | | untested |
| n3 | open a window by hand | `WindowOpenedOrChanged{window}` then `WindowFocusChanged{id}`; if the window landed on the trailing empty workspace, `WorkspacesChanged` with a new empty one after it. Order between the two window events is what to record | | untested |
| n4 | change a title (a terminal `cd`) | `WindowOpenedOrChanged` with the same `id` and the new `title`; no other event | | untested |
| n5 | `-focus-window ID` | reply `{"Ok":{"Handled":null}}`; then `WindowFocusChanged{id}`; if the window is on another workspace, `WorkspaceActivated{id, focused:true}` first or after (record) | | untested |
| n6 | `-focus-workspace N` (index) | `WorkspaceActivated{id, focused:true}` for the workspace at index N on the focused output; `WindowFocusChanged` to that workspace's active window or `null` | | untested |
| n7 | `-move-window ID -to N` | window's `workspace_id` changes in a `WindowOpenedOrChanged`; focus stays where it was (`focus:false`); no `WorkspaceActivated` | | untested |
| n8 | unknown id (`FocusWindow` with 2^62), unknown request (`ReturnError`) | both come back as `{"Err": "..."}` on the same socket; the socket is not closed by niri | | untested |
| n9 | close a window by hand | `WindowClosed{id}`, then `WindowFocusChanged`; if its workspace became empty and was not the last, `WorkspacesChanged` without it | | untested |
| n10 | `-reconnect`, then close and reopen the nested niri while the program runs | the stream ends with EOF; reconnect fails until the socket is back; the new `NIRI_SOCKET` of a restarted niri is a different path, so the program must be restarted inside the new session (record whether the old path is reused) | | untested |
| n11 | disconnect a monitor (nested: close one of two niri windows is not possible; use `niri msg output NAME off`) | `WorkspacesChanged` with the workspaces moved to the remaining output; their `id`s unchanged, `idx` renumbered | | untested |
| n12 | overview and scrolling | `Window.layout` has the tile position and sizes in the workspace view; windows scrolled out of view keep their layout | | untested |

Operations that niri does not have, recorded as unavailable, not simulated:
a window image stream, run-time bindings, a headless output, raw key
observation, "move workspace to a given output" is `MoveWorkspaceToMonitor`
with a direction or a name (there is a name form), create workspace (the
trailing empty one is the new one).

## Wayfire

| # | Step (flag) | Expected from the docs | Observed | Status |
| --- | --- | --- | --- | --- |
| w1 | start-up: `list-methods`, `wayfire/configuration`, `list-outputs`, `list-wsets`, `list-views`, `get-focused-view`, `get-focused-output` | `list-methods` names every method of the enabled plugins; each output has `wset-index` and `workspace{x,y,grid_width,grid_height}`; each view has `id`, `pid`, `title`, `app-id`, `geometry`, `bbox`, `output-id`, `wset-index`, `mapped`, `activated`, `focusable`, `role`, `last-focus-timestamp`. Views on other grid cells have a `geometry` outside the output (record the coordinates) | | untested |
| w2 | `window-rules/events/watch` with no list | one reply; then events as they happen; no initial state (the program's own `list-*` calls are the initial state) | | untested |
| w3 | open a window by hand | `view-mapped{view}` then `view-focused{view}`; order to record. With `view-pre-map` not subscribed, mapping is not held | | untested |
| w4 | change a title | `view-title-changed{view}` | | untested |
| w5 | `-focus-view ID` | reply with `result: ok` (or an object without `error`); then `view-focused`; if the view is on another cell, whether Wayfire also switches the workspace (`wset-workspace-changed`) or focuses an off-screen view is the question | | untested |
| w6 | `-set-workspace X,Y` | `wset-workspace-changed{previous-workspace, new-workspace, output, wset}`; `view-focused` to the view on that cell | | untested |
| w7 | `-send-view ID -to X,Y` | `view-workspace-changed{from, to, view}` and `view-geometry-changed`; whether focus follows is unknown and is the observation | | untested |
| w8 | unknown method, unknown view id | an object with `error`; same connection kept (the server closes only on unparsable JSON or a missing `method`) | | untested |
| w9 | close a window by hand | `view-unmapped{view}` then `view-focused` for the next | | untested |
| w10 | `-reconnect`, restart the nested Wayfire | EOF; the socket path is `wayfire-<display>-.socket` under the runtime dir, so a restarted Wayfire with the same display name reuses the path and the reconnect succeeds (record) | | untested |
| w11 | a plugin missing (remove `vswitch` from the config, reload) | `list-methods` no longer lists `vswitch/*`; calling one returns an `error`; this is what "temporarily unavailable" looks like on Wayfire | | untested |
| w12 | `wayfire/create-headless-output{width,height}` then destroy | `output-added` with the new output, then `output-removed` | | untested |

Unavailable on Wayfire, recorded: a window image stream (only screencopy of
the visible area), a workspace *list* (the grid is implicit), a workspace
move between outputs (sets swap whole), the standard list and workspace
protocols.

## The same slice on Hyprland, for comparison

What inua relies on today, from the audit, in the same terms:

| Aspect | Hyprland | niri | Wayfire |
| --- | --- | --- | --- |
| Window identity | hex address, valid for the instance | `u64` id, valid for the instance | integer id, valid for the instance |
| Workspace identity | positive integer, chosen by the client on create; names optional | `u64` id chosen by niri; `idx` is positional and shifts; names optional and stable | a grid cell `(wset, x, y)`; sets have an index; cells never created or destroyed |
| Workspace ↔ output | one output per workspace; move by dispatcher | one output per workspace; moves with the output; moves back when it returns | a set per output; cells belong to the set |
| Initial state | query four lists after the event socket is up | the event stream's first events are the state | query lists, then watch; nothing replays |
| Event granularity | names with string payloads; a refresh re-reads everything | typed events with full objects; no re-read needed | typed events with full objects |
| Focus a window | `focuswindow address:` | `FocusWindow{id}` | `focus-view{id}` |
| Switch workspace | `workspace N` | `FocusWorkspace{Id/Index/Name}` | `vswitch/set-workspace{x,y,output-id}` |
| Move a window without following | Lua `window.move(follow=false)` | `MoveWindowToWorkspace{focus:false}` | `vswitch/send-view` (focus behaviour unknown) |
| New workspace | the client picks a free integer | the trailing empty workspace always exists | any empty cell |
| Pointer warp on focus | toggled off around the call | config option, off by default | not warped by `focus-view` (unknown for others) |
| Errors | `ok` or text | `Err(String)` per request | object with `error` |
| Disconnect | re-query on reconnect, same socket path | stream ends; the path changes with the instance | EOF; same path for the same display name |

## Constraints for the contracts

What these two models force on the shared types, whatever the session checks
find:

1. **Window and workspace ids are opaque strings** chosen by the backend.
   Nothing outside a backend parses them, sorts by them or picks a new one.
2. **A workspace's position is the backend's** (Hyprland's integer, niri's
   `idx`, Wayfire's `(x, y)`); the shell sorts by a backend-given order key
   and never assumes contiguity or that an id survives a reorder.
3. **"New workspace" is a backend function**: given a snapshot and an output
   it names the workspace a window would land on if it were moved to a new
   one (Hyprland: the lowest free integer; niri: the trailing empty
   workspace; Wayfire: an empty cell). Overview's reservation logic becomes
   that function's Hyprland implementation.
4. **A move without following focus is optional**, and the Overview's drag
   is shown only when the backend says it has it.
5. **Initial state and events are one stream** to the shell: a backend
   delivers snapshots, not raw events. niri's stream is already that;
   Hyprland's query-after-event and Wayfire's list-then-watch become each
   backend's way to produce one.
6. **Capabilities are probed, not assumed**: Wayfire's `list-methods` and
   niri's `Version` decide at connect time; a backend reports a capability as
   unavailable when the probe fails and supported when it passes.
7. **Geometry is optional** and in output-local logical pixels when present.
8. **Errors are values returned to the caller**, with the backend's message,
   so the UI can show them where the control is; no backend logs and
   swallows.

## Go/no-go

- **niri: go** for the integration round (#64), *provided* the session
  checks n2, n5, n7 and n10 come out as the documentation says. No-go
  triggers: the event stream does not start with the full state (n2); a
  move with `focus:false` still moves focus (n7); the socket path cannot be
  rediscovered after a restart without a new environment (n10, which would
  need the session script to re-export it).
- **Wayfire: go as the second target**, *provided* w5, w7 and w11 are
  understood. No-go triggers: `focus-view` cannot focus an off-screen view
  without the client switching the cell first (then focus needs two calls
  and the contract's focus has to say so); a missing plugin kills the
  connection instead of returning an error (w8/w11); the workspace of a
  view cannot be derived from its geometry reliably (w1).
- **Baseline revision**: none from the documentation; the two grey areas
  are whether "workspace list" can be honest on Wayfire (a grid presented as
  a list of cells in use plus one free cell) and whether niri's lack of any
  window capture makes Overview without previews acceptable to the owner.
  Both are the owner's calls, not findings.
