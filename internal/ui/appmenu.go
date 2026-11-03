package ui

import (
	"context"
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appmenu"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/hypr"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
)

// appMenuTarget is what a click in the bar's menu acts on, kept per widget so a
// delayed callback cannot reach a menu that has already been replaced.
type appMenuTarget struct {
	entry   appmenu.Entry
	pid     int
	bar     *bar
	section bool // a top-level entry whose children hang from a popover
}

type appMenuState struct {
	registrar  *appmenu.Registrar
	client     *appmenu.Client
	targets    map[string]appMenuTarget
	nextTarget uint64
	cancel     context.CancelFunc // stops the registrar when the bar's menus are turned off
}

// startAppMenu reads the focused application's menu for the bar, when the
// bar's global menu is on (Settings, Bar & Dock) and it is not already.
func (s *shellUI) startAppMenu() {
	if !s.cfg.Bar.Menu && len(s.cfg.Bar.MenuOn) == 0 {
		return // no application has a menu to show
	}
	if s.appmenu.client != nil {
		return
	}
	conn, err := system.SessionBus()
	if err != nil {
		log.Printf("appmenu: %v", err)
		return
	}
	s.appmenu.registrar = appmenu.New(conn)
	s.appmenu.client = appmenu.NewClient(s.appmenu.registrar, appmenu.NewA11y())
	s.appmenu.targets = make(map[string]appMenuTarget)
	s.appmenu.registrar.SetOnChange(func() { gtkx.Post(s.syncAppMenu) })
	ctx, cancel := context.WithCancel(context.Background())
	s.appmenu.cancel = cancel
	go func() {
		if err := s.appmenu.registrar.Run(ctx); err != nil {
			log.Printf("appmenu registrar: %v", err)
		}
	}()
}

// stopAppMenu takes the bar's menus away and stops the registrar, leaving
// the applications' own menus to themselves.
func (s *shellUI) stopAppMenu() {
	s.closeMenu()
	m := &s.appmenu
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	for _, b := range s.bars {
		b.appMenuGen++
		b.appMenuPID = 0
		gtkx.Clear(&b.appMenu)
		b.appMenu.SetVisible(false)
	}
	m.registrar, m.client, m.targets = nil, nil, nil
}

// setAppMenuOn starts or stops the bar's application menus, as the settings
// switch asks.
func (s *shellUI) setAppMenuOn(on bool) {
	if !on {
		s.stopAppMenu()
		return
	}
	s.startAppMenu()
	s.syncAppMenu()
}

func focusedPID(snap hypr.Snapshot) int {
	for _, win := range snap.Toplevels {
		if win.Address == snap.ActiveToplevel {
			return win.PID
		}
	}
	return 0
}

// menuWanted reports whether the bar shows the menu of the application with
// this window class and desktop ID: the bar's own switch, then the
// per-application lists over it.
func (s *shellUI) menuWanted(class string) bool {
	if class == "" {
		return false // nothing to name the application by
	}
	if s.appMenuListed(s.cfg.Bar.MenuOff, class) {
		return false
	}
	if s.cfg.Bar.Menu {
		return true
	}
	return s.appMenuListed(s.cfg.Bar.MenuOn, class)
}

// appMenuListed reports whether class, or the desktop ID of the application
// whose window class it is, is in list.
func (s *shellUI) appMenuListed(list []string, class string) bool {
	if class == "" {
		return false
	}
	id := class
	if e := s.dockState.apps.ForClass(class); e != nil {
		id = e.ID
	}
	for _, want := range list {
		if strings.EqualFold(want, class) || strings.EqualFold(want, id) {
			return true
		}
	}
	return false
}

