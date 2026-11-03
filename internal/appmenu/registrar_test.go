package appmenu

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/lyricat/inua/internal/dbustest"
)

type testNode struct {
	ID       int32
	Props    map[string]dbus.Variant
	Children []dbus.Variant
}
type testMenu struct {
	mu     sync.Mutex
	events []int32
}

func (m *testMenu) AboutToShow(int32) (bool, *dbus.Error) { return false, nil }
func (m *testMenu) GetLayout(int32, int32, []string) (uint32, testNode, *dbus.Error) {
	child := testNode{ID: 1, Props: map[string]dbus.Variant{"label": dbus.MakeVariant("_Open")}, Children: []dbus.Variant{}}
	root := testNode{ID: 0, Props: map[string]dbus.Variant{}, Children: []dbus.Variant{dbus.MakeVariant(child)}}
	return 1, root, nil
}
func (m *testMenu) Event(id int32, event string, _ dbus.Variant, _ uint32) *dbus.Error {
	if event == "clicked" {
		m.mu.Lock()
		m.events = append(m.events, id)
		m.mu.Unlock()
	}
	return nil
}

func TestRegistrarLifecycleAndMenu(t *testing.T) {
	addr := dbustest.Start(t)
	server := dbustest.Conn(t, addr)
	client := dbustest.Conn(t, addr)
	menu := &testMenu{}
	path := dbus.ObjectPath("/Menu")
	if err := client.Export(menu, path, menuInterface); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	registrar := New(server)
	done := make(chan error, 1)
	go func() { done <- registrar.Run(ctx) }()
	waitFor(t, func() bool { return nameHasOwner(client) })
	var pid uint32
	if err := client.BusObject().Call("org.freedesktop.DBus.GetConnectionUnixProcessID", 0, client.Names()[0]).Store(&pid); err != nil {
		t.Fatal(err)
	}
	if err := client.Object(RegistrarName, RegistrarPath).Call(RegistrarInterface+".RegisterWindow", 0, uint32(42), path).Err; err != nil {
		t.Fatal(err)
	}
	reg, ok := registrar.ForPID(int(pid))
	if !ok || reg.Window != 42 || reg.Path != path {
		t.Fatalf("registration=%+v ok=%v", reg, ok)
	}
	entries, err := registrar.Menu(reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Label != "Open" {
		t.Fatalf("entries=%+v", entries)
	}
	if err := registrar.Activate(reg, 1); err != nil {
		t.Fatal(err)
	}
	menu.mu.Lock()
	got := append([]int32(nil), menu.events...)
	menu.mu.Unlock()
	if !reflect.DeepEqual(got, []int32{1}) {
		t.Fatalf("events=%v", got)
	}
	if err := client.Object(RegistrarName, RegistrarPath).Call(RegistrarInterface+".UnregisterWindow", 0, uint32(42)).Err; err != nil {
		t.Fatal(err)
	}
	if err := client.Object(RegistrarName, RegistrarPath).Call(RegistrarInterface+".RegisterWindow", 0, uint32(43), path).Err; err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { _, ok := registrar.ForPID(int(pid)); return ok })
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { _, ok := registrar.ForPID(int(pid)); return !ok })
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("registrar did not stop")
	}
}

func TestDecodeRejectsOversizedAndMalformedTrees(t *testing.T) {
	props := map[string]dbus.Variant{}
	good := []any{int32(0), props, []dbus.Variant{dbus.MakeVariant([]any{int32(1), props, []dbus.Variant{}})}}
	count := 0
	if _, ok := decode(good, 0, &count); !ok {
		t.Fatal("valid node rejected")
	}
	count = 0
	if _, ok := decode([]any{int32(0), props, []dbus.Variant{dbus.MakeVariant("bad")}}, 0, &count); ok {
		t.Fatal("malformed child accepted")
	}
	count = maxMenuNodes
	if _, ok := decode(good, 0, &count); ok {
		t.Fatal("oversized tree accepted")
	}
	count = 0
	if _, ok := decode(good, maxMenuDepth+1, &count); ok {
		t.Fatal("excessive depth accepted")
	}
}

func nameHasOwner(conn *dbus.Conn) bool {
	var owned bool
	return conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, RegistrarName).Store(&owned) == nil && owned
}
func waitFor(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met")
}
