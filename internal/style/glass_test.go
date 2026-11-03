package style

import (
	"fmt"
	"testing"
)

// The default 16 keeps the surfaces' old 0.84, and an empty workspace's
// islands near their old 0.70; 0 is opaque throughout.
func TestGlass(t *testing.T) {
	for _, c := range []struct {
		transparency int
		want         string
	}{
		{16, "@define-color glass alpha(@app, 0.84);\n@define-color glass_empty alpha(@app, 0.71);\n"},
		{0, "@define-color glass alpha(@app, 1.00);\n@define-color glass_empty alpha(@app, 1.00);\n"},
		{80, "@define-color glass alpha(@app, 0.20);\n@define-color glass_empty alpha(@app, 0.04);\n"},
	} {
		if got := Glass(c.transparency); got != c.want {
			t.Errorf("Glass(%d) = %q, want %q", c.transparency, got, c.want)
		}
	}
}

// The default 4 keeps style.css's corners: 4px, and 6px for the dock.
func TestCorners(t *testing.T) {
	for _, c := range []struct{ radius, surface, dock int }{{4, 4, 6}, {0, 0, 0}, {20, 20, 30}} {
		want := fmt.Sprintf(".bar .island, .launcher, .widget-page, .agenda .card, .control .card, .note, .osd-card, .center .card, .clipboard-panel .clipboard-card, popover.menu > contents, popover.glass-menu.gtk-frame > contents, popover.term-menu > contents { border-radius: %dpx; }\n.dock { border-radius: %dpx; }\n", c.surface, c.dock)
		if got := Corners(c.radius); got != want {
			t.Errorf("Corners(%d) = %q, want %q", c.radius, got, want)
		}
	}
}
