package ui

import (
	"context"
	"log"
	"slices"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appmenu"
	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/shell"
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
	var reg appmenu.Registration
	pid := 0
	if w, ok := shell.BarWindow(s.snap, b.connector()); ok {
		pid = w.PID
		reg, _ = s.appmenu.registrar.ForPID(pid)
		if reg == (appmenu.Registration{}) {
			reg, _ = s.appmenu.registrar.Native(pid)
		}
		if reg == (appmenu.Registration{}) {
			reg, _ = s.appmenu.registrar.Firefox(pid)
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
		var entries []appmenu.Entry
		var err error
		if appmenu.IsFirefoxService(reg) {
			// Firefox's item ids change with every layout read, so read the
			// whole tree once and keep its children for opening.
			entries, err = s.appmenu.registrar.MenuFull(reg, about)
		} else {
			entries, err = s.appmenu.registrar.Menu(reg, about)
		}
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
		var entries []appmenu.Entry
		var err error
		// Electron and GTK applications build their accessibility tree a
		// moment after the window is mapped; reading once too early would
		// settle on the generic menu for an application that has one.
		for try := 0; try < 4; try++ {
			entries, err = a11y.Menu(pid)
			if appmenu.FillableMenu(entries) {
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
		gtkx.Post(func() {
			if b.appMenuPID != pid || b.appMenuReg != (appmenu.Registration{}) {
				return
			}
			if err != nil {
				log.Printf("appmenu accessibility %d: %v", pid, err)
			}
			// Firefox's AT-SPI menubar is a row of named sections with no
			// items; pressing a section to make it build them ends Firefox.
			// It gets the generic menu instead, and its sections are never
			// pressed.
			if !appmenu.FillableMenu(entries) || s.appMenuUnreadable(pid) {
				entries = s.genericAppSections(pid)
				if len(entries) > 0 && !s.appMenuUnreadable(pid) && !b.appMenuRetried {
					// The tree may still be building: look once more, and
					// replace the generic menu if the application's shows up.
					b.appMenuRetried = true
					time.AfterFunc(1500*time.Millisecond, func() {
						gtkx.Post(func() {
							if b.appMenuPID == pid && b.appMenuReg == (appmenu.Registration{}) {
								s.loadA11yMenu(b, pid)
							}
						})
					})
				}
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

// openAppTitleMenu hangs the menu every window has (About, Close, Quit,
// Fullscreen, Float) under the application's icon and name.
func (s *shellUI) openAppTitleMenu(b *bar) {
	if b.appPopover.GoPointer() != 0 {
		if b.appPopover.GetVisible() {
			b.appPopover.Popdown()
			return
		}
		b.appPopover.Unparent()
		b.appPopover = gtk.Popover{}
	}
	w, ok := shell.BarWindow(s.snap, b.connector())
	if !ok {
		return
	}
	sections := s.genericAppSections(w.PID)
	if len(sections) == 0 {
		return
	}
	items := s.appMenuItems(b.connector(), appmenu.Registration{}, w.PID, sections[0].Children)
	if len(items) == 0 {
		return
	}
	pop := uikit.NewMenu(items, uikit.Below)
	pop.SetParent(&b.appButton.Widget)
	pop.Unref() // the button owns it
	pop.Popup()
	b.appPopover = *pop
}

// appMenuUnreadable reports whether pid's menu bar cannot be read: VS Code
// draws its menu bar itself and exposes only an overflow item on the
// accessibility bus.
func (s *shellUI) appMenuUnreadable(pid int) bool {
	w, ok := s.appWindowOf(pid)
	if !ok {
		return false
	}
	id := strings.ToLower(w.AppID)
	if id == "code" || id == "vscode" {
		return true
	}
	if e := s.dockState.apps.ForClass(w.AppID); e != nil {
		id = strings.ToLower(e.ID)
		return id == "code" || strings.HasSuffix(id, ".code")
	}
	return false
}

// appMenuNeverPress reports whether an entry's accessibility action must
// never be pressed: pressing a Firefox section ends Firefox.
func (s *shellUI) appMenuNeverPress(pid int) bool {
	w, ok := s.appWindowOf(pid)
	if !ok {
		return false
	}
	if strings.Contains(strings.ToLower(w.AppID), "firefox") {
		return true
	}
	if e := s.dockState.apps.ForClass(w.AppID); e != nil {
		return strings.Contains(strings.ToLower(e.ID), "firefox")
	}
	return false
}

// appWindowOf is the window of process pid the bar names.
func (s *shellUI) appWindowOf(pid int) (compositor.Window, bool) {
	for _, w := range s.snap.Windows {
		if w.PID == pid {
			return w, true
		}
	}
	return compositor.Window{}, false
}

// appNameOf is what the bar calls an application: its desktop entry's
// name, else its class.
func (s *shellUI) appNameOf(w compositor.Window) string {
	if e := s.dockState.apps.ForClass(w.AppID); e != nil {
		return e.Name
	}
	return w.AppID
}

// genericAppSections is the menu an application without one of its own is
// given: the commands that work for every window, in the places a Mac puts
// them. Their actions ask the shell or the compositor, so they do not
// depend on the toolkit's own menu model.
func (s *shellUI) genericAppSections(pid int) []appmenu.Entry {
	w, ok := s.appWindowOf(pid)
	if !ok {
		return nil
	}
	name := s.appNameOf(w)
	action := func(label, kind, window string) appmenu.Entry {
		return appmenu.Entry{Label: label, Enabled: true, Action: appmenu.Action{Kind: kind, Bus: window, Index: int32(pid)}}
	}
	return []appmenu.Entry{{
		Label:   i18n.T("Application"),
		Enabled: true,
		Children: []appmenu.Entry{
			action(i18n.Tf("About %s", name), appmenu.ActionAbout, w.ID),
			action(i18n.T("Close Window"), appmenu.ActionCloseWindow, w.ID),
			action(i18n.Tf("Quit %s", name), appmenu.ActionQuitApp, w.ID),
			{Separator: true},
			action(i18n.T("Fullscreen"), appmenu.ActionFullscreen, w.ID),
			action(i18n.T("Float"), appmenu.ActionFloat, w.ID),
		},
	}}
}

// showAppAbout says what the application behind pid is: its name and icon,
// the window the bar names, and its process.
func (s *shellUI) showAppAbout(pid int) {
	w, ok := s.appWindowOf(pid)
	if !ok {
		return
	}
	name, icon := s.appNameOf(w), w.AppID
	if e := s.dockState.apps.ForClass(w.AppID); e != nil {
		icon = e.Icon
	}
	win := gtk.NewWindow()
	win.AddCssClass("about")
	win.SetDefaultSize(360, -1)
	win.SetResizable(false)
	win.SetTitle(i18n.Tf("About %s", name))
	body := gtk.NewBox(gtk.OrientationVerticalValue, 10)
	body.AddCssClass("about-body")
	mark := gtk.NewImage()
	mark.AddCssClass("about-icon")
	setIcon(mark, icon)
	mark.SetPixelSize(64)
	title := gtk.NewLabel(name)
	title.AddCssClass("about-name")
	window := gtk.NewLabel(w.Title)
	window.AddCssClass("about-window")
	window.SetEllipsize(3)
	process := gtk.NewLabel(i18n.Tf("Process %d", pid))
	process.AddCssClass("about-process")
	okButton := gtk.NewButtonWithLabel(i18n.T("Close"))
	onClose := func(gtk.Button) { win.Destroy() }
	okButton.ConnectClicked(&onClose)
	for _, child := range []*gtk.Widget{&mark.Widget, &title.Widget, &window.Widget, &process.Widget, &okButton.Widget} {
		body.Append(child)
	}
	win.SetChild(&body.Widget)
	body.Unref()
	win.Present()
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
	s.onAppTitle = func(btn gtk.Button) {
		for _, b := range s.bars {
			if b.appButton.GoPointer() == btn.GoPointer() {
				s.openAppTitleMenu(b)
				return
			}
		}
	}
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
			if len(entry.Children) > 0 {
				entries = entry.Children
			} else {
				entries, err = s.appmenu.registrar.Submenu(reg, entry.ID)
				if err == nil {
					entries = withDbusmenuActions(entries, reg)
				}
			}
		} else if s.appmenu.a11y != nil {
			if len(entry.Children) > 0 {
				entries = entry.Children
			} else if !s.appMenuNeverPress(pid) {
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
	w, ok := shell.BarWindow(s.snap, connector)
	return ok && w.PID == pid
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
		case appmenu.ActionAbout:
			gtkx.Post(func() { s.showAppAbout(pid) })
		case appmenu.ActionCloseWindow:
			s.closeWindow(e.Action.Bus)
		case appmenu.ActionQuitApp:
			for _, w := range s.snap.Windows {
				if w.PID == pid {
					s.closeWindow(w.ID)
				}
			}
		case appmenu.ActionFullscreen:
			if a, ok := s.comp.(compositor.Arranger); ok {
				s.compositorDo("fullscreen window", func(ctx context.Context) error { return a.ToggleFullscreen(ctx, e.Action.Bus) })
			}
		case appmenu.ActionFloat:
			if f, ok := s.comp.(compositor.Floater); ok {
				s.compositorDo("toggle floating", func(ctx context.Context) error { return f.ToggleFloating(ctx, e.Action.Bus) })
			}
		case appmenu.ActionA11y:
			if s.appmenu.a11y == nil {
				return
			}
			err = s.appmenu.a11y.ActivateIn(e)
		case appmenu.ActionDbusmenu:
			err = s.appmenu.registrar.Activate(appmenu.Registration{Service: e.Action.Bus, Path: e.Action.Path}, e.Action.Index)
		}
		if err != nil {
			log.Printf("appmenu activate: %v", err)
		}
		gtkx.Post(func() { s.reloadAppMenu(reg, pid) })
	}()
}
