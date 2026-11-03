package ui

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gobject/types"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"codeberg.org/puregotk/puregotk/v4/pango"

	"github.com/lyricat/inua/internal/capture/hyprland"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/hypr"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/layershell"
	"github.com/lyricat/inua/internal/shell"
)

// workspaceOverview is a full-screen, keyboard-driven workspace chooser.
// Hyprland toplevel frames arrive from the capture worker and are painted by
// GTK on the main thread.
type workspaceOverview struct {
	mouseGuard           bool
	killLayer            *gtk.Fixed
	killGlyph            *gtk.Label
	onPointer            func(gtk.EventControllerMotion, float64, float64)
	syncingQuery         bool
	historyLast          string
	onQueryFocus         func(gtk.EventControllerFocus)
	activation           uint64
	monitor              string
	inputMonitor         string
	views                map[string]*workspaceOverview
	st                   shell.Overview
	win                  *gtk.ApplicationWindow
	root, cards          gtk.Box
	query                gtk.Entry
	queryClear           gtk.Button
	searchStatus         gtk.Label
	mru                  gtk.Button
	images               map[string]*gtk.Picture
	thumbPaths           map[string]string
	thumbStarted         map[string]bool
	layoutKey            string
	cardWidgets          map[int]*overviewCardWidgets
	windowWidgets        map[string]*overviewWindowWidgets
	renderedTargets      []uintptr
	textures             map[string]*gdk.MemoryTexture
	capture              *hyprland.Stream
	captureFailed        bool
	captureGen           uint64
	filterGen            uintptr
	renderGen            uintptr
	captureMu            sync.Mutex
	pendingFrames        map[string]queuedOverviewFrame
	previewSize          map[string][2]int
	frameDrainQueued     bool
	columns              int
	refs                 gtkx.Scope
	searchMode, killMode bool
	dragging             string
	movesPending         int
	dragX, dragY         float64
	targets              map[uintptr]overviewDropTarget
	onDragPrepare        func(gtk.DragSource, float64, float64) gdk.ContentProvider
	onDragBegin          func(gtk.DragSource, uintptr)
	onDragEnd            func(gtk.DragSource, uintptr, bool)
	onWindowGesture      func(gtk.GestureClick, int32, float64, float64)
	onOverviewScroll     func(gtk.EventControllerScroll, float64, float64) bool
	onOverviewHover      func(gtk.EventControllerMotion, float64, float64)
	onOverviewRight      func(gtk.GestureClick, int32, float64, float64)
	onSurfacePress       func(gtk.GestureClick, int32, float64, float64)
	hint                 *gtk.Label
	scope                gtk.Button
}

type queuedOverviewFrame struct {
	gen   uint64
	frame hyprland.Frame
}

func (s *shellUI) showOverview() {
	s.closeClipboard()
	s.closePasswords()
	if s.overview.st.Open {
		s.closeOverview(false)
		return
	}
	if !s.hyprLua && s.wayfire == nil {
		// Hyprland without Lua (before 0.55): the launcher's windows
		// instead, what Super+Tab opened before the overview.
		s.showLauncher(shell.Windows)
		return
	}
	if s.launcher.st.Open {
		s.cancelLauncher()
	}
	s.overview.st.MRU = s.cfg.Overview.MRU
	s.overview.st.Show(s.snap)
	s.overview.activation++
	s.overview.captureFailed = false
	s.overview.searchMode = false
	s.overview.killMode = false
	s.overview.targets = map[uintptr]overviewDropTarget{}
	s.overview.dragging = ""
	s.overview.monitor = s.snap.FocusedMonitor
	s.overview.inputMonitor = s.snap.FocusedMonitor
	s.overview.views = map[string]*workspaceOverview{}
	for _, mon := range s.snap.Monitors {
		if mon.Name != s.overview.monitor {
			s.overview.views[mon.Name] = &workspaceOverview{monitor: mon.Name}
		}
	}
	if s.overview.textures == nil {
		s.overview.textures = map[string]*gdk.MemoryTexture{}
	}
	s.overview.thumbPaths = map[string]string{}
	s.overview.thumbStarted = map[string]bool{}
	// Keep the most recent frame for each live window while the overview is
	// closed. The next open can paint these immediately while capture refreshes.
	live := make(map[string]bool, len(s.snap.Toplevels))
	for _, w := range s.snap.Toplevels {
		live[w.Address] = true
	}
	for address, texture := range s.overview.textures {
		if !live[address] {
			texture.Unref()
			delete(s.overview.textures, address)
		}
	}
	s.renderOverview()
	if s.wayfire != nil {
		s.fillOverviewThumbnails(&s.overview, s.snap, s.overview.activation)
		for _, view := range s.overview.views {
			s.fillOverviewThumbnails(view, s.snap, s.overview.activation)
		}
	}
	s.syncOverviewDocks()
	s.guardOverviewMouse(true)
	log.Printf("overview: open (%d workspaces) wallpaper=%q", len(s.overview.st.Cards), s.cfg.ImagePath())
}

