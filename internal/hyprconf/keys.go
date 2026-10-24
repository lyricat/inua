package hyprconf

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/i18n"
)

// Action is something a shortcut can run (config.ShortcutActions).
type Action struct {
	ID, Label string
	Shot      bool // a screenshot; the settings window lists them apart
}

// Actions are the shell's actions, in the order the settings window shows
// them.
var Actions = []Action{
	{ID: "launcher", Label: i18n.M("Launcher")},
	{ID: "windows", Label: i18n.M("Switch windows")},
	{ID: "overview", Label: i18n.M("Workspace overview")},
	{ID: "control", Label: i18n.M("Control center")},
	{ID: "notifications", Label: i18n.M("Notification center")},
	{ID: "clipboard", Label: i18n.M("Clipboard history")},
	{ID: "pass", Label: i18n.M("Search passwords")},
	{ID: "focus", Label: i18n.M("Focus on or off")},
	{ID: "settings", Label: i18n.M("Settings")},
	{ID: "restart", Label: i18n.M("Restart the shell")},
	{ID: "lock", Label: i18n.M("Lock the screen")},
	{ID: "shot-area", Label: i18n.M("Take a screenshot"), Shot: true},
	{ID: "shot-area-silent", Label: i18n.M("Silent screenshot"), Shot: true},
	{ID: "shot-screen", Label: i18n.M("The whole screen, at once"), Shot: true},
	{ID: "shot-window", Label: i18n.M("The focused window, at once"), Shot: true},
	{ID: "shot-record", Label: i18n.M("Record the screen (again: stop)"), Shot: true},
	{ID: "color", Label: i18n.M("Pick a colour"), Shot: true},
	{ID: "voice", Label: i18n.M("Voice input")},
}

// Command is the shell command behind an action.
func Command(c config.Config, action string) string {
	switch action {
	case "launcher":
		return "inua call launcher toggle"
	case "windows":
		return "inua call launcher windows"
	case "overview":
		return "inua call overview toggle"
	case "control":
		return "inua call control toggle"
	case "notifications":
		return "inua call notifications toggle"
	case "clipboard":
		return "inua call clipboard toggle"
	case "pass":
		return "inua call pass toggle"
	case "focus":
		return "inua call notifications focus"
	case "settings":
		return "inua call settings toggle"
	case "restart":
		return "inua call shell restart"
	case "lock":
		return c.Control.Lock
	case "shot-area":
		return "inua call screenshot area"
	case "shot-area-silent":
		return "inua call screenshot silent-area"
	case "shot-screen":
		return "inua call screenshot screen"
	case "shot-window":
		return "inua call screenshot window"
	case "shot-record":
		return "inua call screenshot record"
	case "voice":
		return "inua call voice toggle"
	case "color":
		return "inua call screenshot color"
	}
	return ""
}

// ShortcutCommand is what a shortcut runs.
func ShortcutCommand(c config.Config, sc config.Shortcut) string {
	if sc.Action == "exec" {
		return sc.Command
	}
	return Command(c, sc.Action)
}

// RecordSubmap is a Hyprland submap with no shortcuts: while the settings
// window records keys, the compositor passes every key to it. SUPER+CTRL+
// ALT+Escape leaves it should inua fail to.
const RecordSubmap = "inua-record"

// Modifier order in key strings ("SUPER+CTRL+ALT+SHIFT+key").
var modOrder = []string{"SUPER", "CTRL", "ALT", "SHIFT"}

// Keys builds a key string from modifiers and an XKB key name. A single
// letter is upper case, as Hyprland configs write them.
func Keys(super, ctrl, alt, shift bool, key string) string {
	var parts []string
	for i, on := range []bool{super, ctrl, alt, shift} {
		if on {
			parts = append(parts, modOrder[i])
		}
	}
	if len(key) == 1 {
		key = strings.ToUpper(key)
	}
	return strings.Join(append(parts, key), "+")
}

// splitKeys parses "SUPER+SHIFT+S" into Hyprland's "SUPER SHIFT" and "S".
func splitKeys(keys string) (mods, key string) {
	parts := strings.Split(keys, "+")
	return strings.Join(parts[:len(parts)-1], " "), parts[len(parts)-1]
}

// Label is a key string to show: "Super Shift S".
func Label(keys string) string {
	if keys == "" {
		return ""
	}
	parts := strings.Split(keys, "+")
	for i, p := range parts[:len(parts)-1] {
		parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
	}
	k := parts[len(parts)-1]
	if len(k) > 1 && strings.ToLower(k) == k {
		k = strings.ToUpper(k[:1]) + k[1:] // "space" → "Space"
	}
	parts[len(parts)-1] = k
	return strings.Join(parts, " ")
}

// SameKeys compares key strings, ignoring the case of the key name.
func SameKeys(a, b string) bool { return strings.EqualFold(a, b) }

// Bind is a shortcut Hyprland has (hyprctl binds).
type Bind struct {
	Keys        string // as Keys builds them
	Dispatcher  string
	Arg         string
	Submap      string
	Description string
}

