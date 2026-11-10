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
	index   int32 // in the entries, the button's name
	id      int32 // the entry's id, as the application published it
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
	s.checkAppMenuPacks()
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

// checkAppMenuPacks has the menu packs read again off the main thread, and
// the bars' menus redrawn when they changed: a pack file is opened and read
// there, never while GTK waits.
func (s *shellUI) checkAppMenuPacks() {
	s.appmenu.packs.Check(func() {
		gtkx.Post(s.syncAppMenus)
	})
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
	request, ok := s.appmenu.beginSearch(s.snap.FocusedWindow)
	if !ok {
		return
	}
	pid := focusedPID(s.snap)
	if pid <= 0 || s.appmenu.registrar == nil {
		s.appmenu.endSearch(request, appmenu.Registration{})
		return
	}
	go func() {
		reg, _ := s.appmenu.registrar.Focused(pid)
		gtkx.Post(func() {
			if s.appmenu.endSearch(request, reg) {
				s.syncAppMenus()
			}
		})
	}()
}

// beginSearch starts a search for window's menu when it is not the window
// searched for last, or the menus changed since, and reports its number.
// Every search starts unanswered, even one for the same window: that the
// application had no menu says nothing about one it may just have
// published, so a pack's menu waits for this search too. A menu found for
// the same window before stays until the search replaces it; reading or
// clicking it asks the registrar whether the application still has it.
func (a *appMenuState) beginSearch(window string) (uint64, bool) {
	if window == a.window && !a.stale {
		return 0, false
	}
	if window != a.window {
		a.found = appmenu.Registration{}
	}
	a.searched = false
	a.window = window
	a.stale = false
	a.finding++
	return a.finding, true
}

// endSearch records what search request found, and reports whether that
// changed what the bar shows. The end of a search says something even when
// nothing was found: the bar can stop waiting and show a pack's menu. A
// search overtaken by a newer one is dropped.
func (a *appMenuState) endSearch(request uint64, reg appmenu.Registration) bool {
	if a.finding != request {
		return false
	}
	if a.searched && a.found == reg {
		return false
	}
	a.found = reg
	a.searched = true
	return true
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
// real menu that is about to be found. Until the packs are first read
// the bar shows none either: a pack may replace the published menu.
func (s *shellUI) syncAppMenu(b *bar) {
	if !s.cfg.Bar.Menu {
		s.clearAppMenu(b)
		return
	}
	if s.appmenu.packs == nil {
		return
	}
	s.checkAppMenuPacks()
	var want appmenu.Registration
	window := ""
	var pack appmenu.Pack
	matched := false
	w, focused := s.snap.Window(s.snap.FocusedWindow)
	if b.connector() == s.snap.FocusedOutput && focused && s.appmenu.packs.Loaded() {
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
	if !s.takeAppMenu(b, entries) {
		return
	}
	gtkx.Clear(&b.appMenu)
	for _, t := range b.appMenuTop {
		btn := gtk.NewButton()
		btn.SetLabel(t.label)
		btn.SetName(strconv.Itoa(int(t.index)))
		btn.SetFocusable(false)
		btn.SetSensitive(t.enabled)
		btn.ConnectClicked(&s.onAppMenuClicked)
		gtkx.Append(&b.appMenu, &btn.Widget)
	}
	b.appMenu.SetVisible(len(b.appMenuTop) > 0)
}

// takeAppMenu keeps a bar's newly read first level and reports whether its
// buttons are to be made again. One that reads as before, with the same
// entry ids, keeps them and the open menu. One that differs, even only in
// the ids the application gave the same labels in the same places (it
// rebuilt its menu), closes the open menu and drops the submenu reads in
// flight: their items carry the ids of the menu that was.
func (s *shellUI) takeAppMenu(b *bar, entries []appmenu.Entry) bool {
	top := appMenuTops(entries)
	b.appMenuEntries = entries
	if slices.Equal(top, b.appMenuTop) {
		return false
	}
	s.closeAppMenu()
	b.appMenuTop = top
	return true
}

// appMenuTops is the bar's buttons for a menu's first level: its entries
// with a label, separators left out.
func appMenuTops(entries []appmenu.Entry) []appMenuTop {
	var top []appMenuTop
	for i, e := range entries {
		if e.Separator || e.Label == "" {
			continue
		}
		top = append(top, appMenuTop{index: int32(i), id: e.ID, label: e.Label, enabled: e.Enabled, submenu: e.Submenu || len(e.Children) > 0})
	}
	return top
}

func (s *shellUI) bindAppMenuHandlers() {
	s.onAppAboutClose = closeAppAbout
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
