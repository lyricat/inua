package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/shell"
)

// The settings' editor and the bar name the same items: every item of
// config.BarItems has a face in the editor, with an icon of the shell's
// own set, and a widget on the bar; the space has a face only.
func TestBarItemsRegistry(t *testing.T) {
	b := &bar{}
	for _, id := range config.BarItems {
		face, ok := barItemFaces[id]
		if !ok || face.label == "" || face.icon == "" {
			t.Errorf("%s: no face in the editor (%+v)", id, face)
			continue
		}
		if _, err := os.Stat(filepath.Join("..", "..", "ui", "icons", face.icon+".svg")); err != nil {
			t.Errorf("%s: icon %s is not in ui/icons", id, face.icon)
		}
		if b.item(id) == nil {
			t.Errorf("%s: no widget on the bar", id)
		}
	}
	if face, ok := barItemFaces[config.BarSpace]; !ok || face.label == "" {
		t.Error("the space has no face")
	}
	if b.item(config.BarSpace) != nil || b.item("weather") != nil {
		t.Error("a widget for what is not a bar item")
	}
	if len(barItemFaces) != len(config.BarItems)+1 {
		t.Errorf("%d faces for %d items and the space", len(barItemFaces), len(config.BarItems))
	}
	for id := range barReaches {
		if b.item(id) == nil {
			t.Errorf("reach %s is not a bar item", id)
		}
	}
	for _, id := range barStartItems {
		if b.item(id) == nil {
			t.Errorf("start item %s is not a bar item", id)
		}
	}
}

// A panel's reach never runs across another item: in a layout of the
// control center, the tray and the notifications, the tray, and the gap
// beside it, is nobody's reach, so its hover, clicks and the wheel are not
// taken for the control center's (or the notifications'). Every item but
// the panels' toggles breaks the reaches.
func TestBarReachStopsAtOtherItems(t *testing.T) {
	for _, id := range config.BarItems {
		name, spot := barSpot(id, shell.Rect{})
		if spot.Reach != (barReaches[id] != "") || spot.Break == spot.Reach || name != barReaches[id] {
			t.Errorf("%s: name %q, %+v", id, name, spot)
		}
	}

	// As measured with bar.layout = ["control_center", "tray",
	// "notifications"]: 6 px apart, 24 px tall at y 8.
	layout := []struct {
		id string
		r  shell.Rect
	}{
		{"control_center", shell.Rect{X: 4, Y: 8, W: 150, H: 24}},
		{"tray", shell.Rect{X: 160, Y: 8, W: 80, H: 24}},
		{"notifications", shell.Rect{X: 246, Y: 8, W: 34, H: 24}},
	}
	var names []string
	var spots []shell.BarSpot
	for _, it := range layout {
		name, spot := barSpot(it.id, it.r)
		names = append(names, name)
		spots = append(spots, spot)
	}
	reach := func(x, y float64) string {
		i, _ := shell.BarReach(spots, 6, x, y)
		if i < 0 {
			return ""
		}
		return names[i]
	}
	for _, c := range []struct {
		x, y float64
		want string
	}{
		{100, 20, "status"},
		{156.9, 20, "status"}, // half the gap past the control center
		{157, 20, ""},         // then the tray's
		{170, 20, ""},         // on the tray
		{200, 0, ""},          // above it, at the screen's edge
		{239, 20, ""},
		{243, 20, "notes"}, // half the gap before the notifications
		{260, 20, "notes"},
	} {
		if got := reach(c.x, c.y); got != c.want {
			t.Errorf("at %v, %v: reach %q, want %q", c.x, c.y, got, c.want)
		}
	}
}

func TestBarDragContent(t *testing.T) {
	if i, id, ok := barDrag("bar:3:clock"); !ok || i != 3 || id != "clock" {
		t.Errorf("bar:3:clock = %d %q %v", i, id, ok)
	}
	for _, bad := range []string{"add:clock", "bar:x:clock", "bar:-1:clock", "bar:3"} {
		if _, _, ok := barDrag(bad); ok {
			t.Errorf("%q read as an item of the bar's", bad)
		}
	}
	if i, ok := chipIndex("bar:12"); !ok || i != 12 {
		t.Errorf("bar:12 = %d %v", i, ok)
	}
	if _, ok := chipIndex("add:space"); ok {
		t.Error("a palette chip read as the strip's")
	}
	if !sameLayout(nil, nil) || sameLayout(nil, []string{}) || !sameLayout([]string{}, []string{}) || sameLayout([]string{"clock"}, []string{"tray"}) {
		t.Error("sameLayout")
	}
}
