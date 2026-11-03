package appmenu

import (
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/dbustest"
)

// KDE applications (Dolphin, Kate) publish their menu at /MenuBar/N on
// their own well-known name, not the Canonical registrar's: the path is
// the first child the object lists.
func TestKDEMenuPathFromIntrospection(t *testing.T) {
	addr := startBus(t)
	conn := connTo(t, addr)
	const xml = `<!DOCTYPE node PUBLIC "-//freedesktop//DTD D-BUS Object Introspection 1.0//EN"
 "<http://www.freedesktop.org/standards/dbus/1.0/introspect.dtd">
<node name="/MenuBar">
  <node name="2"/>
</node>`
	if err := conn.Export(introspectable{xml: xml}, "/MenuBar", "org.freedesktop.DBus.Introspectable"); err != nil {
		t.Fatal(err)
	}
	path, err := kdeMenuPath(conn, uniqueName(t, conn))
	if err != nil {
		t.Fatal(err)
	}
	if path != "/MenuBar/2" {
		t.Fatalf("path = %q, want /MenuBar/2", path)
	}
}

// The bus answers with a root node that has no name of its own; the fixture
// above names it, so this is the shape that actually reaches the reader
// (Kate's /MenuBar was read as having no menu at all).
func TestKDEMenuPathFromBusShape(t *testing.T) {
	addr := startBus(t)
	conn := connTo(t, addr)
	const xml = `<!DOCTYPE node PUBLIC "-//freedesktop//DTD D-BUS Object Introspection 1.0//EN"
 "http://www.freedesktop.org/standards/dbus/1.0/introspect.dtd">
<node>
  <interface name="org.freedesktop.DBus.Introspectable">
    <method name="Introspect">
      <arg name="xml_data" type="s" direction="out"/>
    </method>
  </interface>
  <node name="1"/>
</node>`
	if err := conn.Export(introspectable{xml: xml}, "/MenuBar", "org.freedesktop.DBus.Introspectable"); err != nil {
		t.Fatal(err)
	}
	path, err := kdeMenuPath(conn, uniqueName(t, conn))
	if err != nil {
		t.Fatal(err)
	}
	if path != "/MenuBar/1" {
		t.Fatalf("path = %q, want /MenuBar/1", path)
	}
}

// An object with no children of its own is no menu.
func TestKDEMenuPathEmpty(t *testing.T) {
	addr := startBus(t)
	conn := connTo(t, addr)
	if err := conn.Export(introspectable{xml: `<node/>`}, "/MenuBar", "org.freedesktop.DBus.Introspectable"); err != nil {
		t.Fatal(err)
	}
	path, err := kdeMenuPath(conn, uniqueName(t, conn))
	if err != nil || path != "" {
		t.Fatalf("path = %q, %v; want none", path, err)
	}
}

// A menu bar of sections with nothing under them is not a menu to show:
// an application that fills its menus only when they open (a browser)
// would give the bar a row of empty sections.
func TestLabelledNeedsRealSubmenus(t *testing.T) {
	if labelled([]Entry{{Label: "File"}}) {
		t.Error("a section with no items counted as a menu")
	}
	if !labelled([]Entry{{Label: "File", Children: []Entry{{Label: "Open"}}}}) {
		t.Error("a section with items did not count as a menu")
	}
	if labelled([]Entry{{Label: ""}}) {
		t.Error("an unnamed item counted as a menu")
	}
}

// startBus runs a private bus and returns its address, skipping the test
// when dbus-daemon is not installed.
func startBus(t *testing.T) string {
	t.Helper()
	return dbustest.Start(t)
}

// connTo connects a second connection to the private bus.
func connTo(t *testing.T, addr string) *dbus.Conn {
	t.Helper()
	return dbustest.Conn(t, addr)
}

// uniqueName is conn's own bus name, which every connection has.
func uniqueName(t *testing.T, conn *dbus.Conn) string {
	t.Helper()
	var name string
	if err := conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, "org.freedesktop.DBus").Store(&name); err != nil {
		t.Fatal(err)
	}
	names := conn.Names()
	if len(names) == 0 {
		t.Fatal("the connection has no name")
	}
	return names[0]
}

type introspectable struct{ xml string }

func (i introspectable) Introspect() (string, *dbus.Error) { return i.xml, nil }

type firefoxTestMenu struct {
	testMenu
}

func (m *firefoxTestMenu) GetLayout(int32, int32, []string) (uint32, testNode, *dbus.Error) {
	item := testNode{
		ID:       10,
		Props:    map[string]dbus.Variant{"label": dbus.MakeVariant("New _Tab"), "enabled": dbus.MakeVariant(true)},
		Children: []dbus.Variant{},
	}
	section := testNode{
		ID:       1,
		Props:    map[string]dbus.Variant{"label": dbus.MakeVariant("_File"), "children-display": dbus.MakeVariant("submenu")},
		Children: []dbus.Variant{dbus.MakeVariant(item)},
	}
	root := testNode{
		ID:       0,
		Props:    map[string]dbus.Variant{},
		Children: []dbus.Variant{dbus.MakeVariant(section)},
	}
	return 1, root, nil
}

// Firefox on Wayland exports its global menu at /com/canonical/menu/0 on its
// own org.mozilla.firefox.* bus name without registering on
// com.canonical.AppMenu.Registrar.
func TestFirefoxWaylandMenu(t *testing.T) {
	addr := startBus(t)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", addr)
	conn := connTo(t, addr)
	reply, err := conn.RequestName("org.mozilla.firefox.default", dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("RequestName = %v, %v", reply, err)
	}
	const xml = `<!DOCTYPE node PUBLIC "-//freedesktop//DTD D-BUS Object Introspection 1.0//EN"
 "http://www.freedesktop.org/standards/dbus/1.0/introspect.dtd">
<node>
  <node name="0"/>
</node>`
	if err := conn.Export(introspectable{xml: xml}, firefoxMenuRoot, "org.freedesktop.DBus.Introspectable"); err != nil {
		t.Fatal(err)
	}
	menu := &firefoxTestMenu{}
	if err := conn.Export(menu, "/com/canonical/menu/0", menuInterface); err != nil {
		t.Fatal(err)
	}
	var pid uint32
	if err := conn.BusObject().Call("org.freedesktop.DBus.GetConnectionUnixProcessID", 0, "org.mozilla.firefox.default").Store(&pid); err != nil {
		t.Fatal(err)
	}
	client := NewClient(nil, nil)
	entries, err := client.Menu(int(pid))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Label != "File" || len(entries[0].Children) != 1 || entries[0].Children[0].Label != "New Tab" {
		t.Fatalf("entries = %+v", entries)
	}
	if !entries[0].Action.Direct || !entries[0].Children[0].Action.Direct {
		t.Fatalf("expected direct dbusmenu actions: %+v", entries)
	}
	if err := client.Activate(entries[0].Children[0]); err != nil {
		t.Fatal(err)
	}
	menu.mu.Lock()
	defer menu.mu.Unlock()
	if len(menu.events) != 1 || menu.events[0] != 10 {
		t.Fatalf("events = %v, want [10]", menu.events)
	}
}
