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
	"github.com/lyricat/inua/internal/system"
	"github.com/lyricat/inua/internal/uikit"
)

// Each bar's application menu: the menu of the application its monitor
// speaks for, the first level as buttons, what each opens as a menu read
// when it opens. A menu comes from the Canonical registrar, from an inua
// application's own published menu, or from the application's
// accessibility tree (Firefox, Qt and GTK applications).
type appMenuState struct {
	registrar   *appmenu.Registrar
	a11y        *appmenu.A11y
	menu        *gtk.Popover // the open one, borrowed from its button
	menuRequest uint64       // the newest pending submenu read
}

// appMenuTop is a button of the bar's menu.
type appMenuTop struct {
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
		gtkx.Post(func() { s.reloadAppMenu(reg, 0) })
	})
	go func() {
		if err := s.appmenu.registrar.Run(context.Background()); err != nil {
			log.Printf("appmenu registrar: %v", err)
		}
	}()
}

// focusedPID is the process of the window the compositor has focused.
func focusedPID(snap compositor.Snapshot) int {
	w, _ := snap.Window(snap.FocusedWindow)
	return w.PID
}

func (s *shellUI) syncAppMenus() {
	for _, b := range s.bars {
		s.syncAppMenu(b)
	}
}

// syncAppMenu gives a bar the menu of the application its monitor speaks
// for (the focused window), and none when that application has no menu. It
// runs whenever the focus, the monitors or the registrations change; a
// bar on another monitor keeps its menu while the focus is elsewhere.
func (s *shellUI) syncAppMenu(b *bar) {
	if s.appmenu.registrar == nil {
		return
	}
	var reg appmenu.Registration
	pid := 0
	if b.connector() == s.snap.FocusedOutput {
		pid = focusedPID(s.snap)
		reg, _ = s.appmenu.registrar.ForPID(pid)
		if reg == (appmenu.Registration{}) {
			reg, _ = s.appmenu.registrar.Native(pid)
		}
	}
	if b.appMenuReg == reg && b.appMenuPID == pid {
		return
	}
	s.clearAppMenu(b)
	b.appMenuReg, b.appMenuPID = reg, pid
	switch {
	case reg != (appmenu.Registration{}):
		s.loadAppMenu(b, true)
	case pid > 0:
		s.loadA11yMenu(b, pid)
	}
}

// clearAppMenu takes a bar's menu away, with the open menu if it is this
// bar's; what is still being read for it is dropped when it comes.
func (s *shellUI) clearAppMenu(b *bar) {
	if b.appMenuReg != (appmenu.Registration{}) || b.appMenuPID > 0 {
		s.closeAppMenu()
	}
	b.appMenuReg = appmenu.Registration{}
	b.appMenuPID = 0
	b.appMenuTop = nil
	b.appMenuEntries = nil
	gtkx.Clear(&b.appMenu)
	b.appMenu.SetVisible(false)
}

