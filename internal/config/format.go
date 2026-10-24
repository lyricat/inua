package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Format renders settings as a complete, commented config.toml. The sample
// in the repository is Format(Default()); the settings window saves with
// it, so a saved file stays documented. Keys the shell does not know are
// not kept.
func Format(c Config) string {
	return fmt.Sprintf(template,
		q(c.Language),
		c.Bar.Enable, q(c.Bar.Density), q(c.Bar.Clock), q(c.Bar.Workspaces), c.Bar.Title, c.Bar.Tray, c.Bar.Pass,
		q(c.Launcher.Mode), q(c.Launcher.Terminal), q(c.Launcher.Search), quicklinks(c.Launcher.Quicklinks), c.Overview.MRU, c.Overview.PerMonitor,
		q(c.Control.Lock), q(c.Control.Screenshot),
		q(c.Screenshot.Folder), c.Screenshot.Copy, q(c.Screenshot.Recordings),
		c.OSD.Enable, q(c.OSD.Position),
		q(c.Style.Theme), q(c.Style.Icons), q(c.Style.AppIcons), c.Style.EdgeGaps, c.Style.TextScale, c.Style.Transparency, c.Style.Radius, q(c.Style.CSS),
		c.Background.Enable, q(c.Background.Image), q(c.Background.Fit), q(c.Background.Color),
		c.Background.Generate,
		kbOptionsLine(c.Input.KbOptions), touchpadSpeedLine(c.Input.TouchpadSpeed), q(c.Input.TouchpadAccel),
		c.Displays.LidOff, monitors(c.Displays.Monitors),
		c.Notify.Enable, c.Notify.Banner, c.Notify.CriticalInFocus, c.Notify.Updates, c.Notify.History,
		c.Clipboard.Enable, c.Clipboard.Bar, c.Clipboard.Limit, c.Clipboard.PasteOnClick, list(c.Clipboard.SensitiveSources),
		c.Dock.Enable, q(c.Dock.Position), c.Dock.Autohide, c.Dock.Size, c.Dock.Drawer, list(c.Dock.Pinned),
		c.Voice.Enable, c.Voice.PushToTalk,
		c.Assistant.Enable, q(c.Assistant.Profile), list(c.Assistant.Fallback), q(c.Assistant.Decision), q(c.Assistant.Transcription),
		list(c.Assistant.Tools), c.Assistant.AskBeforeChanges,
		c.Assistant.ServeEnable, q(c.Assistant.ServeListen), aiProfiles(c.Assistant.Profiles),
		q(c.Keyring.Folder), c.Keyring.Notify, q(c.Keyring.Untrusted), c.Keyring.LockAfter,
		c.MCP.Enable, q(c.MCP.Policy),
		shortcuts(c.Shortcuts))
}

// The optional settings are written commented out while unset, so the file
// still shows them.
func kbOptionsLine(o *string) string {
	if o == nil {
		return `# kb_options = ""`
	}
	return "kb_options = " + q(*o)
}

// shortcuts renders the shortcuts as an array of tables.
func shortcuts(all []Shortcut) string {
	var b strings.Builder
	for _, sc := range all {
		fmt.Fprintf(&b, "\n[[shortcuts]]\naction = %s\nkeys = %s\n", q(sc.Action), q(sc.Keys))
		if sc.Action == "exec" {
			fmt.Fprintf(&b, "command = %s\nname = %s\n", q(sc.Command), q(sc.Name))
		}
		if len(sc.Replaces) > 0 {
			fmt.Fprintf(&b, "replaces = %s\n", list(sc.Replaces))
		}
	}
	return b.String()
}

// monitors renders the displays' rules as an array of tables under
// [displays].
func monitors(all []Monitor) string {
	var b strings.Builder
	for _, m := range all {
		fmt.Fprintf(&b, "\n[[displays.monitors]]\noutput = %s\n", q(m.Output))
		if m.Disabled {
			b.WriteString("disabled = true\n")
		}
		fmt.Fprintf(&b, "mode = %s\nposition = %s\nscale = %s\n", q(m.Mode), q(m.Position), strconv.FormatFloat(m.Scale, 'f', -1, 64))
		if m.Mirror != "" {
			fmt.Fprintf(&b, "mirror = %s\n", q(m.Mirror))
		}
	}
	return b.String()
}

