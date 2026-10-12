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
		c.Bar.Enable, q(c.Bar.Clock), q(c.Bar.Workspaces), c.Bar.Title, c.Bar.Tray,
		q(c.Launcher.Mode), q(c.Launcher.Terminal), q(c.Launcher.Search), quicklinks(c.Launcher.Quicklinks), c.Overview.MRU, c.Overview.PerMonitor,
		q(c.Control.Lock), q(c.Control.Screenshot),
		q(c.Screenshot.Folder), c.Screenshot.Copy, q(c.Screenshot.Recordings),
		q(c.Style.Theme), q(c.Style.Icons), c.Style.EdgeGaps, q(c.Style.CSS),
		c.Background.Enable, q(c.Background.Image), q(c.Background.Fit), q(c.Background.Color),
		kbOptionsLine(c.Input.KbOptions), touchpadSpeedLine(c.Input.TouchpadSpeed), q(c.Input.TouchpadAccel),
		c.Notify.Enable, c.Notify.Banner, c.Notify.CriticalInFocus,
		c.Dock.Enable, q(c.Dock.Position), c.Dock.Autohide, c.Dock.Size, c.Dock.Drawer, list(c.Dock.Pinned),
		c.Assistant.Enable, q(c.Assistant.Profile), list(c.Assistant.Fallback),
		list(c.Assistant.Tools), c.Assistant.AskBeforeChanges, aiProfiles(c.Assistant.Profiles),
		q(c.Keyring.Folder),
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
		fmt.Fprintf(&b, "\n\n[[assistant.profiles]]\nname = %s\nprovider = %s\nmodel = %s\nendpoint = %s\napi_key_env = %s\nreasoning = %s",
			q(p.Name), q(p.Provider), q(p.Model), q(p.Endpoint), q(p.APIKeyEnv), q(p.Reasoning))
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

[bar]
# Show the bar along the top of each monitor. Without it the launcher
# (Super+Space), control center (Super+A) and notification center (Super+N)
# are still a key away.
enable = %t

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

[launcher]
# What `+"`inua call launcher toggle`"+` opens: "apps" or "windows".
# (`+"`launcher apps`"+` and `+"`launcher windows`"+` open a mode directly; Tab switches.)
mode = %s

# Terminal for apps marked Terminal=true; the command follows after "-e".
# Empty: $TERMINAL, then xdg-terminal-exec.
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

[style]
# Colour theme:
#   "yoru"      夜     dark, night (the default)
#   "washi"     和紙   light, warm paper and ink
#   "aojashin"  青写真 light, cool paper in blueprint blue
#   "suimen"    水面   dark, night water and cyan
#   "rinko"     燐光   dark, phosphor green
#   "bunko"     分光   dark, indigo and cyan
#   "denko"     電光   dark, violet and electric cyan
theme = %s

# Icons of the bar, control center, launcher and settings:
#   "phosphor"  Phosphor's line icons, built in (the default)
#   "system"    the system's icon theme
# App icons in the launcher and the tray always come from the system's.
icons = %s

# A small gap between the screen edges and the bar and dock. false puts
# them flush against the edges.
edge_gaps = %t

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

[assistant]
# The assistant (inua-assistant, the launcher's "Ask"): an agent with
# inua's tools, run by Mister Morph. Nothing runs until you ask.
enable = %t

# The profile that answers, and the profiles tried in turn when it fails
# (an outage, a rate limit). The window can switch to another profile for
# a conversation.
profile = %s
fallback = %s

# Mister Morph's built-in tools to add to inua's: "web_search", "url_fetch",
# and, asking before each use, "read_file" and "bash".
tools = %s

# Ask before changing anything (Wi-Fi, volume, windows …). Power, ending a
# process and closing a window always ask.
ask_before_changes = %t

# The models, as Mister Morph's profiles: a name, a provider ("openai",
# "anthropic", "gemini", "xai", "deepseek", "kimi", "openrouter", "groq",
# "mistermorph_pro", or "openai_chat_compatible", "openai_response_compatible",
# "anthropic_compatible" with an endpoint), the model, an endpoint ("" for
# the provider's own), the environment variable holding the API key, and
# the reasoning effort ("minimal" to "high"; "" for the model's default).
# A key typed in the settings window (AI) is kept in the keyring (the
# Secret Service; inua-keyring keeps it in pass) instead, or without one
# in ~/.local/share/inua/keys.json, readable only by you; keys are never
# written here.%s

[keyring]
# inua-keyring keeps apps' secrets (the Secret Service: browsers' keys,
# Wi-Fi passwords, the assistant's API keys) in pass, under this folder
# of the password store (PASSWORD_STORE_DIR, else ~/.password-store).
# inua keyring status|import|enable|disable: see docs/feat/keyring.md.
folder = %s

# Keyboard shortcuts set in the settings window (Shortcuts). inua writes
# them to ~/.config/hypr/inua.conf: each unbinds its keys from anything
# else, and the keys its action had in hyprland.conf ("replaces"). Actions
# without an entry keep their hyprland.conf shortcuts. An action is one of
#   launcher windows overview control notifications focus settings restart lock
#   shot-area shot-screen shot-window shot-record color
# or "exec" with a command. keys = "" turns the action's shortcut off.
#
#   [[shortcuts]]
#   action = "launcher"
#   keys = "SUPER+Space"
%s`, "\n")
