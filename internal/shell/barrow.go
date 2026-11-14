package shell

import (
	"math"
	"slices"

	"github.com/lyricat/inua/internal/config"
)

// The bar's row: a layout of the user's (bar.layout) laid out along the
// bar, and along the settings' small bar that previews it, by one rule.
// Items take their natural widths, shrunk towards their least ones only
// when the row is too narrow for them all; the spaces take the room left.
// When the layout has exactly two spaces with something shown between
// them, that group of items, as wide as its items and the spacing between
// them, sits in the middle of the whole bar, its two spaces as wide as
// that takes; where the room either side is short, the group stops at the
// items beside it, as near the middle as they let it, and never goes over
// them or changes places with them. Any other layout (no space, one,
// three or more, two side by side or with nothing shown between them)
// shares the room left equally between its spaces.

// BarSize is how wide one entry of a layout is, as measured on the row:
// its least and natural widths, and whether it is hidden, taking no room
// and no spacing.
type BarSize struct {
	Min, Nat int
	Hidden   bool
}

// BarSpan is where BarRow puts an entry: from X along the row, W wide.
type BarSpan struct{ X, W int }

// BarRowSize is how wide a row of layout's entries, sized by sizes, is at
// the least and naturally: its shown entries and the spacing between
// them.
func BarRowSize(sizes []BarSize, spacing int) (least, natural int) {
	n := 0
	for _, s := range sizes {
		if s.Hidden {
			continue
		}
		least += s.Min
		natural += max(s.Min, s.Nat)
		n++
	}
	if n > 1 {
		least += spacing * (n - 1)
		natural += spacing * (n - 1)
	}
	return least, natural
}

// BarCenterGroup finds the group of a layout that goes in the middle of
// the bar: the entries between its spaces when it has exactly two, with
// at least one shown entry between them (shown reports whether the entry
// at i is). first and last are the two spaces' indices.
func BarCenterGroup(layout []string, shown func(i int) bool) (first, last int, ok bool) {
	first, last = -1, -1
	for i, id := range layout {
		if id != config.BarSpace {
			continue
		}
		switch {
		case first < 0:
			first = i
		case last < 0:
			last = i
		default:
			return -1, -1, false // three or more
		}
	}
	if last < 0 {
		return -1, -1, false
	}
	for i := first + 1; i < last; i++ {
		if shown(i) {
			return first, last, true
		}
	}
	return -1, -1, false
}

// BarRow places layout's entries, sized by sizes (one each), along a row
// width wide, spacing between shown neighbours. center is the middle of
// the whole bar in the row's own coordinates, which the bar's padding
// may move off width/2. Hidden entries get no span. Too narrow even for
// the items' least widths, they overflow the row's end, in their order.
func BarRow(layout []string, sizes []BarSize, width, spacing int, center float64) []BarSpan {
	spans := make([]BarSpan, len(layout))
	shown := func(i int) bool { return i < len(sizes) && !sizes[i].Hidden }
	var order []int // the shown entries
	for i := range layout {
		if shown(i) {
			order = append(order, i)
		}
	}
	if len(order) == 0 {
		return spans
	}
	w := make([]int, len(layout))
	room := width - spacing*(len(order)-1)
	var items []int
	natural := 0
	for _, i := range order {
		s := sizes[i]
		if layout[i] == config.BarSpace {
			w[i] = s.Min
			room -= s.Min
			continue
		}
		items = append(items, i)
		w[i] = max(s.Min, s.Nat)
		natural += w[i]
	}
	free := room - natural
	if free < 0 {
		shrink(items, sizes, w, room)
		free = 0
	}

	var spaces []int
	for _, i := range order {
		if layout[i] == config.BarSpace {
			spaces = append(spaces, i)
		}
	}
	if first, last, ok := BarCenterGroup(layout, shown); ok && free > 0 {
		// The group's place with its spaces at their least, and how far
		// the middle wants it from there; its first space takes that, as
		// much as there is, and the second the rest.
		x := placed(order, w, spacing)
		from, to := -1, 0
		for _, i := range order {
			if i > first && i < last {
				if from < 0 {
					from = x[i]
				}
				to = x[i] + w[i]
			}
		}
		want := int(math.Round(center - float64(to-from)/2))
		push := max(0, min(want-from, free))
		w[first] += push
		w[last] += free - push
	} else if len(spaces) > 0 {
		each, rest := free/len(spaces), free%len(spaces)
		for k, i := range spaces {
			w[i] += each
			if k < rest {
				w[i]++
			}
		}
	}
	x := placed(order, w, spacing)
	for _, i := range order {
		spans[i] = BarSpan{X: x[i], W: w[i]}
	}
	return spans
}

// placed packs the shown entries from the row's start at widths w.
func placed(order []int, w []int, spacing int) []int {
	x := make([]int, len(w))
	at := 0
	for k, i := range order {
		if k > 0 {
			at += spacing
		}
		x[i] = at
		at += w[i]
	}
	return x
}

// shrink brings the items to room together: each its least width, and
// what room is left over that shared out towards their natural widths,
// the nearest to theirs first, as GTK shares a box's room.
func shrink(items []int, sizes []BarSize, w []int, room int) {
	least := 0
	for _, i := range items {
		w[i] = sizes[i].Min
		least += w[i]
	}
	extra := room - least
	if extra <= 0 {
		return
	}
	gap := func(i int) int { return max(0, sizes[i].Nat-sizes[i].Min) }
	byGap := slices.Clone(items)
	slices.SortStableFunc(byGap, func(a, b int) int { return gap(a) - gap(b) })
	for k, i := range byGap {
		left := len(byGap) - k
		give := min(gap(i), (extra+left-1)/left)
		w[i] += give
		extra -= give
	}
}
