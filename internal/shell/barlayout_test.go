package shell

import (
	"slices"
	"testing"

	"github.com/lyricat/inua/internal/config"
)

func TestBarLayoutInsert(t *testing.T) {
	l := []string{"workspaces", "clock"}
	got, ok := BarLayoutInsert(l, "tray", 1)
	if !ok || !slices.Equal(got, []string{"workspaces", "tray", "clock"}) {
		t.Errorf("insert: %v %v", got, ok)
	}
	if !slices.Equal(l, []string{"workspaces", "clock"}) {
		t.Errorf("the layout given was changed: %v", l)
	}
	// An item on the bar already is not added again, wherever it goes.
	if got, ok := BarLayoutInsert(l, "clock", 0); ok || !slices.Equal(got, l) {
		t.Errorf("a repeat: %v %v", got, ok)
	}
	// Nor is one the bar does not have.
	if got, ok := BarLayoutInsert(l, "weather", 0); ok || !slices.Equal(got, l) {
		t.Errorf("unknown: %v %v", got, ok)
	}
	// Spaces, as many as wanted; a gap past the end is the end.
	got, _ = BarLayoutInsert(l, config.BarSpace, 1)
	got, _ = BarLayoutInsert(got, config.BarSpace, 99)
	got, ok = BarLayoutInsert(got, config.BarSpace, -3)
	want := []string{"space", "workspaces", "space", "clock", "space"}
	if !ok || !slices.Equal(got, want) {
		t.Errorf("spaces: %v, want %v", got, want)
	}
	// Into an empty bar.
	if got, ok := BarLayoutInsert([]string{}, "notifications", 0); !ok || !slices.Equal(got, []string{"notifications"}) {
		t.Errorf("into empty: %v %v", got, ok)
	}
}

func TestBarLayoutMove(t *testing.T) {
	l := []string{"workspaces", "clock", "tray"}
	cases := []struct {
		from, to int
		want     []string
		changed  bool
	}{
		{0, 3, []string{"clock", "tray", "workspaces"}, true}, // to the end
		{2, 0, []string{"tray", "workspaces", "clock"}, true}, // to the start
		{0, 2, []string{"clock", "workspaces", "tray"}, true}, // one on
		{1, 1, l, false}, // the gap before it
		{1, 2, l, false}, // the gap after it
		{5, 0, l, false}, // nothing there
		{0, 9, []string{"clock", "tray", "workspaces"}, true}, // past the end: the end
	}
	for _, c := range cases {
		got, ok := BarLayoutMove(l, c.from, c.to)
		if ok != c.changed || !slices.Equal(got, c.want) {
			t.Errorf("move %d to %d: %v %v; want %v %v", c.from, c.to, got, ok, c.want, c.changed)
		}
	}
	// Spaces move as any item, and stay as many.
	got, _ := BarLayoutMove([]string{"space", "clock", "space"}, 0, 3)
	if !slices.Equal(got, []string{"clock", "space", "space"}) {
		t.Errorf("a space: %v", got)
	}
}

func TestBarLayoutRemoveAndPalette(t *testing.T) {
	l := slices.Clone(config.DefaultBarLayout)
	if p := BarPalette(l); !slices.Equal(p, []string{config.BarSpace}) {
		t.Errorf("all on the bar: palette %v", p)
	}
	i := slices.Index(l, "clock")
	got, ok := BarLayoutRemove(l, i)
	if !ok || slices.Contains(got, "clock") || len(got) != len(l)-1 {
		t.Fatalf("remove: %v %v", got, ok)
	}
	// Taken off, it is offered again, and can be put back once.
	if p := BarPalette(got); !slices.Equal(p, []string{"clock", config.BarSpace}) {
		t.Errorf("palette: %v", p)
	}
	back, ok := BarLayoutInsert(got, "clock", i)
	if !ok || !slices.Equal(back, l) {
		t.Errorf("back: %v %v", back, ok)
	}
	if _, ok := BarLayoutInsert(back, "clock", 0); ok {
		t.Error("added twice")
	}
	if _, ok := BarLayoutRemove(l, len(l)); ok {
		t.Error("removed past the end")
	}
	// A space removed leaves the others.
	got, _ = BarLayoutRemove([]string{"space", "clock", "space"}, 2)
	if !slices.Equal(got, []string{"space", "clock"}) {
		t.Errorf("a space: %v", got)
	}
	// Everything removed is an empty bar, not the default one.
	got = []string{"clock"}
	got, _ = BarLayoutRemove(got, 0)
	if got == nil || len(got) != 0 {
		t.Errorf("emptied: %#v", got)
	}
	if p := BarPalette(got); len(p) != len(config.BarItems)+1 {
		t.Errorf("palette of an empty bar: %v", p)
	}
}

// An edit of a layout with repeats (one written by hand) gives a layout
// without them.
func TestBarLayoutEditsNormalize(t *testing.T) {
	l := []string{"clock", "clock", "weather", "space", "space"}
	got, _ := BarLayoutInsert(l, "tray", 0)
	if !slices.Equal(got, []string{"tray", "clock", "space", "space"}) {
		t.Errorf("insert: %v", got)
	}
	got, _ = BarLayoutMove(l, 0, 3)
	if !slices.Equal(got, []string{"space", "space", "clock"}) {
		t.Errorf("move: %v", got)
	}
}
