package ui

import (
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gsk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/shell"
)

// barRows lays out the rows that show a bar layout: each bar's island and
// the settings' small bar that previews it, by shell.BarRow, so that a
// group between two spaces is in the middle of the bar on both. One set of
// callbacks serves every row (a callback is never freed); each call finds
// its row by the widget it is for, from the moment the row has the
// layout: a bar's window measures it before the bar is in the shell's
// list.
type barRows struct {
	islands    map[uintptr]*bar // by their islands, while the bars are
	strip      uintptr          // the editor's, the last window's
	onMode     gtk.CustomRequestModeFunc
	onMeasure  gtk.CustomMeasureFunc
	onAllocate gtk.CustomAllocateFunc
}

// barRow is one row as its callbacks see it: the widget, the layout it
// shows with a widget for each entry, and the spacing between them. A nil
// layout fills the row with each shown child instead: the default bar's
// centred box, or the empty editor's hint.
type barRow struct {
	box     *gtk.Widget
	layout  []string
	entries []*gtk.Widget
	spacing int
}

// The spacings of the rows: the island's (bar.ui) and the editor's
// (settings.ui), which the rows' layout keeps in place of the boxes'.
const (
	islandSpacing = 6
	stripSpacing  = 4
)

// layIsland gives a bar's island the rows' layout; forgetIsland lets it
// go with its bar.
func (s *shellUI) layIsland(b *bar) {
	if s.barRows.islands == nil {
		s.barRows.islands = map[uintptr]*bar{}
	}
	s.barRows.islands[b.island.GoPointer()] = b
	s.layBarRow(&b.island.Widget)
}

func (s *shellUI) forgetIsland(b *bar) { delete(s.barRows.islands, b.island.GoPointer()) }

// layStrip gives the editor's strip the rows' layout.
func (s *shellUI) layStrip() {
	bl := &s.settings.barLayout
	s.barRows.strip = bl.strip.GoPointer()
	s.layBarRow(&bl.strip.Widget)
}

// layBarRow gives w the rows' layout.
func (s *shellUI) layBarRow(w *gtk.Widget) {
	r := &s.barRows
	if r.onMeasure == nil {
		r.onMode = func(uintptr) gtk.SizeRequestMode { return gtk.SizeRequestConstantSizeValue }
		r.onMeasure = func(w uintptr, o gtk.Orientation, _ int32, least, natural, leastBase, naturalBase *int32) {
			*least, *natural, *leastBase, *naturalBase = 0, 0, -1, -1
			if row, ok := s.barRowOf(w); ok {
				m, n := row.measure(o)
				*least, *natural = int32(m), int32(n)
			}
		}
		r.onAllocate = func(w uintptr, width, height, _ int32) {
			if row, ok := s.barRowOf(w); ok {
				row.allocate(int(width), int(height))
			}
		}
	}
	w.SetLayoutManager(&gtk.NewCustomLayout(&r.onMode, &r.onMeasure, &r.onAllocate).LayoutManager)
}

// barRowOf is the row w is: a bar's island, or the editor's strip. The
// islands come first: a strip gone may have left its address to one.
func (s *shellUI) barRowOf(w uintptr) (barRow, bool) {
	if b := s.barRows.islands[w]; b != nil {
		return b.islandRow(), true
	}
	if w != 0 && w == s.barRows.strip {
		return s.settings.barLayout.stripRow(), true
	}
	return barRow{}, false
}

// islandRow is a bar's island as a row: its layout, or the default's box.
func (b *bar) islandRow() barRow {
	layout, entries := b.rowEntries()
	return barRow{&b.island.Widget, layout, entries, islandSpacing}
}

// stripRow is the editor's strip as a row: its chips, or the hint of an
// empty bar.
func (bl *barLayoutSettings) stripRow() barRow {
	row := barRow{box: &bl.strip.Widget, spacing: stripSpacing}
	if len(bl.chips) > 0 && len(bl.chips) == len(bl.shown) {
		row.layout = bl.shown
		for _, c := range bl.chips {
			row.entries = append(row.entries, &c.Widget)
		}
	}
	return row
}

// rowEntries is the layout the island shows with the widget of each of
// its entries; nil for the default's, which bar.ui's centred box shows.
func (b *bar) rowEntries() ([]string, []*gtk.Widget) {
	if b.layout == nil {
		return nil, nil
	}
	var layout []string
	var entries []*gtk.Widget
	spaces := b.spaces
	for _, id := range b.layout {
		var w *gtk.Widget
		if id == config.BarSpace {
			if len(spaces) == 0 {
				continue
			}
			w, spaces = &spaces[0].Widget, spaces[1:]
		} else if w = b.item(id); w == nil {
			continue
		}
		layout = append(layout, id)
		entries = append(entries, w)
	}
	return layout, entries
}

// children are the row's widgets that take part in the layout.
func (r barRow) children() []*gtk.Widget {
	var out []*gtk.Widget
	for c := r.box.GetFirstChild(); c != nil; c = c.GetNextSibling() {
		if c.ShouldLayout() {
			out = append(out, c)
		}
	}
	return out
}

// sizes measures the layout's entries along the row.
func (r barRow) sizes() []shell.BarSize {
	sizes := make([]shell.BarSize, len(r.entries))
	for i, w := range r.entries {
		if !w.ShouldLayout() {
			sizes[i].Hidden = true
			continue
		}
		var m, n int32
		w.Measure(gtk.OrientationHorizontalValue, -1, &m, &n, nil, nil)
		sizes[i] = shell.BarSize{Min: int(m), Nat: int(n)}
	}
	return sizes
}

func (r barRow) measure(o gtk.Orientation) (least, natural int) {
	if r.layout != nil && o == gtk.OrientationHorizontalValue {
		return shell.BarRowSize(r.sizes(), r.spacing)
	}
	// Across the row, or the filling children along it: the largest.
	for _, c := range r.children() {
		var m, n int32
		c.Measure(o, -1, &m, &n, nil, nil)
		least, natural = max(least, int(m)), max(natural, int(n))
	}
	return least, natural
}

func (r barRow) allocate(width, height int) {
	if r.layout == nil {
		for _, c := range r.children() {
			c.Allocate(int32(width), int32(height), -1, nil)
		}
		return
	}
	spans := shell.BarRow(r.layout, r.sizes(), width, r.spacing, r.center(width))
	for i, w := range r.entries {
		if !w.ShouldLayout() {
			continue
		}
		t := gsk.NewTransform().Translate(&graphene.Point{X: float32(spans[i].X)})
		w.Allocate(int32(spans[i].W), int32(height), -1, t)
	}
}

// center is the middle of the whole row, its padding and border too, in
// the coordinates its children are placed in (its content's): the bar's
// padding is wider at its start than at its end, and the group goes in
// the middle of the bar, the screen's.
func (r barRow) center(width int) float64 {
	var b graphene.Rect
	if !r.box.ComputeBounds(r.box, &b) {
		return float64(width) / 2
	}
	f := *(*[4]float32)(unsafe.Pointer(&b)) // x, y, width, height; see rect
	start := -float64(f[0])
	end := float64(f[2]) - start - float64(r.box.GetWidth())
	return (float64(width) + end - start) / 2
}
