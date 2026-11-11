package shell

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/system"
)

func twoMonitors() compositor.Snapshot {
	return compositor.Snapshot{
		Outputs: []compositor.Output{
			{Name: "eDP-1", Focused: true, ActiveWorkspace: "1"},
			{Name: "HDMI-A-1", ActiveWorkspace: "4"},
		},
		Workspaces: []compositor.Workspace{
			{ID: "4", Order: 4, Output: "HDMI-A-1"},
			{ID: "1", Order: 1, Output: "eDP-1"},
			{ID: "2", Order: 2, Output: "eDP-1"},
		},
		Windows: []compositor.Window{
			win("0x1", "foot", "shell", 1, 0),
			win("0x2", "mpv", "old video", 4, 3),
			win("0x3", "firefox", "docs", 4, 1),
			win("0x4", "code", "editor", 2, 2),
		},
		FocusedWindow: "0x1",
		FocusedOutput: "eDP-1",
	}
}

func ids(ws []compositor.Workspace) []string {
	var out []string
	for _, w := range ws {
		out = append(out, w.ID)
	}
	return out
}

func TestBarWorkspaces(t *testing.T) {
	snap := twoMonitors()
	for _, c := range []struct {
		connector string
		all       bool
		want      []string
	}{
		{"eDP-1", false, []string{"1", "2"}},
		{"HDMI-A-1", false, []string{"4"}},
		{"HDMI-A-1", true, []string{"1", "2", "4"}},
		{"WL-1", false, []string{"1", "2", "4"}}, // unknown to the compositor: everything
	} {
		if got := ids(BarWorkspaces(snap, c.connector, c.all)); !slices.Equal(got, c.want) {
			t.Errorf("%s all=%v: %v, want %v", c.connector, c.all, got, c.want)
		}
	}
}

func TestBarWindow(t *testing.T) {
	snap := twoMonitors()
	for _, tc := range []struct {
		connector string
		wantID    string
	}{
		{"eDP-1", "0x1"},
		{"HDMI-A-1", "0x3"},
		{"WL-1", "0x1"},
	} {
		got, ok := BarWindow(snap, tc.connector)
		if !ok || got.ID != tc.wantID {
			t.Errorf("%s: window=%+v, ok=%v; want %q", tc.connector, got, ok, tc.wantID)
		}
	}
	// Without a focused window the bar keeps the last focused one of its
	// monitor's visible workspace.
	snap.FocusedWindow = "missing"
	if got, ok := BarWindow(snap, "eDP-1"); !ok || got.ID != "0x1" {
		t.Errorf("missing focused window: %+v, %v; want the monitor's last window", got, ok)
	}
	snap.FocusedWindow = "0x1"
	snap.Outputs[1].ActiveWorkspace = "missing"
	if got, ok := BarWindow(snap, "HDMI-A-1"); ok || got.ID != "" {
		t.Errorf("missing active workspace: %+v, %v", got, ok)
	}
}

func TestBarTitle(t *testing.T) {
	snap := twoMonitors()
	for _, c := range []struct{ connector, want string }{
		{"eDP-1", "shell"},
		{"HDMI-A-1", "docs"}, // last focused of the two on workspace 4
		{"WL-1", "shell"},
	} {
		if got := BarTitle(snap, c.connector); got != c.want {
			t.Errorf("%s: %q, want %q", c.connector, got, c.want)
		}
	}
	snap.FocusedWindow = ""
	if got := BarTitle(snap, "eDP-1"); got != "shell" {
		t.Errorf("nothing focused: %q, want the monitor's last window", got)
	}
}

// Moving the focus from one monitor to the other leaves each bar on its
// own monitor's window, even when a snapshot pairs the new focused output
// with the window focused before.
func TestBarTitleWhileFocusMoves(t *testing.T) {
	for _, c := range []struct {
		name          string
		focusedOutput string
		focusedWindow string
	}{
		{"focused on eDP-1", "eDP-1", "0x1"},
		{"moving to HDMI-A-1, active window still eDP-1's", "HDMI-A-1", "0x1"},
		{"focused on HDMI-A-1", "HDMI-A-1", "0x3"},
		{"moving back, active window still HDMI-A-1's", "eDP-1", "0x3"},
	} {
		snap := twoMonitors()
		snap.FocusedOutput, snap.FocusedWindow = c.focusedOutput, c.focusedWindow
		for connector, want := range map[string]string{"eDP-1": "shell", "HDMI-A-1": "docs"} {
			if got := BarTitle(snap, connector); got != want {
				t.Errorf("%s: %s = %q, want %q", c.name, connector, got, want)
			}
		}
	}
}

