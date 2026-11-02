package desktop

import (
	"bufio"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/lyricat/inua/internal/i18n"
)

// Default apps: the app a kind of file or link opens with (a Role), the
// desktop's choice as freedesktop's MIME applications specification keeps
// it, in mimeapps.list under [Default Applications], read in its order
// and written to the user's own; GIO, the browsers and other file
// managers read the same. A terminal has no MIME type: its choice is the
// first line of xdg-terminals.list (xdg-terminal-exec's).

// Role is a kind of default app: what it is called, and the types it
// takes, the first the one that tells which apps can (an app that opens
// it is a candidate) and which is the default now.
type Role struct {
	ID, Name, About string
	Types           []string
	// Category marks the candidates when there is no type: an app in it
	// (TerminalEmulator).
	Category string
}

// Roles are the defaults the settings offer, in their order.
var Roles = []Role{
	{ID: "browser", Name: i18n.M("Web browser"), About: i18n.M("Opens links"),
		Types: []string{"x-scheme-handler/http", "x-scheme-handler/https", "text/html", "application/xhtml+xml"}},
	{ID: "email", Name: i18n.M("Email"), About: i18n.M("Opens mailto: links"),
		Types: []string{"x-scheme-handler/mailto"}},
	{ID: "files", Name: i18n.M("File manager"), About: i18n.M("Opens folders"),
		Types: []string{"inode/directory"}},
	{ID: "terminal", Name: i18n.M("Terminal"), About: i18n.M("Super+Return, and apps that run in a terminal"),
		Category: "TerminalEmulator"},
	{ID: "music", Name: i18n.M("Music"), About: i18n.M("Opens audio files"),
		Types: []string{"audio/mpeg", "audio/flac", "audio/x-flac", "audio/ogg", "audio/x-vorbis+ogg", "audio/opus", "audio/x-opus+ogg",
			"audio/mp4", "audio/x-m4a", "audio/aac", "audio/wav", "audio/x-wav", "audio/x-ms-wma", "audio/x-ape", "audio/x-wavpack",
			"audio/x-matroska", "audio/aiff", "audio/x-aiff", "audio/x-musepack", "audio/x-mpegurl", "audio/mpegurl"}},
	{ID: "video", Name: i18n.M("Video"), About: i18n.M("Opens video files"),
		Types: []string{"video/mp4", "video/x-matroska", "video/webm", "video/x-msvideo", "video/quicktime", "video/x-m4v", "video/mpeg",
			"video/x-ms-wmv", "video/x-flv", "video/ogg", "video/mp2t", "video/3gpp"}},
	{ID: "images", Name: i18n.M("Images"), About: i18n.M("Opens pictures"),
		Types: []string{"image/png", "image/jpeg", "image/gif", "image/webp", "image/avif", "image/bmp", "image/heic", "image/heif",
			"image/jxl", "image/svg+xml", "image/tiff", "image/vnd.microsoft.icon", "image/x-tga"}},
	{ID: "pdf", Name: i18n.M("PDF"), About: i18n.M("Opens PDF documents"),
		Types: []string{"application/pdf"}},
	{ID: "text", Name: i18n.M("Text editor"), About: i18n.M("Opens plain text"),
		Types: []string{"text/plain"}},
}

