package ui

import (
	"log"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/layershell"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/uikit"
	"github.com/lyricat/inua/internal/workshop"
)

// The dock's drawer: a button at the dock's end (dock.drawer) that opens
// a card of inua's light apps above it, as a Dock folder opens on macOS
// (bubble.go). The card is in a full-screen overlay, like the control
// center's, so it grows out of the button and back into it whichever way
// it closes: a click outside, Escape, the button again, or an app
// started. The dock stays up while it is open.

// drawerColumns is how many apps a row of the drawer holds.
const drawerColumns = 6

// assistantApp is the assistant's desktop entry, in the drawer when the
// assistant is on.
const assistantApp = "io.github.lyricat.Inua.Assistant"

// The drawers: inua's tools and light apps, the workshop apps, and an
// app's menu (showDockMenu), which opens as they do.
const (
	drawerApps     = "apps"
	drawerWorkshop = "workshop"
	drawerMenu     = "menu"
)

type drawerPanel struct {
	kind     string // drawerApps, drawerWorkshop or drawerMenu
	bar      *bar   // whose dock it opened from
	win      gtk.ApplicationWindow
	backdrop *gtk.Box // borrowed from win
	card     *gtk.Box // borrowed from bubble
	bubble   *bubble
	closing  bool
	then     func() // a shell tool to open once the drawer is gone
}

func (s *shellUI) bindDrawerHandlers() {
	s.onDrawerClicked = func(b gtk.Button) {
		for _, bar := range s.bars {
			d := bar.dock
			switch {
			case d == nil:
			case d.drawer != nil && d.drawer.GoPointer() == b.GoPointer():
				s.toggleDrawer(bar, drawerApps)
				return
			case d.workshop != nil && d.workshop.GoPointer() == b.GoPointer():
				s.toggleDrawer(bar, drawerWorkshop)
				return
			}
		}
	}
	s.onDrawerApp = func(b gtk.Button) {
		id := strings.TrimPrefix(b.GetName(), "drawer:")
		if strings.HasPrefix(id, "inua:") {
			if p := s.dockState.drawer; p != nil {
				p.then = func() { s.drawerShellApp(id) }
			}
			gtkx.Post(s.closeDrawer)
			return
		}
		gtkx.Post(s.closeDrawer) // the button is in the drawer
		for _, g := range append(s.drawerSections(), s.workshopSections()...) {
			for _, e := range g.Apps {
				if e.ID == id {
					s.launchApp(e)
					return
				}
			}
		}
	}
	s.onDrawerBackdrop = func(g gtk.GestureClick, _ int32, x, y float64) {
		if s.barClick(s.barUnder(g.GetWidget()), x, y) {
			return // an entry on the bar, or the bar beside them
		}
		p := s.dockState.drawer
		if p == nil {
			return
		}
		hit := p.backdrop.Pick(x, y, gtk.PickDefaultValue)
		if hit == nil {
			return
		}
		outside := hit.GoPointer() != p.card.GoPointer() && !hit.IsAncestor(&p.card.Widget)
		hit.Unref() // Pick returned a new reference
		if !outside {
			return
		}
		// Over the other drawer's button, under this one's overlay: that
		// drawer, once this one is gone.
		if d := p.bar.dock; d != nil {
			for kind, btn := range map[string]*gtk.Button{drawerApps: d.drawer, drawerWorkshop: d.workshop} {
				if kind != p.kind && btn != nil && s.overDockButton(p.bar, btn, x, y) {
					b := p.bar
					p.then = func() { s.toggleDrawer(b, kind) }
				}
			}
		}
		gtkx.Post(s.closeDrawer)
	}
	s.onDrawerKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		if keyval != keyEscape {
			return false
		}
		gtkx.Post(s.closeDrawer)
		return true
	}
	// A workshop app installed or removed from a terminal: the dock's
	// workshop button comes or goes within a few seconds. A stat of the
	// apps' folder each time; they are read again only when it changed.
	s.onWorkshopTick = func(uintptr) bool {
		was := s.dockState.workshopStamp
		s.installedWorkshop()
		if s.dockState.workshopStamp != was {
			s.renderDocks()
		}
		return true
	}
	glib.TimeoutAdd(3000, &s.onWorkshopTick, 0)
	s.onDrawerGone = func(uintptr) bool {
		if p := s.dockState.drawer; p != nil && p.closing {
			s.dockState.drawer = nil
			p.bubble.forget()
			p.win.Destroy()
			s.renderDocks() // a workshop app installed or removed meanwhile: its button
			s.hideDocksSoon()
			if p.then != nil {
				p.then()
			}
		}
		return false
	}
}

