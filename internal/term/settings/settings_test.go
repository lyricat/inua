package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "term.toml")
	s := Default()
	s.Font.Size = 13
	s.Agents.Notifications = false
	s.Look = Look{Theme: "washi", Scheme: "nord"}
	s.Shell.Program = "/usr/bin/fish"
	s.Keys["zoom"] = ""
	s.Keys["split_right"] = "ctrl+shift+backslash"
	s.Web = Web{On: true, Tailscale: true, Port: 8080}
	if err := Save(path, s); err != nil {
		t.Fatal(err)
	}
	got, warnings, err := Load(path)
	if err != nil || len(warnings) > 0 {
		t.Fatalf("load: %v %v", err, warnings)
	}
	if !reflect.DeepEqual(got, s) {
		t.Errorf("round trip:\n%+v\n%+v", got, s)
	}
}

func TestMissingAndInvalid(t *testing.T) {
	if s, w, err := Load(filepath.Join(t.TempDir(), "none.toml")); err != nil || len(w) > 0 || !reflect.DeepEqual(s, Default()) {
		t.Errorf("missing: %v %v", w, err)
	}
	path := filepath.Join(t.TempDir(), "term.toml")
	os.WriteFile(path, []byte(`
version = 2
[look]
scheme = "nope"
[font]
size = 200
[cursor]
style = "beam"
[keys]
zoom = "Ctrl Shift Z"
nothing = "ctrl+q"
[web]
port = 80
[colors]
x = 1
`), 0o600)
	s, w, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	d := Default()
	if s.Font.Size != 11 || s.Cursor.Style != "block" || s.Keys["zoom"] != d.Keys["zoom"] || s.Keys["copy"] != d.Keys["copy"] || s.Look.Scheme != "" || s.Web.Port != 7681 {
		t.Errorf("defaults back: %+v", s)
	}
	if len(w) != 7 || !strings.Contains(strings.Join(w, "\n"), "unknown setting colors") {
		t.Errorf("warnings %q", w)
	}
	os.WriteFile(path, []byte("[font"), 0o600)
	if _, _, err := Load(path); err == nil {
		t.Error("a broken file")
	}
}

func TestFocusFollowsMouse(t *testing.T) {
	if Default().Terminal.FocusFollowsMouse {
		t.Fatal("hover focus must be off by default")
	}
	for _, tc := range []struct {
		name, content string
		want          bool
	}{
		{name: "missing file"},
		{name: "missing key", content: "[terminal]\ncopy_on_select = false\n"},
		{name: "enabled", content: "[terminal]\nfocus_follows_mouse = true\n", want: true},
		{name: "disabled", content: "[terminal]\nfocus_follows_mouse = false\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "term.toml")
			if tc.content != "" {
				if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			s, warnings, err := Load(path)
			if err != nil || len(warnings) != 0 {
				t.Fatalf("load: %v %v", err, warnings)
			}
			if s.Terminal.FocusFollowsMouse != tc.want {
				t.Fatalf("hover focus = %v, want %v", s.Terminal.FocusFollowsMouse, tc.want)
			}
			if err := Save(path, s); err != nil {
				t.Fatal(err)
			}
			got, warnings, err := Load(path)
			if err != nil || len(warnings) != 0 || !reflect.DeepEqual(got, s) {
				t.Fatalf("round trip: %+v, warnings %v, error %v", got, warnings, err)
			}
		})
	}
}

// A file of the first version: the keys it kept at that version's
// defaults become Ghostty's, the ones the user chose stay, and its hooks
// setting is let go without a warning.
func TestFirstVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "term.toml")
	os.WriteFile(path, []byte(`
[agents]
notifications = true
hooks = true
[keys]
split_right = "ctrl+shift+d"
zoom = "ctrl+shift+m"
settings = "ctrl+shift+comma"
`), 0o600)
	s, w, err := Load(path)
	if err != nil || len(w) > 0 {
		t.Fatalf("load: %v %q", err, w)
	}
	d := Default()
	if s.Version != Version || s.Keys["split_right"] != d.Keys["split_right"] || s.Keys["settings"] != d.Keys["settings"] || s.Keys["zoom"] != "ctrl+shift+m" {
		t.Errorf("keys %+v", s.Keys)
	}
}

