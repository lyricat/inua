package ui

import (
	"cmp"
	"log"
	"slices"
	"strings"
	"time"
	"unsafe"

	"github.com/lyricat/inua/internal/compositor"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gobject/types"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/calendar"
	"github.com/lyricat/inua/internal/apps/clock"
	"github.com/lyricat/inua/internal/apps/store"
	"github.com/lyricat/inua/internal/apps/weather"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/layershell"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/uikit"
)

// widgetPage is the place before the first workspace (widgets.blp): a page
// of cards over the windows of one monitor, between its bar and its dock.
// Its window is built on every open and destroyed once it has slid out.
// The cards sit on a grid of shell.WidgetCell (the board), each where
// widgets.widget puts it; dragged by its head, a card follows the pointer
// over the grid drawn for the drag, and lands on the cell under its
// corner when no other card is there. The cards are built anew when the
// widgets or the apps change, and only moved when their places do; the
// parts that change by themselves (the time, the player, the forecast)
// are kept to be filled in place.
type widgetPage struct {
	open, closing bool
	monitor       string // the connector it shows on
	workspace     string // the workspace under it, which it goes back to

	win    gtk.ApplicationWindow
	reveal gtk.Revealer
	page   gtk.Box // the parent of the menus: it outlives the cards
	board  gtk.Fixed
	add    gtk.MenuButton
	refs   gtkx.Scope

	cards map[string]*gtk.Box // by kind, owned by the board
	heads map[string]*gtk.Box // the cards' heads, to drag them by
	rows  map[string]int      // how many rows each card is tall (sizeWidget)
	// columns is how many columns the monitor it opened on shows.
	columns int
	lines   *gtk.Widget // the grid (snapshotGrid), shown while a card is dragged
	spot    *gtk.Box    // where the dragged card lands

	// The card dragged: its kind, where it was, and where it would land.
	dragKind string
	dragFrom [2]float64
	dragTo   shell.GridRect
	dragFits bool

	// What a menu is about: a widget's kind, or an app's ID.
	menuKind, menuApp string
	picker            *widgetPicker

	time, date                    *gtk.Label
	zones                         *gtk.Box
	month                         *gtk.Label
	grid                          *gtk.Grid
	alarm                         *gtk.Label
	wxIcon                        *gtk.Image
	wxTemp, wxText, wxPlace, wxHL *gtk.Label
	wxDays                        *gtk.Box
	art                           *gtk.Image
	artURL                        string
	title, artist                 *gtk.Label
	prev, play, next              *gtk.Button
}

// widgetPicker is the apps widget's + popover: a search over the apps not
// on it yet.
type widgetPicker struct {
	pop   *gtk.Popover
	query *gtk.SearchEntry
	list  *gtk.Box
}

// widgetsPlace is the page's place among the workspaces the bar's wheel
// goes through.
const widgetsPlace = "widgets"

