// Package dbusmenu publishes an application menu using the Canonical
// com.canonical.dbusmenu protocol.
package dbusmenu

import (
	"fmt"
	"sort"
	"sync"

	"github.com/godbus/dbus/v5"
)

const (
	Interface = "com.canonical.dbusmenu"
	Path      = dbus.ObjectPath("/io/github/lyricat/Inua/MenuBar")
)

// Node is one menu row. Children are displayed as a cascading submenu.
type Node struct {
	Label     string
	Action    string
	Enabled   bool
	Separator bool
	Children  []Node
}

// Activated is called when a client invokes a menu row.
type Activated func(action string)

type layoutNode struct {
	ID         int32
	Properties map[string]dbus.Variant
	Children   []dbus.Variant
}

type Server struct {
	mu        sync.RWMutex
	conn      *dbus.Conn
	nodes     map[int32]Node
	children  map[int32][]int32
	revision  uint32
	activated Activated
}

// New creates a D-Bus menu service on conn. Its owner connection must remain
// alive for as long as the menu is registered.
func New(conn *dbus.Conn, activated Activated) *Server {
	return &Server{conn: conn, nodes: make(map[int32]Node), children: make(map[int32][]int32), revision: 1, activated: activated}
}

// Publish replaces the menu tree and exports the standard D-Bus interface.
func (s *Server) Publish(nodes []Node) error {
	s.mu.Lock()
	s.nodes = map[int32]Node{0: {Enabled: true}}
	s.children = make(map[int32][]int32)
	next := int32(1)
	var add func(int32, []Node)
	add = func(parent int32, list []Node) {
		for _, n := range list {
			id := next
			next++
			s.nodes[id] = n
			s.children[parent] = append(s.children[parent], id)
			add(id, n.Children)
		}
	}
	add(0, nodes)
	s.revision++
	s.mu.Unlock()
	if err := s.conn.Export(s, Path, Interface); err != nil {
		return fmt.Errorf("dbusmenu: export: %w", err)
	}
	if err := s.conn.Export(s, Path, "org.freedesktop.DBus.Properties"); err != nil {
		return fmt.Errorf("dbusmenu: export properties: %w", err)
	}
	return nil
}

// GetLayout returns (revision, layout node). depth=-1 returns the entire tree.
func (s *Server) GetLayout(parentID int32, depth int32, propertyNames []string) (uint32, layoutNode, *dbus.Error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.revision, s.layoutLocked(parentID, depth), nil
}

func (s *Server) layoutLocked(id, depth int32) layoutNode {
	n, ok := s.nodes[id]
	if !ok {
		return layoutNode{ID: id, Properties: map[string]dbus.Variant{}, Children: []dbus.Variant{}}
	}
	props := map[string]dbus.Variant{
		"label": dbus.MakeVariant(n.Label), "enabled": dbus.MakeVariant(n.Enabled && !n.Separator), "visible": dbus.MakeVariant(true),
	}
	if n.Separator {
		props["type"] = dbus.MakeVariant("separator")
	} else {
		props["type"] = dbus.MakeVariant("standard")
	}
	ids := s.children[id]
	if len(ids) > 0 {
		props["children-display"] = dbus.MakeVariant("submenu")
	}
	children := make([]dbus.Variant, 0, len(ids))
	if depth != 0 {
		nextDepth := depth - 1
		for _, child := range ids {
			children = append(children, dbus.MakeVariant(s.layoutLocked(child, nextDepth)))
		}
	}
	return layoutNode{ID: id, Properties: props, Children: children}
}

