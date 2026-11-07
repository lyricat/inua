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
places, and a later place replaces a pack with the same `id`:

1. The packs compiled into the shell, whose source is
   [`share/inua/menus`](../share/inua/menus).
2. `$XDG_DATA_DIRS/inua/menus` (the default is `/usr/local/share` and
   then `/usr/share`; a directory earlier in the variable wins).
3. `$XDG_DATA_HOME/inua/menus` (`~/.local/share/inua/menus`).
   `make install` copies the shipped packs here.
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

`skip_a11y` and `never_press` apply whenever the pack matches, including
when a real menu is also present. `skip_a11y` does not ask the
accessibility bus. `never_press` still shows a menu that was read, and
refuses to invoke a section of it. The shipped `code` pack sets
`skip_a11y`: VS Code draws its menu itself and the accessibility bus
only has an overflow item. The shipped `firefox` pack sets
`never_press`: pressing a section of Firefox's accessibility menu ends
Firefox. A pack with no sections and no `include` adds no items; the
generic menu is shown, and the flags still apply.

Within one directory, files are applied in name order and the last pack
that matches a window wins. Two packs should not match the same window
on purpose. Replacing a shipped pack is done with the same `id`.

### What an item can do

An item is a separator, a submenu, a shell action, or a shortcut. It has
exactly one of those.

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

Copy Window Class from the generic Edit menu. Put a file in
`~/.config/inua/menus/some-app.toml` with that id in `match`. List the
commands you want and the shortcuts the application already has. Focus
another window and come back. The bar reads the file again.

A pack worth shipping goes in `share/inua/menus/` under its `id`, with
no machine-specific path in it. `go test ./internal/appmenu` parses
every shipped file and rejects an unknown field, an unknown action, a
missing `id` or an item that is more than one of an action, a shortcut
and a submenu. The same checks apply to a file on disk; a bad file is
skipped rather than stopping the shell.

## What this does not do

The shell does not load a Go plugin or a shared library for a menu. A
pack is data. It does not download packs. It does not implement KDE's or
GTK's private Wayland menu protocol; an application that only publishes
there is, for now, a candidate for a pack of its shortcuts.