func (s *shellUI) bindWidgetsHandlers() {
	p := &s.widgets
	s.onWidgetsMark = func(btn gtk.Button) {
		if b := s.barUnder(&btn.Widget); b != nil && !s.widgetsShownOn(b) {
			s.openWidgets(b)
			return
		}
		s.closeWidgets()
	}
	s.onWidgetsKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		if keyval == keyEscape {
			s.closeWidgets()
			return true
		}
		return false
	}
	s.onWidgetsAddActive = func(uintptr, uintptr, uintptr) {
		s.widgetsKeyboard(!p.add.GetActive())
	}
	s.onWidgetsRevealed = func(uintptr, uintptr, uintptr) {
		if p.closing && !p.reveal.GetChildRevealed() {
			s.dropWidgets()
		}
	}
	s.onWidgetButton = func(b gtk.Button) {
		name := b.GetName()
		switch {
		case strings.HasPrefix(name, "app:"):
			if e := s.dockState.apps.ByID(strings.TrimPrefix(name, "app:")); e != nil {
				s.closeWidgets()
				s.launchApp(e)
			}
		case name == "add-app":
			s.pickWidgetApp(&b.Widget)
		case strings.HasPrefix(name, "pick:"):
			id := strings.TrimPrefix(name, "pick:")
			if p.picker != nil {
				p.picker.pop.Popdown()
			}
			gtkx.Post(func() { s.moveWidgetApp(id, "") })
		case name == "open-weather", name == "open-clock", name == "open-calendar":
			id := map[string]string{"open-weather": weatherApp, "open-clock": clockApp, "open-calendar": calendarApp}[name]
			if e := s.dockState.apps.ByID(id); e != nil {
				s.closeWidgets()
				s.launchApp(e)
			}
		case name == "media-prev", name == "media-play", name == "media-next":
			s.mediaRequest(map[string]string{"media-prev": "previous", "media-play": "play-pause", "media-next": "next"}[name])
		}
	}
	s.onWidgetMenu = func(g gtk.GestureClick, _ int32, x, y float64) {
		w := g.GetWidget()
		if w == nil {
			return
		}
		name := w.GetName()
		p.menuKind, p.menuApp = "", ""
		label := ""
		switch {
		case strings.HasPrefix(name, "head:"):
			p.menuKind, label = strings.TrimPrefix(name, "head:"), i18n.T("Remove Widget")
		case strings.HasPrefix(name, "app:"):
			p.menuApp, label = strings.TrimPrefix(name, "app:"), i18n.T("Remove from the Page")
		default:
			return
		}
		pop := uikit.NewMenu([]uikit.Item{{Label: label, Run: func() { s.widgetAction("remove") }}}, uikit.AtPointer)
		s.widgetPopover(*pop, w, x, y)
	}

	s.onWidgetMenuClosed = func(pop gtk.Popover) {
		s.widgetsKeyboard(true)
		gtkx.Post(func() { pop.Unparent() })
		if p.picker != nil && p.picker.pop.GoPointer() == pop.GoPointer() {
			p.picker = nil
		}
	}
	s.onWidgetDrop = func(dt gtk.DropTarget, _ uintptr, x, _ float64) bool {
		w, v := dt.GetWidget(), dt.GetValue()
		if w == nil || v == nil {
			return false
		}
		// An app, from the dock or the widget's own tiles: onto the
		// apps widget, or before (or, on its right half, after) a tile.
		name, dragged := w.GetName(), v.GetString()
		if s.dockState.apps.ByID(dragged) == nil {
			return false
		}
		before := ""
		if id, ok := strings.CutPrefix(name, "app:"); ok {
			before = id
			if x > float64(w.GetWidth())/2 {
				before = following(s.cfg.Widgets.Apps, id)
			}
		}
		gtkx.Post(func() { s.moveWidgetApp(dragged, before) })
		return true
	}
	s.onWidgetQuery = func(e gtk.SearchEntry) { s.fillWidgetPicker() }
	s.onWidgetQueryActivate = func(e gtk.SearchEntry) {
		if p.picker == nil {
			return
		}
		if first := p.picker.list.GetFirstChild(); first != nil {
			b := gtk.ButtonNewFromInternalPtr(first.GoPointer())
			s.onWidgetButton(*b)
		}
	}

	// A card dragged by its head follows the pointer over the grid; the
	// spot shows the cell it lands on, or that another card is there. The
	// drag is the board's, which stays still: one of the head's own would
	// measure the pointer from a head that moves with every step, and the
	// card would shake.
	s.onWidgetDragBegin = func(g gtk.GestureDrag, x, y float64) {
		kind := s.widgetHeadAt(x, y)
		card := p.cards[kind]
		if card == nil {
			g.SetState(gtk.EventSequenceDeniedValue) // not on a head: the card's own
			return
		}
		p.dragKind = kind
		p.board.GetChildPosition(&card.Widget, &p.dragFrom[0], &p.dragFrom[1])
		card.InsertBefore(&p.board.Widget, &gtk.Widget{}) // over the others
		card.AddCssClass("lifted")
		p.lines.SetSizeRequest(p.board.GetWidth(), p.board.GetHeight())
		p.lines.AddCssClass("shown") // fades in (style.css)
		r := s.widgetRect(kind, 0, 0)
		p.spot.SetSizeRequest(int32(r.Columns*shell.WidgetCell), int32(r.Rows*shell.WidgetCell))
		p.spot.SetVisible(true)
		s.dragWidget(0, 0)
	}
	s.onWidgetDragUpdate = func(_ gtk.GestureDrag, dx, dy float64) { s.dragWidget(dx, dy) }
	s.onWidgetDragEnd = func(_ gtk.GestureDrag, dx, dy float64) {
		card := p.cards[p.dragKind]
		if card == nil {
			return
		}
		s.dragWidget(dx, dy)
		card.RemoveCssClass("lifted")
		p.lines.RemoveCssClass("shown")
		p.spot.SetVisible(false)
		kind, to, fits := p.dragKind, p.dragTo, p.dragFits
		p.dragKind = ""
		if !fits {
			p.board.Move(&card.Widget, p.dragFrom[0], p.dragFrom[1]) // back where it was
			return
		}
		p.board.Move(&card.Widget, float64(to.Column*shell.WidgetCell), float64(to.Row*shell.WidgetCell))
		c := s.cfg
		c.Widgets.Places = slices.Clone(c.Widgets.Places)
		for i := range c.Widgets.Places {
			if c.Widgets.Places[i].Kind == kind {
				c.Widgets.Places[i].Column, c.Widgets.Places[i].Row = to.Column, to.Row
			}
		}
		gtkx.Post(func() { s.changeSettings(c) })
	}
}

// widgetHeadAt is the kind of the card whose head is at x, y of the
// board, "" for none.
func (s *shellUI) widgetHeadAt(x, y float64) string {
	p := &s.widgets
	hit := p.board.Pick(x, y, gtk.PickDefaultValue)
	if hit == nil {
		return ""
	}
	defer hit.Unref()
	for kind, head := range p.heads {
		if hit.GoPointer() == head.GoPointer() || hit.IsAncestor(&head.Widget) {
			return kind
		}
	}
	return ""
}

// dragWidget moves the dragged card dx, dy from where it was, within the
// board, and the spot to the cell under its corner.
func (s *shellUI) dragWidget(dx, dy float64) {
	p := &s.widgets
	card := p.cards[p.dragKind]
	if card == nil {
		return
	}
	to := s.widgetRect(p.dragKind, 0, 0)
	x := max(0, min(p.dragFrom[0]+dx, float64((s.boardColumns()-to.Columns)*shell.WidgetCell)))
	y := max(0, p.dragFrom[1]+dy)
	p.board.Move(&card.Widget, x, y)
	to.Column, to.Row = shell.Cell(x), shell.Cell(y)
	to.Column = min(to.Column, max(0, s.boardColumns()-to.Columns)) // the cell nearest stays on the page
	p.dragTo, p.dragFits = to, to.Apart(s.widgetRects(p.dragKind))
	p.board.Move(&p.spot.Widget, float64(to.Column*shell.WidgetCell), float64(to.Row*shell.WidgetCell))
	toggle(&p.spot.Widget, "blocked", !p.dragFits)
}

// widgetRect is the cells a card of kind takes at column, row: WidgetSpan
// columns, and the rows sizeWidget found (WidgetSpan before it has).
func (s *shellUI) widgetRect(kind string, column, row int) shell.GridRect {
	rows := s.widgets.rows[kind]
	if rows == 0 {
		rows = config.WidgetSpan
	}
	return shell.GridRect{Column: column, Row: row, Columns: config.WidgetSpan, Rows: rows}
}

// widgetRects are the cells the cards take, but the one of kind but.
func (s *shellUI) widgetRects(but string) []shell.GridRect {
	var out []shell.GridRect
	for _, w := range s.cfg.Widgets.Places {
		if w.Kind != but {
			out = append(out, s.widgetRect(w.Kind, w.Column, w.Row))
		}
	}
	return out
}

// boardColumns is how many columns the page shows: the board's, else
// those of the monitor it opened on, else three widgets' worth.
func (s *shellUI) boardColumns() int {
	p := &s.widgets
	switch {
	case p.open && p.board.GetWidth() > 0:
		return int(p.board.GetWidth()) / shell.WidgetCell
	case p.columns > 0:
		return p.columns
	}
	return 3*config.WidgetSpan + 2
}

