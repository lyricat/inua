package wayfire

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/wlroutput"
)

func mode(w, h, mHz int, preferred bool) wlroutput.Mode {
	return wlroutput.Mode{Width: w, Height: h, Refresh: mHz, Preferred: preferred}
}

// A 1440p display beside a 1080p one, as wlr-output-management lists them.
func twoHeads() []wlroutput.Head {
	return []wlroutput.Head{
		{Name: "DP-1", Description: "Dell U2720 (DP-1)", Make: "Dell", Model: "U2720", Enabled: true, Scale: 1,
			Modes:   []wlroutput.Mode{mode(2560, 1440, 59951, true), mode(2560, 1440, 143912, false), mode(1920, 1080, 60000, false)},
			Current: &wlroutput.Mode{Width: 2560, Height: 1440, Refresh: 59951}},
		{Name: "HDMI-A-1", Description: "LG TV (HDMI-A-1)", Enabled: true, Scale: 1, X: 2560,
			Modes:   []wlroutput.Mode{mode(1920, 1080, 60000, true)},
			Current: &wlroutput.Mode{Width: 1920, Height: 1080, Refresh: 60000}},
	}
}

func TestModeOption(t *testing.T) {
	h := twoHeads()[0]
	headless := wlroutput.Head{Modes: []wlroutput.Mode{mode(1280, 720, 0, false)}}
	for _, tc := range []struct {
		head wlroutput.Head
		mode string
		want string
	}{
		{h, "2560x1440@59.95", "2560x1440@59951"}, // the display's own rate, not the rounded one
		{h, "2560x1440@144", "2560x1440@143912"},  // the nearest it has
		{h, "2560x1440", "2560x1440@143912"},      // the highest without a rate
		{h, "800x600@75", "800x600@75000"},        // a size it does not list goes as asked
		{headless, "1280x720", "1280x720"},        // a head that names no rate
		{headless, "1920x1080", "1920x1080"},
	} {
		if got, ok := modeOption(tc.head, tc.mode); !ok || got != tc.want {
			t.Errorf("modeOption(%q) = %q, %v; want %q", tc.mode, got, ok, tc.want)
		}
	}
	if _, ok := modeOption(h, "preferred"); ok {
		t.Error("a mode that is no size was taken")
	}
}

func TestBeside(t *testing.T) {
	hs := twoHeads()
	hs[0].Scale = 2 // 1280x720 in the layout
	for side, want := range map[string][2]int{"auto-left": {1280, 0}, "auto-right": {4480, 0}, "auto-up": {2560, -720}, "auto-down": {2560, 1080}} {
		if x, y, ok := beside(hs[0], hs, side); !ok || x != want[0] || y != want[1] {
			t.Errorf("%s: %d,%d %v; want %v", side, x, y, ok, want)
		}
	}
	if _, _, ok := beside(hs[0], hs[:1], "auto-left"); ok {
		t.Error("placed beside no other display")
	}
}

