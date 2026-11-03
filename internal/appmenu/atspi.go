package appmenu

// Reading the focused application's menubar through AT-SPI.
//
// Applications export their menu structure on the accessibility bus
// (org.a11y.Bus), which every Qt and GTK application populates when
// accessibility is on. Walking that tree gives the same menus the window
// itself shows, for toolkits that never register a Canonical app menu.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	a11yBusService   = "org.a11y.Bus"
	a11yBusPath      = dbus.ObjectPath("/org/a11y/bus")
	a11yRegistryName = "org.a11y.atspi.Registry"
	a11yRegistryRoot = dbus.ObjectPath("/org/a11y/atspi/accessible/root")
	a11yAccessible   = "org.a11y.atspi.Accessible"
	a11yActionIface  = "org.a11y.atspi.Action"

	roleMenuBar         = 34
	roleMenu            = 33
	rolePopupMenu       = 41
	roleMenuItem        = 35
	roleCheckMenuItem   = 8
	roleRadioMenuItem   = 45
	roleSeparator       = 50
	roleLabel           = 29
	roleTearoffMenuItem = 58

	stateEnabled = 8

	a11yTimeout = 2 * time.Second
)

// a11yRef is one accessible: the connection it lives on and its object path.
type a11yRef struct {
	Name string
	Path dbus.ObjectPath
}

func (r a11yRef) valid() bool { return r.Name != "" && r.Path != "" }

// a11ySource is what walking a menu needs from the accessibility bus, so the
// walk can be tested without a bus.
type a11ySource interface {
	applications() ([]a11yRef, error)
	processID(name string) (uint32, error)
	children(ref a11yRef) ([]a11yRef, error)
	name(ref a11yRef) (string, error)
	role(ref a11yRef) (uint32, error)
	states(ref a11yRef) ([]uint32, error)
	actions(ref a11yRef) ([]string, error)
	doAction(ref a11yRef, index int32) error
}

// A11y reads menus over the accessibility bus.
type A11y struct {
	mu   sync.Mutex
	conn *dbus.Conn
}

// NewA11y returns a source that connects to the accessibility bus on first use.
func NewA11y() *A11y { return &A11y{} }

// Close drops the connection to the accessibility bus.
func (a *A11y) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn != nil {
		a.conn.Close()
		a.conn = nil
	}
}

func (a *A11y) source() (a11ySource, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn != nil {
		return busSource{a.conn}, nil
	}
	session, err := dbus.SessionBus()
	if err != nil {
		return nil, err
	}
	var address string
	call := session.Object(a11yBusService, a11yBusPath).Call(a11yBusService+".GetAddress", 0)
	if call.Err != nil {
		return nil, fmt.Errorf("appmenu: accessibility bus: %w", call.Err)
	}
	if err := call.Store(&address); err != nil {
		return nil, err
	}
	conn, err := dbus.Dial(address)
	if err != nil {
		return nil, err
	}
	if err := conn.Auth(nil); err != nil {
		conn.Close()
		return nil, err
	}
	if err := conn.Hello(); err != nil {
		conn.Close()
		return nil, err
	}
	a.conn = conn
	return busSource{conn}, nil
}

// Menu returns the focused process's menubar entries, top level first.
func (a *A11y) Menu(pid int) ([]Entry, error) {
	if pid <= 0 {
		return nil, nil
	}
	src, err := a.source()
	if err != nil {
		return nil, err
	}
	return menuFor(src, pid)
}

// Activate invokes the action an entry carries.
func (a *A11y) Activate(entry Entry) error {
	if entry.Action.Kind != ActionA11y || entry.Action.Bus == "" || entry.Action.Path == "" {
		return fmt.Errorf("appmenu: entry %q has no accessibility action", entry.Label)
	}
	src, err := a.source()
	if err != nil {
		return err
	}
	ref := a11yRef{Name: entry.Action.Bus, Path: entry.Action.Path}
	return src.doAction(ref, entry.Action.Index)
}

// menuFor walks the accessibility tree of one process to its menubar.
func menuFor(src a11ySource, pid int) ([]Entry, error) {
	apps, err := src.applications()
	if err != nil {
		return nil, err
	}
	var candidates []a11yRef
	for _, app := range apps {
		got, err := src.processID(app.Name)
		if err != nil || int(got) != pid {
			continue
		}
		candidates = append(candidates, app)
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Name < candidates[j].Name
	})
	for _, app := range candidates {
		bar, ok := findRole(src, app, roleMenuBar, 6, map[string]bool{})
		if !ok {
			continue
		}
		entries, err := decodeA11y(src, bar, 0, 0)
		if err != nil {
			continue
		}
		if len(entries) > 0 {
			return entries, nil
		}
	}
	return nil, nil
}

func key(ref a11yRef) string { return ref.Name + string(ref.Path) }

