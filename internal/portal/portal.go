// Package portal serves inua's backends of xdg-desktop-portal
// (docs/portals.md): what every request shares, and each interface
// without GTK, handing what needs the user to a handler.
package portal

import (
	"fmt"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"

	"github.com/lyricat/inua/internal/dbusexport"
)

const (
	// Path is where the frontend calls every backend.
	Path = dbus.ObjectPath("/org/freedesktop/portal/desktop")
	// Prefix starts the backends' interface names.
	Prefix = "org.freedesktop.impl.portal."

	// The bus names of inua's three backends: the shell's, Files' and
	// the keyring's.
	ShellName   = Prefix + "desktop.inua"
	FilesName   = ShellName + ".files"
	KeyringName = ShellName + ".keyring"
)

// The answers of a request.
const (
	Success   uint32 = 0 // the user chose
	Cancelled uint32 = 1 // the user cancelled
	Ended     uint32 = 2 // closed, or it failed
)

// Interface is one backend interface to serve at Path.
type Interface struct {
	Name    string // without Prefix: "Settings"
	Version uint32
	Methods any    // its methods, as godbus exports them
	XML     string // its <method> and <signal> elements
}

// Export serves ifaces at Path on conn, with their version properties and
// introspection; the function returned takes them away again.
func Export(conn *dbus.Conn, ifaces ...Interface) (func(), error) {
	var undo []func()
	unexport := func() {
		for _, u := range undo {
			u()
		}
	}
	props := prop.Map{}
	node := `<node>`
	for _, ifc := range ifaces {
		name := Prefix + ifc.Name
		if err := dbusexport.Export(conn, ifc.Methods, Path, name); err != nil {
			unexport()
			return nil, err
		}
		undo = append(undo, func() { dbusexport.Export(conn, nil, Path, name) })
		props[name] = map[string]*prop.Prop{"version": {Value: ifc.Version, Emit: prop.EmitFalse}}
		node += fmt.Sprintf(`<interface name="%s">%s<property name="version" type="u" access="read"/></interface>`, name, ifc.XML)
	}
	if _, err := dbusexport.Properties(conn, Path, props); err != nil {
		unexport()
		return nil, err
	}
	undo = append(undo, func() { dbusexport.Export(conn, nil, Path, "org.freedesktop.DBus.Properties") })
	node += introspect.IntrospectDeclarationString + prop.IntrospectDataString + `</node>`
	if err := dbusexport.Export(conn, introspect.Introspectable(node), Path, "org.freedesktop.DBus.Introspectable"); err != nil {
		unexport()
		return nil, err
	}
	undo = append(undo, func() { dbusexport.Export(conn, nil, Path, "org.freedesktop.DBus.Introspectable") })
	return unexport, nil
}

// Own takes a backend's bus name, failing when another process has it.
func Own(conn *dbus.Conn, name string) error {
	reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue)
	if err != nil {
		return err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner && reply != dbus.RequestNameReplyAlreadyOwner {
		return fmt.Errorf("%s is served by another process", name)
	}
	return nil
}

// Request is a request the frontend may close while it is open: a dialog
// waiting for the user. Close on its handle closes Done.
type Request struct {
	conn   *dbus.Conn
	handle dbus.ObjectPath
	done   chan struct{}
	once   sync.Once
}

const requestXML = `<node><interface name="org.freedesktop.impl.portal.Request"><method name="Close"/></interface>` +
	introspect.IntrospectDeclarationString + `</node>`

// NewRequest exports the Request object at handle; End takes it away.
func NewRequest(conn *dbus.Conn, handle dbus.ObjectPath) (*Request, error) {
	r := &Request{conn: conn, handle: handle, done: make(chan struct{})}
	if err := dbusexport.Export(conn, requestObj{r}, handle, Prefix+"Request"); err != nil {
		return nil, err
	}
	dbusexport.Export(conn, introspect.Introspectable(requestXML), handle, "org.freedesktop.DBus.Introspectable")
	return r, nil
}

// Done is closed when the frontend closes the request.
func (r *Request) Done() <-chan struct{} { return r.done }

// End takes the Request object away, once the request is answered.
func (r *Request) End() {
	dbusexport.Export(r.conn, nil, r.handle, Prefix+"Request")
	dbusexport.Export(r.conn, nil, r.handle, "org.freedesktop.DBus.Introspectable")
}

type requestObj struct{ r *Request }

func (o requestObj) Close() *dbus.Error {
	o.r.once.Do(func() { close(o.r.done) })
	return nil
}

// Options are a call's a{sv} options.
type Options map[string]dbus.Variant

// String is a string option, or "".
func (o Options) String(key string) string {
	s, _ := o[key].Value().(string)
	return s
}

// Bool is a boolean option, or false.
func (o Options) Bool(key string) bool {
	b, _ := o[key].Value().(bool)
	return b
}

// Uint32 is an unsigned option and whether it was given as one.
func (o Options) Uint32(key string) (uint32, bool) {
	u, ok := o[key].Value().(uint32)
	return u, ok
}

// Strings is a list of strings, or nil.
func (o Options) Strings(key string) []string {
	s, _ := o[key].Value().([]string)
	return s
}

// Bytes is a byte string option (ay) without its ending NUL, as the
// portals send paths, or "".
func (o Options) Bytes(key string) string {
	b, _ := o[key].Value().([]byte)
	return strings.TrimRight(string(b), "\x00")
}
