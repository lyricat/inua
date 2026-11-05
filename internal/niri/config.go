package niri

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/lyricat/inua/internal/compositor"
)

// IncludeFile is the file inua writes its settings to, beside niri's
// config.kdl, which includes it: the last include wins, so it goes last.
const IncludeFile = "inua.kdl"

// IncludeLine is what config.kdl needs for inua's settings to apply; the
// optional form keeps the config valid before inua has written the file.
const IncludeLine = `include optional=true "` + IncludeFile + `"`

// ConfigPath is niri's config: $NIRI_CONFIG, else config.kdl in the
// user's niri directory.
func ConfigPath() string {
	if p := os.Getenv("NIRI_CONFIG"); p != "" {
		return p
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "niri", "config.kdl")
}

var includeRe = regexp.MustCompile(`(?m)^\s*include\b[^\n/]*"(?:[^"\n]*/)?` + regexp.QuoteMeta(IncludeFile) + `"`)

// Includes reports whether a config's text includes inua's file.
func Includes(config string) bool { return includeRe.MatchString(config) }

// included reports whether niri's config includes inua's file now.
func included() bool {
	data, err := os.ReadFile(ConfigPath())
	return err == nil && Includes(string(data))
}

// kdl quotes a string for KDL.
func kdl(s string) string { return strconv.Quote(s) }

// Keys writes a shortcut as niri's binds name it ("Super+Shift+S"); false
// for one niri cannot bind (a mouse button).
func Keys(keys string) (string, bool) {
	c, ok := compositor.ParseKeys(keys)
	if !ok || strings.Contains(c.Key, ":") {
		return "", false
	}
	var parts []string
	for _, m := range []struct {
		on   bool
		name string
	}{{c.Super, "Super"}, {c.Ctrl, "Ctrl"}, {c.Alt, "Alt"}, {c.Shift, "Shift"}} {
		if m.on {
			parts = append(parts, m.name)
		}
	}
	return strings.Join(append(parts, c.Key), "+"), true
}

var xkbBlock = regexp.MustCompile(`\bxkb\s*\{`)

// XKB is what the xkb block of a niri config sets besides the options: its
// statements, such as `layout "us,de"`. niri replaces the block as a whole
// when an included file has one (keyboard's other settings merge), so
// inua.kdl repeats these beside the options it sets.
func XKB(config string) []string {
	loc := xkbBlock.FindStringIndex(config)
	if loc == nil {
		return nil
	}
	body, depth := config[loc[1]:], 1
	end := -1
	for i, r := range body {
		switch r {
		case '{':
			depth++
		case '}':
			depth--
		}
		if depth == 0 {
			end = i
			break
		}
	}
	if end < 0 {
		return nil
	}
	var out []string
	for _, st := range strings.FieldsFunc(body[:end], func(r rune) bool { return r == '\n' || r == ';' }) {
		st = strings.TrimSpace(st)
		if st == "" || strings.HasPrefix(st, "//") || strings.HasPrefix(st, "options") {
			continue
		}
		out = append(out, st)
	}
	return out
}

// Render is inua.kdl for the settings: the keyboard's XKB options (with
// xkb, the rest of the main config's xkb block), the touchpad's speed and
// profile, the shortcuts. A setting left to niri is not written. Binds
// niri cannot express are left out and returned.
func Render(s compositor.Settings, xkb []string) (text string, skipped []string) {
	var b strings.Builder
	b.WriteString("// Written by inua from its settings: change them there, not here.\n")
	b.WriteString("// niri's config.kdl includes this file last, so these win.\n")
	if s.KbOptions != nil || s.TouchpadSpeed != nil || s.TouchpadAccel != "" {
		b.WriteString("input {\n")
		if s.KbOptions != nil {
			b.WriteString("    keyboard {\n        xkb {\n")
			for _, st := range xkb {
				fmt.Fprintf(&b, "            %s\n", st)
			}
			fmt.Fprintf(&b, "            options %s\n        }\n    }\n", kdl(*s.KbOptions))
		}
		if s.TouchpadSpeed != nil || s.TouchpadAccel != "" {
			b.WriteString("    touchpad {\n")
			if s.TouchpadSpeed != nil {
				fmt.Fprintf(&b, "        accel-speed %s\n", strconv.FormatFloat(*s.TouchpadSpeed, 'f', 2, 64))
			}
			if s.TouchpadAccel != "" {
				fmt.Fprintf(&b, "        accel-profile %s\n", kdl(s.TouchpadAccel))
			}
			b.WriteString("    }\n")
		}
		b.WriteString("}\n")
	}
	var binds []string
	for _, kb := range s.Binds {
		if kb.Keys == "" || kb.Command == "" {
			continue
		}
		keys, ok := Keys(kb.Keys)
		if !ok {
			skipped = append(skipped, kb.Keys)
			continue
		}
		binds = append(binds, fmt.Sprintf("    %s { spawn \"sh\" \"-c\" %s; }\n", keys, kdl(kb.Command)))
	}
	if len(binds) > 0 {
		b.WriteString("binds {\n" + strings.Join(binds, "") + "}\n")
	}
	return b.String(), skipped
}

