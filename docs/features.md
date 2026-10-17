# The shell in detail

What each part of inua does. The [README](../README.md) has the short
version; the light apps have [their own page](apps.md).

## Virtual machines

The machines' manager is an app of its own, `inua-vm`
([docs/inua-vm.md](inua-vm.md)): in the launcher, in the drawer's SYSTEM
group, and started from the command line. Its window lists the machines on
the left and shows the chosen one on the right, one page at a time, so it
never offers a control that belongs to another stage: **New** makes a machine
from a template, and one this build has no backend for is stored, listed and
described, with its page saying so; the host checks to fix when something is
missing (**Fix** applies the ones inua can apply itself, through the
session's polkit dialog); a recommended profile, an advanced editor behind
it, and **Install Windows** when nothing is installed yet; progress and the
**web console** while the backend makes the machine and its guest installs
itself, with starting and stopping beside it; and then **Start**, or
**Connect** and **Stop** while the guest runs, with its memory and CPU for
the next start and removal in its own section.

Each machine is one folder under `~/.local/share/inua/vm/NAME/`, described by
its `machine.toml`, with the guest's own files beside it; its runtime files
live in `$XDG_RUNTIME_DIR/inua-vm/NAME/`. The guest password is generated
once and kept in the Secret Service, labelled `inua-vm · NAME`, never in the
Compose project or in your configuration; the Compose environment file that
carries it is written 0600. The image is pinned by version and digest, and
every published port binds to 127.0.0.1, each machine reserving its own, so
two never collide. The window says that the web console on 127.0.0.1:8006 has
no login of its own, and that Windows needs a licence.

Removing a guest deletes the container and the project but keeps the disk
and the password, so a later install reuses that Windows: **Delete disk**,
and **Delete** for the machine and its folder, are separate, confirmed
actions. A group change needs a fresh login, since a running process cannot
change its own group list; the window says so and notifies when it happens.
Docker socket access is as good as root on this machine, so a denied Docker
socket is explained, never fixed with one click: the window names the command
for whoever wants it and points at rootless Docker or Podman instead. The
`kvm` group fix stays, since it grants one device. The host checks include
the Secret Service, since a machine's password lives there.

From a script or a key binding:

```
inua-vm --list              the machines, one name per line
inua-vm --doctor [--json]   what this host provides, and the machines
inua-vm --status [--json]   what each machine is doing, for scripts
inua-vm                     open the manager window
inua-vm --start NAME        start a machine
inua-vm --stop NAME         stop it, keeping its disk
```

`inua vm list|doctor|status` prints the same diagnostics.

## Bar

Workspaces as square marks, the focused window's title, the clock (a click
shows the month under it), the tray, the clipboard history, a bell for
notifications and the status button for the control center. The clock and
the buttons at the end reach up to the screen's top edge and across the
gaps between them, and the status button into the corner, so the pointer
thrown at the edge lands on one. One per monitor.

## Launcher

One search for apps and open windows (Super+Space; Tab switches modes), ranked by
prefix, word starts and subsequences; Enter launches or focuses, Esc
returns focus where it was. It also answers: arithmetic (`2^10 + 3`),
units (`10 km to mi`, `32 f in c`) and currencies (`100 usd to jpy`, ECB
rates) and crypto (`1 eth to usd`, `100 usd to btc`, Coinbase's prices);
Enter copies the answer.

Commands rank with the apps: Lock Screen, Sleep, Restart, Shut Down, Log
Out and Empty Trash (the last four ask for Enter again), Settings,
screenshots and recording, Pick a Colour, Clear Notifications, Focus, Wi-Fi
and Bluetooth, Restart inua; and for the window you came from, Close,
Toggle Floating, Toggle Fullscreen and Center. `workspace 3` goes there,
`move to workspace 3` takes the window along.

## Workspace overview

It needs Hyprland 0.55 or later: the overview switches workspaces, moves
windows and binds its keys through Hyprland's Lua interface (`eval`,
`hl.dsp.*`). The shell asks Hyprland when it connects; on earlier versions
(0.53 answers `eval` with "unknown request") the overview is off, Super+Tab
opens the launcher's windows instead, and none of its key bindings are
installed. Everything that differs between the two versions:
[hyprland-versions.md](hyprland-versions.md).

