# Wayfire on Debian: validation record

## Scope and acceptance level

Target: Debian 13 (trixie), official Wayfire 0.9.0 package and wlroots 0.18.2. Runtime acceptance is **partially verified**: the isolated compositor exposed all requested Wayland globals, answered IPC, ran the checkout-built Inua shell, and produced three distinct sanitized screenshots. The shared overview remains unavailable because Wayfire output capture and overview state integration are not implemented; no overview cards or overview screenshot are claimed.

The runtime session and screenshots below come from a build that also carries the
shell wiring on a private working branch; that wiring is not part of this PR. This
PR supplies the compositor abstraction (`internal/compositor`), the Wayfire IPC and
foreign-toplevel client (`internal/wayfire`), the sample session files and the
Wayfire app-window titlebar. It is verified here by the client's unit tests,
`gofmt`, `go vet` and a full `go build`, not by the shell session.

The installed packages report `wayfire 0.9.0-5`, `libwlroots-0.18 0.18.2-3`, `libwf-config1 0.9.0-2`, and `wf-plugins 0.9.0-5`. `wayfire --version` prints `0.9.0-unknown (Dec 24 2024) branch unknown`.

## Isolated runtime evidence

The acceptance run used the required `WLR_BACKENDS=headless`, one headless output, GLES2 software renderer, `/dev/dri/card0`, `WLR_LIBINPUT_NO_DEVICES=1`, unique `XDG_RUNTIME_DIR`, copied `config/session/wayfire.ini`, and Wayfire's IPC socket path from the goal-owned process.

Measured from Wayfire's log, `wayland-info`, live IPC, and `grim`:

- Wayfire 0.9.0 started with the headless backend; Mesa llvmpipe rendered OpenGL ES 3.2.
- Output: `HEADLESS-1`, 1280×720. The requested 1440×900 was not applied by this Wayfire package/profile.
- The configured `WAYLAND_DISPLAY=wf-accept` was not the actual display socket name. Wayfire selected `wayland-1` under the new runtime. Setting `WAYLAND_DISPLAY=wayland-1` enabled clients; this explains the earlier connection failures.
- `wayland-info` found `zwlr_layer_shell_v1` v4, `zwlr_screencopy_manager_v1` v3, `zwlr_foreign_toplevel_manager_v1` v3, and `ext_session_lock_manager_v1` v1.
- `list-methods` returned 33 methods (the requested count was 28). All observed methods and normalized responses are recorded in [`docs/evidence/wayfire/ipc.json`](evidence/wayfire/ipc.json).
- `list-outputs` reported one 1280×720 output. `list-views` initially returned no windows; after shell startup it reported Inua's bar, background, dock and overlays. `get-focused-view` reported a test window when focused and the control-center overlay during capture.
- A synthetic Xwayland window focus request returned `ok` and `get-focused-view` confirmed Alpha activated. `close-view` on Beta returned `ok`, after which its client exited and its view disappeared. Both workspace switch to `(0,1)` and return to `(0,0)` returned `ok` and the output reflected the new coordinates.
- In that session the checkout-built `./bin/inua-shell`, run with the existing local GTK4 layer-shell 1.3 library, reported `layer=true bars=HEADLESS-1`; compositor status reported Wayfire connected with state query/events, focus, move, close and workspace-switch capabilities.
- `launcher toggle` and `control open` returned `ok`; Wayfire listed the `inua-control` overlay. `grim` captured bar/wallpaper, launcher and control-center states. The three committed PNGs have distinct SHA-256 hashes and measured nonzero RGB variance; see [the evidence README](evidence/wayfire/README.md).
- The system-installed layer-shell library lacks required session-lock symbols; the compatible existing local 1.3 library was used for the successful shell run. A stale binary outside the checkout was not used for final evidence; it is older than this checkout's `bin/inua-shell`.
- An earlier stale-binary run wrote a sample config into the local Hyprland config directory; that file was left unchanged. Unrelated running sessions and their runtimes were not signalled, queried for application state, or changed.

The remaining blocker is **overview behavior**: `overview open` returns `overview is unavailable on Wayfire: compositor state and output capture are not connected`. The required state could not be represented as an overview card, so no overview screenshot is included.

## Protocol and IPC contract (source tier)

The local Wayfire `v0.9.0` source and Debian package metadata establish these contracts; live responses are summarized above and recorded in `docs/evidence/wayfire/ipc.json`:

- `window-rules/list-views`, `list-outputs`, and `list-wsets` provide view IDs, title, app ID, PID, geometry, output and workspace-set/grid state.
- `window-rules/focus-view` and `window-rules/close-view` take `{"id": <integer>}`; both actions were exercised successfully with harmless test windows.
- `window-rules/configure-view` accepts `id`, `output_id`, `geometry` and `sticky`; it does not accept `wset-index`.
- `vswitch/set-workspace` takes unsigned `x`, `y`, and `output-id`, with optional `view-id`; switch and return were both exercised.
- `window-rules/events/watch` is a persistent stream. `view-workspace-changed` carries the actual destination coordinates. A window's `wset-index` is a workspace-set/output identifier, not its current viewport.
- The foreign-toplevel protocol supplies title, app ID, activation and output membership, not PID, geometry or viewport. Workspace membership is assigned only after a real viewport event; it is not inferred from the active viewport.
- Wayfire 0.9 provides the screencopy global in this runtime, and `grim` captured three full-output frames. Inua does not yet consume output frames for overview cards or window crops.

## Current implementation and capability boundary

The shell now consumes Wayfire's `zwlr_foreign_toplevel_manager_v1` snapshots
for its Dock and launcher window mode. Wayfire IPC handles window focus and
close requests. The shared overview still rejects Wayfire open requests
because output capture and overview state integration are not connected;
`compositor status` reports `WindowCapture=false`.

| Capability | Source implementation | Live runtime evidence |
|---|---|---|
| Wayland protocol globals | Wayland registry | All four requested globals present; versions recorded in IPC JSON |
| IPC method enumeration | `list-methods` | 33 methods returned and recorded |
| State snapshot | `list-views` / outputs / wsets | One output at 1280×720; Inua surfaces listed; synthetic test windows observed and removed |
| Focus / close | `window-rules/focus-view`, `close-view` | Both returned `ok`; focused view confirmed and closed view disappeared |
| Workspace switch | `vswitch/set-workspace` | Switch and return returned `ok`; output coordinates confirmed |
| Launcher / control center | Shell IPC and Wayland surfaces | Both opened; screenshots captured |
| Overview UI / cards | Shared GTK overview | Blocked: `overview open` returns unavailable on Wayfire because capture/state integration is missing |
| Output capture | `zwlr_screencopy_manager_v1`, `grim` | Three distinct full-output screenshots captured, hashed and variance-checked |
| Resize / workspace creation / shortcut listing / lock | Not implemented | Unsupported |

## Inua application windows

Wayfire 0.9's `decoration` plugin supplies server-side decorations for
Xwayland windows, not native GTK4 Wayland toplevels. The Inua session therefore
adds GTK's standard headerbar to each built-in app window while running under
Wayfire. GTK draws the title and minimize, maximize and close buttons; the
compositor still owns window placement and resizing. The shell's Settings
window uses the same headerbar. Other sessions keep their existing windows.

The shared app UI covers Files, Viewer, Mail, Terminal, Mobile Link, Remote
Desktop, Disk Usage, Virtual Machines, the Assistant, Games and registered
light apps. The application window-added hook also gives app-owned dialogs a
titlebar.

The SDDM entry “Inua on Wayfire” starts Wayfire with the bundled session
profile. Its autostart command starts the shell after Wayfire creates its
Wayland display and IPC socket. On first launch the wrapper copies the sample
`wayfire.ini` only when the user's config is absent.

## Dock and application switching

The Dock groups open windows by Wayfire's application ID, marks the active
window, and retains pinned app IDs from Inua's config. Its menu can pin,
unpin, launch and close apps; window focus and close use Wayfire's
`window-rules` IPC methods. `Super+Tab` opens Inua's launcher in window mode.
The foreign-toplevel protocol does not provide workspace membership, so the
window list does not show workspace names. Overview and per-window capture
remain unavailable.

## Remaining runtime acceptance

**BLOCKED:** Implement and verify Wayfire overview state integration and card output capture, then capture an overview-open screenshot and verify open/close behavior. The other measured runtime evidence and screenshots are committed.

## Screenshot evidence

Three sanitized screenshots are committed in `docs/evidence/wayfire/`: bar/wallpaper, launcher, and control center. No overview screenshot exists because the current implementation rejects that action on Wayfire.
