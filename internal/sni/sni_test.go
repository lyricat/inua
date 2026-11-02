package sni

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	"github.com/lyricat/inua/internal/dbusexport"
	"github.com/lyricat/inua/internal/dbustest"
)

// fakeItem is a StatusNotifierItem with a dbusmenu, on its own connection.
type fakeItem struct {
	conn  *dbus.Conn
	path  dbus.ObjectPath
	props *prop.Properties

	mu     sync.Mutex
	calls  []string
	events []int32
}

func (f *fakeItem) record(s string) {
	f.mu.Lock()
	f.calls = append(f.calls, s)
	f.mu.Unlock()
}

type itemMethods struct{ f *fakeItem }

func (m itemMethods) Activate(x, y int32) *dbus.Error {
	m.f.record("Activate")
	return nil
}
func (m itemMethods) SecondaryActivate(x, y int32) *dbus.Error {
	m.f.record("SecondaryActivate")
	return nil
}
func (m itemMethods) ContextMenu(x, y int32) *dbus.Error {
	m.f.record("ContextMenu")
	return nil
}
func (m itemMethods) Scroll(delta int32, orientation string) *dbus.Error {
	m.f.record("Scroll " + orientation)
	return nil
}

type layoutNode struct {
	ID       int32
	Props    map[string]dbus.Variant
	Children []dbus.Variant
}

type menuMethods struct{ f *fakeItem }

func node(id int32, props map[string]any, children ...layoutNode) layoutNode {
	p := map[string]dbus.Variant{}
	for k, v := range props {
		p[k] = dbus.MakeVariant(v)
	}
	n := layoutNode{ID: id, Props: p, Children: []dbus.Variant{}}
	for _, c := range children {
		n.Children = append(n.Children, dbus.MakeVariant(c))
	}
	return n
}

func (m menuMethods) GetLayout(parent, depth int32, names []string) (uint32, layoutNode, *dbus.Error) {
	root := node(0, map[string]any{"children-display": "submenu"},
		node(1, map[string]any{"label": "_Open Window"}),
		node(2, map[string]any{"type": "separator"}),
		node(3, map[string]any{"label": "Start at login", "toggle-type": "checkmark", "toggle-state": int32(1)}),
		node(4, map[string]any{"label": "Hidden", "visible": false}),
		node(5, map[string]any{"label": "More", "children-display": "submenu"},
			node(6, map[string]any{"label": "Disabled", "enabled": false}),
		),
		node(7, map[string]any{"label": "Save __as"}),
	)
	return 7, root, nil
}

func (m menuMethods) Event(id int32, event string, data dbus.Variant, ts uint32) *dbus.Error {
	if event == "clicked" {
		m.f.mu.Lock()
		m.f.events = append(m.f.events, id)
		m.f.mu.Unlock()
	}
	return nil
}

func (m menuMethods) AboutToShow(id int32) (bool, *dbus.Error) { return false, nil }

