package appmenu

import (
	"context"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/dbusexport"
	"github.com/lyricat/inua/internal/dbustest"
)

type testNode struct {
	ID       int32
	Props    map[string]dbus.Variant
	Children []dbus.Variant
}

// menuItem is an item of the test application's menu. A lazy one has its
// children only once the application was told it is about to show.
type menuItem struct {
	id    int32
	label string
	lazy  bool
	kids  []*menuItem
}

type testMenu struct {
	mu     sync.Mutex
	root   *menuItem
	events []int32
	about  []int32
}

// newTestMenu is Open, and Recent, which fills in when asked: File A and
// More, which fills in when asked too.
func newTestMenu() *testMenu {
	return &testMenu{root: &menuItem{kids: []*menuItem{
		{id: 1, label: "_Open"},
		{id: 7, label: "_Recent", lazy: true, kids: []*menuItem{
			{id: 10, label: "File A"},
			{id: 8, label: "More", lazy: true, kids: []*menuItem{{id: 9, label: "Deep"}}},
		}},
	}}}
}

func (m *testMenu) find(it *menuItem, id int32) *menuItem {
	if it.id == id {
		return it
	}
	for _, kid := range it.kids {
		if found := m.find(kid, id); found != nil {
			return found
		}
	}
	return nil
}

func (m *testMenu) node(it *menuItem, levels int32) testNode {
	n := testNode{ID: it.id, Props: map[string]dbus.Variant{}, Children: []dbus.Variant{}}
	if it.label != "" {
		n.Props["label"] = dbus.MakeVariant(it.label)
	}
	if it.lazy {
		n.Props["children-display"] = dbus.MakeVariant("submenu")
	}
	if levels == 0 || it.lazy && !slices.Contains(m.about, it.id) {
		return n
	}
	for _, kid := range it.kids {
		n.Children = append(n.Children, dbus.MakeVariant(m.node(kid, levels-1)))
	}
	return n
}

func (m *testMenu) AboutToShow(id int32) (bool, *dbus.Error) {
	m.mu.Lock()
	m.about = append(m.about, id)
	m.mu.Unlock()
	return true, nil
}

func (m *testMenu) GetLayout(parent, levels int32, _ []string) (uint32, testNode, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it := m.find(m.root, parent)
	if it == nil {
		return 0, testNode{}, dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs", []any{"no such item"})
	}
	return 1, m.node(it, levels), nil
}

func (m *testMenu) Event(id int32, event string, _ dbus.Variant, _ uint32) *dbus.Error {
	if event == "clicked" {
		m.mu.Lock()
		m.events = append(m.events, id)
		m.mu.Unlock()
	}
	return nil
}

func (m *testMenu) asked() []int32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.about)
}

// fixture is a registrar on a private bus and an application with a menu
// at /Menu, not yet registered.
type fixture struct {
	addr      string
	registrar *Registrar
	client    *dbus.Conn
	menu      *testMenu
	path      dbus.ObjectPath
	pid       int
}

func start(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{addr: dbustest.Start(t), menu: newTestMenu(), path: "/Menu"}
	server := dbustest.Conn(t, f.addr)
	f.client = dbustest.Conn(t, f.addr)
	if err := dbusexport.Export(f.client, f.menu, f.path, menuInterface); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	f.registrar = New(server)
	done := make(chan error, 1)
	go func() { done <- f.registrar.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(time.Second):
			t.Error("registrar did not stop")
		}
	})
	waitFor(t, func() bool { return nameHasOwner(f.client) })
	var pid uint32
	if err := f.client.BusObject().Call("org.freedesktop.DBus.GetConnectionUnixProcessID", 0, f.client.Names()[0]).Store(&pid); err != nil {
		t.Fatal(err)
	}
	f.pid = int(pid)
	return f
}

func (f *fixture) register(t *testing.T, conn *dbus.Conn, window uint32, path dbus.ObjectPath) {
	t.Helper()
	if err := conn.Object(RegistrarName, RegistrarPath).Call(RegistrarInterface+".RegisterWindow", 0, window, path).Err; err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) unregister(t *testing.T, conn *dbus.Conn, window uint32) {
	t.Helper()
	if err := conn.Object(RegistrarName, RegistrarPath).Call(RegistrarInterface+".UnregisterWindow", 0, window).Err; err != nil {
		t.Fatal(err)
	}
}

