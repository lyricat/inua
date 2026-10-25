# The shell in detail

What each part of inua does. The [README](../README.md) has the short
version; the light apps have [their own page](apps.md).

## Bar

Workspaces as square marks, the focused window's title, the clock (a click
shows the month under it), the tray, the clipboard history, a bell for
notifications and the status button for the control center. The clock and
the buttons at the end reach up to the screen's top edge and across the
gaps between them, and the status button into the corner, so the pointer
thrown at the edge lands on one. Scrolling on the status button, its
reach as well, adjusts the volume 5% a notch; on the workspaces, it goes
to the next workspace the bar shows (down) or the previous one (up), a
notch a step, stopping at the ends. One per monitor.

When the volume or the screen's brightness changes, by the keys, the
wheel or `inua system`, a card at the foot of the screen shows the level
for a moment, as phones do. It takes no clicks, and stays away while the
control center is open.

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
and Bluetooth, Wi-Fi Settings, Bluetooth Settings, Restart inua; and for the window you came from, Close,
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

The arrow beside the Wi-Fi tile opens the networks in range under the
tiles: a click joins a saved or open network, and one that needs a
password opens Settings → Wi-Fi to type it. The arrow beside Bluetooth
lists the paired devices, a click connecting or disconnecting one. A
failure shows on the row it is about. The Tailscale tile connects and
disconnects; its arrow opens the Tailscale app, which has this
computer's tailnet address and MagicDNS name.

## Notifications

inua is the notification server. Banners slide in at the top right, then
wait in an iOS-style notification center grouped by app, saved across
restarts. Focus keeps banners quiet. Cleared notifications slide away.
**Keep a history** (Settings → Notifications, off by default) records
the last 500 that came, dismissed ones too, for the assistant, agents and
workshop apps allowed to read them; turning it off clears it.

## On-screen display

A card shows for a moment on the focused monitor when the volume or the
brightness changes, when Wi-Fi or Bluetooth is turned on or off, and when
it joins a Wi-Fi network or a Bluetooth device, wherever the change came
from: the keys, the wheel on the bar's status button, the control center,
`inua call`. It takes no input, and stays away while the control center,
which has the sliders, is open.

`[osd]` in `~/.config/inua/config.toml` turns it off, or moves it:

```toml
[osd]
enable = true
# bottom-center (the default), bottom-left, bottom-right,
# top-center, top-left or top-right
position = "bottom-center"
```

A new position takes effect at once. Without layer shell the card stays
out of the way rather than raising itself over the windows.

## Dictation

Talk, and the words are typed into whichever window has the focus.
Settings → Devices → Dictation (`[voice] enable` in config.toml) turns
it on: a microphone shows in the bar, and the shortcut (Super+Shift+V)
records while it is held (**Hold**), or starts and ends a recording
(**Press**, `push_to_talk = false`); **Change** opens the shortcut in
Settings → Shortcuts. A click on the microphone starts a recording and
another ends it. While it records, Escape drops it (a Hyprland submap
holds Escape and the shortcut until it ends; Super+Ctrl+Alt+Escape
leaves it should inua not). Off, inua neither listens for it nor binds
the keys. `inua call voice start|stop|toggle|cancel` drives it from
scripts.

The page's first line says how dictation is, with its mark: ready
(filled), listening (red), or what is missing and the one thing to do
about it. **Microphone** chooses PipeWire's default input, and **Test**
records a few seconds and says whether it heard anything.

**Runs** chooses what turns the speech into text (`[voice] engine`):

