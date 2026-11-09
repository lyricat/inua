package appmenu

import (
	"context"
	"encoding/xml"
	"strings"

	"github.com/godbus/dbus/v5"
)

// nativeMenuPath is where an inua application publishes its menu
// (dbusmenu.Path). The name is read rather than imported so this package
// need not depend on the publisher.
const nativeMenuPath = dbus.ObjectPath("/io/github/lyricat/Inua/MenuBar")

// nativeMenuPrefix starts the well-known name one of inua's windows
// publishes its menu on.
const nativeMenuPrefix = "io.github.lyricat.Inua.Menu."

// Native finds the menu an inua application published for its window, by
// the well-known name's owning process. A process with more than one such
// name has more than one window and the shell cannot tell which window's
// menu belongs to the focused one, so none is shown rather than the wrong
// one's commands.
func (r *Registrar) Native(pid int) (Registration, bool) {
	if pid <= 0 || r.conn == nil {
		return Registration{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	var names []string
	if err := r.conn.BusObject().CallWithContext(ctx, busInterface+".ListNames", 0).Store(&names); err != nil {
		return Registration{}, false
	}
	found := ""
	for _, name := range names {
		if !strings.HasPrefix(name, nativeMenuPrefix) {
			continue
		}
		var owner uint32
		if r.conn.BusObject().CallWithContext(ctx, busInterface+".GetConnectionUnixProcessID", 0, name).Store(&owner) != nil || int(owner) != pid {
			continue
		}
		if found != "" {
			return Registration{}, false
		}
		found = name
	}
	if found == "" {
		return Registration{}, false
	}
	return Registration{PID: uint32(pid), Service: found, Path: nativeMenuPath}, true
}

// chromiumMenuPath is where a Chromium-family browser on X11 serves its
// windows' menus, one child named after each window.
const chromiumMenuPath = dbus.ObjectPath("/com/canonical/menu")

// Chromium finds the menu a Chromium-family browser serves on a connection
// of its own: running through XWayland it has one under chromiumMenuPath
// for each window, and does not register it with the registrar when the
// desktop is not Unity. The compositor does not say which X window the
// focused one is, so a browser with menus for more than one window gets
// none rather than another window's commands, as does one that did not
// answer in time.
func (r *Registrar) Chromium(pid int) (Registration, bool) {
	if pid <= 0 || r.conn == nil {
		return Registration{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	var names []string
	if err := r.conn.BusObject().CallWithContext(ctx, busInterface+".ListNames", 0).Store(&names); err != nil {
		return Registration{}, false
	}
	var found Registration
	for _, name := range names {
		if !strings.HasPrefix(name, ":") {
			continue
		}
		var owner uint32
		if r.conn.BusObject().CallWithContext(ctx, busInterface+".GetConnectionUnixProcessID", 0, name).Store(&owner) != nil || int(owner) != pid {
			continue
		}
		var text string
		if r.conn.Object(name, chromiumMenuPath).CallWithContext(ctx, "org.freedesktop.DBus.Introspectable.Introspect", 0).Store(&text) != nil {
			continue
		}
		var doc struct {
			Nodes []struct {
				Name string `xml:"name,attr"`
			} `xml:"node"`
		}
		if xml.Unmarshal([]byte(text), &doc) != nil {
			continue
		}
		for _, n := range doc.Nodes {
			path := chromiumMenuPath + "/" + dbus.ObjectPath(n.Name)
			if n.Name == "" || !path.IsValid() {
				continue
			}
			if found.Service != "" {
				return Registration{}, false
			}
			found = Registration{PID: uint32(pid), Service: name, Path: path}
		}
	}
	if ctx.Err() != nil {
		return Registration{}, false
	}
	return found, found.Service != ""
}

// Focused finds the menu of the focused process: the one it registered with
// the registrar first, the one an inua application published second, the
// one Firefox's own bridge publishes third, the one a Chromium-family
// browser serves fourth. A process that registered
// windows with menus the registrar cannot choose between gets none, not a
// published one in their place. It asks the bus, and an application that
// does not answer holds it for seconds: call it off the main thread.
func (r *Registrar) Focused(pid int) (Registration, bool) {
	if reg, ok, seen := r.forPID(pid); seen {
		return reg, ok
	}
	if reg, ok := r.Native(pid); ok {
		return reg, true
	}
	if reg, ok := r.Firefox(pid); ok {
		return reg, true
	}
	return r.Chromium(pid)
}

// Current is whether reg is still the menu Focused finds for pid: a menu
// read or opened for an application that lost the focus, or whose menu went
// away or was replaced meanwhile, is no longer acted on. Like Focused, it
// asks the bus.
func (r *Registrar) Current(reg Registration, pid int) bool {
	current, ok := r.Focused(pid)
	return ok && current == reg
}
