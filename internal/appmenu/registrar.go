// Package appmenu implements the Canonical AppMenu Registrar and its dbusmenu client.
package appmenu

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

const (
	RegistrarName      = "com.canonical.AppMenu.Registrar"
	RegistrarPath      = dbus.ObjectPath("/com/canonical/AppMenu/Registrar")
	RegistrarInterface = "com.canonical.AppMenu.Registrar"
	menuInterface      = "com.canonical.dbusmenu"
	busInterface       = "org.freedesktop.DBus"
	ownerChanged       = busInterface + ".NameOwnerChanged"
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
  <signal name="WindowRegistered"><arg type="u"/><arg type="o"/></signal>
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
			found = v
		}
	}
	if found.Service == "" {
		return "", "/", dbus.NewError("org.freedesktop.DBus.Error.UnknownObject", []any{"window is not registered"})
	}
	return found.Service, found.Path, nil
}

// Entry is one menu item, from either source.
type Entry struct {
	ID        int32
	Label     string
	Enabled   bool
	Separator bool
	Toggle    string
	Checked   bool
	Children  []Entry
	Action    Action
}

// Action says how to invoke an entry.
type Action struct {
	Kind          string          // ActionA11y or ActionDbusmenu; empty when the entry only groups children
	Bus           string          // the connection that owns the menu
	Path          dbus.ObjectPath // the menu or accessible object
	Index         int32           // an action index (AT-SPI) or an item id (dbusmenu)
	NativeWayland bool            // this app has no X11 Registrar window ID
	Direct        bool            // the shell read this menu itself, not through the Registrar
}

const (
	ActionA11y     = "atspi"
	ActionDbusmenu = "dbusmenu"
	// Actions the shell itself runs, for the entries every application has
	// (its name's menu): Index carries the process they act on.
	ActionAbout       = "about"
	ActionCloseWindow = "close-window"
	ActionQuitApp     = "quit-app"
)

type Registrar struct {
	conn     *dbus.Conn
	mu       sync.RWMutex
	items    map[string]Registration
	onChange func()
}

func (r *Registrar) SetOnChange(fn func()) { r.mu.Lock(); r.onChange = fn; r.mu.Unlock() }
func (r *Registrar) changed() {
	r.mu.RLock()
	fn := r.onChange
	r.mu.RUnlock()
	if fn != nil {
		fn()
	}
}

func New(conn *dbus.Conn) *Registrar {
	return &Registrar{conn: conn, items: make(map[string]Registration)}
}

// Run owns the registrar name when available and serves the registry.
func (r *Registrar) Run(ctx context.Context) error {
	if err := r.conn.Export(r, RegistrarPath, RegistrarInterface); err != nil {
		return err
	}
	defer r.conn.Export(nil, RegistrarPath, RegistrarInterface)
	if err := r.conn.Export(introspect.Introspectable(registrarXML), RegistrarPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		return err
	}
	defer r.conn.Export(nil, RegistrarPath, "org.freedesktop.DBus.Introspectable")
	match := []dbus.MatchOption{dbus.WithMatchSender(busInterface), dbus.WithMatchMember("NameOwnerChanged")}
	if err := r.conn.AddMatchSignal(match...); err != nil {
		return err
	}
	defer r.conn.RemoveMatchSignal(match...)
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
			if sig.Name == ownerChanged && len(sig.Body) == 3 {
				name, _ := sig.Body[0].(string)
				next, _ := sig.Body[2].(string)
				if next == "" {
					r.removeOwner(name)
				}
			}
		}
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
	r.items[key] = Registration{PID: pid, Window: window, Service: service, Path: path}
	r.mu.Unlock()
	r.conn.Emit(RegistrarPath, RegistrarInterface+".WindowRegistered", window, path)
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

// ForPID returns the most recently registered endpoint for a focused process.
func (r *Registrar) ForPID(pid int) (Registration, bool) {
	if pid <= 0 {
		return Registration{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var found Registration
	for _, v := range r.items {
		if int(v.PID) == pid {
			if found.Service != "" {
				return Registration{}, false
			}
			found = v
		}
	}
	return found, found.Service != ""
}

// Menu fetches a bounded dbusmenu tree from a registration.
func (r *Registrar) Menu(reg Registration) ([]Entry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	obj := r.conn.Object(reg.Service, reg.Path)
	obj.CallWithContext(ctx, menuInterface+".AboutToShow", 0, int32(0))
	call := obj.CallWithContext(ctx, menuInterface+".GetLayout", 0, int32(0), int32(-1), []string{})
	if call.Err != nil {
		return nil, call.Err
	}
	if len(call.Body) != 2 {
		return nil, fmt.Errorf("appmenu: GetLayout returned %d values", len(call.Body))
	}
	root, ok := call.Body[1].([]any)
	if !ok {
		return nil, fmt.Errorf("appmenu: malformed layout type %T", call.Body[1])
	}
	count := 0
	node, ok := decode(root, 0, &count)
	if !ok {
		return nil, fmt.Errorf("appmenu: malformed or oversized layout")
	}
	setDbusmenuAction(node.Children, reg)
	return node.Children, nil
}

// setDbusmenuAction gives every entry the endpoint that activates it.
func setDbusmenuAction(entries []Entry, reg Registration) {
	for i := range entries {
		if len(entries[i].Children) > 0 {
			setDbusmenuAction(entries[i].Children, reg)
			continue
		}
		entries[i].Action = Action{Kind: ActionDbusmenu, Bus: reg.Service, Path: reg.Path, Index: entries[i].ID}
	}
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
