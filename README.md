<p>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="design/logo/inua-mark-light.svg">
    <img src="design/logo/inua-mark.svg" alt="The inua mark: a triquetra in a ring" width="96" height="96">
  </picture>
</p>

# inua

A quiet desktop shell for [Hyprland](https://hypr.land), written in Go and
GTK4. [inua.one](https://inua.one)

Hyprland manages your windows; inua gives you the rest of a desktop around
them, in one calm visual language: a bar, a launcher, a control center,
notifications, a dock, a wallpaper, settings, and a set of apps.

- **Pure Go, no cgo.** GTK4 through [puregotk](https://codeberg.org/puregotk/puregotk),
  layer surfaces through gtk4-layer-shell. `CGO_ENABLED=0 go build` builds
  the whole shell.
- **Layouts are markup.** Every surface is a Blueprint file styled with CSS
  from colour tokens; Go only fills in state.
- **The compositor is the truth.** Windows, workspaces and focus come from
  Hyprland's IPC; inua never keeps a second copy.

## The shell

| | |
| --- | --- |
| **Bar** | Workspaces, the window of its own monitor (the focused one there, else the last focused there) with its application menu, the clock and calendar, the tray. One per monitor. |
| **Launcher** | Apps, windows and commands in one search; also arithmetic, units, currencies, emoji (`:`) and the assistant (`?`). |
| **Overview** | Live previews of every workspace (Hyprland 0.55+). |
| **Control center** | Wi-Fi, Bluetooth, Focus, Tailscale, brightness, volume, media and power. |
| **Notifications** | inua is the notification server: banners, then a center grouped by app. |
| **Dock** | Pinned and running apps, and a drawer of the shell's tools. |
| **Settings** | One searchable window, applied as you change it: displays, keyboard and touchpad, Bluetooth, shortcuts, themes, About. |
| **Lock screen** | On the compositor's session lock: if the shell stops, the screen stays locked. |
| **Screenshots** | Areas, windows, screens and recordings, with a drawer of recent shots. |
| **Wallpaper** | A new picture drawn each day, or your own. |
| **Passwords and secrets** | The Polkit agent, gpg's pinentry, and `inua-keyring`, a Secret Service backed by pass. |

In detail: [docs/features.md](docs/features.md).

## The apps

| | |
| --- | --- |
| **Files** | A keyboard-first file manager: two panes, drives through UDisks2, remote disks through rclone. [docs/files.md](docs/files.md) |
| **Terminal** | Workspaces of terminals for running many agents at once, each one's state in view; also on a phone. [docs/terminal.md](docs/terminal.md) |
| **Mail** | Every account in three columns: Gmail, Outlook and IMAP, with phishing checks and the assistant. [docs/mail.md](docs/mail.md) |
| **Assistant** | An agent that answers about this computer and acts on it, asking first; its tools are also an MCP server (`inua mcp`). [docs/mcp.md](docs/mcp.md) |
| **Viewer** | Images, PDFs, Markdown and text. |
| **Virtual machines** | Windows (Dockur), Linux, FreeBSD and your own systems (QEMU), each a folder with a `machine.toml`. |
| **Remote Desktop** | Share this desktop and reach others over Moonlight's protocol. [docs/remote-desktop.md](docs/remote-desktop.md) |
| **Mobile Link** | Your phone and this computer together, over KDE Connect's protocol. [docs/mobile-link.md](docs/mobile-link.md) |
| **Disk Usage** | What fills a disk, and what of it can safely go. [docs/disk-usage.md](docs/disk-usage.md) |
| **Light apps** | Clock, Weather, Stocks, Calculator, Calendar, Color Picker, System Monitor, Tailscale, Pass, GPG, Music and Video. |
| **Games** | Minesweeper, Space Pinball, Small Fortune and Chinese Checkers; the last two with others in a browser, on the network or at a lobby. [docs/game-lobby.md](docs/game-lobby.md) |
| **Workshop** | Write your own light apps in any language; inua draws them in its look. [docs/workshop.md](docs/workshop.md) |

In detail: [docs/apps.md](docs/apps.md).

## Install

```sh
curl -fsSL https://inua.one/install.sh | sh
```

Then log out and pick **Inua** at the login screen.

The script installs the latest release for amd64 or arm64 into `~/.local`,
offers the packages inua needs (apt, pacman, dnf or zypper), writes your
configuration with `inua init` without replacing your files, and adds the
Inua login session (sudo). Where inua would replace something you have —
gpg's pinentry, gnome-keyring, another shell, the login manager — it asks
first; on Debian and Ubuntu, inua's pinentry can also become the system's,
through `update-alternatives`. `INUA_VERSION=v0.1.2` picks a release.

`inua upgrade` updates in place; the shell also checks once a day and
offers an **Upgrade** button.

Requirements:

- Hyprland 0.53 or later, GTK 4.22, gtk4-layer-shell 1.3 (Ubuntu 26.04 or
  a distribution with the same packages; on Fedora, Hyprland from a COPR)
- Recommended: pass and gnupg for `inua-keyring`; GStreamer's plugins for
  Music and Video
- The usual services: NetworkManager, BlueZ, UPower, power-profiles-daemon,
  PipeWire. Without one, its part of the shell says "Unavailable".

The full package list is in [docs/design.md](docs/design.md#packages). On
Ubuntu Desktop, see [removing GNOME](docs/removing-gnome.md).

### Into an existing Hyprland setup

Keep your config and add:

```ini
exec-once = inua-shell
source = ./inua.conf      # keep this last
```

or, in `hyprland.lua` (Hyprland 0.55+):

```lua
hl.on("hyprland.start", function() hl.exec_cmd("inua-shell") end)
pcall(require, "inua")    -- keep this last
```

`inua config hyprland` checks it. Setup in detail:
[config/README.md](config/README.md).

### From source

```sh
git clone https://github.com/lyricat/inua
cd inua
make deps deps-desktop   # packages (sudo)
make install-session     # programs into ~/.local/bin, your config, the login session (sudo)
```

`make install` alone installs the programs; run `inua init` once after it.

## Use

| Keys | |
| --- | --- |
| Super+Space | Launcher |
| Super+Tab | Overview |
| Super+A | Control center |
| Super+N | Notification center |
| Super+Comma | Settings |
| Super+L | Lock |
| Print | Screenshot |
| Super+Shift+R | Restart the shell |

Everything is also a command:

```sh
inua call launcher toggle
inua call screenshot window
inua show                  # everything `call` accepts
```

Settings live in `~/.config/inua/config.toml`, written by the settings
window and fine to edit by hand (`inua config check`, then
`inua call shell reload`). Every option is in the
[sample](config/inua/config.toml). Hyprland's own settings go to
`~/.config/hypr/inua.conf`; restyle anything in `~/.config/inua/style.css`.

## Development

Go 1.26 or later.

```sh
make             # every program into bin/
make test        # session script tests, go vet, Go tests with -race
make generate    # ui/*.blp → ui/*.ui (blueprint-compiler)
make i18n        # po/inua.pot and the catalogs
make nested      # the shell in a nested Hyprland window
```

`inua-shell -ui-dir ui` reads layouts and styles from disk;
`inua call shell restart` re-executes the shell after a rebuild.

Start with [the architecture](docs/architecture.md); the look and its rules
are in [design/ui.md](design/ui.md), translations in
[docs/i18n.md](docs/i18n.md) (Chinese, Japanese, Korean, Spanish, French),
start-up time in [docs/performance.md](docs/performance.md).

CI checks formatting, regenerates layouts and catalogs, runs `make test`
and builds on Ubuntu 26.04. A pushed tag (`v0.1.2`) builds a release with
GoReleaser.

## Credits

Icons by [Phosphor](https://phosphoricons.com) (MIT, `ui/icons/LICENSE`);
the terminal's agent marks from [LobeHub](https://github.com/lobehub/lobe-icons)
(MIT, `ui/icons/LICENSE-lobehub`; the marks remain their owners' trademarks).
Terminal engine: [libghostty-vt](https://github.com/ghostty-org/ghostty) (MIT).
Emoji names from Unicode's data and CLDR (`internal/emoji/LICENSE-UNICODE`).
Exchange rates from the European Central Bank (through Frankfurter), the
Bank of England (through [AllRatesToday](https://allratestoday.com), when
Frankfurter is down) and Coinbase. Built on [Hyprland](https://hypr.land), [GTK](https://gtk.org),
[puregotk](https://codeberg.org/puregotk/puregotk),
[gtk4-layer-shell](https://github.com/wmww/gtk4-layer-shell) and
[godbus](https://github.com/godbus/dbus).

## License

[Apache License 2.0](LICENSE).
