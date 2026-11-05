package wayfire

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/compositor"
)

// What Wayfire 0.11 answered in the experiment (docs/compositor-experiments.md):
// one output showing cell 0,0 of a 3x3 grid, a window there, one sent to
// cell 1,1 (its place is a width and a height further), and the shell's
// own bar.
const (
	outputsJSON = `[{"id":1,"name":"HEADLESS-1","geometry":{"x":0.0,"y":0.0,"width":1280.0,"height":720.0},"workarea":{"x":0.0,"y":36.0,"width":1280.0,"height":684.0},"wset-index":1,"workspace":{"x":0,"y":0,"grid_width":3,"grid_height":3}}]`
	viewsJSON   = `[
	 {"id":3,"pid":39,"title":"first","app-id":"foot","geometry":{"x":1572.0,"y":833.0,"width":696.0,"height":494.0},"output-id":1,"output-name":"HEADLESS-1","last-focus-timestamp":100,"role":"toplevel","mapped":true,"activated":false,"tiled-edges":0,"wset-index":1,"type":"toplevel"},
	 {"id":5,"pid":67,"title":"second","app-id":"second-app","geometry":{"x":292.0,"y":113.0,"width":696.0,"height":494.0},"output-id":1,"output-name":"HEADLESS-1","last-focus-timestamp":200,"role":"toplevel","mapped":true,"activated":true,"tiled-edges":15,"wset-index":1,"type":"toplevel"},
	 {"id":7,"pid":90,"title":"nil","app-id":"nil","geometry":{"x":0.0,"y":0.0,"width":1280.0,"height":36.0},"output-id":1,"output-name":"HEADLESS-1","last-focus-timestamp":0,"role":"desktop-environment","mapped":true,"activated":false,"tiled-edges":0,"wset-index":1,"type":"layer-shell"},
	 {"id":9,"pid":91,"title":"starting","app-id":"x","geometry":{"x":100.0,"y":100.0,"width":0.0,"height":0.0},"output-id":1,"output-name":"HEADLESS-1","last-focus-timestamp":0,"role":"toplevel","mapped":false,"activated":false,"tiled-edges":0,"wset-index":1,"type":"toplevel"}]`
)

var allMethods = []string{"list-methods", "window-rules/list-views", "window-rules/list-outputs", "window-rules/focus-view",
	"window-rules/close-view", "window-rules/configure-view", "window-rules/get-focused-output", "window-rules/events/watch",
	"vswitch/set-workspace", "vswitch/send-view", "wsets/send-view-to-wset"}

// fakeWayfire serves the IPC: replies by method, the calls recorded, an
// event connection fed by emit.
type fakeWayfire struct {
	socket  string
	mu      sync.Mutex
	replies map[string]string
	methods []string
	calls   []string
	streams []net.Conn
	opened  chan struct{}
	ln      net.Listener
}

func newFakeWayfire(t *testing.T) *fakeWayfire {
	t.Helper()
	f := &fakeWayfire{
		socket: filepath.Join(t.TempDir(), "wayfire.socket"),
		replies: map[string]string{
			"window-rules/list-outputs":       outputsJSON,
			"window-rules/list-views":         viewsJSON,
			"window-rules/get-focused-output": `{"result":"ok","info":{"id":1,"name":"HEADLESS-1"}}`,
		},
		methods: allMethods,
		opened:  make(chan struct{}, 16),
	}
	var err error
	if f.ln, err = net.Listen("unix", f.socket); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.ln.Close(); f.drop() })
	go func() {
		for {
			conn, err := f.ln.Accept()
			if err != nil {
				return
			}
			go f.handle(conn)
		}
	}()
	return f
}

func (f *fakeWayfire) handle(conn net.Conn) {
	for {
		body, err := readMessage(conn)
		if err != nil {
			conn.Close()
			return
		}
		var req struct {
			Method string
			Data   json.RawMessage
		}
		json.Unmarshal(body, &req)
		reply := func(s string) {
			var v any
			if err := json.Unmarshal([]byte(s), &v); err != nil {
				panic(err)
			}
			b, _ := json.Marshal(v)
			hdr := make([]byte, 4)
			hdr[0], hdr[1], hdr[2], hdr[3] = byte(len(b)), byte(len(b)>>8), byte(len(b)>>16), byte(len(b)>>24)
			conn.Write(append(hdr, b...))
		}
		f.mu.Lock()
		known := slices.Contains(f.methods, req.Method)
		canned, ok := f.replies[req.Method]
		if known && req.Method != "list-methods" && !ok && req.Method != "window-rules/events/watch" {
			f.calls = append(f.calls, req.Method+" "+string(req.Data))
		}
		methods, _ := json.Marshal(f.methods)
		f.mu.Unlock()
		switch {
		case !known:
			reply(`{"error":"No such method found!","method":"` + req.Method + `"}`)
		case req.Method == "list-methods":
			reply(`{"methods":` + string(methods) + `}`)
		case req.Method == "window-rules/events/watch":
			reply(`{"result":"ok"}`)
			f.mu.Lock()
			f.streams = append(f.streams, conn)
			f.mu.Unlock()
			f.opened <- struct{}{}
			return
		case ok:
			reply(canned)
		default:
			reply(`{"result":"ok"}`)
		}
	}
}

