// Package fm1 serves org.freedesktop.FileManager1 for inua-files: the
// interface browsers ("Show in folder"), the shell's screenshot drawer
// and Viewer call to show folders and files. The same interface on
// inua-files' own name is how a second start hands its paths to the
// running process.
package fm1

import (
	"encoding/xml"
	"os"
	"path/filepath"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/dbusexport"
)

const (
	// Name is inua-files' own name on the session bus.
	Name = "io.github.lyricat.Inua.Files"
	// Standard is the name file managers share.
	Standard = "org.freedesktop.FileManager1"
	Path     = dbus.ObjectPath("/org/freedesktop/FileManager1")
	Iface    = "org.freedesktop.FileManager1"
)

// Handler does what the interface's methods ask. Its methods are called
// on D-Bus's goroutine with local paths, or URIs that are not local
// (sftp://…, smb://…).
type Handler interface {
	ShowFolders(paths []string)
	ShowItems(paths []string)
	ShowItemProperties(paths []string)
}

type object struct{ h Handler }

func (o object) ShowFolders(uris []string, _ string) *dbus.Error {
	o.h.ShowFolders(Paths(uris))
	return nil
}

func (o object) ShowItems(uris []string, _ string) *dbus.Error {
	o.h.ShowItems(Paths(uris))
	return nil
}

func (o object) ShowItemProperties(uris []string, _ string) *dbus.Error {
	o.h.ShowItemProperties(Paths(uris))
	return nil
}

const introspection = `<interface name="org.freedesktop.FileManager1">
  <method name="ShowFolders"><arg type="as" name="URIs" direction="in"/><arg type="s" name="StartupId" direction="in"/></method>
  <method name="ShowItems"><arg type="as" name="URIs" direction="in"/><arg type="s" name="StartupId" direction="in"/></method>
  <method name="ShowItemProperties"><arg type="as" name="URIs" direction="in"/><arg type="s" name="StartupId" direction="in"/></method>
</interface>`

// Claim takes inua-files' name for this process and serves the interface
// for h. It returns false when another process holds the name already:
// this one should hand its paths over (Hand) and leave. When it has the
// name it also asks for the standard one, queued behind a file manager
// that holds it (Nautilus), so it takes over when that one leaves.
func Claim(conn *dbus.Conn, h Handler) (bool, error) {
	if err := dbusexport.Export(conn, object{h}, Path, Iface); err != nil {
		return false, err
	}
	node := &introspect.Node{
		Name:       string(Path),
		Interfaces: []introspect.Interface{introspect.IntrospectData},
	}
	var described introspect.Node
	if err := xml.Unmarshal([]byte("<node>"+introspection+"</node>"), &described); err == nil {
		node.Interfaces = append(node.Interfaces, described.Interfaces...)
	}
	dbusexport.Export(conn, introspect.NewIntrospectable(node), Path, "org.freedesktop.DBus.Introspectable")
	reply, err := conn.RequestName(Name, dbus.NameFlagDoNotQueue)
	if err != nil {
		return false, err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner && reply != dbus.RequestNameReplyAlreadyOwner {
		dbusexport.Export(conn, nil, Path, Iface)
		return false, nil
	}
	conn.RequestName(Standard, 0)
	return true, nil
}

// Hand asks the process that holds inua-files' name to show paths:
// folders as folders, other files selected in their folders. With no
// paths it opens a window on home.
func Hand(conn *dbus.Conn, paths []string) error {
	obj := conn.Object(Name, Path)
	folders, items := Split(paths)
	if len(folders) == 0 && len(items) == 0 {
		folders = []string{files.Home()}
	}
	if len(folders) > 0 {
		if err := obj.Call(Iface+".ShowFolders", 0, URIs(folders), "").Err; err != nil {
			return err
		}
	}
	if len(items) > 0 {
		return obj.Call(Iface+".ShowItems", 0, URIs(items), "").Err
	}
	return nil
}

// Split sorts arguments into folders to open and items to show in their
// folders; a path that does not exist is opened as a folder (the window
// then says it is missing), and a URI that is not local is a folder.
func Split(paths []string) (folders, items []string) {
	for _, p := range paths {
		if !files.IsLocalURI(p) {
			folders = append(folders, p)
			continue
		}
		if local, ok := files.PathOfURI(p); ok {
			p = local
		}
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			items = append(items, p)
		} else {
			folders = append(folders, p)
		}
	}
	return folders, items
}

// URIs writes local paths as file:// URIs and leaves URIs as they are.
func URIs(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		if filepath.IsAbs(p) {
			p = files.URIOfPath(p)
		}
		out[i] = p
	}
	return out
}

// Paths reads URIs back into local paths, leaving other URIs as they are.
func Paths(uris []string) []string {
	out := make([]string, 0, len(uris))
	for _, u := range uris {
		if p, ok := files.PathOfURI(u); ok {
			u = p
		}
		if u != "" {
			out = append(out, u)
		}
	}
	return out
}