func (s *shellUI) closeOverview(restore bool) {
	o := &s.overview
	if !o.st.Open {
		return
	}
	// Destroying a surface may synchronously emit drag-end. Close the model
	// first so that callback cannot rebuild widgets during their destruction.
	previous := o.st.Previous
	o.st.Close()
	o.activation++
	o.dragging = ""
	o.movesPending = 0
	o.killMode = false
	s.guardOverviewMouse(false)
	for _, view := range o.views {
		if view.win != nil {
			view.win.Destroy()
			view.refs.Release()
			view.win.Unref()
			view.win = nil
		}
	}
	o.views = nil
	o.st.SetSnapshot(s.snap)
	s.saveOverviewHistory()
	if o.capture != nil {
		o.captureGen++
		o.capture.Stop()
		o.capture = nil
	}
	if o.win != nil {
		o.win.Destroy()
		o.refs.Release()
		o.win.Unref()
		o.win = nil
	}
	s.syncOverviewDocks()
	o.targets = nil
	o.cardWidgets = nil
	o.windowWidgets = nil
	o.images = nil
	o.renderedTargets = nil
	if restore && previous != "" {
		for _, w := range s.snap.Toplevels {
			if w.Address == previous {
				if s.wayfire != nil {
					s.focusWindow(previous)
				} else {
					s.dispatchOverview(i18n.T("restore focus"), fmt.Sprintf(`hl.dsp.focus({ window = %q })`, "address:"+previous))
				}
				break
			}
		}
	}
}

func (s *shellUI) bindOverviewHandlers() {
	s.onOverviewFilterRender = func(gen uintptr) bool {
		if gen == s.overview.filterGen && s.overview.st.Open {
			s.renderOverview()
		}
		return false
	}
	s.onOverviewRender = func(gen uintptr) bool {
		if gen == s.overview.renderGen && s.overview.st.Open {
			s.renderOverview()
		}
		return false
	}
	s.onOverviewKey = func(_ gtk.EventControllerKey, keyval, _ uint32, mods gdk.ModifierType) bool {
		o := &s.overview
		input := s.overviewInput()
		if (keyval == 'x' || keyval == 'X') && mods&gdk.ControlMaskValue != 0 && mods&gdk.ShiftMaskValue != 0 {
			s.setOverviewKillMode(true)
			return true
		}
		if keyval == keyEscape {
			if o.killMode {
				s.setOverviewKillMode(false)
			} else if o.searchMode {
				o.searchMode = false
				input.query.SetText("")
				input.root.GrabFocus()
			} else {
				s.closeOverview(true)
			}
			return true
		}
		if !o.searchMode {
			if keyval == '/' || (keyval >= 32 && keyval < 0xff00 && keyval != keySpace) {
				o.searchMode = true
				input.query.GrabFocus()
				if keyval != '/' {
					input.query.SetText(string(rune(keyval)))
					input.query.SetPosition(-1)
				}
				return true
			}
		}
		switch keyval {
		case keyUp, keyDown, keyTab, 0xfe20:
			if o.searchMode {
				return false // Keep caret and text navigation inside the entry.
			}
			delta := 1
			if keyval == keyUp {
				delta = -max(1, input.columns)
			} else if keyval == keyDown {
				delta = max(1, input.columns)
			} else if keyval == 0xfe20 || mods&gdk.ShiftMaskValue != 0 {
				delta = -1
			}
			s.navigateOverview(delta)
		case keyLeft, keyRight:
			if o.searchMode {
				return false
			}
			delta := 1
			if keyval == keyLeft {
				delta = -1
			}
			s.navigateOverview(delta)
		case keyReturn, keyKPEnter, keySpace:
			if o.searchMode {
				return false
			} else {
				s.commitOverview()
			}
			return true
		default:
			return !o.searchMode && keyval >= 32 && keyval < 0xff00
		}
		s.renderOverview()
		return true
	}
	s.onOverviewButton = func(b gtk.Button) {
		key := b.GetName()
		if s.overview.dragging != "" {
			return
		}
		if key == "scope" {
			c := s.cfg
			c.Overview.PerMonitor = !c.Overview.PerMonitor
			gtkx.Post(func() { s.changeSettings(c) })
			return
		}
		if key == "mru" {
			c := s.cfg
			c.Overview.MRU = !c.Overview.MRU
			gtkx.Post(func() { s.changeSettings(c) })
			return
		}
		if key == "search-clear" {
			s.overview.query.SetText("")
			s.overview.query.GrabFocus()
			return
		}
		if strings.HasPrefix(key, "window:") {
			addr := strings.TrimPrefix(key, "window:")
			if s.wayfire != nil {
				s.closeOverview(false)
				s.focusWindow(addr)
				return
			}
			if s.overview.killMode {
				s.dispatchOverview(i18n.T("kill window"), fmt.Sprintf(`hl.dsp.window.kill({ window = %q })`, "address:"+addr))
				s.setOverviewKillMode(false)
				s.closeOverview(false)
				return
			}
			s.activateOverview(fmt.Sprintf(`hl.dsp.focus({ window = %q })`, "address:"+addr), s.overviewWindowMonitor(addr))
			return
		}
		if strings.HasPrefix(key, "workspace:") {
			name := strings.TrimPrefix(key, "workspace:")
			s.activateOverview(fmt.Sprintf(`hl.dsp.focus({ workspace = %q })`, name), s.overviewWorkspaceMonitor(name))
			return
		}
		if strings.HasPrefix(key, "new-workspace:") {
			monitor := strings.TrimPrefix(key, "new-workspace:")
			s.createWorkspaceOnMonitor(monitor)
		}
	}
	s.onOverviewQuery = func(entryPtr uintptr, _ uintptr) {
		if s.overview.syncingQuery {
			return
		}
		entry := gtk.Entry{}
		entry.Ptr = entryPtr
		if entry.GetText() != "" {
			s.overview.searchMode = true
		}
		s.overview.st.SetQuery(entry.GetText())
		s.overview.filterGen++
		glib.TimeoutAdd(70, &s.onOverviewFilterRender, s.overview.filterGen)
	}
	s.onOverviewCanvas = func(g gtk.GestureClick, _ int32, x, y float64) {
		widget := g.GetWidget()
		if widget == nil {
			return
		}
		defer widget.Unref()
		picked := widget.Pick(x, y, gtk.PickDefaultValue)
		for picked != nil {
			skip := picked.HasCssClass("overview-window") || picked.HasCssClass("overview-workspace-background")
			parent := picked.GetParent()
			picked.Unref()
			if skip {
				if parent != nil {
					parent.Unref()
				}
				return
			}
			picked = parent
		}
		key := widget.GetName()
		if strings.HasPrefix(key, "new-workspace:") {
			s.createWorkspaceOnMonitor(strings.TrimPrefix(key, "new-workspace:"))
		} else {
			name := strings.TrimPrefix(key, "workspace:")
			s.activateOverview(fmt.Sprintf(`hl.dsp.focus({ workspace = %q })`, name), s.overviewWorkspaceMonitor(name))
		}
	}
	s.onOverviewMotion = func(dt gtk.DropTarget, _, _ float64) gdk.DragAction {
		if w := dt.GetWidget(); w != nil {
			w.AddCssClass("drop-target")
			w.Unref()
		}
		return gdk.ActionMoveValue
	}
	s.onOverviewLeave = func(dt gtk.DropTarget) {
		if w := dt.GetWidget(); w != nil {
			w.RemoveCssClass("drop-target")
			w.Unref()
		}
	}
	s.onOverviewDrop = func(dt gtk.DropTarget, _ uintptr, x, y float64) bool {
		target := dt.GetWidget()
		if target == nil {
			return false
		}
		defer target.Unref()
		value := dt.GetValue()
		if value == nil {
			return false
		}
		address := value.GetString()
		dest, ok := s.overview.targets[target.GoPointer()]
		if !ok || address == "" {
			return false
		}
		return s.moveOverviewWindow(address, dest, x, y)
	}
	s.overview.onQueryFocus = func(gtk.EventControllerFocus) { s.overview.searchMode = true }
	s.loadOverviewHistory()
	s.bindOverviewManagement()
}

