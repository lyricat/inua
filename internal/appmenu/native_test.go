package appmenu

import (
	"slices"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/dbusexport"
	"github.com/lyricat/inua/internal/dbustest"
)

// publishNative serves a menu as an inua application does, under name on a
// connection of its own, and returns the connection and the menu.
func (f *fixture) publishNative(t *testing.T, name string) (*dbus.Conn, *testMenu) {
	t.Helper()
	conn := dbustest.Conn(t, f.addr)
	menu := newTestMenu()
	if err := dbusexport.Export(conn, menu, nativeMenuPath, menuInterface); err != nil {
		t.Fatal(err)
	}
	if reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("request %s: reply=%v err=%v", name, reply, err)
	}
	return conn, menu
}

func (f *fixture) focused(t *testing.T) (Registration, bool) {
	t.Helper()
	return f.registrar.Focused(f.pid)
}

// The bar finds a published menu and acts on it while its process has the
// focus, and stops when the focus moves, a registered menu takes its place,
// or the menu goes or is replaced.
func TestFocusedNativeMenuIsCurrent(t *testing.T) {
	f := start(t)
	name := nativeMenuPrefix + "p1.w1"
	conn, menu := f.publishNative(t, name)

	reg, ok := f.focused(t)
	if !ok || reg.Service != name || reg.Path != nativeMenuPath || int(reg.PID) != f.pid {
		t.Fatalf("native menu not found: registration=%+v ok=%v", reg, ok)
	}
	if !f.registrar.Current(reg, f.pid) {
		t.Fatal("the menu just found is not current")
	}
	entries, err := f.registrar.Menu(reg, true)
	if err != nil || len(entries) != 2 || entries[0].Label != "Open" {
		t.Fatalf("menu=%+v err=%v", entries, err)
	}
	if err := f.registrar.Activate(reg, 1); err != nil {
		t.Fatal(err)
	}
	menu.mu.Lock()
	events := slices.Clone(menu.events)
	menu.mu.Unlock()
	if !slices.Equal(events, []int32{1}) {
		t.Fatalf("events=%v", events)
	}

	// The focus moves to another process, or to none.
	for _, pid := range []int{f.pid + 1, 0} {
		if f.registrar.Current(reg, pid) {
			t.Fatalf("still current with pid %d focused", pid)
		}
	}

	// A menu registered with the registrar comes first, and the published
	// one is back when it goes.
	f.register(t, f.client, 42, f.path)
	if got, ok := f.focused(t); !ok || got.Service != f.client.Names()[0] || got.Path != f.path {
		t.Fatalf("registered menu must come first: registration=%+v ok=%v", got, ok)
	}
	if f.registrar.Current(reg, f.pid) {
		t.Fatal("published menu still current under a registered one")
	}
	// Registered menus the registrar cannot choose between hide the
	// published one too.
	f.register(t, f.client, 43, "/Other")
	if got, ok := f.focused(t); ok {
		t.Fatalf("ambiguous registered menus fell back: registration=%+v", got)
	}
	f.unregister(t, f.client, 42)
	f.unregister(t, f.client, 43)
	if got, ok := f.focused(t); !ok || got != reg || !f.registrar.Current(reg, f.pid) {
		t.Fatalf("published menu not back: registration=%+v ok=%v", got, ok)
	}

	// A second window's menu makes the process's ambiguous.
	other, _ := f.publishNative(t, nativeMenuPrefix+"p1.w2")
	if got, ok := f.focused(t); ok || f.registrar.Current(reg, f.pid) {
		t.Fatalf("two published menus must show none: registration=%+v ok=%v", got, ok)
	}
	if _, err := other.ReleaseName(nativeMenuPrefix + "p1.w2"); err != nil {
		t.Fatal(err)
	}
	if !f.registrar.Current(reg, f.pid) {
		t.Fatal("published menu not current once the other went")
	}

	// The window closes and another publishes in its place.
	if _, err := conn.ReleaseName(name); err != nil {
		t.Fatal(err)
	}
	if got, ok := f.focused(t); ok || f.registrar.Current(reg, f.pid) {
		t.Fatalf("removed menu still found: registration=%+v ok=%v", got, ok)
	}
	f.publishNative(t, nativeMenuPrefix+"p1.w3")
	next, ok := f.focused(t)
	if !ok || next.Service != nativeMenuPrefix+"p1.w3" {
		t.Fatalf("replacement not found: registration=%+v ok=%v", next, ok)
	}
	if f.registrar.Current(reg, f.pid) || !f.registrar.Current(next, f.pid) {
		t.Fatal("the replaced menu must not be current, the new one must")
	}
}

