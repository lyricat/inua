// Package appmenu implements the Canonical AppMenu Registrar and its dbusmenu client.
package appmenu

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"

	"github.com/lyricat/inua/internal/dbusexport"
)

const (
	RegistrarName      = "com.canonical.AppMenu.Registrar"
	RegistrarPath      = dbus.ObjectPath("/com/canonical/AppMenu/Registrar")
	RegistrarInterface = "com.canonical.AppMenu.Registrar"
	menuInterface      = "com.canonical.dbusmenu"
	busInterface       = "org.freedesktop.DBus"
	busPath            = dbus.ObjectPath("/org/freedesktop/DBus")
	ownerChanged       = busInterface + ".NameOwnerChanged"
	layoutUpdated      = menuInterface + ".LayoutUpdated"
	propertiesUpdated  = menuInterface + ".ItemsPropertiesUpdated"
	callTimeout        = 2 * time.Second
	maxMenuNodes       = 4096
	maxMenuDepth       = 32
)

// Registration is the menu endpoint registered by one application process.
type Registration struct {
	PID     uint32
	Window  uint32
	Service string
	Path    dbus.ObjectPath
}

const registrarXML = `<node>
 <interface name="com.canonical.AppMenu.Registrar">
  <method name="RegisterWindow"><arg type="u" direction="in"/><arg type="o" direction="in"/></method>
  <method name="UnregisterWindow"><arg type="u" direction="in"/></method>
  <method name="GetMenuForWindow"><arg type="u" direction="in"/><arg type="s" direction="out"/><arg type="o" direction="out"/></method>
  <signal name="WindowRegistered"><arg type="u"/><arg type="s"/><arg type="o"/></signal>
  <signal name="WindowUnregistered"><arg type="u"/></signal>
 </interface>` + introspect.IntrospectDeclarationString + `</node>`

func (r *Registrar) GetMenuForWindow(window uint32) (string, dbus.ObjectPath, *dbus.Error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var found Registration
	for _, v := range r.items {
		if v.Window == window {
			if found.Service != "" {
				return "", "/", dbus.NewError("org.freedesktop.DBus.Error.Failed", []any{"ambiguous window id"})
			}
			found = v.Registration
		}
	}
	if found.Service == "" {
		return "", "/", nil
	}
	return found.Service, found.Path, nil
}

// Entry is one dbusmenu item.
type Entry struct {
	ID        int32
	Label     string
	Enabled   bool
	Separator bool
	Toggle    string
	Checked   bool
	// Submenu: the item opens children. An application may fill them in
	// only when asked (AboutToShow), so Children can be empty until then.
	Submenu  bool
	Children []Entry
	// Action says how to invoke the entry: through the application's
	// accessibility tree, or its dbusmenu.
	Action Action
	// PID is the process whose accessibility tree the entry came from,
	// kept so a section can be found again on that tree after an open and
	// close has replaced its nodes.
	PID int
	// Open is the menus that must be open for Action to work, outermost
	// first: a section an accessibility application builds only while it
	// is open answers with items that act while it opens, not after it
	// closes. Opening them again and pressing the item by label is how such
	// an item is invoked.
	Open []OpenStep
}

// OpenStep is one menu to open before an entry can be pressed: the label
// that finds it in the level above, and the action that opens it (the
// first step's action is used as read; later ones are found by label).
type OpenStep struct {
	Label  string
	Action Action
}

// Action says how to invoke an entry.
type Action struct {
	Kind  string          // ActionA11y or ActionDbusmenu; empty when the entry only groups children
	Bus   string          // the connection that owns the menu
	Path  dbus.ObjectPath // the menu or accessible object
	Index int32           // an action index (AT-SPI) or an item id (dbusmenu)
}

const (
	ActionA11y     = "atspi"
	ActionDbusmenu = "dbusmenu"
)

// registered is a registration, when it was made among the others, and the
// items of its menu's first level as Menu last read them.
type registered struct {
	Registration
	seq uint64
	top map[int32]bool
}

type Registrar struct {
	conn     *dbus.Conn
	mu       sync.RWMutex
	items    map[string]registered
	seq      uint64
	onChange func()
	onUpdate func(Registration)
}

func (r *Registrar) SetOnChange(fn func()) { r.mu.Lock(); r.onChange = fn; r.mu.Unlock() }