// sizeWidget makes a card a whole number of cells, so that its edges are
// on the grid: WidgetSpan columns wide, and as many rows tall as its
// content needs.
func (s *shellUI) sizeWidget(kind string) {
	p := &s.widgets
	card := p.cards[kind]
	if card == nil {
		return
	}
	w := int32(config.WidgetSpan * shell.WidgetCell)
	card.SetSizeRequest(w, -1) // measured without the rows it had
	var min, nat, minBase, natBase int32
	card.Measure(gtk.OrientationVerticalValue, w, &min, &nat, &minBase, &natBase)
	rows := shell.Span(int(nat))
	card.SetSizeRequest(w, int32(rows*shell.WidgetCell))
	p.rows[kind] = rows
}

// settleWidgets moves a card that touches one placed before it to the
// first free place, as one moved by hand could not be (a file written by
// hand, or by the grid's first version); it saves where they went.
func (s *shellUI) settleWidgets() {
	p := &s.widgets
	c := s.cfg
	c.Widgets.Places = slices.Clone(c.Widgets.Places)
	var placed []shell.GridRect
	moved := false
	for i, w := range c.Widgets.Places {
		r := s.widgetRect(w.Kind, w.Column, w.Row)
		if !r.Apart(placed) {
			r = shell.FreeSpot(placed, r.Columns, r.Rows, s.boardColumns())
			c.Widgets.Places[i].Column, c.Widgets.Places[i].Row = r.Column, r.Row
			if card := p.cards[w.Kind]; card != nil {
				p.board.Move(&card.Widget, float64(r.Column*shell.WidgetCell), float64(r.Row*shell.WidgetCell))
			}
			moved = true
		}
		placed = append(placed, r)
	}
	if moved {
		gtkx.Post(func() { s.changeSettings(c) })
	}
}

// following is what comes after item in items, "" at the end.
func following(items []string, item string) string {
	if i := slices.Index(items, item); i >= 0 && i+1 < len(items) {
		return items[i+1]
	}
	return ""
}

// moveWidgetApp puts an app on the apps widget before another, or at the
// end; one already there moves.
func (s *shellUI) moveWidgetApp(id, before string) {
	c := s.cfg
	c.Widgets.Apps = shell.MovePinned(c.Widgets.Apps, id, before)
	s.changeSettings(c)
}

// widgetsShownOn reports whether the page is up on b's monitor.
func (s *shellUI) widgetsShownOn(b *bar) bool {
	p := &s.widgets
	return p.open && !p.closing && b != nil && p.monitor == b.connector()
}

func (s *shellUI) toggleWidgets() {
	if s.widgets.open && !s.widgets.closing {
		s.closeWidgets()
	} else {
		s.openWidgets(nil)
	}
}

// openWidgets shows the page on b's monitor, the focused one for nil.
func (s *shellUI) openWidgets(b *bar) {
	p := &s.widgets
	if !s.cfg.Widgets.Enable {
		return
	}
	if b == nil {
		for _, x := range s.bars {
			if x.connector() == s.snap.FocusedOutput {
				b = x
			}
		}
	}
	if b == nil || s.widgetsShownOn(b) {
		return
	}
	if p.open {
		if p.closing {
			// Sliding out on this monitor: back in. On another one, it
			// goes, and comes here once gone.
			if p.monitor == b.connector() {
				p.closing = false
				p.reveal.SetRevealChild(true)
				s.markWidgets()
			}
			return
		}
		s.closeWidgets()
		return
	}
	s.closeOverview(false)
	s.cancelLauncher()
	s.closeControl()
	s.closeAgenda()
	s.closeCenter()
	s.closeClipboard()
	s.closePasswords()
	s.closeDrawer()
	p.monitor = b.connector()
	p.workspace = s.snap.ActiveWorkspace(p.monitor)
	s.buildWidgets(b)
	p.open = true
	s.fillWidgets()
	s.markWidgets()
	p.reveal.SetRevealChild(true)
	s.refocusPointer()
}

// closeWidgets slides the page out; the window goes once it is out.
func (s *shellUI) closeWidgets() {
	p := &s.widgets
	if !p.open || p.closing {
		return
	}
	p.closing = true
	if p.picker != nil {
		p.picker.pop.Popdown()
	}
	p.reveal.SetRevealChild(false)
	s.markWidgets()
}

// dropWidgets takes the page's window away at once: slid out, or its
// monitor gone.
func (s *shellUI) dropWidgets() {
	p := &s.widgets
	if !p.open {
		return
	}
	if p.lines != nil {
		gtkx.ForgetCanvas(p.lines)
	}
	p.win.Destroy()
	p.refs.Release()
	p.open, p.closing, p.picker = false, false, nil
	p.time, p.zones, p.grid, p.wxDays, p.art = nil, nil, nil, nil, nil
	p.cards, p.heads, p.rows, p.lines, p.spot, p.dragKind = nil, nil, nil, nil, nil, ""
	s.markWidgets()
}

// markWidgets fills the bars' marks while the page shows on their monitor.
func (s *shellUI) markWidgets() {
	for _, b := range s.bars {
		on := s.widgetsShownOn(b)
		toggle(&b.widgetsMark.Widget, "open", on)
		toggle(&b.workspaces.Widget, "under-page", on)
	}
}

// followWidgets closes the page when its monitor shows another workspace,
// or the page is turned off.
func (s *shellUI) followWidgets() {
	p := &s.widgets
	if !p.open || p.closing {
		return
	}
	if !s.cfg.Widgets.Enable {
		s.closeWidgets()
		return
	}
	for _, mon := range s.snap.Outputs {
		if mon.Name == p.monitor && mon.ActiveWorkspace != p.workspace {
			s.closeWidgets()
		}
	}
}

