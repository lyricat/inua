package shell

import (
	"slices"

	"github.com/lyricat/inua/internal/config"
)

// The bar layout's editing, as the settings' editor does it: a layout is an
// ordered list of config.BarItems and config.BarSpace (bar.layout), and an
// edit puts an item at a gap of the list: gap i is before the item at i,
// len(layout) after the last. Every edit gives a new list, normalized, so
// that no item is on the bar twice however the edits come.

// BarLayoutInsert puts id on the bar at gap at (clamped to the list). A
// space can always be added; another item only when it is not on the bar
// yet. It reports whether the layout changed.
func BarLayoutInsert(layout []string, id string, at int) ([]string, bool) {
	out, _ := config.NormalizeBarLayout(nonNil(layout))
	if id != config.BarSpace && (!slices.Contains(config.BarItems, id) || slices.Contains(out, id)) {
		return out, false
	}
	at = max(0, min(at, len(out)))
	return slices.Insert(out, at, id), true
}

// BarLayoutMove moves the item at from to gap to of the same list: the
// gaps either side of it leave it where it is. It reports whether the
// layout changed.
func BarLayoutMove(layout []string, from, to int) ([]string, bool) {
	out, _ := config.NormalizeBarLayout(nonNil(layout))
	if from < 0 || from >= len(out) {
		return out, false
	}
	to = max(0, min(to, len(out)))
	if to == from || to == from+1 {
		return out, false
	}
	id := out[from]
	out = slices.Delete(out, from, from+1)
	if to > from {
		to--
	}
	return slices.Insert(out, to, id), true
}

// BarLayoutRemove takes the item at i off the bar; it goes back to the
// palette (BarPalette). It reports whether the layout changed.
func BarLayoutRemove(layout []string, i int) ([]string, bool) {
	out, _ := config.NormalizeBarLayout(nonNil(layout))
	if i < 0 || i >= len(out) {
		return out, false
	}
	return slices.Delete(out, i, i+1), true
}

// BarPalette is what can still be added to a layout: the items not on it,
// in config.BarItems' order, and the space, always.
func BarPalette(layout []string) []string {
	var out []string
	for _, id := range config.BarItems {
		if !slices.Contains(layout, id) {
			out = append(out, id)
		}
	}
	return append(out, config.BarSpace)
}

// nonNil is layout, or an empty one for none: an edit starts from the
// layout shown, and the result is the user's, even when empty.
func nonNil(layout []string) []string {
	if layout == nil {
		return []string{}
	}
	return layout
}
