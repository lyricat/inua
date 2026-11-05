package hypr

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/compositor"
)

const (
	monitorsJSON = `[
	 {"id":1,"name":"HDMI-A-1","description":"right","x":1920,"y":0,"width":1920,"height":1080,"scale":1,
	  "focused":false,"activeWorkspace":{"id":3,"name":"3"},"specialWorkspace":{"id":0,"name":""}},
	 {"id":0,"name":"eDP-1","description":"laptop","x":0,"y":0,"width":1920,"height":1080,"scale":1.25,
	  "focused":true,"activeWorkspace":{"id":1,"name":"1"},"specialWorkspace":{"id":0,"name":""}}]`
	workspacesJSON = `[
	 {"id":3,"name":"3","monitor":"HDMI-A-1","hasfullscreen":true},
	 {"id":1,"name":"1","monitor":"eDP-1","hasfullscreen":false},
	 {"id":2,"name":"2","monitor":"eDP-1","hasfullscreen":false}]`
	clientsJSON = `[
	 {"address":"0xAA","pid":12345,"mapped":true,"title":"Terminal, 1","class":"kitty","monitor":0,"workspace":{"id":1,"name":"1"},"at":[10,20],"size":[800,600],"floating":true,"focusHistoryID":0},
	 {"address":"0xbb","mapped":true,"title":"Browser","class":"firefox","monitor":1,"workspace":{"id":3,"name":"3"},"focusHistoryID":-1},
	 {"address":"0xcc","mapped":false,"title":"hidden","class":"x","monitor":0,"workspace":{"id":2,"name":"2"}}]`
)

func TestParseEvent(t *testing.T) {
	ev, ok := ParseEvent("windowtitlev2>>55aa,title, with>>commas\n")
	if !ok || ev.Name != "windowtitlev2" || ev.Data != "55aa,title, with>>commas" {
		t.Fatalf("got %+v %v", ev, ok)
	}
	if args := ev.Args(2); len(args) != 2 || args[1] != "title, with>>commas" {
		t.Fatalf("args %q", args)
	}
	for _, bad := range []string{"", "noseparator", ">>data"} {
		if _, ok := ParseEvent(bad); ok {
			t.Errorf("ParseEvent(%q) accepted", bad)
		}
	}
}

