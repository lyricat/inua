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
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
	"github.com/lyricat/inua/internal/uikit"
)

// The application menus, in each monitor's bar the menu of the window it
// speaks for: the first level as buttons, what each opens as a menu read
// when it opens.
type appMenuState struct {
	registrar   *appmenu.Registrar
	a11y        *appmenu.A11y
	packs       *appmenu.Packs
	menu        *gtk.Popover // the open one, borrowed from its button
	menuRequest uint64       // the newest pending submenu read
}

// appMenuSearch is a bar's search for the menu its window published:
// looking for one asks the bus, so it runs off the main thread, once when
// the bar's window changes and again when the registrar or a published
// menu changes. Each monitor has its own, so the focus on one monitor does
// not take the other's menu away.
type appMenuSearch struct {
	window   string               // the window it was found for
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
			// The menus changed: every bar searches again for its window.
			for _, b := range s.bars {
				b.appMenuSearch.stale = true
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

// findAppMenu looks for the menu of a bar's window when that window changed
// since the bar's last search, or the menus changed. The search asks the
// bus, and an application that does not answer holds it for seconds, so it
// runs on a goroutine and its result comes back through gtkx.Post, dropped
// when another search of the bar started meanwhile. A window that is no
// longer the one searched for has no menu until its own is found.
func (s *shellUI) findAppMenu(b *bar) {
	w, _ := shell.BarWindow(s.snap, b.connector())
	request, ok := b.appMenuSearch.beginSearch(w.ID)
	if !ok {
		return
	}
	if w.PID <= 0 || s.appmenu.registrar == nil {
		b.appMenuSearch.endSearch(request, appmenu.Registration{})
		return
	}
	pid := w.PID
	go func() {
		reg, _ := s.appmenu.registrar.Focused(pid)
		gtkx.Post(func() {
			if b.appMenuSearch.endSearch(request, reg) {
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
func (a *appMenuSearch) beginSearch(window string) (uint64, bool) {
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
func (a *appMenuSearch) endSearch(request uint64, reg appmenu.Registration) bool {
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

// barAppMenu is the menu found for a bar's window, and whether the search
// for it has ended: no menu while searched is false means one may still be
// found.
func (s *shellUI) barAppMenu(b *bar) (reg appmenu.Registration, searched bool) {
	s.findAppMenu(b)
	w, ok := shell.BarWindow(s.snap, b.connector())
	if !ok || b.appMenuSearch.window != w.ID {
		return appmenu.Registration{}, false
	}
	return b.appMenuSearch.found, b.appMenuSearch.searched
}

// barPID is the process of the window a bar speaks for, 0 when none.
func (s *shellUI) barPID(b *bar) int {
	w, _ := shell.BarWindow(s.snap, b.connector())
	return w.PID
}

// syncAppMenu gives a bar the menu of the window it speaks for
// (shell.BarWindow): each monitor shows its own window's menu, and the
// focus on one monitor does not take the other's away. It runs whenever
// the focus, the monitors or the registrations change. An application that
// published a menu gets it; one confirmed to have none gets, when its
// window has the focus, the menu its accessibility tree shows, read first
// unless its pack replaces every menu or skips that tree, and else its
// pack's menu, or the generic one when no pack adds anything. Those act on
// the focused window alone, so another monitor's window shows none of
// them. While the search for a published menu, or the reading of the
// accessibility tree, runs the bar shows none, so a pack's commands never
// stand in for a real menu that is about to be found. Until the packs are
// first read the bar shows none either: a pack may replace the published
// menu. Over an empty desktop the bar shows the system menu, which waits
// for nothing.
func (s *shellUI) syncAppMenu(b *bar) {
	system := shell.EmptyDesktop(s.snap, b.connector())
	if s.appmenu.packs == nil && !system {
		return
	}
	s.checkAppMenuPacks()
	var want appmenu.Registration
	window := ""
	pid := 0
	noPress := false
	var pack appmenu.Pack
	matched := false
	w, shown := shell.BarWindow(s.snap, b.connector())
	if !system && shown && s.appmenu.packs.Loaded() {
		pack, matched = s.menuPack(w)
		replace := matched && pack.When == appmenu.PackReplace
		searched := true
		// A pack that replaces the application's menu does not ask the bus.
		if !replace {
			want, searched = s.barAppMenu(b)
		}
		if appMenuSource(want, searched, replace) == appMenuFromPack && w.ID == s.snap.FocusedWindow {
			window = w.ID
			if appMenuTriesA11y(w.PID, replace, pack, matched) {
				pid = w.PID
				noPress = matched && pack.NeverPress
			}
		}
	}
	gen := s.appmenu.packs.Generation()
	if b.appMenuReg == want && b.appMenuWindow == window && b.appMenuGen == gen && b.appMenuPID == pid && b.appMenuNoPress == noPress && b.appMenuSystem == system {
		return
	}
	s.clearAppMenu(b)
	b.appMenuReg, b.appMenuWindow, b.appMenuGen = want, window, gen
	b.appMenuPID, b.appMenuNoPress, b.appMenuSystem = pid, noPress, system
	switch {
	case system:
		s.askHibernate() // for the Power section, by the time it opens
		s.renderAppMenu(b, systemMenuEntries(s.control.canHibernate))
	case want != (appmenu.Registration{}):
		s.loadAppMenu(b, true)
	case pid > 0:
		s.loadA11yMenu(b, pid)
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

// appMenuTriesA11y is whether a window that is to get its pack's or the
// generic menu has the menu of its accessibility tree read first: not
// without a process, nor when a pack replaces every menu or says to skip
// that tree.
func appMenuTriesA11y(pid int, replace bool, pack appmenu.Pack, matched bool) bool {
	return pid > 0 && !replace && !(matched && pack.SkipA11y)
}

// clearAppMenu takes a bar's menu away, with the open menu if it is this
// bar's; what is still being read for it is dropped when it comes.
func (s *shellUI) clearAppMenu(b *bar) {
	if b.appMenuReg != (appmenu.Registration{}) || b.appMenuWindow != "" || b.appMenuSystem {
		s.closeAppMenu()
	}
	b.appMenuReg = appmenu.Registration{}
	b.appMenuWindow = ""
	b.appMenuGen = 0
	b.appMenuPID = 0
	b.appMenuA11y = false
	b.appMenuNoPress = false
	b.appMenuSystem = false
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
	s.readAppMenu(b, about, nil)
}

// readAppMenu is loadAppMenu, with first run on the reading's goroutine
// before the menu is read. A menu read whole is read again only with its
// open menu taken down, and clicks on it wait for the new reading: the
// items of the one before have lost their ids.
func (s *shellUI) readAppMenu(b *bar, about bool, first func()) {
	if b.appMenuReg.Whole {
		s.closeAppMenu()
	}
	if b.appMenuLoading {
		b.appMenuStale = true
		return
	}
	b.appMenuLoading = true
	reg := b.appMenuReg
	go func() {
		if first != nil {
			first()
		}
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
			// A menu read whole and being read again has lost its ids.
			if b.appMenuReg.Whole && b.appMenuLoading {
				return
			}
			if b.appMenuSystem {
				s.showSystemMenu(i, &btn.Widget)
				return
			}
			entry := b.appMenuEntries[i]
			submenu := entry.Submenu || len(entry.Children) > 0
			if b.appMenuReg == (appmenu.Registration{}) {
				switch {
				case b.appMenuA11y && (submenu || appmenu.Fillable(entry)):
					s.showA11yMenu(b, i, &btn.Widget)
				case b.appMenuA11y:
					s.activateA11yEntry(b.appMenuPID, entry)
				case submenu:
					s.showPackedMenu(b, i, &btn.Widget)
				default:
					s.activatePackedEntry(entry)
				}
				return
			}
			if submenu {
				s.showAppMenu(b, b.appMenuReg, entry, &btn.Widget)
			} else {
				s.activateAppMenu(b, b.appMenuReg, entry.ID)
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
// application has it now, filled in if it is one built when asked for. A
// menu read whole opens what its reading holds, read again by nothing.
func (s *shellUI) showAppMenu(b *bar, reg appmenu.Registration, entry appmenu.Entry, w *gtk.Widget) {
	s.appmenu.menuRequest++
	request := s.appmenu.menuRequest
	pid := s.barPID(b)
	w.Ref() // kept until the menu is built
	go func() {
		entries, err := entry.Children, error(nil)
		if !reg.Whole {
			entries, err = s.appmenu.registrar.Submenu(reg, entry.ID)
		}
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

// appMenuShows is whether reg is still the menu a bar shows, found for the
// window it speaks for. It asks nothing of the bus: what reads or clicks a
// menu checks off the main thread, with the registrar's Current, that the
// application still has it.
func (s *shellUI) appMenuShows(b *bar, reg appmenu.Registration) bool {
	if reg == (appmenu.Registration{}) || b.appMenuReg != reg {
		return false
	}
	found, _ := s.barAppMenu(b)
	return found == reg
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

// activateAppMenu clicks an item, if the bar still shows the menu of the
// application it belongs to. An item of a menu read whole is clicked as
// the first step of the bar's next reading: no other reading may come
// between the one its id is from and the click, and none of its items is
// clicked again until the next reading has its ids.
func (s *shellUI) activateAppMenu(b *bar, reg appmenu.Registration, id int32) {
	if !s.appMenuShows(b, reg) {
		return
	}
	pid := s.barPID(b)
	click := func() {
		if !s.appmenu.registrar.Current(reg, pid) {
			return
		}
		if err := s.appmenu.registrar.Activate(reg, id); err != nil {
			log.Printf("appmenu activate: %v", err)
		}
	}
	if reg.Whole {
		for _, b := range s.bars {
			if b.appMenuReg == reg && !b.appMenuLoading {
				s.readAppMenu(b, false, click)
				return
			}
		}
		return
	}
	go func() {
		click()
		gtkx.Post(func() { s.reloadAppMenu(reg) })
	}()
}
