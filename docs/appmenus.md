# Application menus

The bar shows the menu of the application its monitor speaks for. Linux
has no single menu standard, so the bar takes a real menu wherever an
application publishes one, and a file can supply the menu where nothing
is published. Adding a file does not mean rebuilding the shell.

## Where a menu comes from

For the focused window, in this order:

1. The menu the application registered with
   `com.canonical.AppMenu.Registrar` (`com.canonical.dbusmenu`). Qt
   applications do this themselves. A GTK 3 application does it when
   `appmenu-gtk-module` is loaded. Electron and Chromium do it when
   `libdbusmenu` is installed and `UBUNTU_MENUPROXY=1`.
2. A menu an inua application published on the same registrar.
3. Firefox's menu. On Wayland, Firefox does not register; it leaves the
   menu on its own bus name (`org.mozilla.firefox.*`, under
   `/com/canonical/menu`). The shell reads that. Firefox only exports it
   when `widget.gtk.global-menu.enabled` is true.
4. The application's accessibility tree (AT-SPI), for a GTK, Qt or
   Electron window that published nothing above. The bar reads the menu
   bar and, for a section the application fills in when it opens, asks
   again at the moment it is opened.
5. A [menu pack](#menu-packs) whose `when` is `replace` is used instead
   of all of the above. A pack whose `when` is `fallback` (the default)
   is used when none of the above produced a usable menu. With no pack,
   the bar shows the generic menu: File (Close Window, Quit), Edit (Copy
   Window Title, Copy Window Class), View (Fullscreen, Float, Center
   Window) and Help (About). Those actions are the shell's and the
   compositor's. They do not depend on the application having a menu.

GTK 4 has no module that exports a menu bar, and the Wayland protocols
KDE and GTK use to point at a menu (`org_kde_kwin_appmenu`, `gtk-shell`)
are not a shared standard. The registrar was designed around an X11
window id; the shell matches a registration to a window by process id,
which is what a Wayland window has. A pack is how an application that
will never publish a menu still gets one.

Other desktops that show a global menu (Unity, Plasma, vala-panel-appmenu
on XFCE and MATE, Deepin, helloSystem) stop at the registrar. They ship
toolkit modules so applications publish a real menu, and they leave the
bar empty when an application does not. None of them has a file format
for the applications that never will. The packs are that format. The
same desktops also carry one-off exceptions in code (vala-panel special-
cases Firefox). Those exceptions are packs here, so the next one is a
file rather than a change to the shell.

To have applications publish a real menu, the session can export:

```sh
export GTK_MODULES=appmenu-gtk-module
export UBUNTU_MENUPROXY=1
```

`appmenu-gtk-module` covers GTK 2 and GTK 3. Qt needs no module. Java
Swing needs Jayatana (`_JAVA_OPTIONS` pointing at its agent and
`JAYATANA_FORCE=1`). The shell does not set these: they change other
programs' behaviour. A pack is still what covers GTK 4, an Electron
application that does not export, a terminal, a game.

## Menu packs

One application is one TOML file. The shell reads them from these
places, and a higher-priority source replaces a pack with the same
`id`:

1. The packs compiled into the shell, whose source is
   [`share/inua/menus`](../share/inua/menus).
2. Each `$XDG_DATA_DIRS` entry's `inua/menus` directory. The default is
   `/usr/local/share` then `/usr/share`; earlier entries in the variable
   have higher priority.
3. `$XDG_DATA_HOME/inua/menus` (`~/.local/share/inua/menus`).
   The shipped packs are embedded in the shell binary; `make install` does not copy pack TOML files here, but a user or package can place `.toml` files in this directory.
4. When the shell is run with `-ui-dir`, `share/inua/menus` next to that
   directory's parent, so a checkout is used as it is on disk.
5. `$XDG_CONFIG_HOME/inua/menus` (`~/.config/inua/menus`). This is where
   a menu of your own goes. It wins over a shipped pack with the same
   `id`.

Files are read again when their size or modification time changes. The
bar picks the new menu the next time it builds one, which is when the
focused window changes. A file that does not parse is skipped and the
reason is written to the shell's log. The others are kept.

A pack matches the window's application id (Hyprland's class) and the
desktop entry's id. The comparison is case-insensitive. Copy Window
Class, in the generic Edit menu, puts the application id on the
clipboard.