// syncAppMenu gives every bar the menu of the application that bar names.
//
// A bar names the window it is about (shell.BarWindow): the focused window
// on the focused monitor, the workspace's own on the others. The menus come
// from that same application, so a bar's name and its menus never disagree,
// and a bar keeps its menus while the pointer is on another screen.
func (s *shellUI) syncAppMenu() {
	if s.appmenu.client == nil {
		return
	}
	for _, b := range s.bars {
		win := shell.BarWindow(s.snap, b.connector())
		pid := win.PID
		if pid != 0 && !s.menuWanted(win.Class) {
			// This application's menu is off, by the bar's switch or its own
			// entry in menu_off: the buttons go, and the process is not
			// asked for a menu it would not show.
			pid = 0
		}
		if pid == 0 {
			// Nothing to name (an empty workspace): the buttons go, but an
			// open menu stays - it belongs to the application that opened
			// it, and losing the focus for a moment (a click on the bar,
			// a window closing) must not throw it away.
			if b.appMenuPID != 0 {
				b.appMenuGen++
				b.appMenuPID = 0
				gtkx.Clear(&b.appMenu)
				b.appMenu.SetVisible(false)
			}
			continue
		}
		if b.appMenuPID == pid {
			continue
		}
		b.appMenuPID = pid
		b.appMenuGen++
		s.closeAppMenu(b)
		gtkx.Clear(&b.appMenu)
		b.appMenu.SetVisible(false)
		gen := b.appMenuGen
		go s.loadAppMenu(b, gen, pid)
	}
}

// refreshAppMenu rereads the menu of the process the bar is showing, after an
// item was activated and the application may have changed it.
func (s *shellUI) refreshAppMenu(pid int) {
	for _, b := range s.bars {
		if b.appMenuPID != pid {
			continue
		}
		b.appMenuGen++
		go s.loadAppMenu(b, b.appMenuGen, pid)
	}
}

func (s *shellUI) loadAppMenu(b *bar, gen uint64, pid int) {
	entries, err := s.appmenu.client.Menu(pid)
	gtkx.Post(func() {
		if b.appMenuGen != gen || b.appMenuPID != pid || shell.BarWindow(s.snap, b.connector()).PID != pid {
			return
		}
		if err != nil {
			log.Printf("appmenu %d: %v", pid, err)
			return
		}
		gtkx.Clear(&b.appMenu)
		if os.Getenv("INUA_APPMENU_DEBUG") != "" {
			log.Printf("appmenu %s pid=%d: %d entries", b.connector(), pid, len(entries))
		}
		b.appMenuTargets = make(map[string]appMenuTarget)
		b.appMenuEntries = entries
		for _, entry := range entries {
			if entry.Separator {
				continue
			}
			s.appendAppMenu(b, entry, pid)
		}
		b.appMenu.SetVisible(b.appMenu.GetFirstChild() != nil)
		if os.Getenv("INUA_APPMENU_DEBUG") != "" {
			// Measure once the bar has laid the buttons out: an idle callback
			// runs before that and reports a 12x2 box at y=-1.
			glib.TimeoutAdd(300, &s.onAppMenuMeasure, 0)
		}
	})
}

func (s *shellUI) appendAppMenu(b *bar, entry appmenu.Entry, pid int) {
	button := gtk.NewButton()
	button.SetLabel(entry.Label)
	button.SetHasFrame(false)
	button.SetFocusable(false)
	button.SetSensitive(entry.Enabled || len(entry.Children) > 0)
	// A section is an entry with items under it: everything else is an item
	// that acts. An application that builds its sections only when its own
	// menu opens (a browser's) gives no items, and is left out of the bar
	// rather than shown as a row of sections that open nothing.
	section := len(entry.Children) > 0
	token := s.addAppMenuTarget(b, entry, pid, section)
	button.SetName(token)
	button.ConnectClicked(&s.onAppMenuClicked)
	gtkx.Append(&b.appMenu, &button.Widget)
}

func (s *shellUI) addAppMenuTarget(b *bar, entry appmenu.Entry, pid int, section bool) string {
	s.appmenu.nextTarget++
	token := strconv.FormatUint(s.appmenu.nextTarget, 10)
	b.appMenuTargets[token] = appMenuTarget{entry: entry, pid: pid, bar: b, section: section}
	return token
}

// closeAppMenu drops the menu the bar has open, if any.
func (s *shellUI) closeAppMenu(b *bar) {
	s.closeMenu()
}

