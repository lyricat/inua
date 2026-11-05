package hypr

import (
	"bufio"
	"io"
	"net"
	"path/filepath"
	"sync"
	"testing"

	"github.com/lyricat/inua/internal/sockdir"
)

// fakeHyprland serves the two Hyprland sockets from a temp dir.
type fakeHyprland struct {
	t     *testing.T
	paths Paths

	mu         sync.Mutex
	replies    map[string]string
	dispatched []string
	requests   []string
	eventConns []net.Conn
	queries    int
	connected  chan struct{}

	reqLn, evLn net.Listener
}

func newFakeHyprland(t *testing.T) *fakeHyprland {
	t.Helper()
	dir := sockdir.New(t)
	f := &fakeHyprland{
		t: t,
		paths: Paths{
			Request: filepath.Join(dir, ".socket.sock"),
			Event:   filepath.Join(dir, ".socket2.sock"),
		},
		replies: map[string]string{
			"j/monitors":     `[]`,
			"j/workspaces":   `[]`,
			"j/clients":      `[]`,
			"j/activewindow": `{}`,
		},
		connected: make(chan struct{}, 16),
	}
	var err error
	if f.reqLn, err = net.Listen("unix", f.paths.Request); err != nil {
		t.Fatal(err)
	}
	if f.evLn, err = net.Listen("unix", f.paths.Event); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.close)
	go f.serveRequests()
	go f.serveEvents()
	return f
}

func (f *fakeHyprland) close() {
	f.reqLn.Close()
	f.evLn.Close()
	f.dropEventConns()
}

func (f *fakeHyprland) set(cmd, reply string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies[cmd] = reply
}

func (f *fakeHyprland) queryCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.queries
}

func (f *fakeHyprland) serveRequests() {
	for {
		conn, err := f.reqLn.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			buf := make([]byte, 4096)
			n, _ := conn.Read(buf)
			cmd := string(buf[:n])
			f.mu.Lock()
			reply, ok := f.replies[cmd]
			f.requests = append(f.requests, cmd)
			if cmd == "j/clients" {
				f.queries++
			}
			if len(cmd) > 9 && cmd[:9] == "dispatch " {
				f.dispatched = append(f.dispatched, cmd[9:])
				reply, ok = "ok", true
			}
			f.mu.Unlock()
			if !ok {
				reply = "unknown request"
			}
			io.WriteString(conn, reply)
		}()
	}
}

func (f *fakeHyprland) serveEvents() {
	for {
		conn, err := f.evLn.Accept()
		if err != nil {
			return
		}
		f.mu.Lock()
		f.eventConns = append(f.eventConns, conn)
		f.mu.Unlock()
		f.connected <- struct{}{}
	}
}

func (f *fakeHyprland) emit(lines ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.eventConns {
		w := bufio.NewWriter(c)
		for _, l := range lines {
			w.WriteString(l + "\n")
		}
		w.Flush()
	}
}

// dropEventConns simulates Hyprland closing the event stream.
func (f *fakeHyprland) dropEventConns() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.eventConns {
		c.Close()
	}
	f.eventConns = nil
}
