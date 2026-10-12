# inua

A quiet desktop shell, written in Go and GTK4.

Your compositor manages windows; inua gives you the rest of a desktop around
them: a bar, a launcher, a control center, notifications, a dock, a wallpaper
and a settings window, in one calm visual language. It works with
[Hyprland](https://hypr.land) today.

- **Pure Go, no cgo.** GTK4 through [puregotk](https://codeberg.org/puregotk/puregotk),
  layer surfaces through gtk4-layer-shell. `CGO_ENABLED=0 go build` and you
  have the whole shell.
- **Layouts are markup.** Every surface is a Blueprint file compiled to
  GtkBuilder `.ui`, styled with CSS from colour tokens; Go only fills in
  state.
- **The compositor is the truth.** Windows, workspaces and focus come from
  the compositor (Hyprland's IPC and events); inua never keeps a second copy.

## What's in it

| | |
| --- | --- |
| **Bar** | Workspaces, the focused window's title, the clock and its calendar, the tray, notifications and the control center. One per monitor. |
| **Launcher** | One search for apps, windows and the system's commands; it answers arithmetic, units, currencies and crypto, searches the web and sites by keyword, finds emoji after `:`, and asks the assistant after `?` (Super+Space). |
| **Workspace overview** | Live window previews across monitors; tap Win, or cycle with Win+Tab. Drag to move, middle click to close, / to search. Needs Hyprland 0.55 or later (its Lua interface); before that, Super+Tab switches windows in the launcher. |
| **Control center** | Wi-Fi, Bluetooth, power saver, Focus and Tailscale tiles; brightness, volume and media; power. |
| **Notifications** | inua is the notification server: banners, then a notification center grouped by app. |
| **Dock** | Pinned and running apps; drag to reorder or to unpin; a drawer of the shell's tools and the light apps. |
| **Settings** | One window for everything, applied as you change it. |
| **Shortcuts** | Rebind the shell's actions and your own commands by pressing keys. |
| **Lock screen** | On the compositor's session lock: if the shell stops, the screen stays locked. |
| **Screenshots** | Areas, windows and screens, recordings, and a drawer of recent shots to drag into apps. |
| **Passwords** | inua asks for administrator passwords (it is the Polkit agent) and gpg's passphrases. |
| **Keyring** | `inua-keyring` serves apps' secrets (the Secret Service: browsers, Wi-Fi, the assistant's keys) from pass, after importing gnome-keyring's. |
| **Assistant** | An agent that answers questions about this computer and does things on it (windows, Wi-Fi, volume, Focus, notifications, Tailscale …), asking before it changes anything; on [Mister Morph](https://github.com/quailyquaily/mistermorph), with your own models and keys, and fallbacks between them. |
| **Light apps** | Clock, Weather, Stocks, Calculator, Calendar, Color Picker, System Monitor, Tailscale and Viewer. |
| **Keyboard and touchpad** | Swap Ctrl and Alt, remap Caps Lock, set the touchpad's speed. |
| **Themes** | Seven colour themes, Phosphor icons, matching Fcitx 5 themes; your wallpaper or the theme's. |

More on each: [the shell in detail](docs/features.md) and
[the light apps](docs/apps.md).

## Requirements

- Ubuntu 26.04 (or another distribution with the same packages)
- Hyprland 0.53, GTK 4.22, gtk4-layer-shell 1.3
- Go 1.26 or later to build

System features use the services you probably already run: NetworkManager,
BlueZ, UPower, power-profiles-daemon, PipeWire. Without one, its part of the
shell says "Unavailable" instead. The full list is in
[docs/design.md](docs/design.md#packages).

## Install

```sh
curl -fsSL https://inua.lyric.im/install.sh | sh
```

Then log out and pick **Inua** at the login screen. The script installs the
latest release for amd64 or arm64 (checking its checksum) into `~/.local`,
offers to install the packages inua needs with apt, writes your
configuration with `inua init` (never replacing your files), and adds the
Inua login session (sudo). Where inua replaces something you have, it asks
first, and the answer defaults to yes: gpg's pinentry (with
`inua-pinentry`), the keyring apps keep secrets in (gnome-keyring, with
`inua-keyring`, after copying its items into pass), and the login manager
(GDM, SDDM … with greetd + tuigreet, the login screen inua recommends; the
old one stays installed). Run it again to update;
`INUA_VERSION=v0.1.2` picks a release. The companions the sample config uses
(a terminal, a file manager, screenshot tools …) are in
[docs/design.md](docs/design.md#packages).

From source:

```sh
git clone https://github.com/lyricat/inua
cd inua
make deps deps-desktop   # packages (sudo)
make install-session     # build, install to ~/.local/bin (and the apps' launchers), write the sample config,
                         # and add the "Inua" login session (sudo for that part)
```

`make install-session` runs three steps: `make install` (the programs),
`make install-config` (`inua init`, your configuration) and, with sudo,
the login session. What ends up where:

| | Installed to | |
| --- | --- | --- |
| `inua-shell` | `~/.local/bin` | The shell |
| `inua` | `~/.local/bin` | The command line: `inua call …`, `inua init`, `inua config check` |
| `inua-apps` | `~/.local/bin` | The light apps (`inua-apps clock`) |
| `inua-pinentry` | `~/.local/bin` | gpg's passphrase prompt, asking in inua's dialog |
| Light app launchers and icons | `~/.local/share/applications`, `~/.local/share/icons` | So the launcher and dock list them |
| Sample configuration | `~/.config/hypr`, `~/.config/inua` | Written by `inua init`, which never replaces your files (unless `-force`, keeping `.bak`) and points gpg-agent at `inua-pinentry` (asking first when it uses another pinentry; `-pinentry replace` or `keep` answers ahead) |
| `inua-session`, `inua.desktop` | `/usr/local/bin`, `/usr/share/wayland-sessions` | The login session (sudo); with greetd, also `/etc/greetd/config.toml` |

Installed only the programs (`make install`)? Run `inua init` once for
your configuration. Running it again after an update adds new sample files
and leaves yours alone.

Already using Hyprland? Keep your config and add two lines:

```ini
exec-once = inua-shell
source = ./inua.conf      # keep this last
```

`inua config hyprland` checks both. Setup in detail:
[config/README.md](config/README.md).

## Everyday use

| Keys | |
| --- | --- |
| Super+Space | Launcher |
| Super+Tab | Workspace overview (Hyprland 0.55+; before, switch windows) |
| Super+A | Control center |
| Super+N | Notification center |
| Super+Comma | Settings |
| Super+L | Lock the screen |
| Print | Screenshot tool (Space switches area and window) |
| Super+Shift+R | Restart the shell |

Everything is also a command, for scripts and your own shortcuts:

```sh
inua call launcher toggle
inua call notifications focus on
inua call settings open shortcuts
inua call screenshot window      # the focused window, at once
inua call lock now
inua show                      # list everything `call` accepts
```

## Configuration

Settings live in `~/.config/inua/config.toml`, written with comments by the
settings window; editing it by hand works too (`inua call shell reload`
applies it, `inua config check` checks it). The sample lists every option:
[config/inua/config.toml](config/inua/config.toml).

Keyboard, touchpad and shortcut settings are Hyprland's, so inua writes them
to `~/.config/hypr/inua.conf`, which your `hyprland.conf` sources.
Restyle anything with `~/.config/inua/style.css`.

## Development

```sh
make             # bin/inua-shell, bin/inua, bin/inua-apps and bin/inua-pinentry
make test        # session script tests, go vet, unit tests with -race
make generate    # ui/*.blp → ui/*.ui (needs blueprint-compiler)
make nested      # run the shell in a nested Hyprland window
make run         # run it in the current Hyprland session
```

- `inua-shell -ui-dir ui` reads layouts and styles from disk.
- `inua call shell snapshot DIR` renders the shell's windows to PNG files.
- `inua call shell restart` re-executes the shell in place after a rebuild.

Releases: tag a version (`git tag v0.1.2 && git push --tags`) and GitHub
Actions builds the Linux releases with GoReleaser (`.goreleaser.yaml`);
`goreleaser release --snapshot --clean` tries it locally.

| Path | |
| --- | --- |
| `cmd/inua-shell`, `cmd/inua`, `cmd/inua-apps`, `cmd/inua-pinentry` | The shell, the command line, the light apps, gpg's pinentry |
| `internal/shell` | Shell state: launcher, workspaces, dock (no GTK, unit tested) |
| `internal/ui` | The GTK views |
| `internal/hypr` | Hyprland IPC: requests, events, snapshots |
| `internal/notify`, `internal/sni`, `internal/system` | Notifications, tray, system services over D-Bus |
| `internal/lock` | The lock screen's password check |
| `internal/polkit`, `internal/pinentry` | The Polkit agent and gpg's pinentry protocol, for the password dialog |
| `internal/apps/*`, `internal/appui` | The light apps: their logic (no GTK, unit tested) and their windows |
| `internal/config`, `internal/hyprconf` | `config.toml`, and the Hyprland settings written to `inua.conf` |
| `internal/gtkx`, `internal/layershell` | puregotk's ownership rules, gtk4-layer-shell |
| `ui/` | Blueprint layouts, CSS, colour themes, icons |
| `config/` | Sample configuration, login session, Fcitx themes |

Documentation: [the shell in detail](docs/features.md), [light apps](docs/apps.md),
[design](docs/design.md), [architecture](docs/architecture.md),
[toolkit choice](docs/toolkit.md), [setup](config/README.md).

## Credits

Icons by [Phosphor](https://phosphoricons.com) (MIT, see `ui/icons/LICENSE`).
Emoji and symbol names from Unicode's emoji data and CLDR (Unicode License,
see `internal/emoji/LICENSE-UNICODE`). Exchange rates from the European
Central Bank (through Frankfurter) and Coinbase.
Built on [Hyprland](https://hypr.land), [GTK](https://gtk.org),
[puregotk](https://codeberg.org/puregotk/puregotk),
[gtk4-layer-shell](https://github.com/wmww/gtk4-layer-shell) and
[godbus](https://github.com/godbus/dbus).

## License

[Apache License 2.0](LICENSE).
