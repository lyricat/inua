# Compositor capabilities and a minimum support baseline

This is the second step of #58 (issue #60). It takes the feature list from the
[coupling audit](hyprland-coupling.md) (#59) and asks, for Hyprland, Sway,
niri, Wayfire and KWin, what each compositor exposes for it, with the source
and how far that is verified. It then proposes what "supported compositor"
should mean, how a missing capability shows in the UI, and which compositor
to integrate first. It designs no interface and changes no code; the
contracts are [compositor-contracts.md](compositor-contracts.md) (#62).

Written on 2026-11-04. Compositor versions are the ones the sources
describe on that day: Hyprland 0.52.1 (protocol tables; inua itself asks for
0.53 and uses Lua on 0.55+), Sway 1.11, niri 26.04, Wayfire 0.9.0, KWin 6.7.

## How to read the verification column

- **running**: inua uses it today in real Hyprland sessions.
- **docs**: read in the compositor's documentation, protocol XML, man page or
  source (named per row); not run here.
- **table**: the compositor-support table on wayland.app for that protocol,
  as read on the date above. It is a community table; a version there is a
  claim, not a session check.
- **inferred**: follows from the documented model but no statement was
  found; needs a session check.
- **unknown**: nothing found.

When this document was written nothing in it had been run on Sway, niri,
Wayfire or KWin. niri 26.04 and Wayfire 0.11 have since been run
([compositor-experiments.md](compositor-experiments.md)); what that changed
here is listed under "Corrections from the sessions" at the end, and the
tables keep their original marks. Sway and KWin are still documentation
only.

## Sources

| Short name | Source |
| --- | --- |
| wayland.app | `https://wayland.app/protocols/<name>` compositor tables for `wlr-layer-shell-unstable-v1` (latest 5), `ext-session-lock-v1` (1), `wlr-foreign-toplevel-management-unstable-v1` (3), `ext-foreign-toplevel-list-v1` (1), `ext-workspace-v1` (1), `wlr-screencopy-unstable-v1` (3, deprecated), `ext-image-copy-capture-v1` (1), `ext-image-capture-source-v1` (1), `wlr-output-management-unstable-v1` (4), `virtual-keyboard-unstable-v1` (1), `wlr-virtual-pointer-unstable-v1` (2), `xdg-activation-v1` (1), `xdg-foreign-unstable-v2` (1), `hyprland-toplevel-export-v1` (2) |
| niri-ipc | `niri-ipc/src/lib.rs` on the niri `main` branch: `Request`, `Event`, `Action`, `Window`, `Workspace`, `Output`; the niri wiki pages *IPC* and *Workspaces* |
| sway-ipc | `sway-ipc(7)`: header, message and event types, reply fields; `sway(5)` for commands |
| wayfire-ipc | Wayfire `master`: `plugins/ipc/ipc.cpp` (socket, framing), `plugins/ipc-rules/ipc-rules.cpp`, `ipc-rules-common.hpp`, `ipc-events.hpp` (methods, view/output/wset JSON, events); the `pywayfire` client `wayfire/ipc.py` for the method list of other plugins (`vswitch`, `wsets`, `command`, `wayfire/*`) |
| kwin | KDE bug 502647 (wlr foreign toplevel: *resolved intentional*, pointing at `plasma-window-management`); the KWin scripting API page on develop.kde.org (KWin 6.0: `windowAdded`, `windowRemoved`, `windowActivated`, `desktopsChanged`, `callDBus`); `org.kde.KWin.VirtualDesktopManager` on D-Bus |

## Protocol support

Versions are from wayland.app on 2026-11-04 (**table**), except the Hyprland
row's use by inua (**running**) and the inua column, which is what the code
binds or needs today (from the audit's protocol section).

| Protocol | inua needs | Hyprland | Sway | niri | Wayfire | KWin |
| --- | --- | --- | --- | --- | --- | --- |
| `zwlr_layer_shell_v1` | v2 (`set_layer`), v4 (`keyboard_interactivity` on demand) | 5 | 4 | 5 | 4 | 5 |
| `ext_session_lock_v1` | 1 | 1 | 1 | 1 | 1 | — (KWin locks with its own kscreenlocker path) |
| `xdg_activation_v1` | via GDK | 1 | 1 | 1 | 1 | 1 |
| `zxdg_exporter_v2` / `importer` | via GDK (`SetForeignParent`) | — | 1 | 1 | 1 | 1 |
| `zwlr_foreign_toplevel_manager_v1` | 3 (handles for previews; could carry list, state, activate, close) | 3 | 3 | 3 | 3 | — (refused, bug 502647) |
| `ext_foreign_toplevel_list_v1` | not used; the handle other ext protocols key on | 1 | 1 | 1 | — | — |
| `ext_workspace_v1` | not used | 1 | — | 1 | — | — |
| `zwlr_screencopy_v1` | by `grim`, `wf-recorder` | 3 | 3 | 3 | 3 | — |
| `ext_image_copy_capture_v1` | not used | — | 1 | — | — | — |
| `ext_foreign_toplevel_image_capture_source_manager_v1` | not used; the standard way to capture one window | — | 1 | — | — | — |
| `ext_output_image_capture_source_manager_v1` | not used | — | — | — | — | — |
| `hyprland_toplevel_export_v1` + `hyprland_toplevel_mapping_v1` | 2 and 1: live previews, window picker cards | 2 | — | — | — | — |
| `zwlr_output_manager_v1` | not used (display settings go through IPC/config) | 4 | 4 | 4 | 4 | — (kscreen over D-Bus) |
| `zwp_virtual_keyboard_manager_v1` | 1 (Mobile Link) | 1 | 1 | 1 | 1 | — |
| `zwlr_virtual_pointer_manager_v1` | 1 (Mobile Link) | 2 | 2 | 2 | 2 | — |

Note on Hyprland: the table lists `xdg_foreign` as unsupported at 0.52.1 while
inua calls `SetForeignParent` there; the audit records that the call returns
false where the protocol is absent and the parent is simply not set, so the
table and the code agree. It is listed to show that the standard rows are not
all green even for the compositor inua runs on.

## Control channels

| | Hyprland | Sway | niri | Wayfire | KWin |
| --- | --- | --- | --- | --- | --- |
| Channel | Two Unix sockets from `HYPRLAND_INSTANCE_SIGNATURE`: text/JSON requests and a text event stream; `hyprctl` dispatchers; Lua `eval` on 0.55+ | `SWAYSOCK`: i3 IPC, `i3-ipc` magic, two native-endian u32 (length, type), JSON payload; `RUN_COMMAND` runs the config language | `NIRI_SOCKET`: one JSON request per line, one JSON reply (`Ok`/`Err`); `EventStream` keeps the connection and "will always give you the full current state up-front" | `WAYFIRE_SOCKET` (set by the `ipc` plugin; `_WAYFIRE_SOCKET` overrides): u32 host-order length + JSON `{"method","data"}`; needs the `ipc` and `ipc-rules` plugins in `[core] plugins`; other plugins add methods; `list-methods` enumerates | No window IPC for third parties. D-Bus `org.kde.KWin` (scripting, virtual desktops, kscreen), `plasma-window-management` and `plasma-virtual-desktop` Wayland protocols made for Plasma's own shell, KWin scripts (JavaScript in-process, loadable over D-Bus until restart) |
| Verification | running | docs (`sway-ipc(7)`) | docs (niri-ipc) | docs (wayfire-ipc) | docs (kwin); whether a non-Plasma client may bind `plasma-window-management` is **unknown** |
| Version handshake | none; Lua probed by `eval` returning `ok` | `GET_VERSION` | `Version` request; `ReturnError` for probing | none; `list-methods` and `wayfire/configuration` | D-Bus introspection |

## Capability matrix by feature

One row per audit feature. Each cell: what the compositor offers (source
name), then the verification mark. "—" means nothing found that provides it;
"plugin" means it exists only as a compositor plugin, script or Wayfire
plugin method; "config" means only by writing the compositor's configuration.

| Feature (audit row) | Needs | Hyprland | Sway | niri | Wayfire | KWin |
| --- | --- | --- | --- | --- | --- | --- |
| Window list, titles, running apps | windows with stable id, title, app id, pid, workspace, mapped, focus; change events | `j/clients`, `activewindow`, `focusHistoryID`; events (running) | `GET_TREE` nodes `id`, `name`, `app_id`, `pid`, `focused`, `visible`, workspace by tree position; `window` events `new`, `close`, `focus`, `title`, `move` (docs). XWayland nodes have `app_id` null (docs) | `Windows`; `Window{id,title,app_id,pid,workspace_id,is_focused,is_floating,is_urgent,layout,focus_timestamp}`; `WindowsChanged`, `WindowOpenedOrChanged`, `WindowClosed`, `WindowFocusChanged` (docs) | `window-rules/list-views`; view `id`, `pid`, `title`, `app-id`, `output-id`, `wset-index`, `mapped`, `activated`, `focusable`, `role`, `last-focus-timestamp`; events `view-mapped`, `view-unmapped`, `view-focused`, `view-title-changed`, `view-app-id-changed` (docs). Which workspace of the grid a view is on is derived from its geometry against the output's current `workspace` (inferred) | `plasma-window-management` or a KWin script bridging `workspace.windowList()` and `windowAdded`/`windowRemoved`/`windowActivated` to D-Bus (plugin; docs) |
| MRU order | focus history per window | `focusHistoryID` (running) | `focus` arrays per node (docs) | `focus_timestamp` on `Window`, `WindowFocusTimestampChanged` (docs) | `last-focus-timestamp` (docs) | script-side bookkeeping (plugin) |
| Bar workspaces, scroll, per-monitor title | workspace list with id, output, active, focused; switch | `j/workspaces`, `monitors.activeWorkspace`, `dispatch workspace` (running) | `GET_WORKSPACES` `num`, `name`, `output`, `visible`, `focused`, `urgent`; `workspace` command (docs). Names are the identity; numbers optional | `Workspaces`; `Workspace{id,idx,name,output,is_active,is_focused,active_window_id}`; `FocusWorkspace{Id|Index|Name}`; `WorkspacesChanged`, `WorkspaceActivated` (docs). Workspaces are per output, dynamic, one empty one always at the end; `idx` shifts as they move (docs) | Per output a workspace **set** with a 2-D grid: `window-rules/list-outputs` gives `wset-index` and `workspace{x,y,grid_width,grid_height}`; `vswitch/set-workspace{x,y,output-id}`; events `wset-workspace-changed`, `output-wset-changed` (docs). No workspace list to enumerate: the grid is the list | `org.kde.KWin.VirtualDesktopManager` (D-Bus) desktops with id, index, name; current desktop per activity, global across outputs (docs) |
| Multi-monitor surfaces | layer-shell per GDK monitor; focused output | layer-shell 5; `j/monitors` focused (running) | 4; `GET_OUTPUTS` `focused` (docs) | 5; `FocusedOutput`; `Output.name` (docs) | 4; `window-rules/get-focused-output`, `output-gain-focus` (docs) | 5; focused screen from script `workspace.activeScreen` (plugin) |
| Display modes, scale, mirror, lid | enumerate outputs with modes; apply; persist | `j/monitors all`, `keyword monitor`, generated config (running) | `GET_OUTPUTS` with `modes`; `output NAME mode|scale|position|transform|enable|disable` at run time; persistence is the user's config (docs) | `Outputs` with `modes`, `current_mode`, `vrr`; `Output{output, action}` request for mode/scale/transform/position at run time; persistence in KDL config (docs) | `output-layout-changed` event carries the full configuration; `wayfire/set-config-options` writes `output:NAME/mode` etc. and the config file is the persistence (docs) | kscreen D-Bus and `kscreen-doctor` (docs); no wlr-output-management |
| Focus a window; close, float, fullscreen, center, move | actions by window id | dispatchers, Lua (running) | `[con_id=N] focus`, `kill`, `floating toggle`, `fullscreen`, `move position center`, `move container to workspace` (docs) | `FocusWindow{id}`, `CloseWindow{id}`, `FullscreenWindow`, `ToggleWindowFloating`, `MoveWindowToWorkspace{window_id, reference, focus}` (docs). "Center" is a column op (`CenterColumn`); absolute placement only for floating windows (docs) | `window-rules/focus-view{id}`, `close-view`, `configure-view{id, geometry, output_id, fullscreen, tiled-edges, sticky}`, `wm-actions/set-fullscreen`, `set-minimized` (docs) | script: `workspace.activeWindow = w`, `w.closeWindow()`, `w.fullScreen`, `w.setMaximize`, geometry (plugin) |
| Focus without pointer warp | an activation that does not move the cursor | `cursor:no_warps` toggled around the focus (running) | `mouse_warping none|output|container` option, settable at run time by command (docs) | `warp-mouse-to-focus` is a config option, off unless set (docs) | focus-view does not warp; whether any plugin warps is unknown | KWin warps only for keyboard-driven focus with the "focus follows mouse" family of policies (docs; needs check) |
| Overview: windows per workspace, geometry, New card per monitor | window geometry in output coordinates; workspace ↔ output; create a workspace | `at`/`size`, workspace ids, reserved ids (running) | node `rect` absolute; workspaces named on demand: `workspace NAME` creates (docs) | `Window.layout` has the tile position in the workspace view and sizes (docs); the empty trailing workspace is the "New" slot and a window moved there creates the next (docs); no explicit create | view `geometry`, `bbox`; a cell of the grid is always there, so "New" is a free cell (inferred) | virtual desktops `createDesktop` over D-Bus; window geometry via script (plugin) |
| Overview drag: move without follow, route workspace to monitor | move window to workspace without focusing it; move workspace to output | `window.move(follow=false)`, `workspace.move` (running) | `[con_id=N] move container to workspace X` moves without focusing (docs); `move workspace to output` (docs) | `MoveWindowToWorkspace{focus:false}`; `MoveWorkspaceToMonitor` (docs) | `vswitch/send-view{x,y,view-id}` (docs); whether it keeps focus is **unknown**; `wsets/set-output-wset` swaps whole sets, not one workspace (docs) | script `w.desktops = [...]`, `w.output = ...` (plugin) |
| Live previews of every window | per-window image stream not limited to what is on screen | `hyprland_toplevel_export` (running) | `ext_foreign_toplevel_image_capture_source` + `ext_image_copy_capture` (table: Sway 1.11 has both) | — (table: niri lacks both capture protocols) | — (table) | — ; the portal gives a PipeWire stream per window with a user prompt, not a silent shell preview |
| Dock avoidance, autohide | layer-shell exclusive zone | running | 4 (docs) | 5 (docs) | 4 (docs) | 5 (docs) |
| Shortcuts: read, set, record | enumerate binds; change at run time; a mode that swallows keys | `j/binds`, `bind`/`unbind`, submaps (running) | `GET_BINDING_MODES`, `bindsym`/`unbindsym` and `mode` by `RUN_COMMAND`; `GET_BINDING_STATE` (docs). No bind listing except `GET_CONFIG` text | config only (KDL `binds {}`); niri reloads the file when it changes; no bind IPC (docs). An include mechanism for a generated file: **unknown** | `command/register-binding{binding, exec-always, mode, call-method, call-data}`, `unregister-binding`, `clear-bindings` (docs); no list of existing bindings beyond the config options (`wayfire/list-config-options`) | KGlobalAccel over D-Bus (docs) |
| Super tap, Super+Tab, number slots | observe raw key press/release while other windows have focus | Lua `input.keyboard.key` listener, `is_key_down`, custom events (running) | — ; only bindings (`bindsym --release`) (docs) | — ; bindings only (docs) | `keyboard-modifier-state-changed` event gives modifier state; bindings with `mode` (docs); raw key events **unknown** | — ; KWin scripts cannot see raw keys (docs) |
| Screenshot, picker, recording | capture an output, a region, one window; window geometry | `grim`, `wf-recorder`, export stream (running) | `grim`/`wf-recorder` on screencopy 3; a window by its `rect` while visible; a hidden window by capture source (table) | screencopy 3 for outputs and visible regions; niri's own `Screenshot` actions and `ScreenshotCaptured` event (docs); no hidden-window capture (table) | screencopy 3 (table); no window capture | portal only (`xdg-desktop-portal-kde`, PipeWire) (docs) |
| Portal screen share picker | a portal backend that asks inua which source | `xdg-desktop-portal-hyprland` with an external picker (running) | `xdg-desktop-portal-wlr` picks with `slurp`/its own chooser (docs); whether it can call an external picker: `chooser_cmd` in its config (docs) | `xdg-desktop-portal-gnome` on niri's `org.gnome.Mutter.ScreenCast` implementation (docs) | `xdg-desktop-portal-wlr` (docs) | `xdg-desktop-portal-kde` (docs) |
| Lock screen and lock restore | ext-session-lock; policy after the locker dies | 1 + `allow_session_lock_restore` (running) | 1; sway keeps the lock when the client dies and lets a new locker take over (docs) | 1 (table); restore policy **unknown** | 1 (table); restore policy **unknown** | — ; KWin's lock is kscreenlocker's; an external locker cannot lock the session (docs) |
| Phone touchpad and keyboard | virtual pointer and keyboard | running | 2 and 1 (table) | 2 and 1 (table) | 2 and 1 (table) | — ; `org_kde_kwin_fake_input` is for privileged clients (docs) |
| Paste into a window; dictation insert | focus the target by id and send Ctrl+V | `sendshortcut` by address (running) | `[con_id=N] focus` then a virtual-keyboard key (docs; two steps) | `FocusWindow` then virtual keyboard (docs; two steps) | `focus-view` then virtual keyboard (docs; two steps) | — |
| Activation token, dialog parent | xdg-activation, xdg-foreign via GDK | running | 1, 1 (table) | 1, 1 (table) | 1, 1 (table) | 1, 1 (table) |
| Bring a window here, place beside, tile | find window by pid; move to current workspace; set geometry | `movetoworkspace`, `movewindowpixel`, `resizewindowpixel` (running) | `[pid=N] move container to workspace current`, `move position`, `resize set` (docs) | window by `pid` in `Windows`; `MoveWindowToWorkspace`; floating `MoveFloatingWindow`, `SetWindowWidth/Height` (docs); tiled geometry is the column's | `configure-view{geometry, output_id}` (docs) | script (plugin) |
| Desk API (assistant, MCP, Workshop) | the same reads and actions as the shell | running | as above | as above | as above | as above |
| Session start and first configuration | a session script; generated config loaded by the compositor | `inua-session`, `hyprland.conf`/`.lua`, `source` (running) | `sway` session; `include` in config (docs) | `niri-session`; config reload on change (docs); include **unknown** | `wayfire` session; `wayfire.ini`; `wayfire/set-config-options` at run time (docs) | Plasma session; a non-Plasma shell on KWin is outside KWin's supported use |
| Virtual output for remote desktop | create a headless output at run time | `output create headless` (running) | `create_output` on the headless backend (docs; sway creates one at start) | — (docs; nothing in `Request`/`Action`) | `wayfire/create-headless-output{width,height}`, `destroy-headless-output` (docs) | — |
| Blur behind the bar, window and layer rules | compositor styling by namespace/class | `layerrule`, `windowrule` (running) | — (no blur); window rules by `for_window` (docs) | `layer-rule { match namespace=... }` with `block-out-from`, `shadow`; no blur (docs) | `blur` plugin (docs); per-layer selection **unknown** | KWin blur effect reacts to a KDE-specific surface hint (docs) |

## What needs plugins, scripts or internal APIs

- **KWin**: everything about windows and focus. KWin refused the wlr protocol
  on purpose and offers `plasma-window-management` to its own shell; a
  third-party shell would need a KWin script (loaded over D-Bus, gone on
  restart unless installed as a package) that bridges windows, geometry and
  actions to D-Bus, plus KGlobalAccel for shortcuts, kscreen for displays
  and the portal for every capture. Lock, virtual input and headless outputs
  have no third-party path. This is a port of a different kind from the
  three wlroots- and Smithay-based compositors, and this document does not
  propose it.
- **Wayfire**: all control is plugin methods. The `ipc` and `ipc-rules`
  plugins must be enabled in the user's `wayfire.ini`; `vswitch`, `wsets`,
  `command` and `wm-actions` add the rest. A missing plugin shows as an
  unknown method, so each capability must be probed with `list-methods`, not
  assumed from the compositor version.
- **niri**: nothing to install, but no window capture, no run-time binds, no
  headless output, no raw key observation. Super tap and number slots need
  bindings in the user's config; live previews have no protocol at all.
- **Sway**: binds and modes are commands, so recording and Super+Tab work as
  bindings; a Super tap (press and release with nothing in between) is a
  `bindsym --release` whose semantics differ from Hyprland's listener.
- **Hyprland**: today's Lua listeners, toplevel export and `cursor:no_warps`
  toggling are the Hyprland-internal parts the audit named; they stay in the
  Hyprland backend.

## Proposed minimum usable baseline

A compositor is **supported** when inua's bar, dock, launcher, notifications,
control center, settings (without the display and shortcut pages) and lock
screen work on it, and every other feature either works or says that it does
not. That requires:

1. `zwlr_layer_shell_v1` ≥ 4 and `ext_session_lock_v1`, both through
   gtk4-layer-shell as today.
2. `xdg_activation_v1`; `xdg_foreign` is optional (dialog parenting degrades
   as the audit already records).
3. A **windows** source: each window with a stable id for the compositor's
   lifetime, title, app id, pid, workspace membership, focus, mapped state;
   change events; and the actions focus and close.
4. A **workspaces** source: each workspace with an id, its output, active and
   focused flags; a switch action; change events. On Wayfire the id is the
   grid cell `(wset, x, y)`.
5. Output names that equal the connector names GDK reports, so bars and
   popups land on the right monitor.
6. The whole state readable at connect time, and a reconnect that rebuilds it
   (niri's event stream and Hyprland's queries do; Sway's `GET_TREE` does;
   Wayfire's `list-views` plus `list-outputs` does).

Everything else is **optional** and declared per backend:

| Optional capability | Used by | Without it |
| --- | --- | --- |
| Window geometry | Overview cards, window picker, dictation hint placement | Overview lists windows as titles in equal cards; picker shows titles; the hint sits in a corner |
| Live previews | Overview, picker cards | cards without images (the audit's existing degraded state) |
| Move without follow, workspace routing, create | Overview drag and New | Overview is read-only: cards activate, no drag, no New |
| Focus without warp | Dock click, clipboard paste | the pointer may move; nothing else changes |
| Binds read/write and a recording mode | Settings → Shortcuts | the page is hidden, with the compositor's own way named |
| Raw key observation | Super tap, Super+Tab, number slots | these come from bindings the sample config installs; Super tap is not offered where there is no release binding |
| Output capture, window capture, recording | Screenshot, Record | the buttons that need a missing part are hidden; what remains stays |
| Portal picker | screen sharing | the backend's own chooser |
| Virtual input | Mobile Link remote input | the phone's touchpad page says the compositor does not allow it |
| Send a key to a window | paste from history, dictation insert | copy only, and a notification that says to paste by hand |
| Headless output | remote desktop without a monitor | remote desktop needs a monitor; the setting is hidden |
| Display configuration | Settings → Displays | the page shows the outputs read-only and names the compositor's tool |
| Lock restore policy | recovery after a locker crash | documented per backend; no UI |
| Compositor styling rules | blur, no-anim, rules | the sample config for that compositor does what it can |

## Behaviour of the three states

Every optional capability is in one of three states, decided by the backend
and visible to the UI as data, never guessed from the compositor's name:

- **Supported**: the control works as on Hyprland.
- **Temporarily unavailable**: the backend is connected but the capability is
  not answering (a Wayfire plugin not loaded yet, niri reloading its config,
  the IPC socket reconnecting). The control stays, the last state is shown,
  and an attempt reports the error where [design/ui.md](../design/ui.md) puts
  errors for that control; the backend retries with the audit's backoff. The
  Launcher's "disconnected" line is the existing example.
- **Unsupported**: the compositor cannot do it. The control is not shown, or,
  where hiding would leave a gap the user cannot explain (a settings page), a
  single line in the page says what is missing and on which compositor. No
  silent success, no simulated result.

Configuration follows the same rule: a setting whose capability is
unsupported is not written to that compositor's config, and `inua config
apply` says which settings it skipped.

## Recommendation for the first additional compositor

The owner has already chosen niri and Wayfire as the experiment targets of
#61. For the first *integration* (#64) this document recommends **niri**:

- Its model is the closest to what inua's state already assumes: a list of
  workspaces per output with ids and a focused one, windows with ids, a focus
  history, and an event stream that starts with the full state. The audit's
  "positive integer workspace id, one output each" assumption breaks (ids are
  `u64`, the index moves), which is the right thing to break first.
- It has the most standard protocols of the four alternatives except Sway:
  `ext_workspace_v1`, both foreign-toplevel protocols, layer-shell 5, session
  lock, virtual input, output management.
- Its IPC is versioned, documented in one Rust file, and needs no plugin.
- What it lacks is clear and maps to the optional list: no window capture
  (Overview without previews), no run-time binds (sample config), no headless
  output (remote desktop needs a monitor), no raw keys (Super tap from a
  binding is not available; Super+Tab is).

**Wayfire** second: its IPC can do more (run-time bindings with a method
call, headless outputs, geometry, modifier events), which matters for remote
desktop and shortcuts, but its 2-D grid workspace sets are a different model,
every method lives in a plugin the user must enable, the only window capture
is screencopy of what is visible, and the standard list and workspace
protocols are absent. It is the better check that the workspace contract is
honest, which is why it stays an experiment target.

**Sway** is noted, not recommended: it has the richest standard protocol set
(the only one with per-window capture through `ext` protocols at 1.11) and a
mature IPC, but the owner did not select it. Should the niri experiment fail
on previews being essential, Sway is the compositor where previews can be
done with standard protocols.

**KWin** is not recommended for any round: see above.

The owner's decision on this recommendation is required before #64 starts;
#61's experiments run on both targets regardless.

## Corrections from the sessions

Observed on niri 26.04 and Wayfire 0.11 on 2026-11-05; each replaces the
matching cell above.

- **niri window geometry**: `Window.layout` places floating windows only.
  For a tiled window `tile_pos_in_workspace_view` is null, so the Overview's
  cards have no window layout on niri and geometry is not a capability
  there.
- **niri errors**: an action on a window id that does not exist is
  answered `Ok`. Only malformed requests are `Err`.
- **niri focus events**: a new window's focus comes inside
  `WindowOpenedOrChanged`, with no `WindowFocusChanged`.
- **Wayfire `focus-view`** switches to the view's cell itself and leaves
  the pointer alone; **`vswitch/send-view`** does not take the focus.
- **Wayfire list methods** answer with arrays; a new output is announced
  by `output-layout-changed`, not `output-added`.
- **Wayfire's packaged version** on Arch is 0.11 with an `ext-toplevel`
  plugin, so `ext_foreign_toplevel_list_v1` is there on 0.11 where the
  table (0.9.0) says it is not. Not otherwise used.
- **On-demand keyboard focus** for a layer surface: given as it appears on
  Hyprland and Wayfire, only on a click on niri. A new row the matrix did
  not have; it is `CapOnDemandFocus` in the contract.
- **Virtual keyboard**: niri honours a virtual keyboard's own keymap for a
  focused window.
- **The first integration**: both niri and Wayfire backends exist now, so
  the recommendation's order no longer decides anything.

## Open points for #61

- Whether niri's `Window.layout` positions are enough for Overview cards.
- Whether `vswitch/send-view` keeps focus, and how a view's grid cell is
  derived on Wayfire.
- Both compositors' behaviour when the shell's IPC client disconnects and
  reconnects, and when the compositor restarts under the shell.
- Event order on window open: does the focus event come before or after the
  window appears in the list, on each.
- Whether niri's config supports an include for generated binds.
