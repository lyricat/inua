package appmenu

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/dbusmenu"
)

// Client reads the focused application's menu: the Canonical registrar first,
// then inua's own published menu, then the accessibility tree.
type Client struct {
	Registrar *Registrar
	A11y      *A11y
}

func NewClient(r *Registrar, a *A11y) *Client { return &Client{Registrar: r, A11y: a} }

// Menu returns the focused process's menu, or nil when it has none.
func (c *Client) Menu(pid int) ([]Entry, error) {
	if pid <= 0 {
		return nil, nil
	}
	// The registrar is the de-facto protocol: an application that registered a
	// window means its menu is the one to show.
	if c.Registrar != nil {
		if r, ok := c.Registrar.ForPID(pid); ok {
			return c.Registrar.Menu(r)
		}
	}
	if e, ok, err := c.kdeMenu(pid); ok || err != nil {
		return e, err
	}
	if e, ok, err := c.nativeWaylandMenu(pid); ok || err != nil {
		return e, err
	}
	// AT-SPI last: plain GTK4 apps answer it with a skeleton (a menubar whose
	// items have no labels), which must not shadow a real menu nor put blank
	// buttons in the bar. A browser's sections come through with no items at
	// all - it builds them when its own menu opens - and the shell cannot
	// fill them without pressing them, which crashes Firefox. So a tree of
	// empty sections is no menu either.
	if c.A11y != nil {
		if e, err := c.A11y.Menu(pid); err == nil && labelled(e) {
			return e, nil
		}
	}
	return nil, nil
}

// labelled reports whether a menu is one the bar can show: sections that
// already have their items. A tree of unnamed items is a toolkit skeleton,
// and a browser's menubar - named sections with nothing under them - is a
// menu the shell cannot read: the application builds a section's items only
// when its own menu opens, and pressing it over AT-SPI to force that ends
// Firefox. Both stay out, so the bar never shows a section that opens
// nothing.
func labelled(entries []Entry) bool {
	for _, e := range entries {
		if strings.TrimSpace(e.Label) == "" || e.Separator {
			continue
		}
		if len(e.Children) > 0 {
			return true
		}
	}
	return false
}

// nativeWaylandMenu finds the menu an inua application published for its
// window, by the well-known name's owning process.
func (c *Client) nativeWaylandMenu(pid int) ([]Entry, bool, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, false, err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var names []string
	if err = conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		return nil, false, err
	}
	var found []string
	for _, name := range names {
		if !strings.HasPrefix(name, dbusmenu.NamePrefix) {
			continue
		}
		var owner uint32
		if conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetConnectionUnixProcessID", 0, name).Store(&owner) == nil && int(owner) == pid {
			found = append(found, name)
		}
	}
	// One window per process: with more, the shell has no way to tell which
	// window's menu belongs to the focused one, so it shows none rather than
	// the wrong one's commands.
	if len(found) != 1 {
		return nil, len(found) > 1, nil
	}
	r := Registration{PID: uint32(pid), Service: found[0], Path: dbusmenu.Path}
	entries, err := c.readMenu(conn, r)
	markNative(entries)
	return entries, true, err
}

func (c *Client) readMenu(conn *dbus.Conn, r Registration) ([]Entry, error) {
	obj := conn.Object(r.Service, r.Path)
	obj.Call("com.canonical.dbusmenu.AboutToShow", 0, int32(0))
	call := obj.Call("com.canonical.dbusmenu.GetLayout", 0, int32(0), int32(-1), []string{})
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
	n, ok := decode(root, 0, &count)
	if !ok {
		return nil, fmt.Errorf("appmenu: malformed or oversized layout")
	}
	setDbusmenuAction(n.Children, r)
	return n.Children, nil
}