func (s *shellUI) scheduleOverviewRender() {
	o := &s.overview
	if !o.st.Open || o.dragging != "" || o.movesPending > 0 {
		return
	}
	o.renderGen++
	glib.TimeoutAdd(50, &s.onOverviewRender, o.renderGen)
}

func (s *shellUI) renderOverview() {
	o := &s.overview
	if !o.st.Open || o.dragging != "" {
		return
	}
	if o.targets == nil {
		o.targets = map[uintptr]overviewDropTarget{}
	}
	o.captureMu.Lock()
	o.previewSize = make(map[string][2]int)
	o.captureMu.Unlock()
	s.renderOverviewView(o)
	for _, view := range o.views {
		view.st = o.st
		view.textures = o.textures
		s.renderOverviewView(view)
	}
	s.updateOverviewCapture()
}

func (s *shellUI) renderOverviewView(o *workspaceOverview) {
	if !o.st.Open || o.dragging != "" {
		return
	}
	if o.win == nil {
		o.layoutKey = ""
		s.buildOverview(o)
	}
	if o.query.GetText() != o.st.Query {
		s.overview.syncingQuery = true
		o.query.SetText(o.st.Query)
		s.overview.syncingQuery = false
	}
	o.mru.SetLabel(map[bool]string{true: i18n.T("MRU: ON"), false: i18n.T("MRU: OFF")}[o.st.MRU])
	o.scope.SetLabel(map[bool]string{true: i18n.T("This monitor"), false: i18n.T("All monitors")}[s.cfg.Overview.PerMonitor])
	matching := 0
	for _, card := range o.st.Cards {
		if s.cfg.Overview.PerMonitor && card.Workspace.Monitor != o.monitor {
			continue
		}
		matching += len(card.Windows)
	}
	if o.st.Query == "" {
		o.searchStatus.SetText(i18n.T("FILTER"))
	} else if matching == 0 {
		o.searchStatus.SetText(i18n.T("NO MATCHES"))
	} else {
		o.searchStatus.SetText(i18n.Nf("%d RESULT", "%d RESULTS", matching, matching))
	}
	o.queryClear.SetVisible(o.st.Query != "")
	key := overviewLayoutKey(o.st.Cards, s.snap.Monitors, s.cfg.Overview.PerMonitor, o.monitor, int(o.root.GetScaleFactor()), o.st.Query != "")
	if o.layoutKey == key {
		s.updateOverviewWidgets(o)
		return
	}
	for _, ptr := range o.renderedTargets {
		delete(s.overview.targets, ptr)
	}
	o.renderedTargets = nil
	o.layoutKey = key
	o.images = make(map[string]*gtk.Picture)
	o.cardWidgets = make(map[int]*overviewCardWidgets)
	o.windowWidgets = make(map[string]*overviewWindowWidgets)
	gtkx.Clear(&o.cards)
	type indexedCard struct {
		index int
		card  shell.OverviewCard
	}
	cards := []indexedCard{}
	for i, card := range o.st.Cards {
		if !s.cfg.Overview.PerMonitor || card.Workspace.Monitor == o.monitor {
			cards = append(cards, indexedCard{i, card})
		}
	}
	if len(cards) == 0 {
		message := i18n.T("No workspaces")
		if o.st.Query != "" {
			message = i18n.T("No matches")
		}
		label := gtk.NewLabel(message)
		label.AddCssClass("overview-empty")
		gtkx.Append(&o.cards, &label.Widget)
		return
	}
	// Match the original overview: monitor groups contain proportional
	// workspace canvases; windows occupy their actual compositor geometry.
	viewportW, viewportH := 1920.0, 1080.0
	for _, mon := range s.snap.Monitors {
		if mon.Name == o.monitor {
			scale := max(mon.Scale, 1)
			viewportW, viewportH = float64(mon.Width)/scale, float64(mon.Height)/scale
		}
	}
	availableW, availableH := viewportW-120, viewportH-180
	groups := []string{}
	counts := map[string]int{}
	aspect := 9.0 / 16.0
	for _, item := range cards {
		card := item.card
		if counts[card.Workspace.Monitor] == 0 {
			groups = append(groups, card.Workspace.Monitor)
		}
		counts[card.Workspace.Monitor]++
	}
	for _, mon := range s.snap.Monitors {
		aspect = max(aspect, float64(mon.Height)/float64(mon.Width))
	}
	columns, thumbW := 1, 0.0
	for c := 1; c <= len(cards); c++ {
		rows := 0
		for _, name := range groups {
			rows += (counts[name] + c - 1) / c
		}
		w := min((availableW-float64(c-1)*18-28)/float64(c), (availableH-float64(len(groups))*52-float64(rows-1)*18)/float64(rows)/aspect)
		if w > thumbW {
			columns, thumbW = c, w
		}
	}
	thumbW = max(180, thumbW)
	o.columns = columns
	var grid *gtk.Grid
	lastMonitor := ""
	slot := 0
	for _, item := range cards {
		i, card := item.index, item.card
		var monW, monH, monX, monY float64 = 1920, 1080, 0, 0
		for _, mon := range s.snap.Monitors {
			if mon.Name == card.Workspace.Monitor {
				scale := max(mon.Scale, 1)
				monW, monH, monX, monY = float64(mon.Width)/scale, float64(mon.Height)/scale, float64(mon.X), float64(mon.Y)
			}
		}
		thumbH := thumbW * monH / monW
		if card.Workspace.Monitor != lastMonitor {
			lastMonitor = card.Workspace.Monitor
			section := gtk.NewBox(gtk.OrientationVerticalValue, 10)
			section.AddCssClass("overview-monitor-group")
			label := gtk.NewLabel(lastMonitor)
			label.SetXalign(0)
			label.AddCssClass("overview-monitor")
			gtkx.Append(section, &label.Widget)
			grid = gtk.NewGrid()
			grid.SetHalign(gtk.AlignCenterValue)
			grid.SetValign(gtk.AlignStartValue)
			grid.SetColumnSpacing(18)
			grid.SetRowSpacing(18)
			gtkx.Append(section, &grid.Widget)
			gtkx.Append(&o.cards, &section.Widget)
			slot = 0
		}
		canvas := gtk.NewOverlay()
		canvas.SetHalign(gtk.AlignStartValue)
		canvas.SetValign(gtk.AlignStartValue)
		canvas.SetHexpand(false)
		canvas.SetVexpand(false)
		canvas.SetSizeRequest(int32(thumbW), int32(thumbH))
		canvas.SetOverflow(gtk.OverflowHiddenValue)
		canvas.SetName(fmt.Sprintf("workspace:%d", card.Workspace.ID))
		if card.New {
			canvas.SetName("new-workspace:" + card.Workspace.Monitor)
		}
		click := gtk.NewGestureClick()
		click.ConnectReleased(&s.onOverviewCanvas)
		canvas.AddController(&click.EventController)
		canvas.AddCssClass("overview-workspace")
		if i == o.st.Selected {
			canvas.AddCssClass("selected")
		}
		if card.Workspace.Active {
			canvas.AddCssClass("active")
		}
		background := gtk.NewButton()
		background.AddCssClass("overview-workspace-background")
		background.SetSizeRequest(int32(thumbW), int32(thumbH))
		background.SetName("workspace:" + card.Workspace.Name)
		background.ConnectClicked(&s.onOverviewButton)

		if card.New {
			background.SetName("new-workspace:" + card.Workspace.Monitor)
		}
		canvas.SetChild(&background.Widget)
		background.Unref()
		{
			s.overview.targets[canvas.GoPointer()] = overviewDropTarget{ID: card.Workspace.ID, Monitor: card.Workspace.Monitor, New: card.New, Scale: thumbW / monW}
			o.renderedTargets = append(o.renderedTargets, canvas.GoPointer())
			drop := gtk.NewDropTarget(types.GType(gobject.TypeStringVal), gdk.ActionMoveValue)
			drop.ConnectMotion(&s.onOverviewMotion)
			drop.ConnectLeave(&s.onOverviewLeave)
			drop.ConnectDrop(&s.onOverviewDrop)
			canvas.AddController(&drop.EventController)
		}
		board := gtk.NewFixed()
		board.SetOverflow(gtk.OverflowHiddenValue)
		canvas.AddOverlay(&board.Widget)
		board.Unref()
		if card.New {
			badge := gtk.NewLabel(i18n.T("＋  New workspace"))
			badge.AddCssClass("overview-new-badge")
			badge.SetHalign(gtk.AlignEndValue)
			badge.SetValign(gtk.AlignStartValue)
			badge.SetMarginTop(10)
			badge.SetMarginEnd(10)
			badge.SetCanTarget(false)
			canvas.AddOverlay(&badge.Widget)
			badge.Unref()
		}
		o.cardWidgets[i] = &overviewCardWidgets{canvas: canvas, board: board, order: overviewWindowOrder(card.Windows), monX: monX, monY: monY, monW: monW, monH: monH, thumbW: thumbW, thumbH: thumbH}
		// Paint the focused window last, as the topmost window.
		for wi := len(card.Windows) - 1; wi >= 0; wi-- {
			w := card.Windows[wi]
			x, y, pw, ph := overviewWindowLayout(w, monX, monY, monW, monH, thumbW, thumbH)
			s.noteOverviewPreviewSize(w.Address, pw, ph, int(o.root.GetScaleFactor()))
			b := gtk.NewButton()
			b.SetName("window:" + w.Address)
			b.SetSizeRequest(int32(pw), int32(ph))
			b.SetTooltipText(w.Title) // the label is cut short in small previews
			ww := &overviewWindowWidgets{button: b, rect: [4]float64{x, y, pw, ph}, title: w.Title}
			o.windowWidgets[w.Address] = ww
			b.AddCssClass("overview-window")
			if w.Activated {
				b.AddCssClass("active")
			}
			b.ConnectClicked(&s.onOverviewButton)
			preview := gtk.NewPicture()
			preview.SetCanShrink(true)
			preview.SetContentFit(gtk.ContentFitFillValue)
			preview.AddCssClass("overview-preview")
			if texture := o.textures[w.Address]; texture != nil {
				preview.SetPaintable(texture)
			} else if path := o.thumbPaths[w.Address]; path != "" {
				setPictureFile(preview, path)
			}
			windowLayer := gtk.NewOverlay()
			windowLayer.SetOverflow(gtk.OverflowHiddenValue)
			windowLayer.SetChild(&preview.Widget)
			preview.Unref()
			meta := gtk.NewBox(gtk.OrientationHorizontalValue, 7)
			meta.AddCssClass("overview-window-meta")
			meta.SetHalign(gtk.AlignFillValue)
			meta.SetValign(gtk.AlignEndValue)
			meta.SetSizeRequest(-1, 38)
			icon := gtk.NewImage()
			if entry := s.dockState.apps.ForClass(w.Class); entry != nil && entry.Icon != "" {
				setIcon(icon, entry.Icon)
			} else {
				setIcon(icon, "application-x-executable")
			}
			icon.SetPixelSize(18)
			icon.SetValign(gtk.AlignCenterValue)
			icon.SetCanTarget(false)
			gtkx.Append(meta, &icon.Widget)
			title := gtk.NewLabel(w.Title)
			title.AddCssClass("overview-window-title")
			title.SetWrap(true)
			title.SetWrapMode(pango.WrapWordCharValue)
			title.SetLines(2)
			title.SetEllipsize(3) // end
			title.SetJustify(gtk.JustifyLeftValue)
			title.SetHalign(gtk.AlignFillValue)
			title.SetValign(gtk.AlignCenterValue)
			title.SetHexpand(true)
			gtkx.Append(meta, &title.Widget)
			ww.label = title
			windowLayer.AddOverlay(&meta.Widget)
			meta.Unref()
			b.SetChild(&windowLayer.Widget)
			windowLayer.Unref()
			o.images[w.Address] = preview
			var value gobject.Value
			value.Init(gobject.TypeStringVal)
			value.SetString(w.Address)
			content := gdk.NewContentProviderForValue(&value)
			value.Unset()
			drag := gtk.NewDragSource()
			drag.SetActions(gdk.ActionMoveValue)
			drag.SetContent(content)
			content.Unref()
			drag.ConnectPrepare(&s.overview.onDragPrepare)
			drag.ConnectDragBegin(&s.overview.onDragBegin)
			drag.ConnectDragEnd(&s.overview.onDragEnd)
			b.AddController(&drag.EventController)
			gesture := gtk.NewGestureClick()
			gesture.SetButton(0)
			gesture.SetPropagationPhase(gtk.PhaseCaptureValue)
			gesture.ConnectPressed(&s.overview.onWindowGesture)
			b.AddController(&gesture.EventController)
			hover := gtk.NewEventControllerMotion()
			hover.ConnectEnter(&s.overview.onOverviewHover)
			b.AddController(&hover.EventController)
			board.Put(&b.Widget, x, y)
			b.Unref()
		}
		grid.Attach(&canvas.Widget, int32(slot%columns), int32(slot/columns), 1, 1)
		canvas.Unref()
		slot++
	}
}

