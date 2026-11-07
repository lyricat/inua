package ui

import (
	"testing"

	"github.com/lyricat/inua/internal/compositor"
)

func TestOverviewContentBounds(t *testing.T) {
	windows := []compositor.Window{
		{Geometry: &compositor.Rect{X: 10, Y: 40, W: 300, H: 200}},
		{Geometry: &compositor.Rect{X: 400, Y: 100, W: 500, H: 300}},
	}
	x, y, w, h := overviewContentBounds(windows, 0, 0, 1920, 1080)
	if x != 10 || y != 40 || w != 890 || h != 360 {
		t.Errorf("bounds = %v,%v %vx%v, want 10,40 890x360", x, y, w, h)
	}
	if x, y, w, h := overviewContentBounds(nil, 0, 0, 1920, 1080); x != 0 || y != 0 || w != 1920 || h != 1080 {
		t.Errorf("empty bounds = %v,%v %vx%v, want the monitor", x, y, w, h)
	}
}

func TestOverviewWindowLayoutFillsCard(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		w                      compositor.Window
		boxX, boxY, boxW, boxH float64
	}{
		{"one window", compositor.Window{Geometry: &compositor.Rect{X: 100, Y: 100, W: 900, H: 700}}, 100, 100, 900, 700},
		{"tall window", compositor.Window{Geometry: &compositor.Rect{X: 100, Y: 100, W: 400, H: 900}}, 100, 100, 400, 900},
		{"no geometry", compositor.Window{}, 0, 0, 1920, 1080},
	} {
		x, y, pw, ph := overviewWindowLayout(tc.w, tc.boxX, tc.boxY, tc.boxW, tc.boxH, 320, 180)
		if x > 0 || y > 0 || x+pw < 320 || y+ph < 180 {
			t.Errorf("%s: rect %v,%v %vx%v does not cover the 320x180 card", tc.name, x, y, pw, ph)
		}
	}
}