// drawerSections are the drawer's groups, without the assistant when it
// is off or its program is missing (as the launcher's row).
func (s *shellUI) drawerSections() []shell.AppGroup {
	_, err := exec.LookPath("inua-assistant")
	assistant := s.cfg.Assistant.Enable && err == nil
	var out []shell.AppGroup
	for _, g := range shell.LightAppSections(s.dockState.apps) {
		if !assistant {
			g.Apps = slices.DeleteFunc(g.Apps, func(e *desktop.Entry) bool { return e.ID == assistantApp })
		}
		if len(g.Apps) > 0 {
			out = append(out, g)
		}
	}
	return out
}

// installedWorkshop is the workshop apps installed, read again only when
// their folder changed: the dock asks at every change of the windows.
func (s *shellUI) installedWorkshop() []workshop.Manifest {
	stamp := ""
	if info, err := os.Stat(workshop.AppsDir()); err == nil {
		stamp = info.ModTime().String()
	}
	if stamp != s.dockState.workshopStamp {
		s.dockState.workshopStamp = stamp
		s.dockState.workshopApps, _ = workshop.Installed()
	}
	return s.dockState.workshopApps
}

// workshopSections are the workshop drawer's groups, by category.
func (s *shellUI) workshopSections() []shell.AppGroup {
	return shell.WorkshopSections(s.dockState.apps, s.installedWorkshop())
}

// drawerShellApp opens one of the shell's tools, the drawer gone.
func (s *shellUI) drawerShellApp(id string) {
	switch id {
	case "inua:settings":
		s.openSettings()
	case "inua:launcher":
		if !s.launcher.st.Open {
			m, _ := shell.ParseMode(s.cfg.Launcher.Mode) // validated by config.Load
			s.showLauncher(m)
		}
	case "inua:screenshot":
		go func() {
			time.Sleep(150 * time.Millisecond) // the last frame of the drawer, off the screen
			gtkx.Post(func() { s.screenshotRequest("area") })
		}()
	}
}

// drawerButton is a drawer's button at the dock's end, the size of an
// app's: kind is drawerApps or drawerWorkshop.
func (s *shellUI) drawerButton(size int, kind string) *gtk.Button {
	btn := gtk.NewButton()
	btn.AddCssClass("dock-item")
	btn.AddCssClass("dock-drawer")
	btn.SetName("dock-" + kind)
	icon := "view-app-grid-symbolic"
	btn.SetTooltipText(i18n.T("Apps"))
	if kind == drawerWorkshop {
		icon = "application-x-addon-symbolic"
		btn.SetTooltipText(i18n.T("Workshop apps"))
	}
	btn.SetFocusable(false)
	box := gtk.NewBox(gtk.OrientationVerticalValue, 3)
	frame := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	frame.AddCssClass("drawer-frame")
	frame.SetSizeRequest(int32(size), int32(size))
	img := gtk.NewImageFromIconName(icon)
	img.SetPixelSize(int32(max(14, size*9/20)))
	img.SetHexpand(true)
	img.SetHalign(gtk.AlignCenterValue)
	img.SetValign(gtk.AlignCenterValue)
	gtkx.Append(frame, &img.Widget)
	gtkx.Append(box, &frame.Widget)
	dot := gtk.NewBox(gtk.OrientationHorizontalValue, 0) // keeps it level with the apps
	dot.AddCssClass("dock-dot")
	dot.SetHalign(gtk.AlignCenterValue)
	gtkx.Append(box, &dot.Widget)
	btn.SetChild(&box.Widget)
	box.Unref()
	btn.ConnectClicked(&s.onDrawerClicked)
	return btn
}