// fillOverviewThumbnails captures each card's window once through the
// Wayfire thumbnail path and paints it when it arrives.
func (s *shellUI) fillOverviewThumbnails(o *workspaceOverview, snap hypr.Snapshot, gen uint64) {
	if o.thumbStarted == nil {
		o.thumbStarted = map[string]bool{}
	}
	if o.thumbPaths == nil {
		o.thumbPaths = map[string]string{}
	}
	for address := range o.images {
		if o.thumbStarted[address] {
			continue
		}
		o.thumbStarted[address] = true
		go func(address string) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			path, err := s.windowThumbnail(ctx, snap, address)
			if err != nil {
				log.Printf("overview thumbnail: %v", err)
				return
			}
			gtkx.Post(func() {
				if s.overview.activation != gen || !s.overview.st.Open {
					return
				}
				o.thumbPaths[address] = path
				if picture := o.images[address]; picture != nil {
					setPictureFile(picture, path)
				}
			})
		}(address)
	}
}

func (s *shellUI) updateOverviewCapture() {
	o := &s.overview
	if !o.st.Open {
		return
	}
	if s.client == nil {
		return
	}
	if o.captureFailed {
		return
	}
	var targets, priority []string
	for i, card := range o.st.Cards {
		for _, w := range card.Windows {
			targets = append(targets, w.Address)
			if i == o.st.Selected {
				priority = append(priority, w.Address)
			}
		}
	}
	if len(targets) == 0 {
		if o.capture != nil {
			o.captureGen++
			o.capture.Stop()
			o.capture = nil
		}
		return
	}
	if o.capture != nil {
		o.capture.SetTargetsWithPriority(targets, priority)
		return
	}
	o.captureGen++
	gen := o.captureGen
	o.capture = hyprland.Start(context.Background(), targets, func(f hyprland.Frame) {
		w, h := s.overviewPreviewSize(f.Address)
		s.queueOverviewFrame(gen, hyprland.Shrink(f, w, h))
	}, func(err error) {
		log.Printf("overview capture: %v", err)
		gtkx.Post(func() {
			if o.captureGen != gen || !o.st.Open {
				return
			}
			o.capture = nil
			o.captureFailed = true
			time.AfterFunc(time.Second, func() {
				gtkx.Post(func() {
					if o.captureGen != gen || !o.st.Open {
						return
					}
					o.captureFailed = false
					s.updateOverviewCapture()
				})
			})
		})
	})
	o.capture.SetTargetsWithPriority(targets, priority)
}

