package appmenu

import (
	"context"
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

// Focused finds the menu of the focused process: the one it registered with
// the registrar first, the one an inua application published second. A
// process that registered windows with menus the registrar cannot choose
// between gets none, not a published one in their place.
func (r *Registrar) Focused(pid int) (Registration, bool) {
	if reg, ok, seen := r.forPID(pid); seen {
		return reg, ok
	}
	return r.Native(pid)
}

// Current is whether reg is still the menu Focused finds for pid: a menu
// read or opened for an application that lost the focus, or whose menu went
// away or was replaced meanwhile, is no longer acted on.
func (r *Registrar) Current(reg Registration, pid int) bool {
	current, ok := r.Focused(pid)
	return ok && current == reg
}
