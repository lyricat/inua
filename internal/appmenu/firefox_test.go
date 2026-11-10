package appmenu

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"

	"github.com/lyricat/inua/internal/dbusexport"
	"github.com/lyricat/inua/internal/dbustest"
)

// renumberingMenu serves a menu as Firefox's GTK bridge does: each reading
// of its layout gives every item other ids, and a click with an id of an
// earlier reading reaches no item.
type renumberingMenu struct {
	mu     sync.Mutex
	root   *menuItem // the ids of its items are left out: readings give them
	reads  int32
	clicks []string
}

// newRenumberingMenu is File (New Window, Quit), Edit (Undo) and About.
func newRenumberingMenu() *renumberingMenu {
	return &renumberingMenu{root: &menuItem{kids: []*menuItem{
		{label: "_File", kids: []*menuItem{{label: "New Window"}, {label: "Quit"}}},
		{label: "_Edit", kids: []*menuItem{{label: "Undo"}}},
		{label: "About"},
	}}}
}

// number gives the items of a reading their ids, in order: the reading's
// number times 1000 and the item's place.
func (m *renumberingMenu) number(it *menuItem, next *int32, levels int32) testNode {
	n := testNode{ID: 0, Props: map[string]dbus.Variant{}, Children: []dbus.Variant{}}
	if it != m.root {
		*next++
		n.ID = m.reads*1000 + *next
		n.Props["label"] = dbus.MakeVariant(it.label)
	}
	if len(it.kids) > 0 && it != m.root {
		n.Props["children-display"] = dbus.MakeVariant("submenu")
	}
	for _, kid := range it.kids {
		child := m.number(kid, next, levels-1)
		if levels != 0 {
			n.Children = append(n.Children, dbus.MakeVariant(child))
		}
	}
	return n
}

// label is the item id names in the newest reading, "" for none.
func (m *renumberingMenu) label(id int32) string {
	next := int32(0)
	var find func(it *menuItem) string
	find = func(it *menuItem) string {
		for _, kid := range it.kids {
			next++
			if m.reads*1000+next == id {
				return kid.label
			}
			if l := find(kid); l != "" {
				return l
			}
		}
		return ""
	}
	return find(m.root)
}

func (m *renumberingMenu) AboutToShow(int32) (bool, *dbus.Error) { return false, nil }

func (m *renumberingMenu) GetLayout(parent, levels int32, _ []string) (uint32, testNode, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if parent != 0 {
		return 0, testNode{}, dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs", []any{"no such item"})
	}
	m.reads++
	next := int32(0)
	return uint32(m.reads), m.number(m.root, &next, levels), nil
}

func (m *renumberingMenu) Event(id int32, event string, _ dbus.Variant, _ uint32) *dbus.Error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if event == "clicked" {
		if l := m.label(id); l != "" {
			m.clicks = append(m.clicks, l)
		} else {
			m.clicks = append(m.clicks, fmt.Sprintf("stale %d", id))
		}
	}
	return nil
}

func (m *renumberingMenu) state() (reads int32, clicks []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reads, slices.Clone(m.clicks)
}

// serveFirefox serves a menu for each of windows under firefoxMenuRoot, as
// Firefox's bridge does, on a connection that owns names.
func (f *fixture) serveFirefox(t *testing.T, names []string, windows ...string) (*dbus.Conn, []*renumberingMenu) {
	t.Helper()
	conn := dbustest.Conn(t, f.addr)
	var menus []*renumberingMenu
	for _, w := range windows {
		menu := newRenumberingMenu()
		if err := dbusexport.Export(conn, menu, firefoxMenuRoot+"/"+dbus.ObjectPath(w), menuInterface); err != nil {
			t.Fatal(err)
		}
		menus = append(menus, menu)
	}
	for _, name := range names {
		if reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
			t.Fatalf("request %s: reply=%v err=%v", name, reply, err)
		}
	}
	return conn, menus
}

// fakeProcess is a process of a fake process table: its command name, the
// program it runs and its parent.
type fakeProcess struct {
	name string
	exe  string
	ppid int
}