// SetOnUpdate sets what is told when a registered application says the first
// level of its menu changed.
func (r *Registrar) SetOnUpdate(fn func(Registration)) { r.mu.Lock(); r.onUpdate = fn; r.mu.Unlock() }
func (r *Registrar) changed() {
	r.mu.RLock()
	fn := r.onChange
	r.mu.RUnlock()
	if fn != nil {
		fn()
	}
}

func New(conn *dbus.Conn) *Registrar {
	return &Registrar{conn: conn, items: make(map[string]registered)}
}

// Run owns the registrar name when available and serves the registry.
func (r *Registrar) Run(ctx context.Context) error {
	if err := dbusexport.Export(r.conn, r, RegistrarPath, RegistrarInterface); err != nil {
		return err
	}
	defer dbusexport.Export(r.conn, nil, RegistrarPath, RegistrarInterface)
	if err := dbusexport.Export(r.conn, introspect.Introspectable(registrarXML), RegistrarPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		return err
	}
	defer dbusexport.Export(r.conn, nil, RegistrarPath, "org.freedesktop.DBus.Introspectable")
	for _, match := range [][]dbus.MatchOption{
		{dbus.WithMatchSender(busInterface), dbus.WithMatchMember("NameOwnerChanged")},
		{dbus.WithMatchInterface(menuInterface), dbus.WithMatchMember("LayoutUpdated")},
		{dbus.WithMatchInterface(menuInterface), dbus.WithMatchMember("ItemsPropertiesUpdated")},
	} {
		if err := r.conn.AddMatchSignal(match...); err != nil {
			return err
		}
		defer r.conn.RemoveMatchSignal(match...)
	}
	sigs := make(chan *dbus.Signal, 32)
	r.conn.Signal(sigs)
	defer r.conn.RemoveSignal(sigs)
	if _, err := r.conn.RequestName(RegistrarName, 0); err != nil {
		return err
	}
	defer r.conn.ReleaseName(RegistrarName)
	for {
		select {
		case <-ctx.Done():
			return nil
		case sig, ok := <-sigs:
			if !ok {
				return nil
			}
			r.handle(sig)
		}
	}
}

// handle takes a signal for what its sender shows it to be: a name's owner
// has gone only when the bus says so, and a menu has changed only when the
// connection that registered it says so. Any peer can send a signal of any
// name straight to this connection.
func (r *Registrar) handle(sig *dbus.Signal) {
	switch sig.Name {
	case ownerChanged:
		// By type, not dbus.Store, which turns a number into a string.
		if sig.Sender != busInterface || sig.Path != busPath || len(sig.Body) != 3 {
			return
		}
		name, a := sig.Body[0].(string)
		_, b := sig.Body[1].(string)
		next, c := sig.Body[2].(string)
		if a && b && c && next == "" {
			r.removeOwner(name)
		}
	case layoutUpdated:
		if len(sig.Body) != 2 {
			return
		}
		_, a := sig.Body[0].(uint32)
		parent, b := sig.Body[1].(int32)
		if !a || !b {
			return
		}
		r.updated(sig, func(top map[int32]bool) bool { return parent == 0 || top[parent] })
	case propertiesUpdated:
		var changed []struct {
			ID    int32
			Props map[string]dbus.Variant
		}
		var removed []struct {
			ID    int32
			Props []string
		}
		if dbus.Store(sig.Body, &changed, &removed) != nil {
			return
		}
		r.updated(sig, func(top map[int32]bool) bool {
			for _, item := range changed {
				if top[item.ID] {
					return true
				}
			}
			for _, item := range removed {
				if top[item.ID] {
					return true
				}
			}
			return false
		})
	}
}

// updated tells of a menu signal that comes from a registered menu and, by
// touches, is about its first level.
func (r *Registrar) updated(sig *dbus.Signal, touches func(top map[int32]bool) bool) {
	var regs []Registration
	r.mu.RLock()
	fn := r.onUpdate
	for _, v := range r.items {
		if v.Service == sig.Sender && v.Path == sig.Path && touches(v.top) {
			regs = append(regs, v.Registration)
		}
	}
	r.mu.RUnlock()
	if fn == nil {
		return
	}
	for _, reg := range regs {
		fn(reg)
	}
}