func TestStatusIcons(t *testing.T) {
	for _, c := range []struct {
		got, want string
	}{
		{NetIcon(system.NetworkState{}), "network-offline-symbolic"},
		{NetIcon(system.NetworkState{Available: true, Kind: "wifi", Strength: 60}), "network-wireless-signal-good-symbolic"},
		{NetIcon(system.NetworkState{Available: true, Kind: "ethernet"}), "network-wired-symbolic"},
		{NetIcon(system.NetworkState{Available: true, Kind: "vpn"}), "network-vpn-symbolic"},
		{NetIcon(system.NetworkState{Available: true, WifiDevice: true}), "network-wireless-disabled-symbolic"},
		{NetIcon(system.NetworkState{Available: true, WifiDevice: true, WifiEnabled: true}), "network-wireless-offline-symbolic"},
		{VolumeIcon(system.AudioState{Available: true, Volume: 0.5}), "audio-volume-medium-symbolic"},
		{VolumeIcon(system.AudioState{Available: true, Volume: 0.9, Muted: true}), "audio-volume-muted-symbolic"},
		{BatteryIcon(system.BatteryState{Percent: 47}), "battery-level-50-symbolic"},
		{BatteryIcon(system.BatteryState{Percent: 3, Charging: true}), "battery-level-0-charging-symbolic"},
		{BatteryIcon(system.BatteryState{Percent: 104}), "battery-level-100-symbolic"},
		{BatteryIcon(system.BatteryState{Full: true}), "battery-level-100-charged-symbolic"},
		{BatteryText(system.BatteryState{Percent: 81.6, Charging: true}), "82% · Charging"},
		{Percent(0.644), "64%"},
		{Percent(1.3), "100%"},
		{VolumeText(system.AudioState{Available: true, Volume: 0.5}), "50%"},
		{VolumeText(system.AudioState{Available: true, Volume: 0.5, Muted: true}), "muted"},
		{VolumeText(system.AudioState{}), "—"},
		{SaverTile(system.PowerProfileState{Available: true, Active: "performance"}), "Performance"},
		{SaverTarget(system.PowerProfileState{Active: "power-saver"}), "balanced"},
		{SaverTarget(system.PowerProfileState{Active: "performance"}), "power-saver"},
		{BluetoothTile(system.BluetoothState{Available: true, Powered: true, Connected: []string{"a", "b"}}), "2 devices"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
	for _, c := range []struct {
		st      system.TailscaleState
		sub     string
		on, tog bool
	}{
		{system.TailscaleState{Installed: true}, "Not running", false, false},
		{system.TailscaleState{Running: true, Backend: "Running", Online: 3}, "3 online", true, true},
		{system.TailscaleState{Running: true, Backend: "Running", ExitNode: "pi"}, "via pi", true, true},
		{system.TailscaleState{Running: true, Backend: "Stopped"}, "Off", false, true},
		{system.TailscaleState{Running: true, Backend: "NeedsLogin"}, "Logged out", false, false},
	} {
		if sub, on, tog := TailscaleTile(c.st); sub != c.sub || on != c.on || tog != c.tog {
			t.Errorf("TailscaleTile(%+v) = %q %v %v", c.st, sub, on, tog)
		}
	}
	sub, icon := WifiTile(system.NetworkState{Available: true, WifiDevice: true, WifiEnabled: true, Kind: "ethernet", Name: "Wired"})
	if sub != "Wired" || icon != "network-wireless-signal-none-symbolic" {
		t.Errorf("wifi tile on ethernet: %q %q", sub, icon)
	}
	title, artist := MediaText(system.MediaState{Identity: "Spotify"})
	if title != "Spotify" || artist != "" {
		t.Errorf("media without metadata: %q %q", title, artist)
	}
	title, artist = MediaText(system.MediaState{Identity: "mpv", Title: "clip.mkv"})
	if title != "clip.mkv" || artist != "mpv" {
		t.Errorf("media without artist: %q %q", title, artist)
	}
}

func TestSetterKeepsLatest(t *testing.T) {
	var mu sync.Mutex
	var applied []float64
	release := make(chan struct{})
	s := NewSetter(func(v float64) {
		if v == 1 {
			<-release // the first call is slow
		}
		mu.Lock()
		applied = append(applied, v)
		mu.Unlock()
	})
	s.Set(1)
	for v := 2.0; v <= 10; v++ {
		s.Set(v) // all queued behind the slow call; only 10 should run
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		got := slices.Clone(applied)
		mu.Unlock()
		if len(got) == 2 {
			if !slices.Equal(got, []float64{1, 10}) {
				t.Fatalf("applied %v, want [1 10]", got)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("applied %v", got)
		}
		time.Sleep(5 * time.Millisecond)
	}
	s.Set(3) // idle again: runs right away
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(applied, []float64{1, 10, 3}) {
		t.Errorf("applied %v", applied)
	}
}

func TestBarReach(t *testing.T) {
	// The bar's end, as measured: the tray, then clipboard,
	// notifications, status, 6 px apart, each 24 px tall at y 8 in a 36 px
	// window; the clock before them.
	spots := []BarSpot{
		{Rect: Rect{600, 8, 120, 24}, Break: true}, // the clock
		{Rect: Rect{0, 0, 0, 0}, Break: true},      // a space
		{Rect: Rect{1000, 8, 30, 24}},              // the tray
		{Rect: Rect{1036, 8, 34, 24}, Reach: true},
		{Rect: Rect{1076, 8, 34, 24}, Reach: true},
		{Rect: Rect{1116, 8, 150, 24}, Reach: true},
	}
	cases := []struct {
		x, y float64
		i    int
		on   bool
	}{
		{1050, 0, 3, false},   // above the clipboard, at the screen's edge
		{1050, 20, 3, true},   // on it
		{1033, 20, 3, false},  // half the gap before the first
		{1032, 20, -1, false}, // further: nothing
		{1072.9, 2, 3, false}, // the gap's first half: the clipboard's
		{1073, 2, 4, false},   // its second half: the notifications'
		{1279, 35, 5, false},  // the screen's corner: the status button's
		{1200, 20, 5, true},
		{650, 20, -1, false}, // the clock is not a reach
	}
	for _, c := range cases {
		if i, on := BarReach(spots, 6, c.x, c.y); i != c.i || on != c.on {
			t.Errorf("BarReach(%v, %v) = %d, %v; want %d, %v", c.x, c.y, i, on, c.i, c.on)
		}
	}
	if i, _ := BarReach(nil, 6, 10, 10); i != -1 {
		t.Errorf("no buttons: %d", i)
	}
}

// Buttons moved elsewhere on the bar reach only across the gaps between
// them: a space or the clock between two keeps their reaches apart, and
// one first on the bar reaches the screen's left corner, not its right.
func TestBarReachRearranged(t *testing.T) {
	spots := []BarSpot{
		{Rect: Rect{4, 8, 150, 24}, Reach: true},   // the control center, first
		{Rect: Rect{160, 8, 34, 24}, Reach: true},  // notifications beside it
		{Rect: Rect{200, 8, 80, 24}, Break: true},  // the workspaces
		{Rect: Rect{0, 0, 0, 0}, Break: true},      // a space
		{Rect: Rect{900, 8, 34, 24}, Reach: true},  // the clipboard
		{Rect: Rect{0, 0, 0, 0}, Break: true},      // a space
		{Rect: Rect{1200, 8, 76, 24}, Break: true}, // the clock, last
	}
	cases := []struct {
		x  float64
		i  int
		on bool
	}{
		{0, 0, false},      // the screen's left corner: the control center
		{-5, 0, false},     // and past it
		{100, 0, true},     // on it
		{156.9, 0, false},  // the first half of the gap to notifications
		{157, 1, false},    // the second
		{196.9, 1, false},  // half the gap past notifications
		{197, -1, false},   // the workspaces break the reaches
		{500, -1, false},   // across the space: nothing
		{896.9, -1, false}, // just before half the gap before the clipboard
		{897, 4, false},
		{936.9, 4, false},
		{937, -1, false},
		{1279, -1, false}, // the right corner is the clock's, no button's
	}
	for _, c := range cases {
		if i, on := BarReach(spots, 6, c.x, 20); i != c.i || on != c.on {
			t.Errorf("BarReach(%v) = %d, %v; want %d, %v", c.x, i, on, c.i, c.on)
		}
	}
	// Alone on an otherwise empty bar, a button reaches both corners.
	if i, _ := BarReach([]BarSpot{{Rect: Rect{600, 8, 34, 24}, Reach: true}}, 6, 1279, 0); i != 0 {
		t.Errorf("alone, right corner: %d", i)
	}
	if i, _ := BarReach([]BarSpot{{Rect: Rect{600, 8, 34, 24}, Reach: true}}, 6, 0, 0); i != 0 {
		t.Errorf("alone, left corner: %d", i)
	}
}

func TestEmptyDesktop(t *testing.T) {
	empty := func() compositor.Snapshot {
		snap := twoMonitors()
		snap.Outputs[0].ActiveWorkspace = "3"
		snap.Workspaces = append(snap.Workspaces, compositor.Workspace{ID: "3", Order: 3, Output: "eDP-1"})
		snap.FocusedWindow = ""
		return snap
	}
	for _, tc := range []struct {
		name      string
		change    func(*compositor.Snapshot)
		connector string
		want      bool
	}{
		{"empty and focused", func(*compositor.Snapshot) {}, "eDP-1", true},
		{"another monitor's bar", func(*compositor.Snapshot) {}, "HDMI-A-1", false},
		{"monitor unknown to the compositor", func(*compositor.Snapshot) {}, "WL-1", false},
		{"no focused output", func(s *compositor.Snapshot) { s.FocusedOutput = "" }, "eDP-1", false},
		{"no active workspace", func(s *compositor.Snapshot) { s.Outputs[0].ActiveWorkspace = "" }, "eDP-1", false},
		{"a window without a process", func(s *compositor.Snapshot) {
			s.Windows = append(s.Windows, compositor.Window{ID: "0x9", Workspace: "3"})
		}, "eDP-1", false},
		{"a focused window without a process", func(s *compositor.Snapshot) {
			s.Windows = append(s.Windows, compositor.Window{ID: "0x9", Workspace: "3"})
			s.FocusedWindow = "0x9"
		}, "eDP-1", false},
		{"a window on the special workspace shown over it", func(s *compositor.Snapshot) {
			s.Outputs[0].SpecialWorkspace = "-98"
			s.Windows = append(s.Windows, compositor.Window{ID: "0x9", PID: 9, Workspace: "-98"})
		}, "eDP-1", false},
		{"an empty special workspace shown over it", func(s *compositor.Snapshot) {
			s.Outputs[0].SpecialWorkspace = "-98"
		}, "eDP-1", true},
		{"a window on this monitor with no workspace", func(s *compositor.Snapshot) {
			s.Windows = append(s.Windows, compositor.Window{ID: "0x9", PID: 9, Output: "eDP-1"})
		}, "eDP-1", false},
		{"a window nowhere known", func(s *compositor.Snapshot) {
			s.Windows = append(s.Windows, compositor.Window{ID: "0x9", PID: 9})
		}, "eDP-1", false},
		{"a focused window not listed yet", func(s *compositor.Snapshot) { s.FocusedWindow = "0x9" }, "eDP-1", false},
		{"the focus not yet moved from another monitor", func(s *compositor.Snapshot) { s.FocusedWindow = "0x3" }, "eDP-1", true},
		{"windows on other workspaces only", func(s *compositor.Snapshot) {}, "eDP-1", true},
	} {
		snap := empty()
		tc.change(&snap)
		if got := EmptyDesktop(snap, tc.connector); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	if EmptyDesktop(twoMonitors(), "eDP-1") {
		t.Error("a monitor with its focused window: empty")
	}
}
