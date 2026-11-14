// Package settings is inua-term's own settings, ~/.config/inua/term.toml
// (docs/terminal.md): the window's theme and the terminals' colours, the
// font, the cursor, copying, scrollback, the host's replay, the shell and
// its integration, agents' notifications, remembering commands, and the
// keys. inua's icons and text size follow ~/.config/inua/config.toml.
package settings

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/scheme"
)

// Version is the settings' version: a file of an older one is brought up
// to it as it loads.
const Version = 3

// Settings is the file's content.
type Settings struct {
	Version  int      `toml:"version"`
	Look     Look     `toml:"look"`
	Font     Font     `toml:"font"`
	Cursor   Cursor   `toml:"cursor"`
	Terminal Terminal `toml:"terminal"`
	Shell    Shell    `toml:"shell"`
	Agents   Agents   `toml:"agents"`
	Remember Remember `toml:"remember"`
	Web      Web      `toml:"web"`
	// Keys are inua-term's chords by action ("ctrl+shift+t"); "" turns
	// one off.
	Keys map[string]string `toml:"keys"`
}

type Look struct {
	// Theme is the window's theme, one of inua's; "" follows inua's.
	Theme string `toml:"theme"`
	// Scheme is the terminals' colours (internal/term/scheme); "" takes
	// them from the window's theme.
	Scheme string `toml:"scheme"`
}

type Font struct {
	Family string  `toml:"family"`
	Size   float64 `toml:"size"`
}

type Cursor struct {
	Style string `toml:"style"` // block, bar or underline
	Blink bool   `toml:"blink"`
}

type Terminal struct {
	CopyOnSelect      bool `toml:"copy_on_select"`
	FocusFollowsMouse bool `toml:"focus_follows_mouse"`
	Scrollback        int  `toml:"scrollback"` // lines kept per pane
	Replay            int  `toml:"replay"`     // MiB of a pane's output the host keeps
}

type Shell struct {
	// Program is the shell new panes start; "" is $SHELL.
	Program     string `toml:"program"`
	Integration bool   `toml:"integration"`
}

type Agents struct {
	Notifications bool `toml:"notifications"`
}

type Remember struct {
	Commands bool     `toml:"commands"`
	Forget   []string `toml:"forget"`
}

// Web is the Web Terminal (inua-term serve, internal/term/webterm): on,
// started again at login; on this computer only, or on its Tailscale
// address; at this port.
type Web struct {
	On        bool `toml:"on"`
	Tailscale bool `toml:"tailscale"`
	Port      int  `toml:"port"`
}

// Actions are the keys' actions, in the order the settings show them,
// with their chords by default: Ghostty's where Ghostty has the action (a
// window of Ghostty's is a workspace here, a tab a session, a split a
// pane), Ctrl+Shift chords for the rest; but a new session is Ctrl+Shift+N,
// the one made most, and a new workspace has none (its button, the
// palette). A chord names its modifiers in the order ctrl, shift, alt,
// super.
var Actions = []struct{ Name, Keys, Label string }{
	{"new_session", "ctrl+shift+n", i18n.M("New session in this workspace")},
	{"new_workspace", "", i18n.M("New workspace")},
	{"split_right", "ctrl+shift+o", i18n.M("Split right")},
	{"split_down", "ctrl+shift+e", i18n.M("Split down")},
	{"close_pane", "ctrl+shift+w", i18n.M("Close pane")},
	{"swap", "ctrl+shift+x", i18n.M("Swap with the pane in a direction (then an arrow)")},
	{"zoom", "ctrl+shift+return", i18n.M("Zoom pane / back")},
	{"equalize", "ctrl+shift+super+equal", i18n.M("Equalize")},
	{"next_layout", "ctrl+shift+l", i18n.M("Next layout")},
	{"previous_session", "ctrl+shift+tab", i18n.M("Previous session")},
	{"next_session", "ctrl+tab", i18n.M("Next session")},
	{"next_waiting", "ctrl+shift+j", i18n.M("Next agent that needs you")},
	{"next_idle", "ctrl+grave", i18n.M("Next agent that needs you or is working")},
	{"agents", "ctrl+shift+a", i18n.M("Every agent")},
	{"fold_workspaces", "ctrl+shift+b", i18n.M("Fold the workspaces")},
	{"fold_sessions", "ctrl+shift+s", i18n.M("Fold the sessions")},
	{"copy", "ctrl+shift+c", i18n.M("Copy")},
	{"paste", "ctrl+shift+v", i18n.M("Paste")},
	{"find", "ctrl+shift+f", i18n.M("Search the scrollback")},
	{"copy_mode", "ctrl+shift+space", i18n.M("Copy mode")},
	{"rename", "ctrl+shift+r", i18n.M("Rename the session")},
	{"palette", "ctrl+shift+p", i18n.M("Every action by name")},
	{"keys", "ctrl+shift+slash", i18n.M("Show the keys")},
	{"settings", "ctrl+comma", i18n.M("Settings")},
	{"restart_host", "", i18n.M("Restart the terminal host")},
	{"font_larger", "ctrl+equal", i18n.M("Font larger")},
	{"font_smaller", "ctrl+minus", i18n.M("Font smaller")},
	{"font_reset", "ctrl+0", i18n.M("Font size as set")},
}

