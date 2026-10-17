# Architecture

inua is a desktop shell that works with Hyprland. Its logic is Go, its interface GTK4
(the puregotk binding, no cgo), and its layouts Blueprint/`.ui` markup. This
document follows [design.md](design.md); why this toolkit is in
[toolkit.md](toolkit.md).

## Virtual machines boundary

`internal/vm` owns the machines, and none of it touches GTK. `machine.go`
holds what a machine is — its name and `os` (windows, linux, freebsd,
android or custom), its `backend` (qemu, dockur or waydroid), its memory,
CPUs and disk, its `[boot]`, `[display]` and `[seamless]` sections — and
what is wrong with it, field by field, in stable reasons a window words in the
user's language. `store.go` is the store: one folder per machine under
`~/.local/share/inua/vm/`, each holding a `machine.toml` beside the guest's
own files, its runtime files in `$XDG_RUNTIME_DIR/inua-vm/NAME/`. A folder
whose file does not hold is left out of the list and reported by the
diagnostics, which also name the folders that are not machines.
`templates.go` is the eight templates **New** offers, from Windows in Docker
(Dockur) to Linux, FreeBSD, Android and a custom system. The host checks and
the backends live beside them: this build wires the Dockur backend, the only
one that can run a guest today, while the QEMU and Waydroid ones are the
following steps. `report.go`'s report carries the machines now, not the
single saved configuration (`Config` and `Persisted` survive only to adopt a
machine from the previous version). The Linux platform detection, the
resource recommendation and the backend contract are unchanged: KVM, the
Docker socket, Compose v2, FreeRDP, a browser opener, TUN and disk space,
each with the reason it failed and, when Inua can apply one itself, a remedy
through `pkexec`, so the session's polkit agent takes the password and the
command never runs with the caller's rights. Supported Linux hosts are NixOS,
Ubuntu and Debian. The image is pinned to a version and digest, ports bind to
the loopback interface, each machine reserving its own, and the guest
password lives in the Secret Service, never in the project or the
configuration.

The manager is an app of its own — `cmd/inua-vm` and `internal/vmui`, an
`appui` view over `ui/vm.blp` — not a part of the shell: no VM window, no
drawer entry of its own and no `vm` control-socket target live in
`internal/ui`. `internal/vmui` also carries the thin command line. The
layout is the list of machines and the chosen machine's page; the page is one
stage at a time (`internal/vm.PageFor`): no machine yet, a fresh one to name
and give a template, a kind this build has no backend for, the host to fix,
the guest to install, then start, wait or connect. A guest is "installed"
only after it has come up *and* its own disk image is on disk, so a project
that is only a download cache stays on the install page. Removing a machine
deletes the container and the project but keeps its disk and its password, so
a later install reuses that Windows; deleting the disk, and deleting the
machine with its folder, are separate, confirmed actions, because losing an
installation should take a deliberate second step. `inua-vm --list`,
`--doctor [--json]` and `--status [--json]` (and `inua vm list|doctor|status`)
report the machines and the host's checks, read-only, without contacting
Docker; `--start NAME` and `--stop NAME` drive a backend from a script.

Docker socket access is privileged host access, so the platform layer
reports a socket this user cannot use as denied and offers no fix for it:
membership in the `docker` group is as good as root on the host. The window
explains that and points at rootless Docker, Podman or a QEMU machine; the
`kvm` group remedy, which grants one device, stays.

## 1. Layers

