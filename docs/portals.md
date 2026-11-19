# Portals: inua as the desktop's backend

Apps ask the desktop for things through `xdg-desktop-portal`: a file to
open, a screen to share, whether the theme is dark, a place to keep a
password. The portal checks the app's sandbox and hands each request to a
backend. Without inua's, an inua session has:

| Backend | Package | Interfaces |
| --- | --- | --- |
| `hyprland` | `xdg-desktop-portal-hyprland` | Screenshot, ScreenCast, GlobalShortcuts |
| `gtk` | `xdg-desktop-portal-gtk` | FileChooser, AppChooser, Print, Notification, Inhibit, Access, Account, Email, DynamicLauncher, Lockdown, Settings |
| `gnome-keyring` | `gnome-keyring` | Secret |

They work, but they do not look like inua, they read GNOME's settings
(gsettings) instead of inua's, and Secret goes away with gnome-keyring
(`docs/removing-gnome.md`). Background and Wallpaper have no backend at
all: apps that ask get an error.

inua serves the interfaces where it has the piece behind them and the
user sees the difference, and leaves the rest to the backends above. Each
is turned on by itself, and off again: `inua portals`.

## How a backend is chosen

```
app ──► xdg-desktop-portal ──► $XDG_DESKTOP_PORTAL_DIR ──► inua / inua-files / inua-keyring
        (org.freedesktop.portal.*)   hyprland-portals.conf      else hyprland, gtk
```

