// Package desktop reads XDG desktop entries and launches them; it backs the
// DesktopEntries / DesktopEntry QML types.
package desktop

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Entry is an application from a .desktop file.
type Entry struct {
	ID            string // "org.gnome.Nautilus" for org.gnome.Nautilus.desktop
	Name          string
	GenericName   string
	Comment       string
	Icon          string
	ExecString    string // raw Exec value
	Command       []string
	WorkingDir    string
	RunInTerminal bool
	Categories    []string
	Keywords      []string
	NoDisplay     bool
	WMClass       string   // StartupWMClass: the class of the app's windows
	MimeTypes     []string // the types it opens: "audio/flac", "x-scheme-handler/http"
	Path          string   // file the entry came from
	Actions       []Action // its own actions (Firefox's New Private Window), in order
}

var inuaIconAliases = map[string][]string{
	"Assistant":       {"chat", "help-browser"},
	"Calculator":      {"accessories-calculator", "org.gnome.Calculator"},
	"Calendar":        {"x-office-calendar", "org.gnome.Calendar"},
	"ChineseCheckers": {"applications-games"},
	"Clock":           {"clock", "preferences-system-time"},
	"ColorPicker":     {"color-picker", "applications-graphics"},
	"DiskUsage":       {"baobab", "drive-harddisk"},
	"Files":           {"system-file-manager", "org.gnome.Nautilus"},
	"GPG":             {"seahorse", "dialog-password"},
	"GameLobby":       {"applications-games"},
	"Launcher":        {"system-search"},
	"Mail":            {"internet-mail", "mail-client"},
	"Minesweeper":     {"applications-games"},
	"MobileLink":      {"phone", "phone-symbolic"},
	"Music":           {"multimedia-player", "audio-player"},
	"Pass":            {"dialog-password", "password-manager"},
	"Remote":          {"preferences-desktop-remote", "network-server"},
	"Screenshot":      {"applets-screenshooter", "org.gnome.Screenshot"},
	"Settings":        {"preferences-system"},
	"SmallFortune":    {"applications-games"},
	"SpacePinball":    {"applications-games"},
	"Stocks":          {"office-chart-line", "finance"},
	"SystemMonitor":   {"utilities-system-monitor", "org.gnome.SystemMonitor"},
	"Tailscale":       {"network-vpn", "network-workgroup"},
	"Term":            {"utilities-terminal", "org.gnome.Terminal"},
	"Term.Quick":      {"utilities-terminal", "org.gnome.Terminal"},
	"Video":           {"multimedia-player", "video-x-generic"},
	"Viewer":          {"image-viewer", "x-office-document"},
	"VM":              {"computer", "system-run"},
	"Weather":         {"weather", "weather-clear"},
	"Workshop":        {"applications-development", "text-x-script"},
}

// InuaIconAliases returns standard theme icon names for an inua app icon ID.
// Third-party themes do not know inua's IDs, so callers use these names to
// replace inua's app-specific artwork with icons from the selected theme.
func InuaIconAliases(icon string) []string {
	id, ok := strings.CutPrefix(icon, "io.github.lyricat.Inua.")
	if !ok {
		return nil
	}
	return inuaIconAliases[id]
}

// Action is one of an app's own actions, a [Desktop Action ID] group the
// Actions key lists.
type Action struct {
	ID, Name string
	Command  []string
}

