# Setting up inua

The sample configuration inua needs on a new system:

| File | Installed to | What |
| --- | --- | --- |
| `hypr/hyprland.conf` | `~/.config/hypr/hyprland.conf` | Hyprland 0.53: starts `inua-shell`; the shell's shortcuts, blur rules for its surfaces, common window management keys and media keys. Companions (hypridle, Fcitx 5) start only when installed. inua shows notifications, draws the wallpaper and asks for administrator passwords (as the Polkit agent) itself |
| `hypr/inua.conf` | `~/.config/hypr/inua.conf` | The Hyprland settings inua manages (keyboard, touchpad, shortcuts, the session lock restore setting, the light apps' window rule, blur behind the apps' menus), written by inua from `config.toml`. The sample has no settings of yours; `hyprland.conf` sources it last |
| `inua/config.toml` | `~/.config/inua/config.toml` | The shell's settings: every option at its default, documented |
| `inua/style.css` | `~/.config/inua/style.css` | Style overrides: the built-in colours and examples, all commented out |
| `inua/themes/nord.css` | `~/.config/inua/themes/nord.css` | A sample theme of your own, the Nord palette (MIT): every colour token, each with what it paints. Not in use until chosen |
| `inua/icons/phosphor-bold/` | `~/.config/inua/icons/phosphor-bold/` | A sample icon set of your own: Phosphor's bold weight (MIT), every icon the shell draws. Not in use until chosen |
| `apps/` | `~/.local/share/applications/`, `~/.local/share/icons/hicolor/scalable/apps/` (`make install`) | Desktop entries and icons of the light apps (Clock, Weather, Stocks, Calculator, Calendar, Color Picker, System Monitor, Tailscale, Viewer, Windows VM, Remote Desktop, Disk Usage, Mail; see [docs/apps.md](../docs/apps.md)). The icons' glyphs are Phosphor's (MIT), except the Windows guest's own mark in [inua-vm.md](../docs/inua-vm.md) |
| `firefox/policies.json` | `/etc/firefox/policies/policies.json` (root; merge with any policy already there) | Makes Firefox publish its menu through the AppMenu registrar on Wayland (`widget.gtk.global-menu.enabled`), which is what puts its menus in the bar. Optional: without it Firefox shows no menu in the bar, and every other application is unaffected |
| `fcitx5/themes/` | `~/.local/share/fcitx5/themes/` (`make install-fcitx`) | Fcitx 5 themes in inua's style |

```sh
make deps deps-desktop   # packages (Ubuntu 26.04; what each is for: docs/design.md, "Packages")
make install-session     # build, install, write the sample config, install the login session; DM=greetd by default
```

Run `make install-session` as your user; it calls sudo only to write the
system session entry and the greetd config. Programs go to `~/.local/bin`,
your configuration to `${XDG_CONFIG_HOME:-~/.config}`. The session script
puts `~/.local/bin` and `/usr/local/bin` on PATH. With a custom `PREFIX`,
make sure the login session's PATH has its `bin`; with a custom
`CONFIG_HOME`, point the login session's `XDG_CONFIG_HOME` at it.

To install only the programs and your configuration: `make install
install-config`, or run `inua init` after installing.