func TestNormalizeAddress(t *testing.T) {
	for in, want := range map[string]string{"55D1": "0x55d1", "0x55d1": "0x55d1", "0X55D1": "0x55d1", "": ""} {
		if got := NormalizeAddress(in); got != want {
			t.Errorf("NormalizeAddress(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildSnapshot(t *testing.T) {
	s, err := BuildSnapshot(QueryResult{
		Monitors:     []byte(monitorsJSON),
		Workspaces:   []byte(workspacesJSON),
		Clients:      []byte(clientsJSON),
		ActiveWindow: []byte(`{"address":"0xaa"}`),
	}, map[string]bool{"0xbb": true})
	if err != nil {
		t.Fatal(err)
	}
	if s.FocusedOutput != "eDP-1" || s.FocusedWindow != "0xaa" {
		t.Fatalf("focus: %q %q", s.FocusedOutput, s.FocusedWindow)
	}
	if len(s.Outputs) != 2 || s.Outputs[0].Name != "eDP-1" || s.Outputs[0].ActiveWorkspace != "1" {
		t.Fatalf("outputs not in Hyprland's order: %+v", s.Outputs)
	}
	if len(s.Windows) != 2 {
		t.Fatalf("unmapped client kept: %+v", s.Windows)
	}
	w := s.Windows[0]
	if w.ID != "0xaa" || w.PID != 12345 || !w.Focused || w.Output != "eDP-1" || w.AppID != "kitty" || w.Workspace != "1" {
		t.Fatalf("window: %+v", w)
	}
	if w.FocusRank != 1 || s.Windows[1].FocusRank != 0 || w.Geometry == nil || *w.Geometry != (Rect{X: 10, Y: 20, W: 800, H: 600}) || !w.Floating {
		t.Fatalf("typed fields: %+v %+v", w, w.Geometry)
	}
	if !s.Windows[1].Urgent || s.Windows[0].Urgent {
		t.Fatalf("urgent flags wrong")
	}
	want := []struct {
		id                      string
		active, focused, urgent bool
	}{{"1", true, true, false}, {"2", false, false, false}, {"3", true, false, true}}
	for i, w := range want {
		ws := s.Workspaces[i]
		if ws.ID != w.id || ws.Active != w.active || ws.Focused != w.focused || ws.Urgent != w.urgent {
			t.Errorf("workspace %d: %+v, want %+v", i, ws, w)
		}
	}
	if !s.Workspaces[2].HasFullscreen || s.Workspaces[2].Order != 3 {
		t.Error("hasfullscreen or order lost")
	}
}

func TestBuildSnapshotKeepsDisabledMonitorWithWorkspace(t *testing.T) {
	s, err := BuildSnapshot(QueryResult{
		Monitors: []byte(`[
			{"id":0,"name":"HDMI-A-1","width":1280,"height":1024,"scale":1,"focused":true,"disabled":true,"activeWorkspace":{"id":0}},
			{"id":1,"name":"HDMI-A-2","width":3840,"height":2160,"scale":2,"disabled":true,"activeWorkspace":{"id":0}},
			{"id":2,"name":"disconnected","disabled":true,"activeWorkspace":{"id":0}}
		]`),
		Workspaces: []byte(`[
			{"id":1,"name":"1","monitor":"HDMI-A-1"},
			{"id":2,"name":"2","monitor":"HDMI-A-2"}
		]`),
		Clients: []byte(clientsJSON), ActiveWindow: []byte(`{"address":"0xaa"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Outputs) != 2 || s.Outputs[0].Name != "HDMI-A-1" || s.Outputs[1].Width != 3840 {
		t.Fatalf("workspace-linked monitors not retained: %+v", s.Outputs)
	}
	if s.FocusedOutput != "HDMI-A-1" {
		t.Fatalf("focused monitor = %q", s.FocusedOutput)
	}
}
func TestBuildSnapshotNoActiveWindow(t *testing.T) {
	s, err := BuildSnapshot(QueryResult{
		Monitors: []byte(`[]`), Workspaces: []byte(`[]`), Clients: []byte(`[]`), ActiveWindow: []byte(`{}`),
	}, nil)
	if err != nil || s.FocusedWindow != "" {
		t.Fatalf("%+v %v", s, err)
	}
}

func TestDispatch(t *testing.T) {
	f := newFakeHyprland(t)
	c := NewClient(f.paths)
	if err := c.Dispatch(context.Background(), "focuswindow address:0xaa"); err != nil {
		t.Fatal(err)
	}
	if len(f.dispatched) != 1 || f.dispatched[0] != "focuswindow address:0xaa" {
		t.Fatalf("dispatched %q", f.dispatched)
	}
}

func TestPlaceWindow(t *testing.T) {
	f := newFakeHyprland(t)
	if err := NewClient(f.paths).PlaceWindow(context.Background(), 42, Rect{X: 100, Y: 80, W: 460, H: 700}); err != nil {
		t.Fatal(err)
	}
	want := []string{"resizewindowpixel exact 460 700,pid:42", "movewindowpixel exact 100 80,pid:42"}
	if strings.Join(f.dispatched, "|") != strings.Join(want, "|") {
		t.Fatalf("dispatched %q", f.dispatched)
	}
}

// recorder is a Handler that records calls.
type recorder struct {
	mu      sync.Mutex
	snaps   []compositor.Snapshot
	events  []Event
	conn    []bool
	updated chan struct{}
}

func newRecorder() *recorder { return &recorder{updated: make(chan struct{}, 64)} }

func (r *recorder) OnSnapshot(s compositor.Snapshot) {
	r.mu.Lock()
	r.snaps = append(r.snaps, s)
	r.mu.Unlock()
	r.updated <- struct{}{}
}
func (r *recorder) OnEvent(e Event) { r.mu.Lock(); r.events = append(r.events, e); r.mu.Unlock() }
func (r *recorder) OnConnection(up bool, _ error) {
	r.mu.Lock()
	r.conn = append(r.conn, up)
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

func startStore(t *testing.T, f *fakeHyprland, r *recorder) *Store {
	t.Helper()
	s := NewStore(NewClient(f.paths), r)
	s.Debounce = 20 * time.Millisecond
	s.MinBackoff = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return s
}

func TestStoreInitialSnapshotAndEvents(t *testing.T) {
	f := newFakeHyprland(t)
	f.set("j/monitors", monitorsJSON)
	f.set("j/workspaces", workspacesJSON)
	f.set("j/clients", clientsJSON)
	r := newRecorder()
	startStore(t, f, r)

	waitFor(t, r.updated, "initial snapshot")
	if got := len(r.last().Windows); got != 2 {
		t.Fatalf("initial toplevels = %d", got)
	}
	<-f.connected

	// A burst of events results in one merged refresh.
	before := f.queryCount()
	f.set("j/clients", `[{"address":"0xaa","mapped":true,"title":"renamed","class":"kitty","monitor":0,"workspace":{"id":1}}]`)
	f.emit("windowtitlev2>>aa,renamed", "activewindowv2>>aa", "openlayer>>inua", "closewindow>>bb")
	waitFor(t, r.updated, "refresh after events")
	time.Sleep(60 * time.Millisecond)
	if n := f.queryCount() - before; n != 1 {
		t.Errorf("burst caused %d refreshes, want 1", n)
	}
	s := r.last()
	if len(s.Windows) != 1 || s.Windows[0].Title != "renamed" {
		t.Fatalf("after events: %+v", s.Windows)
	}
	r.mu.Lock()
	nev := len(r.events)
	r.mu.Unlock()
	if nev != 4 {
		t.Errorf("forwarded %d raw events, want 4", nev)
	}
}

func TestStoreIgnoresNonStateEvents(t *testing.T) {
	f := newFakeHyprland(t)
	r := newRecorder()
	startStore(t, f, r)
	<-f.connected
	time.Sleep(30 * time.Millisecond)
	before := f.queryCount()
	f.emit("openlayer>>inua", "closelayer>>inua", "activelayout>>kb,us")
	time.Sleep(80 * time.Millisecond)
	if n := f.queryCount() - before; n != 0 {
		t.Errorf("non-state events caused %d refreshes", n)
	}
}

func TestStoreUrgentTracking(t *testing.T) {
	f := newFakeHyprland(t)
	f.set("j/monitors", monitorsJSON)
	f.set("j/workspaces", workspacesJSON)
	f.set("j/clients", clientsJSON)
	r := newRecorder()
	startStore(t, f, r)
	waitFor(t, r.updated, "initial snapshot")
	<-f.connected

	f.emit("urgent>>bb")
	waitFor(t, r.updated, "urgent")
	if !r.last().Windows[1].Urgent {
		t.Fatal("urgent not applied")
	}
	f.emit("activewindowv2>>bb")
	waitFor(t, r.updated, "urgent cleared")
	if r.last().Windows[1].Urgent {
		t.Fatal("urgent not cleared on activation")
	}
}

func TestStoreReconnectRebuildsState(t *testing.T) {
	f := newFakeHyprland(t)
	r := newRecorder()
	startStore(t, f, r)
	<-f.connected

	// While disconnected the state changes and no event will ever describe it.
	f.set("j/clients", clientsJSON)
	f.dropEventConns()
	<-f.connected
	waitFor(t, r.updated, "snapshot after reconnect")
	if len(r.last().Windows) != 2 {
		t.Fatalf("state not re-queried after reconnect: %+v", r.last())
	}
	// The snapshot is published before the connection is reported up.
	deadline := time.Now().Add(time.Second)
	for {
		r.mu.Lock()
		conn := append([]bool(nil), r.conn...)
		r.mu.Unlock()
		if len(conn) >= 3 {
			if !conn[0] || conn[1] || !conn[2] {
				t.Errorf("connection reports %v, want [true false true ...]", conn)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("connection reports %v, want [true false true ...]", conn)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestStoreManualRefresh(t *testing.T) {
	f := newFakeHyprland(t)
	r := newRecorder()
	s := startStore(t, f, r)
	<-f.connected
	time.Sleep(20 * time.Millisecond)
	f.set("j/clients", clientsJSON)
	s.Refresh()
	waitFor(t, r.updated, "manual refresh")
	if len(r.last().Windows) != 2 {
		t.Fatal("manual refresh did not query")
	}
}

// The room to a window's right: to its monitor's edge in layout pixels
// (scaled, turned), less what is reserved there and the outer gap.
func TestRoomRight(t *testing.T) {
	clients := `[{"pid":7,"mapped":true,"at":[2266,200],"size":[860,620],"monitor":0},
		{"pid":8,"mapped":true,"at":[3000,0],"size":[100,100],"monitor":1},
		{"pid":9,"mapped":true,"at":[3500,0],"size":[600,100],"monitor":0}]`
	monitors := `[{"id":0,"x":2256,"width":3840,"height":2160,"scale":1.5,"transform":0,"reserved":[0,36,0,0]},
		{"id":1,"x":2256,"width":1080,"height":1920,"scale":1,"transform":1,"reserved":[0,0,20,0]}]`
	r, room, err := roomRight([]byte(clients), []byte(monitors), 8, 7)
	// 3840/1.5 = 2560: the edge at 2256+2560-8 = 4808; the window ends at 3126.
	if err != nil || r != (Rect{X: 2266, Y: 200, W: 860, H: 620}) || room != 1682 {
		t.Errorf("%+v %d %v", r, room, err)
	}
	// Turned: 1920 wide, less 20 reserved and the gap; it ends at 3100.
	if _, room, _ := roomRight([]byte(clients), []byte(monitors), 8, 8); room != 2256+1920-20-8-3100 {
		t.Errorf("turned: %d", room)
	}
	// Nearer the edge: what is left, 4808 less where it ends.
	if _, room, _ := roomRight([]byte(clients), []byte(monitors), 8, 9); room != 4808-4100 {
		t.Errorf("near the edge: %d", room)
	}
	if _, _, err := roomRight([]byte(clients), []byte(monitors), 8, 1); err == nil {
		t.Error("no window")
	}
	for in, want := range map[string]int{`{"int":10}`: 10, `{"custom":"5 12 5 5"}`: 12, `{"custom":"6"}`: 6, `nope`: 0} {
		if got := gapsOut([]byte(in)); got != want {
			t.Errorf("gaps %s: %d", in, got)
		}
	}
}
