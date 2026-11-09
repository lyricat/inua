package ui

import (
	"context"
	"log"
	"slices"
	"strconv"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appmenu"
	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
	"github.com/lyricat/inua/internal/uikit"
)

// The focused application's menu, in the bar of the focused monitor: the
// first level as buttons, what each opens as a menu read when it opens.
type appMenuState struct {
	registrar   *appmenu.Registrar
	menu        *gtk.Popover // the open one, borrowed from its button
	menuRequest uint64       // the newest pending submenu read

}

// appMenuTop is a button of the bar's menu.
type appMenuTop struct {
	id      int32
	label   string
	enabled bool
	submenu bool
}

func (s *shellUI) startAppMenu() {
	conn, err := system.SessionBus()
	if err != nil {
		log.Printf("appmenu: %v", err)
		return
	}
	s.appmenu.registrar = appmenu.New(conn)
	s.appmenu.registrar.SetOnChange(func() {
		gtkx.Post(func() {
			// The menus changed: every bar searches again for its window.
			for _, b := range s.bars {
				b.appMenuWindow = ""
			}
			s.syncAppMenus()
		})
	})
	s.appmenu.registrar.SetOnUpdate(func(reg appmenu.Registration) {
		gtkx.Post(func() { s.reloadAppMenu(reg) })
	})
	go func() {
		if err := s.appmenu.registrar.Run(context.Background()); err != nil {
			log.Printf("appmenu registrar: %v", err)
		}
	}()
}

func focusedPID(snap compositor.Snapshot) int {
	w, _ := snap.Window(snap.FocusedWindow)
	return w.PID
}

func (s *shellUI) syncAppMenus() {
	for _, b := range s.bars {
		s.syncAppMenu(b)
	}
}

// findAppMenu looks for a bar's window menu when that window changed since
// the last search, or the menus changed. Each monitor has its own search and
// its own menu: focus on one monitor does not take the other's away. The
// search asks the bus, and an application that does not answer holds it for
// seconds, so it runs on a goroutine and its result comes back through
// gtkx.Post, dropped when another search started meanwhile.
func (s *shellUI) findAppMenu(b *bar) {
	w, ok := shell.BarWindow(s.snap, b.connector())
	window := ""
	pid := 0
	if ok {
		window, pid = w.ID, w.PID
	}
	if window == b.appMenuWindow {
		return
	}
	b.appMenuWindow = window
	b.appMenuSearch = appmenu.Registration{}
	b.appMenuFinding++
	request := b.appMenuFinding
	if pid <= 0 {
		return
	}
	go func() {
		reg, _ := s.appmenu.registrar.Focused(pid)
		gtkx.Post(func() {
			if b.appMenuFinding != request || b.appMenuSearch == reg {
				return
			}
			b.appMenuSearch = reg
			s.syncAppMenus()
		})
	}()
}

// focusedAppMenu is the menu found for a bar's window, if the search for it
// has ended.
func (s *shellUI) focusedAppMenu(b *bar) appmenu.Registration {
	s.findAppMenu(b)
	w, ok := shell.BarWindow(s.snap, b.connector())
	if !ok || w.ID != b.appMenuWindow {
		return appmenu.Registration{}
	}
	return b.appMenuSearch
}

// syncAppMenu gives a bar the menu of the application its monitor shows. It
// runs whenever the focus, the monitors or the registrations change.
func (s *shellUI) syncAppMenu(b *bar) {
	if s.appmenu.registrar == nil {
		return
	}
	var want appmenu.Registration
	pid := 0
	if w, ok := shell.BarWindow(s.snap, b.connector()); ok {
		pid = w.PID
		want = s.focusedAppMenu(b)
	}
	if b.appMenuReg == want && b.appMenuPID == pid {
		return
	}
	s.clearAppMenu(b)
	b.appMenuReg, b.appMenuPID = want, pid
	if want != (appmenu.Registration{}) {
		s.loadAppMenu(b, true)
	}
}