// A file of the second version: a new session and a new workspace take
// the defaults now (on Linux, Ctrl+Shift+N and none), when both kept
// their old ones; a file that changed either keeps both.
func TestSecondVersion(t *testing.T) {
	dir := t.TempDir()
	d := Default()
	for _, c := range []struct {
		keys               string
		session, workspace string
	}{
		{"new_session = \"ctrl+shift+t\"\nnew_workspace = \"ctrl+shift+n\"", d.Keys["new_session"], d.Keys["new_workspace"]},
		{"", d.Keys["new_session"], d.Keys["new_workspace"]}, // no keys: the defaults
		{"new_session = \"ctrl+shift+y\"\nnew_workspace = \"ctrl+shift+n\"", "ctrl+shift+y", "ctrl+shift+n"},
		{"new_session = \"\"\nnew_workspace = \"ctrl+shift+n\"", "", "ctrl+shift+n"},
	} {
		path := filepath.Join(dir, "term.toml")
		os.WriteFile(path, []byte("version = 2\n[keys]\n"+c.keys+"\n"), 0o600)
		s, w, err := Load(path)
		if err != nil || len(w) > 0 {
			t.Fatalf("load: %v %q", err, w)
		}
		if s.Version != Version || s.Keys["new_session"] != c.session || s.Keys["new_workspace"] != c.workspace {
			t.Errorf("%q: session %q, workspace %q", c.keys, s.Keys["new_session"], s.Keys["new_workspace"])
		}
	}
}

func TestShells(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "usr", "bin")
	os.MkdirAll(bin, 0o755)
	os.Symlink(bin, filepath.Join(dir, "bin"))
	for _, name := range []string{"bash", "zsh", "nologin"} {
		os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/true\n"), 0o755)
	}
	os.Symlink("bash", filepath.Join(bin, "sh"))
	os.WriteFile(filepath.Join(bin, "notrun"), nil, 0o644)
	t.Setenv("SHELL", filepath.Join(bin, "zsh"))
	etc := "# /etc/shells\n" + strings.Join([]string{
		filepath.Join(dir, "bin", "sh"),
		filepath.Join(dir, "bin", "bash"),
		filepath.Join(bin, "bash"),
		filepath.Join(bin, "zsh"),
		filepath.Join(bin, "nologin"),
		filepath.Join(bin, "notrun"),
		filepath.Join(bin, "fish"),
	}, "\n")
	want := []string{filepath.Join(bin, "zsh"), filepath.Join(dir, "bin", "sh"), filepath.Join(dir, "bin", "bash")}
	if got := Shells(etc); !slices.Equal(got, want) {
		t.Errorf("shells %q, want %q", got, want)
	}
}

// The Mac's chords are for actions that exist, each chord once with the
// rest of the defaults, each named in the order chordName writes.
func TestMacKeys(t *testing.T) {
	keys := map[string]string{}
	for _, a := range Actions {
		keys[a.Name] = a.Keys
	}
	for name, k := range macKeys {
		if _, ok := keys[name]; !ok {
			t.Errorf("macKeys: no action %q", name)
		}
		keys[name] = k
	}
	order := []string{"ctrl", "shift", "alt", "super"}
	seen := map[string]string{}
	for name, k := range keys {
		if k == "" {
			continue
		}
		if other, ok := seen[k]; ok {
			t.Errorf("%s is both %s's and %s's", k, name, other)
		}
		seen[k] = name
		parts := strings.Split(k, "+")
		last := -1
		for _, p := range parts[:len(parts)-1] {
			i := slices.Index(order, p)
			if i <= last {
				t.Errorf("%s: %q out of order", name, k)
			}
			last = i
		}
	}
}