func (s *shellUI) noteOverviewPreviewSize(address string, width, height float64, scale int) {
	if address == "" {
		return
	}
	if scale < 1 {
		scale = 1
	}
	o := &s.overview
	o.captureMu.Lock()
	defer o.captureMu.Unlock()
	if o.previewSize == nil {
		o.previewSize = make(map[string][2]int)
	}
	old := o.previewSize[address]
	w := int(width*float64(scale) + 0.5)
	h := int(height*float64(scale) + 0.5)
	o.previewSize[address] = [2]int{max(old[0], w), max(old[1], h)}
}

func (s *shellUI) overviewPreviewSize(address string) (int, int) {
	o := &s.overview
	o.captureMu.Lock()
	size := o.previewSize[address]
	o.captureMu.Unlock()
	if size[0] <= 0 || size[1] <= 0 {
		return 720, 720
	}
	return size[0], size[1]
}

// queueOverviewFrame keeps at most the newest frame per client while GTK is
// busy rebuilding the overview. Full capture buffers must never accumulate
// in the generic main-loop queue.
func (s *shellUI) queueOverviewFrame(gen uint64, frame hyprland.Frame) {
	if frame.Width <= 0 || frame.Height <= 0 || len(frame.Pixels) == 0 {
		return
	}
	o := &s.overview
	o.captureMu.Lock()
	if o.pendingFrames == nil {
		o.pendingFrames = make(map[string]queuedOverviewFrame)
	}
	o.pendingFrames[frame.Address] = queuedOverviewFrame{gen: gen, frame: frame}
	post := !o.frameDrainQueued
	if post {
		o.frameDrainQueued = true
	}
	o.captureMu.Unlock()
	if post {
		gtkx.Post(s.drainOverviewFrames)
	}
}

