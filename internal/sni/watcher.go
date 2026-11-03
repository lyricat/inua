// Package sni implements the StatusNotifierItem tray protocol: a watcher (the
// registry items announce themselves to) and a host (the side that shows
// them), plus the com.canonical.dbusmenu menus items export.
//
// Spec: https://www.freedesktop.org/wiki/Specifications/StatusNotifierItem/
package sni

import (
	"context"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"

	"github.com/lyricat/inua/internal/dbusexport"
)

const (
	WatcherName  = "org.kde.StatusNotifierWatcher"
	WatcherPath  = dbus.ObjectPath("/StatusNotifierWatcher")
	itemIface    = "org.kde.StatusNotifierItem"
	menuIface    = "com.canonical.dbusmenu"
	defaultPath  = "/StatusNotifierItem"
	busName      = "org.freedesktop.DBus"
	ownerChanged = busName + ".NameOwnerChanged"
)

// Qt's tray introspects the watcher before registering, so it must describe
// itself.
const watcherXML = `<node>
 <interface name="org.kde.StatusNotifierWatcher">
  <method name="RegisterStatusNotifierItem"><arg name="service" type="s" direction="in"/></method>
  <method name="RegisterStatusNotifierHost"><arg name="service" type="s" direction="in"/></method>
  <property name="RegisteredStatusNotifierItems" type="as" access="read"/>
  <property name="IsStatusNotifierHostRegistered" type="b" access="read"/>
  <property name="ProtocolVersion" type="i" access="read"/>
  <signal name="StatusNotifierItemRegistered"><arg type="s"/></signal>
  <signal name="StatusNotifierItemUnregistered"><arg type="s"/></signal>
  <signal name="StatusNotifierHostRegistered"/>
  <signal name="StatusNotifierHostUnregistered"/>
 </interface>` + introspect.IntrospectDeclarationString + prop.IntrospectDataString + `</node>`

type watcher struct {
	conn  *dbus.Conn
	props *prop.Properties

	mu    sync.Mutex
	items []string // "service/path", in registration order
	hosts map[string]bool
}

// ServeWatcher provides org.kde.StatusNotifierWatcher on conn until ctx ends.
// If another process owns the name, it waits in the bus queue and takes over
// when that process leaves.
func ServeWatcher(ctx context.Context, conn *dbus.Conn) error {
	w := &watcher{conn: conn, hosts: map[string]bool{}}
	if err := dbusexport.Export(conn, w, WatcherPath, WatcherName); err != nil {
		return err
	}
	defer dbusexport.Export(conn, nil, WatcherPath, WatcherName)
	if err := dbusexport.Export(conn, introspect.Introspectable(watcherXML), WatcherPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		return err
	}
	defer dbusexport.Export(conn, nil, WatcherPath, "org.freedesktop.DBus.Introspectable")
	props, err := dbusexport.Properties(conn, WatcherPath, prop.Map{WatcherName: {
		"RegisteredStatusNotifierItems":  {Value: []string{}, Emit: prop.EmitTrue},
		"IsStatusNotifierHostRegistered": {Value: false, Emit: prop.EmitTrue},
		"ProtocolVersion":                {Value: int32(0)},
	}})
	if err != nil {
		return err
	}
	defer dbusexport.Export(conn, nil, WatcherPath, "org.freedesktop.DBus.Properties")
	w.props = props

	sigs := make(chan *dbus.Signal, 32)
	conn.Signal(sigs)
	defer conn.RemoveSignal(sigs)
	match := []dbus.MatchOption{dbus.WithMatchSender(busName), dbus.WithMatchMember("NameOwnerChanged")}
	if err := conn.AddMatchSignal(match...); err != nil {
		return err
	}
	defer conn.RemoveMatchSignal(match...)

	// No DoNotQueue: wait for the name if someone else has it.
	if _, err := conn.RequestName(WatcherName, 0); err != nil {
		return err
	}
	defer conn.ReleaseName(WatcherName)

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
				newOwner, _ := sig.Body[2].(string)
				if newOwner == "" {
					w.forget(name)
				}
			}
		}
	}
}

// RegisterStatusNotifierItem accepts a bus name (the item is at the default
// path) or an object path (the item is on the sender's connection).
func (w *watcher) RegisterStatusNotifierItem(sender dbus.Sender, arg string) *dbus.Error {
	key := arg + defaultPath
	if strings.HasPrefix(arg, "/") {
		key = string(sender) + arg
	}
	w.mu.Lock()
	for _, k := range w.items {
		if k == key {
			w.mu.Unlock()
			return nil
		}
	}
	w.items = append(w.items, key)
	items := append([]string(nil), w.items...)
	w.mu.Unlock()

	w.props.SetMust(WatcherName, "RegisteredStatusNotifierItems", items)
	w.conn.Emit(WatcherPath, WatcherName+".StatusNotifierItemRegistered", key)
	return nil
}

func (w *watcher) RegisterStatusNotifierHost(sender dbus.Sender, service string) *dbus.Error {
	if strings.HasPrefix(service, "/") || service == "" {
		service = string(sender)
	}
	w.mu.Lock()
	w.hosts[service] = true
	w.mu.Unlock()
	w.props.SetMust(WatcherName, "IsStatusNotifierHostRegistered", true)
	w.conn.Emit(WatcherPath, WatcherName+".StatusNotifierHostRegistered")
	return nil
}

// forget drops the items and hosts of a bus name that has gone away.
func (w *watcher) forget(name string) {
	w.mu.Lock()
	var gone []string
	kept := w.items[:0]
	for _, k := range w.items {
		if service, _, _ := strings.Cut(k, "/"); service == name {
			gone = append(gone, k)
		} else {
			kept = append(kept, k)
		}
	}
	w.items = kept
	items := append([]string(nil), w.items...)
	hostGone := w.hosts[name]
	delete(w.hosts, name)
	hasHosts := len(w.hosts) > 0
	w.mu.Unlock()

	if len(gone) > 0 {
		w.props.SetMust(WatcherName, "RegisteredStatusNotifierItems", items)
		for _, k := range gone {
			w.conn.Emit(WatcherPath, WatcherName+".StatusNotifierItemUnregistered", k)
		}
	}
	if hostGone {
		w.props.SetMust(WatcherName, "IsStatusNotifierHostRegistered", hasHosts)
		w.conn.Emit(WatcherPath, WatcherName+".StatusNotifierHostUnregistered")
	}
}