// quicklinks renders the quicklinks as an array of tables under
// [launcher], or an empty array for none.
func quicklinks(all []Quicklink) string {
	if len(all) == 0 {
		return "\nquicklinks = []"
	}
	var b strings.Builder
	for _, l := range all {
		fmt.Fprintf(&b, "\n\n[[launcher.quicklinks]]\nkeyword = %s\nname = %s\nurl = %s", q(l.Keyword), q(l.Name), q(l.URL))
	}
	return b.String()
}

// aiProfiles renders the assistant's profiles as an array of tables
// under [assistant].
func aiProfiles(all []AIProfile) string {
	var b strings.Builder
	for _, p := range all {
		fmt.Fprintf(&b, "\n\n[[assistant.profiles]]\nname = %s\nprovider = %s\nmodel = %s\nendpoint = %s\nreasoning = %s\nabilities = %s",
			q(p.Name), q(p.Provider), q(p.Model), q(p.Endpoint), q(p.Reasoning), list(p.Abilities))
	}
	return b.String()
}

// list renders a TOML array of strings.
func list(items []string) string {
	q2 := make([]string, len(items))
	for i, s := range items {
		q2[i] = q(s)
	}
	return "[" + strings.Join(q2, ", ") + "]"
}

func touchpadSpeedLine(v *float64) string {
	if v == nil {
		return "# touchpad_speed = 0.0"
	}
	return "touchpad_speed = " + strconv.FormatFloat(*v, 'f', 2, 64)
}