func (s *shellUI) buildWidgets(b *bar) {
	p := &s.widgets
	ui := s.load("widgets.ui", &p.refs)
	ui.Get("widgets", &p.win)
	ui.Get("reveal", &p.reveal)
	ui.Get("page", &p.page)
	ui.Get("board", &p.board)
	ui.Get("add", &p.add)
	ui.Close()
	gtkx.ConnectNotify(&p.reveal, "child-revealed", &s.onWidgetsRevealed)
	gtkx.ConnectNotify(&p.add, "active", &s.onWidgetsAddActive)
	drag := gtk.NewGestureDrag()
	drag.ConnectDragBegin(&s.onWidgetDragBegin)
	drag.ConnectDragUpdate(&s.onWidgetDragUpdate)
	drag.ConnectDragEnd(&s.onWidgetDragEnd)
	p.board.AddController(&drag.EventController)
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCaptureValue)
	keys.ConnectKeyPressed(&s.onWidgetsKey)
	p.win.AddController(&keys.EventController)

	// The page's sides are the bar's, and its gaps above and below the
	// bar's gap from the screen's top.
	side, gap := b.island.GetMarginStart(), b.island.GetMarginTop()
	// The columns its monitor shows, for the places found before the board
	// is laid out: less the page's sides and padding (style.css,
	// .widget-page).
	var geo gdk.Rectangle
	b.mon.GetGeometry(&geo)
	p.columns = (int(geo.Width) - 2*32 - 2*int(side)) / shell.WidgetCell

	p.win.SetApplication(s.app)
	w := p.win.GoPointer()
	if s.layer {
		layershell.InitForWindow(w)
		layershell.SetNamespace(w, "inua-widgets")
		layershell.SetLayer(w, layershell.LayerTop)
		layershell.SetKeyboardMode(w, layershell.KeyboardExclusive)
		for _, e := range []layershell.Edge{layershell.EdgeTop, layershell.EdgeBottom, layershell.EdgeLeft, layershell.EdgeRight} {
			layershell.SetAnchor(w, e, true)
		}
		// Between the bar and a dock that keeps its room: the dock stays,
		// to drag apps from.
		layershell.SetExclusiveZone(w, 0)
		layershell.SetMonitor(w, b.mon.GoPointer())
		layershell.SetMargin(w, layershell.EdgeLeft, side)
		layershell.SetMargin(w, layershell.EdgeRight, side)
		layershell.SetMargin(w, layershell.EdgeTop, gap)
		layershell.SetMargin(w, layershell.EdgeBottom, gap)
	} else {
		p.win.SetTitle(i18n.T("Engawa"))
		p.win.SetDefaultSize(1000, 700)
	}
	p.win.Present()
}

// snapshotGrid draws the grid a card snaps to: a cross at each joint,
// every WidgetCell, in the grid's colour (style.css, .widget-grid).
func snapshotGrid(w *gtk.Widget, sn *gtk.Snapshot) {
	var c gdk.RGBA
	w.GetColor(&c)
	const arm = 4 // each side of the joint's pixel
	width, height := int(w.GetWidth()), int(w.GetHeight())
	for y := 0; y <= height; y += shell.WidgetCell {
		for x := 0; x <= width; x += shell.WidgetCell {
			sn.AppendColor(&c, rect(float32(x-arm), float32(y), 2*arm+1, 1))
			sn.AppendColor(&c, rect(float32(x), float32(y-arm), 1, 2*arm+1))
		}
	}
}

// widgetNames are the widgets' names in the page's menus.
var widgetNames = map[string]string{
	"clock":    i18n.M("Clock"),
	"calendar": i18n.M("Calendar"),
	"weather":  i18n.M("Weather"),
	"media":    i18n.M("Now Playing"),
	"apps":     i18n.M("Apps"),
}

// widgetTags are their heads.
var widgetTags = map[string]string{
	"clock":    i18n.M("[CLOCK]"),
	"calendar": i18n.M("[CALENDAR]"),
	"weather":  i18n.M("[WEATHER]"),
	"media":    i18n.M("[NOW PLAYING]"),
	"apps":     i18n.M("[APPS]"),
}

// placeWidgets moves the cards to their places, while one is not dragged.
func (s *shellUI) placeWidgets() {
	p := &s.widgets
	if !p.open || p.dragKind != "" {
		return
	}
	for _, w := range s.cfg.Widgets.Places {
		if card := p.cards[w.Kind]; card != nil {
			p.board.Move(&card.Widget, float64(w.Column*shell.WidgetCell), float64(w.Row*shell.WidgetCell))
		}
	}
}

// fillWidgets builds the cards at their places, over the grid drawn while
// one is dragged, and the menu of those left out.
func (s *shellUI) fillWidgets() {
	p := &s.widgets
	if !p.open {
		return
	}
	if p.lines != nil {
		gtkx.ForgetCanvas(p.lines)
	}
	for ch := p.board.GetFirstChild(); ch != nil; ch = p.board.GetFirstChild() {
		p.board.Remove(ch) // drops the board's reference
		ch.Unref()         // and GetFirstChild's
	}
	p.time, p.zones, p.grid, p.wxDays, p.art, p.artURL = nil, nil, nil, nil, nil, ""
	p.cards, p.heads, p.rows, p.dragKind = map[string]*gtk.Box{}, map[string]*gtk.Box{}, map[string]int{}, ""
	p.lines = gtkx.NewCanvas(&gtkx.Canvas{Snapshot: snapshotGrid})
	p.lines.AddCssClass("widget-grid")
	p.lines.SetCanTarget(false)
	p.board.Put(p.lines, 0, 0) // floating: the board takes it
	p.spot = gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	p.spot.AddCssClass("widget-spot")
	p.spot.SetCanTarget(false)
	p.spot.SetVisible(false)
	p.board.Put(&p.spot.Widget, 0, 0)
	p.spot.Unref()
	for _, place := range s.cfg.Widgets.Places {
		kind := place.Kind
		card, body := s.widgetCard(kind)
		card.SetSizeRequest(config.WidgetSpan*shell.WidgetCell, -1)
		switch kind {
		case "clock":
			s.buildClockWidget(body)
		case "calendar":
			s.buildCalendarWidget(body)
		case "weather":
			s.buildWeatherWidget(body)
		case "media":
			s.buildMediaWidget(body)
		case "apps":
			s.buildAppsWidget(body)
		}
		p.board.Put(&card.Widget, float64(place.Column*shell.WidgetCell), float64(place.Row*shell.WidgetCell))
		card.Unref()
		p.cards[kind] = card
	}
	var adds []uikit.Item
	for _, k := range config.WidgetKinds {
		if p.cards[k] == nil {
			name := "add-" + k
			adds = append(adds, uikit.Item{Label: i18n.T(widgetNames[k]), Run: func() { s.widgetAction(name) }})
		}
	}
	p.add.SetVisible(len(adds) > 0)
	if len(adds) > 0 {
		pop := uikit.NewMenu(adds, uikit.Below)
		p.add.SetPopover(pop) // the button takes it
		pop.Unref()
	}
	s.tickWidgets(time.Now())
	s.showWidgetWeather()
	s.showWidgetMedia()
	for kind := range p.cards {
		s.sizeWidget(kind) // filled: as tall as it is
	}
	s.settleWidgets()
}