// firstKeys are the first version's default chords that are not the
// defaults now.
var firstKeys = map[string]string{
	"split_right": "ctrl+shift+d", "zoom": "ctrl+shift+z", "equalize": "ctrl+shift+equal",
	"previous_session": "ctrl+shift+bracketleft", "next_session": "ctrl+shift+bracketright",
	"settings": "ctrl+shift+comma",
}

// secondKeys are the second version's default chords that are not the
// defaults now: Ghostty's tab and window.
var secondKeys = map[string]string{"new_session": "ctrl+shift+t", "new_workspace": "ctrl+shift+n"}

// macKeys are the default chords on macOS, on Command ("super") as a
// Mac app's and Ghostty's there; Control stays the programs'. Ctrl+` is
// kept: Command+` is the system's, between an app's windows.
var macKeys = map[string]string{
	"new_session": "super+t", "new_workspace": "super+n",
	"split_right": "super+d", "split_down": "shift+super+d", "close_pane": "super+w",
	"swap": "shift+super+x", "zoom": "shift+super+return", "equalize": "ctrl+super+equal",
	"next_layout": "shift+super+l", "previous_session": "shift+super+bracketleft",
	"next_session": "shift+super+bracketright", "next_waiting": "shift+super+j",
	"agents": "shift+super+a", "fold_workspaces": "shift+super+b", "fold_sessions": "shift+super+s",
	"copy": "super+c", "paste": "super+v", "find": "super+f", "copy_mode": "shift+super+space",
	"rename": "shift+super+r", "palette": "shift+super+p", "keys": "shift+super+slash",
	"settings": "super+comma", "font_larger": "super+equal", "font_smaller": "super+minus",
	"font_reset": "super+0",
}

// Default is the settings without a file.
func Default() Settings {
	keys := map[string]string{}
	for _, a := range Actions {
		keys[a.Name] = a.Keys
	}
	// The font: Geist Mono as the shell's; on a Mac, the system's
	// monospace, which every Mac has.
	font := "Geist Mono"
	if runtime.GOOS == "darwin" {
		maps.Copy(keys, macKeys)
		font = "Menlo"
	}
	return Settings{
		Version:  Version,
		Font:     Font{Family: font, Size: 11},
		Cursor:   Cursor{Style: "block"},
		Terminal: Terminal{CopyOnSelect: true, Scrollback: 10000, Replay: 4},
		Shell:    Shell{Integration: true},
		Agents:   Agents{Notifications: true},
		Remember: Remember{Commands: true, Forget: []string{"*password*", "*token=*", "*secret*"}},
		Web:      Web{Port: 7681},
		Keys:     keys,
	}
}

// Path is $XDG_CONFIG_HOME/inua/term.toml.
func Path() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "inua", "term.toml")
}

// Exists says there is a settings file at path: inua-term ran before.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, fs.ErrNotExist)
}