// Open refreshes a D-Bus submenu after notifying its owner that it will be
// shown. Dynamic menus prepare their actions and working directory here.
func (c *Client) Open(e Entry) ([]Entry, error) {
	if e.Action.Kind != ActionDbusmenu {
		return e.Children, nil
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	obj := conn.Object(e.Action.Bus, e.Action.Path)
	if err := obj.CallWithContext(ctx, menuInterface+".AboutToShow", 0, e.ID).Err; err != nil {
		return nil, err
	}
	call := obj.CallWithContext(ctx, menuInterface+".GetLayout", 0, e.ID, int32(-1), []string{})
	if call.Err != nil {
		return nil, call.Err
	}
	if len(call.Body) != 2 {
		return nil, fmt.Errorf("appmenu: malformed submenu reply")
	}
	raw, ok := call.Body[1].([]any)
	if !ok {
		return nil, fmt.Errorf("appmenu: malformed submenu layout")
	}
	count := 0
	node, ok := decode(raw, 0, &count)
	if !ok {
		return nil, fmt.Errorf("appmenu: malformed or oversized submenu")
	}
	setDbusmenuAction(node.Children, Registration{Service: e.Action.Bus, Path: e.Action.Path})
	if e.Action.Direct {
		direct(node.Children)
	}
	if e.Action.NativeWayland {
		markNative(node.Children)
	}
	return node.Children, nil
}

func markNative(entries []Entry) {
	for i := range entries {
		if len(entries[i].Children) > 0 {
			markNative(entries[i].Children)
		}
		entries[i].Action.NativeWayland = true
	}
}

// Activate invokes the entry's action on the application that owns it.
func (c *Client) Activate(e Entry) error {
	switch e.Action.Kind {
	case ActionA11y:
		if c.A11y == nil {
			return fmt.Errorf("appmenu: no accessibility bus")
		}
		return c.A11y.Activate(e)
	case ActionDbusmenu:
		r := Registration{Service: e.Action.Bus, Path: e.Action.Path}
		if e.Action.NativeWayland || e.Action.Direct {
			// An endpoint the shell reads itself (inua's own menu, a KDE
			// application's) lives on the application's own connection: the
			// shell's registrar connection is not the one that serves it.
			conn, err := dbus.ConnectSessionBus()
			if err != nil {
				return err
			}
			defer conn.Close()
			return conn.Object(r.Service, r.Path).Call("com.canonical.dbusmenu.Event", 0,
				e.Action.Index, "clicked", dbus.MakeVariant(int32(0)), uint32(time.Now().Unix())).Err
		}
		if c.Registrar == nil {
			return fmt.Errorf("appmenu: no registrar")
		}
		return c.Registrar.Activate(r, e.Action.Index)
	}
	return nil
}

// kdeNamePrefix starts the well-known name a KDE application publishes its
// menu on: "org.kde.dolphin-280254", "org.kde.kate-3230144".
const kdeNamePrefix = "org.kde."

// kdeMenuRoot is where a KDE application's menus hang: /MenuBar, with one
// child for each window's menu (/MenuBar/1, /MenuBar/2 …).
const kdeMenuRoot = dbus.ObjectPath("/MenuBar")

// firefoxNamePrefix starts the well-known name Firefox publishes on the
// session bus ("org.mozilla.firefox.default", "org.mozilla.firefox.ZGVm…").
const firefoxNamePrefix = "org.mozilla.firefox."

// firefoxMenuRoot is where Firefox's GTK global-menu bridge exports its
// com.canonical.dbusmenu tree (/com/canonical/menu/0, …). On Wayland Firefox
// only announces that path over KWin's proprietary appmenu protocol and skips
// com.canonical.AppMenu.Registrar, leaving the menu exported on its own bus
// name.
const firefoxMenuRoot = dbus.ObjectPath("/com/canonical/menu")

// kdeMenu reads the menu a KDE application (Dolphin, Kate, Konsole …) or
// Firefox on Wayland exports on its own well-known name. Neither uses the
// Canonical registrar on native Wayland, so this is the only place their
// menu is; the object is a plain com.canonical.dbusmenu tree, which the
// registrar's reader takes as it is.
func (c *Client) kdeMenu(pid int) ([]Entry, bool, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, false, err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	var names []string
	if err = conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		return nil, false, err
	}
	var found []Registration
	for _, name := range names {
		var root dbus.ObjectPath
		switch {
		case strings.HasPrefix(name, kdeNamePrefix):
			root = kdeMenuRoot
		case strings.HasPrefix(name, firefoxNamePrefix):
			root = firefoxMenuRoot
		default:
			continue
		}
		var owner uint32
		if conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetConnectionUnixProcessID", 0, name).Store(&owner) != nil || int(owner) != pid {
			continue
		}
		path, err := menuChildPath(conn, name, root)
		if err != nil || path == "" {
			// A name with no menu (Kate until it is asked for one, Firefox
			// without its global-menu bridge, or an application that keeps
			// its menus to itself): not a reason to stop looking, nor to
			// stop the other sources.
			continue
		}
		found = append(found, Registration{PID: uint32(pid), Service: name, Path: path})
	}
	if len(found) != 1 {
		return nil, len(found) > 1, nil
	}
	entries, err := c.readMenu(conn, found[0])
	if err != nil {
		return nil, true, err
	}
	if !labelled(entries) {
		return nil, true, nil
	}
	direct(entries)
	return entries, true, nil
}

// kdeMenuPath is the object a KDE application exports its menu at: the
// first child of /MenuBar ("/MenuBar/2"), or "" when it has none.
func kdeMenuPath(conn *dbus.Conn, service string) (dbus.ObjectPath, error) {
	return menuChildPath(conn, service, kdeMenuRoot)
}

// menuChildPath is the first child object under root ("/MenuBar/2",
// "/com/canonical/menu/0"), or "" when it has none. Applications name the
// child after the window or menu bar it belongs to, so the path is read from
// the introspection XML rather than guessed at.
func menuChildPath(conn *dbus.Conn, service string, root dbus.ObjectPath) (dbus.ObjectPath, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	var decoded string
	call := conn.Object(service, root).CallWithContext(ctx, "org.freedesktop.DBus.Introspectable.Introspect", 0)
	if call.Err != nil {
		return "", call.Err
	}
	if err := call.Store(&decoded); err != nil {
		return "", err
	}
	// The reply is the object's own node, with its children inside it. The
	// root carries no name of its own (the bus answers "<node>" there, only
	// the test's fixture has a name), so the first child named is the menu's.
	var node struct {
		Children []struct {
			Name string `xml:"name,attr"`
		} `xml:"node"`
	}
	if err := xml.Unmarshal([]byte(decoded), &node); err != nil {
		return "", err
	}
	for _, child := range node.Children {
		if child.Name == "" || strings.ContainsAny(child.Name, "/<>") {
			continue
		}
		return root + "/" + dbus.ObjectPath(child.Name), nil
	}
	return "", nil
}

// direct marks entries the shell read itself: their clicks go to the
// application's connection, not through the Registrar.
func direct(entries []Entry) {
	for i := range entries {
		if len(entries[i].Children) > 0 {
			direct(entries[i].Children)
		}
		entries[i].Action.Direct = true
	}
}
