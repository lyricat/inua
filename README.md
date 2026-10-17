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
| **Assistant** | An agent that answers questions about this computer and does things on it (windows, Wi-Fi, volume, Focus, notifications, Tailscale …), asking before it changes anything; on [Mister Morph](https://github.com/quailyquaily/mistermorph), with your own models and keys, and fallbacks between them. It stays running with the desktop and can serve as an [endpoint for other Consoles](docs/apps.md#console-endpoint). Its tools come from inua's [MCP server](docs/apps.md#mcp-inuas-tools-for-the-assistant-and-for-agents), which AI agents such as Claude Code can use too, through `inua mcp` ([the tools](docs/mcp.md)). |
| **Files** | A file manager to replace Nautilus, keyboard first: one or two panes, list or grid, filter by typing, jump to folders used before, search, details; drives and USB sticks through UDisks2 (mount, unlock, eject), network shares through GVfs, the freedesktop trash, undo; `inua-files`, see [docs/files.md](docs/files.md). |
| **Viewer** | Images, PDFs, Markdown and plain text: steps through the files of a folder, zooms, shows the details; `inua-viewer`, see [docs/apps.md](docs/apps.md#viewer). |
| **Virtual machines** | Machines of your own, each one a folder with a `machine.toml`: a list of machines on the left and the chosen machine's page on the right, the host checks with their fixes, the console while it installs, then Start, Stop and Connect. Eight templates, from Windows in Docker (Dockur) to Linux, FreeBSD, Android and a system of your own; Dockur is the backend this build runs; `inua-vm`, see [docs/apps.md](docs/apps.md#virtual-machines). |
| **Light apps** | Clock, Weather, Stocks, Calculator, Calendar, Color Picker, System Monitor, Tailscale, Pass, GPG, Music (a player in Winamp's spirit, with an equalizer and a spectrum) and Video. |
| **Workshop** | Write your own light apps in any language: the program describes its window in inua's building blocks and inua draws it in the shell's look. `inua workshop new`, `dev`, `install`; see [docs/workshop.md](docs/workshop.md). |
| **Games** | Three offline light games: Minesweeper, Space Pinball and Small Fortune, a short property game against the computer or your AI, with others at the screen or, on the same network, in a browser. Each has its own launcher, or run `inua-games minesweeper`. |
| **Keyboard and touchpad** | Swap Ctrl and Alt, remap Caps Lock, set the touchpad's speed. |
| **Themes** | Seven colour themes, Phosphor icons, matching Fcitx 5 themes; your wallpaper or the theme's. |

More on each: [the shell in detail](docs/features.md) and
[the light apps](docs/apps.md).

## Requirements

- Ubuntu 26.04 (or another distribution with the same packages)
- Hyprland 0.53, GTK 4.22, gtk4-layer-shell 1.3
- Recommended: pass and gnupg, where `inua-keyring` keeps apps' secrets
  (without them, apps keep using gnome-keyring); the install script
  offers to install them
- For Music and Video: GStreamer's plugins (base, good, bad, ugly and
  libav), which the install script also offers
- Go 1.26 or later to build

System features use the services you probably already run: NetworkManager,
BlueZ, UPower, power-profiles-daemon, PipeWire. Without one, its part of the
shell says "Unavailable" instead. The full list is in
[docs/design.md](docs/design.md#packages).

## Virtual machines

`inua-vm` is the machines' manager, a program of its own like `inua-files`:
any number of guests, each one a folder under `~/.local/share/inua/vm/NAME/`
described by a `machine.toml` in it, beside the guest's own files. Its window
is a list of machines on the left and the chosen machine's page on the right,
one page per stage: the host checks to fix when something is missing (KVM,
Docker, Compose v2, FreeRDP, …); a recommended profile and **Install
Windows** when nothing is installed yet; the web console, and stopping
without losing the disk, while Windows installs itself; then **Start**,
**Connect** and **Stop**. **New** makes a machine from one of eight
templates: Windows 11 in a container (Dockur), Windows 11 from your own ISO
with QEMU, Ubuntu, Fedora, Arch, FreeBSD, Android in Waydroid, and a custom
system with every boot field open. Removing a guest keeps its disk and its
password — deleting the installation, or the machine with its folder, is its
own, confirmed action.

Docker (Dockur) is the backend this build runs, for Windows; the QEMU
backend (Linux, FreeBSD, custom systems) and the Waydroid backend (Android)
are the following steps, so a machine of those kinds is stored, listed and
described, and its page says this build has no backend for it yet. For a
Windows guest there is no ISO to prepare: the container fetches Windows' own
installation media and installs it unattended, which is what takes tens of
minutes.

`inua-vm --list` names the machines, one per line; `inua-vm --doctor
[--json]` prints what this host provides and which machines there are, and
`inua-vm --status [--json]` what each is doing now, both read-only, for
scripts; `inua-vm --start NAME` and `--stop NAME` run a machine without a
window. `inua vm list|doctor|status` prints the same from the shell's own
command line. The image is pinned by version and digest, each machine's guest
password is kept in the Secret Service under its name (never in the project
or in your configuration), and every published port binds to 127.0.0.1, each
machine reserving its own, so two never collide.

Two things the window says outright: the web console on 127.0.0.1:8006 has
no login of its own, and Windows needs a licence. Docker socket access is
as good as root on the host, so inua explains it and never offers to add
you to the `docker` group: it names the command, for whoever decides they
want it, and points at rootless Docker or Podman instead. The `kvm` group
fix, which grants one device, stays. Docker Engine with Compose v2, KVM,
FreeRDP and `xdg-open` must be installed: the window names whichever is
missing and which packages provide it.

## Install

```sh
curl -fsSL https://inua.lyric.im/install.sh | sh
```

Then log out and pick **Inua** at the login screen. The script installs the
latest release for amd64 or arm64 (checking its checksum) into `~/.local`,
offers to install the packages inua needs with apt, pacman, dnf or zypper
(under each one's names), writes your
configuration with `inua init` (never replacing your files), and adds the
Inua login session (sudo). Where inua replaces something you have, it asks
first, and the answer defaults to yes for gpg's pinentry (with
`inua-pinentry`) and the keyring apps keep secrets in (gnome-keyring, with
`inua-keyring`, after copying its items into pass) and for Omarchy's
shell (Quickshell, with `inua-shell`). With no login manager
it offers greetd + tuigreet, the login screen inua recommends (yes); GDM,
SDDM … are kept unless you answer yes, and Inua joins their list of
sessions. Without a password store it offers to make one with a GPG key of
yours (yes), or with a new key whose passphrase you choose (no), and says
what to run to do it yourself. On Fedora 43 and later, Hyprland comes from a COPR
(Fedora's repositories stopped at 0.45); inua needs 0.53 or later.
`INUA_VERSION=v0.1.2` picks a release.

To update, run `inua upgrade`; `inua upgrade -check` only says whether a
newer release is out. It downloads the release (checking its checksum),
replaces the programs where `inua` is installed and the light apps'
launchers, and restarts the shell in place; it lists the programs that keep
running the old version until they restart (inua-keyring until you log in
again). `-version v0.1.2` installs a
given release. A build from source (`dev`) counts as older than every
release, so `inua upgrade` replaces it with the latest; `git pull` and
`make install` bring it back. Running the install script again also updates. The shell
checks for a new release a random 1 to 10 minutes after you log in, then
once a day, and tells you in a notification with an **Upgrade** button;
Settings → About shows the version, checks for a new release on demand, upgrades, and lists what the latest release changed; its switch (`updates` under `[notifications]` in `config.toml`) turns the daily check off.

The companions the sample config uses (a terminal, a file manager,
screenshot tools …) are in [docs/design.md](docs/design.md#packages).

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
| `inua` | `~/.local/bin` | The command line: `inua call …`, `inua init`, `inua config check`, `inua upgrade` |
| `inua-apps` | `~/.local/bin` | The light apps (`inua-apps clock`) |
| `inua-games` | `~/.local/bin` | The light games (`inua-games minesweeper`) |
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

With a Lua config (`hyprland.lua`, Hyprland 0.55 and later), the same two
lines are:

```lua
hl.on("hyprland.start", function() hl.exec_cmd("inua-shell") end)
pcall(require, "inua")    -- keep this last; loads inua.lua
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
| Print | Screenshot tool, saved and copied quietly (Space switches area and window) |
| Ctrl+Print | Screenshot tool, then the drawer of recent shots |
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
to `~/.config/hypr/inua.conf`, which your `hyprland.conf` sources (or to
`inua.lua`, which a `hyprland.lua` requires).
Restyle anything with `~/.config/inua/style.css`.

## Development

```sh
make             # bin/inua-shell, bin/inua, bin/inua-apps, bin/inua-games and bin/inua-pinentry
make test        # session script tests, go vet, unit tests with -race
make generate    # ui/*.blp → ui/*.ui (needs blueprint-compiler)
make nested      # run the shell in a nested Hyprland window
make run         # run it in the current Hyprland session
```

- `inua-shell -ui-dir ui` reads layouts and styles from disk.
- `inua call shell snapshot DIR` renders the shell's windows to PNG files.
- `inua call shell restart` re-executes the shell in place after a rebuild.

Translations are gettext catalogs in `internal/i18n/locales`, keyed by
the English text; `make i18n` gathers the text to translate into
`po/inua.pot` and updates them, including desktop launchers. The interface
supports Simplified Chinese, Traditional Chinese, Japanese, Spanish, French
and Korean, chosen in Settings → Appearance → Language or by the
environment (`LANGUAGE=zh-hant inua-apps clock`): see
[docs/i18n.md](docs/i18n.md).

Pull requests and pushes to `master` run Go formatting checks, regenerate
Blueprint layouts and the translation catalogs and check for differences, run `make test`, and build all
programs on Ubuntu 26.04. Desktop interactions still need a manual check
in a Hyprland session.

Releases: tag a version (`git tag v0.1.2 && git push --tags`) and GitHub
Actions builds the Linux releases with GoReleaser (`.goreleaser.yaml`);
`goreleaser release --snapshot --clean` tries it locally.

| Path | |
| --- | --- |
| `cmd/inua-shell`, `cmd/inua`, `cmd/inua-apps`, `cmd/inua-games`, `cmd/inua-pinentry` | The shell, the command line, the light apps, the light games, gpg's pinentry |
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

Documentation: [the shell in detail](docs/features.md), [light apps](docs/apps.md), [workshop](docs/workshop.md), [MCP tools](docs/mcp.md),
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
