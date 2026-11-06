package appmenu

import (
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/dbustest"
)

// tree is an a11ySource built from a map: each node's role, name, and
// children, so a menu can be walked without a bus.
type tree struct {
	roles    map[string]uint32 // by path
	names    map[string]string
	kids     map[string][]string
	statesOf map[string][]uint32
	acts     map[string][]string
	acted    []string // paths acted on, in order
}

func newTree() *tree {
	return &tree{
		roles:    map[string]uint32{},
		names:    map[string]string{},
		kids:     map[string][]string{},
		statesOf: map[string][]uint32{},
		acts:     map[string][]string{},
	}
}

func (m *tree) node(path string, role uint32, name string) *tree {
	m.roles[path] = role
	m.names[path] = name
	return m
}

func (m *tree) child(parent, child string) *tree {
	m.kids[parent] = append(m.kids[parent], child)
	return m
}

func (m *tree) applications() ([]a11yRef, error) {
	return []a11yRef{{Name: ":1.1", Path: "/root"}}, nil
}

func (m *tree) processID(string) (uint32, error) { return 42, nil }

func (m *tree) children(ref a11yRef) ([]a11yRef, error) {
	var out []a11yRef
	for _, p := range m.kids[string(ref.Path)] {
		out = append(out, a11yRef{Name: ref.Name, Path: dbus.ObjectPath(p)})
	}
	return out, nil
}

func (m *tree) name(ref a11yRef) (string, error) { return m.names[string(ref.Path)], nil }
func (m *tree) role(ref a11yRef) (uint32, error) { return m.roles[string(ref.Path)], nil }

func (m *tree) states(ref a11yRef) ([]uint32, error) { return m.statesOf[string(ref.Path)], nil }

func (m *tree) actions(ref a11yRef) ([]string, error) { return m.acts[string(ref.Path)], nil }

func (m *tree) doAction(ref a11yRef, index int32) error {
	m.acted = append(m.acted, string(ref.Path))
	return nil
}

// A browser puts its sections at the menubar, labelled, with the items
// under them filled in only when a section opens: the sections must come
// out as entries that open a menu, not as unnamed items spliced in.
func TestMenuForLabelledSections(t *testing.T) {
	m := newTree()
	m.node("/root", 75, "Firefox").child("/root", "/bar")
	m.node("/bar", roleMenuBar, "Application").
		child("/bar", "/file").
		child("/bar", "/edit")
	m.node("/file", roleMenu, "File").child("/file", "/newtab").child("/file", "/sep")
	m.node("/newtab", roleMenuItem, "New Tab")
	m.node("/sep", roleSeparator, "")
	m.node("/edit", roleMenu, "Edit").child("/edit", "/undo")
	m.node("/undo", roleMenuItem, "Undo")
	m.acts["/file"] = []string{"<Alt>F;"}
	m.acts["/newtab"] = []string{"Press"}
	m.acts["/undo"] = []string{"Press"}

	got, err := menuFor(m, 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("sections = %d, want File and Edit: %+v", len(got), got)
	}
	if got[0].Label != "File" || got[1].Label != "Edit" {
		t.Fatalf("labels = %q, %q", got[0].Label, got[1].Label)
	}
	if len(got[0].Children) != 1 || got[0].Children[0].Label != "New Tab" {
		t.Fatalf("File's items = %+v", got[0].Children)
	}
	// A section opens its menu where Firefox answers only a label's keys.
	if got[0].Action.Kind != ActionA11y || got[0].Action.Index != 0 {
		t.Fatalf("File's action = %+v", got[0].Action)
	}
	if got[0].Action.Index != 0 || got[0].Children[0].Action.Index != 0 {
		t.Fatalf("action indexes = %+v, %+v", got[0].Action, got[0].Children[0].Action)
	}
}

// A plain GTK4 application answers the accessibility bus with a skeleton:
// a menubar whose items have no names. That is not a menu, and the bar
// must show none rather than blank buttons.
func TestMenuForSkeletonIsEmpty(t *testing.T) {
	m := newTree()
	m.node("/root", 75, "App").child("/root", "/bar")
	m.node("/bar", roleMenuBar, "Application").child("/bar", "/a")
	m.node("/a", roleMenuItem, "")
	if got, err := menuFor(m, 42); err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v; want none", got, err)
	}
}

// An unnamed popup-menu node under an item is the item's own menu (Qt):
// its items are spliced in as that item's children.
func TestMenuForUnnamedPopup(t *testing.T) {
	m := newTree()
	m.node("/root", 75, "Kate").child("/root", "/bar")
	m.node("/bar", roleMenuBar, "").child("/bar", "/file")
	m.node("/file", roleMenuItem, "File").child("/file", "/popup")
	m.node("/popup", rolePopupMenu, "").child("/popup", "/open")
	m.node("/open", roleMenuItem, "Open")
	m.acts["/file"] = []string{"Press"}

	got, err := menuFor(m, 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Label != "File" {
		t.Fatalf("got %+v", got)
	}
	if len(got[0].Children) != 1 || got[0].Children[0].Label != "Open" {
		t.Fatalf("File's items = %+v", got[0].Children)
	}
}

// DoAction takes the action's index as an int32; the accessibility bus
// refuses a uint32 ("supplied with invalid arguments"), which used to make
// every browser menu item dead.
func TestA11yDoActionSendsInt32(t *testing.T) {
	var got string
	conn := callRecorder(t, func(kind string) { got = kind })
	b := busSource{conn: conn}
	if err := b.doAction(a11yRef{Name: conn.Names()[0], Path: "/x"}, 2); err != nil {
		t.Fatal(err)
	}
	if got != "int32" {
		t.Fatalf("DoAction was not called with the bus's int32 index")
	}
}

// callRecorder is a bus with one object exporting DoAction, recording the
// Go type the caller sent it.
func callRecorder(t *testing.T, seen func(kind string)) *dbus.Conn {
	t.Helper()
	addr := dbustest.Start(t)
	conn := dbustest.Conn(t, addr)
	if err := conn.Export(&recorder{seen: seen}, "/x", a11yActionIface); err != nil {
		t.Fatal(err)
	}
	return conn
}

type recorder struct{ seen func(kind string) }

// DoAction mirrors the accessibility bus, whose index is an int32. A
// uint32 does not marshal into it at all: the call fails on the client.
func (r *recorder) DoAction(index int32) (bool, *dbus.Error) {
	r.seen("int32")
	return true, nil
}
