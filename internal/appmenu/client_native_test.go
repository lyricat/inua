package appmenu

import (
	"github.com/godbus/dbus/v5"
	"github.com/lyricat/inua/internal/dbustest"
	"testing"
)

func TestNativeWaylandLookupAmbiguity(t *testing.T) {
	addr := dbustest.Start(t)
	srv := dbustest.Conn(t, addr)
	client := dbustest.Conn(t, addr)
	for _, n := range []string{"io.github.lyricat.Inua.Menu.p1.w1", "io.github.lyricat.Inua.Menu.p1.w2"} {
		if _, err := srv.RequestName(n, dbus.NameFlagDoNotQueue); err != nil {
			t.Fatal(err)
		}
	}
	names := []string{}
	if err := client.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		t.Fatal(err)
	}
	if len(names) == 0 {
		t.Fatal("no names")
	}
}
