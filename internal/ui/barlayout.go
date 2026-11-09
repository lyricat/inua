package ui

import (
	"slices"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/config"
)

// item is the widget of one of config.BarItems on this bar, nil for a
// name it does not know. The widgets come from bar.ui and live as long as
// the bar, the builder's references keeping those off the bar (arrange).
func (b *bar) item(id string) *gtk.Widget {
	switch id {
	case "workspaces":
		return &b.workspaces.Widget
	case "app_menu":
		return &b.appMenu.Widget
	case "app_title":
		return &b.appTitle.Widget
	case "clock":
		return &b.clockButton.Widget
	case "tray":
		return &b.tray.Widget
	case "recording":
		return &b.rec.Widget
	case "passwords":
		return &b.passwords.Widget
	case "clipboard":
		return &b.clipboard.Widget
	case "notifications":
		return &b.notes.Widget
	case "dictation":
		return &b.voice.Widget
	case "control_center":
		return &b.status.Widget
	}
	return nil
}

// shownLayout is the layout the bar shows now.
func (b *bar) shownLayout() []string {
	if b.layout == nil {
		return config.DefaultBarLayout
	}
	return b.layout
}

// arrange places the bar's items as layout (bar.layout) has them. Unset
// (nil), they are where bar.ui has them: the clock centred on the screen
// between the start's items and the end's. A layout of the user's has
// them in the island in its order, its spaces sharing the room left over
// equally (a GtkBox shares it equally between the children that expand);
// the items it leaves out are off the bar, so that what shows them when
// their setting says so (the tray, a recording) cannot bring them back.
// Each item keeps its widget, its state and its handlers wherever it goes.
func (b *bar) arrange(layout []string) {
	if b.arranged && sameLayout(layout, b.layout) {
		return
	}
	if !b.arranged && layout == nil { // bar.ui's places are the default's
		b.arranged = true
		return
	}
	// Off the bar, from where they were; at first, bar.ui's places.
	if b.arranged && b.layout != nil {
		for _, id := range b.layout {
			if w := b.item(id); w != nil {
				b.island.Remove(w)
			}
		}
	} else {
		start := true
		for _, id := range config.DefaultBarLayout {
			switch {
			case id == config.BarSpace:
			case id == "clock":
				b.centered.SetCenterWidget(nil)
				start = false
			case start:
				b.barStart.Remove(b.item(id))
			default:
				b.barEnd.Remove(b.item(id))
			}
		}
	}
	for _, sp := range b.spaces {
		b.island.Remove(&sp.Widget) // the island's was the only reference
	}
	b.spaces = nil
	b.arranged = true
	b.layout = slices.Clone(layout) // nil stays nil, empty empty

	if layout == nil {
		b.centered.SetVisible(true)
		start := true
		for _, id := range config.DefaultBarLayout {
			w := b.item(id)
			if w == nil {
				continue // a space
			}
			setItemMargin(w, 0)
			switch {
			case id == "clock":
				b.centered.SetCenterWidget(w)
				start = false
			case start:
				b.barStart.Append(w)
			default:
				b.barEnd.Append(w)
			}
		}
		return
	}
	b.centered.SetVisible(false)
	for _, id := range layout {
		if id == config.BarSpace {
			sp := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
			sp.AddCssClass("bar-space")
			sp.SetHexpand(true)
			b.island.Append(&sp.Widget)
			sp.Unref() // the island holds it
			b.spaces = append(b.spaces, sp)
			continue
		}
		w := b.item(id)
		if w == nil {
			continue // normalized away already; not a bar item
		}
		// The island's spacing is the end's (6 px); the start's items and
		// the clock keep the start's 14 px from one another.
		margin := int32(0)
		if slices.Contains(barStartItems, id) {
			margin = 4
		}
		setItemMargin(w, margin)
		b.island.Append(w)
	}
}

// barStartItems are the items of the default bar's start and the clock,
// which keep 14 px from one another where the end's keep 6.
var barStartItems = []string{"workspaces", "app_menu", "app_title", "clock"}

// sameLayout reports whether two layouts are one: unset is not empty.
func sameLayout(a, b []string) bool {
	return (a == nil) == (b == nil) && slices.Equal(a, b)
}

func setItemMargin(w *gtk.Widget, m int32) {
	w.SetMarginStart(m)
	w.SetMarginEnd(m)
}

// arrangeBars gives every bar the layout of the settings, closing first
// what hangs from an item that may move: the tray's menu, and the panels
// hanging from the bar under their buttons.
func (s *shellUI) arrangeBars() {
	for _, b := range s.bars {
		if b.arranged && sameLayout(s.cfg.Bar.Layout, b.layout) {
			continue
		}
		if s.tray.menu != nil {
			s.tray.menu.Popdown()
		}
		if s.control.drop.host == b {
			s.closeControl()
		}
		if s.agenda.drop.host == b {
			s.closeAgenda()
		}
		if s.passwords.drop.host == b {
			s.closePasswords()
		}
		b.arrange(s.cfg.Bar.Layout)
		b.hover(-1, -1)
	}
}
