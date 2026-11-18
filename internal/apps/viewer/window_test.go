package viewer

import "testing"

func TestWindowFor(t *testing.T) {
	screen := Rect{X: 0, Y: 0, W: 2000, H: 1000}
	cur := Rect{X: 450, Y: 120, W: 1100, H: 760} // centred at 1000, 500
	cases := []struct {
		name string
		w, h float64
		want Rect
	}{
		// At its actual size, in its own shape, where the window was.
		{"fits", 800, 600, Rect{X: 600, Y: 200, W: 800, H: 600}},
		// Too big for the screen: as large as 86% of it lets, its shape kept.
		{"large", 4000, 3000, Rect{X: 427, Y: 70, W: 1147, H: 860}},
		// Small: grown to the shortest side, its shape kept.
		{"small", 120, 60, Rect{X: 640, Y: 320, W: 720, H: 360}},
		// A panorama: the window as wide as the image lets, MaxAspect's shape.
		{"wide", 1600, 200, Rect{X: 280, Y: 200, W: 1440, H: 600}},
		// A long screenshot: as tall as the screen lets, MaxAspect's shape.
		{"tall", 400, 8000, Rect{X: 821, Y: 70, W: 358, H: 860}},
	}
	for _, c := range cases {
		if got := WindowFor(c.w, c.h, cur, screen); got != c.want {
			t.Errorf("%s: WindowFor(%v, %v) = %+v, want %+v", c.name, c.w, c.h, got, c.want)
		}
	}
	// On a large screen, a large photo stops at MaxWidth×MaxHeight.
	big := Rect{W: 2560, H: 1440}
	if got := WindowFor(6000, 4000, Rect{}, big); got != (Rect{X: 560, Y: 240, W: 1440, H: 960}) {
		t.Errorf("large screen, landscape: %+v", got)
	}
	if got := WindowFor(3000, 4000, Rect{}, big); got != (Rect{X: 920, Y: 240, W: 720, H: 960}) {
		t.Errorf("large screen, portrait: %+v", got)
	}
	// Near the screen's edge, a larger window is kept on the screen.
	edge := Rect{X: 1800, Y: 900, W: 200, H: 100}
	if got := WindowFor(1000, 800, edge, screen); got.X+got.W > screen.W || got.Y+got.H > screen.H || got.X < 0 || got.Y < 0 {
		t.Errorf("off the screen: %+v", got)
	}
	// On a second screen, it stays there.
	right := Rect{X: 2000, Y: 0, W: 1600, H: 900}
	if got := WindowFor(800, 600, Rect{X: 2400, Y: 100, W: 800, H: 600}, right); got.X < right.X {
		t.Errorf("left its screen: %+v", got)
	}
	// Nothing to go by: the window as it is.
	if got := WindowFor(0, 0, cur, screen); got != cur {
		t.Errorf("no image: %+v", got)
	}
}

func TestFillZoom(t *testing.T) {
	cases := []struct {
		w, h, vw, vh, want float64
	}{
		{4000, 3000, 1000, 750, 0.25},  // the same shape: fills it
		{100, 100, 400, 400, 4},        // small: grown
		{1000, 500, 1000, 1000, 1},     // wider than the view: whole, bars above and below
		{1000, 1000, 1010, 1000, 1.01}, // a pixel or so off: covers
		{0, 100, 100, 100, 1},
	}
	for _, c := range cases {
		if got := FillZoom(c.w, c.h, c.vw, c.vh); got != c.want {
			t.Errorf("FillZoom(%v, %v, %v, %v) = %v, want %v", c.w, c.h, c.vw, c.vh, got, c.want)
		}
	}
}
