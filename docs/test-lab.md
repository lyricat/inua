# Running a test desktop of your own

How to test the shell's UI without touching the session you are working
in, and how the working example on the Debian machine (`debian`) is built.
It is the reference: it runs Hyprland on a virtual output, inside a VM,
with the shell on top, reachable over VNC.

Read it as a recipe, not a description of this repository's scripts: the
Debian machine keeps its copies in `~/.local/bin` and
`~/.config/inua-wayland`, outside any checkout.

## Why a plain nested Hyprland on NixOS fails

A nested Hyprland (one compositor as a client of another) needs a backend.
On the workstation (`hx90`, NixOS, one DRM seat held by the session) every
route was tried and each failed at the same place:

| Attempt | What happens |
| --- | --- |
| Nested, its own `XDG_RUNTIME_DIR` | Hyprland falls back to the DRM backend, takes the real seat, and the session's own devices are contested |
| `monitor = HEADLESS-1` with `WLR_BACKENDS=headless` | aquamarine ignores `WLR_*`; `CBackend::create() failed` |
| Nested inside a headless sway | `Wayland backend cannot start: Missing protocols` |
| `-vga std` / `virtio-vga` / `virtio-vga-gl` in a QEMU guest | the backend starts, the compositor ends after `AsyncResourceGatherer` |
| seatd instead of logind | `Could not add client: seat is VT-bound and has an active client` as soon as a client remains |

The common cause is **libseat**: aquamarine asks it for the DRM device and
the input devices, and a session without a seat (SSH, a nested compositor,
a container) cannot be given one.

## What the Debian machine does instead

It runs Hyprland **as a systemd user service, in a VM whose seat belongs to
it**, and takes the virtual output itself:

```
inua-wayland.service  (user)
  └─ dbus-run-session -- Hyprland -c ~/.config/inua-wayland/hypr/hyprland.conf
       ├─ exec-once: inua-wayland-display   → hyprctl output create headless DEV-1
       └─ exec-once: inua-shell-wayland     → the shell

inua-wayland-vnc.service      wayvnc 127.0.0.1 5901
inua-wayland-novnc.service    websockify 127.0.0.1:6082 127.0.0.1:5901
```

Three things make it work, and each is load-bearing:

1. **`LIBSEAT_BACKEND=noop`** — libseat is told not to consult seatd or
   logind. The process already has the DRM device (it runs as the session
   that owns the VM's console), so asking for a seat is both unnecessary
   and the thing that fails. This is the single most important variable.
2. **`WLR_RENDERER_ALLOW_SOFTWARE=1`** — the VM has `bochs-drm`, a plain
   framebuffer device with no GPU behind it; the renderer must be allowed
   to fall back to software.
3. **The output is created explicitly** — `hyprctl output create headless
   DEV-1` then `keyword monitor "DEV-1,1366x768@60,0x0,1"`, and the real
   `Virtual-1` is disabled. Hyprland's own `headless` backend is not used;
   a virtual *output* on the DRM device is, which is what keeps the whole
   thing inside the compositor that is already running.

The environment the launcher sets (`~/.local/bin/inua-wayland-session`):

```sh
export XDG_CONFIG_HOME=$HOME/.config/inua-wayland   # config kept apart
export XDG_SESSION_TYPE=wayland
export XDG_CURRENT_DESKTOP=Hyprland
export GDK_BACKEND=wayland
export LIBSEAT_BACKEND=noop
export WLR_RENDERER_ALLOW_SOFTWARE=1
export HYPRLAND_NO_CRASHREPORTER=1
unset WAYLAND_DISPLAY HYPRLAND_INSTANCE_SIGNATURE DISPLAY XAUTHORITY
exec dbus-run-session -- Hyprland -c $XDG_CONFIG_HOME/hypr/hyprland.conf
```

Dropping the inherited `WAYLAND_DISPLAY`/`DISPLAY` matters: without it a
nested start inherits the outer session's socket and the two compositors
fight over the same connection.

## Watching and driving it

| What | How |
| --- | --- |
| screen | `wayvnc 127.0.0.1 5901`, then a VNC viewer |
| browser | `websockify --web=/usr/share/novnc 127.0.0.1:6082 127.0.0.1:5901`, then `http://127.0.0.1:6082` |
| state | `hyprctl monitors`, `hyprctl clients` with the compositor's own `XDG_RUNTIME_DIR` and `HYPRLAND_INSTANCE_SIGNATURE` |
| the shell | its control socket, `$XDG_RUNTIME_DIR/inua/<instance>.sock` |
| the screen file | `grim` inside the same `WAYLAND_DISPLAY` |

`hyprctl` must match the compositor's libraries: the Debian machine runs
Hyprland from `~/.local/opt/inua-build`, so `LD_LIBRARY_PATH` has to point
there for `hyprctl` to load at all. A stock `hyprctl` from the system will
fail with `libhyprutils.so.*: cannot open shared object file`.

## Doing it on NixOS

The recipe above is what a NixOS test VM needs, and it is what the
attempts on `hx90` were missing:

- a **user** service, not a system one: a system unit has no user session,
  and `dbus-run-session` under a system unit has no bus to talk to;
- `LIBSEAT_BACKEND=noop`, so nothing asks for a seat;
- the guest's `qemu` display left alone (`-nographic` or `-display none`),
  because the compositor renders into a virtual output, not into QEMU's
  window;
- a cursor theme installed (without one, `AsyncResourceGatherer` never
  finishes and the compositor ends silently);
- the XKB data present at the system path as well as in the environment;
- the shell's own library environment, so a `buildGoModule` binary can
  find `puregotk`'s libraries.

## What is not worth doing

Fighting `bochs-drm` with `virtio-vga-gl` and `-display egl-headless` cost
more time than it saved: the GL path needs a working EGL on the host, and
the plain framebuffer with software rendering is what the working example
uses. The Debian machine proves the simple path works.

## Checking the menu without a pointer

The shell answers on its own control socket, which is how the bar's menu
can be driven and read without moving the pointer:

```sh
socat - UNIX-CONNECT:$XDG_RUNTIME_DIR/inua/default.sock \
  <<< '{"op":"call","target":"bar","function":"state"}'
```

`bar appmenu` opens the application's menu (About, Close Window, Quit and
the application's own sections), `bar closemenu` closes it, `bar state`
prints what is shown - open, pid, section, item count and the card's box -
and `bar hover <x> <y>` runs the bar's hover at bar coordinates. Those
four are what a UI test should use: they read the shell's own state, so a
test does not depend on the pointer, on focus, or on a screenshot.