func findRole(src a11ySource, ref a11yRef, want uint32, depth int, seen map[string]bool) (a11yRef, bool) {
	if depth < 0 || !ref.valid() || seen[key(ref)] {
		return a11yRef{}, false
	}
	seen[key(ref)] = true
	if role, err := src.role(ref); err == nil && role == want {
		return ref, true
	}
	kids, err := src.children(ref)
	if err != nil {
		return a11yRef{}, false
	}
	for _, kid := range kids {
		if found, ok := findRole(src, kid, want, depth-1, seen); ok {
			return found, true
		}
	}
	return a11yRef{}, false
}

func decodeA11y(src a11ySource, ref a11yRef, depth, count int) ([]Entry, error) {
	if depth > maxMenuDepth || count > maxMenuNodes {
		return nil, fmt.Errorf("appmenu: menu too large")
	}
	kids, err := src.children(ref)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(kids))
	for _, kid := range kids {
		role, err := src.role(kid)
		if err != nil {
			continue
		}
		label, _ := src.name(kid)
		if strings.TrimSpace(label) == "" {
			// GTK4 keeps a menu item's text on a label child, not on the item.
			label = labelChildName(src, kid)
		}
		switch role {
		case roleSeparator, roleTearoffMenuItem:
			out = append(out, Entry{Separator: true})
			continue
		case roleMenu, rolePopupMenu:
			// Qt and GTK hang an item's items from an unlabelled popup-menu
			// node: splice them in, so a submenu is the item's own children.
			// Firefox and Chromium put the section itself there, labelled
			// ("File", "Edit"): that one is an item, and its menu is its
			// children.
			if strings.TrimSpace(label) == "" {
				inner, err := decodeA11y(src, kid, depth+1, count+1)
				if err == nil {
					out = append(out, inner...)
				}
				continue
			}
			// A labelled one is a section of the bar ("File"): an item whose
			// items Firefox and Chromium fill in when the menu opens, so it
			// is read here, not spliced.
			section := Entry{
				Label:   strings.TrimSpace(stripMnemonic(label)),
				Enabled: true,
				Action:  a11yOpenAction(src, kid),
			}
			if inner, err := decodeA11y(src, kid, depth+1, count+1); err == nil {
				section.Children = inner
			}
			out = append(out, section)
			continue
		case roleMenuItem, roleCheckMenuItem, roleRadioMenuItem:
		default:
			continue
		}
		entry := Entry{
			Label:   strings.TrimSpace(stripMnemonic(label)),
			Enabled: enabledFor(src, kid),
			Checked: checkedFor(src, kid),
		}
		if role == roleCheckMenuItem || role == roleRadioMenuItem {
			entry.Toggle = "check"
		}
		names, _ := src.actions(kid)
		entry.Action = a11yActionFor(kid, names)
		children, err := decodeA11y(src, kid, depth+1, count+1)
		if err == nil && len(children) > 0 {
			entry.Children = children
			if entry.Toggle == "" {
				entry.Enabled = true
			}
		}
		if entry.Label == "" && len(entry.Children) == 0 && !entry.Separator {
			continue
		}
		out = append(out, entry)
	}
	return trimSeparators(out), nil
}

// a11yOpenAction is the action that opens a menu section: the one that
// says it opens a menu, else the first the item has.
func a11yOpenAction(src a11ySource, ref a11yRef) Action {
	names, _ := src.actions(ref)
	return a11yActionFor(ref, names)
}

// a11yActionFor picks the action that opens or presses an item.
func a11yActionFor(ref a11yRef, names []string) Action {
	for _, want := range []string{"Press", "click", "Click", "clicked"} {
		for i, name := range names {
			if name == want {
				return Action{Kind: ActionA11y, Bus: ref.Name, Path: ref.Path, Index: int32(i)}
			}
		}
	}
	if len(names) > 0 {
		return Action{Kind: ActionA11y, Bus: ref.Name, Path: ref.Path, Index: 0}
	}
	return Action{}
}

func enabledFor(src a11ySource, ref a11yRef) bool {
	bits, err := src.states(ref)
	if err != nil || len(bits) == 0 {
		return true
	}
	return hasState(bits, stateEnabled)
}

func checkedFor(src a11ySource, ref a11yRef) bool {
	bits, err := src.states(ref)
	if err != nil || len(bits) == 0 {
		return false
	}
	return hasState(bits, 4) // ATSPI_STATE_CHECKED
}

func hasState(bits []uint32, index int) bool {
	if index < 0 || len(bits) == 0 {
		return false
	}
	return bits[0]&(1<<uint(index)) != 0
}

func trimSeparators(in []Entry) []Entry {
	out := in[:0]
	for _, entry := range in {
		if entry.Separator && (len(out) == 0 || out[len(out)-1].Separator) {
			continue
		}
		out = append(out, entry)
	}
	for len(out) > 0 && out[len(out)-1].Separator {
		out = out[:len(out)-1]
	}
	return out
}