func (f *fakeWayfire) set(method, reply string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies[method] = reply
}

func (f *fakeWayfire) emit(events ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.streams {
		for _, e := range events {
			b := []byte(`{"event":"` + e + `"}`)
			hdr := []byte{byte(len(b)), byte(len(b) >> 8), 0, 0}
			c.Write(append(hdr, b...))
		}
	}
}

func (f *fakeWayfire) drop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.streams {
		c.Close()
	}
	f.streams = nil
}

func (f *fakeWayfire) did() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

type recorder struct {
	mu      sync.Mutex
	snaps   []compositor.Snapshot
	conn    []bool
	caps    []compositor.Capabilities
	updated chan struct{}
}

func newRecorder() *recorder { return &recorder{updated: make(chan struct{}, 64)} }
func (r *recorder) OnSnapshot(s compositor.Snapshot) {
	r.mu.Lock()
	r.snaps = append(r.snaps, s)
	r.mu.Unlock()
	r.updated <- struct{}{}
}
func (r *recorder) OnConnection(up bool, _ error) {
	r.mu.Lock()
	r.conn = append(r.conn, up)
	r.mu.Unlock()
}
func (r *recorder) OnCapabilities(c compositor.Capabilities) {
	r.mu.Lock()
	r.caps = append(r.caps, c)
	r.mu.Unlock()
}
func (r *recorder) last() compositor.Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snaps[len(r.snaps)-1]
}

func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func start(t *testing.T, f *fakeWayfire) (*Backend, *recorder) {
	t.Helper()
	b := New(NewClient(f.socket))
	b.Debounce, b.MinBackoff = 20*time.Millisecond, 10*time.Millisecond
	r := newRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { b.Run(ctx, r); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return b, r
}

// The grid becomes workspaces, a view's cell comes from its place, the
// shell's surfaces and unmapped views are left out.
func TestSnapshot(t *testing.T) {
	f := newFakeWayfire(t)
	snap, err := New(NewClient(f.socket)).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Outputs) != 1 || snap.Outputs[0].ActiveWorkspace != "1:0,0" || !snap.Outputs[0].Focused || snap.FocusedOutput != "HEADLESS-1" {
		t.Fatalf("outputs: %+v", snap.Outputs)
	}
	if len(snap.Workspaces) != 9 || snap.Workspaces[4].ID != "1:1,1" || snap.Workspaces[4].Name != "5" || snap.Workspaces[4].Order != 4 {
		t.Fatalf("workspaces: %+v", snap.Workspaces)
	}
	if !snap.Workspaces[0].Active || !snap.Workspaces[0].Focused || snap.Workspaces[1].Active {
		t.Fatalf("active cell: %+v", snap.Workspaces[:2])
	}
	if len(snap.Windows) != 2 {
		t.Fatalf("windows: %+v", snap.Windows)
	}
	w := snap.Windows[0]
	if w.ID != "3" || w.Workspace != "1:1,1" || w.AppID != "foot" || w.PID != 39 || w.FocusRank != 2 || !w.Floating {
		t.Fatalf("window on another cell: %+v", w)
	}
	// Its place within its own cell, as if that cell were shown.
	if w.Geometry == nil || *w.Geometry != (compositor.Rect{X: 292, Y: 113, W: 696, H: 494}) {
		t.Fatalf("geometry: %+v", w.Geometry)
	}
	if w := snap.Windows[1]; w.Workspace != "1:0,0" || !w.Focused || w.FocusRank != 1 || w.Floating || snap.FocusedWindow != "5" {
		t.Fatalf("focused window: %+v", w)
	}
	if _, x, y, err := parseWorkspace("2:1,2"); err != nil || x != 1 || y != 2 {
		t.Fatalf("parse: %d %d %v", x, y, err)
	}
	if _, _, _, err := parseWorkspace("3"); err == nil {
		t.Fatal("a Hyprland id parsed")
	}
}

