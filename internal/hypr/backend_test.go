package hypr

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/compositor"
)

type capRecorder struct {
	recorder
	cmu  sync.Mutex
	caps []compositor.Capabilities
}

func (c *capRecorder) OnCapabilities(cs compositor.Capabilities) {
	c.cmu.Lock()
	c.caps = append(c.caps, cs)
	c.cmu.Unlock()
}

func (c *capRecorder) reported() int {
	c.cmu.Lock()
	defer c.cmu.Unlock()
	return len(c.caps)
}

func startBackend(t *testing.T, f *fakeHyprland) (*Backend, *capRecorder) {
	t.Helper()
	b := New(NewClient(f.paths))
	r := &capRecorder{recorder: *newRecorder()}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { b.Run(ctx, r); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return b, r
}

// The backend probes Lua at each connection and reports the capabilities
// before the connection; the actions take the Lua or the classic form.
func TestBackendProbesLuaAndDispatches(t *testing.T) {
	f := newFakeHyprland(t)
	f.set("j/clients", clientsJSON)
	b, r := startBackend(t, f)
	waitFor(t, r.updated, "initial snapshot")
	<-f.connected
	deadline := time.Now().Add(time.Second)
	for b.Lua() || r.reported() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("capabilities not reported")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if b.Lua() || b.Capabilities()[compositor.CapKeyObserver] != compositor.Unsupported {
		t.Fatalf("0.53: lua=%v caps=%v", b.Lua(), b.Capabilities())
	}
	ctx := context.Background()
	if err := b.FocusWindow(ctx, "0xaa"); err != nil {
		t.Fatal(err)
	}
	if err := b.MoveWindow(ctx, "0xaa", "3", false); err != nil {
		t.Fatal(err)
	}
	if err := b.SendKey(ctx, "0xaa", "CTRL SHIFT", "V"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	got := slices.Clone(f.dispatched)
	f.mu.Unlock()
	want := []string{"focuswindow address:0xaa", "movetoworkspacesilent 3,address:0xaa", "focuswindow address:0xaa", "sendshortcut CTRL SHIFT, V, address:0xaa"}
	if !slices.Equal(got, want) {
		t.Fatalf("classic dispatches %q, want %q", got, want)
	}

	// Hyprland restarted as 0.55: Lua is there now.
	f.set("eval _G.inuaOverviewProbe=true", "ok")
	f.dropEventConns()
	<-f.connected
	deadline = time.Now().Add(time.Second)
	for !b.Lua() {
		if time.Now().After(deadline) {
			t.Fatal("Lua not probed again")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !b.Capabilities().Has(compositor.CapKeyObserver) {
		t.Fatal("key observer not supported with Lua")
	}
	f.mu.Lock()
	f.dispatched = nil
	f.mu.Unlock()
	if err := b.FocusWorkspace(ctx, "2"); err != nil {
		t.Fatal(err)
	}
	if err := b.MoveWorkspace(ctx, "2", "HDMI-A-1"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	got = slices.Clone(f.dispatched)
	f.mu.Unlock()
	want = []string{"hl.dsp.focus({workspace=2})", `hl.dsp.workspace.move({workspace="2",monitor="HDMI-A-1"})`}
	if !slices.Equal(got, want) {
		t.Fatalf("lua dispatches %q, want %q", got, want)
	}
}

func TestBackendNewWorkspace(t *testing.T) {
	b := New(NewClient(Paths{}))
	b.FreshWorkspace = 11
	snap := compositor.Snapshot{Workspaces: []compositor.Workspace{{ID: "1", Order: 1, Active: true}}}
	if ws, ok := b.NewWorkspace(snap, "eDP-1", nil); !ok || ws.ID != "11" || ws.Output != "eDP-1" {
		t.Fatalf("%+v %v", ws, ok)
	}
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
	if _, err := Open(); err != compositor.ErrNoCompositor {
		t.Fatalf("Open outside Hyprland: %v", err)
	}
}
