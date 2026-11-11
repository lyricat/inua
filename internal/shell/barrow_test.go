package shell

import (
	"math"
	"slices"
	"testing"

	"github.com/lyricat/inua/internal/config"
)

const sp = config.BarSpace

// barSizes are the bar's items roughly as wide as they are on a bar.
var barSizes = map[string]int{
	"workspaces": 120, "app_menu": 160, "app_title": 140, "clock": 96, "tray": 180,
	"recording": 70, "passwords": 36, "clipboard": 36, "notifications": 36, "dictation": 36,
	"control_center": 110,
}

// sized measures layout as the bar would: items at barSizes, spaces
// empty; hidden ones hidden.
func sized(layout []string, hidden ...string) []BarSize {
	out := make([]BarSize, len(layout))
	for i, id := range layout {
		if id == sp {
			continue
		}
		w := barSizes[id]
		out[i] = BarSize{Min: w, Nat: w, Hidden: slices.Contains(hidden, id)}
	}
	return out
}

// middle is the middle of the entries from..to (indices of layout, the
// first and last shown of a group).
func middle(spans []BarSpan, from, to int) float64 {
	return float64(spans[from].X+spans[to].X+spans[to].W) / 2
}

// checkRow checks what every row must be: hidden entries without a span,
// the shown ones in their order, the spacing between them and no
// overlap, none narrower than its least, and the spaces filling the row
// to its end when there is room.
func checkRow(t *testing.T, layout []string, sizes []BarSize, spans []BarSpan, width, spacing int) {
	t.Helper()
	at, any, spaces := 0, false, false
	for i, s := range spans {
		if sizes[i].Hidden {
			if s != (BarSpan{}) {
				t.Errorf("%d %s hidden but at %+v", i, layout[i], s)
			}
			continue
		}
		if any {
			at += spacing
		}
		if s.X != at {
			t.Errorf("%d %s at %d, want %d (after the one before and the spacing): %+v", i, layout[i], s.X, at, spans)
		}
		if s.W < sizes[i].Min {
			t.Errorf("%d %s %d wide, under its least %d", i, layout[i], s.W, sizes[i].Min)
		}
		at, any = s.X+s.W, true
		spaces = spaces || layout[i] == sp
	}
	least, _ := BarRowSize(sizes, spacing)
	if spaces && least <= width && at != width {
		t.Errorf("the row ends at %d, not at its end %d: %+v", at, width, spans)
	}
	if !spaces && least <= width && at > width {
		t.Errorf("the row runs to %d, past its end %d", at, width)
	}
}

func near(a, b float64) bool { return math.Abs(a-b) <= 0.5 }

// The request's first example: the clock, between two spaces, is in the
// middle of the bar though the end has far more on it than the start;
// the two spaces are as wide as that takes, not as wide as each other.
func TestBarRowCentresTheClock(t *testing.T) {
	layout := []string{"workspaces", sp, "clock", sp, "tray", "notifications", "control_center"}
	sizes := sized(layout)
	spans := BarRow(layout, sizes, 1920, 6, 960)
	checkRow(t, layout, sizes, spans, 1920, 6)
	if c := middle(spans, 2, 2); !near(c, 960) {
		t.Errorf("the clock's middle at %v, not the bar's 960: %+v", c, spans)
	}
	if spans[1].W == spans[3].W {
		t.Errorf("the spaces as wide as each other (%d): the room shared, not the clock centred", spans[1].W)
	}
	if spans[1].W <= spans[3].W {
		t.Errorf("the start's space %d not wider than the end's %d, with less beside it", spans[1].W, spans[3].W)
	}
}

// The second: the clock and the passwords between the spaces are one
// group, its middle (spacing within it included) the bar's; neither is
// centred on its own.
func TestBarRowCentresTheGroup(t *testing.T) {
	layout := []string{"workspaces", sp, "clock", "passwords", sp, "tray", "notifications", "control_center"}
	sizes := sized(layout)
	spans := BarRow(layout, sizes, 1920, 6, 960)
	checkRow(t, layout, sizes, spans, 1920, 6)
	if c := middle(spans, 2, 3); !near(c, 960) {
		t.Errorf("the group's middle at %v, not 960: %+v", c, spans)
	}
	if w := spans[3].X + spans[3].W - spans[2].X; w != 96+6+36 {
		t.Errorf("the group %d wide, not its items and the spacing between, 138", w)
	}
	if c := middle(spans, 2, 2); near(c, 960) {
		t.Error("the clock centred on its own")
	}
}