- **On this computer** (the default): the [voxtype](https://voxtype.io)
  daemon; the recording never leaves the machine. When voxtype is not
  installed, **Install** downloads its release for this processor
  (version pinned in inua) from GitHub into `~/.local/bin`, with the
  size said first: the build with every engine (Whisper, SenseVoice,
  Paraformer, Parakeet, Moonshine …), or Whisper's alone on a processor
  without AVX2. It refuses the file unless its SHA-256 is the one inua
  carries, then has voxtype download a model for the language in use
  (SenseVoice for Chinese, Japanese and Korean; Whisper otherwise) and
  set up and start its user service. **Speech models** lists an
  engine's models with their size and languages: Download and use,
  Use, or delete one's files (voxtype 1.1.1's sizes, from its source by
  `scripts/voxtype-models.py`); a download's progress shows in its row.
  A Whisper-only voxtype offers **More engines**, the fuller build in
  its place. **Start** starts a stopped daemon. inua has voxtype hand
  each transcription to it (`output.post_process.command` = `inua call
  voice type -`; a command of the user's own is left alone), and leaves
  voxtype's own hotkey and notification off.
- **With an AI profile** (`engine = "ai"`): the shell records the
  microphone (pw-cat) and `inua-assistant --transcribe` turns it into
  text with the profile chosen for Voice input in Settings → AI, whose
  provider gets the recording.

Either way inua types the words with `wtype`, or, without it, pastes them
(Ctrl+V; Ctrl+Shift+V in a terminal) and puts the clipboard's text back;
neither copy goes into the clipboard history, and the page gives the
command that installs wtype. Each dictation ends with a space (not after
Chinese or Japanese), so the next does not run on.

While it listens the bar's microphone is red and pulsing, and a small
card shows the dictation as it goes (**Card**, `[voice] show`): a red
mark, the time and the waveform, with ✕ to drop it; then the waveform
faded and "Transcribing…"; then the words for a moment, or why there are
none, selectable, until ✕. It sits at the top of the window you type in
(`show = "window"`, the default: not at its foot, where a prompt or a
message box is), or in the screen's bottom right corner (`"corner"`);
`"off"` leaves the bar's microphone alone, and errors go to a
notification. The card never takes the keyboard.

When the engine is not ready, the microphone is slashed and a click opens
Settings → Dictation, which says what is missing.

Dictation is apart from the assistant's own microphone (Voice input,
docs/apps.md), which puts the words into the question.

## Dock

Pinned apps and running ones, with marks that grow and change colour as
apps start and come to the front. Click to open or cycle windows,
right-click to pin or quit; scroll to bring the next running app forward
(down) or the previous one (up), a notch a step, stopping at the ends;
drag to reorder (the others move aside to make
room) or off the pinned ones to unpin; a drawer at the end slides up the
light apps, the Assistant, Settings, the launcher and Screenshot, grouped
by kind; optional autohide. Starting a light app that is open brings
its window to the current workspace.

## Settings

