# Terminal: inua-term

`inua-term` is inua's terminal, made for running many agents at once and
knowing at a glance which of them needs you. Its window has three
columns: the workspaces (a folder each), the shown workspace's sessions
with their panes, and the shown session's terminals, tiled.

```
inua-term                        open the window (the last workspace shown)
inua-term --workspace DIR        open or show the workspace for a folder
inua-term -e COMMAND [ARGS…]     run a command in a session of its own ("Commands")
inua-term --quick [-e COMMAND …] a terminal for a moment (below)
inua-term agents [--json]        the agents in every pane
inua-term report …               an agent telling inua-term what it does (below)
inua-term integration remove claude|codex|all
                                 take out the hooks an earlier inua-term added
inua-termd                       the terminal host (below; the window starts it)
inua-termd serve [--tailscale]   the terminals in a phone's browser (below)
```

It is in the app drawer, under Tools. It can be the desktop's terminal:
Settings → Default Apps writes `xdg-terminals.list`, and its desktop
entry takes `-e` as xdg-terminal-exec expects.

**Quick mode.** `inua-term --quick` (Quick Terminal in the drawer) is a
terminal for a moment: one window, your shell in the folder it was
started from, and nothing else: no columns, no workspace or session, no
host, nothing remembered. The shell ending closes the window, and the
window closing ends the shell. With `-e`, it runs the command in place of
the shell. Each start is a window of its own. Its keys are the pane's
alone: copy, paste, search, copy mode and the font's size; every other
key goes to the program.

## The first start

With no `term.toml` yet, a guide comes first, before the first pane
starts. Three steps, each a list beside a preview:

- **Your shell**: the login shell (`$SHELL`), or another of those
  installed (`/etc/shells`, one path each, nologin left out). The
  preview shows its prompt.
- **Terminal colours**: the window theme's, or a scheme of its own
  (Tokyo Night, Catppuccin Mocha and Latte, Dracula, Gruvbox Dark and
  Light, Nord, One Dark, Rosé Pine, Solarized Dark and Light), each with
  a line on its look; the preview is a few commands in it and its 16
  colours.
- **Window theme**: inua's, or one of inua's others for this window
  alone, whatever inua's is. The window takes it at once.

Skip, Escape or closing the guide keeps what was chosen so far; every
choice is in the settings after.

## How it works

The terminals belong to a **host**, `inua-termd`, which the window
starts when none runs and which ends with its last pane. It is a program
of its own, with no GTK and no desktop needed: on a computer without
one, `inua-termd serve` still shows its terminals to a phone. Closing the
window, or the window crashing, leaves every program running; opening
it again finds them, their recent output replayed. The window is a client
of the host over `$XDG_RUNTIME_DIR/inua/term.sock` (this user only); the
host keeps the workspaces, sessions and layouts, so every window shows
the same.

A pane has one size, and the screen that looked at it last gives it:
the window gives its tiles' sizes; a phone opening a pane (see *On a
phone*) fits it to the phone, and leaving it, or going, gives the pane
the size the window asked for again. A client attaching to a pane gets
the host's snapshot of it at its size, rewrapped as the host's engine
keeps it, or the pane's recent output as it came.