// RegisterWindow is called by the application. The window ID is retained as protocol identity.
func (r *Registrar) RegisterWindow(sender dbus.Sender, window uint32, path dbus.ObjectPath) *dbus.Error {
	if window == 0 || !path.IsValid() || path == "/" {
		return dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs", []any{"invalid window or menu path"})
	}
	service := string(sender)
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	var pid uint32
	if err := r.conn.BusObject().CallWithContext(ctx, busInterface+".GetConnectionUnixProcessID", 0, service).Store(&pid); err != nil {
		return dbus.NewError("org.freedesktop.DBus.Error.Failed", []any{err.Error()})
	}
	key := registrationKey(service, window)
	r.mu.Lock()
	r.seq++
	r.items[key] = registered{Registration: Registration{PID: pid, Window: window, Service: service, Path: path}, seq: r.seq}
	r.mu.Unlock()
	r.conn.Emit(RegistrarPath, RegistrarInterface+".WindowRegistered", window, service, path)
	r.changed()
	return nil
}

// UnregisterWindow removes one registration owned by the caller.
func (r *Registrar) UnregisterWindow(sender dbus.Sender, window uint32) *dbus.Error {
	key := registrationKey(string(sender), window)
	r.mu.Lock()
	_, existed := r.items[key]
	delete(r.items, key)
	r.mu.Unlock()
	if existed {
		r.conn.Emit(RegistrarPath, RegistrarInterface+".WindowUnregistered", window)
		r.changed()
	}
	return nil
}

func registrationKey(service string, window uint32) string {
	return fmt.Sprintf("%s/%d", service, window)
}
func (r *Registrar) removeOwner(owner string) {
	r.mu.Lock()
	removed := false
	for k, v := range r.items {
		if v.Service == owner {
			delete(r.items, k)
			removed = true
		}
	}
	r.mu.Unlock()
	if removed {
		r.changed()
	}
}