func TestRegistrarLifecycleAndMenu(t *testing.T) {
	f := start(t)
	f.register(t, f.client, 42, f.path)
	reg, ok := f.registrar.ForPID(f.pid)
	if !ok || reg.Window != 42 || reg.Path != f.path {
		t.Fatalf("registration=%+v ok=%v", reg, ok)
	}
	entries, err := f.registrar.Menu(reg, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{{ID: 1, Label: "Open", Enabled: true}, {ID: 7, Label: "Recent", Enabled: true, Submenu: true}}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries=%+v", entries)
	}
	if err := f.registrar.Activate(reg, 1); err != nil {
		t.Fatal(err)
	}
	f.menu.mu.Lock()
	got := slices.Clone(f.menu.events)
	f.menu.mu.Unlock()
	if !reflect.DeepEqual(got, []int32{1}) {
		t.Fatalf("events=%v", got)
	}
	f.unregister(t, f.client, 42)
	if _, ok := f.registrar.ForPID(f.pid); ok {
		t.Fatal("still registered after UnregisterWindow")
	}
	f.register(t, f.client, 43, f.path)
	if _, ok := f.registrar.ForPID(f.pid); !ok {
		t.Fatal("not registered again")
	}
	if err := f.client.Close(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { _, ok := f.registrar.ForPID(f.pid); return !ok })
}

// A process registers each of its windows, and usually all of them at the
// one menu: it still has a menu, the latest registered.
func TestForPIDWithSeveralWindows(t *testing.T) {
	f := start(t)
	f.register(t, f.client, 42, f.path)
	f.register(t, f.client, 43, f.path)
	reg, ok := f.registrar.ForPID(f.pid)
	if !ok || reg.Window != 43 || reg.Path != f.path {
		t.Fatalf("two windows at one menu: registration=%+v ok=%v", reg, ok)
	}
	f.register(t, f.client, 44, "/Other")
	if reg, ok := f.registrar.ForPID(f.pid); !ok || reg.Path != "/Other" {
		t.Fatalf("a later menu: registration=%+v ok=%v", reg, ok)
	}
	f.unregister(t, f.client, 44)
	if reg, ok := f.registrar.ForPID(f.pid); !ok || reg.Window != 43 {
		t.Fatalf("after it went: registration=%+v ok=%v", reg, ok)
	}
}

// WindowRegistered carries the window, the service and the menu's path, as
// the registrars before this one send it.
func TestWindowRegisteredSignal(t *testing.T) {
	f := start(t)
	listener := dbustest.Conn(t, f.addr)
	if err := listener.AddMatchSignal(dbus.WithMatchInterface(RegistrarInterface), dbus.WithMatchMember("WindowRegistered")); err != nil {
		t.Fatal(err)
	}
	sigs := make(chan *dbus.Signal, 4)
	listener.Signal(sigs)
	f.register(t, f.client, 42, f.path)
	select {
	case sig := <-sigs:
		var window uint32
		var service string
		var path dbus.ObjectPath
		if err := dbus.Store(sig.Body, &window, &service, &path); err != nil {
			t.Fatalf("body %v: %v", sig.Body, err)
		}
		if window != 42 || service != f.client.Names()[0] || path != f.path {
			t.Fatalf("WindowRegistered(%d, %q, %q)", window, service, path)
		}
	case <-time.After(time.Second):
		t.Fatal("no WindowRegistered")
	}
}