// reloadAppMenu reads a shown menu's first level again: the application said
// it changed, or an item was activated and may have changed it.
func (s *shellUI) reloadAppMenu(reg appmenu.Registration, pid int) {
	for _, b := range s.bars {
		if reg != (appmenu.Registration{}) && b.appMenuReg == reg {
			s.loadAppMenu(b, false)
		} else if reg == (appmenu.Registration{}) && pid > 0 && b.appMenuPID == pid && b.appMenuReg == (appmenu.Registration{}) {
			s.loadA11yMenu(b, pid)
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
		if err == nil {
			entries = withDbusmenuActions(entries, reg)
		}
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

// loadA11yMenu reads a bar's menu through the application's accessibility
// tree, for toolkits that publish no dbusmenu.
func (s *shellUI) loadA11yMenu(b *bar, pid int) {
	if s.appmenu.a11y == nil {
		s.appmenu.a11y = appmenu.NewA11y()
	}
	a11y := s.appmenu.a11y
	go func() {
		entries, err := a11y.Menu(pid)
		gtkx.Post(func() {
			if b.appMenuPID != pid || b.appMenuReg != (appmenu.Registration{}) {
				return
			}
			if err != nil {
				log.Printf("appmenu accessibility %d: %v", pid, err)
				return
			}
			if !appmenu.FillableMenu(entries) {
				entries = nil
			}
			s.renderAppMenu(b, entries)
		})
	}()
}

// withDbusmenuActions gives registrar entries the endpoint that activates
// them, so a bar can invoke an entry the same way whichever source it came
// from.
func withDbusmenuActions(entries []appmenu.Entry, reg appmenu.Registration) []appmenu.Entry {
	for i := range entries {
		entries[i].Children = withDbusmenuActions(entries[i].Children, reg)
		entries[i].Action = appmenu.Action{Kind: appmenu.ActionDbusmenu, Bus: reg.Service, Path: reg.Path, Index: entries[i].ID}
	}
	return entries
}

// renderAppMenu shows a menu's first level as the bar's buttons. A menu
// that reads as before is left alone, with the menu open under it.
func (s *shellUI) renderAppMenu(b *bar, entries []appmenu.Entry) {
	var top []appMenuTop
	for _, e := range entries {
		if e.Separator || e.Label == "" {
			continue
		}
		top = append(top, appMenuTop{label: e.Label, enabled: e.Enabled, submenu: e.Submenu})
	}
	if slices.Equal(top, b.appMenuTop) {
		b.appMenuEntries = entries
		return
	}
	s.closeAppMenu()
	b.appMenuTop = top
	b.appMenuEntries = entries
	gtkx.Clear(&b.appMenu)
	for i, t := range top {
		btn := gtk.NewButton()
		btn.SetLabel(t.label)
		btn.SetName(strconv.Itoa(i))
		btn.SetFocusable(false)
		btn.SetSensitive(t.enabled)
		btn.ConnectClicked(&s.onAppMenuClicked)
		gtkx.Append(&b.appMenu, &btn.Widget)
	}
	b.appMenu.SetVisible(len(top) > 0)
}

func (s *shellUI) bindAppMenuHandlers() {
	s.onAppMenuClicked = func(btn gtk.Button) {
		i, err := strconv.Atoi(btn.GetName())
		if err != nil {
			return
		}
		for _, b := range s.bars {
			if i < 0 || i >= len(b.appMenuEntries) {
				continue
			}
			entry := b.appMenuEntries[i]
			if len(entry.Children) > 0 || entry.Submenu || (b.appMenuPID > 0 && appmenu.Fillable(entry)) {
				s.showAppMenu(b, i, &btn.Widget)
			} else {
				s.activateEntry(b.connector(), b.appMenuReg, b.appMenuPID, entry)
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
func (s *shellUI) showAppMenu(b *bar, i int, w *gtk.Widget) {
	connector, reg, pid := b.connector(), b.appMenuReg, b.appMenuPID
	if i < 0 || i >= len(b.appMenuEntries) {
		return
	}
	entry := b.appMenuEntries[i]
	s.appmenu.menuRequest++
	request := s.appmenu.menuRequest
	w.Ref() // kept until the menu is built
	go func() {
		var entries []appmenu.Entry
		var err error
		if reg != (appmenu.Registration{}) {
			entries, err = s.appmenu.registrar.Submenu(reg, entry.ID)
			if err == nil {
				entries = withDbusmenuActions(entries, reg)
			}
		} else if s.appmenu.a11y != nil {
			if len(entry.Children) > 0 {
				entries = entry.Children
			} else {
				entries, err = s.appmenu.a11y.FillSection(entry)
			}
		}
		gtkx.Post(func() {
			defer w.Unref()
			if err != nil {
				log.Printf("appmenu open %q: %v", entry.Label, err)
				return
			}
			items := s.appMenuItems(connector, reg, pid, entries)
			// The bar's menu was redrawn or went to another application
			// meanwhile.
			if s.appmenu.menuRequest != request || len(items) == 0 || !w.GetMapped() || !s.appMenuCurrent(connector, reg, pid) {
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

// appMenuCurrent is whether reg (or pid's accessibility menu) is still the
// menu of the application the bar on connector speaks for.
func (s *shellUI) appMenuCurrent(connector string, reg appmenu.Registration, pid int) bool {
	b := s.barByConnector(connector)
	if b == nil || b.appMenuReg != reg || b.appMenuPID != pid {
		return false
	}
	if b.connector() != s.snap.FocusedOutput || focusedPID(s.snap) != pid {
		return false
	}
	return true
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

// appMenuItems turns menu entries into a menu's lines, as the tray's are: a
// toggle's mark, a submenu's lines, a hairline for a separator (none at
// either end or twice); a disabled entry, or a submenu the application left
// empty, greys out.
func (s *shellUI) appMenuItems(connector string, reg appmenu.Registration, pid int, entries []appmenu.Entry) []uikit.Item {
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
		case e.Submenu || len(e.Children) > 0:
			if e.Enabled {
				it.Items = s.appMenuItems(connector, reg, pid, e.Children)
			}
		case e.Enabled:
			entry := e
			it.Run = func() { s.activateEntry(connector, reg, pid, entry) }
		}
		out = append(out, it)
	}
	if len(out) > 0 && out[len(out)-1].Separator {
		out = out[:len(out)-1]
	}
	return out
}

// activateEntry invokes an entry, if the application it belongs to is
// still the one the bar's monitor speaks for.
func (s *shellUI) activateEntry(connector string, reg appmenu.Registration, pid int, e appmenu.Entry) {
	if !s.appMenuCurrent(connector, reg, pid) {
		return
	}
	go func() {
		var err error
		switch e.Action.Kind {
		case appmenu.ActionA11y:
			if s.appmenu.a11y == nil {
				return
			}
			err = s.appmenu.a11y.Activate(e)
		case appmenu.ActionDbusmenu:
			err = s.appmenu.registrar.Activate(appmenu.Registration{Service: e.Action.Bus, Path: e.Action.Path}, e.Action.Index)
		}
		if err != nil {
			log.Printf("appmenu activate: %v", err)
		}
		gtkx.Post(func() { s.reloadAppMenu(reg, pid) })
	}()
}