// Whichever side has more, and however much, the group stays in the
// middle while there is room: the start full and the end empty, the
// other way round, both nearly full.
func TestBarRowUnevenSides(t *testing.T) {
	for _, layout := range [][]string{
		{"workspaces", "app_menu", "app_title", sp, "clock", sp},
		{sp, "clock", sp, "tray", "recording", "passwords", "clipboard", "notifications", "control_center"},
		{"workspaces", "app_menu", "app_title", sp, "clock", sp, "tray", "recording", "control_center"},
		{"app_title", sp, "clock", sp, "tray", "recording", "passwords", "clipboard", "notifications", "dictation", "control_center"},
	} {
		sizes := sized(layout)
		spans := BarRow(layout, sizes, 1920, 6, 960)
		checkRow(t, layout, sizes, spans, 1920, 6)
		c := slices.Index(layout, "clock")
		if m := middle(spans, c, c); !near(m, 960) {
			t.Errorf("%v: the clock's middle at %v: %+v", layout, m, spans)
		}
	}
}

// A wider or narrower group is placed anew, its middle staying put; what
// is beside it does not move.
func TestBarRowGroupChangesWidth(t *testing.T) {
	layout := []string{"workspaces", "app_title", sp, "clock", "passwords", sp, "tray", "control_center"}
	before := BarRow(layout, sized(layout), 2560, 6, 1280)
	for _, w := range []int{40, 96, 97, 250, 611} {
		sizes := sized(layout)
		sizes[3] = BarSize{Min: w, Nat: w} // the clock's text changed
		spans := BarRow(layout, sizes, 2560, 6, 1280)
		checkRow(t, layout, sizes, spans, 2560, 6)
		if m := middle(spans, 3, 4); !near(m, 1280) {
			t.Errorf("clock %d wide: the group's middle at %v", w, m)
		}
		for _, i := range []int{0, 1, 6, 7} {
			if spans[i] != before[i] {
				t.Errorf("clock %d wide: %s moved, %+v to %+v", w, layout[i], before[i], spans[i])
			}
		}
	}
}

// Items beside the group coming and going (the tray, a recording) leave
// it where it is; one of its own going leaves the rest of the group in
// the middle; all of them gone, there is no group, and the spaces share
// the room again.
func TestBarRowVisibility(t *testing.T) {
	layout := []string{"workspaces", "recording", sp, "clock", "passwords", sp, "tray", "notifications", "control_center"}
	for _, hidden := range [][]string{nil, {"recording"}, {"tray"}, {"recording", "tray", "notifications"}, {"workspaces", "recording"}} {
		sizes := sized(layout, hidden...)
		spans := BarRow(layout, sizes, 1920, 6, 960)
		checkRow(t, layout, sizes, spans, 1920, 6)
		if m := middle(spans, 3, 4); !near(m, 960) {
			t.Errorf("%v hidden: the group's middle at %v", hidden, m)
		}
	}
	sizes := sized(layout, "passwords")
	spans := BarRow(layout, sizes, 1920, 6, 960)
	checkRow(t, layout, sizes, spans, 1920, 6)
	if m := middle(spans, 3, 3); !near(m, 960) {
		t.Errorf("the passwords hidden: the clock's middle at %v", m)
	}
	sizes = sized(layout, "clock", "passwords")
	spans = BarRow(layout, sizes, 1920, 6, 960)
	checkRow(t, layout, sizes, spans, 1920, 6)
	if d := spans[2].W - spans[5].W; d < -1 || d > 1 {
		t.Errorf("an empty group: the spaces %d and %d, not sharing the room", spans[2].W, spans[5].W)
	}
}

