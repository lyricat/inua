package dbusmenu_test

import (
	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/appmenu"
	"github.com/lyricat/inua/internal/dbusmenu"
	"github.com/lyricat/inua/internal/dbustest"
	"testing"
	"time"
)

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
	entries, err := r.Menu(reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Label != "File" || len(entries[0].Children) != 2 || entries[0].Children[1].Children[0].Label != "Report" {
		t.Fatalf("menu=%+v", entries)
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
