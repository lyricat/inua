package ui

import (
	"cmp"
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"

	"github.com/lyricat/inua/internal/convert"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/layershell"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/uikit"
)

// gridCols is how many apps a line of the grid holds (appcell.ui's width,
// six to launcher.ui's).
const gridCols = 6

// pageLines is how many lines Page Up and Page Down move.
const pageLines = 3

// The blurred foot of the results: blurSteps strips of stripHeight pixels,
// blurred more the lower (style.css's blur1 to blur8).
const (
	blurSteps   = 8
	stripHeight = 8
)

// Moving the selection, the view scrolls and the highlight glides to it,
// each frame closing the distance as an exponential with glideTau; the
// selection stays keepClear pixels from the view's edges.
const (
	glideTau  = 0.045
	keepClear = 10
)

// noTickDone is a tick callback's destroy notify that has nothing to do.
var noTickDone glib.DestroyNotify = func(uintptr) {}

// launcher shows shell.Launcher. Its window (launcher.ui) exists exactly
// while the launcher is open: it is built on every open and destroyed on
// close, because re-showing a hidden layer surface hung GTK's GPU renderers
// on the 4th time (docs/toolkit.md), and a new surface does not.
type launcher struct {
	st       shell.Launcher
	rates    *convert.Rates
	fetching bool // exchange rates are being read

	win       *gtk.ApplicationWindow // nil while closed
	closed    *gtk.ApplicationWindow // the last one, fading away
	gone      uint32                 // the timer that destroys it
	mode      gtk.Label
	query     gtk.Entry
	scroll    gtk.ScrolledWindow
	results   gtk.Box
	highlight gtk.Box
	foot      gtk.Overlay
	blur      gtk.Box
	fade      gtk.Box
	strips    []blurStrip // the blurred foot, top to bottom
	// The lines the foot's strips copy (l.lines[footFrom:footTo]), and
	// where the first is in the results.
	footFrom, footTo int
	footTop          float64
	// The foot's icons by name, each looked up and decoded once while the
	// launcher is open and shared by every copy of its cell.
	footIcons  map[string]*gtk.IconPaintable
	pinned     gtk.Box
	pinScroll  gtk.ScrolledWindow
	messageBox gtk.Box
	messageTag gtk.Label
	message    gtk.Label
	refs       gtkx.Scope
	hasFocus   bool
	opens      int
	actionsOf  int          // the result Tab's menu is for
	menu       *gtk.Popover // that menu, while it is open
	// catch is a clear window over the screen under the launcher, whose
	// click outside it closes it (showLauncherCatch).
	catch *gtk.ApplicationWindow

	// The results shown: all of them, built again only when they change
	// (shell.Launcher.Version); a move marks another one selected.
	shown    int          // the Version shown, -1 for none
	lines    []shownLine  // the lines that scroll, in order
	items    []gtk.Widget // each result's row or cell, by index
	pin      int          // the first pinned result (shell.Launcher.Pinned)
	selected int          // the result marked selected

	// The animation (stepLauncher): the highlight's place, and whether the
	// view still follows the selection.
	tick    uint32
	last    time.Time
	hl      [4]float64 // x, y, width, height in the results
	hlShown bool
	jump    bool // go straight there: the results were just built
	follow  bool
}

// blurStrip is one strip of the blurred foot: copies of the lines just
// below the view (ghost) in a scrolled window without scrollbars, a strip
// high whatever their height, scrolled to line up with the results. Not
// a picture of the results: GTK leaves what is scrolled out of sight out
// of their drawing, so a picture has nothing below the view.
type blurStrip struct {
	view  gtk.ScrolledWindow
	ghost gtk.Box
}

// shownLine is a line of results and its widget, a row or a grid line.
type shownLine struct {
	w  gtk.Widget
	ln shell.Line
}

const (
	keyEscape     = 0xff1b
	keySpace      = 0x20
	keyReturn     = 0xff0d
	keyKPEnter    = 0xff8d
	keyUp         = 0xff52
	keyDown       = 0xff54
	keyLeft       = 0xff51
	keyRight      = 0xff53
	keyPageUp     = 0xff55
	keyPageDown   = 0xff56
	keyTab        = 0xff09
	keyISOLeftTab = 0xfe20
)

