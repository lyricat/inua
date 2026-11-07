package appmenu

import (
	"os"
	"path/filepath"
	"testing"
)

func isolatePackEnv(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_DATA_DIRS", filepath.Join(root, "share"))
}

func TestShippedPacks(t *testing.T) {
	isolatePackEnv(t)
	list, problems := loadPacks(nil)
	if len(problems) > 0 {
		t.Fatalf("shipped packs: %v", problems)
	}
	packs := NewPacks("")
	code, ok := packs.Match("code", "")
	if !ok || !code.SkipA11y || code.When != PackFallback {
		t.Fatalf("code pack = %+v, ok %v", code, ok)
	}
	if _, ok := packs.Match("com.visualstudio.code", ""); !ok {
		t.Fatal("suffix .code did not match com.visualstudio.code")
	}
	if _, ok := packs.Match("Code", "com.visualstudio.code"); !ok {
		t.Fatal("code match is case sensitive")
	}
	ff, ok := packs.Match("firefox", "")
	if !ok || !ff.NeverPress || ff.SkipA11y {
		t.Fatalf("firefox pack = %+v", ff)
	}
	if _, ok := packs.Match("org.mozilla.firefox", ""); !ok {
		t.Fatal("firefox contains did not match the desktop id")
	}
	if _, ok := packs.Match("org.wezfurlong.wezterm", "wezterm"); ok {
		t.Fatal("an unmatched application got a pack")
	}
	if len(list) < 2 {
		t.Fatalf("shipped = %d", len(list))
	}
}

func TestPackOverrideAndEntries(t *testing.T) {
	isolatePackEnv(t)
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "inua", "menus")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `
id = "code"
match = ["code"]
when = "replace"
skip_a11y = false

[[section]]
label = "File"

[[section.item]]
label = "New Tab"
keys = ["ctrl", "t"]

[[section.item]]
separator = true

[[section.item]]
label = "Close"
action = "close-window"

[[section]]
label = "View"
include = "window"

[[section.item]]
label = "Zoom"
keys = ["CTRL", "plus"]
`
	if err := os.WriteFile(filepath.Join(dir, "code.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	packs := NewPacks("")
	got, ok := packs.Match("code", "com.visualstudio.code")
	if !ok || got.When != PackReplace || got.SkipA11y {
		t.Fatalf("override = %+v, ok %v", got, ok)
	}
	entries := got.Entries("0xaa", 7,
		[]Entry{{Label: "File"}, {Label: "Help"}},
		[]Entry{{Label: "Fullscreen"}})
	if len(entries) != 2 || entries[0].Label != "File" || len(entries[0].Children) != 3 {
		t.Fatalf("entries = %+v", entries)
	}
	key := entries[0].Children[0]
	if key.Action.Kind != ActionKeys || key.Action.Mods != "CTRL" || key.Action.Key != "T" || key.Action.Bus != "0xaa" {
		t.Fatalf("key item = %+v", key.Action)
	}
	if !entries[0].Children[1].Separator {
		t.Fatal("separator was dropped")
	}
	if entries[0].Children[2].Action.Kind != ActionCloseWindow {
		t.Fatalf("close = %+v", entries[0].Children[2].Action)
	}
	if entries[1].Label != "View" || len(entries[1].Children) != 2 || entries[1].Children[0].Label != "Fullscreen" {
		t.Fatalf("view = %+v", entries[1])
	}
	if entries[1].Children[1].Action.Key != "plus" {
		t.Fatalf("plus key = %q", entries[1].Children[1].Action.Key)
	}
}

func TestPackRejects(t *testing.T) {
	isolatePackEnv(t)
	cases := []string{
		"id = \"x\"\n",
		"id = \"x\"\nmatch = [\"\"]\n",
		"id = \"x\"\nmatch = [\"a\"]\nwhen = \"always\"\n",
		"id = \"x\"\nmatch = [\"a\"]\nnope = true\n",
		"id = \"x\"\nmatch = [\"a\"]\n[[section]]\nlabel = \"File\"\n[[section.item]]\nlabel = \"Go\"\naction = \"explode\"\n",
		"id = \"x\"\nmatch = [\"a\"]\n[[section]]\nlabel = \"File\"\n[[section.item]]\nlabel = \"Go\"\naction = \"close-window\"\nkeys = [\"T\"]\n",
	}
	for _, body := range cases {
		if _, err := parsePack("bad.toml", []byte(body)); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}

func TestPackReloadsWhenTheFileChanges(t *testing.T) {
	isolatePackEnv(t)
	config := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "inua", "menus")
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	packs := NewPacks("")
	before := packs.Generation()
	if _, ok := packs.Match("wezterm", ""); ok {
		t.Fatal("wezterm matched before a pack existed")
	}
	body := "id = \"wezterm\"\nmatch = [\"wezterm\"]\nskip_a11y = true\n"
	if err := os.WriteFile(filepath.Join(config, "wezterm.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if packs.Generation() == before {
		t.Fatal("generation did not change")
	}
	got, ok := packs.Match("org.wezfurlong.wezterm", "wezterm")
	if !ok || !got.SkipA11y {
		t.Fatalf("reloaded = %+v, ok %v", got, ok)
	}
	entries := got.Entries("1", 1, []Entry{{Label: "File"}}, nil)
	if len(entries) != 0 {
		t.Fatalf("flag-only pack produced %+v", entries)
	}
}