// ParseBinds reads a j/binds reply. Mouse binds are skipped.
func ParseBinds(reply []byte) ([]Bind, error) {
	var raw []struct {
		Modmask     int    `json:"modmask"`
		Key         string `json:"key"`
		Mouse       bool   `json:"mouse"`
		Dispatcher  string `json:"dispatcher"`
		Arg         string `json:"arg"`
		Submap      string `json:"submap"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(reply, &raw); err != nil {
		return nil, err
	}
	var binds []Bind
	for _, r := range raw {
		if r.Mouse || r.Key == "" {
			continue
		}
		m := r.Modmask
		binds = append(binds, Bind{
			Keys:        Keys(m&64 != 0, m&4 != 0, m&8 != 0, m&1 != 0, r.Key),
			Dispatcher:  r.Dispatcher,
			Arg:         r.Arg,
			Submap:      r.Submap,
			Description: r.Description,
		})
	}
	return binds, nil
}

// KeysFor lists the keys of the binds that run command, or that Hyprland
// describes as label (inua.lua's binds, whose command a Lua bind does not
// show), outside submaps.
func KeysFor(binds []Bind, command, label string) []string {
	var keys []string
	norm := strings.Join(strings.Fields(command), " ")
	for _, b := range binds {
		runs := b.Dispatcher == "exec" && strings.Join(strings.Fields(b.Arg), " ") == norm
		described := label != "" && strings.EqualFold(b.Description, label)
		if b.Submap == "" && (runs || described) && !slices.ContainsFunc(keys, func(k string) bool { return SameKeys(k, b.Keys) }) {
			keys = append(keys, b.Keys)
		}
	}
	return keys
}

// renderShortcuts writes the shortcuts: unbinds first (the keys an action
// had in hyprland.conf, and every key inua binds, whatever else it did),
// then the binds, then the recording submap.
func renderShortcuts(b *strings.Builder, c config.Config) {
	var unbind []string
	for _, sc := range c.Shortcuts {
		for _, k := range append(slices.Clone(sc.Replaces), sc.Keys) {
			if k != "" && !slices.ContainsFunc(unbind, func(u string) bool { return SameKeys(u, k) }) {
				unbind = append(unbind, k)
			}
		}
	}
	if len(c.Shortcuts) > 0 {
		b.WriteString("\n# Shortcuts from inua's settings window.\n")
	}
	for _, k := range unbind {
		mods, key := splitKeys(k)
		fmt.Fprintf(b, "unbind = %s, %s\n", mods, key)
	}
	for _, sc := range c.Shortcuts {
		cmd := ShortcutCommand(c, sc)
		if sc.Keys == "" || cmd == "" {
			continue
		}
		mods, key := splitKeys(sc.Keys)
		// Voice push-to-talk needs the key's release, which Hyprland
		// binds apart; the Lua path has no release binds, so it keeps
		// the toggle.
		if sc.Action == "voice" && c.Voice.Enable && c.Voice.PushToTalk {
			fmt.Fprintf(b, "bind = %s, %s, exec, inua call voice start\nbindr = %s, %s, exec, inua call voice stop\n", mods, key, mods, key)
			continue
		}
		// Hyprland reads # as a comment; ## is a literal #.
		fmt.Fprintf(b, "bind = %s, %s, exec, %s\n", mods, key, strings.ReplaceAll(cmd, "#", "##"))
	}
	fmt.Fprintf(b, "\n# While inua's settings window records a shortcut, no other is active.\nsubmap = %s\nbind = SUPER CTRL ALT, Escape, submap, reset\nsubmap = reset\n", RecordSubmap)
}

// luaKeys are the ways keys can be written in a Lua bind ("SUPER + SHIFT +
// V"). hl.unbind matches case exactly, so a key of more than one letter
// comes as written, in capitals and in lower case: "Tab", "TAB", "tab".
func luaKeys(keys string) []string {
	mods, key := splitKeys(keys)
	prefix := ""
	if mods != "" {
		prefix = strings.ReplaceAll(mods, " ", " + ") + " + "
	}
	var out []string
	for _, k := range []string{key, strings.ToUpper(key), strings.ToLower(key)} {
		if !slices.Contains(out, prefix+k) {
			out = append(out, prefix+k)
		}
	}
	return out
}

// shortcutLabel is a shortcut's description in Hyprland (hyprctl binds),
// which the settings window matches its actions by.
func shortcutLabel(sc config.Shortcut) string {
	if sc.Action == "exec" {
		return cmp.Or(sc.Name, sc.Command)
	}
	for _, a := range Actions {
		if a.ID == sc.Action {
			return a.Label
		}
	}
	return sc.Action
}

// renderShortcutsLua is renderShortcuts for inua.lua.
func renderShortcutsLua(b *strings.Builder, c config.Config) {
	var unbind []string
	for _, sc := range c.Shortcuts {
		for _, k := range append(slices.Clone(sc.Replaces), sc.Keys) {
			if k != "" && !slices.ContainsFunc(unbind, func(u string) bool { return SameKeys(u, k) }) {
				unbind = append(unbind, k)
			}
		}
	}
	if len(c.Shortcuts) > 0 {
		b.WriteString("\n-- Shortcuts from inua's settings window.\n")
	}
	for _, k := range unbind {
		for _, v := range luaKeys(k) {
			fmt.Fprintf(b, "pcall(hl.unbind, %s)\n", luaString(v)) // in case one is not bound
		}
	}
	for _, sc := range c.Shortcuts {
		cmd := ShortcutCommand(c, sc)
		if sc.Keys == "" || cmd == "" {
			continue
		}
		fmt.Fprintf(b, "hl.bind(%s, hl.dsp.exec_cmd(%s), { description = %s })\n",
			luaString(luaKeys(sc.Keys)[0]), luaString(cmd), luaString(shortcutLabel(sc)))
	}
	fmt.Fprintf(b, "\n-- While inua's settings window records a shortcut, no other is active.\nhl.define_submap(%s, function()\n    hl.bind(\"SUPER + CTRL + ALT + Escape\", hl.dsp.submap(\"reset\"))\nend)\n", luaString(RecordSubmap))
}