func (s *shellUI) bindLauncherHandlers() {
	s.onKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		return s.launcherKey(keyval)
	}
	s.onLauncherScrolled = func(gtk.Adjustment) { s.placeFoot() }
	s.onLauncherLaidOut = func(gtk.Adjustment) {
		s.animateLauncher()
		s.placeFoot()
	}
	s.onLauncherTick = func(uintptr, uintptr, uintptr) bool { return s.stepLauncher() }
	// A strip's copy is laid out after the results: place it again.
	s.onLauncherFootLaidOut = func(gtk.Adjustment) { s.placeFoot() }
	s.onLauncherCatch = func(g gtk.GestureClick, _ int32, x, y float64) {
		b := s.barUnder(g.GetWidget())
		s.cancelLauncher()
		s.barClick(b, x, y) // an entry on the bar does its own as well
	}
	s.onLauncherPick = func(g gtk.GestureClick, _ int32, _, _ float64) {
		l := &s.launcher
		w := g.GetWidget()
		if w == nil || !l.st.Open {
			return
		}
		var i int
		if _, err := fmt.Sscanf(w.GetName(), "result:%d", &i); err != nil {
			return
		}
		l.st.Move(i - l.st.Selected)
		s.executeLauncher()
	}
	s.onQueryChanged = func(uintptr, uintptr) {
		l := &s.launcher
		// renderLauncher setting the text also lands here.
		if q := l.query.GetText(); l.win != nil && q != l.st.Query {
			l.st.SetQuery(q)
			s.renderLauncher()
		}
	}
	s.onQueryActivate = func(gtk.Entry) { s.executeLauncher() }
	s.onLauncherGone = func(uintptr) bool {
		s.launcher.gone = 0
		s.dropClosedLauncher()
		return false
	}
	s.onLauncherMenuClosed = func(gtk.Popover) {
		l := &s.launcher
		l.menu = nil
		if l.win == nil {
			return
		}
		l.query.GrabFocus()
		if s.layer {
			// Hyprland leaves the keyboard with the menu's popup once it
			// has gone: let go of it and take it again.
			w := l.win
			layershell.SetKeyboardMode(w.GoPointer(), layershell.KeyboardNone)
			s.runAfter(30, func() {
				if l.win == w {
					layershell.SetKeyboardMode(w.GoPointer(), layershell.KeyboardExclusive)
				}
			})
		}
	}
	s.onQueryFocus = func(gtk.EventControllerFocus) {
		s.launcher.hasFocus = true
	}
}

func (s *shellUI) showLauncher(m shell.Mode) {
	s.closeClipboard()
	s.closePasswords()
	s.freshenApps()
	s.freshenRates()
	s.launcher.st.Show(m)
	s.syncLauncher()
	log.Printf("launcher: open mode=%s", m)
}

func (s *shellUI) cancelLauncher() {
	a := s.launcher.st.Cancel()
	s.syncLauncher()
	if a.Kind == shell.Focus {
		s.focusWindow(a.Address)
	}
}

func (s *shellUI) executeLauncher() {
	l := &s.launcher
	a := l.st.Execute()
	switch a.Kind {
	case shell.Launch:
		log.Printf("launcher: launch %s", a.Entry.ID)
		if err := desktop.LaunchWith(a.Entry, s.cfg.TerminalCommand()); err != nil {
			log.Printf("launch %s: %v", a.Entry.ID, err)
			l.st.Done(fmt.Errorf("could not start %s: %v", a.Entry.Name, err))
		} else {
			l.st.Done(nil)
		}
	case shell.Copy:
		if err := copyText(a.Text); err != nil {
			l.st.Done(fmt.Errorf("could not copy: %v", err))
		} else {
			l.st.Done(nil)
		}
	case shell.Focus:
		log.Printf("launcher: focus %s", a.Address)
		s.focusWindow(a.Address)
		l.st.Done(nil)
	case shell.Fill:
		l.query.SetText(a.Text)
		l.query.SetPosition(-1)
	case shell.Open:
		log.Printf("launcher: open %s", a.Text)
		if err := desktop.Spawn([]string{"xdg-open", a.Text}, ""); err != nil {
			l.st.Done(fmt.Errorf("could not open it: %v", err))
		} else {
			l.st.Done(nil)
		}
	case shell.Ask:
		log.Printf("launcher: ask the assistant")
		if err := desktop.Spawn([]string{"inua-assistant", "--ask", a.Text}, ""); err != nil {
			l.st.Done(fmt.Errorf("could not start the assistant: %v", err))
		} else {
			l.st.Done(nil)
		}
	case shell.Run:
		log.Printf("launcher: %s %s", a.Text, a.Arg)
		l.st.Done(nil)
		// After the launcher is gone: a screenshot must not catch it, and a
		// window command finds its window focused again.
		s.runAfter(120, func() { s.runCommand(a.Text, a.Arg, a.Address) })
	}
	s.syncLauncher()
}

// runCommand carries out a launcher command (shell.Commands) on the
// window it was opened over, if it is one for a window.
func (s *shellUI) runCommand(id, arg, window string) {
	w := "address:" + window
	switch id {
	case "lock":
		if err := s.lockScreen(); err != nil {
			log.Printf("lock: %v", err)
		}
	case "suspend", "reboot", "poweroff", "logout":
		s.doPower(id)
	case "trash":
		go func() {
			if out, err := exec.Command("gio", "trash", "--empty").CombinedOutput(); err != nil {
				log.Printf("empty trash: %v %s", err, out)
			}
		}()
	case "settings":
		s.openSettings()
	case "shot-area", "shot-window", "shot-screen", "record", "color":
		fn := map[string]string{"shot-area": "area", "shot-window": "pick-window", "shot-screen": "pick-screen", "record": "record", "color": "color"}[id]
		s.screenshotRequest(fn)
	case "clear-notes":
		s.clearNotes(s.notes.c.ClearAll())
	case "focus":
		s.controlAction("dnd")
	case "wifi":
		s.controlAction("wifi")
	case "bluetooth":
		s.controlAction("bt")
	case "bluetooth-settings":
		s.openSettings()
		s.showSettingsPage("bluetooth")
	case "wifi-settings":
		s.openSettings()
		s.showSettingsPage("wifi")
	case "restart-shell":
		if err := s.scheduleRestart(); err != nil {
			log.Printf("restart: %v", err)
		}
	case "workspace":
		s.switchWorkspace(arg)
	case "move-window":
		id := strings.TrimPrefix(w, "address:")
		s.compositorDo("move window", func(ctx context.Context) error { return s.comp.MoveWindow(ctx, id, arg, true) })
	case "close-window":
		s.closeWindow(strings.TrimPrefix(w, "address:"))
	case "float":
		s.hyprDispatch("togglefloating "+w, "")
	case "fullscreen":
		s.hyprDispatch("focuswindow "+w, "")
		s.hyprDispatch("fullscreen 0", "")
	case "center":
		s.hyprDispatch("focuswindow "+w, "")
		s.hyprDispatch("centerwindow", "")
	default:
		log.Printf("launcher: unknown command %q", id)
	}
}