// fakeProc writes a process table the registrar reads instead of /proc.
func (f *fixture) fakeProc(t *testing.T, procs map[int]fakeProcess) {
	t.Helper()
	dir := t.TempDir()
	for pid, p := range procs {
		d := filepath.Join(dir, strconv.Itoa(pid))
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
		stat := fmt.Sprintf("%d (%s) S %d %d %d 0 -1 4194304\n", pid, p.name, p.ppid, pid, pid)
		if err := os.WriteFile(filepath.Join(d, "stat"), []byte(stat), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(p.exe, filepath.Join(d, "exe")); err != nil {
			t.Fatal(err)
		}
	}
	f.registrar.proc = dir
}

const (
	firefoxExe     = "/usr/lib/firefox/firefox"
	firefoxProfile = firefoxNamePrefix + "ZGVmYXVsdA__"
)

// Firefox's menu is found for its process whatever other names its
// connection owns, read whole and clicked with the ids of that reading,
// and never read again in parts. A registered or published menu of the
// same process comes first.
func TestFocusedFirefoxMenu(t *testing.T) {
	f := start(t)
	f.fakeProc(t, map[int]fakeProcess{f.pid: {"firefox", firefoxExe, 1}})
	conn, menus := f.serveFirefox(t, []string{firefoxProfile, firefoxNamePrefix + "SearchProvider"}, "1")
	menu := menus[0]

	reg, ok := f.focused(t)
	want := Registration{PID: uint32(f.pid), Service: conn.Names()[0], Path: firefoxMenuRoot + "/1", Whole: true}
	if !ok || reg != want {
		t.Fatalf("firefox menu: registration=%+v ok=%v, want %+v", reg, ok, want)
	}
	if !f.registrar.Current(reg, f.pid) || f.registrar.Current(reg, f.pid+1) {
		t.Fatal("the firefox menu is current only for its own process")
	}

	entries, err := f.registrar.Menu(reg, true)
	if err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, e := range entries {
		labels = append(labels, e.Label)
	}
	if !slices.Equal(labels, []string{"File", "Edit", "About"}) || len(entries[0].Children) != 2 || entries[0].Children[0].Label != "New Window" || !entries[0].Submenu || entries[2].Submenu {
		t.Fatalf("whole menu=%+v", entries)
	}
	reads, _ := menu.state()

	// Looking the menu up again, as Current does before a click, and
	// opening a submenu, read no layout: the reading's ids still hold.
	if !f.registrar.Current(reg, f.pid) {
		t.Fatal("not current")
	}
	if _, err := f.registrar.Submenu(reg, entries[0].ID); err == nil {
		t.Fatal("a menu read whole was read in parts")
	}
	if err := f.registrar.Activate(reg, entries[0].Children[0].ID); err != nil {
		t.Fatal(err)
	}
	if got, clicks := menu.state(); got != reads || !slices.Equal(clicks, []string{"New Window"}) {
		t.Fatalf("reads %d → %d, clicks=%v", reads, got, clicks)
	}

	// Another reading has other ids.
	again, err := f.registrar.Menu(reg, false)
	if err != nil || len(again) != 3 || again[0].Children[1].ID == entries[0].Children[1].ID {
		t.Fatalf("second reading=%+v err=%v", again, err)
	}
	if err := f.registrar.Activate(reg, again[0].Children[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, clicks := menu.state(); !slices.Equal(clicks, []string{"New Window", "Quit"}) {
		t.Fatalf("clicks=%v", clicks)
	}

	// A registered menu comes first, then a published one.
	f.register(t, f.client, 42, f.path)
	if got, ok := f.focused(t); !ok || got.Path != f.path || f.registrar.Current(reg, f.pid) {
		t.Fatalf("registered menu must come first: registration=%+v ok=%v", got, ok)
	}
	f.unregister(t, f.client, 42)
	native := nativeMenuPrefix + "p1.w1"
	nconn, _ := f.publishNative(t, native)
	if got, ok := f.focused(t); !ok || got.Service != native || f.registrar.Current(reg, f.pid) {
		t.Fatalf("published menu must come first: registration=%+v ok=%v", got, ok)
	}
	if _, err := nconn.ReleaseName(native); err != nil {
		t.Fatal(err)
	}
	if got, ok := f.focused(t); !ok || got != reg {
		t.Fatalf("firefox menu not back: registration=%+v ok=%v", got, ok)
	}
}

// Firefox's menu is its own process's, or a Firefox process's it
// started; never a process's that started Firefox, nor another program's
// Firefox started, nor a process that cannot be read.
func TestFirefoxMenuByProcessTree(t *testing.T) {
	f := start(t)
	const (
		terminal = 900040
		shell    = 900050
		content  = 900001
		isolated = 900002
		viewer   = 900003
		reopened = 900004
		odd      = 900005
		missing  = 900099
	)
	f.fakeProc(t, map[int]fakeProcess{
		terminal: {"foot", "/usr/bin/foot", 1},
		shell:    {"bash", "/usr/bin/bash", terminal},
		f.pid:    {"firefox", firefoxExe, shell},
		content:  {"Web Content", firefoxExe, f.pid},
		isolated: {"Isolated Web Co", firefoxExe, content},
		viewer:   {"evince", "/usr/bin/evince", f.pid},
		reopened: {"firefox", firefoxExe, viewer},
		odd:      {"a) (b", firefoxExe, f.pid},
	})
	conn, _ := f.serveFirefox(t, []string{firefoxProfile}, "1")

	for _, pid := range []int{f.pid, content, isolated, odd} {
		reg, ok := f.registrar.Focused(pid)
		if !ok || reg.Service != conn.Names()[0] || int(reg.PID) != pid || !reg.Whole {
			t.Fatalf("pid %d: registration=%+v ok=%v", pid, reg, ok)
		}
		if !f.registrar.Current(reg, pid) {
			t.Fatalf("pid %d: not current", pid)
		}
		if pid != terminal && f.registrar.Current(reg, terminal) {
			t.Fatalf("pid %d: current for the terminal", pid)
		}
	}
	for _, pid := range []int{terminal, shell, viewer, reopened, missing, 0, -1} {
		if reg, ok := f.registrar.Focused(pid); ok {
			t.Fatalf("pid %d: found %+v", pid, reg)
		}
	}
}

// Menus the focused window's cannot be told among give none: a Firefox
// with menus for two windows, or two Firefoxes of the process. The one
// left is found once the other goes.
func TestFirefoxMenuAmbiguous(t *testing.T) {
	f := start(t)
	f.fakeProc(t, map[int]fakeProcess{f.pid: {"firefox", firefoxExe, 1}})
	conn, _ := f.serveFirefox(t, []string{firefoxProfile}, "1", "2")
	if reg, ok := f.focused(t); ok {
		t.Fatalf("two windows' menus must show none: registration=%+v", reg)
	}
	conn.Close()

	one, _ := f.serveFirefox(t, []string{firefoxProfile}, "1")
	waitFor(t, func() bool {
		reg, ok := f.focused(t)
		return ok && reg.Service == one.Names()[0]
	})
	other, _ := f.serveFirefox(t, []string{firefoxNamePrefix + "b3RoZXI_"}, "7")
	if reg, ok := f.focused(t); ok {
		t.Fatalf("two firefoxes' menus must show none: registration=%+v", reg)
	}
	other.Close()
	waitFor(t, func() bool {
		reg, ok := f.focused(t)
		return ok && reg.Service == one.Names()[0]
	})
}

// A Firefox without a menu, one whose introspection is malformed or names
// no valid path, a menu whose layout is malformed or too deep, and a bus
// that is gone give no menu.
func TestFirefoxMenuFailures(t *testing.T) {
	f := start(t)
	f.fakeProc(t, map[int]fakeProcess{f.pid: {"firefox", firefoxExe, 1}})
	// serve takes name for a connection serving obj's iface at path, if any.
	serve := func(name string, path dbus.ObjectPath, iface string, obj any) *dbus.Conn {
		t.Helper()
		conn := dbustest.Conn(t, f.addr)
		if obj != nil {
			if err := dbusexport.Export(conn, obj, path, iface); err != nil {
				t.Fatal(err)
			}
		}
		if reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
			t.Fatalf("request %s: reply=%v err=%v", name, reply, err)
		}
		return conn
	}
	cases := []struct {
		name  string
		path  dbus.ObjectPath
		iface string
		obj   any
	}{
		{"bare", "", "", nil},
		{"malformed", firefoxMenuRoot, "org.freedesktop.DBus.Introspectable", introspect.Introspectable(`<node><node name="1"`)},
		{"invalid", firefoxMenuRoot, "org.freedesktop.DBus.Introspectable", introspect.Introspectable(`<node><node name="a-b"/><node name=""/></node>`)},
	}
	for _, c := range cases {
		conn := serve(firefoxNamePrefix+c.name, c.path, c.iface, c.obj)
		if reg, ok := f.focused(t); ok {
			t.Fatalf("%s: registration=%+v", c.name, reg)
		}
		conn.Close()
	}

	// A layout of the wrong shape, and one deeper than the bounds.
	conn := serve(firefoxNamePrefix+"bad", firefoxMenuRoot+"/1", menuInterface, badLayout{})
	reg := Registration{PID: uint32(f.pid), Service: conn.Names()[0], Path: firefoxMenuRoot + "/1", Whole: true}
	if entries, err := f.registrar.Menu(reg, true); err == nil {
		t.Fatalf("malformed layout read: %+v", entries)
	}
	menu := newRenumberingMenu()
	deep := &menuItem{label: "Deep"}
	for range maxMenuDepth + 2 {
		deep = &menuItem{label: "Level", kids: []*menuItem{deep}}
	}
	menu.root.kids = append(menu.root.kids, deep)
	conn = serve(firefoxNamePrefix+"deep", firefoxMenuRoot+"/1", menuInterface, menu)
	reg.Service = conn.Names()[0]
	if entries, err := f.registrar.Menu(reg, true); err == nil {
		t.Fatalf("too deep a menu read: %+v", entries)
	}

	gone := dbustest.Conn(t, f.addr)
	r := New(gone)
	r.proc = f.registrar.proc
	gone.Close()
	if reg, ok := r.Focused(f.pid); ok {
		t.Fatalf("closed bus: registration=%+v", reg)
	}
}

// badLayout answers GetLayout with a layout of the wrong type.
type badLayout struct{}

func (badLayout) AboutToShow(int32) (bool, *dbus.Error) { return false, nil }
func (badLayout) GetLayout(int32, int32, []string) (uint32, string, *dbus.Error) {
	return 1, "menu", nil
}

// A Firefox name coming or going tells the shell to look again: Firefox
// may have started after its window took the focus.
func TestFirefoxNameTellsOfChange(t *testing.T) {
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
	conn, _ := f.serveFirefox(t, []string{firefoxProfile}, "1")
	wait("came")
	if _, err := conn.ReleaseName(firefoxProfile); err != nil {
		t.Fatal(err)
	}
	wait("went")
}