// clearAppMenu takes a bar's menu away, with the open menu if it is this
// bar's; what is still being read for it is dropped when it comes.
func (s *shellUI) clearAppMenu(b *bar) {
	if b.appMenuReg != (appmenu.Registration{}) {
		s.closeAppMenu()
	}
	b.appMenuReg = appmenu.Registration{}
	b.appMenuPID = 0
	b.appMenuTop = nil
	gtkx.Clear(&b.appMenu)
	b.appMenu.SetVisible(false)
}

// reloadAppMenu reads a shown menu's first level again: the application said
// it changed, or an item was activated and may have changed it.
func (s *shellUI) reloadAppMenu(reg appmenu.Registration) {
	for _, b := range s.bars {
		if b.appMenuReg == reg {
			s.loadAppMenu(b, false)
		}
	}
}

// loadAppMenu reads the first level of a bar's menu and shows it. One
// reading runs at a time for a bar: what changes meanwhile is read again
// when it ends, so an application that changes its menu all the time gets a
// call after a call, not one for each change.
func (s *shellUI) loadAppMenu(b *bar, about bool) {
	if b.appMenuLoading {
		b.appMenuStale = true
		return
	}
	b.appMenuLoading = true
	reg := b.appMenuReg
	go func() {
		entries, err := s.appmenu.registrar.Menu(reg, about)
		gtkx.Post(func() {
			b.appMenuLoading = false
			if b.appMenuStale || b.appMenuReg != reg {
				b.appMenuStale = false
				if b.appMenuReg != (appmenu.Registration{}) {
					s.loadAppMenu(b, b.appMenuReg != reg)
				}
				return
			}
			if err != nil {
				log.Printf("appmenu %s: %v", reg.Service, err)
				return
			}
			s.renderAppMenu(b, entries)
		})
	}()
}

// renderAppMenu shows a menu's first level as the bar's buttons. A menu
// that reads as before is left alone, with the menu open under it.
func (s *shellUI) renderAppMenu(b *bar, entries []appmenu.Entry) {
	var top []appMenuTop
	for _, e := range entries {
		if e.Separator || e.Label == "" {
			continue
		}
		top = append(top, appMenuTop{id: e.ID, label: e.Label, enabled: e.Enabled, submenu: e.Submenu})
	}
	if slices.Equal(top, b.appMenuTop) {
		return
	}
	s.closeAppMenu()
	b.appMenuTop = top
	gtkx.Clear(&b.appMenu)
	for _, t := range top {
		btn := gtk.NewButton()
		btn.SetLabel(t.label)
		btn.SetName(strconv.Itoa(int(t.id)))
		btn.SetFocusable(false)
		btn.SetSensitive(t.enabled)
		btn.ConnectClicked(&s.onAppMenuClicked)
		gtkx.Append(&b.appMenu, &btn.Widget)
	}
	b.appMenu.SetVisible(len(top) > 0)
}

func (s *shellUI) bindAppMenuHandlers() {
	s.onAppMenuClicked = func(btn gtk.Button) {
		id, err := strconv.Atoi(btn.GetName())
		if err != nil {
			return
		}
		parent := btn.GetParent()
		if parent == nil {
			return
		}
		for _, b := range s.bars {
			// The buttons of a bar's menu are its box's children; another
			// bar's menu may have a button with the same id.
			if b.appMenu.GoPointer() != parent.GoPointer() {
				continue
			}
			i := slices.IndexFunc(b.appMenuTop, func(t appMenuTop) bool { return int(t.id) == id })
			if i < 0 || b.appMenuReg == (appmenu.Registration{}) {
				continue
			}
			if b.appMenuTop[i].submenu {
				s.showAppMenu(b, b.appMenuReg, int32(id), &btn.Widget)
			} else {
				s.activateAppMenu(b, b.appMenuReg, int32(id))
			}
			return
		}
	}
	s.onAppMenuClosed = func(p gtk.Popover) {
		// One closeAppMenu took down is no longer the open one, and goes
		// there. Unparenting inside the signal is unsafe; the parent's
		// reference was the last one, so this frees the popover.
		if s.appmenu.menu == nil || s.appmenu.menu.GoPointer() != p.GoPointer() {
			return
		}
		s.appmenu.menu = nil
		s.appmenu.menuRequest++
		gtkx.Post(func() { p.Unparent() })
	}
}