// openAppMenu hangs the section the button names under it, and closes it
// again when the same button is clicked while it is open.
func (s *shellUI) openAppMenu(b *bar, button *gtk.Button, target appMenuTarget) {
	m := &s.menu
	if m.open && !m.closing && m.pid == target.pid && m.section == target.entry.Label {
		s.closeMenu()
		return
	}
	if m.open && !m.closing {
		m.pid, m.section = target.pid, target.entry.Label
		m.entries, m.stack = target.entry.Children, nil
		s.fillMenu()
		return
	}
	if m.closing {
		show := target
		m.gone = func() { s.showMenu(&button.Widget, show) }
		return
	}
	s.showMenu(&button.Widget, target)
}

// activateAppMenu invokes an entry, but only while the process it belongs to
// still has the focus the bar drew the menu for.
func (s *shellUI) activateAppMenu(target appMenuTarget) {
	if focusedPID(s.snap) != target.pid || !target.entry.Enabled {
		return
	}
	switch target.entry.Action.Kind {
	case appmenu.ActionAbout:
		s.closeMenu()
		s.showAboutWindow(target.pid)
		return
	case appmenu.ActionCloseWindow:
		s.closeMenu()
		go s.closeWindowOf(target.pid)
		return
	case appmenu.ActionQuitApp:
		s.closeMenu()
		go s.quitApp(target.pid)
		return
	}
	go func() {
		if err := s.appmenu.client.Activate(target.entry); err != nil {
			log.Printf("appmenu activate %q: %v", target.entry.Label, err)
		}
		gtkx.Post(func() { s.refreshAppMenu(target.pid) })
	}()
}

// closeWindowOf asks the compositor to close the focused window of process
// pid - the one the bar names. A toolkit's own close may be behind a
// confirmation the shell cannot answer; this is the window manager's
// request, which every window answers.
func (s *shellUI) closeWindowOf(pid int) {
	t := s.windowOf(pid)
	if t == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.client.Dispatch(ctx, "closewindow address:"+t.Address); err != nil {
		log.Printf("appmenu: close %s: %v", t.Class, err)
	}
}

// quitApp asks a process to end: its windows close first (the compositor's
// request, so every window of it goes), and its desktop entry's Quit action
// runs when it has one and the process is still there.
func (s *shellUI) quitApp(pid int) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, t := range s.snap.Toplevels {
		if t.PID != pid {
			continue
		}
		if err := s.client.Dispatch(ctx, "closewindow address:"+t.Address); err != nil {
			log.Printf("appmenu: close %s: %v", t.Class, err)
		}
	}
	grace := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(grace) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Still there: it asked to be confirmed, or it ignores its windows
	// closing. Its entry's Quit action is the application's own way out.
	if t := s.windowOf(pid); t != nil {
		if e := s.dockState.apps.ForClass(t.Class); e != nil {
			for _, a := range e.Actions {
				if !strings.EqualFold(a.ID, "Quit") && !strings.EqualFold(a.Name, "Quit") {
					continue
				}
				if err := desktop.Spawn(a.Command, e.WorkingDir); err != nil {
					log.Printf("appmenu: quit %s: %v", e.ID, err)
				}
				return
			}
		}
	}
}

// processAlive reports whether pid is still running.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// openAppMenuFor opens the menu of the application the bar b names, from
// the button of its name.
func (s *shellUI) openAppMenuFor(button gtk.Button, b *bar) {
	pid := shell.BarWindow(s.snap, b.connector()).PID
	if pid == 0 {
		return
	}
	t := s.windowOf(pid)
	if t == nil {
		return
	}
	entries := s.appMenuEntries(pid)
	if len(entries) == 0 {
		return
	}
	section := appmenu.Entry{Label: s.appNameOf(*t), Children: entries}
	target := appMenuTarget{entry: section, pid: pid, bar: b, section: true}
	s.openAppMenu(b, &button, target)
}