`inua init` carries the samples inside the binary; it needs no repository.
Existing files that differ are left alone (and listed); `inua init -force`
replaces them, keeping the old ones as `.bak`; the sample `hyprland.conf`
is not written beside a `hyprland.lua`, the config Hyprland reads. It sets gpg-agent's
`pinentry-program` to `inua-pinentry` in `~/.gnupg/gpg-agent.conf` (or
`$GNUPGHOME`), keeping the rest of the file and another pinentry already
set (unless `-force`), so gpg asks for passphrases in inua's dialog; with
no shell running, `inua-pinentry` runs the usual pinentry. With
Omarchy's Lua config (a `hyprland.lua` that requires
`default.hypr.omarchy`), whose defaults start Omarchy's shell (Quickshell)
with `omarchy-launch-shell`, it asks (default yes; `-force` answers yes)
whether to start `inua-shell` in its place. Yes keeps the file as
`hyprland.lua.bak`, adds a block before that require that turns Omarchy's
`hl.exec_cmd("omarchy-launch-shell")` into `inua-shell`, and loads
`inua.lua` last if nothing does, so inua's shortcuts replace Omarchy's. It
applies to Omarchy's session too, as both read the file; Omarchy's menus
and the keys that talk to its shell (Super+Space, Super+comma …) do
nothing without it, and `omarchy-restart-shell` would start it again.
Delete the block and that line to have Omarchy's shell back. It then checks `config.toml`,
checks `hyprland.conf` with `Hyprland --verify-config` when Hyprland is
installed, and says whether `inua-shell` is on `PATH`.

## Login manager and session

inua recommends **greetd + tuigreet**: greetd authenticates and starts the
session, tuigreet is the terminal login screen with a session menu. GDM and
SDDM work too. All three share `/usr/share/wayland-sessions/inua.desktop`
and `/usr/local/bin/inua-session`; the Inua session runs `start-hyprland`,
which reads your default Hyprland config (no `--config`).

| Login manager | Install | Choosing the session |
| --- | --- | --- |
| greetd + tuigreet (recommended) | `make deps-dm`, then `make install-session` | Starts Inua by default; F3 opens the session menu, where Hyprland is also listed |
| GDM | `make install-session DM=gdm` | After choosing your user, pick Inua from the gear menu |
| SDDM | `make install-session DM=sddm` | Pick Inua from the session menu |