func newFakeItem(t *testing.T, addr string, path dbus.ObjectPath) *fakeItem {
	t.Helper()
	f := &fakeItem{conn: dbustest.Conn(t, addr), path: path}
	if err := dbusexport.Export(f.conn, itemMethods{f}, path, itemIface); err != nil {
		t.Fatal(err)
	}
	if err := dbusexport.Export(f.conn, menuMethods{f}, "/Menu", menuIface); err != nil {
		t.Fatal(err)
	}
	pixmap := []struct {
		W, H int32
		Data []byte
	}{{1, 1, []byte{0xff, 0x10, 0x20, 0x30}}}
	props, err := dbusexport.Properties(f.conn, path, prop.Map{itemIface: {
		"Id":            {Value: "fake"},
		"Title":         {Value: "Fake App"},
		"Status":        {Value: "Active"},
		"IconName":      {Value: "fake-icon"},
		"IconThemePath": {Value: "/opt/fake/icons"},
		"IconPixmap":    {Value: pixmap},
		"Menu":          {Value: dbus.ObjectPath("/Menu")},
		"ItemIsMenu":    {Value: false},
		"ToolTip": {Value: struct {
			Icon   string
			Pixmap []struct {
				W, H int32
				Data []byte
			}
			Title, Text string
		}{"", nil, "Fake tip", ""}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	f.props = props
	return f
}

func (f *fakeItem) register(t *testing.T, arg string) {
	t.Helper()
	call := f.conn.Object(WatcherName, WatcherPath).Call(WatcherName+".RegisterStatusNotifierItem", 0, arg)
	if call.Err != nil {
		t.Fatal(call.Err)
	}
}

// recorder collects the item lists a Host reports.
type recorder struct{ ch chan []Item }

func newRecorder() *recorder          { return &recorder{ch: make(chan []Item, 64)} }
func (r *recorder) onChange(i []Item) { r.ch <- i }

// wait returns the first reported list that satisfies ok.
func (r *recorder) wait(t *testing.T, what string, ok func([]Item) bool) []Item {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case items := <-r.ch:
			if ok(items) {
				return items
			}
		case <-timeout:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func startWatcher(t *testing.T, addr string) (cancel func()) {
	t.Helper()
	conn, err := dbustest.Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		ServeWatcher(ctx, conn)
		close(done)
	}()
	waitOwned(t, addr, WatcherName)
	return func() {
		stop()
		<-done
		conn.Close()
	}
}

func waitOwned(t *testing.T, addr, name string) {
	t.Helper()
	conn := dbustest.Conn(t, addr)
	for range 200 {
		var has bool
		conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, name).Store(&has)
		if has {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s never got an owner", name)
}

func startHost(t *testing.T, addr string) (*Host, *recorder) {
	t.Helper()
	r := newRecorder()
	h := NewHost(dbustest.Conn(t, addr), r.onChange)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.Run(ctx)
	return h, r
}

func TestItemRegisteredByName(t *testing.T) {
	addr := dbustest.Start(t)
	t.Cleanup(startWatcher(t, addr))
	_, r := startHost(t, addr)

	f := newFakeItem(t, addr, "/StatusNotifierItem")
	if _, err := f.conn.RequestName("org.kde.StatusNotifierItem-1-1", 0); err != nil {
		t.Fatal(err)
	}
	f.register(t, "org.kde.StatusNotifierItem-1-1")

	items := r.wait(t, "the item", func(i []Item) bool { return len(i) == 1 })
	got := items[0]
	want := Item{
		Key:           "org.kde.StatusNotifierItem-1-1/StatusNotifierItem",
		ID:            "fake",
		Title:         "Fake App",
		Status:        "Active",
		IconName:      "fake-icon",
		IconThemePath: "/opt/fake/icons",
		Pixmaps:       []Pixmap{{Width: 1, Height: 1, ARGB: []byte{0xff, 0x10, 0x20, 0x30}}},
		Tooltip:       "Fake tip",
		HasMenu:       true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("item\n got %+v\nwant %+v", got, want)
	}

	// Property changes arrive with a New* signal.
	f.props.SetMust(itemIface, "Title", "Renamed")
	if err := f.conn.Emit(f.path, itemIface+".NewTitle"); err != nil {
		t.Fatal(err)
	}
	r.wait(t, "the new title", func(i []Item) bool { return len(i) == 1 && i[0].Title == "Renamed" })

	// The item goes away with its connection.
	f.conn.Close()
	r.wait(t, "removal", func(i []Item) bool { return len(i) == 0 })
}

func TestItemRegisteredByPath(t *testing.T) {
	addr := dbustest.Start(t)
	t.Cleanup(startWatcher(t, addr))
	_, r := startHost(t, addr)

	// libappindicator registers an object path; the service is the sender.
	f := newFakeItem(t, addr, "/org/ayatana/NotificationItem/fake")
	f.register(t, "/org/ayatana/NotificationItem/fake")
	items := r.wait(t, "the item", func(i []Item) bool { return len(i) == 1 })
	if want := f.conn.Names()[0] + "/org/ayatana/NotificationItem/fake"; items[0].Key != want {
		t.Fatalf("key %q, want %q", items[0].Key, want)
	}
	if items[0].Title != "Fake App" {
		t.Fatalf("title %q", items[0].Title)
	}
}

func TestHostSurvivesWatcherRestart(t *testing.T) {
	addr := dbustest.Start(t)
	stop := startWatcher(t, addr)
	_, r := startHost(t, addr)
	f := newFakeItem(t, addr, "/StatusNotifierItem")
	f.register(t, f.conn.Names()[0])
	r.wait(t, "the item", func(i []Item) bool { return len(i) == 1 })

	stop()
	r.wait(t, "the list to clear", func(i []Item) bool { return len(i) == 0 })

	// Items register again when a watcher appears, as real ones do.
	t.Cleanup(startWatcher(t, addr))
	f.register(t, f.conn.Names()[0])
	r.wait(t, "the item again", func(i []Item) bool { return len(i) == 1 })
}

func TestSecondWatcherWaitsForTheName(t *testing.T) {
	addr := dbustest.Start(t)
	stop := startWatcher(t, addr)

	conn := dbustest.Conn(t, addr)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go ServeWatcher(ctx, conn)
	time.Sleep(50 * time.Millisecond)

	stop()
	// The queued watcher takes over.
	for range 200 {
		var owner string
		conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, WatcherName).Store(&owner)
		if owner == conn.Names()[0] {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the second watcher did not take over the name")
}

func TestActivateAndMenu(t *testing.T) {
	addr := dbustest.Start(t)
	t.Cleanup(startWatcher(t, addr))
	h, r := startHost(t, addr)
	f := newFakeItem(t, addr, "/StatusNotifierItem")
	f.register(t, f.conn.Names()[0])
	key := r.wait(t, "the item", func(i []Item) bool { return len(i) == 1 })[0].Key

	if err := h.Activate(key); err != nil {
		t.Fatal(err)
	}
	if err := h.SecondaryActivate(key); err != nil {
		t.Fatal(err)
	}
	if err := h.Scroll(key, 1, "vertical"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	calls := f.calls
	f.mu.Unlock()
	if want := []string{"Activate", "SecondaryActivate", "Scroll vertical"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls %v, want %v", calls, want)
	}

	menu, err := h.Menu(key)
	if err != nil {
		t.Fatal(err)
	}
	want := []MenuEntry{
		{ID: 1, Label: "Open Window", Enabled: true},
		{ID: 2, Separator: true, Enabled: true},
		{ID: 3, Label: "Start at login", Enabled: true, Toggle: "checkmark", Checked: true},
		{ID: 5, Label: "More", Enabled: true, Children: []MenuEntry{
			{ID: 6, Label: "Disabled"},
		}},
		{ID: 7, Label: "Save _as", Enabled: true},
	}
	if !reflect.DeepEqual(menu, want) {
		t.Fatalf("menu\n got %+v\nwant %+v", menu, want)
	}

	if err := h.MenuClicked(key, 3); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	events := f.events
	f.mu.Unlock()
	if !reflect.DeepEqual(events, []int32{3}) {
		t.Fatalf("menu events %v", events)
	}
}

func TestParseKey(t *testing.T) {
	for key, want := range map[string][2]string{
		":1.5/StatusNotifierItem":             {":1.5", "/StatusNotifierItem"},
		":1.262@/StatusNotifierItem":          {":1.262", "/StatusNotifierItem"}, // GNOME AppIndicator
		"org.kde.StatusNotifierItem-9-1":      {"org.kde.StatusNotifierItem-9-1", "/StatusNotifierItem"},
		":1.7/org/ayatana/NotificationItem/x": {":1.7", "/org/ayatana/NotificationItem/x"},
	} {
		service, path, ok := parseKey(key)
		if !ok || service != want[0] || string(path) != want[1] {
			t.Errorf("parseKey(%q) = %q, %q, %v; want %q, %q", key, service, path, ok, want[0], want[1])
		}
	}
	for _, bad := range []string{"", "/StatusNotifierItem", "@/x"} {
		if _, _, ok := parseKey(bad); ok {
			t.Errorf("parseKey(%q) accepted", bad)
		}
	}
}

func TestStripMnemonic(t *testing.T) {
	for in, want := range map[string]string{
		"_Open":     "Open",
		"Save __as": "Save _as",
		"plain":     "plain",
		"a_b_c":     "abc",
		"trailing_": "trailing",
		"__init__":  "_init_",
	} {
		if got := stripMnemonic(in); got != want {
			t.Errorf("stripMnemonic(%q) = %q, want %q", in, got, want)
		}
	}
}