// busSource is the accessibility bus itself.
type busSource struct{ conn *dbus.Conn }

func (b busSource) call(ref a11yRef, method string, args ...any) *dbus.Call {
	ctx, cancel := context.WithTimeout(context.Background(), a11yTimeout)
	defer cancel()
	return b.conn.Object(ref.Name, ref.Path).CallWithContext(ctx, method, 0, args...)
}

func (b busSource) applications() ([]a11yRef, error) {
	root := a11yRef{Name: a11yRegistryName, Path: a11yRegistryRoot}
	return b.children(root)
}

func (b busSource) processID(name string) (uint32, error) {
	var pid uint32
	call := b.conn.BusObject().Call("org.freedesktop.DBus.GetConnectionUnixProcessID", 0, name)
	if call.Err != nil {
		return 0, call.Err
	}
	err := call.Store(&pid)
	return pid, err
}

func (b busSource) children(ref a11yRef) ([]a11yRef, error) {
	var kids []a11yRef
	call := b.call(ref, a11yAccessible+".GetChildren")
	if call.Err == nil {
		if err := call.Store(&kids); err == nil && len(kids) > 0 {
			return kids, nil
		}
	}
	// Qt answers a menu item with ChildCount and GetChildAtIndex but no
	// children list, so a submenu is read one index at a time.
	var n int32
	count := b.call(ref, a11yAccessible+".ChildCount")
	if count.Err != nil {
		return nil, count.Err
	}
	if err := count.Store(&n); err != nil {
		return nil, err
	}
	for i := int32(0); i < n; i++ {
		var one a11yRef
		child := b.call(ref, a11yAccessible+".GetChildAtIndex", i)
		if child.Err != nil {
			continue
		}
		if err := child.Store(&one); err != nil {
			continue
		}
		kids = append(kids, one)
	}
	return kids, nil
}

func (b busSource) name(ref a11yRef) (string, error) {
	var name string
	call := b.call(ref, a11yAccessible+".GetName")
	if call.Err == nil {
		if err := call.Store(&name); err == nil {
			return name, nil
		}
	}
	// Firefox and Chromium answer no GetName: their accessible's name is
	// the property alone.
	return b.property(ref, "Name")
}

func (b busSource) role(ref a11yRef) (uint32, error) {
	var role uint32
	call := b.call(ref, a11yAccessible+".GetRole")
	if call.Err == nil {
		if err := call.Store(&role); err == nil {
			return role, nil
		}
	}
	// The same for the role: a property, where there is no method.
	var v dbus.Variant
	if err := b.call(ref, "org.freedesktop.DBus.Properties.Get", a11yAccessible, "Role").Store(&v); err != nil {
		return 0, err
	}
	role, _ = v.Value().(uint32)
	return role, nil
}

// property is an accessible's property of name, read over Properties.
func (b busSource) property(ref a11yRef, name string) (string, error) {
	var v dbus.Variant
	call := b.call(ref, "org.freedesktop.DBus.Properties.Get", a11yAccessible, name)
	if call.Err != nil {
		return "", call.Err
	}
	if err := call.Store(&v); err != nil {
		return "", err
	}
	s, _ := v.Value().(string)
	return s, nil
}

func (b busSource) states(ref a11yRef) ([]uint32, error) {
	var bits []uint32
	call := b.call(ref, a11yAccessible+".GetState")
	if call.Err == nil {
		if err := call.Store(&bits); err == nil {
			return bits, nil
		}
	}
	// The StateSet property, for a toolkit with no GetState: a list of
	// state names there, which are not the bit field the caller reads.
	return nil, call.Err
}

func (b busSource) actions(ref a11yRef) ([]string, error) {
	var raw []struct {
		Name        string
		Description string
		KeyBinding  string
	}
	call := b.call(ref, a11yActionIface+".GetActions")
	if call.Err != nil {
		return nil, call.Err
	}
	if err := call.Store(&raw); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(raw))
	for _, action := range raw {
		names = append(names, action.Name)
	}
	return names, nil
}

func (b busSource) doAction(ref a11yRef, index int32) error {
	call := b.call(ref, a11yActionIface+".DoAction", index)
	if call.Err != nil {
		return call.Err
	}
	var done bool
	if err := call.Store(&done); err != nil {
		return err
	}
	if !done {
		return fmt.Errorf("appmenu: the application refused the action")
	}
	return nil
}

// labelChildName is a widget's text when it hangs on a label child, as GTK4
// menu items and buttons keep it: the first label child that has a name.
func labelChildName(src a11ySource, ref a11yRef) string {
	kids, err := src.children(ref)
	if err != nil {
		return ""
	}
	for _, kid := range kids {
		if r, err := src.role(kid); err != nil || r != roleLabel {
			continue
		}
		if n, err := src.name(kid); err == nil && strings.TrimSpace(n) != "" {
			return n
		}
	}
	return ""
}