// openAppMenuOn shows the application's own menu (About, Close, Quit and
// its sections) on the bar of the focused monitor, as a click on its name
// does. It is what the shell's IPC "bar appmenu" asks for, so the menu can
// be opened, and taken again, without a pointer.
func (s *shellUI) openAppMenuOn() error {
	pid := focusedPID(s.snap)
	if pid == 0 || s.appmenu.client == nil {
		return errors.New("no focused application")
	}
	for _, b := range s.bars {
		if b.connector() == s.snap.FocusedMonitor {
			s.onAppButton(b.app)
			return nil
		}
	}
	return errors.New("no bar on the focused monitor")
}

func (s *shellUI) bindAppMenuHandlers() {
	s.onAppMenuMeasure = func(uintptr) bool { return s.logAppMenuBounds() }
	// The application's name opens the menu every application has: About,
	// Close Window, Quit, then its own sections (appMenuEntries).
	// The application's name opens the menu every application has: About,
	// Close Window, Quit, then its own sections. Each bar's button calls it
	// with that bar, so the menu is the one that bar is about.
	s.onAppButton = func(button gtk.Button) {
		b := s.barUnder(&button.Widget)
		if b == nil {
			return
		}
		s.openAppMenuFor(button, b)
	}
	s.onAppMenuClicked = func(button gtk.Button) {
		var target appMenuTarget
		ok := false
		for _, b := range s.bars {
			if target, ok = b.appMenuTargets[button.GetName()]; ok {
				break
			}
		}
		if !ok {
			return
		}
		if target.section {
			s.openAppMenu(target.bar, &button, target)
			return
		}
		if !target.entry.Enabled {
			return
		}
		s.activateAppMenu(target)
	}
}

// logAppMenuBounds writes each section button's box to the log, so a test
// script can click the label it wants instead of guessing at pixels. It runs
// only with INUA_APPMENU_DEBUG set.
func (s *shellUI) logAppMenuBounds() bool {
	for _, b := range s.bars {
		for w := b.appMenu.GetFirstChild(); w != nil; w = w.GetNextSibling() {
			var r graphene.Rect
			if !w.ComputeBounds(&b.win.Widget, &r) {
				continue
			}
			box := *(*[4]float32)(unsafe.Pointer(&r))
			label := (*gtk.Button)(unsafe.Pointer(w)).GetLabel()
			log.Printf("appmenu-bounds %q x=%.0f y=%.0f w=%.0f h=%.0f", label, box[0], box[1], box[2], box[3])
		}
	}
	return false
}

// appMenuEntries is the menu behind the application's name: the one menu
// every application has, in the same place and the same order, as a Mac's
// application menu. An application's own menus follow it.
//
// About names the application, its window and what runs it; Close and Quit
// ask the compositor, so they work for every window, whatever toolkit it
// is, and whether or not the application publishes a menu at all.
func (s *shellUI) appMenuEntries(pid int) []appmenu.Entry {
	t := s.windowOf(pid)
	if t == nil {
		return nil
	}
	name := s.appNameOf(*t)
	items := []appmenu.Entry{
		{Label: i18n.Tf("About %s", name), Enabled: true, Action: appmenu.Action{Kind: appmenu.ActionAbout, Index: int32(pid)}},
		{Label: i18n.T("Close Window"), Enabled: true, Action: appmenu.Action{Kind: appmenu.ActionCloseWindow, Index: int32(pid)}},
		{Label: i18n.Tf("Quit %s", name), Enabled: true, Action: appmenu.Action{Kind: appmenu.ActionQuitApp, Index: int32(pid)}},
	}
	if own := s.appmenuEntriesOf(pid); len(own) > 0 {
		// The application's own sections follow the shell's, under a line of
		// their own, as a Mac's do after About and Quit.
		items = append(items, appmenu.Entry{Separator: true})
		items = append(items, own...)
	}
	return items
}

// windowOf is the toplevel of process pid the bar names.
func (s *shellUI) windowOf(pid int) *hypr.Toplevel {
	for i := range s.snap.Toplevels {
		if s.snap.Toplevels[i].PID == pid {
			return &s.snap.Toplevels[i]
		}
	}
	return nil
}