func TestBackendRunAndActions(t *testing.T) {
	f := newFakeWayfire(t)
	b, r := start(t, f)
	waitFor(t, r.updated, "initial snapshot")
	<-f.opened
	if len(r.last().Windows) != 2 {
		t.Fatalf("initial: %+v", r.last())
	}
	if !b.Capabilities().Has(compositor.CapWorkspaces) || !b.Capabilities().Has(compositor.CapMoveWithoutFollow) {
		t.Fatalf("capabilities: %v", b.Capabilities())
	}
	// A burst of events is one read; an animation's events are none.
	f.set("window-rules/list-views", `[]`)
	f.emit("plugin-activation-state-changed", "view-unmapped", "view-focused", "view-geometry-changed")
	waitFor(t, r.updated, "snapshot after events")
	time.Sleep(60 * time.Millisecond)
	if n := len(r.snaps); n != 2 || len(r.last().Windows) != 0 {
		t.Fatalf("%d snapshots, windows %+v", n, r.last().Windows)
	}
	f.set("window-rules/list-views", viewsJSON)

	ctx := context.Background()
	for _, do := range []func() error{
		func() error { return b.FocusWindow(ctx, "3") },
		func() error { return b.CloseWindow(ctx, "5") },
		func() error { return b.FocusWorkspace(ctx, "1:2,0") },
		func() error { return b.MoveWindow(ctx, "5", "1:1,0", false) },
		func() error { return b.MoveWindow(ctx, "5", "2:0,0", true) },
		func() error { return b.BringHere(ctx, 39) },
		func() error { return b.MoveWorkspace(ctx, "1:1,1", "HEADLESS-1") },
	} {
		if err := do(); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{
		`window-rules/focus-view {"id":3}`, `window-rules/close-view {"id":5}`,
		`vswitch/set-workspace {"output-id":1,"x":2,"y":0}`,
		`vswitch/send-view {"view-id":5,"x":1,"y":0}`,
		`wsets/send-view-to-wset {"view-id":5,"wset-index":2}`, `vswitch/send-view {"view-id":5,"x":0,"y":0}`, `window-rules/focus-view {"id":5}`,
		`vswitch/send-view {"view-id":3,"x":0,"y":0}`, `window-rules/focus-view {"id":3}`,
	}
	if got := f.did(); !slices.Equal(got, want) {
		t.Fatalf("calls %q\nwant %q", got, want)
	}
	if err := b.MoveWorkspace(ctx, "1:1,1", "DP-2"); !errors.Is(err, compositor.ErrUnsupported) {
		t.Fatalf("a cell moved to another output: %v", err)
	}
	if err := b.FocusWindow(ctx, "0xaa"); err == nil {
		t.Fatal("a Hyprland address went through")
	}
	if err := b.FocusWorkspace(ctx, "9:0,0"); err == nil || !strings.Contains(err.Error(), "no output") {
		t.Fatalf("an unknown set: %v", err)
	}

	// A plugin unloaded: its capability is unavailable after a refresh,
	// and its method is Wayfire's error, the connection kept.
	f.mu.Lock()
	f.methods = slices.DeleteFunc(slices.Clone(allMethods), func(m string) bool { return strings.HasPrefix(m, "vswitch/") })
	f.mu.Unlock()
	b.Refresh()
	deadline := time.Now().Add(time.Second)
	for b.Capabilities()[compositor.CapWorkspaces] != compositor.Unavailable {
		if time.Now().After(deadline) {
			t.Fatalf("capabilities after the plugin went: %v", b.Capabilities())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := b.FocusWorkspace(ctx, "1:2,0"); err == nil || !strings.Contains(err.Error(), "No such method") {
		t.Fatalf("a missing plugin's method: %v", err)
	}

	// The connection closes: down, then up again on the same socket.
	f.drop()
	<-f.opened
	deadline = time.Now().Add(time.Second)
	for {
		r.mu.Lock()
		conn := slices.Clone(r.conn)
		r.mu.Unlock()
		if len(conn) >= 3 && conn[0] && !conn[1] && conn[2] {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("connection reports %v", conn)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestNewWorkspace(t *testing.T) {
	f := newFakeWayfire(t)
	b := New(NewClient(f.socket))
	snap, err := b.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// 0,0 is shown, 1,1 has a window: the first free cell is 1,0.
	if ws, ok := b.NewWorkspace(snap, "HEADLESS-1", nil); !ok || ws.ID != "1:1,0" {
		t.Fatalf("%+v %v", ws, ok)
	}
	if ws, ok := b.NewWorkspace(snap, "HEADLESS-1", func(id string) bool { return id == "1:1,0" }); !ok || ws.ID != "1:2,0" {
		t.Fatalf("with 1,0 reserved: %+v %v", ws, ok)
	}
	if _, ok := b.NewWorkspace(snap, "DP-9", nil); ok {
		t.Fatal("a cell on an unknown output")
	}
	// Asked before Run, the capabilities are probed at once.
	if !b.Capabilities().Has(compositor.CapPlacement) {
		t.Fatalf("lazy probe: %v", b.Capabilities())
	}
	t.Setenv("WAYFIRE_SOCKET", "")
	if _, err := Open(); err != compositor.ErrNoCompositor {
		t.Fatalf("Open outside Wayfire: %v", err)
	}
}