Each backend is a `.portal` file naming its bus name and interfaces, and
the session's config (`hyprland-portals.conf` for a Hyprland session)
picks a backend for each interface. The frontend (1.21, Ubuntu 26.04's)
reads both from one directory: `/usr/share/xdg-desktop-portal`, or
`XDG_DESKTOP_PORTAL_DIR` when that is set. It reads no backends from the
user's directories, and with `XDG_DESKTOP_PORTAL_DIR` set it reads the
config from there too, not from `~/.config/xdg-desktop-portal`.

So inua keeps a directory of its own, `~/.local/share/inua/portals`:

- its backends' `.portal` files;
- links to the system's `.portal` files and to the configs of other
  desktops (the user's in `~/.config/xdg-desktop-portal` and `/etc`'s
  over the system's);
- `hyprland-portals.conf`: the user's (`~/.config/xdg-desktop-portal`),
  else `/etc`'s, else the system's, with a line for each interface inua
  serves, naming inua's backend first and the one the config named
  before after it:

  ```ini
  [preferred]
  default=hyprland;gtk
  org.freedesktop.impl.portal.Settings=inua;hyprland;gtk
  org.freedesktop.impl.portal.FileChooser=inua-files;hyprland;gtk
  org.freedesktop.impl.portal.Secret=inua-keyring;hyprland;gtk
  ```

`inua-session` gives the frontend the directory
(`systemctl --user set-environment XDG_DESKTOP_PORTAL_DIR=…`) when an
interface is on, and takes it away when none is; then it restarts the
frontend if a session before left it running. A plain Hyprland session
keeps the system's backends.

inua has three processes that serve, so three backends; one bus name
belongs to one process:

| Backend (`.portal`) | Bus name | Process | Interfaces |
| --- | --- | --- | --- |
| `inua` | `org.freedesktop.impl.portal.desktop.inua` | `inua-shell` | Settings, Notification, Inhibit, Screenshot, Access, Background, Wallpaper |
| `inua-files` | `org.freedesktop.impl.portal.desktop.inua.files` | `inua-files` | FileChooser, AppChooser |
| `inua-keyring` | `org.freedesktop.impl.portal.desktop.inua.keyring` | `inua-keyring` | Secret |

The processes serve whether or not the frontend asks them; the config
decides. D-Bus starts `inua-files -portal` and `inua-keyring` for a
request (service files in `~/.local/share/dbus-1/services`); the shell
is the session's.

How the frontend treats them, as seen on a private bus:

- It picks an interface's backend when it starts, by which `.portal`
  files there are, and calls it at each request. A backend that takes
  its name after the frontend started works.
- Settings is asked of every backend the config lists, key by key: inua's
  keys win, and the next backend's fill in the keys inua has none for.
- Any other interface goes to its first backend alone. While that
  process is not running (and D-Bus cannot start it), requests are lost
  without an error. This is why the directory is for inua's sessions
  only.

### Who writes it

`inua portals`, as `inua keyring` does for the Secret Service:

- `inua portals status`: each backend, whether its process runs, and
  each interface on or off;
- `inua portals enable [INTERFACE …]` (all of them without one): the
  directory, the service files, then in an inua session the frontend's
  environment and a restart of the frontend, which reads its backends
  only when it starts;
- `inua portals disable [INTERFACE …]`: the lines out; with none left,
  the directory and the service files go, and the frontend reads the
  system's again;
- `inua portals dir`: for `inua-session`, the directory written again
  (the system's backends as they are now) and its path, or exit status 1
  with none on.

`SharePicker` is one more name it takes: xdg-desktop-portal-hyprland's
picker (ScreenCast, below). `inua-files -picker` turns FileChooser and
AppChooser on (`-picker=false` off). The code is `internal/portalconf`,
without GTK.

Settings, Default Apps, [DIALOGS] chooses a row at a time: open and
save (FileChooser, AppChooser), the share picker, the shell's
interfaces, and Secret, each with a drop-down of the backends that serve
its first interface: inua's, then the system's `.portal` files that list
it (GTK, GNOME, GNOME Keyring…). A system backend goes first in the
config's line for each of the row's interfaces it serves, and the others
go back to the session's config; one the session's config gives already
writes no line. Under each row is the backend apps get now, worked out
as the frontend picks one (the config's list, then `UseIn`, then GTK's)
from the running frontend's environment (`/proc/PID/environ`):
`XDG_DESKTOP_PORTAL_DIR` and `XDG_CURRENT_DESKTOP`. While that differs
from what inua's config gives, the line is in `warn` and a notice over
the rows offers Apply now: the directory written again, the frontend's
environment set and the frontend restarted, as an inua session does. A
session started by an older `inua-session`, which did not give the
frontend the directory, is the usual case. `inua portals status` says
the same.

`scripts/install.sh` turns them on once, after asking (yes without a
terminal): all of them, Secret only when `inua keyring replace` made
inua-keyring the keyring, else that interface's requests would go
unanswered. A later install, `inua upgrade` too, keeps what was turned
off since: with any interface on it leaves them.

### The display

D-Bus and the user's systemd start inua-files, inua-keyring and
xdg-desktop-portal-hyprland with their activation environment. Hyprland
writes its `WAYLAND_DISPLAY` and `HYPRLAND_INSTANCE_SIGNATURE` there as
it starts, a nested one too, and what they started after it then opened
on a display gone with the nested window ("Failed to open display").
The session's shell (instance `default`) writes its own back as it
starts (`portalconf.ActivationDisplay`), and `scripts/spike.sh nested`
writes the outer session's back once its Hyprland is up.

## Serving a request

Every method that waits for the user takes a `handle` and exports
`org.freedesktop.impl.portal.Request` at it while the request is open.
`Close` on it closes what the request showed and answers 2. The answers
are 0 with a result, 1 when the user cancelled and 2 when closed or
failed. godbus runs each call on a goroutine of its own, so a method
waits for its window there, and the window lives on the main thread
(`gtkx.Post`/`gtkx.Call`).

The frontend has already checked the sandbox and the permission store
when the backend is called; for Flatpaks it maps the paths we return to
document-portal paths. The backend works with plain paths and app IDs,
and remembers nothing the frontend keeps.

A window for another app is placed over that app's window: the request's
`parent_window` is `wayland:HANDLE`, which GDK takes
(`gdk_wayland_toplevel_set_transient_for_exported`, `gtkx.SetForeignParent`),
and Hyprland floats it as the app's dialog. It also gets the class
`org.freedesktop.impl.portal.desktop.inua.files`, which inua's window
rule for the portal's dialogs floats and centres when there is no parent.

`internal/portal` holds what the backends share (the `Request` object,
the answers, options from `a{sv}`, exporting interfaces with their
`version` property and introspection) and each interface without GTK,
handing what needs the user to a handler the shell implements.

## The interfaces

### Settings

`ReadAll`, `Read`, and `SettingChanged`, from inua's settings:

| Namespace | Key | From |
| --- | --- | --- |
| `org.freedesktop.appearance` | `color-scheme` | The theme: 1 (dark) when its ground is dark, else 2 (light) |
| | `accent-color` | The theme's accent token, as `(ddd)` |
| | `contrast` | 0, until a high-contrast theme exists |
| `org.gnome.desktop.interface` | `icon-theme` | `style.app_icons`, when it names a theme |
| | `text-scaling-factor` | `style.text_scale` |

GTK, Qt, Firefox, Chromium and Electron read these, so apps follow inua's
theme and change with it. The keys inua has no setting for (fonts, the
cursor, reduced motion, the icon theme when `app_icons` is `system`) come
from the next backend. inua's apps read the desktop's icon theme through
this interface too (`internal/appui/app.go`).

### Notification

`AddNotification`, `RemoveNotification`, `ActionInvoked`. The shell turns
the portal's notification (title, body or markup body, a themed icon or
one sent as bytes or a file descriptor, priority, buttons, default action,
transient) into one of its own, numbered by its notification server, and
gives it to the center as the server's are. The app ID names the app, so
the center has its name and icon. A button sends `ActionInvoked` with its
action and target; a click with no default action brings the app's
window forward, or starts it. While the shell shows no notifications
(`notify.enable` off, another daemon), the portal's are dropped.

### Inhibit

`Inhibit` (logout, switch user, suspend, idle), `CreateMonitor` and
`QueryEndResponse`. Idle goes to the idle daemon's
`org.freedesktop.ScreenSaver.Inhibit` (hypridle's, when it runs; without
one nothing goes idle to hold off). Suspend holds a logind lock, which
stops the idle suspend; the shell's Suspend and Hibernate go past it, the
user having asked (an app that forgets its lock would block them). An app
outside a sandbox comes with no ID; it is named by its program
(`firefox`), from the bus name in the request's handle, in logind's lock
and before Log out. Logout is
held by the shell: Restart, Shut down and Log out, asking for their
second click, name the apps holding it (*"Firefox is busy. Log out?"*).
Monitors hear the screen locked and unlocked, and the session asked to
end (the second click armed), back to running (disarmed), and ending.

### Screenshot

`Screenshot` and `PickColor`, through the shell's screenshot tool.

- Not interactive: the focused screen, written with `grim` to a file in
  the runtime directory, answered with its URI. The frontend asks the
  user once per app.
- Interactive: the tool, as `inua call screenshot area`; what is taken
  (an area, a window, a screen) goes to the app, not to the screenshots
  folder or the clipboard.
- `PickColor`: the tool's colour picker; the colour goes to the app and
  into the Color Picker's history, not to the clipboard.

### ScreenCast: the picker only

Capturing the screen into a PipeWire stream stays with
`xdg-desktop-portal-hyprland`: it follows Hyprland's protocols across
versions (`docs/hyprland-versions.md`). Its picker is replaced:
`inua portals enable SharePicker` writes into
`~/.config/hypr/xdph.conf`

```
screencopy {
    custom_picker_binary = /path/to/inua-share-picker
}
```

(the rest of the file kept; an absolute path, since xdph's PATH may not
have `~/.local/bin`) and restarts xdph, which reads it only when it
starts.

`inua-share-picker` asks the shell (`inua call share pick`, which waits)
to open the screenshot tool for sharing: a screen, a window or an area,
chosen on the screen itself. It prints the choice as xdph reads it:
`[SELECTION]`, the flags (`r` when xdph passed `--allow-token`, which
lets the app share the same again without asking), `/`, then
`screen:OUTPUT`, `window:HANDLE` or `region:OUTPUT@X,Y,W,H` (logical
pixels on the output). A window is named by the handle xdph lists for its
address in `XDPH_WINDOW_SHARING_LIST`
(`HANDLE[HC>]CLASS[HT>]TITLE[HE>]ADDRESS[HA>]`). Cancelled, it prints
nothing, which xdph takes as no.

With no shell to ask (a plain Hyprland session reads the same
`xdph.conf`), it runs xdph's own `hyprland-share-picker` in its place,
with the same arguments.

### FileChooser

`docs/picker.md`.

### AppChooser

`ChooseApplication` and `UpdateChoices`: an app asking which app opens a
file or a link. inua-files shows the apps the frontend offers in a window
of its own over the app, Open With's list: the one chosen last first, the
rest by name, a field to find one, Enter for the first. A new list while
it is open (an app installed meanwhile) replaces the old. *Always use
this app* is not offered: the frontend asks that itself and remembers it.

### Secret

`RetrieveSecret`: the app's own secret, written to the file descriptor
the call passes; a Flatpak app keeps its passwords encrypted with it.
inua-keyring keeps one secret per app ID in the default collection
(attributes `xdg:schema` = `org.freedesktop.portal.Secret`, `app_id`, as
gnome-keyring does, so `inua keyring import` brings an app's along), made
the first time the app asks, 64 random bytes. While the store is locked,
the read waits for gpg-agent's passphrase. Only the frontend may call it:
the owner of `org.freedesktop.portal.Desktop`, which has checked who the
app is; any other process could name any app. Reads go in the keyring's
record of reads.

### Access

`AccessDialog`: the frontend's questions (the camera, the location). The
shell asks in its own dialog, the one gpg's and the keyring's questions
use: the title, the subtitle and body, the app that asks, and the app's
labels for the buttons, else *Allow* and *Deny*; Deny has the focus. The
choices a question may carry are answered with their defaults: the
frontend's questions have none.

### Background

`GetAppState`, `NotifyBackground`, `EnableAutostart` and
`RunningApplicationsChanged`. The apps with a window are Hyprland's
classes (the app ID on Wayland), the focused one active; the frontend
takes a sandboxed app it knows that has none for running in the
background, and asks `NotifyBackground`. The shell asks in a
notification, *"Sync is running in the background"*, with *Allow*
(allowed, and remembered by the frontend) and *Quit* (the frontend stops
it); dismissed, or left, the app is let be this time. `EnableAutostart`
writes the app's entry in `~/.config/autostart`; newer frontends write it
themselves.

### Wallpaper

`SetWallpaperURI`. The picture is copied to
`~/.local/share/inua/backgrounds` (a sandboxed app's file may not stay),
then becomes `background.image`, with the generated picture turned off,
as choosing one in Settings does. With `show-preview` it shows as the
background while the shell asks, *Set as background?*, Cancel focused;
cancelled, the background is as it was. The lock screen shows the
background, so `set-on` is the same for each.

## What stays with other backends

| Interface | Stays with | Why |
| --- | --- | --- |
| ScreenCast (capture) | `hyprland` | Above: inua replaces the picker only |
| GlobalShortcuts | `hyprland` | Hyprland owns the key bindings; inua's shortcuts are Hyprland's |
| Print | `gtk` | CUPS's dialog is a lot to rebuild, and printing is rare |
| Account, Email, DynamicLauncher, Lockdown | `gtk` | Rare, and nothing in inua to answer with that GTK does not have |
| RemoteDesktop, InputCapture | none | Later, on `inua-remote` (`docs/remote-desktop.md`) |
| Clipboard | none | Only RemoteDesktop sessions use it |

## Tests

- `internal/portal`: the `Request` object (closed twice, gone after
  `End`); each interface against a client on a private bus
  (`internal/dbustest`) with a fake handler; Settings' patterns,
  `NotFound` and `SettingChanged` only for what changed; and Settings
  through the real frontend, when it is installed, given a directory as
  inua writes it, inua's keys over a second backend's.
- `internal/portalconf`: the directory over none, over the user's config
  and `/etc`'s, enabling twice, the last interface off, the system's
  backends changing, the service files (and one inua did not write left
  alone), xdph's config (none, the user's with another picker, a section
  without one).
- `internal/keyring`: Secret's same secret twice for one app, another for
  another app, kept as gnome-keyring keeps them, none without an app ID,
  and a caller other than the frontend refused.
- `cmd/inua-share-picker`: the choices in xdph's format, windows by
  their handle, a window xdph did not list.
- `scripts/test-session.py`: `inua-session` giving the frontend the
  directory, and taking it away, on a fake `systemctl`.
- By hand, on GTK's Broadway backend with a fake HOME and a private bus:
  the shell's Settings, a notification and its button, an access
  question (Enter denies), a background question (Allow answers 1), a
  wallpaper with its preview; for the picker, `docs/picker.md`.
- Not yet run: in a real inua session, with Firefox, Chromium and a
  Flatpak; the screenshot and share picker paths, which need Hyprland and
  `grim`.

## Open questions

- inua does not start the entries in `~/.config/autostart` (neither does
  Hyprland), so an app's autostart through the portal is written and not
  run. The shell could start those the portal wrote (`X-Flatpak`), once
  a session.
- The control center does not list the apps running in the background
  yet, to quit one later than its question.
- Firefox outside Flatpak uses the portal's file chooser only with
  `widget.use-xdg-desktop-portal.file-picker = 1`.
