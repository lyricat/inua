package ui

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/lyricat/inua/internal/appmenu"
	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/compositor/compositortest"
)

// isolatePacks keeps the packs a test reads to the shipped ones.
func isolatePacks(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_DATA_DIRS", filepath.Join(root, "share"))
}

// twoBrowserWindows is two windows of one process, the second focused, and
// a window of another process.
func twoBrowserWindows() compositor.Snapshot {
	return compositor.Snapshot{
		Windows: []compositor.Window{
			{ID: "0xa", PID: 40, AppID: "chromium", Title: "First"},
			{ID: "0xb", PID: 40, AppID: "chromium", Title: "Second", Focused: true},
			{ID: "0xc", PID: 41, AppID: "kitty", Title: "Shell"},
		},
		FocusedWindow: "0xb",
	}
}

// find is the entry labelled label, at any depth.
func find(entries []appmenu.Entry, label string) (appmenu.Entry, bool) {
	for _, e := range entries {
		if e.Label == label {
			return e, true
		}
		if c, ok := find(e.Children, label); ok {
			return c, true
		}
	}
	return appmenu.Entry{}, false
}

// A pack's menu acts on the focused window, not on the first window of
// its process.
func TestPackMenuActsOnTheFocusedWindow(t *testing.T) {
	isolatePacks(t)
	s := &shellUI{snap: twoBrowserWindows()}
	s.appmenu.packs = appmenu.NewPacks("")
	w, _ := s.snap.Window(s.snap.FocusedWindow)
	pack, ok := s.menuPack(w)
	if !ok {
		t.Fatal("no pack for chromium")
	}
	entries := s.packMenuEntries(w, pack, true)
	newTab, ok := find(entries, "New Tab")
	if !ok || newTab.Action.Kind != appmenu.ActionKeys {
		t.Fatalf("chromium pack has no New Tab shortcut: %+v", newTab)
	}
	if newTab.Action.Bus != "0xb" {
		t.Fatalf("New Tab bound to %q, want the focused 0xb", newTab.Action.Bus)
	}
	target, ok := packTargetOf(s.snap, newTab)
	if !ok || target.window.ID != "0xb" {
		t.Fatalf("New Tab targets %+v, %v; want 0xb", target.window, ok)
	}

	fake := compositortest.New()
	if err := sendPackKeys(fake, target.window.ID, newTab.Action, 0); err != nil {
		t.Fatal(err)
	}
	want := []string{"send-key 0xb " + newTab.Action.Mods + " " + newTab.Action.Key}
	if got := fake.Actions(); !slices.Equal(got, want) {
		t.Fatalf("sent %q, want %q", got, want)
	}
}

// An entry built for one window does nothing once another window, even
// one of the same process, has the focus.
func TestPackEntryRefusedForAnotherWindow(t *testing.T) {
	snap := twoBrowserWindows()
	first, _ := snap.Window("0xa")
	closeFirst, _ := find(genericAppSections(first, "Chromium"), "Close Window")
	if _, ok := packTargetOf(snap, closeFirst); ok {
		t.Fatal("an entry built for 0xa ran while 0xb has the focus")
	}
	gone := closeFirst
	gone.Action.Bus = "0xdead"
	snap.FocusedWindow = "0xdead"
	if _, ok := packTargetOf(snap, gone); ok {
		t.Fatal("an entry ran for a window the snapshot no longer has")
	}
}

// Quit closes every window of the focused window's process, as read when
// it is chosen, and no other.
func TestPackQuitTargetsTheProcessWindows(t *testing.T) {
	snap := twoBrowserWindows()
	w, _ := snap.Window(snap.FocusedWindow)
	quit, ok := find(genericAppSections(w, "Chromium"), "Quit Chromium")
	if !ok {
		t.Fatal("no Quit in the generic menu")
	}
	target, ok := packTargetOf(snap, quit)
	if !ok {
		t.Fatal("quit refused for the focused window")
	}
	// The target is a copy: the snapshot changing later does not reach it.
	snap.Windows[0].ID = "0xchanged"
	if want := []string{"0xa", "0xb"}; !slices.Equal(target.windows, want) {
		t.Fatalf("quit closes %q, want %q", target.windows, want)
	}
}

// A pack that adds no entries of its own (the shipped code and firefox
// packs carry only flags) leaves the generic menu, as does no pack.
func TestEmptyPackShowsTheGenericMenu(t *testing.T) {
	isolatePacks(t)
	for _, class := range []string{"code", "firefox", "unknown.app"} {
		snap := compositor.Snapshot{Windows: []compositor.Window{{ID: "0x1", PID: 7, AppID: class}}, FocusedWindow: "0x1"}
		s := &shellUI{snap: snap}
		s.appmenu.packs = appmenu.NewPacks("")
		w := snap.Windows[0]
		pack, matched := s.menuPack(w)
		if class != "unknown.app" && !matched {
			t.Fatalf("%s: no shipped pack", class)
		}
		got := s.packMenuEntries(w, pack, matched)
		var labels []string
		for _, e := range got {
			labels = append(labels, e.Label)
		}
		if want := []string{"File", "Edit", "View", "Help"}; !slices.Equal(labels, want) {
			t.Errorf("%s: menu %q, want the generic %q", class, labels, want)
		}
	}
}

// A pack's menu waits for the search for a published one to end, so a
// real menu found a moment later is not preceded by the pack's commands.
func TestAppMenuSourceWaitsForTheSearch(t *testing.T) {
	published := appmenu.Registration{Service: ":1.9", Path: "/menu"}
	for _, tc := range []struct {
		name     string
		found    appmenu.Registration
		searched bool
		replace  bool
		want     int
	}{
		{"searching", appmenu.Registration{}, false, false, appMenuNone},
		{"none published", appmenu.Registration{}, true, false, appMenuFromPack},
		{"published", published, true, false, appMenuPublished},
		{"replace while searching", appmenu.Registration{}, false, true, appMenuFromPack},
		{"replace over published", published, true, true, appMenuFromPack},
	} {
		if got := appMenuSource(tc.found, tc.searched, tc.replace); got != tc.want {
			t.Errorf("%s: source %d, want %d", tc.name, got, tc.want)
		}
	}
}