// toggleDrawer opens a drawer over a bar's dock, or closes it; the other
// drawer, when open, gives way to it.
func (s *shellUI) toggleDrawer(b *bar, kind string) {
	if p := s.dockState.drawer; p != nil {
		if !p.closing && p.kind == kind {
			s.closeDrawer()
			return
		}
	}
	s.dropDrawer()
	d := b.dock
	if d == nil {
		return
	}
	button := d.drawer
	if kind == drawerWorkshop {
		button = d.workshop
		s.freshenApps() // an app just installed: its desktop entry
	}
	if button == nil {
		return
	}
	s.openDrawer(b, kind, &button.Widget, s.drawerContent(kind))
}

// dropDrawer takes the open drawer away at once: the other drawer, or
// one opened again while going.
func (s *shellUI) dropDrawer() {
	if p := s.dockState.drawer; p != nil {
		s.dockState.drawer = nil
		p.bubble.forget()
		p.win.Destroy()
	}
}

// drawerContent is the card: each group's title, then its apps in rows of
// drawerColumns, every cell as wide as the others, so a long name wraps
// instead of widening the row.
func (s *shellUI) drawerContent(kind string) *gtk.Box {
	sections := s.drawerSections()
	if kind == drawerWorkshop {
		sections = s.workshopSections()
	}
	content := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	for _, g := range sections {
		title := gtk.NewLabel(i18n.T(g.Title))
		title.AddCssClass("kicker")
		title.AddCssClass("drawer-title")
		title.SetXalign(0)
		gtkx.Append(content, &title.Widget)
		grid := gtk.NewGrid()
		grid.AddCssClass("drawer-grid")
		grid.SetColumnHomogeneous(true)
		grid.SetColumnSpacing(4)
		grid.SetRowSpacing(4)
		for i, e := range g.Apps {
			b := gtk.NewButton()
			b.AddCssClass("drawer-app")
			b.SetName("drawer:" + e.ID)
			col := gtk.NewBox(gtk.OrientationVerticalValue, 7)
			img := gtk.NewImage()
			setIcon(img, e.Icon)
			img.SetPixelSize(44)
			gtkx.Append(col, uikit.AppIcon(img, nil))
			name, comment := e.Name, e.Comment
			if strings.HasPrefix(e.ID, "inua:") {
				name, comment = i18n.T(name), i18n.T(comment)
			}
			b.SetTooltipText(comment)
			l := gtk.NewLabel(name)
			l.AddCssClass("drawer-name")
			l.SetWrap(true)
			l.SetJustify(gtk.JustifyCenterValue)
			l.SetLines(2)
			l.SetEllipsize(3) // end
			l.SetMaxWidthChars(10)
			l.SetValign(gtk.AlignStartValue)
			gtkx.Append(col, &l.Widget)
			b.SetChild(&col.Widget)
			col.Unref()
			b.ConnectClicked(&s.onDrawerApp)
			grid.Attach(&b.Widget, int32(i%drawerColumns), int32(i/drawerColumns), 1, 1)
			b.Unref()
		}
		// A short group keeps all the columns, so its apps are as wide
		// as the others' rather than sharing the whole row.
		for i := len(g.Apps); i < drawerColumns; i++ {
			empty := gtk.NewBox(gtk.OrientationVerticalValue, 0)
			grid.Attach(&empty.Widget, int32(i), 0, 1, 1)
			empty.Unref()
		}
		gtkx.Append(content, &grid.Widget)
	}
	if kind == drawerApps && len(shell.LightApps(s.dockState.apps)) == len(shell.ShellApps) {
		l := gtk.NewLabel(i18n.T("The light apps are not installed (make install)."))
		l.AddCssClass("drawer-empty")
		gtkx.Append(content, &l.Widget)
	}
	return content
}