// setOptions are the set-config-options calls the fake recorded, decoded.
func setOptions(t *testing.T, f *fakeWayfire) []map[string]string {
	t.Helper()
	var out []map[string]string
	for _, call := range f.did() {
		data, ok := strings.CutPrefix(call, "wayfire/set-config-options ")
		if !ok {
			continue
		}
		m := map[string]string{}
		if err := json.Unmarshal([]byte(data), &m); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	return out
}

func displayBackend(t *testing.T, hs []wlroutput.Head) (*Backend, *fakeWayfire) {
	t.Helper()
	f := newFakeWayfire(t)
	f.mu.Lock()
	f.methods = append(slices.Clone(allMethods), "wayfire/set-config-options", "wayfire/get-config-option")
	f.mu.Unlock()
	f.set("wayfire/get-config-option", `{"result":"ok","value":"auto","default":"auto"}`)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	oldHeads, oldSettle := heads, settle
	heads, settle = func(context.Context) ([]wlroutput.Head, error) { return hs, nil }, 0
	t.Cleanup(func() { heads, settle = oldHeads, oldSettle })
	return New(NewClient(f.socket)), f
}

// The rules become the outputs' options; what the options were is kept,
// and put back when the rules go.
func TestApplyDisplays(t *testing.T) {
	b, f := displayBackend(t, twoHeads())
	rules := []compositor.DisplayRule{
		{Output: "DP-1", Mode: "2560x1440@59.95", Position: "0x0", Scale: 1.5},
		{Output: "desc:LG TV (HDMI-A-1)", Position: "auto-left"},
		{Output: "desc:A display that is away", Mode: "1920x1080@60"},
	}
	if err := b.ApplySettings(context.Background(), compositor.Settings{Displays: rules}); err != nil {
		t.Fatal(err)
	}
	sets := setOptions(t, f)
	if len(sets) != 2 {
		t.Fatalf("calls %v", sets)
	}
	if want := map[string]string{"output:DP-1/mode": "2560x1440@59951", "output:DP-1/scale": "1.5", "output:DP-1/position": "0,0"}; !mapsEqual(sets[0], want) {
		t.Errorf("first step %v, want %v", sets[0], want)
	}
	// Then the one beside the others, as they are once that took.
	if want := map[string]string{"output:HDMI-A-1/position": "-1920,0"}; !mapsEqual(sets[1], want) {
		t.Errorf("second step %v, want %v", sets[1], want)
	}
	data, _ := os.ReadFile(outputsFile())
	var saved map[string]string
	json.Unmarshal(data, &saved)
	if len(saved) != 4 || saved["output:DP-1/mode"] != "auto" || saved["output:HDMI-A-1/position"] != "auto" {
		t.Fatalf("kept %v", saved)
	}

	// No rules: every option inua set goes back to what it was.
	if err := b.ApplySettings(context.Background(), compositor.Settings{}); err != nil {
		t.Fatal(err)
	}
	sets = setOptions(t, f)
	if want := map[string]string{"output:DP-1/mode": "auto", "output:DP-1/scale": "auto", "output:DP-1/position": "auto", "output:HDMI-A-1/position": "auto"}; len(sets) != 3 || !mapsEqual(sets[2], want) {
		t.Fatalf("put back %v, want %v", sets, want)
	}
	if data, _ := os.ReadFile(outputsFile()); string(data) != "{}" {
		t.Fatalf("still kept %s", data)
	}
	// And with nothing to do, nothing is asked of Wayfire.
	if err := b.ApplySettings(context.Background(), compositor.Settings{}); err != nil || len(setOptions(t, f)) != 3 {
		t.Fatalf("an apply with no rules and nothing kept: %v, %v", err, setOptions(t, f))
	}
}

// One display given a place keeps the others where they are, which
// Wayfire would otherwise lay out again to its right; off and mirror are
// modes.
func TestApplyDisplaysPlaceOffMirror(t *testing.T) {
	b, f := displayBackend(t, twoHeads())
	if err := b.ApplySettings(context.Background(), compositor.Settings{Displays: []compositor.DisplayRule{{Output: "HDMI-A-1", Position: "auto-up"}}}); err != nil {
		t.Fatal(err)
	}
	if sets, want := setOptions(t, f), map[string]string{"output:HDMI-A-1/position": "0,-1080", "output:DP-1/position": "0,0"}; len(sets) != 1 || !mapsEqual(sets[0], want) {
		t.Fatalf("placed above %v, want %v", sets, want)
	}
	b, f = displayBackend(t, twoHeads())
	if err := b.ApplySettings(context.Background(), compositor.Settings{Displays: []compositor.DisplayRule{{Output: "HDMI-A-1", Position: "0x1440"}}}); err != nil {
		t.Fatal(err)
	}
	if sets, want := setOptions(t, f), map[string]string{"output:HDMI-A-1/position": "0,1440", "output:DP-1/position": "0,0"}; len(sets) != 1 || !mapsEqual(sets[0], want) {
		t.Fatalf("placed %v, want %v", sets, want)
	}
	if err := b.ApplySettings(context.Background(), compositor.Settings{Displays: []compositor.DisplayRule{
		{Output: "HDMI-A-1", Mirror: "DP-1"}, {Output: "DP-1", Disabled: true, Mode: "1920x1080@60"}}}); err != nil {
		t.Fatal(err)
	}
	sets := setOptions(t, f)
	last := sets[len(sets)-1]
	if last["output:HDMI-A-1/mode"] != "mirror DP-1" || last["output:DP-1/mode"] != "off" || last["output:DP-1/position"] != "auto" {
		t.Fatalf("mirror and off: %v", last)
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// The displays for the settings: the modes by name, the one shown, a
// mirror from the option; a headless output is left out beside a real one
// and kept when the session has no other.
func TestDisplays(t *testing.T) {
	hs := append(twoHeads()[:1], wlroutput.Head{Name: "HEADLESS-1", Enabled: true, Scale: 1,
		Modes: []wlroutput.Mode{mode(1280, 720, 0, false)}, Current: &wlroutput.Mode{Width: 1280, Height: 720}})
	b, f := displayBackend(t, hs)
	f.set("wayfire/get-config-option", `{"result":"ok","value":"mirror HDMI-A-1"}`)
	got, err := b.Displays(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "DP-1" || got[0].Width != 2560 || got[0].Refresh != 59.951 || got[0].MirrorOf != "HDMI-A-1" ||
		!slices.Equal(got[0].Modes, []string{"2560x1440@59.95", "2560x1440@143.91", "1920x1080@60.00"}) {
		t.Fatalf("displays %+v", got)
	}
	heads = func(context.Context) ([]wlroutput.Head, error) { return hs[1:], nil }
	f.set("wayfire/get-config-option", `{"result":"ok","value":"auto"}`)
	if got, _ = b.Displays(context.Background()); len(got) != 1 || got[0].Name != "HEADLESS-1" || !slices.Equal(got[0].Modes, []string{"1280x720"}) {
		t.Fatalf("a headless session's displays %+v", got)
	}
	if b.Capabilities()[compositor.CapDisplays] != compositor.Supported {
		t.Fatal("displays need only the config options")
	}
}