// applyLauncherWeb gives the launcher the web search and quicklinks, and
// the assistant's row when it is installed and on.
func (s *shellUI) applyLauncherWeb() {
	_, err := exec.LookPath("inua-assistant")
	s.launcher.st.SetAssistant(s.cfg.Assistant.Enable && err == nil)
	var links []shell.Quicklink
	for _, q := range s.cfg.Launcher.Quicklinks {
		links = append(links, shell.Quicklink{Keyword: q.Keyword, Name: q.Name, URL: q.URL})
	}
	s.launcher.st.SetWeb(s.cfg.Launcher.Search, links)
}

func (s *shellUI) launcherKey(keyval uint32) bool {
	l := &s.launcher
	if l.menu != nil { // the keys are the menu's: Esc closes it alone
		return false
	}
	switch keyval {
	case keyEscape:
		s.cancelLauncher()
		return true
	case keyReturn, keyKPEnter:
		s.executeLauncher()
		return true
	case keyDown:
		l.st.Step(l.st.Lines(gridCols), 0, 1)
	case keyUp:
		l.st.Step(l.st.Lines(gridCols), 0, -1)
	case keyRight, keyLeft:
		// Along a line of the grid; on a row they move in the search.
		dx := 1
		if keyval == keyLeft {
			dx = -1
		}
		if !l.st.Step(l.st.Lines(gridCols), dx, 0) {
			return false
		}
	case keyPageDown:
		l.st.Step(l.st.Lines(gridCols), 0, pageLines)
	case keyPageUp:
		l.st.Step(l.st.Lines(gridCols), 0, -pageLines)
	case keyTab, keyISOLeftTab:
		s.showLauncherActions()
		return true
	default:
		return false
	}
	s.renderLauncher()
	return true
}

// syncLauncher makes the window match the state: built when the launcher
// opened, destroyed when it closed, redrawn while open.
func (s *shellUI) syncLauncher() {
	l := &s.launcher
	switch {
	case l.st.Open && l.win == nil:
		s.dropClosedLauncher()
		s.buildLauncher()
		s.renderLauncher()
		s.showLauncherCatch() // before the launcher, so under it
		l.win.Present()
		l.query.GrabFocus()
	case !l.st.Open && l.win != nil:
		// It fades away (style.css); the keyboard goes back at once, for
		// the window focused after it.
		if s.layer {
			layershell.SetKeyboardMode(l.win.GoPointer(), layershell.KeyboardNone)
		}
		if box := l.win.GetChild(); box != nil {
			box.AddCssClass("closing")
		}
		l.closed = l.win
		l.win, l.hasFocus, l.menu = nil, false, nil
		s.dropLauncherCatch()
		l.gone = glib.TimeoutAdd(closeMS, &s.onLauncherGone, 0)
	case l.st.Open:
		s.renderLauncher()
	}
}

// showLauncherCatch covers the launcher's monitor with a clear window
// under it, the bar too, so that a click anywhere outside the launcher
// closes it; on the bar, the entry clicked does its own as well.
func (s *shellUI) showLauncherCatch() {
	l := &s.launcher
	if !s.layer || l.catch != nil {
		return
	}
	l.catch = gtk.NewApplicationWindow(s.app)
	l.catch.AddCssClass("launcher-catch")
	click := gtk.NewGestureClick()
	click.SetButton(0) // any button
	click.ConnectPressed(&s.onLauncherCatch)
	l.catch.AddController(&click.EventController)
	w := l.catch.GoPointer()
	layershell.InitForWindow(w)
	layershell.SetNamespace(w, "inua-launcher-catch")
	layershell.SetLayer(w, layershell.LayerTop)
	layershell.SetKeyboardMode(w, layershell.KeyboardNone)
	for _, e := range []layershell.Edge{layershell.EdgeTop, layershell.EdgeBottom, layershell.EdgeLeft, layershell.EdgeRight} {
		layershell.SetAnchor(w, e, true)
	}
	layershell.SetExclusiveZone(w, -1)
	if mon := s.focusedMonitor(); mon != nil {
		layershell.SetMonitor(w, mon.GoPointer())
	}
	l.catch.Present()
}

// dropLauncherCatch takes the catching window away, the launcher closing.
func (s *shellUI) dropLauncherCatch() {
	l := &s.launcher
	if l.catch != nil {
		l.catch.Destroy()
		l.catch = nil
	}
}

// closeMS is how long the launcher takes to fade away (launcher-out).
const closeMS = 130

// dropClosedLauncher destroys the window of a launcher closed and fading
// away, if there is one.
func (s *shellUI) dropClosedLauncher() {
	l := &s.launcher
	if l.gone != 0 {
		glib.SourceRemove(l.gone)
		l.gone = 0
	}
	if l.closed != nil {
		l.closed.Destroy()
		l.closed = nil
		l.refs.Release()
	}
}