// Candidates are the apps that can be the role's default: those shown
// in menus that open its first type, or are in its category; by name.
func (r Role) Candidates(entries []*Entry) []*Entry {
	var out []*Entry
	for _, e := range entries {
		ok := slices.Contains(e.Categories, r.Category)
		if r.Category == "" {
			ok = slices.Contains(e.MimeTypes, r.Types[0])
		}
		if ok && !e.NoDisplay {
			out = append(out, e)
		}
	}
	slices.SortStableFunc(out, func(a, b *Entry) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return out
}

// MimeAppsPaths are the mimeapps.list files in their order: the user's
// config, the system's config, the user's and the system's applications
// directories, each a desktop's own list before the general one.
func MimeAppsPaths(desktops []string) []string {
	dirs := []string{configHome()}
	dirs = append(dirs, filepath.SplitList(cmpEnv("XDG_CONFIG_DIRS", "/etc/xdg"))...)
	dirs = append(dirs, DataDirs()...)
	var out []string
	for _, d := range dirs {
		for _, name := range desktops {
			out = append(out, filepath.Join(d, name+"-mimeapps.list"))
		}
		out = append(out, filepath.Join(d, "mimeapps.list"))
	}
	return out
}

// CurrentDesktops are XDG_CURRENT_DESKTOP's names, lower case.
func CurrentDesktops() []string {
	var out []string
	for _, d := range strings.Split(os.Getenv("XDG_CURRENT_DESKTOP"), ":") {
		if d != "" {
			out = append(out, strings.ToLower(d))
		}
	}
	return out
}

// Wayfire reports whether this is a Wayfire session: the apps Wayfire
// starts carry its socket, and the session's desktop names Wayfire.
func Wayfire() bool {
	if os.Getenv("WAYFIRE_SOCKET") != "" || os.Getenv("_WAYFIRE_SOCKET") != "" {
		return true
	}
	return slices.ContainsFunc(CurrentDesktops(), func(d string) bool {
		return strings.Contains(d, "wayfire")
	})
}

// UserMimeApps is the user's own mimeapps.list, where choices are written.
func UserMimeApps() string { return filepath.Join(configHome(), "mimeapps.list") }

// Default is the ID (no .desktop) of the app type opens with: the first
// installed app of the first file that names one. "" when none does.
func Default(paths []string, mime string, installed func(id string) bool) string {
	for _, p := range paths {
		for _, id := range readList(p, "Default Applications", mime) {
			id = strings.TrimSuffix(id, ".desktop")
			if installed(id) {
				return id
			}
		}
	}
	return ""
}

// SetDefault makes app id the default for types in the mimeapps.list at
// path, keeping the rest of the file as it was.
func SetDefault(path string, types []string, id string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(data) == 0 {
		lines = nil
	}
	want := map[string]bool{}
	for _, t := range types {
		want[t] = true
	}
	// Drop the types' lines in [Default Applications], then add them at
	// its end, the section made if need be.
	var out []string
	section, end := "", -1
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[") {
			if section == "Default Applications" {
				end = len(out)
			}
			section = strings.Trim(t, "[]")
		}
		if section == "Default Applications" && !strings.HasPrefix(t, "[") {
			if k, _, ok := strings.Cut(t, "="); ok && want[strings.TrimSpace(k)] {
				continue
			}
		}
		out = append(out, l)
	}
	if section == "Default Applications" {
		end = len(out)
	}
	var add []string
	for _, t := range types {
		add = append(add, t+"="+id+".desktop")
	}
	if end < 0 {
		if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}
		out = append(out, "[Default Applications]")
		end = len(out)
	}
	for end > 0 && strings.TrimSpace(out[end-1]) == "" { // before the blank line ending the section
		end--
	}
	out = slices.Insert(out, end, add...)
	return writeFile(path, strings.Join(out, "\n")+"\n")
}

// readList is key's value in section of an ini-like file, split at ';'.
func readList(path, section, key string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	in := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		t := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(t, "[") {
			in = t == "["+section+"]"
			continue
		}
		if k, v, ok := strings.Cut(t, "="); in && ok && strings.TrimSpace(k) == key {
			return splitList(strings.TrimSpace(v))
		}
	}
	return nil
}

// TerminalsPaths are the xdg-terminals.list files in their order: the
// user's config, then the system's, each a desktop's own list first.
func TerminalsPaths(desktops []string) []string {
	dirs := []string{configHome()}
	dirs = append(dirs, filepath.SplitList(cmpEnv("XDG_CONFIG_DIRS", "/etc/xdg"))...)
	var out []string
	for _, d := range dirs {
		for _, name := range desktops {
			out = append(out, filepath.Join(d, name+"-xdg-terminals.list"))
		}
		out = append(out, filepath.Join(d, "xdg-terminals.list"))
	}
	return out
}

// DefaultTerminal is the ID of the first installed terminal the lists
// name (a line is a desktop file's name; # starts a comment).
func DefaultTerminal(paths []string, installed func(id string) bool) string {
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, l := range strings.Split(string(data), "\n") {
			l = strings.TrimSpace(l)
			if l == "" || l[0] == '#' {
				continue
			}
			l, _, _ = strings.Cut(l, ":") // an action: "foot.desktop:server"
			if id := strings.TrimSuffix(l, ".desktop"); installed(id) {
				return id
			}
		}
	}
	return ""
}

// SetTerminal puts terminal id first in the xdg-terminals.list at path,
// keeping the others after it.
func SetTerminal(path, id string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	out := []string{id + ".desktop"}
	for _, l := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		name, _, _ := strings.Cut(strings.TrimSpace(l), ":")
		if l == "" || strings.TrimSuffix(name, ".desktop") == id {
			continue
		}
		out = append(out, l)
	}
	return writeFile(path, strings.Join(out, "\n")+"\n")
}

// Terminal is the terminal to run, as argv, and whether a command run in
// it follows "-e": configured (the launcher's), else $TERMINAL, else the
// first installed terminal the xdg-terminals.list files name, else
// xdg-terminal-exec, which takes the command as it is.
func Terminal(configured []string) (argv []string, dashE bool) {
	if len(configured) > 0 {
		return slices.Clone(configured), true
	}
	if env := strings.Fields(os.Getenv("TERMINAL")); len(env) > 0 {
		return env, true
	}
	var entry *Entry
	installed := func(id string) bool {
		e, err := Find(DataDirs(), id, "")
		if err == nil && len(e.Command) > 0 {
			entry = e
		}
		return entry != nil
	}
	if DefaultTerminal(TerminalsPaths(CurrentDesktops()), installed) != "" {
		return slices.Clone(entry.Command), true
	}
	return []string{"xdg-terminal-exec"}, false
}

// UserTerminals is the user's own xdg-terminals.list.
func UserTerminals() string { return filepath.Join(configHome(), "xdg-terminals.list") }

// writeFile replaces the file at path whole, through a file beside it.
func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func configHome() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config")
}

func cmpEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
