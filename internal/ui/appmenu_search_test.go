package ui

import (
	"slices"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/appmenu"
	"github.com/lyricat/inua/internal/compositor/compositortest"
)

// A search for the same window again, after the menus changed, starts
// unanswered: an application found without a menu may just have
// published one, and its pack's menu must not stand in while that is
// looked for. A published menu found before stays until the search ends.
func TestAppMenuSearchAgainForgetsTheAbsence(t *testing.T) {
	published := appmenu.Registration{Service: ":1.9", Path: "/menu"}
	var a appMenuState
	source := func() int { return appMenuSource(a.found, a.searched, false) }

	first, ok := a.beginSearch("0xa")
	if !ok || source() != appMenuNone {
		t.Fatalf("first search: started %v, source %d", ok, source())
	}
	if !a.endSearch(first, appmenu.Registration{}) || source() != appMenuFromPack {
		t.Fatalf("no menu found: source %d, want the pack's", source())
	}
	if _, ok := a.beginSearch("0xa"); ok {
		t.Fatal("searched again with nothing changed")
	}

	// The application registers a menu: the registrar says the menus changed.
	a.stale = true
	second, ok := a.beginSearch("0xa")
	if !ok {
		t.Fatal("no search after the menus changed")
	}
	if source() != appMenuNone {
		t.Fatalf("while searching again: source %d, want none (the pack's menu stayed)", source())
	}
	if a.endSearch(first, appmenu.Registration{}) || source() != appMenuNone {
		t.Fatal("an overtaken search ended the new one")
	}
	if !a.endSearch(second, published) || source() != appMenuPublished {
		t.Fatalf("published menu found: source %d", source())
	}

	// The menus change again: the published menu stays while searching.
	a.stale = true
	third, _ := a.beginSearch("0xa")
	if source() != appMenuPublished {
		t.Fatalf("published menu dropped while searching again: source %d", source())
	}
	if !a.endSearch(third, appmenu.Registration{}) || source() != appMenuFromPack {
		t.Fatalf("menu gone: source %d, want the pack's", source())
	}

	// Another window starts with nothing.
	a.endSearch(a.finding, published)
	a.beginSearch("0xb")
	if a.found != (appmenu.Registration{}) || source() != appMenuNone {
		t.Fatalf("another window kept the menu: %+v", a.found)
	}
}

// A pack item runs only while a bar shows the pack's menu for its window:
// one withdrawn while a published menu is looked for runs nothing, even
// from a menu that was already open.
func TestPackEntryRefusedOnceWithdrawn(t *testing.T) {
	snap := twoBrowserWindows()
	w, _ := snap.Window(snap.FocusedWindow)
	closeIt, _ := find(genericAppSections(w, "Chromium"), "Close Window")
	fake := compositortest.New()
	b := &bar{}
	s := &shellUI{snap: snap, comp: fake, bars: map[uintptr]*bar{1: b}}

	s.activatePackedEntry(closeIt)
	b.appMenuWindow = w.ID
	b.appMenuReg = appmenu.Registration{Service: ":1.9", Path: "/menu"}
	s.activatePackedEntry(closeIt)
	if s.packMenuShown(w.ID) {
		t.Fatal("a published menu counted as the pack's")
	}

	b.appMenuReg = appmenu.Registration{}
	if !s.packMenuShown(w.ID) {
		t.Fatal("the pack's menu shown for the window was not seen")
	}
	s.activatePackedEntry(closeIt)
	want := []string{"close-window " + w.ID}
	deadline := time.Now().Add(5 * time.Second)
	for len(fake.Actions()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := fake.Actions(); !slices.Equal(got, want) {
		t.Fatalf("actions %q, want only %q", got, want)
	}
}
