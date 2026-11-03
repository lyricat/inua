package ui

import (
	"strconv"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appmenu"
	"github.com/lyricat/inua/internal/layershell"
)

// A menu cannot be a GTK popover here: the bar is a layer-shell surface, and
// Wayland only lets a popup hang from an xdg_toplevel. The menu is a card in
// its own surface instead, hung under the bar the way the calendar is
// (drop.go).
//
// The card hangs in the bar's own layer (top, not overlay): Hyprland
// arranges the surfaces that reserve space in layer order, so a bar raised
// to overlay while the dock stays on top would be laid out after the dock
// and start at the dock's edge, clear of the screen's corner, for as long as
// the menu is open. The bar keeps its space from windows meanwhile (hold).
//
// Submenus cascade: each one is a column of its own, beside the item it hangs
// from and level with it, as a Mac's menus do. They are columns in the one
// card, not windows of their own, so the whole menu is a single surface.
type menuPanel struct {
	drop     drop
	open     bool
	closing  bool
	win      *gtk.Window
	backdrop *gtk.Box
	card     *gtk.Box // the columns, side by side
	keys     *gtk.EventController
	pid      int
	section  string
	entries  []appmenu.Entry
	stack    []int // the open branch: one index for each submenu shown
	rows     map[string]menuRow
	pairs    []menuPair // a submenu column and the item it hangs from
	next     uint64
	gone     func()
}

// menuPair ties a submenu column to the row it hangs from, so the column can
// be set level with that row once both are laid out.
type menuPair struct {
	row *gtk.Widget
	col *gtk.Box
}

type menuRow struct {
	target appMenuTarget
	expand bool
	path   []int
}

// row heights, as style.css sets them; the columns are laid out from these
// so a submenu can be set level with the item it hangs from.
const (
	menuRowHeight  = 32
	menuLineHeight = 1
)

func (s *shellUI) buildMenuPanel() {
	m := &s.menu
	m.win = gtk.NewWindow()
	m.backdrop = gtk.NewBox(gtk.OrientationVerticalValue, 0)
	m.card = gtk.NewBox(gtk.OrientationHorizontalValue, 4)
	m.card.AddCssClass("menu-panel")
	m.backdrop.Append(&m.card.Widget)
	m.backdrop.SetVexpand(true)
	m.backdrop.SetHexpand(true)
	m.win.SetChild(&m.backdrop.Widget)

	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCaptureValue)
	keys.ConnectKeyPressed(&s.onMenuKey)
	m.win.AddController(&keys.EventController)
	m.keys = &keys.EventController

	click := gtk.NewGestureClick()
	click.SetPropagationPhase(gtk.PhaseCaptureValue)
	click.ConnectPressed(&s.onMenuBackdrop)
	m.backdrop.AddController(&click.EventController)

	m.drop.attach(m.win, &keys.EventController, m.backdrop, &m.card.Widget, layershell.LayerTop, false)
	m.drop.keyboard = layershell.KeyboardNone // the menu never takes the keyboard from the app
	m.win.SetApplication(s.app)
	w := m.win.GoPointer()
	if s.layer {
		layershell.InitForWindow(w)
		layershell.SetNamespace(w, "inua-appmenu")
		layershell.SetLayer(w, layershell.LayerOverlay)
		// This pointer menu must not steal focus or block clicks outside it.
		layershell.SetKeyboardMode(w, layershell.KeyboardNone)
		for _, e := range []layershell.Edge{layershell.EdgeTop, layershell.EdgeBottom, layershell.EdgeLeft, layershell.EdgeRight} {
			layershell.SetAnchor(w, e, true)
		}
		layershell.SetExclusiveZone(w, -1)
		if mon := s.focusedMonitor(); mon != nil {
			layershell.SetMonitor(w, mon.GoPointer())
		}
	}
	if m.rows == nil {
		m.rows = make(map[string]menuRow)
	}
}