// ForPID returns the menu endpoint of a focused process when it is
// unambiguous. A process may register the same menu endpoint for several
// windows, but a PID alone cannot choose between different endpoints.
func (r *Registrar) ForPID(pid int) (Registration, bool) {
	if pid <= 0 {
		return Registration{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var found registered
	for _, v := range r.items {
		if int(v.PID) != pid {
			continue
		}
		if found.Service != "" && (found.Service != v.Service || found.Path != v.Path) {
			return Registration{}, false
		}
		if v.seq > found.seq {
			found = v
		}
	}
	return found.Registration, found.Service != ""
}

// Menu reads the first level of a registration's menu, what a bar shows.
// With about set, the application is first told the menu is about to show,
// so one that builds it lazily fills it in; a reading that follows the
// application's own word of a change leaves that out, as the telling can
// itself be answered with such a word.
func (r *Registrar) Menu(reg Registration, about bool) ([]Entry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if about {
		r.conn.Object(reg.Service, reg.Path).CallWithContext(ctx, menuInterface+".AboutToShow", 0, int32(0))
	}
	// Two levels: the second only shows which items of the first open one.
	count := 0
	top := make(map[int32]bool)
	node, err := r.layout(ctx, reg, 0, 2, 0, &count, top)
	if err != nil {
		return nil, err
	}
	for i := range node.Children {
		node.Children[i].Children = nil
	}
	key := registrationKey(reg.Service, reg.Window)
	r.mu.Lock()
	if v, ok := r.items[key]; ok && v.Registration == reg {
		v.top = top
		r.items[key] = v
	}
	r.mu.Unlock()
	return node.Children, nil
}

// Submenu reads what item id opens, as it is now: the application and each
// nested submenu are told that they are about to show before their contents
// are read. One that stays empty, or that no longer fits the bounds, is left
// empty.
func (r *Registrar) Submenu(reg Registration, id int32) ([]Entry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	count := 0
	node, err := r.open(ctx, reg, id, 0, &count)
	if err != nil {
		return nil, err
	}
	return node.Children, nil
}

func (r *Registrar) open(ctx context.Context, reg Registration, id int32, depth int, count *int) (Entry, error) {
	r.conn.Object(reg.Service, reg.Path).CallWithContext(ctx, menuInterface+".AboutToShow", 0, id)
	node, err := r.layout(ctx, reg, id, -1, depth, count, nil)
	if err != nil {
		return Entry{}, err
	}
	r.fill(ctx, reg, node.Children, depth+1, count)
	return node, nil
}

func (r *Registrar) fill(ctx context.Context, reg Registration, entries []Entry, depth int, count *int) {
	for i := range entries {
		e := &entries[i]
		if !e.Submenu {
			continue
		}
		before := *count
		*count -= entryCount(*e)
		if node, err := r.open(ctx, reg, e.ID, depth, count); err == nil {
			e.Children = node.Children
		} else {
			*count = before
		}
	}
}

func entryCount(entry Entry) int {
	count := 1
	for _, child := range entry.Children {
		count += entryCount(child)
	}
	return count
}

// layout reads the tree under item id, levels deep (-1: all of it), within
// the bounds count and depth have left.
func (r *Registrar) layout(ctx context.Context, reg Registration, id, levels int32, depth int, count *int, topIDs map[int32]bool) (Entry, error) {
	call := r.conn.Object(reg.Service, reg.Path).CallWithContext(ctx, menuInterface+".GetLayout", 0, id, levels, []string{})
	if call.Err != nil {
		return Entry{}, call.Err
	}
	if len(call.Body) != 2 {
		return Entry{}, fmt.Errorf("appmenu: GetLayout returned %d values", len(call.Body))
	}
	root, ok := call.Body[1].([]any)
	if !ok {
		return Entry{}, fmt.Errorf("appmenu: malformed layout type %T", call.Body[1])
	}
	before := *count
	node, ok := decode(root, depth, count)
	if !ok {
		*count = before
		return Entry{}, fmt.Errorf("appmenu: malformed or oversized layout")
	}
	if topIDs != nil {
		children, ok := root[2].([]dbus.Variant)
		if !ok {
			*count = before
			return Entry{}, fmt.Errorf("appmenu: malformed top-level children")
		}
		for _, child := range children {
			childNode, ok := child.Value().([]any)
			if !ok || len(childNode) != 3 {
				*count = before
				return Entry{}, fmt.Errorf("appmenu: malformed top-level item")
			}
			childID, ok := childNode[0].(int32)
			if !ok {
				*count = before
				return Entry{}, fmt.Errorf("appmenu: malformed top-level item id")
			}
			topIDs[childID] = true
		}
	}
	return node, nil
}

// Activate sends the dbusmenu click event to the registered application.
func (r *Registrar) Activate(reg Registration, id int32) error {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return r.conn.Object(reg.Service, reg.Path).CallWithContext(ctx, menuInterface+".Event", 0, id, "clicked", dbus.MakeVariant(int32(0)), uint32(time.Now().Unix())).Err
}

func decode(raw []any, depth int, count *int) (Entry, bool) {
	*count++
	if depth > maxMenuDepth || *count > maxMenuNodes || len(raw) != 3 {
		return Entry{}, false
	}
	id, a := raw[0].(int32)
	props, b := raw[1].(map[string]dbus.Variant)
	children, c := raw[2].([]dbus.Variant)
	if !a || !b || !c {
		return Entry{}, false
	}
	e := Entry{ID: id, Enabled: true}
	if v, ok := props["label"].Value().(string); ok {
		e.Label = stripMnemonic(v)
	}
	if v, ok := props["enabled"].Value().(bool); ok {
		e.Enabled = v
	}
	if v, ok := props["type"].Value().(string); ok {
		e.Separator = v == "separator"
	}
	if v, ok := props["toggle-type"].Value().(string); ok {
		e.Toggle = v
	}
	if v, ok := props["toggle-state"].Value().(int32); ok {
		e.Checked = v == 1
	}
	if v, ok := props["children-display"].Value().(string); ok {
		e.Submenu = v == "submenu"
	}
	for _, child := range children {
		rawChild, ok := child.Value().([]any)
		if !ok {
			return Entry{}, false
		}
		visible := true
		if len(rawChild) == 3 {
			if p, ok := rawChild[1].(map[string]dbus.Variant); ok {
				if v, ok := p["visible"].Value().(bool); ok {
					visible = v
				}
			}
		}
		if !visible {
			continue
		}
		decoded, ok := decode(rawChild, depth+1, count)
		if !ok {
			return Entry{}, false
		}
		e.Children = append(e.Children, decoded)
	}
	e.Submenu = e.Submenu || len(e.Children) > 0
	return e, true
}

func stripMnemonic(s string) string {
	out := make([]rune, 0, len(s))
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '_' {
			if i+1 < len(runes) && runes[i+1] == '_' {
				out = append(out, '_')
				i++
			}
			continue
		}
		out = append(out, runes[i])
	}
	return string(out)
}
