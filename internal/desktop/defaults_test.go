package desktop

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func roleOf(id string) Role {
	for _, r := range Roles {
		if r.ID == id {
			return r
		}
	}
	panic(id)
}

// The candidates for a role: the apps shown in menus that open its first
// type, or are in its category, by name.
func TestRoleCandidates(t *testing.T) {
	entries := []*Entry{
		{ID: "zbrowser", Name: "Zeta", MimeTypes: []string{"text/html", "x-scheme-handler/http"}},
		{ID: "abrowser", Name: "alpha", MimeTypes: []string{"x-scheme-handler/http"}},
		{ID: "hidden", Name: "Hidden", MimeTypes: []string{"x-scheme-handler/http"}, NoDisplay: true},
		{ID: "html", Name: "Html only", MimeTypes: []string{"text/html"}},
		{ID: "foot", Name: "Foot", Categories: []string{"System", "TerminalEmulator"}},
	}
	ids := func(es []*Entry) (out []string) {
		for _, e := range es {
			out = append(out, e.ID)
		}
		return out
	}
	if got := ids(roleOf("browser").Candidates(entries)); !slices.Equal(got, []string{"abrowser", "zbrowser"}) {
		t.Errorf("browsers %v", got)
	}
	if got := ids(roleOf("terminal").Candidates(entries)); !slices.Equal(got, []string{"foot"}) {
		t.Errorf("terminals %v", got)
	}
}

// A type's default is the first installed app of the first file that
// names one: the user's choice before the system's.
func TestDefault(t *testing.T) {
	dir := t.TempDir()
	user, desk, system := filepath.Join(dir, "user.list"), filepath.Join(dir, "hyprland-mimeapps.list"), filepath.Join(dir, "system.list")
	os.WriteFile(user, []byte("[Added Associations]\ntext/html=gone.desktop;\n[Default Applications]\ntext/html=gone.desktop;firefox.desktop;\n"), 0o644)
	os.WriteFile(desk, []byte("[Default Applications]\ninode/directory=nautilus.desktop\n"), 0o644)
	os.WriteFile(system, []byte("[Default Applications]\ntext/html=chromium.desktop\ninode/directory=thunar.desktop\n"), 0o644)
	installed := func(id string) bool { return id != "gone" }
	paths := []string{user, desk, system}
	for mime, want := range map[string]string{"text/html": "firefox", "inode/directory": "nautilus", "audio/flac": ""} {
		if got := Default(paths, mime, installed); got != want {
			t.Errorf("%s: %q, want %q", mime, got, want)
		}
	}
}

// Choosing a default rewrites the types' lines in [Default Applications]
// and keeps everything else in the file as it was.
func TestSetDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conf", "mimeapps.list")
	if err := SetDefault(path, []string{"inode/directory"}, "files"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != "[Default Applications]\ninode/directory=files.desktop\n" {
		t.Errorf("new file: %q", got)
	}
	os.WriteFile(path, []byte("# mine\n[Default Applications]\ntext/html=old.desktop\nimage/png=viewer.desktop\n\n[Added Associations]\ntext/html=old.desktop;\n"), 0o644)
	if err := SetDefault(path, []string{"text/html", "x-scheme-handler/http"}, "firefox"); err != nil {
		t.Fatal(err)
	}
	want := "# mine\n[Default Applications]\nimage/png=viewer.desktop\ntext/html=firefox.desktop\nx-scheme-handler/http=firefox.desktop\n\n[Added Associations]\ntext/html=old.desktop;\n"
	if got := read(t, path); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if got := Default([]string{path}, "x-scheme-handler/http", func(string) bool { return true }); got != "firefox" {
		t.Errorf("read back %q", got)
	}
	// A file without the section gets one at its end.
	os.WriteFile(path, []byte("[Added Associations]\nimage/png=viewer.desktop;"), 0o644)
	SetDefault(path, []string{"image/png"}, "viewer")
	if got := read(t, path); !strings.HasSuffix(got, "viewer.desktop;\n\n[Default Applications]\nimage/png=viewer.desktop\n") {
		t.Errorf("section added: %q", got)
	}
}

// The terminal: the first installed one the lists name, and the one
// chosen put first, the others kept after it.
func TestTerminal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "xdg-terminals.list")
	os.WriteFile(path, []byte("# mine\ngone.desktop\nkitty.desktop\nfoot.desktop:server\n"), 0o644)
	installed := func(id string) bool { return id != "gone" }
	if got := DefaultTerminal([]string{path}, installed); got != "kitty" {
		t.Errorf("default %q", got)
	}
	if err := SetTerminal(path, "foot"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != "foot.desktop\n# mine\ngone.desktop\nkitty.desktop\n" {
		t.Errorf("list %q", got)
	}
	if got := DefaultTerminal([]string{path}, installed); got != "foot" {
		t.Errorf("after setting: %q", got)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The terminal to run: the launcher's, else $TERMINAL, else the first
// installed one xdg-terminals.list names, else xdg-terminal-exec.
func TestTerminalArgv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_CONFIG_DIRS", filepath.Join(dir, "etc"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("XDG_DATA_DIRS", filepath.Join(dir, "share"))
	t.Setenv("XDG_CURRENT_DESKTOP", "Hyprland")
	t.Setenv("TERMINAL", "")
	check := func(configured []string, want string, dashE bool) {
		t.Helper()
		argv, e := Terminal(configured)
		if strings.Join(argv, " ") != want || e != dashE {
			t.Errorf("%v: %q %v, want %q %v", configured, argv, e, want, dashE)
		}
	}
	check(nil, "xdg-terminal-exec", false)
	os.MkdirAll(filepath.Join(dir, "data", "applications"), 0o755)
	os.WriteFile(filepath.Join(dir, "data", "applications", "foot.desktop"),
		[]byte("[Desktop Entry]\nType=Application\nName=Foot\nExec=foot --server-off %U\nCategories=TerminalEmulator;\n"), 0o644)
	SetTerminal(UserTerminals(), "foot")
	check(nil, "foot --server-off", true)
	t.Setenv("TERMINAL", "alacritty")
	check(nil, "alacritty", true)
	check([]string{"kitty", "--single-instance"}, "kitty --single-instance", true)
}

// A Wayfire session is named by the desktop or by the socket Wayfire gives
// the apps it starts; another session is not.
func TestWayfire(t *testing.T) {
	t.Setenv("WAYFIRE_SOCKET", "")
	t.Setenv("_WAYFIRE_SOCKET", "")
	t.Setenv("XDG_CURRENT_DESKTOP", "Hyprland")
	if Wayfire() {
		t.Error("Hyprland reported as Wayfire")
	}
	t.Setenv("XDG_CURRENT_DESKTOP", "Wayfire:Inua")
	if !Wayfire() {
		t.Error("the desktop name was not seen")
	}
	t.Setenv("XDG_CURRENT_DESKTOP", "")
	t.Setenv("_WAYFIRE_SOCKET", "/run/user/1000/wayfire-wayland-1.socket")
	if !Wayfire() {
		t.Error("the socket was not seen")
	}
}
