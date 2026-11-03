# Wayfire runtime evidence

This evidence records an isolated Wayfire 0.9.0 run using the requested headless/GLES2 recipe. Wayfire selected `wayland-1` inside its unique runtime directory; the recipe's literal `WAYLAND_DISPLAY=wf-accept` does not match the automatically chosen socket name. Setting `WAYLAND_DISPLAY=wayland-1` allowed clients to connect. Inua was run from the checkout's `bin/inua-shell` with the locally built GTK4 layer-shell 1.3 library.

The run used a build that also carries the shell wiring on a private working branch. That wiring is not part of this PR, which supplies the Wayfire client and compositor abstraction; the screenshots show the runtime those connect to, not a shell in this PR driving it.

## Screenshots

All images are 1280×720 RGB PNGs captured by `grim`, visually inspected, and contain no hostname, username, IP address, absolute path, or private window title.

| File | State | RGB variance | SHA-256 |
|---|---|---:|---|
| [bar-wallpaper.png](bar-wallpaper.png) | Inua bar, dock and wallpaper; launcher/control center closed | R=1157.5, G=1132.0, B=1159.4 | `33908a82c1f10e343f93ee98b6b3e82a9692b94ca2749f556998910ac0351ac0` |
| [launcher.png](launcher.png) | Launcher open | R=2150.4, G=1785.2, B=1712.0 | `dbf8ef1da68a82fdefe078d16d5662b8dec19948a8065079ff3d1c6e04e55926` |
| [control-center.png](control-center.png) | Control center open | R=818.9, G=799.8, B=830.7 | `4ab8389da093859f6ca9ef03f71735b9de6ab4f434c64f043cd6219a717ec1d0` |

Each screenshot has a distinct hash and substantial pixel variance. A harmless test window was closed before these images were captured.

## Runtime results

[ipc.json](ipc.json) contains the four requested Wayland globals, the full 33-method runtime list, compositor state, focus/close/workspace results and shell command results. Focus, close and workspace switch were exercised through Wayfire IPC. The shell attached to `HEADLESS-1`; launcher and control center opened successfully.

**Overview remains unverified and unavailable in the current implementation.** `overview open` returns `overview is unavailable on Wayfire: compositor state and output capture are not connected`. No overview screenshot or cards are claimed. The requested method count was 28, but this installed Wayfire returned 33; all 33 observed methods are recorded.