// closeAppMenu takes the open menu down and off its button at once: the
// button it hangs from is about to go, or another menu to open.
func (s *shellUI) closeAppMenu() {
	s.appmenu.menuRequest++
	p := s.appmenu.menu
	if p == nil {
		return
	}
	s.appmenu.menu = nil
	p.Popdown()
	p.Unparent()
}

// showAppMenu reads what a button of the bar's menu opens and pops it up
// under the button w. It is read each time, so it is the menu as the
// application has it now, filled in if it is one built when asked for.
func (s *shellUI) showAppMenu(b *bar, reg appmenu.Registration, id int32, w *gtk.Widget) {
	s.appmenu.menuRequest++
	request := s.appmenu.menuRequest
	pid := b.appMenuPID
	w.Ref() // kept until the menu is built
	go func() {
		entries, err := s.appmenu.registrar.Submenu(reg, id)
		live := err == nil && s.appmenu.registrar.Current(reg, pid)
		gtkx.Post(func() {
			defer w.Unref()
			if err != nil {
				log.Printf("appmenu %s: %v", reg.Service, err)
				return
			}
			items := s.appMenuItems(b, reg, entries)
			// The bar's menu was redrawn or went to another application
			// meanwhile.
			if s.appmenu.menuRequest != request || len(items) == 0 || !w.GetMapped() || !live || !s.appMenuShows(b, reg) {
				return
			}
			s.closeAppMenu()
			pop := uikit.NewMenu(items, uikit.Below)
			pop.SetParent(w)
			pop.ConnectClosed(&s.onAppMenuClosed)
			pop.Unref() // the parent holds it now
			pop.Popup()
			s.appmenu.menu = pop
		})
	}()
}

// appMenuShows is whether reg is still the menu of the window the bar shows.
// It asks nothing of the bus: what reads or clicks a menu checks off the main
// thread, with the registrar's Current, that the application still has it.
func (s *shellUI) appMenuShows(b *bar, reg appmenu.Registration) bool {
	if reg == (appmenu.Registration{}) || b.appMenuReg != reg {
		return false
	}
	w, ok := shell.BarWindow(s.snap, b.connector())
	return ok && w.PID == b.appMenuPID
}

// appMenuItems turns dbusmenu entries into a menu's lines, as the tray's
// are: a toggle's mark, a submenu's lines, a hairline for a separator (none
// at either end or twice); a disabled entry, or a submenu the application
// left empty, greys out.
func (s *shellUI) appMenuItems(b *bar, reg appmenu.Registration, entries []appmenu.Entry) []uikit.Item {
	var out []uikit.Item
	for _, e := range entries {
		if e.Separator {
			if len(out) > 0 && !out[len(out)-1].Separator {
				out = append(out, uikit.Item{Separator: true})
			}
			continue
		}
		it := uikit.Item{Label: e.Label}
		switch {
		case e.Toggle == "checkmark" && e.Checked:
			it.Mark = "check"
		case e.Toggle == "radio" && e.Checked:
			it.Mark = "radio"
		case e.Toggle != "":
			it.Mark = "off"
		}
		switch {
		case e.Submenu:
			if e.Enabled {
				it.Items = s.appMenuItems(b, reg, e.Children)
			}
		case e.Enabled:
			id := e.ID
			it.Run = func() { s.activateAppMenu(b, reg, id) }
		}
		out = append(out, it)
	}
	if len(out) > 0 && out[len(out)-1].Separator {
		out = out[:len(out)-1]
	}
	return out
}

// activateAppMenu clicks an item, if the application it belongs to still
// has the focus.
func (s *shellUI) activateAppMenu(b *bar, reg appmenu.Registration, id int32) {
	if !s.appMenuShows(b, reg) {
		return
	}
	pid := b.appMenuPID
	go func() {
		if !s.appmenu.registrar.Current(reg, pid) {
			return
		}
		if err := s.appmenu.registrar.Activate(reg, id); err != nil {
			log.Printf("appmenu activate: %v", err)
		}
		gtkx.Post(func() { s.reloadAppMenu(reg) })
	}()
}