// Load reads path over the defaults; a missing file is the defaults. The
// error is for a file that cannot be read or parsed (the defaults come
// back then); warnings name unknown keys and invalid values, replaced by
// their defaults.
func Load(path string) (Settings, []string, error) {
	s := Default()
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil, nil
	}
	if err != nil {
		return s, nil, err
	}
	s.Version = 0 // a file without one is of the first
	md, err := toml.Decode(string(b), &s)
	if err != nil {
		return Default(), nil, fmt.Errorf("%s: %w", path, err)
	}
	d := Default()
	if s.Version < 2 {
		// The keys the file kept at the first version's defaults take
		// the new ones, Ghostty's.
		for name, old := range firstKeys {
			if s.Keys[name] == old {
				s.Keys[name] = d.Keys[name]
			}
		}
	}
	if s.Version < 3 {
		// A new session moves to Ctrl+Shift+N when the file kept both at
		// the second version's: one the user had changed keeps its chord,
		// and the other with it, so no two share one.
		if s.Keys["new_session"] == secondKeys["new_session"] && s.Keys["new_workspace"] == secondKeys["new_workspace"] {
			s.Keys["new_session"], s.Keys["new_workspace"] = d.Keys["new_session"], d.Keys["new_workspace"]
		}
	}
	s.Version = Version
	var w []string
	unknown := slices.DeleteFunc(md.Undecoded(), func(k toml.Key) bool {
		return k.String() == "agents.hooks" // the first version's, which added agents' hooks
	})
	for _, k := range unknown {
		// An unknown table once, not each key in it.
		if slices.ContainsFunc(unknown, func(o toml.Key) bool { return strings.HasPrefix(k.String(), o.String()+".") }) {
			continue
		}
		w = append(w, fmt.Sprintf("unknown setting %s", k))
	}
	return s, append(w, s.validate()...), nil
}

var chordRe = regexp.MustCompile(`^((ctrl|shift|alt|super)\+)*[a-z0-9_]+$`)

// ValidKeys says s is a chord as the file writes them, or "" for none.
func ValidKeys(s string) bool { return s == "" || chordRe.MatchString(s) }

func (s *Settings) validate() []string {
	d := Default()
	var w []string
	if s.Look.Scheme != "" {
		if _, ok := scheme.Find(s.Look.Scheme); !ok {
			w = append(w, fmt.Sprintf("look.scheme = %q: no such scheme; using the theme's colours", s.Look.Scheme))
			s.Look.Scheme = ""
		}
	}
	if strings.TrimSpace(s.Font.Family) == "" {
		w = append(w, fmt.Sprintf("font.family is empty; using %q", d.Font.Family))
		s.Font.Family = d.Font.Family
	}
	if s.Font.Size < 6 || s.Font.Size > 48 {
		w = append(w, fmt.Sprintf("font.size = %v: want 6..48; using %v", s.Font.Size, d.Font.Size))
		s.Font.Size = d.Font.Size
	}
	if !slices.Contains([]string{"block", "bar", "underline"}, s.Cursor.Style) {
		w = append(w, fmt.Sprintf("cursor.style = %q: want block, bar or underline; using %q", s.Cursor.Style, d.Cursor.Style))
		s.Cursor.Style = d.Cursor.Style
	}
	if s.Terminal.Scrollback < 0 || s.Terminal.Scrollback > 1000000 {
		w = append(w, fmt.Sprintf("terminal.scrollback = %d: want 0..1000000 lines; using %d", s.Terminal.Scrollback, d.Terminal.Scrollback))
		s.Terminal.Scrollback = d.Terminal.Scrollback
	}
	if s.Web.Port < 1024 || s.Web.Port > 65535 {
		w = append(w, fmt.Sprintf("web.port = %d: want 1024..65535; using %d", s.Web.Port, d.Web.Port))
		s.Web.Port = d.Web.Port
	}
	if s.Terminal.Replay < 1 || s.Terminal.Replay > 64 {
		w = append(w, fmt.Sprintf("terminal.replay = %d: want 1..64 MiB; using %d", s.Terminal.Replay, d.Terminal.Replay))
		s.Terminal.Replay = d.Terminal.Replay
	}
	for name, k := range s.Keys {
		if !slices.ContainsFunc(Actions, func(a struct{ Name, Keys, Label string }) bool { return a.Name == name }) {
			w = append(w, fmt.Sprintf("keys.%s: no such action", name))
			delete(s.Keys, name)
			continue
		}
		if !ValidKeys(k) {
			w = append(w, fmt.Sprintf("keys.%s = %q: want modifiers and a key, as \"ctrl+shift+t\"; using %q", name, k, d.Keys[name]))
			s.Keys[name] = d.Keys[name]
		}
	}
	for name, k := range d.Keys {
		if _, ok := s.Keys[name]; !ok {
			s.Keys[name] = k
		}
	}
	return w
}