```
┌──────────────────────────────────────────────────────────────┐
│ Layout   ui/*.blp → ui/*.ui (widget trees, static props)  CSS  │  markup, loadable at run time
├──────────────────────────────────────────────────────────────┤
│ View     bar, dock, launcher, control center, notifications,   │  thin: state → widgets
│          settings: fetch named widgets, fill them, take events │
├──────────────────────────────────────────────────────────────┤
│ State    internal/shell: modes, search, selection, focus      │  toolkit-free, unit tested
│          rules, dock model (main thread)                       │
├──────────────────────────────────────────────────────────────┤
│ GTK glue internal/gtkx (ownership, callbacks, main thread,    │
│          .ui loading)   internal/layershell (gtk4-layer-shell) │
├──────────────────────────────────────────────────────────────┤
│ Services internal/hypr  internal/desktop  internal/ipc         │  no UI library, unit tested
│          internal/sni (tray)  internal/notify (notifications)  │
│          internal/system (system state)  internal/hyprconf     │
├──────────────────────────────────────────────────────────────┤
│ Outside  Hyprland's two sockets   XDG .desktop   unix socket   │
│          D-Bus: NetworkManager BlueZ UPower logind MPRIS tray  │
│          PipeWire (pw-dump / wpctl)   notifications (served)   │
└──────────────────────────────────────────────────────────────┘
```

Dependencies only point down. Services and shell state never import GTK;
the view uses the tricky parts of puregotk only through `gtkx`.

## 2. Layout of the repository