// buildLauncher instantiates launcher.ui and turns it into an overlay layer
// surface with exclusive keyboard focus on the focused monitor.
func (s *shellUI) buildLauncher() {
	l := &s.launcher
	l.win = &gtk.ApplicationWindow{}
	l.opens++
	ui := s.load("launcher.ui", &l.refs)
	ui.Get("launcher", l.win)
	ui.Get("mode", &l.mode)
	ui.Get("query", &l.query)
	ui.Get("scroll", &l.scroll)
	ui.Get("results", &l.results)
	ui.Get("highlight", &l.highlight)
	ui.Get("foot", &l.foot)
	ui.Get("blur", &l.blur)
	ui.Get("fade", &l.fade)
	ui.Get("pinned", &l.pinned)
	ui.Get("pinned_scroll", &l.pinScroll)
	ui.Get("message_box", &l.messageBox)
	ui.Get("message_tag", &l.messageTag)
	ui.Get("message", &l.message)
	ui.Close()

	l.win.SetApplication(s.app)
	w := l.win.GoPointer()
	if s.layer {
		layershell.InitForWindow(w)
		layershell.SetNamespace(w, "inua-launcher")
		layershell.SetLayer(w, layershell.LayerTop)
		layershell.SetKeyboardMode(w, layershell.KeyboardExclusive)
		if mon := s.focusedMonitor(); mon != nil {
			layershell.SetMonitor(w, mon.GoPointer())
		}
		// No anchors: the compositor centers the surface.
	} else {
		l.win.SetTitle(i18n.T("inua launcher"))
	}

	// Keys are handled in the capture phase, before the entry sees them.
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCaptureValue)
	keys.ConnectKeyPressed(&s.onKey)
	l.win.AddController(&keys.EventController) // takes the only reference

	focus := gtk.NewEventControllerFocus()
	focus.ConnectEnter(&s.onQueryFocus)
	l.query.AddController(&focus.EventController)

	gtkx.ConnectChanged(&l.query, &s.onQueryChanged)
	l.query.ConnectActivate(&s.onQueryActivate)
	adj := l.scroll.GetVadjustment()
	adj.ConnectValueChanged(&s.onLauncherScrolled)
	adj.ConnectChanged(&s.onLauncherLaidOut)
	l.shown, l.lines, l.items = -1, nil, nil
	l.tick, l.hlShown = 0, false
	s.buildFoot()
}

// buildFoot makes the blurred foot's strips, empty till placeFoot copies
// lines into them.
func (s *shellUI) buildFoot() {
	l := &s.launcher
	l.strips, l.footFrom, l.footTo = l.strips[:0], -1, -1
	for _, p := range l.footIcons {
		p.Unref()
	}
	l.footIcons = map[string]*gtk.IconPaintable{}
	for i := range blurSteps {
		ghost := gtk.NewBox(gtk.OrientationVerticalValue, 0)
		ghost.AddCssClass("ghost")
		ghost.AddCssClass(fmt.Sprintf("blur%d", i+1))
		view := gtk.NewScrolledWindow()
		view.SetPolicy(gtk.PolicyExternalValue, gtk.PolicyExternalValue)
		view.SetPropagateNaturalHeight(false)
		view.SetMinContentHeight(stripHeight)
		view.SetMaxContentHeight(stripHeight)
		view.SetChild(&ghost.Widget)
		view.GetVadjustment().ConnectChanged(&s.onLauncherFootLaidOut)
		l.strips = append(l.strips, blurStrip{view: *view, ghost: *ghost})
		ghost.Unref()
		gtkx.Append(&l.blur, &view.Widget)
	}
}

// renderLauncher shows the state: the results, built again when they
// changed, rows from row.ui and in apps mode the apps side by side, cells
// of appcell.ui; the selected one marked and scrolled into view.
func (s *shellUI) renderLauncher() {
	l := &s.launcher
	if l.win == nil {
		return
	}
	if l.query.GetText() != l.st.Query { // Show started over
		l.query.SetText(l.st.Query)
	}
	l.mode.SetText(l.st.Mode.Tag())
	msg := l.st.Message()
	l.message.SetText(msg)
	l.messageTag.SetVisible(l.st.Failed())
	l.messageBox.SetVisible(msg != "")

	if l.shown != l.st.Version {
		s.fillLauncher()
	} else if l.selected != l.st.Selected {
		l.follow = true
	}
	l.selected = l.st.Selected
	for i := l.pin; i < len(l.items); i++ { // the pinned rows mark themselves
		toggle(&l.items[i], "selected", i == l.selected)
	}
	s.animateLauncher()
	s.placeFoot()
}