// Without a bus or a focused process there is no menu.
func TestFocusedWithoutBus(t *testing.T) {
	r := New(nil)
	if reg, ok := r.Focused(1); ok {
		t.Fatalf("registration=%+v", reg)
	}
	if r.Current(Registration{}, 1) || r.Current(Registration{}, 0) {
		t.Fatal("an empty registration is current")
	}
}

// serveChromium serves menus as a Chromium-family browser on XWayland does:
// one under chromiumMenuPath for each window, on a connection of its own,
// registered with no registrar.
func (f *fixture) serveChromium(t *testing.T, windows ...string) (*dbus.Conn, []*testMenu) {
	t.Helper()
	conn := dbustest.Conn(t, f.addr)
	var menus []*testMenu
	for _, w := range windows {
		menu := newTestMenu()
		if err := dbusexport.Export(conn, menu, chromiumMenuPath+"/"+dbus.ObjectPath(w), menuInterface); err != nil {
			t.Fatal(err)
		}
		menus = append(menus, menu)
	}
	return conn, menus
}

// A browser's own menu is found and clicked when it serves one window's,
// and none is when it serves several: the focused window cannot be told
// apart. A registered menu still comes first.
func TestFocusedChromiumMenu(t *testing.T) {
	f := start(t)
	conn, menus := f.serveChromium(t, "4000001")

	reg, ok := f.focused(t)
	if !ok || reg.Service != conn.Names()[0] || reg.Path != chromiumMenuPath+"/4000001" || int(reg.PID) != f.pid {
		t.Fatalf("browser menu not found: registration=%+v ok=%v", reg, ok)
	}
	if !f.registrar.Current(reg, f.pid) || f.registrar.Current(reg, f.pid+1) {
		t.Fatal("the browser menu is current only for its own process")
	}
	entries, err := f.registrar.Menu(reg, true)
	if err != nil || len(entries) != 2 || entries[0].Label != "Open" {
		t.Fatalf("menu=%+v err=%v", entries, err)
	}
	if err := f.registrar.Activate(reg, 1); err != nil {
		t.Fatal(err)
	}
	menus[0].mu.Lock()
	events := slices.Clone(menus[0].events)
	menus[0].mu.Unlock()
	if !slices.Equal(events, []int32{1}) {
		t.Fatalf("events=%v", events)
	}

	f.register(t, f.client, 42, f.path)
	if got, ok := f.focused(t); !ok || got.Path != f.path || f.registrar.Current(reg, f.pid) {
		t.Fatalf("registered menu must come first: registration=%+v ok=%v", got, ok)
	}
	f.unregister(t, f.client, 42)

	// A second window: either one's menu could be the focused window's.
	if err := dbusexport.Export(conn, newTestMenu(), chromiumMenuPath+"/4000002", menuInterface); err != nil {
		t.Fatal(err)
	}
	if got, ok := f.focused(t); ok || f.registrar.Current(reg, f.pid) {
		t.Fatalf("two windows' menus must show none: registration=%+v ok=%v", got, ok)
	}
	// The browser goes, another comes with one window, then a second
	// connection of the same process serves another window's menu.
	conn.Close()
	next, _ := f.serveChromium(t, "4000003")
	waitFor(t, func() bool {
		got, ok := f.focused(t)
		return ok && got.Service == next.Names()[0] && got.Path == chromiumMenuPath+"/4000003"
	})
	f.serveChromium(t, "4000004")
	if got, ok := f.focused(t); ok {
		t.Fatalf("menus on two connections must show none: registration=%+v", got)
	}
}

// A published menu's name coming or going tells the shell to look again.
func TestNativeNameTellsOfChange(t *testing.T) {
	f := start(t)
	changes := make(chan struct{}, 8)
	f.registrar.SetOnChange(func() { changes <- struct{}{} })
	wait := func(what string) {
		t.Helper()
		select {
		case <-changes:
		case <-time.After(2 * time.Second):
			t.Fatalf("no change told when the name %s", what)
		}
	}
	name := nativeMenuPrefix + "p1.w1"
	conn, _ := f.publishNative(t, name)
	wait("came")
	if _, err := conn.ReleaseName(name); err != nil {
		t.Fatal(err)
	}
	wait("went")
}
