package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lyricat/inua/internal/config"
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
	for _, id := range barBreaks {
		if b.item(id) == nil {
			t.Errorf("break %s is not a bar item", id)
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