A two-column settings window for everything, its index grouped by what is
most often looked for: Connections (Wi-Fi, Bluetooth), Devices (displays,
keyboard, mouse and touchpad, sound, dictation), Desktop (appearance and wallpaper, the bar
and dock, the launcher, notifications, shortcuts), Apps (default apps, AI
for the assistant's models, clipboard, keyring) and System (power, About).
Changes apply as you make them.

The search above the index (Ctrl+F, or just type) finds pages and the
settings on them by what they say, and pages by other words for them
("network", "trackpad", "neofetch", "system card"); the results replace the index while
it is typed. Enter or a click turns to the result's page, scrolls its
row into view and lights it for a moment. Escape clears the search, and
closes the window when there is none.

About starts with the system manifest, a card of this computer to
share: a slip the machine prints of itself, in the theme's colours. It
lists the unit (the machine), its silicon (CPU, GPU, memory and disk
with their use), the stack from Linux to inua with the packages, the
screens, and the uptime as the total, under a serial (`INUA-0.0.30-7A3F`:
the version and four hex digits of the machine's model). A label at its
foot carries a barcode of the serial and a QR code of this release's
page on GitHub, both real, dark on light, to scan. A stamp says
NOMINAL, or what needs a look (a disk or the memory at 90%, a battery
under 15%), and so does the headline beside it. Nothing of yours is on
it: no user or host name, no paths. It is read from `/proc`, `/sys` and
`/etc` each time the page is shown. *Copy image* puts it on the
clipboard as a PNG (2400×1350), *Copy text* as Markdown, *Save…* writes
the PNG to a file.

`inua fetch` prints the same card in a terminal, as neofetch and its kind
do: inua's mark on the left, the facts on the right, the theme's colours
under them. The mark, the labels and the stamp take the theme's accent and
the values the terminal's own colour, so it reads on a dark terminal or a
light one; `-ground` puts it on the theme's own ground, as on the page,
and `-theme NAME` takes another theme's colours. `-markdown` prints what
*Copy text* copies, `-json` the card's fields.

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

Away from the desk, the web terminal on a phone (`inua-termd serve`,
docs/terminal.md) is asked too: while its page is open, gpg's passphrase
comes up there as well, with gpg's words for the key and who asks, and
the first answer, the phone's or the desktop's, goes to gpg; the other
closes. A new passphrase (asked twice) and inua-keyring's approvals are
asked at the desk only.

ssh, sudo and git ask in the same dialog through `inua-askpass` when they
have no terminal to ask on: a git GUI or an editor pushing over ssh,
Ansible, `sudo -A`. It is their askpass program, named in the session's
environment; the sample `hyprland.conf` sets ssh's:

```
env = SSH_ASKPASS, inua-askpass
env = SUDO_ASKPASS, /usr/local/bin/inua-askpass   # its full path (which inua-askpass): sudo does not search PATH
env = GIT_ASKPASS, inua-askpass                   # HTTPS user names and passwords
```

ssh uses it only without a terminal and with a display (`DISPLAY`, which
XWayland sets); `SSH_ASKPASS_REQUIRE=prefer` asks in the dialog from a
terminal too. A passphrase, a password and a PIN are asked hidden, a git
user name as it is typed (at the desk only); ssh's question whether to
trust a host's key and `ssh-add -c`'s whether to use one are Yes and No;
a security key's "touch it" is a note, closed when ssh is done. Without an
agent, ssh asks again for each connection (ControlPersist keeps one
open; gpg-agent's `enable-ssh-support` asks through `inua-pinentry`).
With no shell running, it runs another askpass (ksshaskpass,
lxqt-openssh-askpass, ssh-askpass).

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


## Passwords

The key in the top bar (`bar.pass`, Settings → Bar "Passwords"; shown
when there is a password store), a shortcut set for "Search passwords",
or `inua call pass toggle|open|close` opens a search through the store
the Pass app uses, in the clipboard history's card. Each word typed is
looked for in the entry's whole path, in any order: `google abc` finds
`corp/google.com/abc@corp.com`. Up and Down choose, Enter or a click opens
an entry: its user (the last part of its path), password, one-time code,
fields and notes, each with a copy button; Enter copies the password. A
copy closes the panel, is marked as a password manager's, and is cleared
after 45 seconds. The panel opens again as it was left: the search and the
entry open; the decrypted entry is forgotten five minutes after it
closes. See docs/feat/pass.md.
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

## Wi-Fi

Settings → Wi-Fi (or Wi-Fi Settings in the launcher) turns Wi-Fi on or
off through NetworkManager and shows the network in use: disconnect or
forget it, or open its details (Details…), a dialog with its security,
signal and band, its IP address, router and DNS servers and the device's
hardware address, each to select and copy. The saved networks follow, in range or not (connect, forget; Forget
asks again), then the others in range, strongest first, with their
security and a lock on those that take a password. Connect asks for the
password under the network's row; a network joined is saved, so it is
joined again by itself, and one that does not come up is not kept. A
password turned down says so and asks again; a saved network takes a
new password the same way. Networks that sign in with a user name or a
certificate (802.1X) are left to `nmtui` or `nm-connection-editor`.

The list is read every few seconds while the page (or the control
center's list) is shown, with a scan every 20 seconds, and not at all
otherwise.

## Bluetooth

Settings → Bluetooth (or Bluetooth Settings in the launcher) turns
Bluetooth on or off and lists the paired devices, each connected or not
with its battery when it reports one: connect, disconnect, or forget it
(Forget asks again). While the page is shown, the computer searches for
devices nearby and is visible to them under its name; Pair pairs one,
trusts it so it connects again by itself, and connects it. The search
and the visibility end when you leave the page.

The shell is BlueZ's pairing agent, so pairing needs no other program:
a code to type on a keyboard is shown digit by digit, lighting as you
type; a phone's number is confirmed; an older device's PIN is asked for.
A device that starts pairing from its side opens the page with its
request. Without inua-shell running, pairing falls back to whatever
agent is there (`bluetoothctl`).

## Displays

Each display Hyprland has, on or off: turn it on or off, choose its
resolution, scale and place beside the others, or mirror another. A
display outside the laptop is set by its description, so its settings
follow it from one port to another; one never set in inua stays as
`hyprland.conf` has it ("Use hyprland.conf" gives one back).

On a laptop, closing the lid while another display is connected turns
the built-in display off, and its windows and workspaces move to the
other; opening the lid turns it on again, so both are in use
(`[displays] lid_off`, on by default). Without another display, the lid
is left to logind (Settings → Power). inua writes the panel off into
`inua.conf` and binds the lid switch there to `inua config apply`, so it
works without the shell running; the shell also applies it when a
display is plugged in or out, and `inua-session` before Hyprland starts,
so logging in with the lid closed keeps the panel off from the first
frame.

## Sound

The output and the input PipeWire has: choose the device sound plays on
and the microphone apps record from (each becomes the default, through
`wpctl set-default`), set their volume or mute them. Test on the output
plays a soft tone on the left, then a higher one on the right; Test on
the input records a few seconds and draws how loud it hears you. The
page follows PipeWire as it changes: a headset plugged in, the volume
keys, another app choosing the default. The microphone is the same one
Settings → Dictation shows.

## Keyboard and touchpad

Swap Ctrl and Alt, remap Caps Lock, set the touchpad's speed: written to
Hyprland, so they hold even without the shell.

## Power

Settings → Power shows the battery and chooses the power mode (Power
saver, Balanced, Performance, as power-profiles-daemon offers them; the
control center's tile is the same).

When idle sets hypridle's four timers: dim the screen, lock, turn the
screens off and suspend, each after a minute to an hour, or never. They
are written to `~/.config/hypr/hypridle.conf` and hypridle
is started again to take them; the file the page first replaces is kept
as `hypridle.conf.pre-inua`, and a time the file has that is not among
the choices (2.5 minutes) is shown and kept. Without hypridle installed
the page says so, since nothing then happens when the computer is idle.

Lid and power button chooses what closing the lid on battery, closing
it on mains power and pressing the power button do: suspend, hibernate
(where the computer can), lock the screen, power off or nothing. They
are logind's settings, written to
`/etc/systemd/logind.conf.d/50-inua.conf` after asking for an
administrator's password (pkexec), and logind is reloaded to take them.
With another display connected, closing the lid still turns only the
built-in display off (`[displays] lid_off`).

## Themes

Seven colour themes, from 夜 yoru (night) to 青写真 aojashin (blueprint),
Phosphor icons, and matching Fcitx 5 themes.

## Wallpaper

An image placed your way, the theme's ground with a fine texture, or, with
*inua draws the wallpaper* (`background.generate`, the default), a picture
inua draws for your theme, another each day; the shuffle button beside it
draws another for the rest of the day.

The pictures are made by code, not shipped as files (`internal/wallgen`):
scenes in their own colours (a sunset over the sea, the cosmos, a cat
watching the moon), at dusk under a dark theme and in daylight under a
light one, and patterns in the theme's (contours, dunes, aurora, dots),
taking turns day by day, each time round in another variation. A picture
is drawn once for the largest monitor, off the main thread, and kept in
`~/.cache/inua/wallpapers` under a name that says what it shows; the next
day, or a change of theme or monitor, draws it again, and the texture shows
meanwhile.