// fillLauncher builds every result, from the top: apps to scroll, and the
// quick actions after them pinned below the foot (with no apps, they
// scroll themselves).
func (s *shellUI) fillLauncher() {
	l := &s.launcher
	gtkx.Clear(&l.results)
	gtkx.Clear(&l.pinned)
	l.lines, l.items = l.lines[:0], make([]gtk.Widget, len(l.st.Results))
	l.pin = l.st.Pinned()
	rowUI, cellUI := s.readUI("row.ui"), s.readUI("appcell.ui")
	grid := false
	for _, ln := range l.st.Lines(gridCols) {
		var w gtk.Widget
		switch apps := l.st.Results[ln.Start].Entry != nil; {
		case !ln.Grid: // windows mode: a row the window's width
			w = *s.launcherRow(rowUI, ln.Start)
		case apps:
			line := gtk.NewBox(gtk.OrientationHorizontalValue, 4)
			line.AddCssClass("grid-line")
			line.SetHalign(gtk.AlignStartValue) // a short line from the left
			for i := ln.Start; i < ln.End; i++ {
				gtkx.Append(line, s.launcherCell(cellUI, i))
			}
			w, grid = line.Widget, true
		default: // quick actions, two to a line, a lone one half wide
			line := gtk.NewBox(gtk.OrientationHorizontalValue, 4)
			line.SetHomogeneous(true)
			for i := ln.Start; i < ln.End; i++ {
				gtkx.Append(line, s.launcherRow(rowUI, i))
			}
			for range shell.RowCols - (ln.End - ln.Start) {
				gtkx.Append(line, &gtk.NewBox(gtk.OrientationHorizontalValue, 0).Widget)
			}
			w = line.Widget
		}
		if ln.Start >= l.pin {
			gtkx.Append(&l.pinned, &w)
			continue
		}
		l.lines = append(l.lines, shownLine{w: w, ln: ln})
		gtkx.Append(&l.results, &w)
	}
	l.pinScroll.SetVisible(l.pin < len(l.items))
	l.pinScroll.GetVadjustment().SetValue(0)
	l.foot.SetVisible(grid) // only apps blur
	l.shown = l.st.Version
	l.scroll.GetVadjustment().SetValue(0)
	l.jump, l.follow = true, true
	l.footFrom, l.footTo = -2, -2 // copy the foot's lines again
}

// bounds is a widget's place in what scrolls (the overlay holding the
// results and the highlight, whose coordinates the view's scroll and the
// highlight's margins are in): x, y, width, height, false before it is
// laid out.
func (s *shellUI) bounds(w *gtk.Widget) ([4]float64, bool) {
	var r graphene.Rect
	if !w.ComputeBounds(s.launcher.highlight.GetParent(), &r) {
		return [4]float64{}, false
	}
	f := *(*[4]float32)(unsafe.Pointer(&r))
	return [4]float64{float64(f[0]), float64(f[1]), float64(f[2]), float64(f[3])}, f[3] > 0
}

// showPinned scrolls the pinned quick actions so that result i is in
// sight.
func (s *shellUI) showPinned(i int) {
	l := &s.launcher
	var r graphene.Rect
	if !l.items[i].ComputeBounds(&l.pinned.Widget, &r) {
		return
	}
	f := *(*[4]float32)(unsafe.Pointer(&r))
	y, h := float64(f[1]), float64(f[3])
	adj := l.pinScroll.GetVadjustment()
	v, page := adj.GetValue(), adj.GetPageSize()
	switch {
	case page <= 0:
	case y < v:
		adj.SetValue(y)
	case y+h > v+page:
		adj.SetValue(y + h - page)
	}
}

// animateLauncher starts the frames that bring the view and the highlight
// to the selection, unless they run already.
func (s *shellUI) animateLauncher() {
	l := &s.launcher
	if l.win == nil || l.tick != 0 {
		return
	}
	l.last = time.Now()
	l.tick = l.win.AddTickCallback(&s.onLauncherTick, 0, &noTickDone)
}

// stepLauncher moves the view and the highlight a frame towards the
// selection, and reports whether there is more to do. Straight there when
// the results were just built or animations are off.
func (s *shellUI) stepLauncher() bool {
	l := &s.launcher
	if l.win == nil {
		l.tick = 0
		return false
	}
	now := time.Now()
	dt := min(now.Sub(l.last).Seconds(), 0.034)
	l.last = now
	k := 1 - math.Exp(-dt/glideTau)
	if l.jump || !gtk.SettingsGetDefault().GetPropertyGtkEnableAnimations() {
		k = 1
	}
	busy := false
	sel := l.st.Selected
	inView := sel < l.pin && sel < len(l.items)

	// The view: the selected line wholly in it, keepClear from its edges.
	if l.follow {
		adj := l.scroll.GetVadjustment()
		v, page := adj.GetValue(), adj.GetPageSize()
		var line *shownLine
		for n := range l.lines {
			if sel >= l.lines[n].ln.Start && sel < l.lines[n].ln.End {
				line = &l.lines[n]
			}
		}
		b, ok := [4]float64{}, false
		if line != nil && inView {
			b, ok = s.bounds(&line.w)
		}
		switch {
		case line == nil || !inView:
			l.follow = false
		case !ok || page <= 0:
			busy = true // not laid out yet
		default:
			to := v
			if b[1]-keepClear < v {
				to = b[1] - keepClear
			} else if b[1]+b[3]+keepClear > v+page {
				to = b[1] + b[3] + keepClear - page
			}
			to = max(0, min(to, adj.GetUpper()-page))
			if math.Abs(to-v) < 0.5 {
				adj.SetValue(to)
				l.follow = false
			} else {
				adj.SetValue(v + (to-v)*k)
				busy = true
			}
		}
	}

	// The highlight, over the selected result in the view.
	if !inView {
		l.highlight.SetVisible(false)
		l.hlShown = false
	} else if to, ok := s.bounds(&l.items[sel]); !ok {
		busy = true
	} else {
		if !l.hlShown || k == 1 {
			l.hl = to
		} else {
			far := false
			for n := range l.hl {
				l.hl[n] += (to[n] - l.hl[n]) * k
				far = far || math.Abs(to[n]-l.hl[n]) >= 0.5
			}
			if !far {
				l.hl = to
			}
			busy = busy || far
		}
		l.highlight.SetMarginStart(int32(math.Round(l.hl[0])))
		l.highlight.SetMarginTop(int32(math.Round(l.hl[1])))
		l.highlight.SetSizeRequest(int32(math.Round(l.hl[2])), int32(math.Round(l.hl[3])))
		l.highlight.SetVisible(true)
		l.hlShown = true
	}
	if sel >= l.pin && sel < len(l.items) {
		s.showPinned(sel)
	}
	// The foot's strips take their places once laid out, which no
	// signal the bindings have tells.
	if !s.placeFoot() {
		busy = true
	}
	if !busy {
		l.jump = false
		l.tick = 0
	}
	return busy
}