// widgetCard is a card: its head (the mark and the tag, by which it is
// dragged, and right-clicked for its menu) over its body.
func (s *shellUI) widgetCard(kind string) (card, body *gtk.Box) {
	card = gtk.NewBox(gtk.OrientationVerticalValue, 12)
	card.AddCssClass("widget-card")
	card.AddCssClass("widget-" + kind)
	card.SetName("card:" + kind)
	if kind == "apps" { // apps dropped on it from the dock
		drop := gtk.NewDropTarget(types.GType(gobject.TypeStringVal), gdk.ActionMoveValue|gdk.ActionCopyValue)
		drop.ConnectDrop(&s.onWidgetDrop)
		card.AddController(&drop.EventController)
	}

	head := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	head.AddCssClass("widget-head")
	head.SetName("head:" + kind)
	head.SetCursorFromName("grab")
	mark := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	mark.AddCssClass("mark")
	mark.SetValign(gtk.AlignCenterValue)
	gtkx.Append(head, &mark.Widget)
	tag := gtk.NewLabel(i18n.T(widgetTags[kind]))
	tag.AddCssClass("section-label")
	tag.SetXalign(0)
	gtkx.Append(head, &tag.Widget)
	s.widgets.heads[kind] = head // the board's drag starts on it
	menu := gtk.NewGestureClick()
	menu.SetButton(3)
	menu.ConnectPressed(&s.onWidgetMenu)
	head.AddController(&menu.EventController)
	gtkx.Append(card, &head.Widget)

	body = gtk.NewBox(gtk.OrientationVerticalValue, 10)
	body.AddCssClass("widget-body")
	gtkx.Append(card, &body.Widget)
	return card, body
}

// dragString lets w be dragged, carrying value.
func (s *shellUI) dragString(w *gtk.Widget, value string) {
	var v gobject.Value
	v.Init(gobject.TypeStringVal)
	v.SetString(value)
	content := gdk.NewContentProviderForValue(&v)
	v.Unset()
	drag := gtk.NewDragSource()
	drag.SetActions(gdk.ActionMoveValue)
	drag.SetContent(content)
	content.Unref()
	w.AddController(&drag.EventController)
}

// widgetPopover shows pop on the page at x, y of w.
func (s *shellUI) widgetPopover(pop gtk.Popover, w *gtk.Widget, x, y float64) {
	p := &s.widgets
	var r graphene.Rect
	if !w.ComputeBounds(&p.page.Widget, &r) {
		pop.Unref()
		return
	}
	f := *(*[4]float32)(unsafe.Pointer(&r))
	pop.SetHasArrow(false)
	pop.SetParent(&p.page.Widget)
	pop.SetPointingTo(&gdk.Rectangle{X: int32(float64(f[0]) + x), Y: int32(float64(f[1]) + y), Width: 1, Height: 1})
	pop.ConnectClosed(&s.onWidgetMenuClosed)
	pop.Unref() // the page holds it
	s.widgetsKeyboard(false)
	pop.Popup()
}

// widgetAction does what a page's menu chose: removes the widget or the
// app it opened on, or adds a widget at a free place.
func (s *shellUI) widgetAction(name string) {
	p := &s.widgets
	c := s.cfg
	switch {
	case name == "remove" && p.menuKind != "":
		c.Widgets.Places = slices.DeleteFunc(slices.Clone(c.Widgets.Places), func(w config.WidgetPlace) bool { return w.Kind == p.menuKind })
	case name == "remove" && p.menuApp != "":
		c.Widgets.Apps = slices.DeleteFunc(slices.Clone(c.Widgets.Apps), func(k string) bool { return k == p.menuApp })
	case strings.HasPrefix(name, "add-"):
		r := shell.FreeSpot(s.widgetRects(""), config.WidgetSpan, config.WidgetSpan, s.boardColumns())
		c.Widgets.Places = append(slices.Clone(c.Widgets.Places), config.WidgetPlace{Kind: strings.TrimPrefix(name, "add-"), Column: r.Column, Row: r.Row})
	default:
		return
	}
	s.changeSettings(c)
}

// widgetsKeyboard gives the page the keyboard alone, or lets go of it while
// one of its popovers is open: Hyprland gives no clicks to the popups of a
// layer surface that holds the keyboard exclusively. Taken back, the
// keyboard goes through none first, as the launcher's does after its menu.
func (s *shellUI) widgetsKeyboard(exclusive bool) {
	p := &s.widgets
	if !s.layer || !p.open {
		return
	}
	w := p.win.GoPointer()
	if !exclusive {
		layershell.SetKeyboardMode(w, layershell.KeyboardOnDemand)
		return
	}
	layershell.SetKeyboardMode(w, layershell.KeyboardNone)
	win := p.win
	s.runAfter(30, func() {
		if p.open && p.win.GoPointer() == win.GoPointer() {
			layershell.SetKeyboardMode(w, layershell.KeyboardExclusive)
		}
	})
}