// showMenu hangs the application's menu under the button that was clicked,
// opening the section that button names.
func (s *shellUI) showMenu(button *gtk.Widget, target appMenuTarget) {
	m := &s.menu
	if m.win == nil {
		s.buildMenuPanel()
	} else if m.drop.card == nil {
		// Closed since: the card went back into the panel's window.
		m.drop.attach(m.win, m.keys, m.backdrop, &m.card.Widget, layershell.LayerTop, false)
		m.drop.keyboard = layershell.KeyboardNone
	}
	m.pid = target.pid
	m.section = target.entry.Label
	m.entries = target.entry.Children
	m.stack = nil
	m.open = true
	m.closing = false
	s.fillMenu()
	s.dropFromThisBar(&m.drop, target.bar, func(*bar) *gtk.Widget { return button })
	glib.IdleAdd(&s.onMenuLayout, 0)
}

func (s *shellUI) closeMenu() {
	m := &s.menu
	if !m.open || m.closing {
		return
	}
	m.closing = true
	m.drop.close(func() { s.onMenuGone(0) })
}

func (s *shellUI) onMenuGone(uintptr) {
	m := &s.menu
	m.open = false
	m.closing = false
	// Take the card out of the bar, or it stays drawn where it hung.
	m.drop.detach()
	if m.win != nil {
		m.win.SetVisible(false)
	}
	if g := m.gone; g != nil {
		m.gone = nil
		g()
	}
}

// fillMenu draws the section's column, then a column for each submenu open in
// stack, each beside the item it hangs from and level with it.
func (s *shellUI) fillMenu() {
	m := &s.menu
	for ch := m.card.GetFirstChild(); ch != nil; ch = m.card.GetFirstChild() {
		m.card.Remove(ch)
	}
	m.rows = make(map[string]menuRow)
	m.pairs = nil
	m.next = 0
	s.appendMenuColumn(m.entries, nil)
	glib.IdleAdd(&s.onMenuLayout, 0)
}

// appendMenuColumn adds one column of entries at prefix, its top level with
// the item it hangs from in the column before it. It returns the offsets the
// column's items begin at, so a submenu can be set level with its parent item.
func (s *shellUI) appendMenuColumn(entries []appmenu.Entry, prefix []int) *gtk.Box {
	m := &s.menu
	col := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	col.AddCssClass("menu-col")
	if len(prefix) > 0 {
		// A submenu is a menu of its own: the same card as its parent's,
		// standing beside it, not a column of the parent's card.
		col.AddCssClass("menu-sub")
	}
	m.card.Append(&col.Widget)

	for i, entry := range entries {
		if entry.Separator {
			// A hairline in its own row: the separator's margins are the
			// room around it, so it spans the column and not the window.
			line := gtk.NewSeparator(gtk.OrientationHorizontalValue)
			line.AddCssClass("menu-line")
			line.SetSizeRequest(-1, menuLineHeight)
			col.Append(&line.Widget)
			continue
		}
		path := append(append([]int{}, prefix...), i)

		box := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		box.AddCssClass("menu-row")
		label := gtk.NewLabel(entry.Label)
		label.SetXalign(0)
		label.SetHexpand(true)
		box.Append(&label.Widget)
		if len(entry.Children) > 0 {
			mark := gtk.NewLabel("›")
			mark.AddCssClass("menu-mark")
			box.Append(&mark.Widget)
		}
		button := gtk.NewButton()
		button.SetChild(&box.Widget)
		button.SetHasFrame(false)
		button.SetFocusable(false)
		button.SetSizeRequest(-1, menuRowHeight)
		button.AddCssClass("menu-item")
		if !entry.Enabled && len(entry.Children) == 0 {
			button.SetSensitive(false)
		}
		m.next++
		token := strconv.FormatUint(m.next, 10)
		button.SetName(token)
		m.rows[token] = menuRow{target: appMenuTarget{entry: entry, pid: m.pid}, expand: len(entry.Children) > 0, path: path}
		button.ConnectClicked(&s.onMenuRow)
		motion := gtk.NewEventControllerMotion()
		motion.ConnectEnter(&s.onMenuHover)
		button.AddController(&motion.EventController)
		col.Append(&button.Widget)

		// The branch open at this item gets its own column; the layout pass
		// sets it level with the row.
		if len(entry.Children) > 0 && equalPath(m.stack, path) {
			sub := s.appendMenuColumn(entry.Children, path)
			m.pairs = append(m.pairs, menuPair{row: &button.Widget, col: sub})
		}
	}
	return col
}

