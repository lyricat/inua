# The shell in detail

What each part of inua does. The [README](../README.md) has the short
version; the light apps have [their own page](apps.md).

## Bar

Workspaces as square marks, the focused window's title, the clock (a click
shows the month under it), the tray, a bell for notifications and the
status button for the control center (whose hit area reaches the screen
corner). One per monitor.

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
installed.

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
and dock, launcher, AI (the assistant's models), notifications, shortcuts,
keyboard, mouse and touchpad. Changes apply as you make them.

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
shown as a notification with its thumbnail, and the recent shots and
recordings wait in a drawer to drag into other apps. Shift+Print takes the
whole screen at once. With Record on, the choice is recorded instead
(wf-recorder) until the red square in the bar stops it. `inua call
screenshot color` picks a colour from the still with a loupe.

## Passwords

inua is the session's Polkit agent: a program asking for administrator
rights (pkexec, a system setting) gets inua's password dialog, and the
system's helper checks the password. gpg asks for passphrases in the same
dialog through `inua-pinentry`, which `inua init` sets up. The shell keeps
none of it.

## Keyring

`inua-keyring` is a Secret Service (what apps use through libsecret:
browsers' keys, Wi-Fi passwords, the assistant's API keys) that keeps
every secret in pass, under `inua-keyring/` in the password store, so
they are gpg-encrypted and synced like the rest. `inua keyring import`
copies gnome-keyring's items (`-n` lists them first), `inua keyring
enable` lets D-Bus start inua-keyring instead of gnome-keyring, and
`inua keyring status` says who serves now. gpg asks for the passphrase,
through inua's dialog, when a secret is read and gpg-agent has not
cached it. The design: [feat/keyring.md](feat/keyring.md).

## Keyboard and touchpad

Swap Ctrl and Alt, remap Caps Lock, set the touchpad's speed: written to
Hyprland, so they hold even without the shell.

## Themes

Seven colour themes, from 夜 yoru (night) to 青写真 aojashin (blueprint),
Phosphor icons, and matching Fcitx 5 themes.

## Wallpaper

An image placed your way, or the theme's ground with a fine texture.