// placeFoot copies into the foot's strips the lines just below the view
// (again only when they change) and scrolls each strip to line up with
// the results there: blank past their end. It reports whether every strip
// is in place: one set while it is laid out keeps where it was.
func (s *shellUI) placeFoot() bool {
	l := &s.launcher
	if l.win == nil {
		return true
	}
	adj := l.scroll.GetVadjustment()
	foot := adj.GetValue() + adj.GetPageSize()
	l.fade.SetVisible(adj.GetUpper() > foot+1) // over more to come only
	if !l.foot.GetVisible() {
		return true
	}
	// The lines from the view's foot down the foot's height.
	from, to, top := -1, -1, 0.0
	band := float64(blurSteps * stripHeight)
	for n := range l.lines {
		b, ok := s.bounds(&l.lines[n].w)
		if !ok {
			return false // not laid out yet
		}
		if b[1]+b[3] <= foot || b[1] >= foot+band {
			continue
		}
		if from < 0 {
			from, top = n, b[1]
		}
		to = n + 1
	}
	// Copied with two lines to spare each way, so a scroll copies again
	// only every few lines.
	switch covered := from >= l.footFrom && to <= l.footTo && l.footFrom >= 0; {
	case from < 0 && l.footFrom != -1:
		s.copyFoot(-1, -1)
		l.footFrom, l.footTo = -1, -1
	case from >= 0 && !covered:
		cf, ct := max(0, from-2), min(len(l.lines), to+2)
		if b, ok := s.bounds(&l.lines[cf].w); ok {
			top = b[1]
		}
		s.copyFoot(cf, ct)
		l.footFrom, l.footTo, l.footTop = cf, ct, top
	}
	placed := true
	for i, st := range l.strips {
		adj := st.view.GetVadjustment()
		want := foot - l.footTop + float64(i*stripHeight)
		if from < 0 {
			want = 0
		}
		adj.SetValue(want)
		end := adj.GetUpper() - adj.GetPageSize()
		placed = placed && adj.GetUpper() > band && math.Abs(adj.GetValue()-min(want, end)) < 0.5
	}
	return placed
}

// copyFoot fills each strip with copies of lines from to to, each as
// tall as the line, and a blank as tall as the foot below them. Apps are
// copied; a row (not blurred) is left blank.
func (s *shellUI) copyFoot(from, to int) {
	l := &s.launcher
	for _, st := range l.strips {
		gtkx.Clear(&st.ghost)
		for n := from; n < to && n >= 0; n++ {
			ln := l.lines[n]
			line := gtk.NewBox(gtk.OrientationHorizontalValue, 4)
			line.SetHalign(gtk.AlignStartValue)
			if b, ok := s.bounds(&ln.w); ok {
				line.SetSizeRequest(-1, int32(math.Round(b[3])))
			}
			for i := ln.ln.Start; ln.ln.Grid && i < ln.ln.End && l.st.Results[i].Entry != nil; i++ {
				gtkx.Append(line, s.ghostCell(i))
			}
			gtkx.Append(&st.ghost, &line.Widget)
		}
		blank := gtk.NewBox(gtk.OrientationVerticalValue, 0)
		blank.SetSizeRequest(-1, blurSteps*stripHeight)
		gtkx.Append(&st.ghost, &blank.Widget)
	}
}

// footIcon is the icon setIcon would show for name, at the grid's size,
// from the foot's cache: the same themes in the same order.
func (s *shellUI) footIcon(name string) *gtk.IconPaintable {
	l := &s.launcher
	if p, ok := l.footIcons[name]; ok {
		return p
	}
	const size = 64
	scale := l.win.GetScaleFactor()
	var p *gtk.IconPaintable
	display := gtk.IconThemeGetForDisplay(gdk.DisplayGetDefault())
	defer display.Unref()
	if n := themedFile(name); strings.HasPrefix(name, "/") && n != "" {
		p = appIcons.LookupIcon(n, nil, size, scale, gtk.TextDirLtrValue, 0)
	} else if strings.HasPrefix(name, "/") {
		f := gio.FileNewForPath(name)
		p = gtk.NewIconPaintableForFile(f, size, scale)
		unref(f)
	} else {
		var themes []*gtk.IconTheme
		if strings.HasPrefix(name, "io.github.lyricat.Inua.") && inuaIcons != nil {
			themes = append(themes, inuaIcons)
		}
		if appIcons != nil {
			themes = append(themes, appIcons)
		}
	find:
		for _, t := range append(themes, display) {
			for _, n := range []string{name, strings.ToLower(name)} {
				if n != "" && t.HasIcon(n) {
					p = t.LookupIcon(n, nil, size, scale, gtk.TextDirLtrValue, 0)
					break find
				}
			}
		}
		if p == nil {
			p = display.LookupIcon("application-x-executable", nil, size, scale, gtk.TextDirLtrValue, 0)
		}
	}
	l.footIcons[name] = p
	return p
}