Tap Win to toggle a full-screen workspace chooser. Win+Tab and Win+Shift+Tab
cycle workspaces; releasing Win commits the selection. It groups Hyprland's current
workspaces by monitor, shows live window thumbnails at their real positions, and lets you move
between workspaces with the arrow keys. Search filters workspace names,
monitor names, app classes and window titles; the MRU button toggles recent
workspace ordering and saves that choice. Empty workspaces are always hidden. Each display has its own independent
workspace order, and its preview shows only that display's workspaces. Drag a window thumbnail to another
workspace to move it, including across monitors. Enter switches to the selected
workspace (or focuses its first window); Escape restores the window that was
focused before opening. Click a workspace or window to switch directly.

Window thumbnails are captured from Hyprland's toplevel-export protocol.
Drag onto an existing or new workspace on either display; floating windows
can also be repositioned inside their workspace. Middle click closes a client.
Ctrl+Shift+X arms addressed-client force termination; Escape/right click cancels.
Arrows, Tab/Shift+Tab and the wheel navigate. Press / or start typing to
filter the overview to windows whose title or app class matches the query. MRU
and per-monitor scope are saved settings. Win+number always follows the focused display's slots; the slots use MRU
order when MRU is on and ascending workspace IDs when it is off. Right-click the workspace bar to open.

Every query ends with a web search (`launcher.search`). A quicklink's
keyword searches its site: `gh inua`, `yt`, `wiki`, `maps`, `tr` … (your
own in `launcher.quicklinks`); the keyword alone fills itself in. A web
address opens as typed. After `:` the launcher finds emoji and symbols by
name and keyword (`:heart`, `:arrow`, `:check`); Enter copies one. With
the assistant on (`[assistant]`), every query ends with "Ask the
assistant", and after `?` the query is a question for it at once
(`?why is the fan loud`).

## Control center

Wi-Fi, Bluetooth, power saver and Focus tiles, and Tailscale's when it is
installed; brightness and volume; media controls; screenshot, lock and
settings; suspend (locking first), restart, shut down and log out, the
last three on a second click.

## Notifications

inua is the notification server. Banners slide in at the top right, then
wait in an iOS-style notification center grouped by app, saved across
restarts. Focus keeps banners quiet. Cleared notifications slide away.
**Keep a history** (Settings → Notifications, off by default) records
the last 500 that came, dismissed ones too, for the assistant, agents and
workshop apps allowed to read them; turning it off clears it.

## Dock

Pinned apps and running ones, with marks that grow and change colour as
apps start and come to the front. Click to open or cycle windows,
right-click to pin or quit; drag to reorder (the others move aside to make
room) or off the pinned ones to unpin; a drawer at the end slides up the
light apps, the Assistant, Settings, the launcher and Screenshot, grouped
by kind; optional autohide. Starting a light app that is open brings
its window to the current workspace.

## Settings