```toml
id = "org.wezfurlong.wezterm"
match = ["org.wezfurlong.wezterm", "wezterm"]
suffix = [".wezterm"]      # the id ends with this
contains = ["wezterm"]     # the id contains this; prefer match
when = "fallback"          # or "replace"
skip_a11y = false          # do not read the accessibility menu
never_press = false        # do not activate an accessibility section

include = ["generic"]      # start from the generic File, Edit, View, Help

[[section]]
label = "File"

[[section.item]]
label = "New Tab"
keys = ["CTRL", "T"]

[[section.item]]
separator = true

[[section.item]]
label = "Close Window"
action = "close-window"

[[section]]
label = "View"
include = "window"         # Fullscreen, Float, Center Window

[[section.item]]
label = "Zoom In"
keys = ["CTRL", "plus"]
```

`id` is required. At least one of `match`, `suffix` and `contains` is
required. `when` is `fallback` or `replace`; omitted, it is `fallback`.
`fallback` is used only when the application has no usable menu of its
own. `replace` is shown even when it has one, which is for a published
menu that cannot be used.

`skip_a11y` and `never_press` are parsed and preserved on packs. Runtime handling may be limited depending on the menu source and branch integration; the shipped code/firefox/chromium packs carry these flags for their intended behavior, and unsupported combinations should be treated as unverified rather than guaranteed. The shipped `code` pack sets
`skip_a11y`: VS Code draws its menu itself and the accessibility bus
only has an overflow item. The shipped `zed` pack also sets `skip_a11y`:
Zed draws its menu itself and exposes neither an accessible menu nor a
D-Bus menu. Its pack mirrors Zed's Linux menus in full; commands that
have a default shortcut or a key sequence are sent, and the rest are
shown disabled. A custom keymap or Vim mode can change which keys those
commands need. Chromium and Chrome are read from the menu they publish on
XWayland; running natively on Wayland they publish none, so the shipped
`chromium` pack sends their keyboard shortcuts. The shipped `firefox`
pack sets `never_press`: pressing a section of Firefox's accessibility
menu ends Firefox. A pack with no sections and no `include` adds no
items; the generic menu is shown, and the flags still apply.

Within one directory, files are applied in name order and the last pack
that matches a window wins. Two packs should not match the same window
on purpose. Replacing a shipped pack is done with the same `id`.

### What an item can do

An item is a separator, a submenu, a shell action, a shortcut, or a
disabled line. It has exactly one of those. `disabled = true` shows the
line greyed out, for a command the application has but the pack cannot
invoke without a keybinding.

| `action` | What it does |
| --- | --- |
| `close-window` | Closes the window the bar is showing. |
| `quit-app` | Closes every window of that process. |
| `fullscreen` | Fullscreen, where the compositor can. |
| `float` | Floats or tiles the window. |
| `center` | Centres the window. |
| `copy-title` | Copies the window title. |
| `copy-class` | Copies the application id, which is what `match` compares. |
| `about` | A window with the application's name, icon, title and process id. |

`include = ["generic"]` on the pack inserts those four sections, already
translated. `include = "window"` on a section inserts Fullscreen, Float
and Center Window, then the section's own items.

`keys` is sent to that window. The last element is the key and the ones
before it are the modifiers, written as Hyprland names them: `CTRL`,
`SHIFT`, `ALT`, `SUPER`. A single letter is uppercased (`t` is sent as
`T`). A longer name is sent as written (`Return`, `plus`, `Print`), so
the spelling has to be the one the compositor uses. On Hyprland the
shell focuses the window without moving the pointer and dispatches
`sendshortcut` to its address. A compositor without the keystroke
capability does not send the shortcut; the item is still shown, and the
failure is logged. The shortcut is sent whether or not the application's
own menu item would be enabled, and it goes stale when the application
changes its bindings.

`sequence` sends a series of key strokes for a chord such as `g` then
`d`. Each element is one key, optionally preceded by `CTRL+`, `SHIFT+`,
`ALT+` or `SUPER+`; key strokes are sent 60 ms apart. This is useful for
Vim-style keymaps, which have bindings that cannot be read from the
application's menu. For example:

```toml
[[section.item]]
label = "Go to Definition"
sequence = ["g", "d"]
```

A Zed pack follows the Linux default keymap. To match a custom keymap or
Vim mode, override `zed.toml` in `$XDG_CONFIG_HOME/inua/menus` and use
`sequence` for its chords; menu packs cannot discover another application's
user keymap.

A submenu is an item with items of its own:

```toml
[[section.item]]
label = "Share"

[[section.item.item]]
label = "Copy Link"
keys = ["CTRL", "SHIFT", "C"]
```

Labels in a pack are shown as written. They are not translated and they
do not follow the application's language. The generic menu does, because
the shell owns its wording. A real dbusmenu is already in the
application's language.

