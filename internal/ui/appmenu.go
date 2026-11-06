package ui

import (
	"context"
	"log"
	"slices"
	"strconv"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appmenu"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
	"github.com/lyricat/inua/internal/uikit"
)

// Each bar's application menu: the menu of the application its monitor
// speaks for, the first level as buttons, what each opens as a menu read
// when it opens.
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
	s.appmenu.registrar.SetOnChange(func() { gtkx.Post(s.syncAppMenus) })
	s.appmenu.registrar.SetOnUpdate(func(reg appmenu.Registration) {
		gtkx.Post(func() { s.reloadAppMenu(reg) })
	})
	go func() {
		if err := s.appmenu.registrar.Run(context.Background()); err != nil {
			log.Printf("appmenu registrar: %v", err)
		}
	}()
}

func (s *shellUI) syncAppMenus() {
	for _, b := range s.bars {
		s.syncAppMenu(b)
	}
}

// syncAppMenu gives a bar the menu of the application its monitor speaks
// for (shell.BarWindow), and none when that application has no menu. It
// runs whenever the focus, the monitors or the registrations change; a
// bar on another monitor keeps its menu while the focus is elsewhere.
func (s *shellUI) syncAppMenu(b *bar) {
	if s.appmenu.registrar == nil {
		return
	}
	var want appmenu.Registration
	if w, ok := shell.BarWindow(s.snap, b.connector()); ok {
		want, _ = s.appmenu.registrar.ForPID(w.PID)
	}
	if b.appMenuReg == want {
		return
	}
	s.clearAppMenu(b)
	b.appMenuReg = want
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
		for _, b := range s.bars {
			i := slices.IndexFunc(b.appMenuTop, func(t appMenuTop) bool { return int(t.id) == id })
			if i < 0 || b.appMenuReg == (appmenu.Registration{}) {
				continue
			}
			if b.appMenuTop[i].submenu {
				s.showAppMenu(b, int32(id), &btn.Widget)
			} else {
				s.activateAppMenu(b.connector(), b.appMenuReg, int32(id))
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
func (s *shellUI) showAppMenu(b *bar, id int32, w *gtk.Widget) {
	connector, reg := b.connector(), b.appMenuReg
	s.appmenu.menuRequest++
	request := s.appmenu.menuRequest
	w.Ref() // kept until the menu is built
	go func() {
		entries, err := s.appmenu.registrar.Submenu(reg, id)
		gtkx.Post(func() {
			defer w.Unref()
			if err != nil {
				log.Printf("appmenu %s: %v", reg.Service, err)
				return
			}
			items := s.appMenuItems(connector, reg, entries)
			// The bar's menu was redrawn or went to another application
			// meanwhile.
			if s.appmenu.menuRequest != request || len(items) == 0 || !w.GetMapped() || !s.appMenuCurrent(connector, reg) {
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

// appMenuCurrent is whether reg is still the menu of the application the
// bar on connector speaks for.
func (s *shellUI) appMenuCurrent(connector string, reg appmenu.Registration) bool {
	b := s.barByConnector(connector)
	if b == nil || b.appMenuReg != reg {
		return false
	}
	w, ok := shell.BarWindow(s.snap, connector)
	if !ok {
		return false
	}
	current, ok := s.appmenu.registrar.ForPID(w.PID)
	return ok && current == reg
}

// barByConnector is the bar on a monitor, or nil when the monitor is gone.
func (s *shellUI) barByConnector(connector string) *bar {
	for _, b := range s.bars {
		if b.connector() == connector {
			return b
		}
	}
	return nil
}

// appMenuItems turns dbusmenu entries into a menu's lines, as the tray's
// are: a toggle's mark, a submenu's lines, a hairline for a separator (none
// at either end or twice); a disabled entry, or a submenu the application
// left empty, greys out.
func (s *shellUI) appMenuItems(connector string, reg appmenu.Registration, entries []appmenu.Entry) []uikit.Item {
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
				it.Items = s.appMenuItems(connector, reg, e.Children)
			}
		case e.Enabled:
			id := e.ID
			it.Run = func() { s.activateAppMenu(connector, reg, id) }
		}
		out = append(out, it)
	}
	if len(out) > 0 && out[len(out)-1].Separator {
		out = out[:len(out)-1]
	}
	return out
}

// activateAppMenu clicks an item, if the application it belongs to is still
// the one the bar's monitor speaks for.
func (s *shellUI) activateAppMenu(connector string, reg appmenu.Registration, id int32) {
	if !s.appMenuCurrent(connector, reg) {
		return
	}
	go func() {
		if err := s.appmenu.registrar.Activate(reg, id); err != nil {
			log.Printf("appmenu activate: %v", err)
		}
		gtkx.Post(func() { s.reloadAppMenu(reg) })
	}()
}