func label(text string, classes ...string) *gtk.Label {
	l := gtk.NewLabel(text)
	for _, c := range classes {
		l.AddCssClass(c)
	}
	l.SetXalign(0)
	return l
}

// widgetButton is a flat button over child, named for onWidgetButton.
func (s *shellUI) widgetButton(name string, child *gtk.Widget, classes ...string) *gtk.Button {
	b := gtk.NewButton()
	b.SetName(name)
	b.SetFocusable(false)
	for _, c := range classes {
		b.AddCssClass(c)
	}
	b.SetChild(child)
	child.Unref()
	b.ConnectClicked(&s.onWidgetButton)
	return b
}

// clockApp and calendarApp are the light apps the clock and calendar
// widgets open.
const (
	clockApp    = "io.github.lyricat.Inua.Clock"
	calendarApp = "io.github.lyricat.Inua.Calendar"
)

func (s *shellUI) buildClockWidget(body *gtk.Box) {
	p := &s.widgets
	box := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	p.time = label("", "widget-time")
	gtkx.Append(box, &p.time.Widget)
	p.date = label("", "readout")
	gtkx.Append(box, &p.date.Widget)
	open := s.widgetButton("open-clock", &box.Widget, "widget-plain")
	open.SetTooltipText(i18n.T("Open Clock"))
	gtkx.Append(body, &open.Widget)
	p.zones = gtk.NewBox(gtk.OrientationVerticalValue, 0)
	p.zones.AddCssClass("rows")
	gtkx.Append(body, &p.zones.Widget)
}

func (s *shellUI) buildCalendarWidget(body *gtk.Box) {
	p := &s.widgets
	box := gtk.NewBox(gtk.OrientationVerticalValue, 8)
	p.month = label("", "readout")
	gtkx.Append(box, &p.month.Widget)
	p.grid = gtk.NewGrid()
	p.grid.AddCssClass("agenda-grid")
	p.grid.SetColumnHomogeneous(true)
	box.Append(&p.grid.Widget)
	p.grid.Unref()
	open := s.widgetButton("open-calendar", &box.Widget, "widget-plain")
	open.SetTooltipText(i18n.T("Open Calendar"))
	gtkx.Append(body, &open.Widget)
	p.alarm = label("", "sub")
	gtkx.Append(body, &p.alarm.Widget)
}

func (s *shellUI) buildWeatherWidget(body *gtk.Box) {
	p := &s.widgets
	box := gtk.NewBox(gtk.OrientationVerticalValue, 10)
	now := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	p.wxIcon = gtk.NewImage()
	p.wxIcon.SetPixelSize(32)
	gtkx.Append(now, &p.wxIcon.Widget)
	text := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	p.wxTemp = label("", "agenda-weather-temp")
	gtkx.Append(text, &p.wxTemp.Widget)
	p.wxText = label("", "agenda-weather-text")
	p.wxText.SetWrap(true)
	gtkx.Append(text, &p.wxText.Widget)
	text.SetHexpand(true)
	gtkx.Append(now, &text.Widget)
	side := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	p.wxPlace = label("", "readout")
	p.wxPlace.SetXalign(1)
	gtkx.Append(side, &p.wxPlace.Widget)
	p.wxHL = label("", "agenda-weather-text")
	p.wxHL.SetXalign(1)
	gtkx.Append(side, &p.wxHL.Widget)
	gtkx.Append(now, &side.Widget)
	gtkx.Append(box, &now.Widget)
	p.wxDays = gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	p.wxDays.AddCssClass("widget-days")
	p.wxDays.SetHomogeneous(true)
	gtkx.Append(box, &p.wxDays.Widget)
	open := s.widgetButton("open-weather", &box.Widget, "widget-plain")
	open.SetTooltipText(i18n.T("Open Weather"))
	gtkx.Append(body, &open.Widget)
	s.readWeather()
}

func (s *shellUI) buildMediaWidget(body *gtk.Box) {
	p := &s.widgets
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	p.art = gtk.NewImage()
	p.art.AddCssClass("widget-art")
	gtkx.Append(row, &p.art.Widget)
	text := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	text.SetValign(gtk.AlignCenterValue)
	text.SetHexpand(true)
	p.title = label("", "label")
	p.title.SetEllipsize(3) // end
	gtkx.Append(text, &p.title.Widget)
	p.artist = label("", "sub")
	p.artist.SetEllipsize(3)
	gtkx.Append(text, &p.artist.Widget)
	gtkx.Append(row, &text.Widget)
	gtkx.Append(body, &row.Widget)
	keys := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
	keys.SetHalign(gtk.AlignCenterValue)
	icon := func(name string) *gtk.Widget { return &gtk.NewImageFromIconName(name).Widget }
	p.prev = s.widgetButton("media-prev", icon("media-skip-backward-symbolic"), "flat")
	p.play = s.widgetButton("media-play", icon("media-playback-start-symbolic"), "flat")
	p.next = s.widgetButton("media-next", icon("media-skip-forward-symbolic"), "flat")
	for _, b := range []*gtk.Button{p.prev, p.play, p.next} {
		gtkx.Append(keys, &b.Widget)
	}
	gtkx.Append(body, &keys.Widget)
}

// widgetAppSize is the apps widget's icon size.
const widgetAppSize = 40

func (s *shellUI) buildAppsWidget(body *gtk.Box) {
	tiles := gtk.NewFlowBox()
	tiles.AddCssClass("widget-apps")
	tiles.SetSelectionMode(gtk.SelectionNoneValue)
	tiles.SetHomogeneous(true)
	tiles.SetMinChildrenPerLine(4)
	tiles.SetMaxChildrenPerLine(4)
	tiles.SetColumnSpacing(4)
	tiles.SetRowSpacing(4)
	for _, id := range s.cfg.Widgets.Apps {
		e := s.dockState.apps.ByID(id)
		if e == nil {
			continue // not installed, or not read yet
		}
		tile := s.appTile("app:"+id, e.Name, e.Icon)
		s.dragString(&tile.Widget, id)
		drop := gtk.NewDropTarget(types.GType(gobject.TypeStringVal), gdk.ActionMoveValue|gdk.ActionCopyValue)
		drop.ConnectDrop(&s.onWidgetDrop)
		tile.AddController(&drop.EventController)
		menu := gtk.NewGestureClick()
		menu.SetButton(3)
		menu.ConnectPressed(&s.onWidgetMenu)
		tile.AddController(&menu.EventController)
		tiles.Append(&tile.Widget)
		tile.Unref()
	}
	add := s.appTile("add-app", i18n.T("Add"), "list-add-symbolic")
	add.AddCssClass("widget-add")
	add.SetTooltipText(i18n.T("Add an app, or drag one here from the dock"))
	tiles.Append(&add.Widget)
	add.Unref()
	body.Append(&tiles.Widget)
	tiles.Unref()
}