// Parse reads the [Desktop Entry] group and the action groups it lists.
// locale is like "zh_CN.UTF-8"; "" selects untranslated values. Entries
// that are not applications, or are Hidden, return an error.
func Parse(r io.Reader, id, locale string) (*Entry, error) {
	vals := map[string]string{}
	groups := map[string]map[string]string{} // the action groups, by ID
	var group map[string]string              // the group being read; nil for another
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		if line[0] == '[' {
			group = nil
			if line == "[Desktop Entry]" {
				group = vals
			} else if a, ok := strings.CutPrefix(line, "[Desktop Action "); ok {
				group = map[string]string{}
				groups[strings.TrimSuffix(a, "]")] = group
			}
			continue
		}
		if group == nil {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		group[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	if vals["Type"] != "Application" {
		return nil, fmt.Errorf("%s: not an application", id)
	}
	if vals["Hidden"] == "true" {
		return nil, fmt.Errorf("%s: hidden", id)
	}
	in := func(vals map[string]string, key string) string {
		for _, l := range localeKeys(locale) {
			if v, ok := vals[key+"["+l+"]"]; ok {
				return unescape(v)
			}
		}
		return unescape(vals[key])
	}
	get := func(key string) string { return in(vals, key) }
	e := &Entry{
		ID:            id,
		Name:          get("Name"),
		GenericName:   get("GenericName"),
		Comment:       get("Comment"),
		Icon:          unescape(vals["Icon"]),
		ExecString:    unescape(vals["Exec"]),
		WorkingDir:    unescape(vals["Path"]),
		RunInTerminal: vals["Terminal"] == "true",
		Categories:    splitList(vals["Categories"]),
		Keywords:      splitList(get("Keywords")),
		NoDisplay:     vals["NoDisplay"] == "true",
		WMClass:       unescape(vals["StartupWMClass"]),
		MimeTypes:     splitList(vals["MimeType"]),
	}
	if e.Name == "" {
		return nil, fmt.Errorf("%s: no Name", id)
	}
	if e.ExecString != "" {
		cmd, err := ParseExec(e.ExecString, e)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", id, err)
		}
		e.Command = cmd
	}
	for _, a := range splitList(vals["Actions"]) {
		g := groups[a]
		name := in(g, "Name")
		cmd, err := ParseExec(unescape(g["Exec"]), e)
		if name == "" || err != nil || len(cmd) == 0 {
			continue
		}
		e.Actions = append(e.Actions, Action{ID: a, Name: name, Command: cmd})
	}
	return e, nil
}

// localeKeys returns lookup keys for "lang_COUNTRY.ENCODING@MODIFIER" in the
// order the spec prescribes.
func localeKeys(locale string) []string {
	if locale == "" || locale == "C" || locale == "POSIX" {
		return nil
	}
	locale, _, _ = strings.Cut(locale, ".")
	base, mod, hasMod := strings.Cut(locale, "@")
	lang, country, hasCountry := strings.Cut(base, "_")
	var keys []string
	if hasCountry && hasMod {
		keys = append(keys, lang+"_"+country+"@"+mod)
	}
	if hasCountry {
		keys = append(keys, lang+"_"+country)
	}
	if hasMod {
		keys = append(keys, lang+"@"+mod)
	}
	return append(keys, lang)
}

func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 's':
			b.WriteByte(' ')
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '\\':
			b.WriteByte('\\')
		default:
			// Keep unknown escapes (e.g. "\;" in lists, Exec quoting).
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ";") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ParseExec splits an Exec value into argv and expands field codes. File and
// URL codes (%f %F %u %U) are dropped since the launcher passes no files.
func ParseExec(exec string, e *Entry) ([]string, error) {
	args, err := splitExec(exec)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, a := range args {
		switch a {
		case "%f", "%F", "%u", "%U", "%d", "%D", "%n", "%N", "%v", "%m":
			continue
		case "%i":
			if e != nil && e.Icon != "" {
				out = append(out, "--icon", e.Icon)
			}
			continue
		}
		var b strings.Builder
		for i := 0; i < len(a); i++ {
			if a[i] != '%' || i+1 == len(a) {
				b.WriteByte(a[i])
				continue
			}
			i++
			switch a[i] {
			case '%':
				b.WriteByte('%')
			case 'c':
				if e != nil {
					b.WriteString(e.Name)
				}
			case 'k':
				if e != nil {
					b.WriteString(e.Path)
				}
			}
			// Other codes inside a word are removed.
		}
		out = append(out, b.String())
	}
	if len(out) == 0 {
		return nil, errors.New("empty Exec")
	}
	return out, nil
}

// DataDirs returns application directories in precedence order.
// Stamp sums up the times of the applications folders dirs (as DataDirs
// gives them): it changes when an entry is added or removed there, so the
// entries can be scanned again only then.
func Stamp(dirs []string) string {
	var b strings.Builder
	for _, d := range dirs {
		if info, err := os.Stat(d); err == nil {
			b.WriteString(info.ModTime().String())
		}
		b.WriteByte(0)
	}
	return b.String()
}

func DataDirs() []string {
	home := os.Getenv("XDG_DATA_HOME")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, ".local", "share")
		}
	}
	dirs := os.Getenv("XDG_DATA_DIRS")
	if dirs == "" {
		dirs = "/usr/local/share:/usr/share"
	}
	var out []string
	if home != "" {
		out = append(out, filepath.Join(home, "applications"))
	}
	for _, d := range filepath.SplitList(dirs) {
		if d != "" {
			out = append(out, filepath.Join(d, "applications"))
		}
	}
	return out
}

