package files

import (
	"cmp"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Place is a folder the sidebar offers.
type Place struct {
	Name string
	Path string // a local path; for a bookmark of a URI, its URI
	Icon string // a freedesktop icon name, drawn from the desktop's theme
	Kind string // "home", "xdg", "bookmark", "root"
}

// Home is the user's home folder.
func Home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "/"
	}
	return h
}

func configHome() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return d
	}
	return filepath.Join(Home(), ".config")
}

// DataHome is $XDG_DATA_HOME, ~/.local/share by default.
func DataHome() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d
	}
	return filepath.Join(Home(), ".local", "share")
}

// xdgDirs are the user folders in the order the sidebar shows them, with
// their icons.
var xdgDirs = []struct{ key, icon string }{
	{"DESKTOP", "user-desktop"},
	{"DOCUMENTS", "folder-documents"},
	{"DOWNLOAD", "folder-download"},
	{"MUSIC", "folder-music"},
	{"PICTURES", "folder-pictures"},
	{"VIDEOS", "folder-videos"},
}

// UserDirs reads ~/.config/user-dirs.dirs: XDG_DESKTOP_DIR="$HOME/Desktop"
// and the like, by key (DESKTOP, DOCUMENTS …). A folder set to home
// itself means the folder is turned off, and is left out.
func UserDirs() map[string]string {
	b, err := os.ReadFile(filepath.Join(configHome(), "user-dirs.dirs"))
	if err != nil {
		return nil
	}
	home := Home()
	out := map[string]string{}
	for line := range strings.SplitSeq(string(b), "\n") {
		line = strings.TrimSpace(line)
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.HasPrefix(line, "#") || !strings.HasPrefix(k, "XDG_") || !strings.HasSuffix(k, "_DIR") {
			continue
		}
		v = strings.Trim(v, `"`)
		switch {
		case strings.HasPrefix(v, "$HOME/"):
			v = filepath.Join(home, v[len("$HOME/"):])
		case v == "$HOME":
			v = home
		case !strings.HasPrefix(v, "/"):
			continue
		}
		if filepath.Clean(v) == home {
			continue
		}
		out[strings.TrimSuffix(strings.TrimPrefix(k, "XDG_"), "_DIR")] = filepath.Clean(v)
	}
	return out
}

// Places are the folders the sidebar lists: home and the user folders
// that exist (its Places section), then the bookmarks (Kind "bookmark",
// its Bookmarks section).
func Places() []Place {
	home := Home()
	out := []Place{{Name: "Home", Path: home, Icon: "user-home", Kind: "home"}}
	dirs := UserDirs()
	for _, d := range xdgDirs {
		p, ok := dirs[d.key]
		if !ok {
			continue
		}
		if st, err := os.Stat(p); err != nil || !st.IsDir() {
			continue
		}
		out = append(out, Place{Name: filepath.Base(p), Path: p, Icon: d.icon, Kind: "xdg"})
	}
	// A bookmark of a place listed already (GTK's file chooser adds the
	// user folders to the bookmarks) is not listed twice.
	listed := map[string]bool{}
	for _, p := range out {
		listed[p.Path] = true
	}
	for _, b := range Bookmarks() {
		if listed[b.Path] {
			continue
		}
		listed[b.Path] = true
		out = append(out, Place{Name: b.Name, Path: b.Path, Icon: "folder", Kind: "bookmark"})
	}
	return out
}

// Bookmark is a line of GTK's bookmarks file.
type Bookmark struct {
	Path string // a local path, or the URI when it is not file://
	Name string // the label, else the folder's name
	URI  string
}

// BookmarksPath is GTK's bookmarks file, shared with the file chooser and
// Nautilus.
func BookmarksPath() string { return filepath.Join(configHome(), "gtk-3.0", "bookmarks") }

// Bookmarks reads GTK's bookmarks: "URI [label]" a line.
func Bookmarks() []Bookmark {
	b, err := os.ReadFile(BookmarksPath())
	if err != nil {
		return nil
	}
	var out []Bookmark
	for line := range strings.SplitSeq(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		uri, label, _ := strings.Cut(line, " ")
		bm := Bookmark{URI: uri, Path: uri}
		if p, ok := PathOfURI(uri); ok {
			bm.Path = p
		}
		bm.Name = cmp.Or(strings.TrimSpace(label), filepath.Base(strings.TrimRight(bm.Path, "/")))
		out = append(out, bm)
	}
	return out
}

// SaveBookmarks writes GTK's bookmarks file.
func SaveBookmarks(bs []Bookmark) error {
	var sb strings.Builder
	for _, b := range bs {
		uri := cmp.Or(b.URI, URIOfPath(b.Path))
		sb.WriteString(uri)
		if b.Name != "" && b.Name != filepath.Base(strings.TrimRight(b.Path, "/")) {
			sb.WriteString(" " + b.Name)
		}
		sb.WriteByte('\n')
	}
	return writeFileAtomic(BookmarksPath(), []byte(sb.String()), 0o644)
}

// ToggleBookmark adds dir to the bookmarks, or removes it when it is
// there; it says whether dir is bookmarked now.
func ToggleBookmark(dir string) (bool, error) {
	bs := Bookmarks()
	for i, b := range bs {
		if b.Path == dir {
			return false, SaveBookmarks(append(bs[:i], bs[i+1:]...))
		}
	}
	return true, SaveBookmarks(append(bs, Bookmark{Path: dir, URI: URIOfPath(dir)}))
}

// Bookmarked says whether dir is among the bookmarks.
func Bookmarked(dir string) bool {
	for _, b := range Bookmarks() {
		if b.Path == dir {
			return true
		}
	}
	return false
}

// URIOfPath is a local path as a file:// URI, escaped.
func URIOfPath(p string) string {
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// PathOfURI is the local path of a file:// URI (or of a plain path).
func PathOfURI(s string) (string, bool) {
	if strings.HasPrefix(s, "/") {
		return filepath.Clean(s), true
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") {
		return "", false
	}
	return filepath.Clean(u.Path), true
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	tmp.Chmod(perm)
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
