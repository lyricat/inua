package niri

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/sockdir"
)

const (
	outputsJSON = `{"eDP-1":{"name":"eDP-1","make":"BOE","model":"NE160","modes":[{"width":2560,"height":1600,"refresh_rate":60000,"is_preferred":true}],"current_mode":0,
	  "logical":{"x":0,"y":0,"width":1280,"height":800,"scale":2.0,"transform":"Normal"}},
	 "HDMI-A-1":{"name":"HDMI-A-1","make":"Dell","model":"U27","modes":[{"width":1920,"height":1080,"refresh_rate":59997,"is_preferred":true}],"current_mode":0,
	  "logical":{"x":1280,"y":0,"width":1920,"height":1080,"scale":1.0,"transform":"90"}},
	 "DP-3":{"name":"DP-3","make":"","model":"","modes":[],"current_mode":null,"logical":null}}`
	workspacesJSON = `[{"id":3,"idx":1,"name":null,"output":"HDMI-A-1","is_urgent":false,"is_active":true,"is_focused":false,"active_window_id":null},
	 {"id":1,"idx":1,"name":"web","output":"eDP-1","is_urgent":false,"is_active":true,"is_focused":true,"active_window_id":10},
	 {"id":2,"idx":2,"name":null,"output":"eDP-1","is_urgent":false,"is_active":false,"is_focused":false,"active_window_id":null}]`
	windowsJSON = `[{"id":10,"title":"Terminal","app_id":"kitty","pid":1234,"workspace_id":1,"is_focused":true,"is_floating":false,"is_urgent":false,
	  "layout":{"pos_in_scrolling_layout":[1,1],"tile_size":[640.0,780.0],"window_size":[636,776],"tile_pos_in_workspace_view":[10.0,10.0],"window_offset_in_tile":[2.0,2.0]},
	  "focus_timestamp":{"secs":100,"nanos":0}},
	 {"id":11,"title":"Browser","app_id":"firefox","pid":1235,"workspace_id":1,"is_focused":false,"is_floating":true,"is_urgent":true,
	  "layout":{"pos_in_scrolling_layout":null,"tile_size":[600.0,400.0],"window_size":[600,400],"tile_pos_in_workspace_view":null,"window_offset_in_tile":[0.0,0.0]},
	  "focus_timestamp":{"secs":200,"nanos":5}},
	 {"id":12,"title":null,"app_id":null,"pid":null,"workspace_id":3,"is_focused":false,"is_floating":false,"is_urgent":false,
	  "layout":{"pos_in_scrolling_layout":[1,1],"tile_size":[1.0,1.0],"window_size":[1,1],"tile_pos_in_workspace_view":[0.0,0.0],"window_offset_in_tile":[0.0,0.0]},
	  "focus_timestamp":null}]`
)

// fakeNiri serves niri's socket: replies by request name, actions
// recorded, an event stream fed by emit.
type fakeNiri struct {
	socket  string
	mu      sync.Mutex
	replies map[string]string
	actions []string
	streams []net.Conn
	opened  chan struct{}
	ln      net.Listener
}

func newFakeNiri(t *testing.T) *fakeNiri {
	t.Helper()
	f := &fakeNiri{
		socket: filepath.Join(sockdir.New(t), "niri.sock"),
		replies: map[string]string{
			"Outputs": outputsJSON, "Workspaces": workspacesJSON, "Windows": windowsJSON, "Version": `"26.04"`,
		},
		opened: make(chan struct{}, 16),
	}
	var err error
	if f.ln, err = net.Listen("unix", f.socket); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.ln.Close(); f.drop() })
	go f.serve()
	return f
}

func (f *fakeNiri) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.handle(conn)
	}
}

