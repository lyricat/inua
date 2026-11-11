package appmenu

import (
	"context"
	"encoding/xml"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"
)

// firefoxNamePrefix starts the well-known names Firefox owns on its
// connection ("org.mozilla.firefox.ZGVmYXVsdA__", and the search
// provider's on some desktops): several of them may be one connection's.
const firefoxNamePrefix = "org.mozilla.firefox."

// firefoxMenuRoot is where Firefox's GTK global-menu bridge serves its
// windows' com.canonical.dbusmenu trees (/com/canonical/menu/<window>). On
// Wayland it registers none of them with the registrar.
const firefoxMenuRoot = dbus.ObjectPath("/com/canonical/menu")

// maxProcessDepth bounds the walk up from the focused process to the
// Firefox process that started it.
const maxProcessDepth = 16

// Firefox finds the menu Firefox's own bridge serves for the focused
// process pid. The bridge's item ids change each time its layout is read,
// so the menu is read whole (Registration.Whole).
func (r *Registrar) Firefox(pid int) (Registration, bool) {
	reg, ok, _ := r.firefox(pid)
	return reg, ok
}

// firefox is Firefox, and whether a Firefox of pid's serves menus at all,
// even ones it cannot choose between: the connection is then not looked
// at again as a Chromium-family browser's, whose menus are read in parts.
//
// The connections owning a Firefox name are the candidates, each once
// however many such names it owns. One matches when its process is pid,
// or the Firefox process pid was started by (firefoxOf); a process that
// merely started Firefox, a terminal, does not match, nor does another
// program Firefox started. A matching connection with menus for more than
// one window, or more than one such connection, gives none: the compositor
// does not say which of them is the focused window's. One that does not
// answer in time gives none too.
func (r *Registrar) firefox(pid int) (reg Registration, ok, seen bool) {
	if pid <= 0 || r.conn == nil {
		return Registration{}, false, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	var names []string
	if err := r.conn.BusObject().CallWithContext(ctx, busInterface+".ListNames", 0).Store(&names); err != nil {
		return Registration{}, false, false
	}
	var owners []string
	for _, name := range names {
		if !strings.HasPrefix(name, firefoxNamePrefix) {
			continue
		}
		var owner string
		if r.conn.BusObject().CallWithContext(ctx, busInterface+".GetNameOwner", 0, name).Store(&owner) != nil || owner == "" {
			continue
		}
		if !slices.Contains(owners, owner) {
			owners = append(owners, owner)
		}
	}
	var found Registration
	for _, owner := range owners {
		var opid uint32
		if r.conn.BusObject().CallWithContext(ctx, busInterface+".GetConnectionUnixProcessID", 0, owner).Store(&opid) != nil || !r.firefoxOf(int(opid), pid) {
			continue
		}
		paths, err := menuChildren(ctx, r.conn, owner, firefoxMenuRoot)
		if err != nil {
			continue // no menus served, or none in time (below)
		}
		for _, path := range paths {
			if found.Service != "" {
				return Registration{}, false, true
			}
			found = Registration{PID: uint32(pid), Service: owner, Path: path, Whole: true}
		}
	}
	if ctx.Err() != nil {
		return Registration{}, false, found.Service != ""
	}
	return found, found.Service != "", found.Service != ""
}

// firefoxOf reports whether owner, the process owning a Firefox name, is
// the focused process pid or the one that started it: each process on the
// way up from pid to owner runs the same program as owner.
func (r *Registrar) firefoxOf(owner, pid int) bool {
	if owner <= 0 {
		return false
	}
	if owner == pid {
		return true
	}
	program := r.program(owner)
	if program == "" {
		return false
	}
	for range maxProcessDepth {
		if pid <= 1 || r.program(pid) != program {
			return false
		}
		ppid, ok := r.parentPID(pid)
		if !ok {
			return false
		}
		if ppid == owner {
			return true
		}
		pid = ppid
	}
	return false
}

// program is the executable process pid runs, "" when it cannot be read.
func (r *Registrar) program(pid int) string {
	exe, err := os.Readlink(filepath.Join(r.proc, strconv.Itoa(pid), "exe"))
	if err != nil {
		return ""
	}
	return exe
}

// parentPID reads a process's parent from its stat file.
func (r *Registrar) parentPID(pid int) (int, bool) {
	b, err := os.ReadFile(filepath.Join(r.proc, strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, false
	}
	// The command name, in parentheses, may itself hold spaces and ')'.
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
	return ppid, err == nil && ppid > 0
}

// menuChildren are the objects service has under root, one menu for each
// of its windows, as its introspection names them.
func menuChildren(ctx context.Context, conn *dbus.Conn, service string, root dbus.ObjectPath) ([]dbus.ObjectPath, error) {
	var text string
	if err := conn.Object(service, root).CallWithContext(ctx, "org.freedesktop.DBus.Introspectable.Introspect", 0).Store(&text); err != nil {
		return nil, err
	}
	var doc struct {
		Nodes []struct {
			Name string `xml:"name,attr"`
		} `xml:"node"`
	}
	if err := xml.Unmarshal([]byte(text), &doc); err != nil {
		return nil, err
	}
	var paths []dbus.ObjectPath
	for _, n := range doc.Nodes {
		path := root + "/" + dbus.ObjectPath(n.Name)
		if n.Name == "" || strings.Contains(n.Name, "/") || !path.IsValid() {
			continue
		}
		paths = append(paths, path)
	}
	return paths, nil
}