| Path | What | State |
| --- | --- | --- |
| `internal/hypr` | Hyprland requests, events, snapshots, the state store | Done, tested |
| `internal/capture/hyprland` | Standalone Wayland client for Hyprland's toplevel export protocol; streams SHM frames to the workspace overview | Implemented; runtime verification needs Hyprland |
| `internal/desktop` | `.desktop` parsing, Exec expansion, launching | Done, tested |
| `internal/ipc` | The control socket | Done, tested |
| `internal/sni` | Tray: StatusNotifierWatcher (when no one else provides it), host, dbusmenu menus | Done, tested (private dbus-daemon) |
| `internal/hyprconf` | Hyprland settings inua manages, written to `hypr/inua.conf` (`hypr/inua.lua` for a Lua config, Hyprland 0.55+): keyboard, touchpad, shortcuts (`unbind` + `bind`, the empty recording submap); actions and their commands; parsing `hyprctl binds` | Done, tested |
| `internal/lock` | The lock screen's password check through `unix_chkpwd` (no cgo, no PAM headers), and the wait after wrong passwords | Done, tested |
| `internal/convert` | The launcher's answers: arithmetic, units, currencies (rates from frankfurter.app, cached) | Done, tested |
| `internal/apps/calc`, `clock`, `weather`, `stocks`, `calendar`, `colors`, `sysmon`, `tailscale`, `chart`, `store` | The light apps' logic without GTK: expression evaluation; alarms, zones and time formats; Open-Meteo and Yahoo Finance clients; month grids and days' distance from today; colour forms; meters and processes from /proc and /sys, and signals to processes; tailscaled's local API (status, settings, pings, Taildrop); line charts drawn in Go (`golang.org/x/image/vector`); JSON data in `~/.local/share/inua` | Done, tested |
| `internal/apps/files`, `internal/apps/files/disks`, `internal/apps/files/fm1`, `internal/filesui`, `cmd/inua-files` | The file manager (docs/files.md): reading, sorting and filtering folders, the jump list, places and bookmarks, mounts, the freedesktop trash, operations with conflicts, progress and undo, searching, watching (inotify), thumbnails (the freedesktop cache); UDisks2 drives; `org.freedesktop.FileManager1` and handing paths to the running instance; the windows, whose signals go through one handler of each kind for the process | Done, tested (temporary folders; a fake UDisks2 and a second instance on private buses) |
| `internal/apps/viewer`, `internal/viewerui`, `cmd/inua-viewer` | Viewer (docs/apps.md#viewer): what kind a file is (by its name, or text by its start) and the files of its kind in its folder; images decoded in Go (libjpeg-turbo through purego when it is installed) and EXIF; PDF pages drawn by poppler-glib and cairo through purego; Markdown read with goldmark into blocks, and plain text; the window: the picture zoomed and turned by a transform, a PDF's pages drawn off the main thread as they come into view, Markdown set with text tags (tables and pictures as widgets in the text) | Done, tested (PDFs written by the tests, when poppler is installed) |
| `internal/appui`, `cmd/inua-apps` | The light apps' windows: one process and application ID per app, the shell's theme and icons followed while open; Music and Video take the files of a later start (GApplication's open) | Done |
| `internal/gst`, `internal/mpris`, `internal/apps/music`, `internal/apps/video`, `internal/appui/music.go`, `internal/appui/video.go` | Music and Video (docs/feat/players.md): GStreamer through purego (playbin; for music balance, preamp, a ten-band equalizer and a spectrum; for video the pictures from an appsink as textures), its bus read in a goroutine; an MPRIS player each; the playlist, shuffle and repeat, M3U, presets, places to resume, subtitles beside a video | Done, tested (a tone and a clip made with GStreamer, played into fakesink; the shell's MPRIS client on a private bus; the playlist's order) |
| `internal/mcpserver`, `cmd/inua-assistant/mcp.go`, `internal/assistant/mcp.go` | MCP (docs/mcp.md): inua's tools served on stdio by `inua-assistant --mcp` (`inua mcp`). Agents get them by `mcp.policy`, asked in the shell's dialog; the assistant (its child, told apart by its program) gets them through Mister Morph's MCP client, with its tool_search and codemode, asking as its settings say; its window's lines come from Mister Morph's events; a record of calls | Done, tested (the SDK's in-memory transport; the test binary as the server for Mister Morph's client) |
| `internal/desk` | inua's desk functions (docs/feat/desk.md), one table for the assistant, MCP and workshop apps: windows and workspaces, apps, the system and the shell's panels through the control socket, notifications and their history, the clipboard and its history, media, processes, Tailscale, weather; each with its risk, its workshop use and a line saying what a call does; a watcher that follows a topic by polling | Done, tested (fake desktops) |
| `internal/workshop`, `internal/appui/workshop.go`, `internal/appui/workshop_blocks.go`, `workshop/examples` | Workshop apps (docs/workshop.md): the manifest, JSON-RPC 2.0 a line at a time over the program's stdio, the session (handshake, the functions an app may call by its manifest's `uses`, the desk functions by name, topics followed, file dialogs, its store, timers, running commands), install and remove, the Python and JavaScript helpers; the window draws the building blocks (tabs, segmented choices, sliders, charts, tiles… as the light apps') and keeps widgets across renders | Tested (JSON-RPC both ways, Python, shell and JavaScript apps driven through a session, desk functions and topics with a fake desktop, install with a fake trash) |
| `internal/apps/gpg` | The GPG app's GnuPG: the keyring (`--with-colons`), encrypting and decrypting bytes and files, gpg's status lines (who it was for, who signed it), output names that overwrite nothing, the armored block in a text, showing and importing keys, creating a key, extending one, and deleting another person's | Done, tested (real gpg in a temporary home) |
| `internal/apps/pass` | The Pass app's store: entries by category, `pass show/insert/rm/mv`, an entry's fields and TOTP codes (RFC 6238), the stores to choose from | Done, tested (real pass and gpg in a temporary home) |
| `internal/games`, `internal/gamesui`, `cmd/inua-games` | The light games: their rules without GTK, and their windows, each game a light app of its own (`appui.Register`) | Done, tested |
| `internal/games/fortunenet` | Small Fortune on the local network: the lobby, join codes and seats' keys, moves over a WebSocket (gorilla/websocket), each browser's view of the game, the computer playing for whoever is gone; the page (`web/`, three.js) | Done, tested (a host on the loopback, browsers as WebSocket clients) |
| `internal/assistant`, `internal/assistantui`, `cmd/inua-assistant` | The assistant: Mister Morph's runtime with inua's desk functions (`internal/desk`) over MCP, approvals and a streamed answer, without GTK and tested against a scripted model; its window, a light app in a binary of its own; `--ask` reaches an open window through its GApplication action on D-Bus | Done, tested |
| `internal/keyring`, `cmd/inua-keyring` | The Secret Service (`org.freedesktop.secrets`) kept in pass: each item a pass entry under `inua-keyring/`, labels and attributes in an index beside them (re-read when changed, written under a lock); sessions `plain` and `dh-ietf1024-sha256-aes128-cbc-pkcs7`; collections, aliases, the in-memory session collection; a client for importing from gnome-keyring and for the assistant's keys; started by D-Bus activation; the secrets kept unlocked in memory of their own (`memfd_secret`, else mlock) until locked; the apps trusted with each secret (who a process is: its program or script and SHA-256, root-owned or not, Flatpak, LD_PRELOAD), in a list sealed with an HMAC whose key is in pass; the user asked in the shell's dialog; `Lock`, `State`, `Untrust`, `Reseal` on its own interface | Done, tested (fake and real pass, libsecret, private dbus-daemon, a fake /proc) |
| `internal/emoji` | The launcher's emoji and symbols: Unicode's emoji list and CLDR's English names and keywords (`scripts/emoji.py` rebuilds the table), searched by name and keyword | Done, tested |
| `internal/pinentry` | gpg-agent's pinentry protocol (Assuan) for `inua-pinentry`: the requests, the answers and gpg's error codes | Done, tested |
| `internal/polkit` | The session's polkit authentication agent: registering with polkitd for the shell's login session, `BeginAuthentication`/`CancelAuthentication`, and the conversation with polkit's helper (its socket, else the setuid program) that checks the password through PAM | Done, tested (private dbus-daemon, fake helper) |
| `internal/notify` | Notifications: the `org.freedesktop.Notifications` server (queues behind another daemon), markup cleaning, the notification center model (banner decisions, grouping by app, Focus, unread, saved to `$XDG_STATE_HOME/inua/notifications.json`) | Done, tested (private dbus-daemon) |
| `internal/system` | The control center's system state and actions: network, Bluetooth, battery, power profile, media, volume, brightness, mako's Do Not Disturb | Done, tested (fake services on a private dbus-daemon) |
| `internal/dbustest` | A private dbus-daemon for tests | Done |
| `internal/layershell` | purego binding of gtk4-layer-shell and the `LD_PRELOAD` re-exec | Done |
| `internal/gtkx` | Wrappers for puregotk's ownership, callbacks, main thread and `.ui` loading | Done |
| `internal/shell` | Shell state: the launcher (modes, ranking, selection, focus rules for cancel and execute), each monitor's workspaces and title, the dock (apps and windows, order, click target), screenshot geometry (selections, window stacking, pixels), status icons and text, merging slider values, relative times | Done, tested |
| `internal/config` | `~/.config/inua/config.toml` (TOML): defaults, validation (bad values fall back with a warning), strftime clocks, the commented writer | Done, tested |
| `internal/bootstrap` | `inua init`: writes the embedded samples without overwriting (`-force` keeps a `.bak`); checks that Hyprland starts the shell and loads `inua.conf` (or requires `inua.lua`) | Done, tested |
| `internal/ui` | GTK4 views: bar and dock (per monitor), wallpaper, launcher, control center, notification banners and center, tray, settings window (with shortcut recording), screenshot tool, lock screen (on the session lock protocol; the screen stays locked if the shell stops, and a restarted shell takes the lock back), the Clock app's alarms, snapshots | Done |
| `internal/vm`, `internal/vmui`, `cmd/inua-vm`, `ui/vm.blp` | Virtual machines (docs/inua-vm.md): the toolkit-free machines and their `machine.toml`, the store (one folder per machine), the templates, the Linux platform checks with their remedies, the resource profile and the backends (Docker/Dockur today; QEMU and Waydroid following); the manager window as an app of its own — the list and the chosen machine's page — and its thin command line (`--list`, `--doctor [--json]`, `--status [--json]`, `--start NAME`, `--stop NAME`) | Done, tested (an injected runner and secret store; a fake platform root) |
| `ui/` | Blueprint layouts, generated `.ui`, `style.css`, colour themes, Phosphor icons; embedded as `ui.FS` | Done |
| `internal/upgrade` | `inua upgrade`: the newest release's tag (from where `releases/latest` redirects), its archive checked against `checksums.txt`, the programs, launchers and icons staged next to their targets and then renamed into place | Done, tested |
| `cmd/inua-shell` | The shell | Done |
| `cmd/inua` | Command line: `call`, `show`, `hypr`, `config check|path|hyprland|apply`, `init`, `upgrade` | Done |
| `config/` | Samples (`hypr/hyprland.conf`, `hypr/inua.conf`, `inua/config.toml`, `inua/style.css`), the login session and greetd template, Fcitx themes ([README](../config/README.md)) | Done; greetd + tuigreet recommended, GDM and SDDM supported |
| `scripts/` | `blueprint.sh` (`.blp` → `.ui`), `spike.sh` (nested Hyprland, stress), `phosphor-icons.sh`, `fcitx-themes.py`, the session scripts and their tests | Done |
| `spike/gtk4-layershell` | Notes from the toolkit experiment; its code became the packages above | Record |

## 3. Markup and code

| In markup | In Go |
| --- | --- |
| Widget trees, named widgets (`id`), spacing, sizes, alignment, static text | Dynamic content: titles, the clock, results |
| Style classes (`styles [...]`); the look is in `style.css` | Classes that follow state (`selected`, `focused`, `on`) |
| A template for repeated items (`row.blp`, `note.blp`, `group.blp`) | How many items and what they show (one template instance each) |
| — | Layer-shell setup, events, keyboard, focus |

- `.blp` files are the source; `scripts/blueprint.sh` (`go generate`)
  writes the `.ui` files, which are committed. The program embeds the `.ui`
  files and `style.css`.
- In development, `-ui-dir <dir>` reads them from disk: the launcher reloads
  its layout on every open, so a layout edit needs no rebuild or restart.
- Code fetches widgets by `id`; a missing id panics right away, so markup and
  code cannot drift apart unnoticed.

## 4. Rules for puregotk (`internal/gtkx`)

| Rule | Why | How |
| --- | --- | --- |
| Drop Go's reference once a new widget is in a container | Constructors `ref_sink` widgets and there is no finalizer | `gtkx.Append` |
| Drop references to children you fetched | `GetFirstChild` and friends return new references | `gtkx.Clear` |
| Objects kept from a `.ui` are released with their window | `GetObject` returns a new reference | `Builder.Get` + `Scope.Release` |
| Objects taken from a `.ui` go to a parent | Likewise | `Builder.Take` + `gtkx.Append` |
| Do not release an event controller after adding it | Controllers are not floating; `AddController` takes the only reference | Plain `AddController` |
| Signal handlers are long-lived variables shared by widgets | purego makes one native trampoline per function pointer, at most 2000, never freed | Fields like `s.onKey`; tell widgets apart by name |
| Interface signals go through wrappers | puregotk has no `Connect` for interfaces like GtkEditable | `gtkx.ConnectChanged` |
| Call GTK only on the main thread | GTK is not thread-safe | `gtkx.Post` / `gtkx.Call` |
| Only one purego in the program | Two copies clash at link time | `layershell.Loader` is passed in by the caller |

## 5. Threads

- GTK runs the GLib main loop on the main thread. Shell state is only read
  and written there.
- Services run in goroutines (the Hyprland store, the desktop scan, IPC
  connections, D-Bus services, the clock) and hand results to the main
  thread with `gtkx.Post`; IPC calls that need an answer use `gtkx.Call`
  (with a timeout).
- `gtkx.Post` drains its queue from one fixed idle callback rather than
  creating one per post.
- Slow work (dispatcher requests, launching apps, writing `inua.conf`) moves
  to a goroutine; results come back as Hyprland events.

The assistant is a separate process, started with `--background` while
enabled. Its hidden window keeps the application alive; opening or closing
the window does not start or stop the agent service. The optional Runtime
API uses Mister Morph's `ServeRuntimeAPI` with inua's wrapped tool registry.
Each local question carries its UI callbacks in its context, so remote
tasks cannot write into a local answer. Remote topics and local conversation
history are separate. Both paths use the desktop's approval cards; endpoint
cancellation releases pending approvals. Reloading configuration waits for
the old remote tasks in a goroutine before replacing the runtime.

## 6. Layer shell

gtk4-layer-shell must load before libwayland-client, but puregotk loads GTK
during package initialization. `layershell.EnsurePreloaded`, first thing in
`main`, re-executes the process once with the library in `LD_PRELOAD`, then
restores the original `LD_PRELOAD` so launched apps do not inherit it.

Creating a window: take the `ApplicationWindow` from its `.ui` →
`SetApplication` → `gtk_layer_init_for_window` and settings → `Present`.

| Surface | Layer | Anchors | Keyboard | Namespace | Lifetime |
| --- | --- | --- | --- | --- | --- |
| Wallpaper | Background | All edges, exclusive zone -1 | None | `inua-background` | One per monitor |
| Bar | Top | Top, left, right; automatic exclusive zone | None | `inua-bar` | One per monitor; hidden when `bar.enable` is off |
| Dock | Top | Selected edge; automatic exclusive zone, or 0 with autohide | None | `inua-dock` | One per monitor while `dock.enable` |
| Launcher | Overlay | None (centred) | Exclusive | `inua-launcher` | Built on every open, destroyed on close |
| Control center | Overlay | All edges (full screen, zone -1) | Exclusive | `inua-control` | As the launcher; closes on a backdrop click or Esc; the card slides in a GtkRevealer |
| Notification center | Overlay | All edges | Exclusive | `inua-center` | As the control center |
| Banners | Overlay | Top, right | None | `inua-banners` | One window, shown while banners are up |

The launcher rebuilds its window on every open because, in a nested
Hyprland, GTK's GPU renderer hung the fourth time the same layer surface was
shown again ([toolkit.md](toolkit.md)).

Several monitors: GTK4's `GdkMonitor.GetConnector()` returns the same names
as Hyprland (`eDP-1`), so the two map directly. Each monitor gets a bar,
wallpaper and dock, added and removed with GDK's monitor list (a new monitor
has no name until `notify::connector`). Overlays open on the monitor
Hyprland reports focused.

## 7. Keeping up with Hyprland

`internal/hypr` (toolkit-free):

1. Connect to the event socket first, then query the initial state; changes
   during the query arrive as events and trigger another refresh.
2. Only state events refresh; events within 15 ms merge into one query.
3. After a disconnect, reconnect with exponential back-off and query the
   whole state again.
4. `urgent` comes from events; window addresses are lower-case `0x...`,
   valid only in this session.
5. The events that follow are the result of an action.

`configreloaded` events also refresh what the settings window shows as in
effect (keyboard options, shortcuts).

## 8. Focus rules

| Case | Behaviour |
| --- | --- |
| Open | Remember the active window; show the launcher with exclusive keyboard, focus in the entry |
| Cancel (Esc, or the shortcut again) | Close; `focuswindow address:` back to the previous window if it still exists |
| Pick a window | Close; `focuswindow` to it |
| Launch an app | Close; Hyprland places the new window by its rules; focus is not pulled back |
| Previous window gone | Leave Hyprland's focus alone |

When an action fails (the app does not start, the window has closed) the
launcher stays open, says why under the results and refreshes the list; the
message goes once you type. Empty results say why (no match, apps not read
yet, no windows, not connected to Hyprland).

The dock focuses windows without moving the pointer: `focuswindow` is
batched with `cursor:no_warps` set, unless the user set it already.

Control from outside: Hyprland shortcuts run `inua call …` (`internal/ipc`,
one socket per instance, named by `-n`, default `$INUA_INSTANCE` or
`default`). `inua show` lists the targets:

`launcher toggle|apps|windows|close`, `overview toggle|open|close`, `control toggle|open|close`,
`settings toggle|open [PAGE]|close`,
`notifications toggle|open|close|clear|focus [on|off]`,
`shell state|close|reload|restart|snapshot DIR|tray-menu N`.

`shell restart` re-executes the shell in place (same PID, arguments,
environment and log; a rebuilt binary runs the new version).

The shell is its own binary, `cmd/inua-shell`, rather than `inua run`:
puregotk loads GTK during package initialization (about 50 ms, 130 ms cold),
which would slow every `inua call` from a shortcut; apart, `inua call` takes
about 3 ms.

## 9. Testing

| Layer | How |
| --- | --- |
| Services | Unit tests, with a fake Hyprland socket and fake D-Bus services on private buses (`make test`) |
| Shell state | Unit tests: ranking, modes, focus rules, failures, the dock model |
| Settings and generated files | Unit tests: defaults equal the sample, round trips, validation; `inua.conf` checked with `Hyprland --verify-config` when installed |
| Layer-shell integration | `scripts/spike.sh shell nested` + `stress`: layers, reserved space, focus, repeated opening, memory, GTK warnings |
| Real session | By hand: Chinese input, fullscreen, several monitors, long runs |

Logic gets its tests first; purely visual changes (spacing in `.blp`,
`style.css`) get none. `inua call shell snapshot DIR` renders the shell's
windows to PNG files for checking layouts without a screenshot tool.

## 10. Tray and system state

- Tray: the shell provides `org.kde.StatusNotifierWatcher` on the session
  bus; if someone else holds the name it queues and takes over when they
  leave. The host does not depend on its own watcher, so it still reads the
  items when a watcher such as GNOME's refuses a second host. Watchers format
  items differently (`service/path`, `service@/path`, a bare service); all
  are parsed. Menus are read before showing (`AboutToShow` + `GetLayout`);
  `LayoutUpdated` is not followed.
- System state: one connection per D-Bus service; read the whole state,
  read it again on that service's signals (merged within 40 ms), report only
  changes; reconnect and reread after a restart or disconnect. Volume uses
  `pw-dump -m` as the change signal and `wpctl` to read and write (as the
  volume keys do; only node/device/metadata are followed, or wpctl's own
  clients coming and going would loop). Brightness is read from sysfs and
  set through logind's `SetBrightness` (no root), on brightnessctl's `-e4`
  perceptual curve; while the control center is open it is read every
  500 ms to follow the brightness keys.
- Sliders: for 600 ms after the user drags one, service updates do not move
  it; only the latest value is applied, so a drag does not queue calls.

## 11. Notifications

- The shell serves `org.freedesktop.Notifications` (`internal/notify`):
  actions, body markup (b, i, u, a; sanitised and balanced), `image-data`,
  `replaces_id`, urgency, transient and resident hints, and the
  `NotificationClosed`, `ActionInvoked` and `ActivationToken` signals. It
  queues behind another daemon and takes over when that one exits.
- Banners show for `notifications.banner_seconds` (hovering holds them;
  critical ones stay), then the notification waits in the center, grouped by
  app. Clicking runs the default action with an xdg-activation token and
  removes it unless resident. Focus quiets banners; the center still
  collects. The center is saved across restarts.

- The shell is also the session's polkit agent (`internal/polkit`): a
  program asking for rights that take a password (pkexec, a system setting)
  gets a dialog over the focused screen with polkit's message, who answers
  (another user when several may) and the helper's prompts and messages
  (a fingerprint reader's too). The password goes to polkit's helper, which
  checks it and tells polkitd; the shell keeps nothing. It registers for
  the session logind puts the shell in; an agent already registered there
  keeps the job.
- gpg asks for passphrases in the same card: `inua-pinentry` (set as
  `pinentry-program` in `~/.gnupg/gpg-agent.conf`) speaks the pinentry
  protocol (`internal/pinentry`) and asks the shell over its control
  socket (`call password getpin|confirm|message`); with no shell running
  it runs the usual pinentry instead.

## 12. Settings

- File: `$XDG_CONFIG_HOME/inua/config.toml` (`inua-shell -config` changes
  it), read at start, on `shell reload` and on `shell restart`; not watched.
  The sample with every option is
  [config/inua/config.toml](../config/inua/config.toml); a test keeps it
  equal to the defaults.
- Errors never stop the shell: an unparsable file means all defaults;
  unknown keys and bad values warn one by one, and bad values fall back. The
  warnings go to the log, and `inua config check` checks without loading GTK.
- Sections: `[bar]` (shown, clock in a strftime subset, workspace scope,
  title, tray), `[launcher]`, `[control]` (lock and screenshot commands),
  `[style]` (theme, icon set, extra stylesheet), `[background]`, `[input]`
  (XKB options, touchpad), `[notifications]`, `[dock]` and `[[shortcuts]]`.
- `[input]` and `[[shortcuts]]` are Hyprland settings: written to
  `hypr/inua.conf`, which `hyprland.conf` sources last, so they hold without
  the shell. Hyprland reloads by itself when the file changes.
- Styles: `style.css` (relative to the config directory) loads after the
  built-in style at USER priority; overriding a colour with `@define-color`
  changes every built-in rule that uses it.
- Changes apply while running: `applyConfig` compares old and new settings
  and updates only what changed (themes and stylesheet, wallpaper, bar,
  dock, tray, clock, icons, notifications).
- The settings window (`inua call settings open|close|toggle`, the control
  center's gear, Super+Comma) is a normal window, floated by the sample
  `hyprland.conf`: two columns, the sections on the left, the page on the
  right. Each change applies at once and is saved 0.4 s after the last one;
  invalid input (half a colour, a missing image) is marked and not applied.
  Saving writes a complete commented file with `config.Format`; the sample
  `config.toml` is `Format(Default())`, kept so by a test. Keys the shell
  does not know are not kept.

## 13. Look

- Restraint and space: few colours, one accent per surface, used only for
  current, selected and on states; hairline borders; 2–4 px corners; no
  glows. Readouts, labels and mode names are monospace (small capitals
  tracked wide; mode names in brackets like `[APPS]`); body text is sans
  serif. States are 7 px squares, filled when on and hollow when off; the
  bar's workspaces are such squares too, the focused one stretched into a
  bar.
- Colours are tokens only: `ui/themes/<name>.css` defines `app`, `surface`,
  `t0`–`t2`, `line`, `accent`, `danger` and the rest, and `ui/style.css`
  uses the names. Seven themes: `yoru` (the default, dark), `washi` and
  `aojashin` (light), `suimen`, `rinko`, `bunko` and `denko` (dark),
  chosen with `style.theme`; a test keeps the names and the files in step.
  A user's theme, `themes/<name>.css` beside `config.toml`, is loaded over
  `yoru` (or over the built-in theme of its name), so it needs only the
  tokens it changes (`config.ThemeCSS`).
- Icons: Phosphor's regular icons (`ui/icons`, MIT), written as an icon
  theme for the shell's process that inherits the system's; app and tray
  icons stay the system's. `style.icons = "system"` uses the system theme
  throughout. A user's set, `icons/<name>/` beside `config.toml`, is
  copied over the Phosphor icons into an icon theme of its own name
  (`shell.IconTheme`), in the shell and in inua's apps.
- Text size: `style.text_scale` (80 to 150 percent, the Appearance page's
  "Text size") multiplies the `font-size` pixel values of `ui/style.css` as
  it loads (`shell.ScaleText`), in the shell and in inua's apps; icons,
  spacing and the user's `style.css` keep their sizes.
- Launcher: a head with the mark, the mode and the switch hint; the entry
  has only a bottom hairline; the selected row is the accent at 9% with a
  38% hairline; notices (why the list is empty, or `[ERROR]` and the reason)
  follow the list; key hints at the bottom. Its height is fixed: a notice
  takes a result row, so the window never resizes.
- Control center: four tiles with status marks; display and sound sections
  of "label + hairline + percentage"; hairline sliders with small square
  knobs; mute left of the volume slider; screenshot, lock and settings in
  the head.

## 14. Wallpaper

The shell draws the wallpaper: a background layer surface per monitor
(`inua-background`, exclusive zone -1), added and removed with the bar. Its
look is a CSS background: an image placed by `fit` (cover, contain, tile,
center) over `color` or the theme's ground; without an image, the ground with
a fine texture (a 3 × 1.5 px run every 6 px, rows every 26 px), drawn in the
theme's `field` colour at 4× and written to the runtime directory.
`background.enable = false` creates none of these and leaves the background
to another program.
