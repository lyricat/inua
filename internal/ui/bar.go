package ui

import (
	"fmt"
	"log"
	"slices"
	"time"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/hypr"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/layershell"
	"github.com/lyricat/inua/internal/shell"
)

// bar is one bar.ui instance, one per monitor, alive while the monitor is.
type bar struct {
	mon         *gdk.Monitor           // our reference, dropped in dropBar
	wallpaper   *gtk.ApplicationWindow // this monitor's background, if inua draws it
	dock        *dock                  // this monitor's dock, if shown
	win         gtk.ApplicationWindow
	island      gtk.CenterBox
	workspaces  gtk.Box
	title       gtk.Label
	clock       gtk.Label
	clipboard   gtk.Button
	passwords   gtk.Button
	notes       gtk.Button
	notesIcon   gtk.Image
	voice       gtk.Button
	voiceIcon   gtk.Image
	unread      gtk.Box
	tray        gtk.Box
	status      gtk.Button
	netIcon     gtk.Image
	btIcon      gtk.Image
	volIcon     gtk.Image
	battery     gtk.Box
	batIcon     gtk.Image
	batLabel    gtk.Label
	rec         gtk.Button
	clockButton gtk.Button // opens the calendar panel
	recTime     gtk.Label
	refs        gtkx.Scope
	edgeGaps    bool
	compact     bool

	// The workspaces this bar shows, and their buttons by id, owned by the
	// workspaces box. Buttons are kept across updates so a focus change
	// restyles them, which CSS animates.
	shown []hypr.Workspace
	ws    map[int]*gtk.Button
	// Buttons not styled yet: a class set before their first frame would
	// not animate.
	fresh map[int]bool
}

// connector is the monitor's name, as Hyprland calls it too ("eDP-1"). A
// monitor that just appeared may not have it yet; see onConnector.
func (b *bar) connector() string { return b.mon.GetConnector() }

// syncBars gives every monitor GDK knows a bar, and drops bars whose
// monitor went away. It runs at start and whenever the monitor list changes.
func (s *shellUI) syncBars() {
	list := gdk.DisplayGetDefault().GetMonitors()
	seen := map[uintptr]bool{}
	for i := range list.GetNItems() {
		mon := gdk.MonitorNewFromInternalPtr(list.GetItem(i)) // a new reference
		key := mon.GoPointer()
		seen[key] = true
		if s.bars[key] != nil {
			mon.Unref()
			continue
		}
		// gtk4-layer-shell 1.1 substitutes invisible stub surfaces for
		// ordinary windows created while a session lock is active. Keep
		// removing disconnected monitors, but create replacements on unlock.
		if l := &s.lock; l.held() {
			// Before 1.2 there is no ::monitor to cover a monitor that
			// came while locked; without this it has no password field.
			if !l.ending && !layershell.AtLeast(1, 2) && !l.covers(mon) {
				log.Printf("lock: a monitor came while locked")
				s.buildLockSurface(mon)
			} else {
				mon.Unref()
			}
			continue
		}
		s.bars[key] = s.newBar(mon)
		gtkx.ConnectNotify(mon, "connector", &s.onConnector)
		log.Printf("bar: added %q", mon.GetConnector())
	}
	for key, b := range s.bars {
		if !seen[key] {
			log.Printf("bar: removed %q (locked: %v)", b.connector(), s.lock.held())
			s.dropBar(b)
		}
	}
	s.lock.forget(func(mon *gdk.Monitor) bool { return !seen[mon.GoPointer()] })
}

func (s *shellUI) dropBar(b *bar) {
	if s.tray.menu != nil {
		s.tray.menu.Popdown() // it may hang off this bar
	}
	delete(s.bars, b.mon.GoPointer())
	dropWallpaper(b)
	dropDock(b)
	b.win.Destroy()
	b.refs.Release()
	b.mon.Unref()
}

