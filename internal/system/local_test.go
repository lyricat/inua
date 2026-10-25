package system

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/dbustest"
)

func TestParseVolume(t *testing.T) {
	for in, want := range map[string]VolumeState{
		"Volume: 0.40\n":         {Available: true, Volume: 0.40},
		"Volume: 1.00 [MUTED]\n": {Available: true, Volume: 1, Muted: true},
		"Volume: 0.00":           {Available: true},
	} {
		got, err := parseVolume(in)
		if err != nil || got != want {
			t.Errorf("parseVolume(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	if _, err := parseVolume("Translate ID error: '@DEFAULT_AUDIO_SINK@'"); err == nil {
		t.Error("parseVolume accepted an error message")
	}
}

// The stream pw-dump -m writes: the full graph, then one array per change.
const pwStream = `[
  {"id": 0, "type": "PipeWire:Interface:Core", "info": {}},
  {"id": 49, "type": "PipeWire:Interface:Device", "info": {}},
  {"id": 67, "type": "PipeWire:Interface:Node", "info": {}},
  {"id": 30, "type": "PipeWire:Interface:Metadata", "metadata": []}
]
[ {"id": 87, "type": "PipeWire:Interface:Client", "info": {}} ]
[ {"id": 87, "info": null} ]
[ {"id": 67, "type": "PipeWire:Interface:Node", "info": {"params": {}}} ]
[ {"id": 88, "type": "PipeWire:Interface:Link", "info": {}} ]
[ {"id": 67, "info": null} ]
[ {"id": 99, "info": null} ]
`

func TestWatchDump(t *testing.T) {
	// One read for the initial graph, then one for each change to a node,
	// device or metadata. Clients come and go with every wpctl call, so
	// following them would loop.
	n := 0
	err := watchDump(strings.NewReader(pwStream), func() { n++ })
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 { // initial graph, node 67 changed, node 67 removed
		t.Fatalf("%d reads, want 3", n)
	}
}

func TestBacklight(t *testing.T) {
	root := t.TempDir()
	dev := filepath.Join(root, "amdgpu_bl1")
	os.MkdirAll(dev, 0o755)
	os.WriteFile(filepath.Join(dev, "max_brightness"), []byte("65535\n"), 0o644)
	os.WriteFile(filepath.Join(dev, "brightness"), []byte("4096\n"), 0o644)

	addr := dbustest.Start(t)
	logind := &fakeLogind{}
	conn := service(t, addr, "org.freedesktop.login1")
	if err := conn.Export(logind, "/org/freedesktop/login1/session/auto", "org.freedesktop.login1.Session"); err != nil {
		t.Fatal(err)
	}

	b, err := FindBacklight(root, dialer(addr))
	if err != nil {
		t.Fatal(err)
	}
	// Perceived brightness, as brightnessctl -e4 (the keybinds) computes it.
	p, err := b.Percent()
	if err != nil {
		t.Fatal(err)
	}
	if want := 100 * math.Pow(4096.0/65535, 0.25); math.Abs(p-want) > 0.01 {
		t.Fatalf("percent %.2f, want %.2f", p, want)
	}

	for _, c := range []struct {
		percent float64
		raw     uint32
	}{{50, 4096}, {100, 65535}, {0, 1}, {150, 65535}} {
		if err := b.SetPercent(c.percent); err != nil {
			t.Fatal(err)
		}
		logind.mu.Lock()
		got := logind.last
		logind.mu.Unlock()
		if got != [3]any{"backlight", "amdgpu_bl1", c.raw} {
			t.Errorf("SetPercent(%v) sent %v, want raw %d", c.percent, got, c.raw)
		}
	}
}

func TestNoBacklight(t *testing.T) {
	if _, err := FindBacklight(t.TempDir(), nil); err == nil {
		t.Fatal("found a backlight in an empty directory")
	}
}

type fakeLogind struct {
	mu   sync.Mutex
	last [3]any
}

func (f *fakeLogind) SetBrightness(subsystem, name string, value uint32) *dbus.Error {
	f.mu.Lock()
	f.last = [3]any{subsystem, name, value}
	f.mu.Unlock()
	return nil
}

func TestParseMakoModes(t *testing.T) {
	if !hasMode("default\ndo-not-disturb\n", "do-not-disturb") {
		t.Error("mode not found")
	}
	if hasMode("default\n", "do-not-disturb") {
		t.Error("mode found when absent")
	}
}