A pack cannot run a program or call D-Bus. A file installed with the
system would otherwise be able to run what it liked. An application that
can only be driven that way needs a shell action added deliberately.

Dynamic menus (a recent-files list, a menu that depends on the
selection) cannot be described in the file. Those have to come from the
application.

### Writing one

This walkthrough creates a private menu for one application. It does not
require rebuilding or restarting the shell.

1. Open the application and use **Edit → Copy Window Class** in the bar's
generic menu. This copies the window's application id. If the application
has no generic menu visible, its class can also be read from the
compositor's client list.
2. Create the personal pack directory and a TOML file. Use a stable pack
`id`; it is the override key and does not have to equal the application
id. A useful convention is to use the desktop entry id.
3. Put the copied class in `match`, then define sections and items. Use
`keys` for shortcuts the application already supports; use `action` for
shell and window operations.
4. Save, focus another window, then focus the application again. The bar
rebuilds its menu when focus changes and rereads changed pack files.

For example, save this as `~/.config/inua/menus/org.wezfurlong.wezterm.toml`
(replace the value in `match` with the class copied from your window):

```toml
id = "org.wezfurlong.wezterm"
match = ["org.wezfurlong.wezterm"]
include = ["generic"]

[[section]]
label = "Terminal"

[[section.item]]
label = "New Tab"
keys = ["CTRL", "SHIFT", "T"]

[[section.item]]
label = "Close Window"
action = "close-window"
```

The `generic` include contributes File, Edit, View and Help. The Terminal
section is added after those. To start with only your own sections, remove
`include`. To nest options, add `[[section.item.item]]` below a submenu
item. A `separator = true` item needs no label. Each ordinary item must
specify exactly one of `action`, `keys`, `sequence`, child `item` entries
or `disabled = true`; a separator cannot be combined with those. TOML
table array nesting matters: child items belong immediately under their
parent item.

The match fields are alternatives: a pack matches if the case-insensitive
application id or desktop entry id equals any `match` token, ends with
any `suffix` token, or contains any `contains` token. Prefer `match` when
you know the exact id; broad suffix or substring rules can affect several
applications. If multiple packs match, the last one in the loaded order
wins. Keep matching rules narrow and avoid accidental overlaps.

To change a shipped pack, put a file with the same `id` in
`~/.config/inua/menus/`. For example, copying `code.toml` and changing
`skip_a11y` overrides the shipped VS Code exception. The same directory
can contain packs for applications that have no shipped pack. Pack files
are limited to 256 KiB and submenu nesting to eight levels. Unknown
fields, unknown includes or actions, invalid `when` values, and malformed
items make that file invalid; it is skipped and the reason appears in the
shell log. Other valid packs continue to load.

A pack worth shipping goes in `share/inua/menus/` under its `id`, with
no machine-specific paths. The shipped files are embedded in the binary;
The shipped files are embedded in the shell binary. A package or local development setup can also install them under a data directory for
packaging workflows. `go test ./internal/appmenu` parses every shipped
file and checks the schema.

#### Choosing fallback or replace

Leave `when` out, or set `when = "fallback"`, for a menu that fills the
gap only when the application has no usable published menu. This is the
usual choice. Use `when = "replace"` only when the published menu is
unusable and you deliberately want the pack every time. A pack with no
sections and no include adds no entries, but its `skip_a11y` and
`never_press` settings still affect how published menus are handled.

`skip_a11y = true` avoids reading that window's accessibility menu.
`never_press = true` leaves an accessibility menu visible, but prevents
its sections from being invoked. These options apply whenever the pack
matches, whether or not the pack's own menu is selected. The shipped VS
Code and Firefox packs demonstrate those cases.

#### Reloading and troubleshooting

The shell checks pack files by their size and modification time. After
editing, change focus away from the target window and back. If the menu
does not change:

- Check that the filename ends in `.toml` and is directly inside
  `~/.config/inua/menus/` (subdirectories are not scanned).
- Confirm `id` is non-empty and at least one match field is present.
- Check TOML array-of-table placement, especially nested
  `[[section.item.item]]` entries.
- Check the shell log for a parse or validation error. One invalid file
  does not prevent other packs from loading.
- Confirm the pack matches the copied application class or desktop id;
  matching is case-insensitive, but whitespace and spelling still matter.
- If another pack also matches, narrow its rules. Within one directory,
  `.toml` filenames are applied in alphabetical order, and the later
  matching pack wins.

## What this does not do

The shell does not load a Go plugin or a shared library for a menu. A
pack is data. It does not download packs. It does not implement KDE's or
GTK's private Wayland menu protocol; an application that only publishes
there is, for now, a candidate for a pack of its shortcuts.