func (s *shellUI) newBar(mon *gdk.Monitor) *bar {
	b := &bar{mon: mon, ws: map[int]*gtk.Button{}, fresh: map[int]bool{}}
	ui := s.load("bar.ui", &b.refs)
	ui.Get("bar", &b.win)
	ui.Get("island", &b.island)
	ui.Get("workspaces", &b.workspaces)
	overviewClick := gtk.NewGestureClick()
	overviewClick.SetButton(3)
	overviewClick.SetPropagationPhase(gtk.PhaseCaptureValue)
	overviewClick.ConnectPressed(&s.onWorkspaceOverview)
	b.workspaces.AddController(&overviewClick.EventController)
	ui.Get("title", &b.title)
	ui.Get("clock", &b.clock)
	ui.Get("notes", &b.notes)
	ui.Get("clipboard", &b.clipboard)
	ui.Get("passwords", &b.passwords)
	ui.Get("rec", &b.rec)
	ui.Get("rec_time", &b.recTime)
	ui.Get("clock_button", &b.clockButton)
	ui.Get("notes_icon", &b.notesIcon)
	ui.Get("voice", &b.voice)
	ui.Get("voice_icon", &b.voiceIcon)
	ui.Get("unread", &b.unread)
	ui.Get("tray", &b.tray)
	ui.Get("status", &b.status)
	ui.Get("net_icon", &b.netIcon)
	ui.Get("bt_icon", &b.btIcon)
	ui.Get("vol_icon", &b.volIcon)
	ui.Get("battery", &b.battery)
	ui.Get("bat_icon", &b.batIcon)
	ui.Get("bat_label", &b.batLabel)
	ui.Close()
	b.setEdgeGap(s.cfg.Style.EdgeGaps)
	b.setDensity(s.cfg.Bar.Density)
	b.status.ConnectClicked(&s.onControlButton)
	b.notes.ConnectClicked(&s.onNotesButton)
	b.voice.ConnectClicked(&s.onVoiceButton)
	b.voice.SetVisible(s.cfg.Voice.Enable)
	b.voiceIcon.SetFromIconName(shell.VoiceIcon(s.sys.Voice))
	b.voiceIcon.SetTooltipText(shell.VoiceTooltip(s.sys.Voice))
	b.clipboard.ConnectClicked(&s.onClipboardButton)
	b.clipboard.SetVisible(s.cfg.Clipboard.Enable && s.cfg.Clipboard.Bar)
	b.passwords.ConnectClicked(&s.onPasswordsButton)
	b.passwords.SetVisible(s.cfg.Bar.Pass && s.passwords.hasStore)
	if s.passwords.open {
		b.passwords.AddCssClass("open")
	}
	b.rec.ConnectClicked(&s.onRecButton)
	b.clockButton.ConnectClicked(&s.onClockButton)
	s.showNotesButton(b)
	// The buttons reach to the screen's edges: the bar's margin around and
	// between them hovers, clicks and scrolls as the button (endReach,
	// clockReach), so the pointer thrown at the top or the corner lands on
	// one.
	motion := gtk.NewEventControllerMotion()
	motion.ConnectEnter(&s.onBarMotion)
	motion.ConnectMotion(&s.onBarMotion)
	motion.ConnectLeave(&s.onBarLeave)
	b.win.AddController(&motion.EventController)
	click := gtk.NewGestureClick()
	click.ConnectReleased(&s.onBarReleased)
	b.win.AddController(&click.EventController)
	scroll := gtk.NewEventControllerScroll(gtk.EventControllerScrollVerticalValue | gtk.EventControllerScrollDiscreteValue)
	scroll.ConnectScroll(&s.onBarScroll)
	b.win.AddController(&scroll.EventController)
	if s.control.open && !s.control.closing {
		b.status.AddCssClass("open")
	}
	b.setStatus(&s.sys)
	b.setHypr(s, s.snap)
	b.setClock(s, time.Now())
	b.title.SetVisible(s.cfg.Bar.Title)
	b.tray.SetVisible(s.cfg.Bar.Tray)
	s.renderTray(b)

	b.win.SetApplication(s.app)
	w := b.win.GoPointer()
	if s.layer {
		layershell.InitForWindow(w)
		layershell.SetNamespace(w, "inua-bar")
		layershell.SetLayer(w, layershell.LayerTop)
		layershell.SetMonitor(w, mon.GoPointer())
		layershell.SetAnchor(w, layershell.EdgeTop, true)
		layershell.SetAnchor(w, layershell.EdgeLeft, true)
		layershell.SetAnchor(w, layershell.EdgeRight, true)
		layershell.AutoExclusiveZone(w)
	} else {
		b.win.SetTitle("inua bar " + b.connector())
		b.win.SetDefaultSize(900, 30)
	}
	b.win.SetVisible(s.cfg.Bar.Enable)
	if s.cfg.Bar.Enable {
		b.win.Present()
	}
	s.syncWallpaper(b)
	s.syncDock(b)
	return b
}