The terminal engine is [libghostty-vt](https://github.com/ghostty-org/ghostty),
Ghostty's terminal core, as a library: the screen, the scrollback, the
keyboard (with the kitty keyboard protocol Claude Code and Codex use to
tell Shift+Enter from Enter), the mouse, paste, search and the shell's
prompt marks. inua loads it with purego, so inua-term builds without cgo.
It is `libinua-vt.so`, libghostty-vt at a pinned commit linked with
inua's small shim (`internal/term/vt/shim`): releases have it in
`lib/inua/`; from source, `make term-engine` builds it (it needs git and
Zig 0.16). inua-term looks for it beside the program, in `../lib/inua`
from it, or at `$INUA_VT_LIB`.

## The window

- **Workspaces** (column 1): at its top, the search, then the new
  workspace button and the menu button. Its menu has the command
  palette (Ctrl+Shift+P) and the settings; the Web Terminal, what it is
  doing and a switch for it; and restarting the terminal host. Each
  workspace is a name, its folder, and what its agents add up to: a
  square filled in warn while one waits for you, in the accent while one
  works, hollow when quiet. Drag a workspace to put it in another place;
  its menu (a right click, or the Menu key) renames it or closes it,
  after asking, since every program in it quits.
  The search (its button in the head) finds workspaces by name, folder
  or pane title. The framed button at the head's start hides the
  column, sliding away (Ctrl+Shift+B); one at the top of column 2 brings
  it back. At its
  foot, as Warcraft's idle worker, the finder counts the agents waiting
  for you (a square in warn) and those done or idle (hollow); a click on
  it, or Ctrl+`, goes to the next: waiting, then done, then idle.
- **Sessions** (column 2): the shown workspace's sessions, each a section of its
  panes under a head with the same square and a badge counting the
  agents that wait. A pane's row shows its agent's mark and title, its state and
  how long it has been in it; an agent waiting for you shows what it
  asks. A shell shows the command running in it. An agent's mark takes
  its own colour (Claude Code's orange, Gemini's blue…) while it runs.
  After the last session, *New session* starts one.
- **Terminals** (column 3): the shown session's panes, tiled, each with a
  head (state, mark, title, folder). In a git checkout, the folder's
  place shows its name and branch (a linked worktree's with a fork for
  a mark), read from the checkout's files when the shell changes folder
  and at each prompt; a narrow pane shows the branch alone, or neither.
  Drag a gap to resize; drag a head
  onto another pane to swap them, or onto a session's head or a pane's
  row in column 2 to move the pane to that session, beside its focused
  pane or that one. When the layout changes, the panes slide to their
  new places, the programs resized once they are there. A pane closes
  when its program ends (`exit`, or the command of `inua-term -e` done),
  the others taking its room; a session closes with its last pane, a
  workspace with its last session, and the window with the last of all.

Drag a column's right edge to make it wider or narrower (column 1 from
180 to 420 px, column 2 from 220 to 560); the next window opens with the
same widths. A window narrower than 1040 px folds column 2, narrower than 720 px
column 1 as well; Ctrl+Shift+S and Ctrl+Shift+B show them anyway.

## Agents

inua-term knows Claude Code, Codex, Gemini CLI, opencode, pi, Mister
Morph, Aider, Amp, Cursor Agent, Copilot CLI, Goose and Qwen Code by
their process, and what they are doing (working, waiting for you, done,
idle) from what they report, from what they show at the bottom of their
screen, and from the notifications they send through the terminal. A
manifest of each says how; yours in `~/.config/inua/term/agents/*.toml`
add agents or replace the built-in ones:

```toml
type = "claude"
name = "Claude Code"
icon = "agent-claude"            # ui/icons/agent-claude-symbolic.svg
color = "#d97757"                # its mark's colour; none for the theme's
process = ["claude"]
resume = "claude --continue"     # what a restored pane puts back

[[state]]
state = "blocked"
lines = 12                       # the last twelve rows with text
any = ["Do you want to proceed?", "❯ 1. Yes"]

[[state]]
state = "working"
lines = 40                       # its spinner may be far up, over its todos
any = ["esc to interrupt"]
match = ['^[·✢✳✶✻✽*] \S+… \(\d']   # regular expressions, for a row with no fixed text
```

A wrapper can name its agent with `INUA_TERM_AGENT=<type>`.

**Nothing added to the agents.** inua-term writes no hooks, skills or
settings of the agents': it knows them by their process and their
screen alone. An earlier inua-term added hooks to Claude Code's
`~/.claude/settings.json` and Codex's `~/.codex/config.toml`;
`inua-term integration remove all` takes them out (the file before is
kept as `.bak`). A program of your own may still report through
`inua-term report`, which does nothing outside an inua-term pane. The
host takes a report only from a process running in the pane, its
terminal the pane's: Codex runs its `notify` from a daemon that left the
pane it started in, and those reports once landed on that pane whatever
it ran.

```
inua-term report --agent mybot --state working --title "Rename the flag"
inua-term report --state blocked --summary "Allow rm -rf build?"
inua-term report --state done
```

When an agent waits for you or finishes in a pane you are not looking at,
a notification says so; its *Show* brings the window to that pane.
Ctrl+Shift+J goes to the next agent that needs you, in any workspace;
Ctrl+` (the finder) to the next that needs you or is idle;
Ctrl+Shift+A lists every agent.

## What it remembers

While the host runs nothing needs remembering. When it ends (a reboot, a
logout, or "Restart the terminal host" in Ctrl+Shift+P, which asks first
and opens the new host in the same window, as for a new build), it
keeps the sessions you used (typed in, or ran something in),
their layouts, and each pane's folder, title and **last command**: what
was typed at the prompt and not run, else the command running, else the
one run last. The next host starts a shell in each pane's folder, in the
same layout, and puts the command back at the prompt **not run**; the
pane says RESTORED until you type. An agent with a resume command comes
back as it (`claude --continue`). A pane whose folder is gone starts in
its workspace's; a session with neither there yet (a network share or a
disk mounted later) waits for it, still kept, and comes back within a
few seconds of the folder appearing. A session that fails to start is
left out whole, its panes started closed again. Commands matching the `forget`
patterns (`*password*`, `*token=*`, `*secret*`) are never kept, nor is
anything typed at a program's own prompt (a password for sudo or ssh).
The file is `~/.local/state/inua/term.json` (mode 0600).

This uses the shell's prompt marks (OSC 133). inua-term injects small
scripts that send them for bash (`--rcfile`, which then loads your
`~/.bashrc`), zsh (`ZDOTDIR`, handing back to yours) and fish
(`XDG_DATA_DIRS`); `integration = false` in `term.toml` turns that off.

## Keys

The keys are Ghostty's on Linux, a workspace standing for Ghostty's
window, a session for its tab and a pane for its split; inua-term's own
actions are Ctrl+Shift chords. Everything else goes to the program.
Ctrl+Shift+/ shows them all, Ctrl+Shift+P finds any action by name, and
the settings change them. They work in a workspace without panes too. A `term.toml` of an older version has the keys it
kept at that version's defaults moved to these: the first version's to
Ghostty's, and the second's Ctrl+Shift+T and Ctrl+Shift+N (new session,
new workspace) to Ctrl+Shift+N for a new session.

| Keys | Action |
|---|---|
| Ctrl+Shift+N | New session (a new workspace: its + button, or the palette) |
| Ctrl+Shift+O / Ctrl+Shift+E | Split right / down |
| Ctrl+Shift+W | Close the pane (asks if its agent is working) |
| Ctrl+Alt+←↑→↓ | Focus the pane in that direction |
| Ctrl+Shift+Super+←↑→↓ | Resize the focused pane |
| Ctrl+Shift+X, then an arrow | Swap with the pane in that direction |
| Ctrl+Shift+Enter / Ctrl+Shift+Super+= / Ctrl+Shift+L | Zoom / equalize / next layout |
| Ctrl+Tab / Ctrl+Shift+Tab, Ctrl+PgDn / Ctrl+PgUp | Next / previous session |
| Alt+1…8 / Alt+9 | Session 1…8 / the last session |
| Ctrl+Shift+1…9 | Workspace 1…9 |
| Ctrl+Shift+J / Ctrl+Shift+A | Next agent that needs you / every agent |
| Ctrl+` | Next agent that needs you or is idle |
| Ctrl+Shift+B / Ctrl+Shift+S | Fold the workspaces / the sessions |
| Ctrl+Shift+C / Ctrl+Shift+V | Copy / paste |
| Shift+Insert, middle click | Paste the primary selection |
| Ctrl+Shift+F | Search the scrollback |
| Ctrl+Shift+Space | Copy mode: h j k l w b 0 $ g G, v and V to select, y to copy |
| Shift+PgUp / Shift+PgDn, Shift+Home / Shift+End | Scroll |
| Ctrl+Shift+PgUp / Ctrl+Shift+PgDn | Previous / next prompt |
| Ctrl+Shift+R | Rename the session (or double-click its head) |
| Ctrl+, | Settings |
| Ctrl+= / Ctrl+- / Ctrl+0 | Font larger / smaller / as set |

## Copy and paste

Selecting copies (a setting); the primary selection follows the
selection either way. Double-click selects a word, triple-click a line;
with a program tracking the mouse (htop, vim with `mouse=a`), hold Shift
to select. Pasted text goes in as the program asked for it (bracketed
paste); text that could run commands where the program did not ask (a
line break, the end of a bracketed paste) shows a sheet first: Paste,
Paste as one line, or Cancel. Files on the clipboard paste as their
paths, quoted for the shell; an image with no text saves to
`$XDG_RUNTIME_DIR/inua/term-paste/` and pastes its path. Programs may set
the clipboard through OSC 52 (agents over SSH do); reading it is not
offered. Ctrl+click opens a link, an OSC 8 one or a URL in the text.

## Settings

`~/.config/inua/term.toml`, written by the guide and the settings
(Ctrl+,: an index of pages at the left, Look, Terminal, Shell and
agents, Web terminal and Keys) with what each setting means: the window's theme (`look.theme`,
`""` for inua's) and the terminals' colours (`look.scheme`, `""` for the
theme's), the font and its size, the cursor, copy on select, scrollback,
the host's replay size, the shell new panes start (`shell.program`, `""`
for `$SHELL`) and its integration, agents' notifications, remembering
commands and what never to keep, and the keys by action (`""` frees one
for the program), and the Web Terminal (`[web]`: `on`, `tailscale`,
`port`). The theme, the colours and the shell take hold at
once; the icons and the text size follow inua's `config.toml`.

## On a phone

The **Web Terminal** shows the terminals in a browser, a phone's over
Tailscale. Turn it on in the settings (Ctrl+, → Web terminal): it runs
`inua-termd serve` in a user service of its own (`inua-term-web.service`;
on a Mac the launchd agent `io.github.lyricat.Inua.Term.Web`) and starts
again at login while on; *Where* puts it on this computer
only or on its Tailscale address, and the section gives its link to copy,
or a new one. By hand:

```
inua-termd serve                  on this computer only (127.0.0.1:7681)
inua-termd serve --tailscale      on this computer's Tailscale address
inua-termd serve --addr HOST:PORT on another address
inua-termd serve --new-token      a new token: links with the old one stop working
```

It prints the page's link, with its token; every request carries it,
and it is kept in the keyring (inua-keyring; the login keychain on a
Mac), so the link stays the same. Where the keyring cannot keep it (no
desktop session; a Mac reached over SSH, its keychain locked), it is
kept in `term-web-token` in inua's configuration folder (`~/.config/inua`,
`~/Library/Application Support/inua` on a Mac), readable by you alone.
The page is a client of the host, as the window is, and starts the host
when none runs. Open the link on the phone, or add it to the home
screen: it is a web app in the theme's colours. Opened without its
token, or with an old one, the page asks for it: the token or the whole
link, pasted. The browser keeps the last good token and tries it first,
so a home-screen app made from an old link gets in by itself; a page
left open when New link is made goes to the question.

`serve` speaks plain HTTP. On this computer, and over Tailscale, whose
link is encrypted, that is safe; from any other address, the token, the
keys typed and the screens cross the network as they are. The page cannot
tell which (a Tailscale address looks like others; a proxy's TLS is not
its own): when it came over `http://` from anywhere but this computer,
each page's head has a warning mark at its start (a tap shows
Overview), Overview's Connection shows Encryption: plain HTTP, and a
passphrase card says the connection may not be encrypted.

- **Agents**: every agent by its state (needs you, working, done, idle),
  with where it runs; **Workspaces**: the workspaces, then a workspace's
  sessions and their panes; **Overview**: the agents in numbers, the connection, and the settings.
- **New ones**: + at the head of Workspaces makes a workspace for a
  folder (typed, `~/` for the home folder; one there already is shown),
  and in a workspace a new session; + beside a session's panes starts a
  shell beside the one open; each opens its shell. A session's pencil
  renames it.
- **gpg's passphrase** (a deploy's, `pass`, a signed commit): while the
  page is open, what inua-pinentry asks on the desktop comes up on it
  too; the first answer, here or there, goes to gpg (docs/features.md).
  The page must be open: a phone's browser in the background is not
  woken.
- **A pane** fills the screen at the phone's size: columns at the text
  size (10 px, about 62 columns on a phone held upright; pinch to change
  it), rows to the keyboard. At its foot, the session's panes with a
  back button, and a row of the keys a phone lacks (Esc, Tab, Ctrl, ^C,
  Page Up and Down, the arrows, Shift+Enter, Shift+Tab). The keyboard
  comes up with its button only; the button beside it opens a panel of
  more keys in the keyboard's place (Ctrl and Alt combinations, Home,
  End, symbols, F1–F12); its Snippets tab has replies typed often ("Sure,
  please do it.", "Wait, let me think." …) and your own, added there and
  kept in the browser: tapping a snippet sends it, with Enter.
  Its Info tab says what the pane's connection says.
- **Uploading**: the panel's Files tab sends files from the phone to
  the computer, up to 100 MB each, into the folder pasted images
  go to (`$XDG_RUNTIME_DIR/inua/term-paste`, gone at logout), and types
  their paths in the pane, quoted, for an agent or a command to take.
- **Downloading**: under Upload, the Files tab lists the folder the pane
  is in (hidden files left out); a tap on a folder goes in, the arrow goes
  up, and a tap on a file keeps it on the phone. The pencil takes a
  path: a folder is listed, a file (one an agent named) downloaded.
- Scrolling goes as a phone's lists do: the finger moves the history by
  the pixel, a swipe glides on, and the ends stretch. A program on the
  alternate screen (Claude Code's fullscreen mode, vim, less) scrolls
  itself, and gets the wheel.
- **Selecting**: a finger held still selects the word under it, and
  moving it, still held, takes the selection on. Lifted, a drop under
  each end of the selection moves that end when dragged, and a bar beside
  it offers Copy and Select all. A tap lets the selection go. The
  keyboard stays as it was.
- The theme, the terminals' colours, the font, the cursor and the key row
  follow inua-term's settings, or the device's own (Overview → Settings),
  kept in its browser.
- **On a tablet or a computer** (a window 720 px wide and 500 px tall or
  more; a phone on its side keeps the phone's layout), the lists stay in
  a column at the left with the tabs under them and the open pane beside
  them, as in the inua-term window; the back button closes the pane. The
  text is 13 px there. A keyboard of the device's own types into the pane
  once it is clicked, or once the keyboard button is pressed on an iPad;
  a computer has no key row unless it is turned on in Settings. Ctrl and
  the wheel, or a trackpad's pinch, change the text size.

The page asks nothing of the internet: xterm.js and the fonts come with
inua (`scripts/term-web.sh`). Over plain `http://`, an iPhone adds it
to the home screen as an app; Android installs one only over HTTPS
(`tailscale serve` can give the address one).

## On macOS

The window runs on a Mac as `inua-term.app`, a self-contained app: GTK
and the libraries it loads, the host and the engine are inside it, so
nothing needs installing. A release has it as
`inua-term-VERSION-macos-arm64.dmg`, signed and notarized; drag it to
Applications. It is the same window, with a Mac's ways:

- The window's keys are on Command, Control stays the programs': ⌘T a
  new session, ⌘N a new workspace, ⌘D and ⇧⌘D split, ⌘W closes the pane,
  ⌘C and ⌘V copy and paste, ⌘F searches, ⌘, the settings, ⌘= ⌘- ⌘0 the
  font, ⇧⌘P every action. The fixed keys follow: ⌘⌥ and an arrow moves
  the focus, ⌘⌃ and one resizes, ⌘1…9 shows a session, ⇧⌘1…9 a
  workspace, ⌘-click opens a link. A chord in term.toml writes Command
  as `super`, as Ghostty does; the keys show ⌘ ⌥ ⌃ ⇧.
- The system's fonts: the window's text in the system font, the
  terminal in Menlo unless term.toml names another.
- The menu bar has the window's actions: Terminal (Settings, Quit),
  Shell, Edit, View, Window, Help.
- A pasted image is read through GTK (there is no wl-paste) and pasted
  as a PNG file's path.

Closing the window quits the app; the host keeps the sessions, as on
Linux. To build the app on a Mac, with Go, Zig 0.16 and Homebrew's
`gtk4`, `librsvg` and `adwaita-icon-theme`:

```
scripts/macos-app.sh                   build/macos/inua-term.app and its .dmg
```

It copies GTK and what it loads into `Contents/Frameworks` and rewrites
each reference to the copy beside it, signs everything (ad hoc, or with
a Developer ID named in `APPLE_CODESIGN_IDENTITY`), and checks that
nothing points outside the bundle. A tagged release signs and notarizes
it when the Apple secrets are set
([apple_signing_linux.md](apple_signing_linux.md)). CI builds it on every
change and starts it once (`INUA_TERM_SMOKE=1` quits after the first
frame), failing if any library came from Homebrew instead.

The host runs on a Mac on its own too, for the page: `inua-termd serve`
(with `--tailscale` for a phone on the tailnet) starts it when none runs,
and the phone or a browser shows its terminals. From source, on the Mac:

```
scripts/libghostty.sh                  bin/libinua-vt.dylib (Zig 0.16)
go build -o bin/inua-termd ./cmd/inua-termd
bin/inua-termd serve --tailscale
```

`ZIG_TARGET=aarch64-macos` (or `x86_64-macos`) builds the engine on
Linux as well. The page's token is kept in the login keychain, or in a file
when the keychain is locked (over SSH); an agent
that needs you posts a notification of the Mac's own (through
osascript), which opens nothing when clicked. Turned on in the app's
settings, it runs as a launchd agent: its property list is
`~/Library/LaunchAgents/io.github.lyricat.Inua.Term.Web.plist` while it
is on, so it starts again at login and 3 seconds after a failure, and
its output goes to `~/Library/Logs/inua-term-web.log`.

## Not yet

- Images in the terminal (the kitty graphics protocol, sixel).
- inua's clipboard history in the terminal (the shell's clipboard panel
  has it).
- Reading the clipboard through OSC 52, allowed per workspace, and
  notifications turned off per workspace.