func equalPath(stack, path []int) bool {
	if len(stack) != len(path) {
		return false
	}
	for i := range stack {
		if stack[i] != path[i] {
			return false
		}
	}
	return true
}

func hasPrefix(path, prefix []int) bool {
	if len(path) <= len(prefix) {
		return false
	}
	for i := range prefix {
		if path[i] != prefix[i] {
			return false
		}
	}
	return true
}

func (s *shellUI) bindMenuHandlers() {
	s.onMenuLayout = func(uintptr) bool { return s.layoutMenu() }
	// A click on an item with a submenu opens it; on any other, it acts.
	s.onMenuRow = func(button gtk.Button) {
		m := &s.menu
		row, ok := m.rows[button.GetName()]
		if !ok {
			return
		}
		if row.expand {
			if equalPath(m.stack, row.path) {
				m.stack = m.stack[:len(row.path)-1]
			} else {
				m.stack = row.path
			}
			s.fillMenu()
			return
		}
		s.activateAppMenu(row.target)
		s.closeMenu()
	}
	// The pointer entering an item opens its submenu and closes any other, as
	// a Mac's menus do; entering one without a submenu closes the deeper ones.
	s.onMenuHover = func(motion gtk.EventControllerMotion, _, _ float64) {
		m := &s.menu
		w := motion.GetWidget()
		if w == nil {
			return
		}
		row, ok := m.rows[w.GetName()]
		if !ok {
			return
		}
		switch {
		case row.expand:
			if equalPath(m.stack, row.path) {
				return
			}
			m.stack = row.path
			s.fillMenu()
		case hasPrefix(m.stack, row.path):
			// A plain item beside an open branch closes the deeper menus.
			m.stack = m.stack[:len(row.path)-1]
			s.fillMenu()
		}
	}
	s.onMenuBackdrop = func(g gtk.GestureClick, _ int32, x, y float64) {
		if s.barClick(s.barUnder(g.GetWidget()), x, y) {
			return // the bar takes it: another section, or this one again
		}
		m := &s.menu
		hit := m.backdrop.Pick(x, y, gtk.PickDefaultValue)
		if hit == nil {
			return
		}
		outside := hit.GoPointer() != m.card.GoPointer() && !hit.IsAncestor(&m.card.Widget)
		hit.Unref()
		if outside {
			s.closeMenu()
		}
	}
	s.onMenuKey = func(_ gtk.EventControllerKey, key uint32, _ uint32, _ gdk.ModifierType) bool {
		m := &s.menu
		switch key {
		case uint32(gdk.KEY_Escape):
			if len(m.stack) > 0 {
				m.stack = m.stack[:len(m.stack)-1]
				s.fillMenu()
				return true
			}
			s.closeMenu()
			return true
		case uint32(gdk.KEY_Left):
			if len(m.stack) > 0 {
				m.stack = m.stack[:len(m.stack)-1]
				s.fillMenu()
				return true
			}
		}
		return false
	}
}

// menuOwnedBy reports whether the open menu belongs to pid.
func (s *shellUI) menuOwnedBy(pid int) bool {
	return s.menu.open && s.menu.pid == pid
}

// layoutMenu sets each submenu column level with the row it hangs from, once
// the columns have been laid out: the offsets come from GTK's own measurement
// rather than from the style sheet's row heights.
func (s *shellUI) layoutMenu() bool {
	m := &s.menu
	for _, pair := range m.pairs {
		var r graphene.Rect
		if !pair.row.ComputeBounds(&m.card.Widget, &r) {
			continue
		}
		box := *(*[4]float32)(unsafe.Pointer(&r))
		pair.col.SetMarginTop(int32(box[1]))
	}
	return false
}
