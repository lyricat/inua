package ui

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/appmenu"
	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/compositor/compositortest"
	"github.com/lyricat/inua/internal/uikit"
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
	s.appmenu.packs.Refresh()
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

// runItem runs the menu line labelled label, at any depth.
func runItem(t *testing.T, items []uikit.Item, label string) {
	t.Helper()
	var walk func([]uikit.Item) bool
	walk = func(items []uikit.Item) bool {
		for _, it := range items {
			if it.Label == label && it.Run != nil {
				it.Run()
				return true
			}
			if walk(it.Items) {
				return true
			}
		}
		return false
	}
	if !walk(items) {
		t.Fatalf("no line %q to run", label)
	}
}

// actionsAfter is what the fake compositor was asked, once want arrived
// or a moment passed: the shell asks it from a goroutine.
func actionsAfter(fake *compositortest.Fake, want []string) []string {
	deadline := time.Now().Add(2 * time.Second)
	if len(want) == 0 {
		deadline = time.Now().Add(150 * time.Millisecond)
	}
	for time.Now().Before(deadline) {
		if got := fake.Actions(); len(want) > 0 && slices.Equal(got, want) {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fake.Actions()
}

// The menu the application's name opens acts on its window whatever menu
// the bar shows beside it (the generic one, a published one, an
// accessibility one or none), while the bar's own pack lines still act
// only while the bar shows them; and neither acts once the window it was
// built for has lost the focus or is gone.
func TestTitleMenuActsBesideAnyBarMenu(t *testing.T) {
	for _, tc := range []struct {
		name     string
		set      func(*bar)
		packLine bool // the bar's pack line for the window acts too
	}{
		{"generic menu", func(b *bar) { b.appMenuWindow = "0xb" }, true},
		{"published menu", func(b *bar) { b.appMenuReg = appmenu.Registration{Service: ":1.9", Path: "/MenuBar/1"} }, false},
		{"accessibility menu", func(b *bar) { b.appMenuWindow, b.appMenuPID, b.appMenuA11y = "0xb", 40, true }, false},
		{"no menu", func(*bar) {}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := compositortest.New()
			b := &bar{}
			tc.set(b)
			s := &shellUI{snap: twoBrowserWindows(), comp: fake, bars: map[uintptr]*bar{1: b}}
			w, _ := s.snap.Window(s.snap.FocusedWindow)
			file := genericAppSections(w, "Chromium")[0].Children

			runItem(t, s.titleMenuItems(file), "Close Window")
			want := []string{"close-window 0xb"}
			if got := actionsAfter(fake, want); !slices.Equal(got, want) {
				t.Fatalf("title menu's Close Window asked %q, want %q", got, want)
			}

			runItem(t, s.packedMenuItems(file), "Close Window")
			if tc.packLine {
				want = append(want, "close-window 0xb")
			}
			if got := actionsAfter(fake, want); !slices.Equal(got, want) {
				t.Fatalf("pack line's Close Window asked %q, want %q", got, want)
			}

			// The focus moved to another window of the process: a menu
			// still open for 0xb closes nothing.
			title := s.titleMenuItems(file)
			s.snap.FocusedWindow = "0xa"
			runItem(t, title, "Close Window")
			runItem(t, title, "Quit Chromium")
			// 0xb is gone, the focus with it.
			s.snap = twoBrowserWindows()
			s.snap.Windows = slices.DeleteFunc(s.snap.Windows, func(w compositor.Window) bool { return w.ID == "0xb" })
			s.snap.FocusedWindow = ""
			runItem(t, title, "Close Window")
			if got := actionsAfter(fake, nil); !slices.Equal(got, want) {
				t.Fatalf("a menu built for 0xb acted after it lost the focus: %q, want %q", got, want)
			}
		})
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
		s.appmenu.packs.Refresh()
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