func (s *shellUI) drainOverviewFrames() {
	o := &s.overview
	o.captureMu.Lock()
	if o.dragging != "" {
		o.frameDrainQueued = false
		o.captureMu.Unlock()
		return
	}
	pending := o.pendingFrames
	o.pendingFrames = nil
	o.frameDrainQueued = false
	o.captureMu.Unlock()
	for _, update := range pending {
		f := update.frame
		if !o.st.Open || update.gen != o.captureGen || o.dragging == f.Address {
			continue
		}
		format := gdk.MemoryFormat(-1)
		switch f.Format {
		case 0, 0x34325241:
			format = gdk.MemoryB8g8r8a8PremultipliedValue
		case 1, 0x34325258:
			format = gdk.MemoryB8g8r8x8Value
		case 0x34324241:
			format = gdk.MemoryR8g8b8a8PremultipliedValue
		case 0x34324258:
			format = gdk.MemoryR8g8b8x8Value
		default:
			continue
		}
		bytes := glib.NewBytes(f.Pixels, uint(len(f.Pixels)))
		texture := gdk.NewMemoryTexture(int32(f.Width), int32(f.Height), format, bytes, uint(f.Stride))
		bytes.Unref()
		if texture == nil {
			continue
		}
		if p := o.images[f.Address]; p != nil {
			p.SetPaintable(texture)
		}
		for _, view := range o.views {
			if p := view.images[f.Address]; p != nil {
				p.SetPaintable(texture)
			}
		}
		if old := o.textures[f.Address]; old != nil {
			old.Unref()
		}
		o.textures[f.Address] = texture
	}
}