// appTile is an app's icon over its name.
func (s *shellUI) appTile(name, title, icon string) *gtk.Button {
	box := gtk.NewBox(gtk.OrientationVerticalValue, 6)
	img := gtk.NewImage()
	setIcon(img, icon)
	img.SetPixelSize(widgetAppSize)
	gtkx.Append(box, &img.Widget)
	l := gtk.NewLabel(title)
	l.AddCssClass("sub")
	l.SetEllipsize(3)
	l.SetMaxWidthChars(9)
	gtkx.Append(box, &l.Widget)
	b := s.widgetButton(name, &box.Widget, "widget-app")
	b.SetTooltipText(title)
	return b
}

// tickWidgets redraws what follows the time: the clock, the zones, the
// month, the next alarm.
func (s *shellUI) tickWidgets(now time.Time) {
	p := &s.widgets
	if !p.open || p.closing {
		return
	}
	defer s.sizeWidget("clock") // the zones may change
	defer s.sizeWidget("calendar")
	if p.time != nil {
		layout := "15:04"
		if c := s.cfg.Bar.Clock; strings.Contains(c, "%I") || strings.Contains(c, "%l") || strings.Contains(c, "%p") {
			layout = "3:04"
		}
		p.time.SetText(i18n.Time(now, layout))
		_, week := now.ISOWeek()
		p.date.SetText(i18n.Tf("%s · WK %d", strings.ToUpper(i18n.Time(now, "Monday, January 2")), week))
		var data clock.Data
		if err := store.Load(clock.StoreName, &data); err != nil {
			log.Printf("widgets: clock: %v", err)
		}
		gtkx.Clear(p.zones)
		for _, z := range data.Zones {
			loc, err := time.LoadLocation(z)
			if err != nil {
				continue
			}
			row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
			row.AddCssClass("widget-zone")
			text := gtk.NewBox(gtk.OrientationVerticalValue, 0)
			text.SetHexpand(true)
			city := label(clock.City(z), "label")
			gtkx.Append(text, &city.Widget)
			diff := label(clock.Difference(loc, now), "sub")
			gtkx.Append(text, &diff.Widget)
			gtkx.Append(row, &text.Widget)
			t := label(i18n.Time(now.In(loc), layout), "readout", "widget-zone-time")
			t.SetValign(gtk.AlignCenterValue)
			gtkx.Append(row, &t.Widget)
			gtkx.Append(p.zones, &row.Widget)
		}
		p.zones.SetVisible(len(data.Zones) > 0)
	}
	if p.grid != nil {
		var data calendar.Data
		if err := store.Load(calendar.StoreName, &data); err != nil {
			log.Printf("widgets: calendar: %v", err)
		}
		p.month.SetText(strings.ToUpper(i18n.Time(now, "January 2006")))
		fillMonth(p.grid, firstOfMonth(now), data.MondayFirst, now)
		p.alarm.SetText(nextAlarm(now))
		p.alarm.SetVisible(p.alarm.GetText() != "")
	}
}

// nextAlarm says when the next alarm set in Clock goes off, "" for none.
func nextAlarm(now time.Time) string {
	var data clock.Data
	if err := store.Load(clock.StoreName, &data); err != nil {
		return ""
	}
	var next time.Time
	var what string
	for _, a := range data.Alarms {
		if !a.On {
			continue
		}
		if t := a.Next(now); next.IsZero() || t.Before(next) {
			next, what = t, a.Label
		}
	}
	if next.IsZero() {
		return ""
	}
	day := i18n.Time(next, "Mon")
	if y, m, d := next.Date(); y == now.Year() && m == now.Month() && d == now.Day() {
		day = i18n.T("Today")
	}
	return strings.TrimSuffix(i18n.Tf("Alarm %s %s · %s", day, i18n.Time(next, "15:04"), what), " · ")
}

// showWidgetWeather fills the weather widget from the agenda's forecast.
func (s *shellUI) showWidgetWeather() {
	p := &s.widgets
	if !p.open || p.wxDays == nil {
		return
	}
	defer s.sizeWidget("weather")
	a := &s.agenda
	gtkx.Clear(p.wxDays)
	p.wxPlace.SetText(strings.ToUpper(a.wxFor.Name))
	p.wxHL.SetText("")
	switch {
	case a.wxFor == weather.Place{}:
		p.wxIcon.SetFromIconName("weather-few-clouds-symbolic")
		p.wxTemp.SetText(i18n.T("Weather"))
		p.wxText.SetText(i18n.T("Add a place in Weather to see it here"))
	case a.wxAt.IsZero():
		p.wxIcon.SetFromIconName("weather-few-clouds-symbolic")
		p.wxTemp.SetText("—")
		p.wxText.SetText(i18n.T("Reading the forecast…"))
		if a.wxErr != nil && !a.wxLoading {
			p.wxText.SetText(i18n.T("No forecast: offline?"))
		}
	default:
		f := a.wx
		text, icon := weather.Describe(f.Now.Code, f.Now.Day)
		p.wxIcon.SetFromIconName(icon)
		p.wxTemp.SetText(weather.Temp(f.Now.Temp))
		p.wxText.SetText(i18n.Tf("%s · feels %s", text, weather.Temp(f.Now.Feels)))
		if len(f.Days) > 0 {
			p.wxHL.SetText(i18n.Tf("H %s  L %s", weather.Temp(f.Days[0].Max), weather.Temp(f.Days[0].Min)))
		}
		weatherDays(p.wxDays, f) // in the room kept for them (.widget-days)
	}
}