func (f *fakeNiri) handle(conn net.Conn) {
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		conn.Close()
		return
	}
	line = strings.TrimSpace(line)
	var name string
	if json.Unmarshal([]byte(line), &name) == nil {
		if name == "EventStream" {
			f.mu.Lock()
			f.streams = append(f.streams, conn)
			f.mu.Unlock()
			f.emitTo(conn, `{"Ok":"Handled"}`)
			f.emitTo(conn, `{"WorkspacesChanged":{"workspaces":`+workspacesJSON+`}}`, `{"WindowsChanged":{"windows":`+windowsJSON+`}}`)
			f.opened <- struct{}{}
			return
		}
		f.mu.Lock()
		r, ok := f.replies[name]
		f.mu.Unlock()
		if !ok {
			f.emitTo(conn, `{"Err":"unknown request"}`)
		} else {
			f.emitTo(conn, `{"Ok":{"`+name+`":`+r+`}}`)
		}
		conn.Close()
		return
	}
	var req struct{ Action map[string]json.RawMessage }
	if json.Unmarshal([]byte(line), &req) == nil && len(req.Action) == 1 {
		for a, data := range req.Action {
			f.mu.Lock()
			f.actions = append(f.actions, a+" "+string(data))
			f.mu.Unlock()
		}
		conn.Write([]byte(`{"Ok":"Handled"}` + "\n"))
	} else {
		conn.Write([]byte(`{"Err":"bad request"}` + "\n"))
	}
	conn.Close()
}

// emitTo writes each message on a line of its own, as niri does; the
// fixtures above span lines for reading.
func (f *fakeNiri) emitTo(c net.Conn, lines ...string) {
	for _, l := range lines {
		var buf bytes.Buffer
		if err := json.Compact(&buf, []byte(l)); err != nil {
			panic(err)
		}
		c.Write(append(buf.Bytes(), '\n'))
	}
}

func (f *fakeNiri) emit(lines ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.streams {
		f.emitTo(c, lines...)
	}
}

func (f *fakeNiri) drop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.streams {
		c.Close()
	}
	f.streams = nil
}

func (f *fakeNiri) did() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.actions)
}

