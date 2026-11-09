package appmenu

import (
	"context"
	"encoding/xml"
	"os"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"
)

// firefoxNamePrefix starts the well-known name Firefox publishes
// ("org.mozilla.firefox.default", "org.mozilla.firefox.ZGVm…").
const firefoxNamePrefix = "org.mozilla.firefox."

// firefoxMenuRoot is where Firefox's GTK global-menu bridge exports its
// com.canonical.dbusmenu tree (/com/canonical/menu/0, …). On Wayland
// Firefox skips the Canonical registrar and leaves the menu on its own bus
// name.
const firefoxMenuRoot = dbus.ObjectPath("/com/canonical/menu")

// Firefox finds the menu Firefox published for pid on its own bus name, the
// only place its Wayland menu is.
func (r *Registrar) Firefox(pid int) (Registration, bool) {
	if pid <= 0 || r.conn == nil {
		return Registration{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	var names []string
	if err := r.conn.BusObject().CallWithContext(ctx, busInterface+".ListNames", 0).Store(&names); err != nil {
		return Registration{}, false
	}
	found := Registration{}
	for _, name := range names {
		if !strings.HasPrefix(name, firefoxNamePrefix) {
			continue
		}
		var owner uint32
		if r.conn.BusObject().CallWithContext(ctx, busInterface+".GetConnectionUnixProcessID", 0, name).Store(&owner) != nil || !sameProcessTree(int(owner), pid) {
			continue
		}
		path, err := menuChildPath(r.conn, name, firefoxMenuRoot)
		if err != nil || path == "" {
			continue
		}
		if found.Service != "" {
			return Registration{}, false
		}
		found = Registration{PID: uint32(pid), Service: name, Path: path}
	}
	return found, found.Service != ""
}

// sameProcessTree reports whether a and b are one process tree: Firefox's
// menu name belongs to its parent process, while its window is a content
// process, a child of it.
func sameProcessTree(a, b int) bool {
	if a == b {
		return true
	}
	return ancestorOf(a, b) || ancestorOf(b, a)
}

// ancestorOf reports whether anc is pid or one of its parents.
func ancestorOf(anc, pid int) bool {
	for i := 0; i < 16 && pid > 1; i++ {
		if pid == anc {
			return true
		}
		ppid, ok := parentPID(pid)
		if !ok {
			return false
		}
		pid = ppid
	}
	return false
}

// parentPID reads a process's parent from /proc/PID/stat.
func parentPID(pid int) (int, bool) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0, false
	}
	fields := strings.Fields(s[i+1:])
	if len(fields) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(fields[1])
	return ppid, err == nil
}

// menuChildPath is the first child object under root ("/com/canonical/menu/0"),
// read from the introspection XML rather than guessed at.
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