// showWidgetMedia fills the player widget from the media state.
func (s *shellUI) showWidgetMedia() {
	p := &s.widgets
	if !p.open || p.art == nil {
		return
	}
	defer s.sizeWidget("media")
	m := s.sys.Media
	if !m.Available {
		p.title.SetText(i18n.T("Nothing playing"))
		p.artist.SetText(i18n.T("A player that plays shows here"))
	} else {
		title, artist := shell.MediaText(m)
		p.title.SetText(title)
		p.artist.SetText(cmp.Or(artist, m.Identity))
	}
	p.play.SetIconName(shell.PlayIcon(m.Playing))
	p.play.SetSensitive(m.Available && m.CanPlayPause)
	p.prev.SetSensitive(m.Available && m.CanPrev)
	p.next.SetSensitive(m.Available && m.CanNext)
	url := m.ArtURL
	if !m.Available {
		url = ""
	}
	if url == p.artURL && p.art.GetStorageType() != gtk.ImageEmptyValue {
		return
	}
	p.artURL = url
	if path := localPath(url); path != "" {
		p.art.SetFromFile(path)
		p.art.SetPixelSize(64)
	} else {
		p.art.SetFromIconName("audio-x-generic-symbolic")
		p.art.SetPixelSize(28)
	}
}

// pickWidgetApp opens the search for an app to add, at the + tile.
func (s *shellUI) pickWidgetApp(at *gtk.Widget) {
	p := &s.widgets
	pop := gtk.NewPopover()
	pop.AddCssClass("widget-picker")
	box := gtk.NewBox(gtk.OrientationVerticalValue, 6)
	query := gtk.NewSearchEntry()
	query.SetPlaceholderText(i18n.T("Search apps"))
	query.ConnectSearchChanged(&s.onWidgetQuery)
	query.ConnectActivate(&s.onWidgetQueryActivate)
	gtkx.Append(box, &query.Widget)
	list := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	gtkx.Append(box, &list.Widget)
	box.SetSizeRequest(240, -1)
	pop.SetChild(&box.Widget)
	box.Unref()
	uikit.Menu(pop, uikit.Below)
	p.picker = &widgetPicker{pop: pop, query: query, list: list}
	s.fillWidgetPicker()
	s.widgetPopover(*pop, at, float64(at.GetWidth())/2, float64(at.GetHeight()))
	query.GrabFocus()
}

// widgetPickerRows is how many apps the search shows at most.
const widgetPickerRows = 8

// fillWidgetPicker lists the apps matching the search, not on the widget
// yet.
func (s *shellUI) fillWidgetPicker() {
	pk := s.widgets.picker
	if pk == nil {
		return
	}
	gtkx.Clear(pk.list)
	q := strings.ToLower(strings.TrimSpace(pk.query.GetText()))
	var found []*desktop.Entry
	for _, e := range s.apps {
		if e.NoDisplay || slices.Contains(s.cfg.Widgets.Apps, e.ID) || s.dockState.apps.ByID(e.ID) == nil {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(e.Name), q) || strings.Contains(strings.ToLower(e.GenericName), q) {
			found = append(found, e)
		}
	}
	slices.SortFunc(found, func(a, b *desktop.Entry) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	for _, e := range found[:min(len(found), widgetPickerRows)] {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		img := gtk.NewImage()
		setIcon(img, e.Icon)
		img.SetPixelSize(20)
		gtkx.Append(row, &img.Widget)
		gtkx.Append(row, &label(e.Name, "label").Widget)
		b := s.widgetButton("pick:"+e.ID, &row.Widget, "menu-item", "widget-pick")
		gtkx.Append(pk.list, &b.Widget)
	}
	if len(found) == 0 {
		gtkx.Append(pk.list, &label(i18n.T("No app by that name"), "sub").Widget)
	}
}

// widgetsRequest answers `inua call widgets toggle|open|close|right|left`.
func (s *shellUI) widgetsRequest(fn string) bool {
	switch fn {
	case "toggle":
		s.toggleWidgets()
	case "open":
		s.openWidgets(nil)
	case "close":
		s.closeWidgets()
	case "right":
		s.swipeWorkspaces(-1)
	case "left":
		s.swipeWorkspaces(1)
	default:
		return false
	}
	return true
}

// swipeWorkspaces is the three-finger swipe (widgets.swipe = 3), which
// takes the workspace swipe's place: right (dir < 0) goes to the previous
// workspace the focused monitor's bar shows, and from the first to the
// widget page; left comes back from the page, or goes to the next
// workspace, and past the last to a new one, as Hyprland's swipe does.
// Right again on the page goes to the first workspace.
func (s *shellUI) swipeWorkspaces(dir int) {
	var b *bar
	for _, x := range s.bars {
		if x.connector() == s.snap.FocusedOutput {
			b = x
		}
	}
	if b == nil {
		return
	}
	if s.widgetsShownOn(b) {
		s.closeWidgets()
		if dir < 0 && len(b.shown) > 0 && !b.shown[0].Active {
			s.switchWorkspace(b.shown[0].ID)
		}
		return
	}
	current := s.snap.ActiveWorkspace(b.connector())
	i := slices.IndexFunc(b.shown, func(w compositor.Workspace) bool { return w.ID == current })
	switch {
	case dir < 0 && i <= 0:
		s.openWidgets(b)
	case dir < 0:
		s.switchWorkspace(b.shown[i-1].ID)
	case i >= 0 && i+1 < len(b.shown):
		s.switchWorkspace(b.shown[i+1].ID)
	case current != "" && slices.ContainsFunc(s.snap.Windows, func(t compositor.Window) bool { return t.Workspace == current }):
		// Past the last: an empty one, as the compositor names it.
		if ws, ok := s.newWorkspace(s.snap, b.connector(), nil); ok {
			s.switchWorkspace(ws.ID)
		}
	}
}
