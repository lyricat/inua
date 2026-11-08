package ui

import (
	"context"
	"log"
	"path/filepath"
	"slices"
	"strconv"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appmenu"
	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/system"
	"github.com/lyricat/inua/internal/uikit"
)

// The focused application's menu, in the bar of the focused monitor: the
// first level as buttons, what each opens as a menu read when it opens.
type appMenuState struct {
	registrar   *appmenu.Registrar
	packs       *appmenu.Packs
	menu        *gtk.Popover // the open one, borrowed from its button
	menuRequest uint64       // the newest pending submenu read

	// The menu found for the focused window: looking for one asks the bus,
	// so it runs off the main thread, once when the focus moves to another
	// window and again when the registrar or a published menu changes.
	window   string               // the focused window it was found for
	found    appmenu.Registration // its menu, if any
	searched bool                 // the search for window has ended
	finding  uint64               // the newest search
	stale    bool                 // search again for the same window
}

// appMenuTop is a button of the bar's menu.
type appMenuTop struct {
	id      int32
	label   string
	enabled bool
	submenu bool
}

func (s *shellUI) startAppMenu() {
	dev := ""
	if s.opts.UIDir != "" {
		dev = filepath.Join(filepath.Dir(s.opts.UIDir), "share", "inua", "menus")
	}
	s.appmenu.packs = appmenu.NewPacks(dev)
	conn, err := system.SessionBus()
	if err != nil {
		log.Printf("appmenu: %v", err)
		return
	}
	s.appmenu.registrar = appmenu.New(conn)
	s.appmenu.registrar.SetOnChange(func() {
		gtkx.Post(func() {
			s.appmenu.stale = true
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

// findAppMenu looks for the focused window's menu when the focus moved to
// another window since the last search, or the menus changed. The search
// asks the bus, and an application that does not answer holds it for
// seconds, so it runs on a goroutine and its result comes back through
// gtkx.Post, dropped when another search started meanwhile. A window that
// is no longer the one searched for has no menu until its own is found.
func (s *shellUI) findAppMenu() {
	window := s.snap.FocusedWindow
	if window == s.appmenu.window && !s.appmenu.stale {
		return
	}
	if window != s.appmenu.window {
		s.appmenu.found = appmenu.Registration{}
		s.appmenu.searched = false
	}
	s.appmenu.window = window
	s.appmenu.stale = false
	s.appmenu.finding++
	request := s.appmenu.finding
	pid := focusedPID(s.snap)
	if pid <= 0 || s.appmenu.registrar == nil {
		s.appmenu.found = appmenu.Registration{}
		s.appmenu.searched = true
		return
	}
	go func() {
		reg, _ := s.appmenu.registrar.Focused(pid)
		gtkx.Post(func() {
			if s.appmenu.finding != request {
				return
			}
			// The end of a search says something even when nothing was
			// found: the bar can stop waiting and show a pack's menu.
			if s.appmenu.searched && s.appmenu.found == reg {
				return
			}
			s.appmenu.found = reg
			s.appmenu.searched = true
			s.syncAppMenus()
		})
	}()
}

// focusedAppMenu is the menu found for the focused window, and whether
// the search for it has ended: no menu while searched is false means one
// may still be found.
func (s *shellUI) focusedAppMenu() (reg appmenu.Registration, searched bool) {
	s.findAppMenu()
	if s.appmenu.window != s.snap.FocusedWindow {
		return appmenu.Registration{}, false
	}
	return s.appmenu.found, s.appmenu.searched
}

// syncAppMenu gives a bar the menu of the focused application when the bar
// is the focused monitor's, and none otherwise. It runs whenever the focus,
// the monitors or the registrations change. An application that published
// a menu gets it; one confirmed to have none gets its pack's menu, or the
// generic one when no pack adds anything. While the search for a published
// menu runs the bar shows none, so a pack's commands never stand in for a
// real menu that is about to be found.
func (s *shellUI) syncAppMenu(b *bar) {
	if s.appmenu.packs == nil {
		return
	}
	var want appmenu.Registration
	window := ""
	var pack appmenu.Pack
	matched := false
	w, focused := s.snap.Window(s.snap.FocusedWindow)
	if b.connector() == s.snap.FocusedOutput && focused {
		pack, matched = s.menuPack(w)
		replace := matched && pack.When == appmenu.PackReplace
		searched := true
		// A pack that replaces the application's menu does not ask the bus.
		if !replace {
			want, searched = s.focusedAppMenu()
		}
		if appMenuSource(want, searched, replace) == appMenuFromPack {
			window = w.ID
		}
	}
	gen := s.appmenu.packs.Generation()
	if b.appMenuReg == want && b.appMenuWindow == window && b.appMenuGen == gen {
		return
	}
	s.clearAppMenu(b)
	b.appMenuReg, b.appMenuWindow, b.appMenuGen = want, window, gen
	switch {
	case want != (appmenu.Registration{}):
		s.loadAppMenu(b, true)
	case window != "":
		s.renderAppMenu(b, s.packMenuEntries(w, pack, matched))
	}
}

// What a bar's menu is drawn from.
const (
	appMenuNone      = iota // nothing yet: the search for a published menu runs
	appMenuPublished        // the menu the application published
	appMenuFromPack         // a pack's menu, or the generic one
)

// appMenuSource chooses a focused window's menu: found is the menu it
// published, if any, searched whether the search for one has ended, and
// replace whether a pack replaces any published menu.
func appMenuSource(found appmenu.Registration, searched, replace bool) int {
	switch {
	case replace:
		return appMenuFromPack
	case found != (appmenu.Registration{}):
		return appMenuPublished
	case searched:
		return appMenuFromPack
	}
	return appMenuNone
}

// clearAppMenu takes a bar's menu away, with the open menu if it is this
// bar's; what is still being read for it is dropped when it comes.
func (s *shellUI) clearAppMenu(b *bar) {
	if b.appMenuReg != (appmenu.Registration{}) || b.appMenuWindow != "" {
		s.closeAppMenu()
	}
	b.appMenuReg = appmenu.Registration{}
	b.appMenuWindow = ""
	b.appMenuGen = 0
	b.appMenuEntries = nil
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
	for i, e := range entries {
		if e.Separator || e.Label == "" {
			continue
		}
		top = append(top, appMenuTop{id: int32(i), label: e.Label, enabled: e.Enabled, submenu: e.Submenu || len(e.Children) > 0})
	}
	if slices.Equal(top, b.appMenuTop) {
		b.appMenuEntries = entries
		return
	}
	s.closeAppMenu()
	b.appMenuTop = top
	b.appMenuEntries = entries
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
		i, err := strconv.Atoi(btn.GetName())
		if err != nil {
			return
		}
		parent := btn.GetParent()
		if parent == nil {
			return
		}
		for _, b := range s.bars {
			// The buttons of a bar's menu are its box's children; another
			// bar's menu may have a button with the same index.
			if b.appMenu.GoPointer() != parent.GoPointer() || i < 0 || i >= len(b.appMenuEntries) {
				continue
			}
			entry := b.appMenuEntries[i]
			submenu := entry.Submenu || len(entry.Children) > 0
			if b.appMenuReg == (appmenu.Registration{}) {
				if submenu {
					s.showPackedMenu(b, i, &btn.Widget)
				} else {
					s.activatePackedEntry(entry)
				}
				return
			}
			if submenu {
				s.showAppMenu(b.appMenuReg, entry.ID, &btn.Widget)
			} else {
				s.activateAppMenu(b.appMenuReg, entry.ID)
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
func (s *shellUI) showAppMenu(reg appmenu.Registration, id int32, w *gtk.Widget) {
	s.appmenu.menuRequest++
	request := s.appmenu.menuRequest
	pid := focusedPID(s.snap)
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
			items := s.appMenuItems(reg, entries)
			// The bar's menu was redrawn or went to another application
			// meanwhile.
			if s.appmenu.menuRequest != request || len(items) == 0 || !w.GetMapped() || !live || !s.appMenuCurrent(reg) {
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

// appMenuCurrent is whether reg is still the menu found for the focused
// window. It asks nothing of the bus: what reads or clicks a menu checks off
// the main thread, with the registrar's Current, that the application still
// has it.
func (s *shellUI) appMenuCurrent(reg appmenu.Registration) bool {
	found, _ := s.focusedAppMenu()
	return reg != (appmenu.Registration{}) && found == reg
}

// appMenuItems turns dbusmenu entries into a menu's lines, as the tray's
// are: a toggle's mark, a submenu's lines, a hairline for a separator (none
// at either end or twice); a disabled entry, or a submenu the application
// left empty, greys out.
func (s *shellUI) appMenuItems(reg appmenu.Registration, entries []appmenu.Entry) []uikit.Item {
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
				it.Items = s.appMenuItems(reg, e.Children)
			}
		case e.Enabled:
			id := e.ID
			it.Run = func() { s.activateAppMenu(reg, id) }
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
func (s *shellUI) activateAppMenu(reg appmenu.Registration, id int32) {
	if !s.appMenuCurrent(reg) {
		return
	}
	pid := focusedPID(s.snap)
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