A two-column settings window for everything: appearance and wallpaper, bar
and dock, launcher, default apps, AI (the assistant's models),
notifications, shortcuts, keyboard, mouse and touchpad. Changes apply as
you make them.

Default Apps chooses what opens links (the web browser), mailto: links,
folders (the file manager), audio, video, pictures, PDFs and plain text,
from the installed apps that open them, inua's own among them; the
choice is the desktop's, in `~/.config/mimeapps.list`, so other apps
follow it too. The terminal chosen there is the launcher's terminal and
the first of `~/.config/xdg-terminals.list`; `inua terminal` opens it,
and the sample Hyprland config's Super+Return runs `inua terminal`.

## Shortcuts

Rebind the shell's actions, screenshots and your own commands by pressing
keys. inua writes them to Hyprland so nothing fires twice.

## Lock screen

The time over your wallpaper and your password. inua holds the
compositor's session lock, so if the shell stops the screen stays locked,
and the shell takes it back when it starts again. Passwords are checked by
the system's `unix_chkpwd`; repeated wrong ones wait longer each time.

## Screenshots

Print opens the screenshot tool over a still of every screen: drag out an
area, click a window or a screen. Each shot is saved and copied, then
shown as a notification with its thumbnail. Ctrl+Print does the same and
then opens the drawer, where the recent shots and recordings wait to drag
into other apps (Print alone leaves it closed: most shots go straight to
the clipboard). Shift+Print takes the whole screen at once. With Record on, the choice is recorded instead
(wf-recorder) until the red square in the bar stops it. `inua call
screenshot color` picks a colour from the still with a loupe.

## Passwords

inua is the session's Polkit agent: a program asking for administrator
rights (pkexec, a system setting) gets inua's password dialog, and the
system's helper checks the password. gpg asks for passphrases in the same
dialog through `inua-pinentry`, which `inua init` sets up. The dialog
shows the key the passphrase unlocks, found in gpg's public keyring by the
keygrip gpg-agent gives: its owner and address, whether it is a subkey and
what for, its algorithm, fingerprint and dates. Below it are what it is
for (the pass entry gpg decrypts, or the secret inua-keyring reads) and
who asked and what it runs under: the program running gpg ("pass ← zsh ←
ghostty"), or the app inua-keyring reads a secret for, as the reads list
it ("gh ← claude ← zsh ← ghostty"). The shell keeps none of it.

The eye button in the dialog's corner hides the owner, the address, the
fingerprints and the pass entry behind dots, for a screenshot or a shared
screen; the choice lasts until the shell restarts. They are hidden on
their own while the screen is recorded or shared (Hyprland's screencast
event, or inua's own recording) and while inua's screenshot tool takes a
shot, and shown again afterwards; the eye shows them during that capture
if wanted. A screenshot taken by another program in one frame (`grim`
bound to a key) cannot be seen coming: hide them first.

## Clipboard history

The clipboard button in the top bar (or a shortcut set for "Clipboard
history", or `inua call clipboard toggle|open|close`) opens a searchable
list of the text and images you copied, in a sidebar like the notification
center's. Hover over an entry for its text or image, when it was copied
and where from; click it to paste into the window you were in, or use its buttons to only
copy it, paste an image's file path, or delete it. Escape or a click
outside closes the panel.

The shell runs `wl-paste --watch` (wl-clipboard), which hands each new
selection to `inua clipboard capture`; it keeps the newest
`clipboard.limit` entries (300; 20 to 1000) in
`~/.local/state/inua/clipboard-history.json`, images in
`clipboard-images/` next to it, readable only by you. Text over 1 MiB and
images over 32 MiB are not kept, nor a password manager's copies when it
marks them as sensitive (the `x-kde-passwordManagerHint` type, as
KeePassXC and inua's Pass offer, or `CLIPBOARD_STATE=sensitive`). Deleting, clearing
or falling past the limit removes an image's file too.

Copies from the programs and window classes in
`clipboard.sensitive_sources` (Settings → Clipboard, "Never record copies
from") are not kept either; it lists pass, gopass, passmenu, rofi-pass,
KeePassXC, Bitwarden and 1Password by default. Wayland does not say which
program copied, so the capture looks for itself: a `wl-copy` started in
the last few seconds serves the selection, and the programs in the job
that ran it count, with what they run under. That finds `pass -c`, which
keeps a process there while the password is on the clipboard. The focused
window counts too, by its class and program. An app that copies from its
own window while another is focused, or a `wl-copy` run from a program not
listed, is not recognised.
`clipboard.enable = false` (Settings → Clipboard) records nothing, and
what is on the clipboard when recording starts (the shell starting, or the
switch turned on) is not kept either.

Pasting focuses the window again without moving the pointer and sends it
Ctrl+V, Ctrl+Shift+V in a terminal, by address, so the window under the
pointer cannot take it: with Hyprland's classic dispatchers on 0.53, its
Lua ones on 0.55. Without Hyprland it types the shortcut with `wtype`.
`clipboard.paste_on_click = false` only copies.

## Keyring

`inua-keyring` is a Secret Service (what apps use through libsecret:
browsers' keys, Wi-Fi passwords, the assistant's API keys) that keeps
every secret in pass, under `inua-keyring/` in the password store, so
they are gpg-encrypted and synced like the rest. `inua keyring import`
copies gnome-keyring's items (`-n` lists them first), `inua keyring
enable` lets D-Bus start inua-keyring instead of gnome-keyring, and
`inua keyring status` says who serves now. gpg asks for the passphrase,
through inua's dialog, when a secret is read and gpg-agent has not
cached it. Every secret handed out is logged with the program and PID
that read it and recorded in `~/.local/state/inua/keyring-access.jsonl`
(the newest 200 to 400), with the secret's pass entry and the process as
it was then: its program's path, command line, what it runs under and its
bus name. inua-pinentry records each passphrase it asks for there too:
the program running gpg, the pass entry gpg decrypts (or the key), and
whether the passphrase was entered. gpg-agent keeps the passphrase for a
while (10 minutes by default), so reads of pass made meanwhile, `pass -c`
among them, ask nothing and are not listed. Settings → Keyring lists the
reads and passphrases with their pass entries; clicking one shows all of
that in a side panel. The record is private
(mode 600), but a command line can hold whatever its program was given.
`keyring.notify` (Settings → Keyring, off by default) adds a notification
for each app's first read since inua-keyring started.

After the first read, inua-keyring keeps its secrets in protected memory
(`memfd_secret`, else locked out of swap and core dumps; its process
can't be traced or read by the user's other programs) until it locks:
with inua's lock screen, before suspend, with `inua keyring lock` or
Settings → Keyring, or after `keyring.lock_after` minutes without a
read. So gpg asks for the passphrase once a session, not whenever
gpg-agent forgot it, and the rest of the password store keeps gpg-agent's
short cache.

Each secret has the apps trusted with it. Another app reading it gets a
dialog: Deny, Allow once (for that process), Always allow. The app that
stored a secret is trusted with it. An app is its program (for a script,
the script), known by its SHA-256: a program only root can change (the
file and its folders root's: the package manager's) stays trusted when
updated; any other program is asked about again when it changes. An app
run with `LD_PRELOAD`, `LD_AUDIT` or `LD_LIBRARY_PATH` can only be
allowed once. The list is kept in `~/.local/state/inua/keyring-trust.json`,
sealed with a key kept in pass; changed outside inua-keyring, it is not
trusted until you trust it again in Settings → Keyring. Its Trusted apps
page lists them, with a filter and Remove.

An app that updates itself every day (`~/.local/share/claude/versions/…`)
would be asked about after each update. An allowed path lets any program
at it read every secret, whatever its hash: a pattern, `*` matching
within a folder (`~/.local/share/claude/versions/*`); for a script, the
script's path. Add one under Allowed paths on the Trusted apps page, or
with `inua keyring allow PATTERN`: inua-keyring asks you in its own
dialog first, so another program cannot add one; `inua keyring paths`
lists them, `inua keyring disallow PATTERN` and Remove drop one. A
pattern that lets too much through (`/*`, your home folder itself, less
than two folders before its first wildcard) is refused. The paths are
sealed with the rest of the list; an app run with `LD_PRELOAD` and the
like is still only allowed once. Allow only a folder that only you and
the app's updater write to: any program put there reads everything.

The Pass app shows, under an
entry of inua-keyring's, the apps trusted with it and its last reads. With no inua shell to ask in,
`keyring.untrusted` decides ("allow", the default, or "deny"). This keeps
programs from reading secrets they were never given; a program running
as you can still start a trusted one and take over what it does.

## Workshop

Light apps of your own, in any language: a folder with `app.toml` and a
program that describes its window in inua's building blocks (pages,
columns, sections, rows, entries, buttons, tabs, sliders, charts, tiles,
…) over JSON-RPC on its stdin and stdout; inua draws it in the shell's
look and sends back what the user does. It may notify, copy, open, run
commands or the terminal, and use inua's desk functions (the same the
assistant and agents use: windows, apps, the system, notifications, the
clipboard history, what plays), when its manifest names them, follow
changes on the desktop as they happen, and keeps its data in a store
inua holds. `inua
workshop new DIR` makes one from the template, `dev DIR` runs it again
on every save, `install URL|DIR` adds it to the launcher, `remove ID`
takes it away. Python and JavaScript apps import `inua`, which comes with
inua. A puzzle button beside the dock's drawer, there once an app is
installed, opens the workshop drawer: the apps by the category their
manifest gives, and the Workshop app to open, install and remove them.
See [workshop.md](workshop.md).

## Keyboard and touchpad

Swap Ctrl and Alt, remap Caps Lock, set the touchpad's speed: written to
Hyprland, so they hold even without the shell.

## Themes

Seven colour themes, from 夜 yoru (night) to 青写真 aojashin (blueprint),
Phosphor icons, and matching Fcitx 5 themes.

## Wallpaper

An image placed your way, or the theme's ground with a fine texture.
