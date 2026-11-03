// Package dbusexport serializes godbus exports and removals on each connection.
// All exports, including startup exports and properties, must use this package.
package dbusexport

import (
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
)

// godbus v5.2.2's export and unexport read the default handler's object map
// without its lock. Serialize those operations without locking method dispatch
// or execution: a method may itself export or remove an object.
var exports = struct {
	sync.Mutex
	conns map[*dbus.Conn]*pending
}{conns: make(map[*dbus.Conn]*pending)}

type pending struct {
	mu   sync.Mutex
	refs int // executing and waiting exports, guarded by exports.Mutex
}

func lock(conn *dbus.Conn) func() {
	exports.Lock()
	p := exports.conns[conn]
	if p == nil {
		p = new(pending)
		exports.conns[conn] = p
	}
	p.refs++
	exports.Unlock()
	p.mu.Lock()
	return func() {
		p.mu.Unlock()
		exports.Lock()
		p.refs--
		if p.refs == 0 {
			// No caller can still acquire this mutex. Forget idle connections
			// immediately, without a close hook or a permanent registry entry.
			delete(exports.conns, conn)
		}
		exports.Unlock()
	}
}

// Export is conn.Export with per-connection serialization, including nil removal.
func Export(conn *dbus.Conn, v any, path dbus.ObjectPath, iface string) error {
	defer lock(conn)()
	return conn.Export(v, path, iface)
}

// ExportAll is conn.ExportAll with per-connection serialization.
func ExportAll(conn *dbus.Conn, v any, path dbus.ObjectPath, iface string) error {
	defer lock(conn)()
	return conn.ExportAll(v, path, iface)
}

// ExportWithMap is conn.ExportWithMap with per-connection serialization.
func ExportWithMap(conn *dbus.Conn, v any, mapping map[string]string, path dbus.ObjectPath, iface string) error {
	defer lock(conn)()
	return conn.ExportWithMap(v, mapping, path, iface)
}

// ExportSubtree is conn.ExportSubtree with per-connection serialization.
func ExportSubtree(conn *dbus.Conn, v any, path dbus.ObjectPath, iface string) error {
	defer lock(conn)()
	return conn.ExportSubtree(v, path, iface)
}

// ExportSubtreeWithMap is conn.ExportSubtreeWithMap with per-connection serialization.
func ExportSubtreeWithMap(conn *dbus.Conn, v any, mapping map[string]string, path dbus.ObjectPath, iface string) error {
	defer lock(conn)()
	return conn.ExportSubtreeWithMap(v, mapping, path, iface)
}

// ExportMethodTable is conn.ExportMethodTable with per-connection serialization.
func ExportMethodTable(conn *dbus.Conn, methods map[string]any, path dbus.ObjectPath, iface string) error {
	defer lock(conn)()
	return conn.ExportMethodTable(methods, path, iface)
}

// ExportSubtreeMethodTable is conn.ExportSubtreeMethodTable with per-connection serialization.
func ExportSubtreeMethodTable(conn *dbus.Conn, methods map[string]any, path dbus.ObjectPath, iface string) error {
	defer lock(conn)()
	return conn.ExportSubtreeMethodTable(methods, path, iface)
}

// Properties protects prop.Export, which calls conn.Export internally.
func Properties(conn *dbus.Conn, path dbus.ObjectPath, props prop.Map) (*prop.Properties, error) {
	defer lock(conn)()
	return prop.Export(conn, path, props)
}
