package ui

import (
	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"
)

// narrowSettingsWidth is where the settings window stops fitting two
// columns comfortably: below it the index is an icon rail.
const narrowSettingsWidth = 900

// watchSettingsWidth follows the settings window's width so that a small
// screen keeps the page's controls in view. It also takes a window wider
// than its monitor down to the monitor's width.
func (s *shellUI) watchSettingsWidth() {
	st := &s.settings
	if st.win == nil {
		return
	}
	st.onSettingsLayout = func(_ gdk.Surface, width, _ int32) {
		s.setSettingsNarrow(int(width) < narrowSettingsWidth)
	}
	st.onSettingsRealize = func(gtk.Widget) { s.watchSettingsSurface() }
	if st.win.GetSurface() != nil {
		s.watchSettingsSurface()
		return
	}
	st.win.ConnectRealize(&st.onSettingsRealize)
}

// watchSettingsSurface fits the window to its monitor and starts following
// its width, once the surface exists.
func (s *shellUI) watchSettingsSurface() {
	st := &s.settings
	sf := st.win.GetSurface()
	if sf == nil {
		return
	}
	available := int32(0)
	if mon := gdk.DisplayGetDefault().GetMonitorAtSurface(sf); mon != nil {
		var g gdk.Rectangle
		mon.GetGeometry(&g)
		available = g.Width - 16
		var width, height int32
		st.win.GetDefaultSize(&width, &height)
		if g.Width > 32 && width > available {
			st.win.SetDefaultSize(available, min(height, g.Height-16))
		}
	}
	sf.ConnectLayout(&st.onSettingsLayout)
	width := st.win.GetWidth()
	if width == 0 {
		width = available
	}
	// The window's own width is the truth once laid out; the monitor's
	// decides before the first frame, so a small screen starts collapsed
	// instead of after the window has already opened too wide.
	s.setSettingsNarrow(int(width) < narrowSettingsWidth)
}

// setSettingsNarrow turns the index into an icon rail while the window is
// narrow, and lets the page's titles and lines ellipsize so that the
// controls at their end stay in view. Returning to a wide window restores
// the labels and the wrapped descriptions.
func (s *shellUI) setSettingsNarrow(on bool) {
	st := &s.settings
	if st.win == nil || st.narrow == on {
		return
	}
	st.narrow = on
	toggle(&st.win.Widget, "narrow", on)
	for _, l := range st.indexLabels {
		l.SetVisible(!on)
	}
	for _, h := range st.indexHeads {
		h.SetVisible(!on)
	}
	st.indexTitle.SetVisible(!on)
	st.search.entry.SetVisible(!on)
	if on {
		st.rail.SetSizeRequest(56, -1)
	} else {
		st.rail.SetSizeRequest(220, -1)
	}
	for _, name := range settingsPages {
		if child := st.pages.GetChildByName(name); child != nil {
			setSettingsLabelsNarrow(child, on)
		}
	}
}

// setSettingsLabelsNarrow ellipsizes the rows' titles and lines of a page
// on a narrow window, and wraps the descriptions again on a wide one.
func setSettingsLabelsNarrow(w *gtk.Widget, on bool) {
	for c := w.GetFirstChild(); c != nil; c = c.GetNextSibling() {
		if c.HasCssClass("title") || c.HasCssClass("sub") {
			l := gtk.LabelNewFromInternalPtr(c.GoPointer())
			if on {
				l.SetEllipsize(pango.EllipsizeEndValue)
				l.SetWrap(false)
			} else if c.HasCssClass("sub") {
				l.SetWrap(true)
			}
		}
		setSettingsLabelsNarrow(c, on)
	}
}