// Save writes Format(c) to path, replacing the file in one step.
func Save(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op once renamed
	if _, err := tmp.WriteString(Format(c)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// q quotes a TOML string: a basic string when it needs no escapes, a
// literal string for text with double quotes or backslashes (shell
// commands), and an escaped basic string otherwise.
func q(s string) string {
	plain := !strings.ContainsAny(s, "\"\\") && strings.IndexFunc(s, isControl) < 0
	switch {
	case plain:
		return `"` + s + `"`
	case !strings.ContainsRune(s, '\'') && strings.IndexFunc(s, isControl) < 0:
		return "'" + s + "'"
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case isControl(r):
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

// The documented layout; %s are quoted strings (or whole lines for unset
// optional settings), %t booleans, %d integers.
var template = strings.TrimLeft(`
# inua-shell settings, ~/.config/inua/config.toml.
#
# Every value below is the default until you change it; delete a line to go
# back to its default. Unknown keys and bad values are reported
# (`+"`inua config check`"+`) and fall back to the default.
#
# The settings window (inua call settings open) edits this file and applies
# changes at once; it rewrites the file in this layout. After editing by
# hand: `+"`inua call shell reload`"+`.

# The interface's language: "auto" follows the system (LANGUAGE, LC_ALL,
# LC_MESSAGES, LANG); or "en", "zh_CN", "zh_TW", "ja", "ko", "es", "fr".
# The shell and apps read it when they start.
language = %s

[bar]
# Show the bar along the top of each monitor. Without it the launcher
# (Super+Space), control center (Super+A) and notification center (Super+N)
# are still a key away.
enable = %t

# Bar density: "default" or "compact". Compact saves vertical screen space.
density = %s

# Clock, in strftime format: %%a %%A weekday, %%b %%B month, %%d %%e day, %%m month,
# %%Y year, %%H %%I hour (24h/12h), %%M minute, %%S second, %%p AM/PM.
# A '-' drops zero padding (%%-d, %%-H). With seconds, the clock ticks every
# second.
clock = %s

# "monitor": each bar shows the workspaces of its own monitor.
# "all": every bar shows every workspace.
workspaces = %s

# Show the focused window's title.
title = %t

# Show tray (StatusNotifierItem) icons.
tray = %t

# Show a button that searches the password store (the one the Pass app
# uses) and copies a password, a user name or a one-time code. Shown only
# when there is a store. `+"`inua call pass toggle`"+` opens it too.
pass = %t

[launcher]
# What `+"`inua call launcher toggle`"+` opens: "apps" or "windows".
# (`+"`launcher apps`"+` and `+"`launcher windows`"+` open a mode directly; Tab switches.)
mode = %s

# The default terminal (Settings, Default Apps): `+"`inua terminal`"+` opens it, and
# apps marked Terminal=true run in it, the command after "-e".
# Empty: $TERMINAL, then xdg-terminals.list's first, then xdg-terminal-exec.
terminal = %s

# The web search offered after the results: a web address with %%s where
# the words go.
search = %s

# Quicklinks: a query starting with a keyword searches that site
# ("gh inua"). Add your own; quicklinks = [] turns them off.%s

[overview]
# Sort workspaces by recent window focus instead of their numeric order.
mru = %t
# Show each display's workspaces on its own overview surface.
per_monitor = %t

[control]
# Shell commands run by the control center's buttons.
lock = %s
screenshot = %s

[screenshot]
# Where screenshots are saved, "~/" for your home directory. Empty: not
# saved.
folder = %s
# Put each screenshot on the clipboard as well.
copy = %t
# Where screen recordings are saved (Record in the screenshot tool; needs
# wf-recorder).
recordings = %s

[osd]
# The on-screen display: the card that shows for a moment when the
# volume, the brightness, Wi-Fi or Bluetooth changes, wherever the change
# came from (the keys, the wheel on the bar, the control center).
enable = %t
# Where the card anchors on the focused monitor: top-center, top-left,
# top-right, bottom-center, bottom-left or bottom-right.
position = %s

[style]
# Colour theme:
#   "yoru"      夜     dark, night (the default)
#   "washi"     和紙   light, warm paper and ink
#   "aojashin"  青写真 light, cool paper in blueprint blue
#   "suimen"    水面   dark, night water and cyan
#   "rinko"     燐光   dark, phosphor green
#   "bunko"     分光   dark, indigo and cyan
#   "denko"     電光   dark, violet and electric cyan
# or a theme of your own: themes/NAME.css next to this file, with
# @define-color lines for the colors it changes (themes/nord.css, from
# inua init, lists them all); the others come from yoru, or from the
# built-in theme it names.
theme = %s

# Icons of the bar, control center, launcher and settings:
#   "phosphor"  Phosphor's line icons, built in (the default)
#   "system"    the system's icon theme
# or an installed icon theme, by its folder's name ("Papirus", in
# /usr/share/icons or ~/.local/share/icons), or a set of your own: a folder
# icons/NAME/ next to this file, with SVGs named like the built-in ones
# (NAME-symbolic.svg); icons it lacks stay Phosphor's (icons/phosphor-bold,
# from inua init, is one). Draw them in one colour: it follows the theme.
icons = %s

# Icons of apps (dock, launcher, drawer, notifications) and of files in
# Files: "system", the system's icon theme (the default), or an installed
# icon theme, by its folder's name. The tray's come from each app.
app_icons = %s

# A small gap between the screen edges and the bar and dock. false puts
# them flush against the edges.
edge_gaps = %t

# Size of the text in the shell and inua's apps, 80 to 150 percent. Icons,
# spacing and the stylesheet below keep their sizes.
text_scale = %d

# Background transparency of the bar, the launcher, the panels, the dock,
# notifications and the level indicator, from 0%% (opaque) to 80%%. An
# empty workspace's bar is lighter. Menus, the settings window and the
# lock screen keep theirs.
transparency = %d

# Corner radius of the same surfaces and the menus, 0 (square) to 20
# pixels; the dock's is half as much again.
radius = %d

# Extra stylesheet loaded after the built-in one, relative to this file's
# directory. Ignored if the file does not exist. See style.css next to this
# file for the colors you can override.
css = %s

[background]
# Draw the wallpaper. false leaves the background to another program.
enable = %t

# Picture to show: a path, "~/" for your home directory. Empty: the theme's
# ground colour with a fine texture.
image = %s

# How the picture fills each monitor:
#   "cover"    fill the monitor, cropping what does not fit
#   "contain"  show all of it, with the colour around it
#   "tile"     repeat it at its own size
#   "center"   its own size, in the middle
fit = %s

# Colour around and behind the picture, "#rrggbb". Empty: the theme's ground.
color = %s

# inua draws the wallpaper for your theme, in place of the image: a
# sunset, the cosmos, a cat watching the moon, contours … another each day.
generate = %t

[input]
# Keyboard and touchpad settings for Hyprland. inua writes them to
# ~/.config/hypr/inua.conf, which hyprland.conf must source after its own
# input settings (`+"`inua config hyprland`"+` checks). A setting left commented
# out or empty stays with hyprland.conf. After editing by hand:
# `+"`inua config apply`"+`.

# XKB options for every keyboard, comma-separated, e.g. "caps:escape" (Caps
# Lock is Escape), "ctrl:nocaps" (Caps Lock is Ctrl), "ctrl:swap_lalt_lctl"
# (swap left Ctrl and Alt), "ctrl:swap_ralt_rctl" (the right ones).
# "" clears the options hyprland.conf sets.
%s

# Touchpad pointer speed, -1 (slowest) to 1 (fastest); 0 is libinput's.
%s

# Touchpad acceleration: "adaptive" (faster swipes go further) or "flat".
touchpad_accel = %s

[displays]
# Monitors, set in the settings window (Displays). inua writes them to
# ~/.config/hypr/inua.conf as monitor rules after hyprland.conf's, so a
# display listed here is as it says, and one not listed stays as
# hyprland.conf has it. After editing by hand: `+"`inua config apply`"+`.

# While a laptop's lid is closed and another display is connected, turn
# the built-in display off (its workspaces move to the others); opening
# the lid turns it on again, beside them.
lid_off = %t

# A display: output is its connector ("DP-1"), or "desc:" and its
# description (hyprctl monitors) to follow it from port to port. mode is
# "WIDTHxHEIGHT@HZ" ("" the preferred); position "auto", "auto-left",
# "auto-right", "auto-up", "auto-down" (beside the others) or "XxY";
# scale 0.25 to 4 (0 automatic); mirror another output to show the same
# picture; disabled = true leaves it off.
#
#   [[displays.monitors]]
#   output = "desc:Dell Inc. DELL U2720QM GZF09H3"
#   mode = "3840x2160@60"
#   position = "auto-right"
#   scale = 1.5
%s

[notifications]
# Show notifications: a banner for a moment, then the notification center
# (the bell in the bar, or Super+N), grouped by app. false leaves them to another
# notification daemon (mako, dunst). While one runs, inua waits for it to
# exit.
enable = %t

# How long a banner shows, in seconds (2 to 60). Critical notifications
# stay until dismissed.
banner_seconds = %d

# Show banners for critical notifications in Focus (the control center's Do
# Not Disturb), which otherwise keeps banners quiet.
critical_in_focus = %t

# Check once a day for a new inua release, a few minutes after you log in,
# and tell you in a notification, with a button to upgrade (inua upgrade).
updates = %t

# Keep a history of the notifications that come, the last 500, also those
# dismissed, in ~/.local/state/inua/notification-history.jsonl: for the
# assistant, agents (inua mcp) and workshop apps that may read them.
# Turning it off clears it.
history = %t

[clipboard]
# Keep a history of what you copy (text and images, with wl-paste from
# wl-clipboard) in ~/.local/state/inua. A password manager's copies, which
# it marks as sensitive, are left out. false records nothing.
enable = %t

# Show the clipboard history button in the top bar; a shortcut set in
# Shortcuts (Clipboard history) opens the panel too.
bar = %t

# How many entries the history keeps, 20 to 1000; the oldest go first.
limit = %d

# Clicking an entry pastes it into the window you were in; false only
# copies it.
paste_on_click = %t

# Programs and window classes whose copies are never recorded, in any
# case. Wayland does not say who copied: inua looks at the job that ran
# wl-copy (pass -c, passmenu) and at the focused window. Hover over an
# entry in the panel to see where it came from.
sensitive_sources = %s

[dock]
# Show the app dock on each monitor: pinned apps, then the others with open
# windows. Click to open or bring forward (again to cycle through its windows);
# right-click to pin, open a new window or quit.
enable = %t

# Screen edge for the dock: bottom, top, left or right.
position = %s

# Hide the dock until the pointer reaches its screen edge. false: windows
# make room for it.
autohide = %t

# Icon size in pixels (24 to 96).
size = %d

# A button at the dock's end that opens Settings, the launcher, Screenshot,
# the Assistant and inua's light apps (Clock, Weather, Stocks, Calculator).
drawer = %t

# Apps always on the dock, by desktop ID (the .desktop file's name without
# .desktop), in order. Missing ones are skipped.
pinned = %s

[voice]
# Voice input, on top of the voxtype daemon (https://voxtype.io): hold the
# voice shortcut and talk; the transcription is pasted into the focused
# window. false turns the microphone and its shortcut off.
enable = %t

# true: the shortcut's key press starts the recording and its release ends
# it (push to talk). false: the shortcut toggles a recording on and off.
push_to_talk = %t

[assistant]
# The assistant (inua-assistant, the launcher's "Ask"): an agent with
# inua's tools, run by Mister Morph. The shell starts it in the background;
# model calls only run when you or a connected Console submit a task.
enable = %t

# The profile that answers, and the profiles tried in turn when it fails
# (an outage, a rate limit). The window can switch to another profile for
# a conversation.
profile = %s
fallback = %s

# The profile that makes quick choices from a fixed list, as the games' AI
# players do (Mister Morph's decision route); "" for the one that answers,
# or, when it makes none, the first profile that does.
decision = %s

# The profile that turns voice input (the assistant window's microphone)
# into text, with OpenAI, Groq or a compatible server: Mister Morph's
# transcription route; "" for the first profile with the "transcription"
# ability. None, and the window has no microphone.
transcription = %s

# Mister Morph's built-in tools to add to inua's: "web_search", "url_fetch",
# and, asking before each use, "read_file" and "bash".
tools = %s

# Ask before changing anything (Wi-Fi, volume, windows …). Power, ending a
# process and closing a window always ask.
ask_before_changes = %t

# Console Runtime API: turn on in Settings → AI, which also creates and
# copies its token. The token stays in the Secret Service (inua-keyring
# keeps it in pass); inua-assistant --serve-token also prints it.
# Console uses http://ADDRESS/runtime. Turning serving off keeps the
# address and token; the assistant continues running in the background.
serve_enable = %t
serve_listen = %s

# The models, as Mister Morph's profiles: a name, a provider ("openai",
# "anthropic", "gemini", "xai", "deepseek", "kimi", "openrouter", "groq",
# "mistermorph_pro", or "openai_chat_compatible", "openai_response_compatible",
# "anthropic_compatible" with an endpoint, or "typesafe", which makes
# quick choices only), the model, an endpoint ("" for the provider's
# own), the reasoning effort ("minimal" to "high"; "" for the model's
# default), and what it is for: abilities "text" answers,
# "decision" makes quick choices (it alone: only those), "transcription"
# turns voice input into text (OpenAI, Groq and the compatible servers;
# a speech-to-text model such as "gpt-4o-mini-transcribe" or
# "whisper-large-v3-turbo"); [] for the first two.
# Another provider of Mister Morph's is kept as it is. The API key is
# typed in the settings window (AI) and kept in the keyring (the Secret
# Service; inua-keyring keeps it in pass), or without one in
# ~/.local/share/inua/keys.json, readable only by you; keys are never
# written here.%s

[keyring]
# inua-keyring keeps apps' secrets (the Secret Service: browsers' keys,
# Wi-Fi passwords, the assistant's API keys) in pass, under this folder
# of the password store (PASSWORD_STORE_DIR, else ~/.password-store).
# inua keyring status|import|enable|disable|paths|allow|disallow: see
# docs/features.md.
folder = %s

# A notification when an app reads its first secret since inua-keyring
# started; off by default. Every read is listed in Settings → Keyring.
notify = %t

# Each secret has the apps trusted with it; another app reading it is
# asked about (Deny, Allow once, Always allow). With no inua shell to
# ask in: "allow" (as without the list, and recorded) or "deny".
untrusted = %s

# After the first read, the keyring keeps its secrets in protected memory
# until it locks: with the screen, before suspend, with "inua keyring
# lock", or after this many minutes without a read (0: not by time).
lock_after = %d

[mcp]
# inua as an MCP server: its tools (windows, apps, settings,
# notifications…) for the assistant, which uses it whatever this says,
# and for agents on this computer (Claude Code, Codex…) through
# "inua mcp", e.g. claude mcp add inua -- inua mcp. Off for agents by
# default.
enable = %t

# What an agent may do: "read" (look only), "ask" (ask before changes, in
# inua's dialog) or "allow" (change without asking; closing windows,
# ending processes, power, the clipboard and notifications still ask).
policy = %s

# Keyboard shortcuts set in the settings window (Shortcuts). inua writes
# them to ~/.config/hypr/inua.conf: each unbinds its keys from anything
# else, and the keys its action had in hyprland.conf ("replaces"). Actions
# without an entry keep their hyprland.conf shortcuts. An action is one of
#   launcher windows overview control notifications clipboard focus settings restart lock
#   shot-area shot-area-silent shot-screen shot-window shot-record color
# or "exec" with a command. keys = "" turns the action's shortcut off.
#
#   [[shortcuts]]
#   action = "launcher"
#   keys = "SUPER+Space"
%s`, "\n")