// setEdgeGap insets the bar from the screen edges by 4 px, or not
// (style.edge_gaps).
func (b *bar) setEdgeGap(gaps bool) {
	b.edgeGaps = gaps
	margin := int32(0)
	if gaps {
		margin = 4
	}
	topMargin := margin
	if b.compact && gaps {
		topMargin = 1
	}
	b.island.SetMarginTop(topMargin)
	b.island.SetMarginStart(margin)
	b.island.SetMarginEnd(margin)
	if gaps {
		b.win.RemoveCssClass("flush")
	} else {
		b.win.AddCssClass("flush") // square against the screen's edges
	}
}

// setDensity applies the selected bar spacing without rebuilding the surface.
func (b *bar) setDensity(density string) {
	compact := density == "compact"
	b.compact = compact
	height := int32(30)
	spacing := int32(5)
	if compact {
		height = 24
		spacing = 4
	}
	b.workspaces.SetSpacing(spacing)
	if compact {
		b.win.AddCssClass("compact")
		b.island.SetSizeRequest(-1, 24)
		topMargin := int32(0)
		if b.edgeGaps {
			topMargin = 1
		}
		b.island.SetMarginTop(topMargin)
	} else {
		b.win.RemoveCssClass("compact")
		b.island.SetSizeRequest(-1, 30)
		margin := int32(0)
		if b.edgeGaps {
			margin = 4
		}
		b.island.SetMarginTop(margin)
	}
	// A mapped GTK4 Layer Shell surface does not resize from its child's
	// size request alone. Updating the toplevel default size reconfigures
	// the surface and its automatically calculated exclusive zone as well.
	b.win.SetDefaultSize(-1, height)
	b.win.QueueResize()
}

func (b *bar) setClock(s *shellUI, now time.Time) {
	b.clock.SetText(config.Strftime(s.cfg.Bar.Clock, now))
}

func (b *bar) setHypr(s *shellUI, snap hypr.Snapshot) {
	name := b.connector()
	activeWorkspace := 0
	for _, mon := range snap.Monitors {
		if mon.Name == name {
			activeWorkspace = mon.ActiveWorkspaceID
			break
		}
	}
	hasWindows := false
	for _, win := range snap.Toplevels {
		if win.WorkspaceID == activeWorkspace {
			hasWindows = true
			break
		}
	}
	if hasWindows {
		b.win.RemoveCssClass("empty")
		b.win.AddCssClass("occupied")
	} else {
		b.win.RemoveCssClass("occupied")
		b.win.AddCssClass("empty")
	}
	b.title.SetText(shell.BarTitle(snap, name))
	order := s.overview.st
	order.MRU = s.cfg.Overview.MRU
	b.setWorkspaces(s, order.BarWorkspaces(snap, name, s.cfg.Bar.Workspaces == "all"))
}

// setWorkspaces adds and removes workspace buttons in place. Buttons that
// already exist are restyled right away, so the focus change animates; new
// ones are styled a moment later, after their first frame, so they grow in
// too.
func (b *bar) setWorkspaces(s *shellUI, wss []hypr.Workspace) {
	b.shown = wss
	for id, btn := range b.ws {
		if !slices.ContainsFunc(wss, func(w hypr.Workspace) bool { return w.ID == id }) {
			b.workspaces.Remove(&btn.Widget) // drops the last reference
			delete(b.ws, id)
			delete(b.fresh, id)
		}
	}
	added := false
	prev := &gtk.Widget{} // a NULL sibling: the start of the box
	for _, w := range wss {
		btn, ok := b.ws[w.ID]
		if !ok {
			btn = gtk.NewButton()
			btn.SetName(fmt.Sprint(w.ID))
			btn.SetFocusable(false)
			btn.SetValign(gtk.AlignCenterValue)
			btn.SetTooltipText(i18n.Tf("Workspace %s", w.Name))
			btn.ConnectClicked(&s.onWorkspaceClicked)
			b.workspaces.InsertChildAfter(&btn.Widget, prev)
			btn.Unref() // the box holds it
			b.ws[w.ID] = btn
			b.fresh[w.ID] = true
			added = true
		} else {
			b.workspaces.ReorderChildAfter(&btn.Widget, prev)
		}
		if s.cfg.Overview.MRU {
			btn.SetLabel("·")
			if w.Focused || w.Active {
				btn.SetLabel(i18n.T("WORKSPACE"))
			}
		} else {
			btn.SetLabel(w.Name)
		}
		prev = &btn.Widget
	}
	b.styleWorkspaces()
	if added {
		glib.TimeoutAdd(60, &s.onWorkspaceSettle, 0)
	}
}