// Every monitor's bar centres the group on its own width, the middle
// where its padding puts it (the island's is wider at its start).
func TestBarRowMonitorWidths(t *testing.T) {
	layout := []string{"workspaces", "app_title", sp, "clock", "passwords", sp, "tray", "notifications", "control_center"}
	sizes := sized(layout)
	for _, width := range []int{1024, 1280, 1366, 1920, 2560, 3440, 3840} {
		for _, center := range []float64{float64(width) / 2, float64(width)/2 - 2} {
			spans := BarRow(layout, sizes, width, 6, center)
			checkRow(t, layout, sizes, spans, width, 6)
			if m := middle(spans, 3, 4); !near(m, center) {
				t.Errorf("%d wide: the group's middle at %v, not %v", width, m, center)
			}
		}
	}
}

// Short of room on one side, the group stops at the items there, at the
// spacing either side of an empty space, and goes as near the middle as
// that lets it; never over them, never out of the order.
func TestBarRowShortOfRoom(t *testing.T) {
	layout := []string{"workspaces", "app_menu", "app_title", sp, "clock", sp, "control_center"}
	sizes := sized(layout)
	sizes[2] = BarSize{Min: 400, Nat: 400} // a long title
	spans := BarRow(layout, sizes, 1000, 6, 500)
	checkRow(t, layout, sizes, spans, 1000, 6)
	if spans[3].W != 0 || spans[4].X != spans[2].X+spans[2].W+12 {
		t.Errorf("the start too full: the clock not beside it: %+v", spans)
	}
	if spans[5].W <= 0 {
		t.Errorf("the end's space has none of the room: %+v", spans)
	}

	layout = []string{"workspaces", sp, "clock", sp, "tray", "recording", "passwords", "clipboard", "notifications", "control_center"}
	sizes = sized(layout)
	sizes[4] = BarSize{Min: 600, Nat: 600} // a full tray
	spans = BarRow(layout, sizes, 1280, 6, 640)
	checkRow(t, layout, sizes, spans, 1280, 6)
	if spans[3].W != 0 || spans[2].X+spans[2].W+12 != spans[4].X {
		t.Errorf("the end too full: the clock not beside it: %+v", spans)
	}
	if m := middle(spans, 2, 2); m >= 640 {
		t.Errorf("the clock at %v, past the middle", m)
	}
}

// Narrower than the items' natural widths, they shrink towards their
// least, the room shared as GTK shares a box's, and the spaces are empty;
// narrower than even their least, they keep their order and overflow the
// end rather than go over one another.
func TestBarRowTooNarrow(t *testing.T) {
	layout := []string{"workspaces", sp, "app_title", "clock", sp, "control_center"}
	sizes := []BarSize{{Min: 100, Nat: 100}, {}, {Min: 40, Nat: 400}, {Min: 90, Nat: 96}, {}, {Min: 110, Nat: 110}}
	spans := BarRow(layout, sizes, 600, 6, 300)
	checkRow(t, layout, sizes, spans, 600, 6)
	if spans[1].W != 0 || spans[4].W != 0 {
		t.Errorf("the spaces take room the items need: %+v", spans)
	}
	if spans[3].W != 96 || spans[2].W != 600-5*6-100-96-110 {
		t.Errorf("not shrunk as GTK shares the room, the nearest to natural first: %+v", spans)
	}

	spans = BarRow(layout, sizes, 200, 6, 100)
	checkRow(t, layout, sizes, spans, 200, 6)
	for i, s := range spans {
		if s.W != sizes[i].Min {
			t.Errorf("%s %d wide, not its least %d", layout[i], s.W, sizes[i].Min)
		}
	}
}

// Without exactly two spaces with something between, nothing is
// centred: the spaces share the room left equally, as before, and
// without any the items are at the start.
func TestBarRowSharesOtherwise(t *testing.T) {
	for _, tc := range []struct {
		name   string
		layout []string
	}{
		{"none", []string{"workspaces", "clock", "control_center"}},
		{"one", []string{"workspaces", "clock", sp, "control_center"}},
		{"three", []string{"workspaces", sp, "clock", sp, "tray", sp, "control_center"}},
		{"four", []string{sp, "workspaces", sp, "clock", sp, "control_center", sp}},
		{"side by side", []string{"workspaces", "clock", sp, sp, "control_center"}},
		{"spaces only", []string{sp, sp}},
		{"empty", []string{}},
	} {
		sizes := sized(tc.layout)
		spans := BarRow(tc.layout, sizes, 1921, 6, 960.5)
		checkRow(t, tc.layout, sizes, spans, 1921, 6)
		var ws []int
		for i, id := range tc.layout {
			if id == sp {
				ws = append(ws, spans[i].W)
			}
		}
		if len(ws) > 0 && slices.Max(ws)-slices.Min(ws) > 1 {
			t.Errorf("%s: the spaces %v, not sharing the room", tc.name, ws)
		}
		if len(ws) == 0 && len(tc.layout) > 0 && spans[0].X != 0 {
			t.Errorf("%s: not from the start: %+v", tc.name, spans)
		}
	}
	if _, _, ok := BarCenterGroup(config.DefaultBarLayout, func(int) bool { return true }); !ok {
		t.Error("the default layout has no centred group")
	}
}