func (s *shellUI) createWorkspaceOnMonitor(monitor string) {
	if s.client == nil || monitor == "" {
		return
	}
	id := strconv.Itoa(s.nextOverviewWorkspaceID(monitor))
	s.activateOverview(fmt.Sprintf(`hl.dsp.focus({workspace=%s})`, id), monitor, fmt.Sprintf(`hl.dsp.workspace.move({workspace=%q,monitor=%q})`, id, monitor))
}

func (s *shellUI) dispatchOverview(action, expression string) {
	if s.client == nil {
		log.Printf("overview: Hyprland is not connected; cannot %s", action)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.client.LuaDispatch(ctx, expression); err != nil {
			log.Printf("overview: %s: %v", action, err)
		}
	}()
}

func (s *shellUI) buildOverview(o *workspaceOverview) {
	o.win = gtk.NewApplicationWindow(s.app)
	o.win.AddCssClass("overview")
	o.root = *gtk.NewBox(gtk.OrientationVerticalValue, 10)
	o.root.AddCssClass("overview-scrim")
	o.root.SetFocusable(true)
	toolbar := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	toolbar.SetHalign(gtk.AlignCenterValue)
	toolbar.AddCssClass("overview-toolbar")
	o.mru = *gtk.NewButton()
	o.mru.SetName("mru")
	o.mru.AddCssClass("overview-mru")
	o.mru.ConnectClicked(&s.onOverviewButton)
	gtkx.Append(toolbar, &o.mru.Widget)
	o.scope = *gtk.NewButton()
	o.scope.SetName("scope")
	o.scope.AddCssClass("overview-mru")
	o.scope.ConnectClicked(&s.onOverviewButton)
	gtkx.Append(toolbar, &o.scope.Widget)
	o.query = *gtk.NewEntry()
	o.query.SetPlaceholderText(i18n.T("Filter by window title or app"))
	o.query.SetSizeRequest(400, -1)
	o.query.AddCssClass("overview-query")
	search := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	search.AddCssClass("overview-search")
	searchIcon := gtk.NewImageFromIconName("system-search-symbolic")
	searchIcon.AddCssClass("overview-search-icon")
	gtkx.Append(search, &searchIcon.Widget)
	gtkx.Append(search, &o.query.Widget)
	o.queryClear = *gtk.NewButton()
	o.queryClear.SetName("search-clear")
	o.queryClear.SetLabel("×")
	o.queryClear.AddCssClass("overview-search-clear")
	o.queryClear.ConnectClicked(&s.onOverviewButton)
	gtkx.Append(search, &o.queryClear.Widget)
	gtkx.Append(toolbar, &search.Widget)
	o.searchStatus = *gtk.NewLabel(i18n.T("TYPE TO FILTER WINDOWS"))
	o.searchStatus.AddCssClass("overview-search-status")
	gtkx.Append(toolbar, &o.searchStatus.Widget)
	gtkx.Append(&o.root, &toolbar.Widget)
	o.cards = *gtk.NewBox(gtk.OrientationVerticalValue, 24)
	o.cards.SetHalign(gtk.AlignCenterValue)
	o.cards.SetValign(gtk.AlignCenterValue)
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	scroll.SetVexpand(true)
	scroll.SetChild(&o.cards.Widget)
	o.cards.Unref()
	gtkx.Append(&o.root, &scroll.Widget)
	hint := gtk.NewLabel(i18n.T("Type or / to filter windows   Esc · clear / close   Arrows / Tab · select   Middle click · close   Ctrl+Shift+X · kill   Drag · move"))
	o.hint = hint
	hint.AddCssClass("overview-hint")
	gtkx.Append(&o.root, &hint.Widget)
	stage := gtk.NewOverlay()
	stage.SetChild(&o.root.Widget)
	o.root.Unref()
	o.killLayer = gtk.NewFixed()
	o.killLayer.SetCanTarget(false)
	o.killGlyph = gtk.NewLabel("󰅖")
	o.killGlyph.AddCssClass("overview-kill-glyph")
	o.killGlyph.SetVisible(false)
	o.killLayer.Put(&o.killGlyph.Widget, 0, 0)
	o.killGlyph.Unref()
	stage.AddOverlay(&o.killLayer.Widget)
	o.killLayer.Unref()
	o.win.SetChild(&stage.Widget)
	stage.Unref()
	pointer := gtk.NewEventControllerMotion()
	pointer.ConnectEnter(&s.overview.onPointer)
	pointer.ConnectMotion(&s.overview.onPointer)
	o.win.AddController(&pointer.EventController)
	press := gtk.NewGestureClick()
	press.SetButton(0)
	press.SetPropagationPhase(gtk.PhaseCaptureValue)
	press.ConnectPressed(&s.overview.onSurfacePress)
	o.win.AddController(&press.EventController)
	w := o.win.GoPointer()
	if s.layer {
		layershell.InitForWindow(w)
		layershell.SetNamespace(w, "inua-overview")
		layershell.SetLayer(w, layershell.LayerTop)
		layershell.SetKeyboardMode(w, overviewKeyboardMode())
		for _, e := range []layershell.Edge{layershell.EdgeTop, layershell.EdgeBottom, layershell.EdgeLeft, layershell.EdgeRight} {
			layershell.SetAnchor(w, e, true)
		}
		layershell.SetExclusiveZone(w, -1)
		for _, bar := range s.bars {
			if bar.connector() == o.monitor {
				layershell.SetMonitor(w, bar.mon.GoPointer())
				break
			}
		}
	} else {
		o.win.SetTitle(i18n.T("inua overview"))
		o.win.SetDefaultSize(900, 700)
	}
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCaptureValue)
	keys.ConnectKeyPressed(&s.onOverviewKey)
	o.win.AddController(&keys.EventController)
	gtkx.ConnectChanged(&o.query, &s.onOverviewQuery)
	queryFocus := gtk.NewEventControllerFocus()
	queryFocus.ConnectEnter(&s.overview.onQueryFocus)
	o.query.AddController(&queryFocus.EventController)
	o.win.Present()
	o.root.GrabFocus()
	scrollKeys := gtk.NewEventControllerScroll(gtk.EventControllerScrollVerticalValue | gtk.EventControllerScrollDiscreteValue)
	scrollKeys.SetPropagationPhase(gtk.PhaseCaptureValue)
	scrollKeys.ConnectScroll(&s.overview.onOverviewScroll)
	o.win.AddController(&scrollKeys.EventController)
	right := gtk.NewGestureClick()
	right.SetButton(3)
	right.SetPropagationPhase(gtk.PhaseCaptureValue)
	right.ConnectPressed(&s.overview.onOverviewRight)
	o.win.AddController(&right.EventController)
}