// Any peer can send a signal named NameOwnerChanged straight to the
// registrar; only the bus's own takes a registration away.
func TestOwnerChangedOnlyFromTheBus(t *testing.T) {
	f := start(t)
	f.register(t, f.client, 42, f.path)
	victim := f.client.Names()[0]
	gone := []any{victim, victim, ""}
	for name, sig := range map[string]*dbus.Signal{
		"another sender": {Sender: ":1.999", Path: busPath, Name: ownerChanged, Body: gone},
		"another path":   {Sender: busInterface, Path: "/elsewhere", Name: ownerChanged, Body: gone},
		"other types":    {Sender: busInterface, Path: busPath, Name: ownerChanged, Body: []any{victim, uint32(1), ""}},
		"too short":      {Sender: busInterface, Path: busPath, Name: ownerChanged, Body: []any{victim, ""}},
	} {
		f.registrar.handle(sig)
		if _, ok := f.registrar.ForPID(f.pid); !ok {
			t.Fatalf("%s: the registration was removed", name)
		}
	}
	f.registrar.handle(&dbus.Signal{Sender: busInterface, Path: busPath, Name: ownerChanged, Body: gone})
	if _, ok := f.registrar.ForPID(f.pid); ok {
		t.Fatal("the bus's signal did not remove the registration")
	}
}

// A submenu the application fills in only when asked is asked for, and so
// is one inside it.
func TestSubmenuAsksLazyItems(t *testing.T) {
	f := start(t)
	f.register(t, f.client, 42, f.path)
	reg, _ := f.registrar.ForPID(f.pid)
	if _, err := f.registrar.Menu(reg, true); err != nil {
		t.Fatal(err)
	}
	if got := f.menu.asked(); !reflect.DeepEqual(got, []int32{0}) {
		t.Fatalf("reading the first level asked %v", got)
	}
	entries, err := f.registrar.Submenu(reg, 7)
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{
		{ID: 10, Label: "File A", Enabled: true},
		{ID: 8, Label: "More", Enabled: true, Submenu: true, Children: []Entry{{ID: 9, Label: "Deep", Enabled: true}}},
	}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries=%+v", entries)
	}
	if got := f.menu.asked(); !reflect.DeepEqual(got, []int32{0, 7, 8}) {
		t.Fatalf("asked %v", got)
	}
}

// The application's word that its menu changed is told on when it comes
// from the connection that registered the menu and is about its first level.
func TestMenuUpdatesFromTheRegisteredMenu(t *testing.T) {
	f := start(t)
	f.register(t, f.client, 42, f.path)
	reg, _ := f.registrar.ForPID(f.pid)
	if _, err := f.registrar.Menu(reg, true); err != nil {
		t.Fatal(err)
	}
	updates := make(chan Registration, 8)
	f.registrar.SetOnUpdate(func(reg Registration) { updates <- reg })
	type changed struct {
		ID    int32
		Props map[string]dbus.Variant
	}
	type removed struct {
		ID    int32
		Props []string
	}
	label := map[string]dbus.Variant{"label": dbus.MakeVariant("Renamed")}
	// Each one that must not be told on is sent before one that must, and
	// has reached the bus by then: the registrar takes them in that order.
	stranger := dbustest.Conn(t, f.addr)
	emit := func(conn *dbus.Conn, path dbus.ObjectPath, name string, body ...any) {
		t.Helper()
		if err := conn.Emit(path, name, body...); err != nil {
			t.Fatal(err)
		}
		if !nameHasOwner(conn) {
			t.Fatal("the registrar has gone")
		}
	}
	expect := func(what string) {
		t.Helper()
		select {
		case got := <-updates:
			if got != reg {
				t.Fatalf("%s: told of %+v", what, got)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s: not told", what)
		}
		select {
		case got := <-updates:
			t.Fatalf("%s: told twice, of %+v", what, got)
		default:
		}
	}
	emit(stranger, f.path, layoutUpdated, uint32(2), int32(0))
	emit(f.client, "/Other", layoutUpdated, uint32(2), int32(0))
	emit(f.client, f.path, layoutUpdated, uint32(2), int32(9))
	emit(f.client, f.path, layoutUpdated, uint32(2), int32(0))
	expect("LayoutUpdated")
	emit(f.client, f.path, propertiesUpdated, []changed{{ID: 9, Props: label}}, []removed{})
	emit(f.client, f.path, propertiesUpdated, []changed{{ID: 7, Props: label}}, []removed{})
	expect("ItemsPropertiesUpdated")
	emit(f.client, f.path, propertiesUpdated, []changed{}, []removed{{ID: 1, Props: []string{"enabled"}}})
	expect("ItemsPropertiesUpdated with a removed property")
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