// The bar and the settings' small bar are laid out by the one rule: the
// same layout picks the same group on both, and each centres it on its
// own width with its own items' widths: the editor's chips, its spaces
// as wide as their names at least, its spacing.
func TestBarRowPreviewAsTheBar(t *testing.T) {
	for _, layout := range [][]string{
		{"workspaces", sp, "clock", sp, "tray", "notifications", "control_center"},
		{"workspaces", sp, "clock", "passwords", sp, "tray", "notifications", "control_center"},
		config.DefaultBarLayout,
	} {
		all := func(int) bool { return true }
		f, l, ok := BarCenterGroup(layout, all)
		if !ok {
			t.Fatalf("%v: no group", layout)
		}
		chips := make([]BarSize, len(layout))
		for i, id := range layout {
			chips[i] = BarSize{Min: 30, Nat: 30}
			if id == sp {
				chips[i] = BarSize{Min: 52, Nat: 52}
			}
		}
		for _, row := range []struct {
			name           string
			sizes          []BarSize
			width, spacing int
		}{
			{"bar", sized(layout), 1920, 6},
			{"preview", chips, 640, 4},
			{"wider preview", chips, 720, 4},
		} {
			spans := BarRow(layout, row.sizes, row.width, row.spacing, float64(row.width)/2)
			checkRow(t, layout, row.sizes, spans, row.width, row.spacing)
			if m := middle(spans, f+1, l-1); !near(m, float64(row.width)/2) {
				t.Errorf("%v on the %s: the group's middle at %v of %d: %+v", layout, row.name, m, row.width, spans)
			}
		}
	}
}

func TestBarCenterGroup(t *testing.T) {
	all := func(int) bool { return true }
	for _, tc := range []struct {
		layout      []string
		hidden      int
		first, last int
		ok          bool
	}{
		{[]string{"workspaces", sp, "clock", sp, "tray"}, -1, 1, 3, true},
		{[]string{sp, "clock", "passwords", sp}, -1, 0, 3, true},
		{[]string{sp, "clock", sp}, 1, -1, -1, false},
		{[]string{"workspaces", sp, sp, "tray"}, -1, -1, -1, false},
		{[]string{"workspaces", sp, "tray"}, -1, -1, -1, false},
		{[]string{sp, "clock", sp, "tray", sp}, -1, -1, -1, false},
		{[]string{"clock"}, -1, -1, -1, false},
		{nil, -1, -1, -1, false},
	} {
		shown := all
		if tc.hidden >= 0 {
			shown = func(i int) bool { return i != tc.hidden }
		}
		f, l, ok := BarCenterGroup(tc.layout, shown)
		if f != tc.first || l != tc.last || ok != tc.ok {
			t.Errorf("%v: %d, %d, %v; want %d, %d, %v", tc.layout, f, l, ok, tc.first, tc.last, tc.ok)
		}
	}
}

func TestBarRowSize(t *testing.T) {
	sizes := []BarSize{{Min: 10, Nat: 20}, {}, {Min: 5, Nat: 5, Hidden: true}, {Min: 7, Nat: 9}}
	if m, n := BarRowSize(sizes, 6); m != 10+7+2*6 || n != 20+9+2*6 {
		t.Errorf("%d, %d", m, n)
	}
	if m, n := BarRowSize(nil, 6); m != 0 || n != 0 {
		t.Errorf("empty: %d, %d", m, n)
	}
}