// ghostCell is a copy of app i's cell (appcell.ui) for the foot: its icon
// and its name, nothing to click.
func (s *shellUI) ghostCell(i int) *gtk.Widget {
	r := s.launcher.st.Results[i]
	cell := gtk.NewBox(gtk.OrientationVerticalValue, 6)
	cell.AddCssClass("cell")
	cell.SetSizeRequest(124, -1)
	cell.SetHexpand(false)
	icon := gtk.NewImage()
	if p := s.footIcon(r.Icon); p != nil {
		icon.SetFromPaintable(p)
	}
	icon.SetPixelSize(64)
	icon.SetHalign(gtk.AlignCenterValue)
	gtkx.Append(cell, &icon.Widget)
	title := gtk.NewLabel(r.Title)
	title.AddCssClass("title")
	title.SetJustify(gtk.JustifyCenterValue)
	title.SetEllipsize(pango.EllipsizeEndValue)
	title.SetMaxWidthChars(1)
	title.SetHexpand(true)
	gtkx.Append(cell, &title.Widget)
	return &cell.Widget
}

// launcherRow is result i as a row, for the results to hold.
func (s *shellUI) launcherRow(rowUI string, i int) *gtk.Widget {
	l := &s.launcher
	r := l.st.Results[i]
	var refs gtkx.Scope
	ui, err := gtkx.Load(rowUI, &refs)
	if err != nil {
		log.Fatalf("row.ui: %v", err)
	}
	var row gtk.Box
	var icon gtk.Image
	var title, detail, glyph gtk.Label
	ui.Take("row", &row)
	ui.Get("icon", &icon)
	ui.Get("glyph", &glyph)
	ui.Get("title", &title)
	ui.Get("detail", &detail)
	ui.Close()
	if r.Glyph != "" {
		icon.SetVisible(false)
		glyph.SetText(r.Glyph)
		glyph.SetVisible(true)
	} else {
		setIcon(&icon, r.Icon)
	}
	title.SetText(r.Title)
	detail.SetText(r.Detail)
	detail.SetVisible(r.Detail != "")
	s.markLauncherResult(&row.Widget, i)
	refs.Release()
	return &row.Widget
}

// launcherCell is app i as a cell of the grid, for its line to hold.
func (s *shellUI) launcherCell(cellUI string, i int) *gtk.Widget {
	r := s.launcher.st.Results[i]
	var refs gtkx.Scope
	ui, err := gtkx.Load(cellUI, &refs)
	if err != nil {
		log.Fatalf("appcell.ui: %v", err)
	}
	var cell gtk.Box
	var icon gtk.Image
	var title gtk.Label
	ui.Take("cell", &cell)
	ui.Get("icon", &icon)
	ui.Get("title", &title)
	ui.Close()
	setIcon(&icon, r.Icon)
	title.SetText(r.Title)
	cell.SetTooltipText(cmp.Or(r.Detail, r.Title))
	cell.SetHexpand(false) // its name's expanding must not widen it
	s.markLauncherResult(&cell.Widget, i)
	refs.Release()
	return &cell.Widget
}

// markLauncherResult names a result's widget after its index, for a click
// to open it, and marks the selected one.
func (s *shellUI) markLauncherResult(w *gtk.Widget, i int) {
	s.launcher.items[i] = *w
	w.SetName(fmt.Sprintf("result:%d", i))
	click := gtk.NewGestureClick()
	click.ConnectReleased(&s.onLauncherPick)
	w.AddController(&click.EventController)
}

// setIcon shows an app icon (a theme name or an absolute path) or a window
// class, which usually names the app's icon too, in any case.
func setIcon(img *gtk.Image, name string) {
	if strings.HasPrefix(name, "/") {
		if n := themedFile(name); n != "" && setFromTheme(img, appIcons, n) {
			return
		}
		img.SetFromFile(name)
		return
	}
	if strings.HasPrefix(name, "io.github.lyricat.Inua.") && setFromTheme(img, inuaIcons, name) {
		return
	}
	if setFromTheme(img, appIcons, name) {
		return
	}
	theme := gtk.IconThemeGetForDisplay(gdk.DisplayGetDefault())
	defer theme.Unref()
	for _, n := range []string{name, strings.ToLower(name)} {
		if n != "" && theme.HasIcon(n) {
			img.SetFromIconName(n)
			return
		}
	}
	img.SetFromIconName("application-x-executable")
}

// themedFile is the name, in the theme app icons come from, of an app's
// icon given as a file, when the theme draws it: "" for none. An icon
// theme cannot stand in for a file, so a snap's (Chromium's
// /snap/chromium/current/chromium.png) would never follow it otherwise.
func themedFile(path string) string {
	if appIcons == nil {
		return ""
	}
	for _, n := range fileIconNames(path) {
		if appIcons.HasIcon(n) {
			return n
		}
	}
	return ""
}

// fileIconNames are the names an app's icon file may go by in an icon
// theme: a snap's name (/snap/NAME/…), then the file's own, without its
// extension.
func fileIconNames(path string) []string {
	var names []string
	if rest, ok := strings.CutPrefix(path, "/snap/"); ok {
		if snap, _, _ := strings.Cut(rest, "/"); snap != "" {
			names = append(names, snap)
		}
	}
	base := filepath.Base(path)
	if base = strings.TrimSuffix(base, filepath.Ext(base)); base != "" && base != "." && base != "/" && !slices.Contains(names, base) {
		names = append(names, base)
	}
	return names
}