func overviewPair(value any, fallbackX, fallbackY float64) (float64, float64) {
	if pair, ok := value.([]any); ok && len(pair) >= 2 {
		x, xok := pair[0].(float64)
		y, yok := pair[1].(float64)
		if xok && yok {
			return x, y
		}
	}
	return fallbackX, fallbackY
}

// overviewWindowLayout maps compositor geometry onto a workspace card. A
// window that still reports coordinates on another monitor (typical right
// after a New drop) fills the card so the existing screenshot stays visible.
func overviewWindowLayout(w hypr.Toplevel, monX, monY, monW, monH, thumbW, thumbH float64) (x, y, pw, ph float64) {
	if w.Raw == nil {
		return 0, 0, thumbW, thumbH
	}
	ax, ay := overviewPair(w.Raw["at"], monX, monY)
	ww, wh := overviewPair(w.Raw["size"], monW, monH)
	if ax+ww <= monX || ay+wh <= monY || ax >= monX+monW || ay >= monY+monH {
		return 0, 0, thumbW, thumbH
	}
	scale := thumbW / max(monW, 1)
	x, y = max(0, (ax-monX)*scale), max(0, (ay-monY)*scale)
	pw, ph = max(24, min(ww*scale, thumbW-x)), max(24, min(wh*scale, thumbH-y))
	return x, y, pw, ph
}
