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
	if got := BarTitle(snap, "eDP-1"); got != "" {
		t.Errorf("nothing focused: %q", got)
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
	// The bar's end, as measured: clipboard, notifications, status, 6 px
	// apart, each 24 px tall at y 8 in a 36 px window.
	buttons := []Rect{{1036, 8, 34, 24}, {1076, 8, 34, 24}, {1116, 8, 150, 24}}
	cases := []struct {
		x, y float64
		i    int
		on   bool
	}{
		{1050, 0, 0, false},   // above the clipboard, at the screen's edge
		{1050, 20, 0, true},   // on it
		{1033, 20, 0, false},  // half the gap before the first
		{1032, 20, -1, false}, // further: nothing
		{1072.9, 2, 0, false}, // the gap's first half: the clipboard's
		{1073, 2, 1, false},   // its second half: the notifications'
		{1279, 35, 2, false},  // the screen's corner: the status button's
		{1200, 20, 2, true},
	}
	for _, c := range cases {
		if i, on := BarReach(buttons, 6, c.x, c.y); i != c.i || on != c.on {
			t.Errorf("BarReach(%v, %v) = %d, %v; want %d, %v", c.x, c.y, i, on, c.i, c.on)
		}
	}
	if i, _ := BarReach(nil, 6, 10, 10); i != -1 {
		t.Errorf("no buttons: %d", i)
	}
}