// setFromTheme shows the theme's file for the icon name, which the image
// draws at its own size and scale; the largest, so that one drawn of a
// fixed size is only ever scaled down. It reports whether the theme has it.
func setFromTheme(img *gtk.Image, theme *gtk.IconTheme, name string) bool {
	if theme == nil {
		return false
	}
	for _, n := range []string{name, strings.ToLower(name)} {
		if n == "" || !theme.HasIcon(n) {
			continue
		}
		p := theme.LookupIcon(n, nil, 256, 1, gtk.TextDirLtrValue, 0)
		if p == nil {
			continue
		}
		file := p.GetFile()
		p.Unref()
		if file == nil {
			continue
		}
		icon := gio.NewFileIcon(file)
		(&gobject.Object{Ptr: file.Ptr}).Unref() // GetFile gave a reference
		img.SetFromGicon(icon)
		icon.Unref()
		return true
	}
	return false
}

// freshenRates gives the launcher's answers exchange rates: the cached
// ones at once, and new ones when those are stale (convert.Rates.Stale).
func (s *shellUI) freshenRates() {
	l := &s.launcher
	if l.rates == nil {
		if r := convert.LoadRates(convert.CachePath()); r != nil {
			l.rates = r
			l.st.SetRates(r)
		}
	}
	if l.fetching || !l.rates.Stale(time.Now()) {
		return
	}
	l.fetching = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		r, err := convert.FetchRates(ctx, &http.Client{})
		cancel()
		if err == nil {
			if err := convert.SaveRates(convert.CachePath(), r); err != nil {
				log.Printf("rates: %v", err)
			}
		}
		gtkx.Post(func() {
			l.fetching = false
			if err != nil {
				log.Printf("rates: %v", err)
				return
			}
			l.rates = r
			l.st.SetRates(r)
			if l.st.Open {
				s.syncLauncher()
			}
		})
	}()
}

// showLauncherActions opens the selected result's actions
// (shell.ItemActions) in a menu under it, at Tab.
func (s *shellUI) showLauncherActions() {
	l := &s.launcher
	i := l.st.Selected
	if l.win == nil || i >= len(l.st.Results) || i >= len(l.items) {
		return
	}
	r := l.st.Results[i]
	var pinned bool
	var windows int
	if r.Entry != nil {
		pinned = slices.Contains(s.cfg.Dock.Pinned, r.Entry.ID)
		if it, ok := s.dockItem(r.Entry.ID); ok {
			windows = len(it.Windows)
		}
	}
	acts := shell.ItemActions(r, pinned, windows)
	if len(acts) == 0 {
		return
	}
	var lines []uikit.Item
	for _, act := range acts {
		id := act.ID
		lines = append(lines, uikit.Item{Label: act.Title, Run: func() { s.launcherAction(id) }})
	}
	w := &l.items[i]
	pop := uikit.NewMenu(lines, uikit.Below)
	pop.SetParent(w)
	pop.ConnectClosed(&s.onLauncherMenuClosed)
	pop.Unref() // the result holds it
	l.actionsOf, l.menu = i, pop
	pop.Popup()
}

// launcherAction carries out one of a result's actions: the one Enter
// does through executeLauncher, the others here, the launcher closing
// first where they act on a window.
func (s *shellUI) launcherAction(id string) {
	l := &s.launcher
	i := l.actionsOf
	if !l.st.Open || i >= len(l.st.Results) {
		return
	}
	r := l.st.Results[i]
	l.st.Move(i - l.st.Selected)
	switch id {
	case "open", "focus", "copy", "open-url":
		s.executeLauncher()
		return
	case "new":
		s.launchApp(r.Entry)
		l.st.Done(nil)
	case "pin", "unpin":
		c := s.cfg
		c.Dock.Pinned = shell.Pin(c.Dock.Pinned, r.Entry.ID, id == "pin")
		s.changeSettings(c)
		l.st.Done(nil)
	case "quit":
		if it, ok := s.dockItem(r.Entry.ID); ok {
			for _, w := range it.Windows {
				s.closeWindow(w.ID)
			}
		}
		l.st.Done(nil)
	case "copy-url":
		if err := copyText(r.URL); err != nil {
			l.st.Done(fmt.Errorf("could not copy: %v", err))
		} else {
			l.st.Done(nil)
		}
	default:
		var own int
		if _, err := fmt.Sscanf(id, "own-%d", &own); err == nil && r.Entry != nil && own < len(r.Entry.Actions) {
			// The app's own: its entry with the action's command.
			e := *r.Entry
			e.Command = r.Entry.Actions[own].Command
			log.Printf("launcher: %s %s", e.ID, r.Entry.Actions[own].ID)
			if err := desktop.LaunchWith(&e, s.cfg.TerminalCommand()); err != nil {
				l.st.Done(fmt.Errorf("could not start %s: %v", e.Name, err))
			} else {
				l.st.Done(nil)
			}
			break
		}
		// A window's: close-window, float, fullscreen, center.
		l.st.Done(nil)
		s.runAfter(120, func() { s.runCommand(id, "", r.Address) })
	}
	s.syncLauncher()
}
