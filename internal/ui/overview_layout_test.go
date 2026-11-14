package ui

import (
	"math"
	"testing"

	"github.com/lyricat/inua/internal/compositor"
)

// A card is the monitor, so every window inside it maps with one scale and
// nothing leaves the card. Cropping to the windows' union cut edge windows.
func TestOverviewWindowLayoutKeepsWindowsInsideTheCard(t *testing.T) {
	const thumbW, thumbH = 320.0, 180.0
	// Two windows tiled side by side on a 1920x1080 monitor, below a 32px bar.
	for i, geom := range []compositor.Rect{
		{X: 0, Y: 32, W: 960, H: 1048},
		{X: 960, Y: 32, W: 960, H: 1048},
	} {
		w := compositor.Window{Geometry: &geom}
		x, y, pw, ph := overviewWindowLayout(w, 0, 0, 1920, 1080, thumbW, thumbH)
		if x < 0 || y < 0 || x+pw > thumbW || y+ph > thumbH {
			t.Errorf("window %d at %+v: rect %v,%v %vx%v leaves the %vx%v card", i, geom, x, y, pw, ph, thumbW, thumbH)
		}
		wantX := float64(i) * 160
		if math.Abs(x-wantX) > 0.01 || math.Abs(pw-160) > 0.01 {
			t.Errorf("window %d: x=%v width=%v, want x=%v width=160", i, x, pw, wantX)
		}
	}

	// A maximized window keeps the bar's band above it visible, uncut.
	geom := compositor.Rect{X: 0, Y: 32, W: 1920, H: 1048}
	x, y, pw, ph := overviewWindowLayout(compositor.Window{Geometry: &geom}, 0, 0, 1920, 1080, thumbW, thumbH)
	if x != 0 || math.Abs(y-32.0/6) > 0.01 || pw != thumbW || math.Abs(ph-1048.0/6) > 0.01 {
		t.Errorf("maximized window: rect %v,%v %vx%v, want 0,5.33 320x174.67", x, y, pw, ph)
	}
}

func TestOverviewWindowLayoutFillsUsableDesktop(t *testing.T) {
	geom := compositor.Rect{X: 0, Y: 32, W: 1920, H: 1048}
	x, y, w, h := overviewWindowLayout(compositor.Window{Geometry: &geom}, 0, 32, 1920, 1048, 320, 180)
	if x != 0 || y != 0 || w != 320 || h != 180 {
		t.Fatalf("got %v,%v %vx%v; want full 320x180 card", x, y, w, h)
	}
}

func TestOverviewUsableHeight(t *testing.T) {
	windows := []compositor.Window{{Geometry: &compositor.Rect{Y: 28}}}
	y, h := overviewUsableHeight(windows, 0, 1080, 26)
	if y != 26 || h != 1054 {
		t.Fatalf("got %v, %v", y, h)
	}
	windows[0].Geometry.Y = 0
	y, h = overviewUsableHeight(windows, 0, 1080, 26)
	if y != 0 || h != 1080 {
		t.Fatalf("fullscreen got %v, %v", y, h)
	}
}
