package config

import (
	"fmt"
	"slices"
	"strings"
)

// BarSpace is the bar layout's flexible space: it takes a share of the
// room the bar's items leave, as much as every other space on the bar, and
// may be placed any number of times.
const BarSpace = "space"

// BarItems are the things the bar can show, one of each at most, as
// bar.layout names them: the workspaces with the widget page's mark, the
// focused app's menu, its icon and name, the clock, the tray, the
// recording's stop button, the password search, the clipboard history,
// the notifications, dictation and the control center. The settings'
// editor offers them in this order.
var BarItems = []string{
	"workspaces", "app_menu", "app_title", "clock", "tray", "recording",
	"passwords", "clipboard", "notifications", "dictation", "control_center",
}

// DefaultBarLayout is the bar's layout with the global menu off: the
// workspaces and the app at the start, the clock in the middle, the rest at
// the end. Unset, the clock is centred on the screen exactly; a layout of
// the user's shares the room between its spaces instead.
var DefaultBarLayout = []string{
	"workspaces", "app_menu", "app_title", BarSpace, "clock", BarSpace,
	"tray", "recording", "passwords", "clipboard", "notifications", "dictation", "control_center",
}

// BarLayout is the layout the bar shows: the user's, or the default one
// for none, with the clock at the end while Menu is on. An empty layout of the user's is an empty bar, not the default.
func (b Bar) BarLayout() []string {
	if b.Layout == nil {
		layout := slices.Clone(DefaultBarLayout)
		if b.Menu {
			clock := slices.Index(layout, "clock")
			layout = slices.Delete(layout, clock, clock+2) // clock and its trailing space
			layout = append(layout, "clock")
		}
		return layout
	}
	return slices.Clone(b.Layout)
}

// NormalizeBarLayout leaves out of a layout the names that are not bar
// items and the repeats of an item, the first one staying; spaces stay,
// however many. It says what it left out. nil stays nil (unset), and a
// layout left with nothing is empty, not nil.
func NormalizeBarLayout(layout []string) (out []string, dropped []string) {
	if layout == nil {
		return nil, nil
	}
	out = []string{}
	seen := map[string]bool{}
	for _, id := range layout {
		switch {
		case id == BarSpace:
		case !slices.Contains(BarItems, id), seen[id]:
			dropped = append(dropped, id)
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, dropped
}

// validateBarLayout normalizes bar.layout, with a warning for each name
// left out.
func (c *Config) validateBarLayout() []string {
	var w []string
	out, dropped := NormalizeBarLayout(c.Bar.Layout)
	for _, id := range dropped {
		if slices.Contains(BarItems, id) {
			w = append(w, fmt.Sprintf("bar.layout %q: on the bar already; leaving the repeat out", id))
		} else {
			w = append(w, fmt.Sprintf("bar.layout %q: want one of %s; leaving it out", id, barItemList()))
		}
	}
	c.Bar.Layout = out
	return w
}

func barItemList() string {
	return strings.Join(append(slices.Clone(BarItems), BarSpace), ", ")
}

// barLayoutLine is bar.layout as the file has it: commented out with the
// default while unset, so that the clock follows the menu switch.
func barLayoutLine(b Bar) string {
	if b.Layout == nil {
		return "# layout = " + list(b.BarLayout())
	}
	return "layout = " + list(b.Layout)
}
