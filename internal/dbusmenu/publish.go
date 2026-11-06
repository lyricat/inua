package dbusmenu

import (
	"os"
	"sync/atomic"

	"github.com/godbus/dbus/v5"
)

// menuSeq numbers the menus one process publishes, so several windows of
// one application own a name each.
var menuSeq atomic.Uint64

// Published is a menu served on the session bus under NamePrefix.
type Published struct {
	conn   *dbus.Conn
	server *Server
	name   string
}

// Publish serves nodes for this process on the session bus, under the name
// one of its windows publishes a menu on.
func Publish(nodes []Node, activated Activated) (*Published, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	name := Name(os.Getpid(), menuSeq.Add(1))
	server := New(conn, activated)
	if err := server.OwnName(name); err != nil {
		conn.Close()
		return nil, err
	}
	if err := server.Publish(nodes); err != nil {
		server.Close(name)
		conn.Close()
		return nil, err
	}
	return &Published{conn: conn, server: server, name: name}, nil
}

// Close unregisters the menu and drops the bus connection.
func (p *Published) Close() {
	if p == nil {
		return
	}
	p.server.Close(p.name)
	p.conn.Close()
}