// appNameOf is what the bar calls an application: its desktop entry's
// name, else its class.
func (s *shellUI) appNameOf(t hypr.Toplevel) string {
	if e := s.dockState.apps.ForClass(t.Class); e != nil {
		return shell.AppName(e)
	}
	return t.Class
}

// showAboutWindow says what the application behind pid is: its name and
// icon, the window the bar names, its process. The shell shows it itself
// (a small card of its own) rather than through GtkAboutDialog, so it looks
// like the rest of inua, and works for every application whether or not it
// can open an about box of its own.
func (s *shellUI) showAboutWindow(pid int) {
	t := s.windowOf(pid)
	if t == nil {
		return
	}
	name, icon := s.appNameOf(*t), t.Class
	if e := s.dockState.apps.ForClass(t.Class); e != nil {
		icon = e.Icon
	}
	win := gtk.NewWindow()
	win.AddCssClass("about")
	win.SetDefaultSize(380, -1)
	win.SetResizable(false)
	win.SetTitle(i18n.Tf("About %s", name))

	body := gtk.NewBox(gtk.OrientationVerticalValue, 10)
	body.AddCssClass("about-body")
	mark := gtk.NewImage()
	mark.AddCssClass("about-icon")
	if icon != "" {
		setIcon(mark, icon)
	}
	mark.SetPixelSize(64)
	title := gtk.NewLabel(name)
	title.AddCssClass("about-name")
	window := gtk.NewLabel(t.Title)
	window.AddCssClass("about-window")
	window.SetEllipsize(3)
	process := gtk.NewLabel(i18n.Tf("Process %d", pid))
	process.AddCssClass("about-process")
	ok := gtk.NewButtonWithLabel(i18n.T("Close"))
	ok.AddCssClass("about-close")
	onClose := func(gtk.Button) { win.Destroy() }
	ok.ConnectClicked(&onClose)
	body.Append(&mark.Widget)
	body.Append(&title.Widget)
	body.Append(&window.Widget)
	body.Append(&process.Widget)
	body.Append(&ok.Widget)
	win.SetChild(&body.Widget)
	body.Unref()
	win.Present()
}

// appMenuButtonAt is the bar's menu button under x, y, and what it opens.
func (b *bar) appMenuButtonAt(x, y float64) (*gtk.Button, appMenuTarget, bool) {
	px, py := float32(x), float32(y)
	for w := b.appMenu.GetFirstChild(); w != nil; w = w.GetNextSibling() {
		var r graphene.Rect
		if !w.ComputeBounds(&b.win.Widget, &r) {
			continue
		}
		box := *(*[4]float32)(unsafe.Pointer(&r))
		if px < box[0] || px >= box[0]+box[2] || py < box[1] || py >= box[1]+box[3] {
			continue
		}
		btn := (*gtk.Button)(unsafe.Pointer(w))
		target, ok := b.appMenuTargets[btn.GetName()]
		if !ok || !target.section {
			return nil, appMenuTarget{}, false
		}
		return btn, target, true
	}
	return nil, appMenuTarget{}, false
}

// dropToButton moves the open menu under another of the bar's buttons.
func (s *shellUI) dropToButton(d *drop, b *bar, button *gtk.Button) {
	var r, br graphene.Rect
	if !b.island.ComputeBounds(&b.win.Widget, &r) || !button.ComputeBounds(&b.win.Widget, &br) {
		return
	}
	box := *(*[4]float32)(unsafe.Pointer(&br))
	d.rehang(b, *(*[4]float32)(unsafe.Pointer(&r)), float64(box[0]+box[2]/2), float64(box[0]))
}

// appmenuEntriesOf is the menu of process pid as a bar last read it.
func (s *shellUI) appmenuEntriesOf(pid int) []appmenu.Entry {
	for _, b := range s.bars {
		if b.appMenuPID == pid {
			return b.appMenuEntries
		}
	}
	return nil
}