// GetGroupProperties returns requested item properties. An empty ID list means all IDs.
func (s *Server) GetGroupProperties(ids []int32, propertyNames []string) ([][]interface{}, *dbus.Error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(ids) == 0 {
		for id := range s.nodes {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	}
	out := make([][]interface{}, 0, len(ids))
	for _, id := range ids {
		n, ok := s.nodes[id]
		if !ok {
			continue
		}
		props := map[string]dbus.Variant{
			"label": dbus.MakeVariant(n.Label), "enabled": dbus.MakeVariant(n.Enabled && !n.Separator), "visible": dbus.MakeVariant(true),
		}
		if n.Separator {
			props["type"] = dbus.MakeVariant("separator")
		}
		out = append(out, []interface{}{id, props})
	}
	return out, nil
}

// GetProperty returns a property of one menu item.
func (s *Server) GetProperty(id int32, name string) (dbus.Variant, *dbus.Error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n, ok := s.nodes[id]
	if !ok {
		return dbus.MakeVariant(""), dbus.NewError("com.canonical.dbusmenu.Error", []any{"unknown item"})
	}
	switch name {
	case "label":
		return dbus.MakeVariant(n.Label), nil
	case "enabled":
		return dbus.MakeVariant(n.Enabled && !n.Separator), nil
	case "visible":
		return dbus.MakeVariant(true), nil
	case "type":
		if n.Separator {
			return dbus.MakeVariant("separator"), nil
		}
		return dbus.MakeVariant("standard"), nil
	}
	return dbus.MakeVariant(""), nil
}

// Event handles menu events from the shell.
func (s *Server) Event(id int32, eventID string, data dbus.Variant, timestamp uint32) *dbus.Error {
	if eventID != "clicked" {
		return nil
	}
	s.mu.RLock()
	n, ok := s.nodes[id]
	s.mu.RUnlock()
	if ok && n.Enabled && n.Action != "" && s.activated != nil {
		s.activated(n.Action)
	}
	return nil
}

// EventGroup handles batched menu events.
func (s *Server) EventGroup(events []interface{}) *dbus.Error { return nil }

// AboutToShow reports that all menu contents are already present.
func (s *Server) AboutToShow(id int32) (bool, *dbus.Error) { return false, nil }

// AboutToShowGroup reports that all requested menus are already present.
func (s *Server) AboutToShowGroup(ids []int32) ([]int32, []int32, *dbus.Error) {
	return []int32{}, []int32{}, nil
}

// Version, TextDirection, Status and IconThemePath implement D-Bus properties.
func (s *Server) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	switch name {
	case "Version":
		return dbus.MakeVariant(uint32(3)), nil
	case "TextDirection":
		return dbus.MakeVariant("ltr"), nil
	case "Status":
		return dbus.MakeVariant("normal"), nil
	case "IconThemePath":
		return dbus.MakeVariant([]string{}), nil
	}
	return dbus.MakeVariant(""), nil
}
func (s *Server) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	return map[string]dbus.Variant{"Version": dbus.MakeVariant(uint32(3)), "TextDirection": dbus.MakeVariant("ltr"), "Status": dbus.MakeVariant("normal"), "IconThemePath": dbus.MakeVariant([]string{})}, nil
}
func (s *Server) Set(iface, name string, value dbus.Variant) *dbus.Error { return nil }

// OwnName claims a well-known D-Bus name for standard consumers.
func (s *Server) OwnName(name string) error {
	reply, err := s.conn.RequestName(name, dbus.NameFlagDoNotQueue)
	if err != nil {
		return err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf("dbusmenu: name already owned: %s", name)
	}
	return nil
}

// Close unexports the menu and closes its D-Bus connection.
func (s *Server) Close(name string) {
	_ = s.conn.Export(nil, Path, Interface)
	_ = s.conn.Export(nil, Path, "org.freedesktop.DBus.Properties")
	if name != "" {
		_, _ = s.conn.ReleaseName(name)
	}
	_ = s.conn.Close()
}

// NamePrefix starts the well-known name one window's menu is published on.
// The full name is NamePrefix followed by "p<PID>.w<N>": the owning process
// and a counter, so a process with several windows publishes several names.
const NamePrefix = "io.github.lyricat.Inua.Menu."

// Name is the service name one window's menu is published on.
func Name(pid int, window uint64) string {
	return fmt.Sprintf("%sp%x.w%x", NamePrefix, pid, window)
}