// openDrawer opens a drawer of content beside its button, out of the
// dock; it takes content's reference.
func (s *shellUI) openDrawer(b *bar, kind string, button *gtk.Widget, content *gtk.Box) {
	d := b.dock
	p := &drawerPanel{kind: kind, bar: b, win: *gtk.NewApplicationWindow(s.app)}
	p.win.AddCssClass("drawer-overlay")
	backdrop := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	backdrop.SetHexpand(true)
	backdrop.SetVexpand(true)
	// The card scrolls once the groups are taller than the room between
	// the bar and the dock.
	card := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	card.AddCssClass("drawer")
	if kind == drawerMenu {
		card.AddCssClass("dock-menu")
	}
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	scroll.SetPropagateNaturalWidth(true)
	scroll.SetPropagateNaturalHeight(true)
	scroll.SetChild(&content.Widget)
	content.Unref()
	gtkx.Append(card, &scroll.Widget)

	// The arrow's tip by the drawer button: the dock is centred along its
	// edge.
	var geo gdk.Rectangle
	b.mon.GetGeometry(&geo)
	barH := int32(0) // the bar takes the top of the screen
	if s.cfg.Bar.Enable {
		barH = int32(b.win.GetHeight())
	}
	dockW, dockH := int32(d.win.GetWidth()), int32(d.win.GetHeight())
	// The tallest the card may be: the screen less the bar, the dock
	// when it is at the top or bottom, the arrow and a gap.
	room := geo.Height - barH - 2*drawerGap
	if !d.vertical {
		room -= dockH + bubbleGap + bubbleArrow
	}
	var cardSize, contentSize gtk.Requisition
	card.GetPreferredSize(nil, &cardSize)
	content.GetPreferredSize(nil, &contentSize)
	if cardSize.Height > room { // scroll the rest
		scroll.SetMaxContentHeight(max(120, room-(cardSize.Height-contentSize.Height)))
	}
	var r graphene.Rect
	var f [4]float32
	if button.ComputeBounds(&d.win.Widget, &r) {
		f = *(*[4]float32)(unsafe.Pointer(&r))
	}
	W, H := float64(geo.Width), float64(geo.Height)
	alongX := (W-float64(dockW))/2 + float64(f[0]+f[2]/2)
	// A dock on a side is centred in the height the bar leaves.
	alongY := float64(barH) + (H-float64(barH+dockH))/2 + float64(f[1]+f[3]/2)
	side, tipX, tipY := "bottom", alongX, H-float64(dockH)-bubbleGap
	switch d.position {
	case "left":
		side, tipX, tipY = "left", float64(dockW)+bubbleGap, alongY
	case "right":
		side, tipX, tipY = "right", W-float64(dockW)-bubbleGap, alongY
	case "top": // the dock sits under the bar
		side, tipY = "top", float64(barH+dockH)+bubbleGap
	}
	p.bubble = newBubble(&card.Widget, side, tipX, tipY, float64(barH+drawerGap), float64(s.cfg.Style.Radius*3/2)) // the dock's corners (style.Corners)
	card.Unref()
	backdrop.Append(p.bubble.canvas)
	p.win.SetChild(&backdrop.Widget)
	backdrop.Unref()
	p.backdrop, p.card = backdrop, card

	click := gtk.NewGestureClick()
	click.ConnectPressed(&s.onDrawerBackdrop)
	backdrop.AddController(&click.EventController)
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(&s.onDrawerKey)
	p.win.AddController(&keys.EventController)
	s.followBar(&p.win.Widget)

	w := p.win.GoPointer()
	layershell.InitForWindow(w)
	layershell.SetNamespace(w, "inua-drawer")
	layershell.SetLayer(w, layershell.LayerOverlay)
	layershell.SetKeyboardMode(w, layershell.KeyboardExclusive)
	for _, e := range []layershell.Edge{layershell.EdgeTop, layershell.EdgeBottom, layershell.EdgeLeft, layershell.EdgeRight} {
		layershell.SetAnchor(w, e, true)
	}
	layershell.SetExclusiveZone(w, -1)
	layershell.SetMonitor(w, b.mon.GoPointer())
	s.dockState.drawer = p
	p.win.Present()
	p.bubble.open() // after Present, so it grows in
	// So that a second click on the button, the pointer unmoved, closes it.
	s.refocusPointer()
}