// Format is the settings as the file writes them, with what each means.
func Format(s Settings) string {
	var b strings.Builder
	b.WriteString(`# inua-term's settings. inua's icons and text size follow
# ~/.config/inua/config.toml; this file is inua-term's own, written by its
# settings (Ctrl+,) and read when it starts.
version = ` + fmt.Sprint(s.Version) + `

[look]
theme = ` + q(s.Look.Theme) + ` # the window's theme, one of inua's; "" follows inua's
scheme = ` + q(s.Look.Scheme) + ` # the terminals' colours: ` + schemeNames() + `; "" takes the theme's

[font]
family = ` + q(s.Font.Family) + ` # a monospace family; Pango falls back for CJK
size = ` + fmt.Sprint(s.Font.Size) + `

[cursor]
style = ` + q(s.Cursor.Style) + ` # block, bar or underline
blink = ` + fmt.Sprint(s.Cursor.Blink) + `

[terminal]
copy_on_select = ` + fmt.Sprint(s.Terminal.CopyOnSelect) + ` # selecting copies to the clipboard
focus_follows_mouse = ` + fmt.Sprint(s.Terminal.FocusFollowsMouse) + ` # focus a pane when the pointer enters it; off by default
scrollback = ` + fmt.Sprint(s.Terminal.Scrollback) + ` # lines kept per pane
replay = ` + fmt.Sprint(s.Terminal.Replay) + ` # MiB of a pane's recent output the host keeps for a window opened later

[shell]
program = ` + q(s.Shell.Program) + ` # the shell new panes start; "" is $SHELL
integration = ` + fmt.Sprint(s.Shell.Integration) + ` # the prompt marks for bash, zsh and fish, for remembering commands

[agents]
notifications = ` + fmt.Sprint(s.Agents.Notifications) + ` # when an agent needs you or is done in a pane you are not looking at

[remember]
commands = ` + fmt.Sprint(s.Remember.Commands) + ` # put each pane's last command back at its prompt, not run
forget = ` + list(s.Remember.Forget) + ` # commands never kept

[web]
on = ` + fmt.Sprint(s.Web.On) + ` # the Web Terminal: the terminals in a browser, started again at login
tailscale = ` + fmt.Sprint(s.Web.Tailscale) + ` # on this computer's Tailscale address, for a phone; else on this computer only
port = ` + fmt.Sprint(s.Web.Port) + `

[keys]
# inua-term's chords; "" turns one off.
`)
	for _, a := range Actions {
		fmt.Fprintf(&b, "%s = %s # %s\n", a.Name, q(s.Keys[a.Name]), a.Label)
	}
	return b.String()
}

func schemeNames() string {
	names := make([]string, len(scheme.All))
	for i, sc := range scheme.All {
		names[i] = sc.Name
	}
	return strings.Join(names, ", ")
}

func q(s string) string { return fmt.Sprintf("%q", s) }

func list(items []string) string {
	qs := make([]string, len(items))
	for i, s := range items {
		qs[i] = q(s)
	}
	return "[" + strings.Join(qs, ", ") + "]"
}

// Save writes the settings to path, mode 0600, through a file renamed
// over the old one.
func Save(path string, s Settings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(Format(s)), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Shells are the login shells installed, as /etc/shells (its content
// etc) lists them: those present and runnable, one path each (/bin/bash
// and /usr/bin/bash are one where /bin is /usr/bin), nologin and false
// left out. $SHELL comes first when it is one of them.
func Shells(etc string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		// The folder resolved, not the program: sh may be bash, and is
		// sh all the same.
		dir, err := filepath.EvalSymlinks(filepath.Dir(p))
		key := filepath.Join(dir, filepath.Base(p))
		if err != nil || seen[key] {
			return
		}
		if base := filepath.Base(p); base == "nologin" || base == "false" || base == "true" {
			return
		}
		if fi, err := os.Stat(p); err != nil || fi.IsDir() || fi.Mode()&0o111 == 0 {
			return
		}
		seen[key] = true
		out = append(out, p)
	}
	if sh := os.Getenv("SHELL"); filepath.IsAbs(sh) {
		add(sh)
	}
	for line := range strings.SplitSeq(etc, "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, "/") {
			add(line)
		}
	}
	return out
}