// Scan loads all applications from dirs. An ID found in an earlier dir hides
// later ones, including when the earlier file is Hidden. Unparseable files are
// skipped. The result is sorted by ID.
func Scan(dirs []string, locale string) []*Entry {
	seen := map[string]bool{}
	var out []*Entry
	for _, dir := range dirs {
		filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".desktop") {
				return nil
			}
			rel, _ := filepath.Rel(dir, path)
			id := strings.TrimSuffix(strings.ReplaceAll(rel, string(filepath.Separator), "-"), ".desktop")
			if seen[id] {
				return nil
			}
			seen[id] = true
			f, err := os.Open(path)
			if err != nil {
				return nil
			}
			defer f.Close()
			if e, err := Parse(f, id, locale); err == nil {
				e.Path = path
				if e.ExecString != "" {
					e.Command, _ = ParseExec(e.ExecString, e)
				}
				out = append(out, e)
			}
			return nil
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// splitExec splits an Exec value into its words, with the specification's
// quoting.
func splitExec(exec string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inArg, quoted := false, false
	for i := 0; i < len(exec); i++ {
		c := exec[i]
		switch {
		case quoted && c == '\\' && i+1 < len(exec):
			i++
			cur.WriteByte(exec[i])
		case c == '"':
			quoted = !quoted
			inArg = true
		case !quoted && (c == ' ' || c == '\t'):
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteByte(c)
			inArg = true
		}
	}
	if quoted {
		return nil, errors.New("unterminated quote in Exec")
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args, nil
}

// ExecFiles is the command lines that open paths with an entry, as the
// Desktop Entry specification's field codes say: %F and %U take every
// file in one command; %f and %u one, so each file gets a command of its
// own; an Exec with no file code gets the paths at its end.
func ExecFiles(exec string, e *Entry, paths []string) ([][]string, error) {
	base, err := splitExec(exec)
	if err != nil {
		return nil, err
	}
	uris := make([]string, len(paths))
	for i, p := range paths {
		uris[i] = (&url.URL{Scheme: "file", Path: p}).String()
	}
	single, many := false, false
	for _, a := range base {
		switch a {
		case "%f", "%u":
			single = true
		case "%F", "%U":
			many = true
		}
	}
	expand := func(files, urls []string) ([]string, error) {
		var out []string
		for _, a := range base {
			switch a {
			case "%f":
				out = append(out, files[:min(1, len(files))]...)
			case "%u":
				out = append(out, urls[:min(1, len(urls))]...)
			case "%F":
				out = append(out, files...)
			case "%U":
				out = append(out, urls...)
			default:
				fixed, err := ParseExec(quoteArg(a), e)
				if err == nil {
					out = append(out, fixed...)
				}
			}
		}
		if !single && !many {
			out = append(out, files...)
		}
		if len(out) == 0 {
			return nil, errors.New("empty Exec")
		}
		return out, nil
	}
	if single && !many && len(paths) > 1 {
		var cmds [][]string
		for i := range paths {
			c, err := expand(paths[i:i+1], uris[i:i+1])
			if err != nil {
				return nil, err
			}
			cmds = append(cmds, c)
		}
		return cmds, nil
	}
	c, err := expand(paths, uris)
	if err != nil {
		return nil, err
	}
	return [][]string{c}, nil
}

// quoteArg quotes one argument for ParseExec, which then expands its
// other field codes (%i, %c, %k).
func quoteArg(a string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(a) + `"`
}

// Find reads the entry with desktop ID id ("org.gnome.TextEditor", with or
// without ".desktop") from the first of dirs (as DataDirs gives them) that
// has it. A dash in the ID may stand for a subfolder: kde-foo.desktop is
// kde/foo.desktop.
func Find(dirs []string, id, locale string) (*Entry, error) {
	id = strings.TrimSuffix(id, ".desktop")
	for _, d := range dirs {
		rel := id + ".desktop"
		for {
			if f, err := os.Open(filepath.Join(d, rel)); err == nil {
				e, err := Parse(f, id, locale)
				f.Close()
				if err == nil {
					e.Path = filepath.Join(d, rel)
				}
				return e, err
			}
			i := strings.Index(rel, "-")
			if i < 0 {
				break
			}
			rel = rel[:i] + "/" + rel[i+1:]
		}
	}
	return nil, fmt.Errorf("%s: %w", id, os.ErrNotExist)
}