// styleWorkspaces sets focused/active/urgent classes, except on fresh
// buttons.
func (b *bar) styleWorkspaces() {
	for _, w := range b.shown {
		btn := b.ws[w.ID]
		if btn == nil || b.fresh[w.ID] {
			continue
		}
		toggle(&btn.Widget, "focused", w.Focused)
		toggle(&btn.Widget, "active", w.Active && !w.Focused)
		toggle(&btn.Widget, "urgent", w.Urgent && !w.Focused)
	}
}

// setStatus updates the bar's status button.
func (b *bar) setStatus(st *shell.System) {
	b.netIcon.SetFromIconName(shell.NetIcon(st.Net))
	b.netIcon.SetTooltipText(shell.NetTooltip(st.Net))
	b.btIcon.SetVisible(st.BT.Available && st.BT.Powered)
	b.btIcon.SetFromIconName("bluetooth-active-symbolic")
	b.volIcon.SetFromIconName(shell.VolumeIcon(st.Audio))
	b.battery.SetVisible(st.Bat.Present)
	if st.Bat.Present {
		b.batIcon.SetFromIconName(shell.BatteryIcon(st.Bat))
		b.batLabel.SetText(fmt.Sprintf("%.0f%%", st.Bat.Percent))
	}
}

// barOf is the bar whose window w is.
func (s *shellUI) barOf(w *gtk.Widget) *bar {
	for _, b := range s.bars {
		if w != nil && b.win.GoPointer() == w.GoPointer() {
			return b
		}
	}
	return nil
}

// endButtons are the bar's toggle buttons at its end, left to right, as
// shown, with their names.
func (b *bar) endButtons() (names []string, rects []shell.Rect) {
	for _, c := range []struct {
		name string
		btn  *gtk.Button
	}{{"passwords", &b.passwords}, {"clipboard", &b.clipboard}, {"notes", &b.notes}, {"status", &b.status}} {
		var r graphene.Rect
		if c.btn.IsVisible() && c.btn.ComputeBounds(&b.win.Widget, &r) {
			f := *(*[4]float32)(unsafe.Pointer(&r)) // x, y, width, height; see rect
			names = append(names, c.name)
			rects = append(rects, shell.Rect{X: float64(f[0]), Y: float64(f[1]), W: float64(f[2]), H: float64(f[3])})
		}
	}
	return names, rects
}

// endReach names the toggle button at the bar's end whose reach a point in
// the bar's window is in ("passwords", "clipboard", "notes", "status"; ""
// for none), and
// whether it is on the button itself, which takes its own clicks. The
// reaches cover the gaps between them and run up to the screen's edges, so
// the pointer thrown at the top or the corner lands on one (shell.BarReach).
func (b *bar) endReach(x, y float64) (name string, onButton bool) {
	const gap = 6 // the end box's spacing
	names, rects := b.endButtons()
	i, on := shell.BarReach(rects, gap, x, y)
	if i < 0 {
		return "", false
	}
	return names[i], on
}

// clockReach reports whether a point in the bar's window is in the clock's
// reach: its column, a little wider than the clock, the window's full
// height up to the screen's edge. onButton is the button itself, which
// takes its own clicks.
func (b *bar) clockReach(x, y float64) (reach, onButton bool) {
	var r graphene.Rect
	if !b.clockButton.IsVisible() || !b.clockButton.ComputeBounds(&b.win.Widget, &r) {
		return false, false
	}
	f := *(*[4]float32)(unsafe.Pointer(&r))
	const wider = 12
	px, py := float32(x), float32(y)
	reach = px >= f[0]-wider && px < f[0]+f[2]+wider
	onButton = reach && px >= f[0] && px < f[0]+f[2] && py >= f[1] && py < f[1]+f[3]
	return reach, onButton
}

func toggle(w *gtk.Widget, class string, on bool) {
	if on {
		w.AddCssClass(class)
	} else {
		w.RemoveCssClass(class)
	}
}
