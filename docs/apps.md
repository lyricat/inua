# Light apps

Small apps in the shell's look: each is its own window, with its own
application ID, so the launcher, the dock and Hyprland treat it as an app.
They follow inua's theme and style as you change them, and use the shell's
icons.

Open them from the launcher, from the drawer at the end of the dock, or
with `inua-apps NAME` (`inua-apps clock`, `inua-apps tailscale` …). An app
runs once: starting it again brings its window to the current workspace.
Music and Video take files given to a second start (Music adds them,
Video plays the file). The Assistant is a program of its own,
`inua-assistant`, and so are the file manager, `inua-files`
([files.md](files.md)), Viewer, `inua-viewer`, which opens a
window for each file opened, and the machines' manager, `inua-vm`
([virtual machines](#virtual-machines)).

Their data is kept in `~/.local/share/inua`.

## Games

Three small games, each its own app with its own launcher in the dock's
Play section, run by `inua-games` (`inua-games minesweeper`). All three run
offline and draw their boards in the desktop theme. Every window has the
rules behind its ⓘ button, its keys along the bottom, and New game (N).

| Game | Command | What it is |
| --- | --- | --- |
| Minesweeper (扫雷) | `inua-games minesweeper` | 9×9, 16×16 or 30×16. The first square is always safe; right-click flags; click a number with its flags placed to open around it. Best times are kept. |
| Space Pinball (太空弹球) | `inua-games space-pinball` | After the Windows table: fly missions from the mission targets and the launch ramp to climb nine ranks, with fuel to keep up, a hyperspace ramp of five levels, a black hole that locks balls for multiball, a wormhole, a skill shot picked by the plunger's pull, nudging and tilt, an end-of-ball bonus and sound. A display beside the table shows the rank, the mission and its clock, fuel and the latest news. |
| Small Fortune (大富翁) | `inua-games small-fortune` | A property game for two to four players: people taking turns at the screen, people in a browser on the same network, and computer players, on a printed washi board, seen from above and in front and larger than the window: drag it, scroll or use the arrow keys to look about, and Home to find your pawn; a pawn on the move is followed. A ring of 80 squares is laid along a track shaped like the city (the Yamanote Line, Manhattan, Puxi in the Huangpu's bend, Rome's walls), with two branch roads of 16 squares across it or, in New York, over the rivers (at a fork, chance picks the road), and the city's landmarks standing on the map; 72 streets in 24 colour groups, four stations or piers, two utilities (78 properties), Chance, twelve card shops, jail, and a fountain that collects taxes and fines. Before a game, choose who sits in each of the four seats (a hot seat for a person at the screen; Mika, the careful banker, Ren, the bold developer, or Sol, the trickster, computer players each with a way of playing of its own; the AI; or an open seat, for a visitor in a browser or no one), whether others join from a browser, the city (Tokyo, New York, Shanghai or Rome, each with its own names and buildings), the rounds (30 to 120), the starting money ($1,000 to $5,000), and the rules: one die or two, and whether a street may go up a level the turn it is bought. The game menu restarts the game, opens the compendium (every card, the kinds of squares, the rivals and the rules), or goes back to the setup. Every street of yours you pass or land on may go up one level that turn, four houses then a hotel; a whole colour group doubles bare rent and adds half to built rents. Passing Start pays $400. Cards come from Chance and the card shops (three on offer, buy any of them); a hand holds ten, played any number in your turn before the roll or after the move: Loaded Dice (walk the steps you choose), Free Rent, Requisition (buy the rival's property you stand on at its cost), Pickpocket (a card from a rival), Demolition, Building Permit, Turtle (one square a roll, three times), Swap Places, Jail Pass, Tax Audit, Fair Shares and Sleeping Pill (a rival misses a turn). The computer players buy and play cards too. After the last round, or when one player is left, the most worth wins. |

Pinball pauses with P, and on its own when its window is
left; M turns its sound off (it plays through `pw-cat`, silent without it). Games are kept in memory; best scores are kept in
`~/.local/share/inua/games.json`. No accounts or database; only Small
Fortune uses the network: when you open a game to it, and when the AI
plays.
Small Fortune is a property game with its own board and rules, not a
copy of a commercial one.

### The AI in Small Fortune

A seat set to **AI** is played by a profile of Settings → AI that makes
quick choices (its **Decision** ability on): with more than one, a
second drop-down under the seat picks it (first the profile for **Quick
choices with**), and two seats
may play two profiles against each other, each player named for its
profile. Choosing it starts `inua-assistant --answer` and has each
profile chosen pick from a small choice: the seat names the model when
it does, or says why it cannot (the assistant turned off, no key, the
no profile for quick choices, the profile not answering, no answer in
time; the whole error in the
tooltip), and Start waits for it. In the game, at each choice of the
AI's with more than one move, the model picks one of the moves it may
make, told the rules in short and judging the game as it would see it
(its own cards, not the others'), with Mister Morph's Evaluate (the
decision route): it can only pick, not write. A profile that cannot
choose so is asked instead, the moves numbered, if it answers questions. A roll with nothing
else to do is made without asking. The model has no tools. When it does
not answer, or picks no move, the computer finishes that turn; after
three such turns, it plays the seat for the rest of the game. Each move
takes two to four seconds, and a game asks a few hundred times. On the network the host's computer asks,
and the visitors need nothing set up.

### Small Fortune on the network

With **On the network** set before a game, Start opens a lobby instead:
the game listens on port 7341 (another when that one is taken) on every
address of this computer, and the lobby shows the link, with a join code
after `#`, and its QR code. Others on the same network open it on a
phone or a computer, give a name, and join; the lobby lists them as they
come, and **Send away** turns one away. **Start the game** starts it with
those in the lobby, the people at the screen first, and the computer
players asked for, as many as there is room for (four players in all);
whoever joins after watches. The host may play, alone or with others
taking turns at the screen, or only host (none at the screen).

The game lives in the light app; the page (`internal/games/fortunenet`,
three.js 0.170 under `web/vendor`, MIT) shows the host's board print in
3D, with the pawns, buildings and dice, and sends its moves. Each browser
sees its own cards and no one else's. A browser gone (a closed tab, a
phone asleep) has the computer play its seat after 15 seconds, until it
comes back: the page keeps its seat's key for that game. The page's words
are in the host's language. There is no password beyond the code, and
nothing is sent outside the network; to play from elsewhere, use a
network of your own such as Tailscale, whose address the lobby lists
too. A firewall must let port 7341 in.

## Clock

World clocks, alarms, a stopwatch and a timer. The shell rings the alarms,
so they go off with Clock closed.

## Weather

The weather now, for the next 24 hours and the next 7 days, from
[Open-Meteo](https://open-meteo.com); places to switch between.

## Stocks

A watchlist with a chart for each, from Yahoo Finance's unofficial
endpoints (which may change without notice).

## Calculator

A calculator with scientific keys.

## Calendar

The month with week numbers; a chosen day's distance from today, its week
and day of the year. The bar's clock opens a calendar panel too.

## Color Picker

Picks a colour from the screen with a loupe and keeps the colours picked,
in hex, RGB and HSL to copy. `inua call screenshot color` picks one
without opening the app.

## System Monitor

- **Overview:** each core, memory, the GPU, network and disks, with their
  last minute; the battery.
- **Processes:** sorted by CPU, memory or name, filtered by name or user. A
  process opens to its own page: CPU, memory and threads, its command,
  program, folder, open files and disk traffic. It can be asked to quit,
  interrupted, hung up, paused and resumed, or forced to quit, alone, with
  its children, or with every process of the same name; another user's
  processes ask for an administrator's password first.
- **System:** the computer, its processor, memory, graphics and storage.

## Pass

The password store ([pass](https://www.passwordstore.org)) in three
columns: the categories (each entry's first path segment, `work` of
`work/mail/ann`), the chosen category's entries (`mail/ann`), and the
chosen entry. A search above the entries looks through every category.
Each entry has an icon its name calls for (a service's logo, as for
GitHub or Google, or what it is for: wifi, bank, mail, a server, a
token …), else its initial.

- An entry opens decrypted: the password, a one-time code (from an
  `otpauth://totp/` line, with the seconds it stays), its `key: value`
  fields and its notes. The password, and fields whose name says they are
  secret (PIN, token, recovery code …), show as dots, with a button to
  show them; every value has a copy button. A copy is marked as a
  password manager's, so clipboard histories leave it out, and is taken
  off the clipboard after 45 seconds.
- Edit opens the entry's text; Delete asks first. + (beside the search)
  makes a new entry.
- A one-time code is added with the QR-code button on an entry, or
  **One-time code…** in the editor: paste the secret a site shows
  (`JBSW Y3DP EHPK 3PXP`; spaces and lower case are fine) or its
  `otpauth://` link, or read its QR code: **Scan the screen** (the Pass
  windows step aside while each monitor is read), **Select an area**
  (drag it out, with slurp), or **From an image…** (PNG or JPEG). The
  code shows as it would be now; **Add** puts the `otpauth://` line in the
  text, replacing the one there, and Save encrypts it. Google
  Authenticator's export (several accounts in one code) is not read.
- A category (right-click it) or an entry can be
  renamed or moved, in a dialog that takes the new path and previews it:
  its category and name, and every entry that moves. Another name renames
  it; another first part moves it to that category. inua-keyring's folder
  is not renamed or moved, as its index names its entries by path.
- A line of dashes (`---`, as gopass writes) divides; it is not a note.
- At the foot of the categories: the store in use, a menu when there is
  more than one (`$PASSWORD_STORE_DIR`, `~/.password-store`, and folders
  added with the folder button).

Every change goes through `pass`, so git and `.gpg-id` work as on the
command line; decrypting asks for the passphrase in inua's dialog when
gpg-agent has not kept it.

## GPG

Encrypt and decrypt with [GnuPG](https://gnupg.org), through `gpg` and
your keyring, in three tabs, each with its main button in a bar at the
bottom:

- **Encrypt:** a composer. **To** works like a mail client's: type a
  name, address or key ID and pick a match (Enter takes the first), or
  list all keys with the button at the end; the people chosen show as
  chips, and Backspace takes the last one off. Expired or revoked keys
  are listed greyed, with the date, since gpg cannot encrypt to them.
  **From** is your key, with its key ID, algorithm and dates so keys of
  the same name are told apart; with several, it opens a list of them.
  **Sign** (so they know it is from you and unchanged) and **Let me read
  it too** (encrypt to your key as well) are switches, on by default. Then **Text** or **Files** (chosen or dropped): the tab
  shown is what is encrypted. The result takes the composer's place: a
  message comes out armored, to copy or save; a file is written beside it
  as `NAME.gpg`. **Back** returns to the composer as it was.
- **Decrypt:** **Text** or **Files**, as in Encrypt. A message pasted is
  decrypted as soon as it is whole, a block in a longer mail included;
  files chosen or dropped are decrypted at once. When the clipboard holds
  an encrypted message, the tab offers to decrypt it. The result shows
  under the input: who signed it (a good signature, one that does not
  match, a key not in your keyring, or one no longer valid) and who it was
  encrypted to; a message's text, to copy or save, or the files written
  beside the encrypted ones without `.gpg` or `.asc`, each with **Move to
  Trash…**, which asks first.
- **Keys:** your keyring in three sections, **My keys**, **Others** and
  **Invalid keys** (expired, revoked or disabled), each key with its
  fingerprint and dates, and **Copy fingerprint**, **Copy public key** and
  **Encrypt to…**. An expired key of yours has **Extend 2 years**, and no
  Delete: its secret key still opens what was encrypted to it. Another
  person's invalid key says to ask them for an updated key, and has
  **Delete…**, which asks first; messages it signed then show as signed
  by a key not in your keyring.
  **Import…** (in the bar at the bottom) takes a key pasted, from a file, or from the clipboard, and
  shows the keys in it before adding them. **Create key** makes your own
  (name, email, expiry), its passphrase asked in inua's dialog; the form
  also gives `gpg --full-generate-key` for a terminal. With no key of
  your own, the tab offers to create one.

Nothing is overwritten. Decrypted text is copied with the password
manager's hint, so clipboard histories leave it out, and taken off the
clipboard after 45 seconds. Signing, decrypting, creating and extending
ask for your passphrase in inua's dialog when gpg-agent has not kept it.
`inua-apps gpg encrypt|decrypt|keys` opens a tab.

## Tailscale

[Tailscale](https://tailscale.com) through tailscaled's local API:

- **Devices:** the tailnet's, online first, filtered by name, address, OS or
  tag; each with a menu to copy its address or MagicDNS name. A device
  opens to its addresses, how it is reached (direct or relayed), traffic,
  key expiry and tags, with Ping, Use as Exit Node, and a drop zone that
  sends files with Taildrop.
- **Exit node:** none or any device offering itself, and local network
  access while one is in use.
- **This device:** its addresses; Tailscale DNS, subnet routes, refusing
  incoming connections, offering itself as an exit node and Tailscale SSH;
  files received by Taildrop, saved to Downloads; logging out.

A notice says what is in the way and offers the fix: starting the daemon,
logging in, an admin's approval, or (once, with an administrator's
password) letting your user change Tailscale. The control center has a
Tailscale tile to connect and disconnect.

## Assistant

An agent with inua's tools, in a program of its own (`inua-assistant`), on
[Mister Morph](https://github.com/quailyquaily/mistermorph)'s runtime. While
enabled, it starts with the shell and stays running in the background,
whether or not its Console endpoint is enabled. Closing its window hides
it; opening it again returns to the same conversation. Ask
it about this computer ("what is using the CPU?", "what did I miss?",
"which devices on my tailnet are online?") or to do something ("turn on
Focus and set the volume to 30%", "move Firefox to workspace 2"); it can
also search the web and run shell commands. The answer streams in, with a
line for each thing it does; anything that changes the system waits for
Allow, and powering off, ending processes, closing windows, a shell
command and reading a file (an opt-in tool) ask every time. Ask from the launcher (the last row, or `?` before the query)
or with `inua-assistant --ask "…"`; questions go to the open window and
keep its conversation, and + starts a new one.

Set it up in Settings → AI. Each profile is a model: a provider
(OpenAI, Anthropic, Gemini, xAI, DeepSeek, Kimi, OpenRouter, Groq, a
compatible server such as one on this computer, or TypeSafe, which only
makes quick choices), the model, and its key,
typed there (kept in the keyring, the Secret Service: in pass with
inua-keyring, see the shell's features; without one, in
`~/.local/share/inua/keys.json`, readable only by you); never in
`config.toml`. One
profile answers first; others marked Fallback take over, in order, when it
fails, and the window's drop-down switches profile for a conversation.
The profiles are listed one a row (the provider's initials, the name,
provider · model, what it does); a row clicked opens its settings in a
panel beside the page, as in Mister Morph's Console. As in macOS, the
window keeps its size: the panel slides in and the page gives up the
width, in one animation, and takes it back when the panel closes. Only
when the page would get narrower than it can be does the window widen
first, by what is missing, to its right while there is room (so does
the Keyring page's panel). It has **Benchmark**:
it tries the profile as inua uses it, alone, without fallbacks (a short
answer, a tool call through the agent, a quick choice on the decision
route; only the ones its abilities allow), each test running, then
passed or failed with how long it took; a test clicked shows what the
model answered. Each profile's abilities say what it is for, as two chips under its
endpoint: **Chat** (it answers and runs tasks) and **Decision** (it makes
quick choices from a list, as the games' AI players do); a line under
them says what the ones on allow. A profile with only Decision is never
asked a question. The button beside the model lists the provider's
models (its own list, asked with the profile's key), to search and pick
one.
What the tools read is sent to the provider answering.

### Console endpoint

Other Mister Morph Consoles can connect to this desktop as a Runtime API
endpoint. Configure the assistant's model first, then create its access
token in **Settings → AI → Console endpoint** with **Create token**.
Set **Listen address** to `127.0.0.1:8794` for a Console on this computer
and turn on **Serve endpoint**, then use **Copy URL** and **Copy token**
to fill in Console's endpoint settings. **Show token** reads the saved token; **Hide token** clears it
from the field. Closing settings hides it too. Turn off **Serve endpoint**
to stop HTTP serving while keeping the address, token and assistant.

You can also create or read the same token from the command line:

```sh
inua-assistant --serve-token
```

The command prints the existing token, or creates one if missing. It stores
the token in the Secret Service; with inua-keyring, the secret is encrypted
in pass. A Secret Service is required: this token never falls back to
`keys.json` and is never written to `config.toml`. Keep the printed value
for the Console's endpoint settings.

Set the listening address under the existing `[assistant]` section in
`config.toml`:

```toml
[assistant]
serve_enable = true
serve_listen = "127.0.0.1:8794"
```

`serve_enable = false` (the default) disables HTTP serving while the
assistant continues running. The address above accepts connections from this
computer. For a Console on another computer, use the desktop's reachable
address, such as its Tailscale IP, and that address in the endpoint URL.

In the other Console, add an endpoint with:

- **URL:** `http://127.0.0.1:8794/runtime` (replace the address for a remote desktop).
- **Auth token:** the value printed by `inua-assistant --serve-token`.

The API uses `Authorization: Bearer TOKEN`. Console can submit tasks, read
their status and results, continue a topic, and cancel tasks. Local tasks
run while HTTP serving is enabled are also saved for Console to list;
remote topics keep their own history. Changing the local window's model
does not change a remote topic's model selection.

Remote tasks use the same desktop tools and confirmation rules as local
ones. When a tool needs approval, the assistant window appears with a
**Remote Console** card; approval happens on the desktop. Canceling the
task cancels its pending card. File attachments and remote approval of
these desktop cards are not supported by this endpoint.

Settings and saved keys are checked while the assistant runs. A reload
stops the old endpoint and cancels its remote tasks before starting the
replacement. A local answer finishes before settings reload. Missing
keys or listener errors are shown in the assistant window. Closing that
window leaves the process running; disabling the assistant stops its
endpoint. `SIGINT` or `SIGTERM` stops remote tasks and exits the process.

The shell starts it with `inua-assistant --background`; running this command
again reaches the existing process without showing its window. Starting
`inua-assistant` normally, or choosing it in the dock, shows the window.

### MCP: inua's tools for the assistant and for agents

inua serves its tools over MCP: list and focus windows, move them between
workspaces, open apps and the shell's panels, read the system's state,
change Wi-Fi, volume, brightness or Focus, lock the screen, take
screenshots, read, open and dismiss notifications (and their history),
read the clipboard and its history, play or pause what plays, look at
processes and Tailscale. The server is `inua-assistant --mcp` (`inua
mcp` runs it). Workshop apps call the same tools
([workshop.md](workshop.md#inuas-desk-functions)).

**The assistant** uses them through this server, with Mister Morph's MCP
client, which starts it for each question. Its `tool_search` keeps the
tools out of requests until the model finds the ones it needs, so each
request is short; its `codemode` lets the model chain several reads in
one step. What the assistant does still shows in its window, line by
line. Changes ask as **Ask before changes** says, and the riskiest
(closing windows, ending processes, power) always ask, in inua's dialog:
**Deny** or **Allow**, call by call.

**Agents** on this computer (Claude Code, Codex, any MCP client) use the
same server. Turn it on for them in **Settings → AI → MCP** (off by
default; the assistant uses it either way), then connect an agent:

```sh
claude mcp add inua -- inua mcp
```

Any other agent: a stdio MCP server whose command is `inua mcp`.

**What agents may do** sets the policy (`[mcp] policy` in config.toml):
**Look only** offers only the tools that read; **Ask before changes**
(the default) asks in inua's dialog before anything that changes
something; **Change without asking** lets changes run. Whatever the
policy, closing windows, ending processes, power, and reading the
clipboard or the notifications ask. The dialog names the agent and what
it runs under (`claude ← zsh`), and says what the call does ("Closing
Firefox"): **Deny**, **Allow once**, **Allow for this session**. Every
call, the assistant's too, is recorded in
`~/.local/state/inua/mcp-calls.jsonl`. Turning MCP off, or tightening
the policy, takes effect at the next call.

Every tool, its arguments, results and risk: [mcp.md](mcp.md).


## Virtual machines

A program of its own: `inua-vm` manages machines — guests of their own —
each one a folder under `~/.local/share/inua/vm/NAME/` with a `machine.toml`
in it and the guest's own files beside it. Its window lists the machines on
the left and shows the chosen machine's page on the right, one page at a
time: the host checks with their fixes, the recommended profile and
**Install Windows**, the progress of the first installation with the web
console, and then **Start**, **Connect** and **Stop**; memory and CPU for the
next start, and removal, sit in their own section. **New** makes a machine
from one of eight templates: Windows 11 in a container (Docker, Dockur),
Windows 11 from your own ISO with QEMU, Ubuntu, Fedora, Arch, FreeBSD,
Android in Waydroid, and a custom system whose boot section is open field by
field.

A machine's `machine.toml` is read and written by `internal/vm`
(`machine.go`, `store.go`, `templates.go`): its name, its `os` (windows,
linux, freebsd, android or custom), its `backend` (qemu, dockur or
waydroid), its memory, CPUs and disk, and its `[boot]`, `[display]` and
`[seamless]` sections. A folder whose file does not hold is left out of the
window's list and named by `inua-vm --doctor`, which also lists the folders
that are not machines. A machine's runtime files (its sockets, a pid, the
serial console) live in `$XDG_RUNTIME_DIR/inua-vm/NAME/`; its password stays
in the Secret Service, labelled `inua-vm · NAME`. A machine installed by the
previous version is adopted automatically, keeping its disk and its password.
Each machine reserves its own loopback ports for RDP and the web console, so
several machines' ports never collide.

Docker (Dockur) is the only backend this build runs, for Windows; the QEMU
backend (Linux, FreeBSD, a custom system) and the Waydroid backend (Android),
and seamless windows, are the following steps ([inua-vm.md](inua-vm.md)): a
machine of those kinds is stored, listed and described, and its page says
this build has no backend for it yet.

`inua-vm --list` names the machines, one per line; `inua-vm --doctor
[--json]` and `inua-vm --status [--json]` print what this host provides and
what each machine is doing, read-only, for scripts; `inua-vm --start NAME`
and `--stop NAME` run a machine without a window, and
`inua vm list|doctor|status` prints the same diagnostics from the shell.

It needs Docker Engine with Compose v2, KVM (`/dev/kvm`), FreeRDP,
`xdg-open` and `/dev/net/tun`; the window names whichever is missing and the
packages that provide it. The baseline is 4 GiB of memory, 2 CPUs and 64 GiB
of disk, adjusted to the host's resources. For Windows there is no ISO to
prepare: the container fetches Windows' own installation media and installs
it unattended, which is why the first installation takes tens of minutes. The
image is pinned to a version and digest, every port binds to 127.0.0.1, and
the guest password is generated once and kept in the Secret Service — never
in the Compose project or in your configuration. The host checks include the
Secret Service, since a machine's password lives there. Two things the window
says outright: the web console on 127.0.0.1:8006 has no login of its own, and
Windows needs a licence.

Docker socket access is as good as root on this machine, so inua explains it
and never offers to add you to the `docker` group: it shows the command, for
whoever decides they want it, and points at rootless Docker or Podman
instead. The `kvm` group fix, which grants one device, stays. Removing a
guest deletes the container and the project but keeps its disk and its
password, so a later install reuses that Windows; **Delete disk** and
**Delete** (the machine and its folder) are separate, confirmed actions.

## Files

The file manager, a program of its own: `inua-files [PATH …]`. One or
two panes of a folder as a list or a grid, a sidebar of places, drives and
network shares, and the details of the selection; every action has a key,
and typing filters the folder. It serves `org.freedesktop.FileManager1`,
so "Show in folder" opens it; `inua-files -default` makes it the
desktop's file manager in place of Nautilus. See [files.md](files.md).

## Viewer

A program of its own: `inua-viewer [FILE|FOLDER …]` shows images, PDFs,
Markdown and plain text, each start in a window of its own. A file steps
through the files of its kind in its folder (← and →, or the arrows at
the sides), a folder through its images, several files through those
given. It registers for images, PDFs and Markdown; plain text it opens
when asked (Open With, or `inua-viewer notes.txt`). The details (I) say
what the file is, the trash button (Delete) moves it to Trash after a
second press, and F shows it full screen.

- **Images** load beside the shown one before you step to them; zoom and
  pan, rotate, play animations, see a photo's EXIF, copy the image, set
  it as the wallpaper. With `libturbojpeg0` installed it decodes JPEG
  several times faster.
- **PDFs** show their pages one under another, fitting the width, each
  drawn as it comes into view. Page Up and Page Down (or ← and →) go a
  page, Home and End to the ends, Ctrl and the wheel or a pinch zoom, and
  Ctrl+C copies the page's text; Ctrl+Page Up and Ctrl+Page Down step to
  the other PDFs in the folder. The details give the title, author, pages
  and page size. PDFs need `libpoppler-glib8t64`; a PDF locked with a
  password is not opened.
- **Markdown** is laid out to read in a column, with GitHub's tables,
  task lists and strikethrough; links open in the browser, other files
  open in the viewer and `#heading` links scroll. S switches to the
  source. Pictures beside the file are shown; those on the web show as
  links.
- **Plain text** (code, logs, configuration, any UTF-8 file) is shown in
  a fixed-width font. Ctrl+C copies the selection, or all of it.

+, − and 0 (or Ctrl with them) zoom documents and text as they do
pictures.

## Music

A player in Winamp's spirit: three panels, the last two shown or hidden
with **EQ** and **PL**.

- **The main panel**: a display with the time (a click shows the time
  left), the title scrolling when it is long, the bitrate, sample rate
  and STEREO or MONO, and a spectrum analyser with falling peaks; the
  seek bar; previous, play, pause, stop and next, and the eject button to
  open files; volume and balance; shuffle (each track once before any
  repeats) and repeat (off, the playlist, this track).
- **The equalizer**: on or off, a preamp and ten bands from 60 Hz to
  16 kHz (±12 dB), its curve drawn above, and presets (Flat, Rock, Pop,
  Jazz, Classical, Dance, Bass, Treble, Vocal, Laptop speakers).
- **The playlist**: numbers, names (from the tags, else the file's),
  lengths, the total; a click selects, a double click or Return plays;
  Delete removes, Alt+↑/↓ moves; a filter; drop files, folders or
  playlists anywhere on the window, or add them with the buttons below;
  load and save `.m3u`.

Winamp's keys: Z previous, X play, C pause, V stop, B next, ←/→ 5 seconds,
↑/↓ volume, S shuffle, R repeat, Ctrl+O open, Ctrl+L load a playlist.
The playlist, the track and where in it, the volume, the equalizer and
the panels are kept between runs. It plays what GStreamer plays (MP3,
FLAC, Ogg, Opus, AAC, WAV …) and registers for audio files.

## Video

A simple player: the picture fills the window, and nothing else shows
until the mouse moves. Open a video by starting it with the file, by
dropping one on the window, or with the button an empty window shows.

- The controls show over the picture's foot while the mouse moves, and go
  2 seconds after it rests, the cursor with them: play or pause, the
  time, the seek bar, the length (a click shows the time left), the sound
  track and subtitles when there are several or any, the speed, mute and
  volume, full screen.
- A click plays or pauses; a double click goes full screen.
- Keys: Space or K play or pause, ←/→ 5 seconds, J/L 10 seconds, ↑/↓
  volume, M mute, F full screen, Esc leaves it, [ and ] the speed (0.5× to
  2×), S the next subtitles (then none), Ctrl+O open.
- Subtitles in the file, or in a `.srt`, `.ass`, `.ssa`, `.vtt` or `.sub`
  beside it with the same name (or with a language added:
  `film.en.srt`).
- It remembers where each video was left (the last 200) and starts there,
  saying so with a **From the start** button; not near the start or the
  end.
- The screen stays awake while a video plays.

Music and Video play with GStreamer (its plugins: base, good, bad, ugly
and libav; the install script offers them). Both are MPRIS players, so
the control center, media keys and the [desk functions](mcp.md)
(`media_status`, `media_control`) see and drive them.
