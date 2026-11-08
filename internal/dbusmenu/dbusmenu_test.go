package dbusmenu_test

import (
	"os"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/appmenu"
	"github.com/lyricat/inua/internal/dbusmenu"
	"github.com/lyricat/inua/internal/dbustest"
	"testing"
	"time"
)

// An inua app's menu is found by the name it publishes, without the
// application telling the Canonical registrar first.
func TestNativeDiscovery(t *testing.T) {
	addr := dbustest.Start(t)
	serverConn := dbustest.Conn(t, addr)
	name := "io.github.lyricat.Inua.Menu.p1.w1"
	if _, err := serverConn.RequestName(name, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	s := dbusmenu.New(serverConn, func(string) {})
	if err := s.Publish([]dbusmenu.Node{{Label: "File", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	readerConn := dbustest.Conn(t, addr)
	r := appmenu.New(readerConn)
	reg, ok := r.Native(os.Getpid())
	if !ok || reg.Service != name || reg.Path != dbusmenu.Path {
		t.Fatalf("registration=%+v, ok=%v", reg, ok)
	}
	entries, err := r.Menu(reg, true)
	if err != nil || len(entries) != 1 || entries[0].Label != "File" {
		t.Fatalf("menu=%+v err=%v", entries, err)
	}
}

// A menu published by Publish is found for the process that published it,
// without an X11 window registered with the Canonical registrar.
func TestPublishedMenuFound(t *testing.T) {
	addr := dbustest.Start(t)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", addr)
	p, err := dbusmenu.Publish([]dbusmenu.Node{{Label: "File", Enabled: true}}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	r := appmenu.New(dbustest.Conn(t, addr))
	reg, ok := r.Native(os.Getpid())
	if !ok {
		t.Fatal("not found")
	}
	entries, err := r.Menu(reg, true)
	if err != nil || len(entries) != 1 || entries[0].Label != "File" {
		t.Fatalf("menu=%+v err=%v", entries, err)
	}
}

func TestPublishMenuRoundTrip(t *testing.T) {
	addr := dbustest.Start(t)
	serverConn := dbustest.Conn(t, addr)
	readerConn := dbustest.Conn(t, addr)
	clicked := make(chan string, 1)
	name := "io.github.lyricat.Inua.Menu.test"
	if _, err := serverConn.RequestName(name, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	s := dbusmenu.New(serverConn, func(action string) { clicked <- action })
	tree := []dbusmenu.Node{{Label: "File", Enabled: true, Children: []dbusmenu.Node{{Label: "Open", Action: "open", Enabled: true}, {Label: "Recent", Enabled: true, Children: []dbusmenu.Node{{Label: "Report", Action: "report", Enabled: true}}}}}}
	if err := s.Publish(tree); err != nil {
		t.Fatal(err)
	}
	reg := appmenu.Registration{PID: 1, Service: name, Path: dbusmenu.Path}
	r := appmenu.New(readerConn)
	entries, err := r.Menu(reg, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Label != "File" || !entries[0].Submenu {
		t.Fatalf("menu=%+v", entries)
	}
	children, err := r.Submenu(reg, entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 2 || children[1].Label != "Recent" || !children[1].Submenu {
		t.Fatalf("submenu=%+v", children)
	}
	report, err := r.Submenu(reg, children[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(report) != 1 || report[0].Label != "Report" {
		t.Fatalf("nested=%+v", report)
	}
	if err := r.Activate(reg, 4); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-clicked:
		if got != "report" {
			t.Fatalf("action=%q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("click not delivered")
	}
}

// Publishing again replaces the tree, an empty menu stays published, and
// Close withdraws the menu and its name.
func TestPublishAgainAndClose(t *testing.T) {
	addr := dbustest.Start(t)
	serverConn := dbustest.Conn(t, addr)
	readerConn := dbustest.Conn(t, addr)
	name := "io.github.lyricat.Inua.Menu.p2.w1"
	s := dbusmenu.New(serverConn, func(string) {})
	if err := s.OwnName(name); err != nil {
		t.Fatal(err)
	}
	reg := appmenu.Registration{PID: 2, Service: name, Path: dbusmenu.Path}
	r := appmenu.New(readerConn)
	if err := s.Publish([]dbusmenu.Node{{Label: "File", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Publish([]dbusmenu.Node{{Label: "Edit", Enabled: true}, {Label: "View", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	entries, err := r.Menu(reg, true)
	if err != nil || len(entries) != 2 || entries[0].Label != "Edit" || entries[1].Label != "View" {
		t.Fatalf("menu=%+v err=%v", entries, err)
	}
	if err := s.Publish(nil); err != nil {
		t.Fatal(err)
	}
	if entries, err := r.Menu(reg, true); err != nil || len(entries) != 0 {
		t.Fatalf("empty menu=%+v err=%v", entries, err)
	}
	s.Close(name)
	var owned bool
	if err := readerConn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, name).Store(&owned); err != nil {
		t.Fatal(err)
	}
	if owned {
		t.Fatalf("%s still owned after Close", name)
	}
}
