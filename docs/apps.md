# Apps

inua's apps are windows of their own, each with its own application ID, so
the launcher, the dock and Hyprland treat them as any other app. They take
the shell's theme, style and icons, and follow them as you change them.

Open them from the launcher, from the drawer at the end of the dock, or
from the command line. An app runs once: starting it again brings its
window to the current workspace. Their data is kept in
`~/.local/share/inua`.

| App | Command | |
| --- | --- | --- |
| Files | `inua-files` | [files.md](files.md) |
| Terminal | `inua-term` | [terminal.md](terminal.md) |
| Mail | `inua-mail` | [mail.md](mail.md) |
| Assistant | `inua-assistant` | [below](#assistant) |
| Viewer | `inua-viewer` | [below](#viewer) |
| Virtual machines | `inua-vm` | [below](#virtual-machines) |
| Remote Desktop | `inua-remote` | [remote-desktop.md](remote-desktop.md) |
| Mobile Link | `inua-connect` | [mobile-link.md](mobile-link.md) |
| Disk Usage | `inua-disk-usage` | [disk-usage.md](disk-usage.md) |
| Light apps | `inua-apps NAME` | [below](#light-apps) |
| Games | `inua-games NAME` | [below](#games) |
| Your own | `inua workshop` | [workshop.md](workshop.md) |

## Files, Terminal, Mail

Each has its own page; in short:

- **Files** — one or two panes, as a list or a grid; a sidebar of places,
  drives and remote disks (SFTP, SMB, WebDAV, FTP, S3 and R2, mounted by
  rclone); every action has a key, and typing filters the folder. It
  serves `org.freedesktop.FileManager1`, so "Show in folder" opens it;
  `inua-files -default` makes it the desktop's file manager.
- **Terminal** — workspaces of terminals for running many agents at once,
  with each agent's state in view and sessions that come back after a
  reboot. `inua-termd serve` opens it to a phone over Tailscale.
- **Mail** — every account in three columns: mailboxes, conversations,
  the conversation. Gmail and Outlook sign in through the browser (OAuth),
  others with IMAP and a password. New mail arrives over IDLE; HTML shows
  as text without remote images; each message is checked for phishing.
  The assistant summarises, suggests replies, drafts and searches, when
  asked.

## Assistant

An agent with inua's tools, on
[Mister Morph](https://github.com/quailyquaily/mistermorph)'s runtime.
While enabled it starts with the shell and stays running; closing its
window hides it, and opening it again returns to the same conversation.

Ask it about this computer ("what is using the CPU?", "what did I miss?")
or to do something ("turn on Focus and set the volume to 30%", "move
Firefox to workspace 2"). It can also search the web and run shell
commands. The answer streams in with a line for each thing it does.
Anything that changes the system waits for **Allow**; powering off, ending
processes, closing windows, shell commands and reading files ask every
time.

With **As administrator** (Settings → AI, under Run commands;
`elevated_commands` in `[assistant]`, on by default), a shell command
may run as root: Mister Morph runs it with `sudo -k -A`, and sudo's
password is asked by `inua-askpass` in inua's dialog, which shows the
exact command (the system's pinentry asks when `inua-askpass` is not
installed). The password goes to sudo alone, never to the model.
Declined, or unanswered within two minutes, the command does not run.

Ask from the launcher (`?` before the query, or its last row) or with
`inua-assistant --ask "…"`. Questions go to the open window and keep its
conversation; + starts a new one.

### Profiles

Set it up in Settings → AI. Each profile is a model: a provider (OpenAI,
Anthropic, Gemini, xAI, DeepSeek, Kimi, OpenRouter, Groq, a compatible
server such as a local one, or TypeSafe for quick choices only), the
model, and its key. Keys go to the Secret Service (in pass, with
inua-keyring), or without one to `~/.local/share/inua/keys.json`, readable
only by you; never to `config.toml`.

One profile answers first; those marked **Fallback** take over in order
when it fails. The window's drop-down switches profile for a
conversation. Each profile has three abilities: **Chat** (answers and
runs tasks), **Decision** (picks from a list, as the games' AI players
do) and **Voice** (turns speech into text; OpenAI, Groq or a compatible
server, with a speech-to-text model such as `gpt-4o-mini-transcribe` or
`whisper-large-v3-turbo`).
**Benchmark** tries a profile alone, test by test, and shows what the
model answered.

What the tools read is sent to the provider answering.

### Voice input

With a Voice profile (**Voice input with**), the microphone beside
**Ask** records what you say (through `pw-cat`, PipeWire's) until you
click it again, then puts the words in the question to read over and
send. ✕ or Escape drops the recording; it stops by itself after five
minutes. The recording goes to that profile's provider, and only after
you click the microphone. A connected Console's microphone uses the same
profile. Typing by voice into any other window is Dictation (Settings →
Dictation, through voxtype; see docs/features.md).

### Console endpoint

Other Mister Morph Consoles can connect to this desktop as a Runtime API
endpoint.

1. Configure the assistant's model.
2. In **Settings → AI → Console endpoint**, **Create token**.
3. Set **Listen address** (`127.0.0.1:8794` for a Console on this
   computer, or the desktop's Tailscale address for another) and turn on
   **Serve endpoint**.
4. In the other Console, add an endpoint with **Copy URL**
   (`http://ADDRESS/runtime`) and **Copy token**.

`inua-assistant --serve-token` prints the token, creating it if missing.
It lives only in the Secret Service, never in `keys.json` or
`config.toml`. The same settings in `config.toml`:

```toml
[assistant]
serve_enable = true
serve_listen = "127.0.0.1:8794"
```

The API takes `Authorization: Bearer TOKEN`. A Console can submit tasks,
read their status and results, continue a topic and cancel. Remote tasks
use the same tools and approvals as local ones: a **Remote Console** card
appears in the assistant's window, and approval happens on the desktop.
File attachments are not supported.

Turning **Serve endpoint** off stops serving and keeps the assistant
running. A settings reload stops the old endpoint and its remote tasks
first. The shell starts the assistant with `--background`; running plain
`inua-assistant`, or picking it in the dock, shows the window.

### MCP: inua's tools for the assistant and for agents

inua serves its tools over MCP: windows and workspaces, apps and the
shell's panels, the system's state, Wi-Fi, volume, brightness and Focus,
the lock screen, screenshots, notifications and their history, the
clipboard, media, processes and Tailscale. The server is `inua mcp`
(`inua-assistant --mcp`). Workshop apps call the same tools
([workshop.md](workshop.md#inuas-desk-functions)).

The assistant always uses it. For agents on this computer (Claude Code,
Codex, any MCP client), turn it on in **Settings → AI → MCP**, then:

```sh
claude mcp add inua -- inua mcp
```

**What agents may do** (`[mcp] policy`) is **Look only**, **Ask before
changes** (the default) or **Change without asking**. Whatever the policy,
closing windows, ending processes, power, and reading the clipboard or
notifications ask. The dialog names the agent and what it runs under
(`claude ← zsh`) and what the call does: **Deny**, **Allow once**,
**Allow for this session**. Every call is recorded in
`~/.local/state/inua/mcp-calls.jsonl`. Changes to the setting apply at
the next call.

Every tool, its arguments and its risk: [mcp.md](mcp.md).

## Viewer

`inua-viewer [FILE|FOLDER …]` shows images, PDFs, Markdown and plain
text, a window for each start. A file steps through the files of its kind
in its folder (← and →), a folder through its images. It registers for
images, PDFs and Markdown; plain text it opens when asked.

| | |
| --- | --- |
| **Images** | Zoom and pan, rotate, animations, EXIF, copy, set as wallpaper. `libturbojpeg0` makes JPEG faster. |
| **PDFs** | Pages one under another, fitting the width; Page Up/Down, Home/End, Ctrl+wheel or pinch to zoom, Ctrl+C copies the page's text. Needs `libpoppler-glib8t64`; locked PDFs are not opened. |
| **Markdown** | Laid out to read, with GitHub's tables and task lists; links open, `#heading` links scroll; S shows the source. |
| **Plain text** | Fixed-width; Ctrl+C copies the selection, or everything. |

I shows the details, F full screen, +/−/0 zoom, Delete moves the file to
the Trash after a second press.

## Virtual machines

`inua-vm` keeps machines of your own, each a folder under
`~/.local/share/inua/vm/NAME/` with a `machine.toml` beside the guest's
files. The window lists the machines on the left and shows the chosen
one's page on the right: the host checks and their fixes, the first
installation with its console, then **Start**, **Connect** and **Stop**.

**New** starts from one of eight templates: Windows 11 in a container
(Dockur), Windows 11 from your own ISO, Ubuntu, Fedora, Arch, FreeBSD,
Android, and a custom system with every boot field open.

- **QEMU** runs Linux, FreeBSD and custom systems from an ISO (chosen, or
  downloaded from the distributions' list), a disk or a kernel, on this
  architecture or another, and can hold a guest at its first instruction
  for gdb.
- **Docker (Dockur)** runs Windows. There is no ISO to prepare: the
  container fetches Windows' media and installs it unattended, which
  takes tens of minutes.
- **Waydroid** (Android) is still to come: an Android machine is stored
  and listed, and its page says so.

It needs KVM, Docker Engine with Compose v2, FreeRDP, `xdg-open` and
`/dev/net/tun`; the window names whatever is missing and its package.

Safety:

- Each machine's password is generated once and kept in the Secret
  Service (`inua-vm · NAME`), never in the project or your configuration.
- Every port binds to 127.0.0.1, each machine with its own.
- The Dockur image is pinned by version and digest.
- The web console (127.0.0.1:8006) has no login of its own, and Windows
  needs a licence; the window says both.
- Docker socket access is as good as root, so inua never offers to add
  you to the `docker` group; it shows the command and points at rootless
  Docker or Podman. The `kvm` group fix stays.
- **Remove** keeps the disk; **Delete disk** and **Delete** ask first.

For scripts: `inua-vm --list`, `--doctor [--json]`, `--status [--json]`,
`--start NAME`, `--stop NAME`; `inua vm list|doctor|status` from the
shell's command line.

## Remote Desktop, Mobile Link, Disk Usage

- **Remote Desktop** — shares this desktop through Sunshine and shows
  other machines' desktops here over Moonlight's protocol; found on the
  local network or the tailnet, paired once with a PIN, decoded by the
  graphics card. [remote-desktop.md](remote-desktop.md)
- **Mobile Link** — pairs with KDE Connect, GSConnect and Valent devices:
  the clipboard both ways, files and links, the phone's notifications and
  battery, media control, the phone as touchpad and keyboard, and
  commands. A service of its own runs from login while it is on.
  [mobile-link.md](mobile-link.md)
- **Disk Usage** — maps a disk as rings of folders, knows what developers
  and agents leave behind (dependencies, build output, caches,
  conversations, worktrees, models), says whether each can go, and
  removes it. [disk-usage.md](disk-usage.md)

## Light apps

Small apps run by `inua-apps NAME` (`inua-apps clock`).

### Clock

World clocks, alarms, a stopwatch and a timer. The shell rings the
alarms, so they go off with Clock closed.

### Weather

Now, the next 24 hours and the next 7 days, from
[Open-Meteo](https://open-meteo.com), for the places you add.

### Stocks

A watchlist with a chart for each, from Yahoo Finance's unofficial
endpoints, which may change without notice.

### Calculator

A calculator with scientific keys.

### Calendar

The month with week numbers; a day's distance from today, its week and
day of the year. The bar's clock opens a calendar too.

### Color Picker

Picks a colour from the screen with a loupe and keeps those picked, in
hex, RGB and HSL. `inua call screenshot color` picks one without the app.

### System Monitor

- **Overview:** each core, memory, the GPU, network, disks and the
  battery, with their last minute.
- **Processes:** sorted and filtered; each opens to its CPU, memory,
  threads, command, open files and disk traffic, and can be paused,
  resumed, asked to quit or ended, alone or with its children. Another
  user's processes ask for an administrator's password.
- **System:** the computer, processor, memory, graphics and storage.

### Pass

The password store ([pass](https://www.passwordstore.org)) in three
columns: categories (the first part of each path), their entries, and the
entry. Search looks through every category.

- An entry shows its password, one-time code, `key: value` fields and
  notes. Secrets show as dots until asked; every value has a copy button.
  Copies are hidden from clipboard histories and cleared after 45 seconds.
- **+** makes an entry; Edit and Delete act on the one open; entries and
  categories can be renamed or moved, with a preview of what moves.
- A one-time code comes from a pasted secret or `otpauth://` link, or a
  QR code read from the screen, an area of it, or an image.
- With more than one store, a menu at the foot of the categories switches.

Every change goes through `pass`, so git and `.gpg-id` work as on the
command line.

### GPG

Encrypt and decrypt with [GnuPG](https://gnupg.org), in three tabs
(`inua-apps gpg encrypt|decrypt|keys` opens one):

- **Encrypt:** a composer. **To** finds keys as a mail client finds
  addresses; **From** is your key; **Sign** and **Let me read it too** are
  on by default. Text comes out armored to copy or save; files are written
  beside the original as `NAME.gpg`.
- **Decrypt:** pasted text is decrypted as soon as it is whole, files at
  once, and an encrypted message on the clipboard is offered. The result
  says who signed it and to whom it was encrypted.
- **Keys:** yours, others' and invalid ones, with copy, encrypt-to,
  import, and create; an expired key of yours can be extended two years.

Nothing is overwritten. Decrypted text is copied as a password manager's
is. Passphrases are asked in inua's dialog when gpg-agent has none.

### Tailscale

[Tailscale](https://tailscale.com) through tailscaled's local API:

- **Devices:** the tailnet's, online first, filtered; each with its
  addresses, connection, traffic, key expiry and tags, Ping, Use as Exit
  Node, and a drop zone for Taildrop.
- **Exit node:** none or any offering device, and local network access.
- **This device:** its addresses and options (DNS, routes, exit node,
  SSH), files received, logging out.

When something is in the way (the daemon, logging in, an approval,
permission), a notice says so and offers the fix.

### Music

A player in Winamp's spirit, in three panels: the player (display,
spectrum, seek, transport, volume and balance, shuffle and repeat), a
ten-band equalizer with presets (**EQ**), and the playlist (**PL**), which
takes dropped files and folders and reads and writes `.m3u`.

Winamp's keys: Z X C V B, ←/→ seek, ↑/↓ volume, S shuffle, R repeat,
Ctrl+O open, Ctrl+L load a playlist. Everything is kept between runs.

### Video

The picture fills the window; the controls show while the mouse moves.

- A click plays or pauses; a double click goes full screen.
- Space or K, ←/→ 5 seconds, J/L 10 seconds, ↑/↓ volume, M mute,
  F full screen, [ and ] speed, S subtitles, Ctrl+O open.
- Subtitles from the file, or a `.srt`, `.ass`, `.vtt` … beside it
  (`film.srt`, `film.en.srt`).
- It starts where each video was left, with a **From the start** button.
- The screen stays awake while a video plays.

Music and Video play what GStreamer plays (its base, good, bad, ugly and
libav plugins; the install script offers them). Both are MPRIS players,
so the control center and media keys drive them. A second start with
files adds them (Music) or plays them (Video).

## Games

Three offline games, each with its own launcher in the dock's Play
section, run by `inua-games NAME`. Every window has its rules behind ⓘ,
its keys along the bottom, and New game (N). Best scores are kept in
`~/.local/share/inua/games.json`.

| | |
| --- | --- |
| **Minesweeper** (`minesweeper`) | 9×9, 16×16 or 30×16. The first square is always safe; right-click flags; click a number to open around it. |
| **Space Pinball** (`space-pinball`) | After the Windows table: missions and ranks, a hyperspace ramp, multiball, a skill shot, nudge and tilt. P pauses, M mutes (sound through `pw-cat`). |
| **Small Fortune** (`small-fortune`) | A property game for two to four: people at the screen, people in a browser, and computer players. |

### Small Fortune

A printed washi board of 80 squares laid along a city — Tokyo's Yamanote
Line, Manhattan, Shanghai's Puxi, Rome's walls — with two branch roads and
the city's landmarks. Drag, scroll or use the arrow keys to look about;
Home finds your pawn.

Before a game, choose each seat (a person at the screen; Mika, Ren or
Sol, computer players of different temperaments; the AI; or an open seat
for a browser), the city, the rounds (30 to 120), the starting money and
the rules (one die or two; building the turn you buy).

- A street of yours you pass or land on may go up a level: four houses,
  then a hotel. A whole colour group doubles bare rent.
- Passing Start pays $400; a fountain collects taxes and fines.
- Cards come from Chance and the card shops; a hand holds ten, played
  before the roll or after the move: Loaded Dice, Free Rent, Requisition,
  Pickpocket, Demolition, Building Permit, Turtle, Swap Places, Jail Pass,
  Tax Audit, Fair Shares, Sleeping Pill.
- After the last round, or when one player is left, the most worth wins.

The game menu has the compendium: every card, square, rival and rule.
Small Fortune has its own board and rules; it is not a copy of a
commercial game.

**The AI.** A seat set to **AI** is played by a profile of Settings → AI
with **Decision** on; two seats can play two profiles against each other.
At each choice the model picks one of the legal moves, knowing only what
its player would see; it has no tools and cannot write. When it does not
answer, the computer plays that turn, and after three such turns the rest
of the game. Each move takes two to four seconds.

**On the network.** With **On the network** set, Start opens a lobby: the
game listens on port 7341 and shows a link with a join code and its QR
code. Others on the network open it, give a name and join; **Send away**
turns one away, and **Start the game** fills the rest with computer
players. Each browser sees only its own cards; a browser gone for 15
seconds has the computer play its seat until it returns. There is no
password beyond the code, and nothing leaves the network; to play from
elsewhere, use a network such as Tailscale. A firewall must let port 7341
in.

Games are kept in memory; the network is used only for a game opened to
it, and when the AI plays.