// ApplySettings writes inua.kdl beside niri's config when it changed; niri
// loads its config again by itself. Without the include line in
// config.kdl the file is written all the same, ready for it.
func (b *Backend) ApplySettings(_ context.Context, s compositor.Settings) error {
	main, _ := os.ReadFile(ConfigPath())
	text, skipped := Render(s, XKB(string(main)))
	path := filepath.Join(filepath.Dir(ConfigPath()), IncludeFile)
	if old, err := os.ReadFile(path); err != nil || string(old) != text {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
			return err
		}
		if err := os.Rename(tmp, path); err != nil {
			return err
		}
	}
	if len(skipped) > 0 {
		return fmt.Errorf("niri cannot bind %s", strings.Join(skipped, ", "))
	}
	return nil
}

// Source names where the settings go.
func (b *Backend) Source() string { return "niri's " + IncludeFile }

// Hint: the settings apply once config.kdl includes inua's file.
func (b *Backend) Hint(c compositor.Capability) string {
	switch c {
	case compositor.CapKeyboard, compositor.CapTouchpad, compositor.CapBinds:
		if !included() {
			return "Add this as the last line of niri's config.kdl: " + IncludeLine
		}
	}
	return ""
}

var (
	bindsBlock = regexp.MustCompile(`(?m)^\s*binds\s*\{`)
	oneBind    = regexp.MustCompile(`([A-Za-z0-9_]+(?:\+[A-Za-z0-9_]+)*)\s*(?:[a-z-]+=\S+\s*)*\{\s*([^{}]*?)\s*;?\s*\}`)
	quoted     = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)
	comment    = regexp.MustCompile(`(?m)^\s*//.*$`)
)

// block is the text between the brace that ends at start and its match.
func block(text string, start int) (string, int) {
	depth := 1
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return text[start:i], i + 1
			}
		}
	}
	return text[start:], len(text)
}

// neutralKeys reads a niri bind's keys ("Mod+Shift+S") as inua writes
// them ("SUPER+SHIFT+S"); Mod is Super in a session of its own.
func neutralKeys(keys string) (string, bool) {
	parts := strings.Split(keys, "+")
	c := compositor.Combo{Key: parts[len(parts)-1]}
	for _, m := range parts[:len(parts)-1] {
		switch strings.ToLower(m) {
		case "mod", "super", "win":
			c.Super = true
		case "ctrl", "control":
			c.Ctrl = true
		case "alt":
			c.Alt = true
		case "shift":
			c.Shift = true
		default:
			return "", false // Mod3, Mod5, ISO_Level3_Shift: not ours to show
		}
	}
	return c.String(), true
}

// ParseBinds reads the binds blocks of a niri config: the keys and what
// each runs, a spawned command as one shell line.
func ParseBinds(config string) []compositor.Bind {
	config = comment.ReplaceAllString(config, "")
	var out []compositor.Bind
	for at := 0; ; {
		loc := bindsBlock.FindStringIndex(config[at:])
		if loc == nil {
			break
		}
		body, end := block(config, at+loc[1])
		at = end
		for _, m := range oneBind.FindAllStringSubmatch(body, -1) {
			keys, ok := neutralKeys(m[1])
			if !ok {
				continue
			}
			bind := compositor.Bind{Keys: keys, Action: m[2]}
			if rest, isSpawn := strings.CutPrefix(m[2], "spawn"); isSpawn {
				var args []string
				for _, q := range quoted.FindAllStringSubmatch(rest, -1) {
					if s, err := strconv.Unquote(`"` + q[1] + `"`); err == nil {
						args = append(args, s)
					}
				}
				if len(args) == 3 && args[0] == "sh" && args[1] == "-c" {
					args = args[2:]
				}
				bind.Command, bind.Action = strings.Join(args, " "), ""
			}
			out = append(out, bind)
		}
	}
	return out
}

// Binds are the shortcuts in niri's own config.kdl, not inua's file.
func (b *Backend) Binds(context.Context) ([]compositor.Bind, error) {
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		return nil, err
	}
	return ParseBinds(string(data)), nil
}

// Exit quits niri, ending the session.
func (b *Backend) Exit(ctx context.Context) error {
	return b.Client.Action(ctx, "Quit", map[string]any{"skip_confirmation": true})
}