type recorder struct {
	mu      sync.Mutex
	snaps   []compositor.Snapshot
	conn    []bool
	caps    int
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
func (r *recorder) OnCapabilities(compositor.Capabilities) {
	r.mu.Lock()
	r.caps++
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

func start(t *testing.T, f *fakeNiri) (*Backend, *recorder) {
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

func TestSnapshotFromState(t *testing.T) {
	f := newFakeNiri(t)
	snap, err := New(NewClient(f.socket)).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Outputs) != 2 || snap.Outputs[0].Name != "HDMI-A-1" || snap.Outputs[0].Transform != 1 || snap.Outputs[1].ActiveWorkspace != "1" {
		t.Fatalf("outputs: %+v", snap.Outputs)
	}
	if snap.Outputs[1].RefreshRate != 60 || snap.Outputs[1].Scale != 2 || !snap.Outputs[1].Focused {
		t.Fatalf("eDP-1: %+v", snap.Outputs[1])
	}
	if snap.FocusedOutput != "eDP-1" || snap.FocusedWindow != "10" {
		t.Fatalf("focus: %q %q", snap.FocusedOutput, snap.FocusedWindow)
	}
	// Workspaces by output then index; the unnamed ones named by index.
	var ids, names []string
	for _, w := range snap.Workspaces {
		ids, names = append(ids, w.ID), append(names, w.Name)
	}
	if !slices.Equal(ids, []string{"3", "1", "2"}) || !slices.Equal(names, []string{"1", "web", "2"}) {
		t.Fatalf("workspaces: %v %v", ids, names)
	}
	w := snap.Windows[0]
	if w.ID != "10" || w.AppID != "kitty" || w.PID != 1234 || w.Workspace != "1" || w.Output != "eDP-1" || !w.Focused {
		t.Fatalf("window: %+v", w)
	}
	if w.Geometry == nil || *w.Geometry != (compositor.Rect{X: 12, Y: 12, W: 636, H: 776}) {
		t.Fatalf("geometry: %+v", w.Geometry)
	}
	// Focus ranks from the timestamps: the browser was focused last.
	if snap.Windows[1].FocusRank != 1 || w.FocusRank != 2 || snap.Windows[2].FocusRank != 0 {
		t.Fatalf("ranks: %d %d %d", snap.Windows[1].FocusRank, w.FocusRank, snap.Windows[2].FocusRank)
	}
	if snap.Windows[1].Geometry != nil || !snap.Windows[1].Floating || !snap.Windows[1].Urgent {
		t.Fatalf("out of view: %+v", snap.Windows[1])
	}
	if snap.Windows[2].Title != "" || snap.Windows[2].Output != "HDMI-A-1" {
		t.Fatalf("nameless: %+v", snap.Windows[2])
	}
}

func TestBackendStreamAndActions(t *testing.T) {
	f := newFakeNiri(t)
	b, r := start(t, f)
	waitFor(t, r.updated, "initial snapshot")
	<-f.opened
	if len(r.last().Windows) != 3 {
		t.Fatalf("initial: %+v", r.last())
	}
	// A burst of events gives one snapshot.
	f.emit(`{"WindowOpenedOrChanged":{"window":{"id":13,"title":"New","app_id":"foot","pid":9,"workspace_id":2,"is_focused":true,"is_floating":false,"is_urgent":false,"layout":{"pos_in_scrolling_layout":[1,1],"tile_size":[1.0,1.0],"window_size":[1,1],"tile_pos_in_workspace_view":null,"window_offset_in_tile":[0.0,0.0]},"focus_timestamp":{"secs":300,"nanos":0}}}}`,
		`{"WindowFocusChanged":{"id":13}}`, `{"WorkspaceActivated":{"id":2,"focused":true}}`, `{"WindowClosed":{"id":12}}`)
	waitFor(t, r.updated, "snapshot after events")
	time.Sleep(50 * time.Millisecond)
	s := r.last()
	if len(s.Windows) != 3 || s.FocusedWindow != "13" || s.Outputs[1].ActiveWorkspace != "2" {
		t.Fatalf("after events: focused=%s outputs=%+v windows=%+v", s.FocusedWindow, s.Outputs, s.Windows)
	}
	if ws, _ := s.Workspace("1"); ws.Active || ws.Focused {
		t.Fatalf("old workspace still active: %+v", ws)
	}
	if n := len(r.snaps); n != 2 {
		t.Fatalf("%d snapshots, want 2", n)
	}

	ctx := context.Background()
	for _, do := range []func() error{
		func() error { return b.FocusWindow(ctx, "10") },
		func() error { return b.MoveWindow(ctx, "10", "2", false) },
		func() error { return b.FocusWorkspace(ctx, "2") },
		func() error { return b.MoveWorkspace(ctx, "2", "HDMI-A-1") },
		func() error { return b.FocusOutput(ctx, "HDMI-A-1") },
		func() error { return b.CloseWindow(ctx, "10") },
	} {
		if err := do(); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{
		`FocusWindow {"id":10}`, `MoveWindowToWorkspace {"focus":false,"reference":{"Id":2},"window_id":10}`,
		`FocusWorkspace {"reference":{"Id":2}}`, `MoveWorkspaceToMonitor {"output":"HDMI-A-1","reference":{"Id":2}}`,
		`FocusMonitor {"output":"HDMI-A-1"}`, `CloseWindow {"id":10}`,
	}
	if got := f.did(); !slices.Equal(got, want) {
		t.Fatalf("actions %q\nwant %q", got, want)
	}
	if err := b.FocusWindow(ctx, "0xaa"); err == nil {
		t.Fatal("a Hyprland address went through")
	}

	// The stream closes: down, then up again with the state re-read.
	f.drop()
	<-f.opened
	deadline := time.Now().Add(time.Second)
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
	if r.caps == 0 {
		t.Fatal("capabilities never reported")
	}
}

func TestNewWorkspace(t *testing.T) {
	b := New(NewClient(""))
	snap := compositor.Snapshot{
		Workspaces: []compositor.Workspace{{ID: "1", Order: 1, Output: "A"}, {ID: "2", Order: 2, Output: "A"}, {ID: "3", Order: 1, Output: "B"}},
		Windows:    []compositor.Window{{ID: "10", Workspace: "1"}},
	}
	if ws, ok := b.NewWorkspace(snap, "A", nil); !ok || ws.ID != "2" {
		t.Fatalf("trailing empty: %+v %v", ws, ok)
	}
	if _, ok := b.NewWorkspace(snap, "A", func(id string) bool { return id == "2" }); ok {
		t.Fatal("a reserved workspace was named again")
	}
	if ws, ok := b.NewWorkspace(snap, "B", nil); !ok || ws.ID != "3" {
		t.Fatalf("B: %+v %v", ws, ok)
	}
	t.Setenv("NIRI_SOCKET", "")
	if _, err := Open(); err != compositor.ErrNoCompositor {
		t.Fatalf("Open outside niri: %v", err)
	}
}