// overDockButton reports whether the point x, y of the drawer's overlay
// (the monitor's coordinates) is on one of the dock's buttons. The dock is
// centred along its edge, as openDrawer places the card by it.
func (s *shellUI) overDockButton(b *bar, btn *gtk.Button, x, y float64) bool {
	d := b.dock
	var r graphene.Rect
	if d == nil || !btn.ComputeBounds(&d.win.Widget, &r) {
		return false
	}
	f := *(*[4]float32)(unsafe.Pointer(&r))
	var geo gdk.Rectangle
	b.mon.GetGeometry(&geo)
	dockW, dockH := float64(d.win.GetWidth()), float64(d.win.GetHeight())
	barH := 0.0
	if s.cfg.Bar.Enable {
		barH = float64(b.win.GetHeight())
	}
	var ox, oy float64 // the dock window's corner; on a side, centred under the bar
	switch d.position {
	case "left":
		ox, oy = 0, barH+(float64(geo.Height)-barH-dockH)/2
	case "right":
		ox, oy = float64(geo.Width)-dockW, barH+(float64(geo.Height)-barH-dockH)/2
	case "top":
		ox, oy = (float64(geo.Width)-dockW)/2, barH
	default: // bottom
		ox, oy = (float64(geo.Width)-dockW)/2, float64(geo.Height)-dockH
	}
	bx, by := ox+float64(f[0]), oy+float64(f[1])
	return x >= bx && x < bx+float64(f[2]) && y >= by && y < by+float64(f[3])
}

// drawerGap is the least space between the drawer and the screen's edges.
const drawerGap = 8

// closeDrawer shrinks the drawer back into its button; the window goes
// when it has.
func (s *shellUI) closeDrawer() {
	p := s.dockState.drawer
	if p == nil || p.closing {
		return
	}
	p.closing = true
	p.bubble.close(func() { s.onDrawerGone(0) })
}

// drawerRequest answers `inua call drawer toggle|open|close` with the
// drawer of the dock on the focused monitor.
func (s *shellUI) drawerRequest(fn string) bool {
	p := s.dockState.drawer
	kind := drawerApps
	switch fn {
	case "toggle", "open", "close":
	case "workshop": // toggles the workshop drawer
		kind = drawerWorkshop
	default:
		return false
	}
	open := p != nil && !p.closing && p.kind == kind
	if fn == "close" || ((fn == "toggle" || fn == "workshop") && open) {
		s.closeDrawer()
		return true
	}
	if open {
		return true
	}
	for _, b := range s.bars {
		// A dock only the overview shows is unmapped outside it: no drawer
		// to grow from.
		if b.dock == nil || b.connector() != s.snap.FocusedOutput || b.dock.drawer == nil || !b.dock.win.GetVisible() {
			continue
		}
		if kind == drawerWorkshop && b.dock.workshop == nil {
			continue // no workshop app installed
		}
		s.showDock(b.dock)
		s.toggleDrawer(b, kind)
	}
	return true
}

// openLightApp opens a light app by its desktop entry, or runs it
// directly when the entry is not installed.
func (s *shellUI) openLightApp(id, name string) {
	for _, e := range shell.LightApps(s.dockState.apps) {
		if e.ID == id {
			s.launchApp(e)
			return
		}
	}
	if err := desktop.Spawn([]string{"inua-apps", name}, ""); err != nil {
		log.Printf("%s: %v", name, err)
	}
}