The install script (`curl -fsSL https://inua.one/install.sh | sh`)
finds the login manager systemd starts. With greetd it installs the greetd
config; with another it only registers the Inua session, after asking
whether to switch to greetd + tuigreet (default no; yes installs both,
disables the current one and enables greetd for the next boot). With no
login manager it offers greetd + tuigreet, default yes. When the current
one logs you in without a password (as Omarchy's SDDM does), it says to set
its autologin session to `inua.desktop` to be logged in to Inua instead.

The GDM and SDDM commands are for systems that already have that login
manager; they only register the Inua session. `make deps-dm` installs Ubuntu
26.04's `greetd` and `tuigreet` packages, which may ask which login manager
should be the default.

The greetd mode also installs `session/greetd.toml`, with the greeter
account of the greetd package installed (`_greetd` on Debian and Ubuntu,
`greeter` on Arch, `greetd` on Fedora) and the VT of the config it
replaces, else tty7. It reads sessions from
`--sessions /usr/share/wayland-sessions`, runs `inua-session` by default and
does not log in automatically.

With `DM=greetd` the script installs the tuigreet config as
`/etc/greetd/config.toml`. An existing file that differs is first kept as a
numbered backup (`config.toml.~1~`, `config.toml.~2~`, …), then replaced. If
it is already the same, a reinstall changes nothing and adds no backup.

The script never starts, stops or switches login managers. To move from
Ubuntu's GDM to greetd once it is set up:

```sh
sudo systemctl disable gdm3.service
sudo systemctl enable --force greetd.service
sudo reboot
```

From SDDM, use `sddm.service` in the first command. Without `--now`, the
current desktop session lasts until the reboot. Other distributions use
their own service names.

Keeping GDM or SDDM, log out after installing and choose Inua. Without a
login manager, log in on a TTY and run `/usr/local/bin/inua-session`.

The Hyprland and Inua sessions both use
`${XDG_CONFIG_HOME:-$HOME/.config}/hypr/hyprland.conf` (the samples are for
Hyprland 0.53). `inua init` writes the sample only when there is no config;
an existing one is kept. The sample has inua's startup line and shortcuts.

With an existing Hyprland config, add the startup line (once), and merge the
sample's shortcuts and layer rules as you like:

```ini
exec-once = inua-shell
```

`inua init` and `inua config hyprland` check whether the config (and the
files it sources) starts `inua-shell`, and print the line to add if not. The
Inua session refuses to start when the config exists but lacks it, and says
why (the Hyprland session is unaffected), rather than giving you a session
without the shell.

Both sessions share this config and so both run its startup lines; inua no
longer creates or reads a Hyprland config of its own.

### Hyprland settings inua manages

The settings window's Keyboard, Mouse & Touch and Shortcuts pages change Hyprland
settings. They are saved in `config.toml` like everything else (`[input]`,
`[[shortcuts]]`), and inua writes them to `~/.config/hypr/inua.conf`, which
`hyprland.conf` reads. So they apply when Hyprland starts and survive
reloads, with or without `inua-shell` running. Settings left unset
(commented out or empty) are not written and stay with `hyprland.conf`; "Use
hyprland.conf" on a page stops inua managing that group.

Add this as the **last** line of an existing Hyprland config. Hyprland
applies settings in order, so `kb_options`, `sensitivity` or `accel_profile`
after it would override inua's; `inua config hyprland` checks the line is
there and not overridden, and the settings window says so too:

```ini
source = ./inua.conf
```

With a Lua config (`hyprland.lua`, Hyprland 0.55 and later) inua writes
`~/.config/hypr/inua.lua` instead, and the last line is the one below; the
inua-shell line is
`hl.on("hyprland.start", function() hl.exec_cmd("inua-shell") end)`.
inua writes `inua.lua` when Hyprland runs Lua and `hyprland.lua` is there,
or when there is no `hyprland.conf`.

```lua
pcall(require, "inua")
```

Hyprland rejects a `source` of a missing file, so `inua init` writes an empty
`inua.conf`, and `inua-shell` creates it when it starts. When `inua.conf`
changes, Hyprland reloads by itself (inua asks it to when
`misc:disable_autoreload` is on). After editing `config.toml` by hand, run
`inua config apply` or `inua call shell reload`. Touchpad settings are
written per device (Hyprland has no touchpad-wide speed): for the touchpads
Hyprland sees now and those already in `inua.conf` (a Bluetooth one that is
away, say).

To package or inspect the installation, write the system files to a staging
directory, without sudo:

```sh
sh scripts/install-session.sh greetd ./session-root
```

That installs only the session script, the `.desktop` file and the greetd
config; it builds nothing and writes no user configuration. Use `gdm` or
`sddm` as the first argument to see theirs.

References: [tuigreet options](https://manpages.ubuntu.com/manpages/stonking/man1/tuigreet.1.html),
[greetd config](https://manpages.debian.org/testing/greetd/greetd.5.en.html),
[Debian's default greetd config](https://sources.debian.org/src/greetd/0.10.3-4/config.toml/).

## Logs

Started by Hyprland, whose programs write their standard error to
`/dev/null`, `inua-shell` writes its log, a crash's trace and GTK's warnings
to `~/.local/state/inua/shell.log` (`$XDG_STATE_HOME`); the terminal's go to
`term.log` and `term-host.log` there. A log over 1 MB is kept as `NAME.1`
and begun again. Started from a terminal or with its standard error
redirected, the shell writes there instead.

## Shortcuts

The sample's shortcuts (`$mod` is Super in `hyprland.conf`):

| Keys | Action |
| --- | --- |
| Super+Space | Launcher (apps; the mode is `launcher.mode`) |
| Super+Tab | Launcher (windows, most recent first; Enter switches to the previous one) |
| Super+A | Control center |
| Super+N | Notification center (also the bell in the bar) |
| Super+Comma | inua's settings |
| Super+Shift+R | Restart the shell (after editing `config.toml` or `style.css`, or rebuilding) |
| Super+L | Lock the screen |

### The Shortcuts page

The settings window's Shortcuts page manages shortcuts: inua's own actions
(launcher, window switching, control center, notification center, Focus,
settings, restarting the shell, locking), four screenshots (an area or the
screen, to the clipboard or to `~/Pictures/Screenshots`) and your own
commands, followed by the other shortcuts in `hyprland.conf` (read-only).

Click Change and press the new keys (Esc cancels, Backspace turns the
shortcut off). While recording, Hyprland switches to a submap with no
shortcuts (`inua-record`), so even keys that are already bound reach the
window. It gives up after 15 seconds without a key; Super+Ctrl+Alt+Escape is
a way out should it get stuck.

Shortcuts set there are saved as `[[shortcuts]]` in `config.toml` and written
to `inua.conf`: first `unbind` the keys from whatever they did, and the keys
the action had in `hyprland.conf` (`replaces`), then `bind`, so nothing fires
twice. Actions you never change keep their `hyprland.conf` shortcuts; ↺
hands an action back to `hyprland.conf`.

## Settings

Changes in the settings window apply at once and are saved to `config.toml`.
Its pages: Appearance, Wallpaper, Bar, Dock, Launcher, Control center,
Notifications, Shortcuts, Keyboard and Touchpad; `inua call settings open
keyboard` opens one directly. After editing `config.toml` by hand, `inua call
shell reload` applies it without a restart. A broken file never stops the
shell: an unparsable file means all defaults, a bad value means that
setting's default, and the problems go to the log and to `inua config check`.

The shell's own icons (bar, control center, launcher, settings) are the
built-in [Phosphor](https://phosphoricons.com) regular icons (MIT; licence in
`ui/icons/LICENSE`), for inua's windows only; `style.icons = "system"` or the
Appearance page switches back to the system icon theme. App icons in the
launcher, dock and tray always come from the system theme. To change or add
an icon, edit the table in `scripts/phosphor-icons.sh` and run it again (its
argument is the `@phosphor-icons/core` directory), then once more with `bold`
as a second argument for the sample icon set.

Themes and icon sets of your own live next to `config.toml`:

- `themes/NAME.css` is a theme named `NAME`: `@define-color` lines for the
  tokens it changes (`app`, `surface`, `t0`, `accent`, … ; copy one of
  `ui/themes/` to start). The tokens it leaves out come from `yoru`; a file
  named like a built-in theme (`themes/washi.css`) changes that theme
  instead. Rules other than colours belong in `style.css`.
- `icons/NAME/` is an icon set named `NAME`: SVGs named like those in
  `ui/icons/` (`go-home-symbolic.svg`), each replacing the built-in icon;
  the icons it lacks stay Phosphor's. Draw them in one colour: GTK repaints
  symbolic icons in the text colour, so one set serves light and dark
  themes alike. inua's apps use the set too.

`inua init` installs one of each to start from: `themes/nord.css` and
`icons/phosphor-bold/`.

Names are lower-case letters, digits, `-` and `_`. Both appear in the
Appearance page and in `style.theme` / `style.icons`. The page also adds
them: Addâ¦ copies a theme file or an icon folder here, named after it
(never over one already there), and chooses it; Move to Trash takes away
the one in use; Open Folder shows the folder. After editing the files,
`inua call shell reload` loads them again; inua's apps follow at their
next start or settings change.

`style.text_scale` (80 to 150 percent, default 100; the Appearance page's
"Text size") sizes the text of the shell and inua's apps alone: icons,
spacing and your own `style.css` keep their sizes.

Tests keep `config.toml` equal to the defaults (`internal/config`): add new
settings to the sample too. `samples.go` embeds this directory's `hypr/` and
`inua/` files in `inua`; a test keeps the sample theme setting every token
`ui/style.css` uses and the sample icon set naming every built-in icon; a test keeps the `inua.conf` sample equal to what empty settings
produce (`internal/hyprconf`).

## Dock and bar

The dock sits on the selected screen edge of each monitor (`dock.position`:
`bottom`, `top`, `left` or `right`; default `bottom`): the pinned apps first
(`dock.pinned`, by desktop ID, in order), then, after a separator, the other
apps with windows. A square marks running apps, and an accent bar the one in
front. Click: launch an app that is not running; otherwise bring its most
recent window forward, and when it is already in front, cycle through its
other windows. The pointer stays where it is. Right-click: a new window, keep
it in or remove it from the dock, quit (close all its windows). Windows map
to apps by `StartupWMClass`, desktop ID, or the last part of a reverse-DNS ID
(`nautilus` for `org.gnome.Nautilus`).

With `dock.autohide = true` the dock stays hidden until the pointer reaches
its selected edge; otherwise windows make room for it. The Dock page turns
the dock on and off, sets its position, autohide and icon size, and reorders
or removes pinned apps. `style.edge_gaps = false` puts the dock and the bar flush
against the screen edges (the Dock page's "Remove screen edge gaps").
On the Bar page, "Show the bar" turns the bar off
(`bar.enable = false`); Super+Space, Super+A and Super+N still open the
launcher, control center and notifications.

## Notifications

`inua-shell` is the notification server (`org.freedesktop.Notifications`);
mako is not needed. It behaves like iOS:

- A new notification shows as a banner at the top right and goes after
  `notifications.banner_seconds` (5 by default); critical ones stay until
  dismissed. Hovering a banner holds it. An update from the same app with
  the same ID replaces it in place.
- After the banner, the notification waits in the notification center,
  grouped by app, newest first; several from one app form a stack ("N more"
  opens it), × clears the app's notifications, "Clear all" clears
  everything. Clicking a notification runs its default action (usually
  opening the app) and removes it; buttons run their actions. The center is
  saved in `~/.local/state/inua/notifications.json` and outlasts restarts.
- The bell in the bar, left of the control center's button, or Super+N opens
  the center. A square beside the bell marks unread notifications; the bell
  turns into a moon while Focus is on.
- With Focus on (the control center's Focus tile, or the moon in the
  center's head), banners stay quiet and notifications still collect in the
  center; `notifications.critical_in_focus` decides whether critical ones
  still show banners.
- `inua call notifications toggle|open|close|clear|focus [on|off]` controls
  it from scripts and shortcuts.

With `notifications.enable = false`, inua does not serve notifications: use
mako or another daemon (start it in `hyprland.conf`), and the Focus tile
switches mako's mode through `makoctl`. While another daemon runs, inua
waits and takes over when it exits.

Ubuntu's AppArmor profile (`/etc/apparmor.d/notify-send`) only lets
`notify-send` call `Notify` and `GetServerInformation`, so `notify-send -A`
can neither ask what the server supports nor receive button events, and says
"Actions are not supported". That happens with any notification server;
ordinary apps are not affected.

## Input method (Fcitx 5)

The sample starts Fcitx 5 when it is installed and sets only `XMODIFIERS`
and `QT_IM_MODULE`; GTK apps reach Fcitx through the Wayland text-input
protocol and need no `GTK_IM_MODULE`.

`make install-fcitx` installs two Fcitx 5 themes in inua's style:
`inua-light` (the aojashin colours) and `inua-dark` (yoru). The panel is
inua's card (the surface colour, a hairline border, 4 px corners, a soft
shadow), the selected candidate inua's selection (the accent at 9% with a
38% border, 2 px corners), and the page arrows are Phosphor's. Choose them in
Fcitx's settings (Addons → Classic User Interface → Theme), or set `Theme=`
and `DarkTheme=` in `~/.config/fcitx5/conf/classicui.conf`. Fcitx themes have
no corner radius: the corners and shadow are 9-slice images that
`scripts/fcitx-themes.py` draws from `ui/themes/*.css`; other inua colour
themes work too (`scripts/fcitx-themes.py denko:inua-denko`).

If the candidate window flickers on every key in Firefox or another GTK app,
something still exports `GTK_IM_MODULE=fcitx` (an old X11 setting, often in
`~/.bashrc`, `~/.profile` or `/etc/environment`). Programs started from a
terminal inherit it, including an `inua-shell` restarted from a terminal and
the apps its launcher opens. Remove the variable and restart those programs;
a normal login to the Inua session does not read `~/.bashrc`.
