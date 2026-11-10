package appmenu

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
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
	packs.Refresh()
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
	zed, ok := packs.Match("dev.zed.Zed", "")
	if !ok || !zed.SkipA11y {
		t.Fatalf("zed pack = %+v, ok %v", zed, ok)
	}
	if len(zed.Sections) != 9 {
		t.Fatalf("zed sections = %+v, want Zed/File/Edit/Selection/View/Go/Run/Window/Help", zed.Sections)
	}
	for i, label := range []string{"Zed", "File", "Edit", "Selection", "View", "Go", "Run", "Window", "Help"} {
		if zed.Sections[i].Label != label {
			t.Errorf("zed section %d = %q, want %q", i, zed.Sections[i].Label, label)
		}
	}
	for _, item := range zed.Sections[0].Items {
		if item.Label == "About Zed" && (!item.Disabled || item.Action != "") {
			t.Fatal("Zed's About command must be shown disabled, not run inua's About")
		}
	}
	if _, ok := packs.Match("zeditor", ""); !ok {
		t.Fatal("zeditor did not match the zed pack")
	}
	chrome, ok := packs.Match("chromium-browser", "")
	if !ok || !chrome.SkipA11y {
		t.Fatalf("chromium pack = %+v, ok %v", chrome, ok)
	}
	for i, label := range []string{"File", "Edit", "View", "History", "Bookmarks", "Profiles", "Tab", "Window", "Help"} {
		if i >= len(chrome.Sections) || chrome.Sections[i].Label != label {
			t.Fatalf("chromium section %d = %+v, want %q", i, chrome.Sections, label)
		}
	}
	if _, ok := packs.Match("google-chrome", ""); !ok {
		t.Fatal("google-chrome did not match the chromium pack")
	}
	if _, ok := packs.Match("org.chromium.Chromium", ""); !ok {
		t.Fatal("org.chromium.Chromium did not match the chromium pack")
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
	packs.Refresh()
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

func TestPackKeySequence(t *testing.T) {
	body := `
id = "zed"
match = ["zed"]
[[section]]
label = "Go"
[[section.item]]
label = "Definition"
sequence = ["g", "d"]
[[section.item]]
label = "Declaration"
sequence = ["g", "shift+d"]
`
	pack, err := parsePack("zed.toml", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	entries := pack.Entries("window-1", 7, nil, nil)
	if len(entries) != 1 || len(entries[0].Children) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	definition := entries[0].Children[0].Action
	if definition.Kind != ActionKeySequence || definition.Bus != "window-1" || len(definition.Sequence) != 2 || definition.Sequence[0] != (KeyStroke{Key: "g"}) || definition.Sequence[1] != (KeyStroke{Key: "d"}) {
		t.Errorf("definition action = %+v", definition)
	}
	declaration := entries[0].Children[1].Action
	if declaration.Sequence[1] != (KeyStroke{Mods: "SHIFT", Key: "d"}) {
		t.Errorf("declaration sequence = %+v", declaration.Sequence)
	}
}

func TestPackDisabledItem(t *testing.T) {
	body := `
id = "zed"
match = ["zed"]
[[section]]
label = "Help"
[[section.item]]
label = "Documentation"
disabled = true
[[section.item]]
label = "Quit"
action = "quit-app"
`
	pack, err := parsePack("zed.toml", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	entries := pack.Entries("w", 1, nil, nil)
	help := entries[0].Children
	if len(help) != 2 {
		t.Fatalf("entries = %+v", help)
	}
	if help[0].Label != "Documentation" || help[0].Enabled || help[0].Action.Kind != "" || len(help[0].Children) != 0 {
		t.Errorf("disabled item = %+v", help[0])
	}
	if !help[1].Enabled || help[1].Action.Kind != ActionQuitApp {
		t.Errorf("enabled item = %+v", help[1])
	}
	bad := `
id = "zed"
match = ["zed"]
[[section]]
label = "Help"
[[section.item]]
label = "Documentation"
disabled = true
keys = ["CTRL", "H"]
`
	if _, err := parsePack("bad.toml", []byte(bad)); err == nil {
		t.Fatal("accepted a disabled item with keys")
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
		// Punctuation is not an XKB keysym name: the key would never arrive.
		"id = \"x\"\nmatch = [\"a\"]\n[[section]]\nlabel = \"Tabs\"\n[[section.item]]\nlabel = \"Next\"\nkeys = [\"CTRL\", \"SHIFT\", \".\"]\n",
		"id = \"x\"\nmatch = [\"a\"]\n[[section]]\nlabel = \"Tabs\"\n[[section.item]]\nlabel = \"Next\"\nkeys = [\"CTRL\", \"]\"]\n",
		"id = \"x\"\nmatch = [\"a\"]\n[[section]]\nlabel = \"Go\"\n[[section.item]]\nlabel = \"Mark\"\nsequence = [\"g\", \"[\"]\n",
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
	packs.Refresh()
	before := packs.Generation()
	if _, ok := packs.Match("wezterm", ""); ok {
		t.Fatal("wezterm matched before a pack existed")
	}
	body := "id = \"wezterm\"\nmatch = [\"wezterm\"]\nskip_a11y = true\n"
	if err := os.WriteFile(filepath.Join(config, "wezterm.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if !packs.Refresh() || packs.Generation() == before {
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

// packConfigDir is the user's pack directory of an isolated environment.
func packConfigDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "inua", "menus")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// refreshWithin runs Refresh and fails the test when it does not return in
// a few seconds: reading a FIFO or an endless file would hang it.
func refreshWithin(t *testing.T, packs *Packs) bool {
	t.Helper()
	done := make(chan bool, 1)
	go func() { done <- packs.Refresh() }()
	select {
	case changed := <-done:
		return changed
	case <-time.After(5 * time.Second):
		t.Fatal("Refresh blocked")
		return false
	}
}

// A FIFO named like a pack, with no writer, is refused at once instead of
// holding the reading; the other packs still load.
func TestPackFIFOIsRefused(t *testing.T) {
	isolatePackEnv(t)
	dir := packConfigDir(t)
	if err := syscall.Mkfifo(filepath.Join(dir, "a-fifo.toml"), 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	body := "id = \"wezterm\"\nmatch = [\"wezterm\"]\nskip_a11y = true\n"
	if err := os.WriteFile(filepath.Join(dir, "wezterm.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	packs := NewPacks("")
	refreshWithin(t, packs)
	if _, ok := packs.Match("wezterm", ""); !ok {
		t.Fatal("the pack next to the FIFO was not loaded")
	}
	if !slices.ContainsFunc(packs.problems, func(p string) bool { return strings.Contains(p, "a-fifo.toml") }) {
		t.Fatalf("the FIFO was not reported: %q", packs.problems)
	}
}

// A FIFO where the pack directory should be is not waited on either.
func TestPackDirectoryFIFOIsRefused(t *testing.T) {
	isolatePackEnv(t)
	parent := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "inua")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(parent, "menus"), 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	packs := NewPacks("")
	refreshWithin(t, packs)
	if _, ok := packs.Match("code", ""); !ok {
		t.Fatal("the shipped packs were not loaded")
	}
}

// A pack that is not a regular file is refused before anything is read
// from it: /dev/zero would otherwise be read without end.
func TestPackDeviceIsRefused(t *testing.T) {
	isolatePackEnv(t)
	if _, err := os.Stat("/dev/zero"); err != nil {
		t.Skip("no /dev/zero")
	}
	dir := packConfigDir(t)
	if err := os.Symlink("/dev/zero", filepath.Join(dir, "zero.toml")); err != nil {
		t.Fatal(err)
	}
	if _, err := readPackFile(filepath.Join(dir, "zero.toml")); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("read /dev/zero as a pack: %v", err)
	}
	refreshWithin(t, NewPacks(""))
}

// A pack over the limit is refused, and no more than the limit and a byte
// is read of it.
func TestPackTooLarge(t *testing.T) {
	isolatePackEnv(t)
	dir := packConfigDir(t)
	path := filepath.Join(dir, "big.toml")
	big := "id = \"big\"\nmatch = [\"big\"]\n# " + strings.Repeat("x", maxPackBytes) + "\n"
	if err := os.WriteFile(path, []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPackFile(path); err == nil {
		t.Fatal("read a pack over the limit")
	}
	exact := "id = \"edge\"\nmatch = [\"edge\"]\n#"
	exact += strings.Repeat("x", maxPackBytes-len(exact)-1) + "\n"
	if err := os.WriteFile(path, []byte(exact), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := readPackFile(path)
	if err != nil || len(data) != maxPackBytes {
		t.Fatalf("a pack of exactly the limit: %d bytes, %v", len(data), err)
	}
}

// Match and Generation answer from what was read and touch no file; the
// files are read by Refresh, which Check runs on a goroutine and which
// reports a change through its callback.
func TestPackLookupsDoNotRead(t *testing.T) {
	isolatePackEnv(t)
	dir := packConfigDir(t)
	packs := NewPacks("")
	if packs.Loaded() {
		t.Fatal("loaded before any reading")
	}
	if _, ok := packs.Match("code", ""); ok {
		t.Fatal("Match read the packs")
	}
	if packs.Generation() != 0 {
		t.Fatal("Generation read the packs")
	}
	changed := make(chan struct{}, 4)
	packs.Check(func() { changed <- struct{}{} })
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("Check did not report the first reading")
	}
	if !packs.Loaded() {
		t.Fatal("not loaded after the first reading")
	}
	gen := packs.Generation()
	body := "id = \"wezterm\"\nmatch = [\"wezterm\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "wezterm.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := packs.Match("wezterm", ""); ok || packs.Generation() != gen {
		t.Fatal("a lookup read the new file")
	}
	packs.Check(func() { changed <- struct{}{} })
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("Check did not report the new pack")
	}
	if _, ok := packs.Match("wezterm", ""); !ok {
		t.Fatal("the new pack was not read")
	}
}
