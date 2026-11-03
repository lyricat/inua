package ui

import (
	"math"
	"time"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gsk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/layershell"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/uikit"
)

// drop hangs a card from the bar (shell.DropAt) as one piece with it: the
// card comes down out of the bar's lower edge, soft, stretching and
// wobbling, and goes back up into it. The card moves into a canvas as big
// as the window, which draws the bar's glass for it: the card's box, arcs
// that join it to the bar, and one outline round both with the bar's
// lower edge, whose own border is off meanwhile (.joined). A flush card
// is flush with the bar's end instead, its side going on from the bar's,
// with no arc there (.joined-end squares the bar's corner). The card
// itself is .hanging, with no background of its own, and the canvas
// places it, stretched, clipped at the bar's edge. The drop outlives the windows:
// each window gives it its backdrop and card when built.
//
// The backdrop hangs inside the bar's window, which grows down over the
// monitor meanwhile (bar.hold): one surface, which the compositor blurs
// as one, with no seam where the card meets the bar. The panel's own
// window, never shown then, has it back to go with it; without a bar the
// card shows in that window instead.
type drop struct {
	win      *gtk.Window
	keys     *gtk.EventController // the panel's, on the window it shows in
	layer    layershell.Layer     // the bar's while it hangs from it
	app      *gtk.Application     // for a window that has to show itself
	canvas   *gtk.Widget
	backdrop *gtk.Box
	card     *gtk.Widget
	host     *bar
	bar      *gtk.Widget             // the window of the bar it hangs from
	keyboard layershell.KeyboardMode // the bar's keyboard mode while it hangs

	// Where it hangs, in the window's coordinates: the bar's island ends
	// at left and right and its lower edge is at y, with corners of
	// radius; border is whether it has a border; corner is the card's.
	left, right, y float64
	radius, corner float64
	border         bool
	end            bool    // flush with the bar's end
	under, at      float64 // the middle of the button it hangs under, and its left edge
	x, w, h        float64 // the card's box, at rest
	glass, line    gdk.RGBA

	hanging bool
	moving  bool
	opening bool
	back    bool    // closing while it opened: the opening's frames backwards
	u       float64 // how far through, 0 to 1
	waited  int     // frames waited for the bar to grow
	last    time.Time
	tick    uint32
	gone    func() // when it has closed

	onTick     gtk.TickCallback
	onTickDone glib.DestroyNotify

	// A card whose width eases to a new one (resizes) rather than jumping:
	// the width drawn, and from and to which it goes, wu of the way.
	resizes        bool
	shownW         float64
	fromW, toW, wu float64
	wlast          time.Time
	wtick          uint32
	onWTick        gtk.TickCallback
	onWTickDone    glib.DestroyNotify
}

// attach takes a newly built window, its keys' controller, its backdrop
// and the card in it, to hang on layer; a flush card is flush with the
// bar's end.
func (d *drop) attach(win *gtk.Window, keys *gtk.EventController, backdrop *gtk.Box, card *gtk.Widget, layer layershell.Layer, flush bool) {
	d.win, d.keys, d.layer = win, keys, layer
	d.backdrop, d.card, d.canvas, d.bar, d.host = backdrop, card, nil, nil, nil
	d.end = flush
	d.keyboard = layershell.KeyboardExclusive
	d.hanging, d.moving, d.back, d.gone = false, false, false, nil
	d.shownW = 0
	if d.onTick == nil {
		d.onTick = func(uintptr, uintptr, uintptr) bool { return d.step() }
		d.onTickDone = func(uintptr) { d.tick = 0 }
		d.onWTick = func(uintptr, uintptr, uintptr) bool { return d.stepWidth() }
		d.onWTickDone = func(uintptr) { d.wtick = 0 }
	}
}

// detach gives the window its card back to go with it, and the bar its
// border.
func (d *drop) detach() {
	if d.bar != nil {
		d.bar.RemoveCssClass("joined")
		d.bar.RemoveCssClass("joined-end")
	}
	if d.canvas != nil {
		// Both go: the card back into the backdrop it was built in, the
		// canvas out of the backdrop entirely. A canvas left there keeps
		// expanding, and the next open adds another beside it, the two
		// splitting the window's height between them - the card then lays
		// itself out in the lower half, off the bar.
		d.card.Unparent()
		d.backdrop.Remove(d.canvas)
		d.backdrop.Append(d.card)
		gtkx.ForgetCanvas(d.canvas)
	}
	if b := d.host; b != nil {
		// The keys' controller goes back to the panel's window, which then
		// holds the only reference again. RemoveController drops the ref the
		// bar held, so the controller is held across it, and AddController
		// takes that ref as its own: unref it as well and the widget keeps a
		// controller that is already freed (Gtk-CRITICAL on the next event).
		d.keys.Ref()
		b.win.RemoveController(d.keys)
		d.win.AddController(d.keys)
		d.backdrop.Ref()
		b.frame.RemoveOverlay(&d.backdrop.Widget)
		for _, c := range d.win.GetCssClasses() {
			d.backdrop.RemoveCssClass(c)
		}
		d.win.SetChild(&d.backdrop.Widget)
		d.backdrop.Unref()
		b.release()
	}
	d.canvas, d.backdrop, d.card, d.bar, d.host = nil, nil, nil, nil, nil
	d.win, d.keys = nil, nil
}

// destroyPanel drops a panel's window. A panel that hung from the bar keeps
// its card in the bar's window instead, so its own window was never realized
// and has no GdkSurface; Destroy on such a window reaches a null surface and
// takes the shell down with a SIGSEGV (gdk_surface_get_display),
// so that one is only hidden.
func destroyPanel(w *gtk.Window) {
	if w == nil {
		return
	}
	if w.GetSurface() == nil {
		w.SetVisible(false)
		return
	}
	w.Destroy()
}

// moveTo moves the backdrop into the bar b's window, with the panel's
// classes for its style, and the keys with it.
func (d *drop) moveTo(b *bar) {
	d.backdrop.Ref()
	d.win.SetChild(nil)
	for _, c := range d.win.GetCssClasses() {
		if c != "background" { // GTK's, for a window's own
			d.backdrop.AddCssClass(c)
		}
	}
	b.hold(d.layer, d.keyboard)
	b.frame.AddOverlay(&d.backdrop.Widget)
	d.backdrop.Unref()
	d.keys.Ref() // RemoveController drops the ref the panel's window held; AddController takes this one
	d.win.RemoveController(d.keys)
	b.win.AddController(d.keys)
	d.host = b
}

// hang moves the card into a canvas, to hang from the bar b under the
// button whose middle is at under.
// rehang moves the open card under another button of the same bar: the
// menu follows the pointer along the bar, as a Mac's does, instead of
// staying where it was first opened.
func (d *drop) rehang(b *bar, island [4]float32, under, at float64) {
	if d.host != b || d.canvas == nil {
		return
	}
	d.left, d.right = float64(island[0]), float64(island[0]+island[2])
	d.y = float64(island[1] + island[3])
	d.under, d.at = under, at
	if d.shownW != 0 {
		d.resizes = true // the new section's width eases in
	}
	d.canvas.QueueAllocate()
	d.canvas.QueueDraw()
}

// hang hangs the card from the bar b, under the button whose middle is at
// under; at is the button's left edge, where the card starts when centring
// it would push it off the button's own end.
func (d *drop) hang(s *shellUI, b *bar, island [4]float32, under, at float64) bool {
	if !d.colors(b.win.HasCssClass("empty")) {
		return false
	}
	d.moveTo(b)
	d.canvas = gtkx.NewCanvas(&gtkx.Canvas{Snapshot: d.snapshot, Allocate: d.allocate})
	d.canvas.SetHexpand(true)
	d.canvas.SetVexpand(true)
	d.card.Ref()
	d.backdrop.Remove(d.card)
	d.card.SetParent(d.canvas)
	d.card.Unref()
	d.backdrop.Append(d.canvas)
	d.card.SetMarginTop(0)
	d.card.SetMarginEnd(0)
	d.card.AddCssClass("hanging")
	d.backdrop.AddCssClass("hanging")

	d.left, d.right = float64(island[0]), float64(island[0]+island[2])
	d.y = float64(island[1] + island[3])
	d.under, d.at = under, at
	d.radius = float64(s.cfg.Style.Radius)
	if !b.edgeGaps {
		d.radius = 0 // flush: square
	}
	d.corner = float64(s.cfg.Style.Radius)
	d.border = !b.compact // a compact bar has none
	d.bar = &b.win.Widget
	d.bar.AddCssClass("joined")
	if d.end {
		d.bar.AddCssClass("joined-end")
	}
	d.hanging = true
	return true
}

// colors reads the bar's glass (empty, an empty workspace's) and border
// from the theme.
func (d *drop) colors(empty bool) bool {
	sc := d.backdrop.GetStyleContext()
	glass := "glass"
	if empty {
		glass = "glass_empty"
	}
	if !sc.LookupColor(glass, &d.glass) || !sc.LookupColor("line", &d.line) {
		return false
	}
	d.line.Alpha *= 0.9 // as the island's: alpha(@line, 0.9)
	return true
}

// open drops the card; with animations off it hangs there at once.
func (d *drop) open() {
	if !d.hanging {
		return
	}
	d.opening = true
	if !gtk.SettingsGetDefault().GetPropertyGtkEnableAnimations() {
		d.u = 1
		return
	}
	d.u = 0
	d.start()
}

// close draws the card back up and calls gone when it is out of sight; a
// drop still opening goes back the way it came.
func (d *drop) close(gone func()) {
	switch {
	case !d.hanging || !gtk.SettingsGetDefault().GetPropertyGtkEnableAnimations():
		gone()
	case d.moving && d.opening:
		d.opening, d.back = false, true
		d.gone = gone
	case d.moving:
		d.gone = gone
	default:
		d.opening, d.u = false, 0
		d.gone = gone
		d.start()
	}
}

// finish ends a close still going, at once.
func (d *drop) finish() {
	if gone := d.gone; gone != nil {
		d.gone, d.moving = nil, false
		gone()
	}
}

func (d *drop) start() {
	d.moving = true
	d.last = time.Now()
	d.waited = 0
	if d.tick == 0 {
		d.tick = d.canvas.AddTickCallback(&d.onTick, 0, &d.onTickDone)
	}
	d.canvas.QueueAllocate()
}

// step moves the drop on a frame. Its time starts once the bar's window
// has grown to hold it, and a slow frame moves it on one frame's time at
// most, so it starts from the bar and never jumps.
func (d *drop) step() bool {
	now := time.Now()
	dt := min(float64(now.Sub(d.last).Milliseconds()), 34)
	d.last = now
	if d.host != nil && float64(d.canvas.GetHeight()) <= d.y+1 && d.waited < 12 {
		d.waited++
		return true
	}
	switch {
	case d.opening:
		d.u += dt / shell.DropOpenMS
	case d.back: // as fast as a close
		d.u -= dt / shell.DropCloseMS
	default:
		d.u += dt / shell.DropCloseMS
	}
	d.canvas.QueueAllocate()
	switch {
	case d.opening && d.u >= 1:
		d.u, d.moving = 1, false
		return false
	case !d.opening && d.moving && (d.back && d.u <= 0 || !d.back && d.u >= 1):
		d.u, d.moving = 0, false // drawn out of sight first, then gone
	case !d.opening && !d.moving:
		if gone := d.gone; gone != nil {
			d.gone = nil
			gone()
		}
		return false
	}
	return true
}

// width is the width to allocate the card at: its natural width, or,
// for a card that resizes, the width on its way to it, never under the
// least it takes.
func (d *drop) width(least, natural int32) int32 {
	to := float64(natural)
	switch {
	case !d.resizes || d.shownW == 0 || d.moving || !gtk.SettingsGetDefault().GetPropertyGtkEnableAnimations():
		d.shownW, d.toW = to, to // at once: the first time, or while it drops
	case to != d.toW:
		d.fromW, d.toW, d.wu = d.shownW, to, 0
		d.wlast = time.Now()
		if d.wtick == 0 {
			d.wtick = d.canvas.AddTickCallback(&d.onWTick, 0, &d.onWTickDone)
		}
	}
	return max(least, int32(math.Round(d.shownW)))
}

// stepWidth moves the width on a frame.
func (d *drop) stepWidth() bool {
	now := time.Now()
	d.wu += min(float64(now.Sub(d.wlast).Milliseconds()), 34) / shell.DropResizeMS
	d.wlast = now
	d.shownW = shell.DropWidth(d.fromW, d.toW, d.wu)
	d.canvas.QueueAllocate()
	if d.wu >= 1 {
		d.shownW = d.toW
		return false
	}
	return true
}

// now is the card's shape now.
func (d *drop) now() shell.Drop {
	if !d.moving {
		if d.opening {
			return shell.Drop{Shown: 1, ScaleX: 1, ScaleY: 1}
		}
		return shell.Drop{ScaleX: 1, ScaleY: 1}
	}
	return shell.DropAt(d.u, d.opening || d.back)
}

// allocate places the card at its natural size, flush with the bar's end
// or under its button, with room for the arcs inside the bar, as far up
// in the bar as it has not yet come out, and stretched about where it
// meets the bar.
func (d *drop) allocate(_ *gtk.Widget, _, _ int) {
	var least, w, h int32
	d.card.Measure(gtk.OrientationHorizontalValue, -1, &least, &w, nil, nil)
	w = d.width(least, w)
	d.card.Measure(gtk.OrientationVerticalValue, w, nil, &h, nil, nil)
	d.w, d.h = float64(w), float64(h)
	if d.end {
		// Its outline, just outside it, on the bar's border, just inside
		// the bar.
		d.x = d.right - 1 - d.w
	} else {
		room := d.radius + d.corner // the arc's
		// Centred under what opened it, inside the bar's island: the card
		// only gives way when it would run off the island's end, and then
		// it stops at the edge rather than at the button's own left side -
		// a card that jumped to the button's left edge looked dropped at
		// the screen's corner whenever the button sat far right.
		d.x = math.Round(max(d.left+room, min(d.under-d.w/2, d.right-room-d.w)))
	}
	n := d.now()
	at := d.anchor()
	t := gsk.NewTransform().
		Translate(&graphene.Point{X: float32(at), Y: float32(d.y)}).
		Scale(float32(n.ScaleX), float32(n.ScaleY)).
		Translate(&graphene.Point{X: float32(d.x - at), Y: float32(-d.h * (1 - n.Shown))})
	d.card.Allocate(w, h, -1, t)
	d.canvas.QueueDraw() // the glass follows the card
}

// anchor is where the card stretches from along the bar: its middle, or
// its end, which stays on the bar's.
func (d *drop) anchor() float64 {
	if d.end {
		return d.x + d.w
	}
	return d.x + d.w/2
}

func (d *drop) snapshot(_ *gtk.Widget, s *gtk.Snapshot) {
	if !d.hanging {
		return
	}
	n := d.now()
	at := d.anchor()
	l, r := at+(d.x-at)*n.ScaleX, at+(d.x+d.w-at)*n.ScaleX
	b := d.y + d.h*n.Shown*n.ScaleY
	cw, ch := float32(d.canvas.GetWidth()), float32(d.canvas.GetHeight())
	stroke := gsk.NewStroke(1)
	defer stroke.Free()
	if b-d.y < 0.5 {
		if d.border {
			edge := barEdge(d)
			s.AppendStroke(edge, stroke, &d.line)
			edge.Unref()
		}
		return
	}
	g := d.shape(l, r, b)

	// The shadow, round the card's box, neither over the bar nor under
	// the glass: the arcs would be darker than the bar and the card.
	fill := g.glass()
	below := rect(0, float32(d.y), cw, ch)
	s.PushClip(below)
	s.PushMask(gsk.MaskModeInvertedAlphaValue)
	s.AppendFill(fill, gsk.FillRuleWindingValue, &gdk.RGBA{Alpha: 1})
	s.Pop()
	shadow := gdk.RGBA{Alpha: 0.32}
	s.AppendOutsetShadow(hangingRect(l, d.y, r-l, b-d.y, g.round), &shadow, 0, 8, 0, 28)
	s.Pop()
	s.Pop()
	s.AppendFill(fill, gsk.FillRuleWindingValue, &d.glass)
	fill.Unref()
	if d.border {
		edge := g.outline()
		s.AppendStroke(edge, stroke, &d.line)
		edge.Unref()
	}
	s.PushClip(below)
	d.canvas.SnapshotChild(d.card, s)
	s.Pop()
}

// dropShape is the outline the canvas draws: the card's box from l to r
// and down to bottom, joined to the bar's edge at y by arcs of left and
// right, inside the island from il to ir with corners of radius; or flush
// with its end (end), where the island's corner is square.
type dropShape struct {
	l, r, y, bottom float64
	left, right     float64 // the arcs' sizes
	round           float64 // the card's lower corners
	il, ir, radius  float64
	end             bool
}

func (d *drop) shape(l, r, b float64) dropShape {
	g := dropShape{l: l, r: r, y: d.y, bottom: b, il: d.left, ir: d.right, radius: d.radius, end: d.end}
	g.round = min(d.corner, (r-l)/2, (b-d.y)/2)
	arc := min(d.corner, b-d.y-g.round) // as round as the corners
	g.left = max(0, min(arc, l-(d.left+d.radius)))
	g.right = max(0, min(arc, d.right-d.radius-r))
	if d.end {
		g.right = 0
	}
	return g
}

// glass is the shape's inside: the card's box and the arcs, up to the
// bar's edge.
func (g dropShape) glass() *gsk.Path {
	pb := gsk.NewPathBuilder()
	pb.MoveTo(f32(g.l-g.left), f32(g.y))
	uikit.Arc(pb, g.l-g.left, g.y+g.left, g.left, 270, 360)
	pb.LineTo(f32(g.l), f32(g.bottom-g.round))
	uikit.Arc(pb, g.l+g.round, g.bottom-g.round, g.round, 180, 90)
	pb.LineTo(f32(g.r-g.round), f32(g.bottom))
	uikit.Arc(pb, g.r-g.round, g.bottom-g.round, g.round, 90, 0)
	pb.LineTo(f32(g.r), f32(g.y+g.right))
	uikit.Arc(pb, g.r+g.right, g.y+g.right, g.right, 180, 270)
	pb.Close()
	return pb.FreeToPath()
}

// outline is the border round the bar's island and the card as one: the
// island's lower corners and edge, down each arc, round the card. It runs
// half a pixel inside the island, as the island's own border does, and
// half a pixel outside the card, so its pixels are whole; each arc, half
// a pixel up and out, goes from the one line into the other without a
// step.
func (g dropShape) outline() *gsk.Path {
	pb := gsk.NewPathBuilder()
	g.barStart(pb)
	pb.LineTo(f32(g.l-g.left-0.5), f32(g.y-0.5))
	uikit.Arc(pb, g.l-g.left-0.5, g.y+g.left-0.5, g.left, 270, 360)
	pb.LineTo(f32(g.l-0.5), f32(g.bottom-g.round))
	uikit.Arc(pb, g.l+g.round, g.bottom-g.round, g.round+0.5, 180, 90)
	pb.LineTo(f32(g.r-g.round), f32(g.bottom+0.5))
	uikit.Arc(pb, g.r-g.round, g.bottom-g.round, g.round+0.5, 90, 0)
	if g.end { // up into the bar's side, which goes on from here
		pb.LineTo(f32(g.r+0.5), f32(g.y))
		return pb.FreeToPath()
	}
	pb.LineTo(f32(g.r+0.5), f32(g.y+g.right-0.5))
	uikit.Arc(pb, g.r+g.right+0.5, g.y+g.right-0.5, g.right, 180, 270)
	pb.LineTo(f32(g.r+g.right+0.5), f32(g.y-0.5))
	g.barEnd(pb)
	return pb.FreeToPath()
}

// barStart starts at the island's lower left corner, round it; barEnd
// goes along to the lower right corner and round it.
func (g dropShape) barStart(pb *gsk.PathBuilder) {
	if g.radius < 1 { // square
		pb.MoveTo(f32(g.il), f32(g.y-0.5))
		return
	}
	pb.MoveTo(f32(g.il+0.5), f32(g.y-g.radius))
	uikit.Arc(pb, g.il+g.radius, g.y-g.radius, g.radius-0.5, 180, 90)
}

func (g dropShape) barEnd(pb *gsk.PathBuilder) {
	if g.end || g.radius < 1 { // square
		pb.LineTo(f32(g.ir), f32(g.y-0.5))
		return
	}
	pb.LineTo(f32(g.ir-g.radius), f32(g.y-0.5))
	uikit.Arc(pb, g.ir-g.radius, g.y-g.radius, g.radius-0.5, 90, 0)
}

// barEdge is the island's lower edge alone, while nothing hangs from it.
func barEdge(d *drop) *gsk.Path {
	g := dropShape{y: d.y, il: d.left, ir: d.right, radius: d.radius, end: d.end}
	pb := gsk.NewPathBuilder()
	g.barStart(pb)
	g.barEnd(pb)
	return pb.FreeToPath()
}

func f32(v float64) float32 { return float32(v) }

// hangingRect is the card's box with its lower corners rounded, for its
// shadow. As with graphene_rect_t (rect), puregotk declares
// GskRoundedRect with pointer fields; the C struct is twelve floats: the
// bounds, then each corner's width and height from the top left round.
func hangingRect(x, y, w, h, round float64) *gsk.RoundedRect {
	r := float32(round)
	v := new([12]float32)
	*v = [12]float32{float32(x), float32(y), float32(w), float32(h), 0, 0, 0, 0, r, r, r, r}
	return (*gsk.RoundedRect)(unsafe.Pointer(v))
}

// dropFromBar hangs d's card from the lower edge of the bar on mon (nil:
// the focused monitor), where the panel opens, under that bar's button,
// and drops it, in the bar's window. Without a bar the panel's own window
// shows, and the card as a card of its own.
func (s *shellUI) dropFromBar(d *drop, mon *gdk.Monitor, button func(*bar) *gtk.Widget) {
	if mon == nil {
		mon = s.focusedMonitor()
	}
	var on *bar
	if mon != nil && s.layer {
		for _, b := range s.bars {
			if b.mon.GoPointer() == mon.GoPointer() {
				on = b
			}
		}
	}
	s.hangFrom(d, on, button)
}

// dropFromThisBar hangs d's card from b's own bar, under the button it
// gives: the bar the control sits on, not the focused monitor's. A panel
// opened from one monitor's bar hangs from that bar, where its control is,
// whatever monitor has the focus by then.
func (s *shellUI) dropFromThisBar(d *drop, b *bar, button func(*bar) *gtk.Widget) {
	if !s.layer {
		b = nil
	}
	s.hangFrom(d, b, button)
}

// hangFrom is the rest of dropFromBar: the card in the bar's window, under
// its button, or the panel's own window when there is no bar to hang it in.
func (s *shellUI) hangFrom(d *drop, on *bar, button func(*bar) *gtk.Widget) {
	var r, br graphene.Rect
	if on != nil && on.island.ComputeBounds(&on.win.Widget, &r) && button(on).ComputeBounds(&on.win.Widget, &br) {
		b := *(*[4]float32)(unsafe.Pointer(&br))
		// Under the button's middle, but never past the card's own end:
		// a card wider than the room to its right stops there instead of
		// running off, and its left edge stays on the button's.
		if d.hang(s, on, *(*[4]float32)(unsafe.Pointer(&r)), float64(b[0]+b[2]/2), float64(b[0])) {
			d.open()
			return
		}
	}
	// A panel's own window only joins the application when it has to show
	// itself. A window that hangs from the bar never presents its own, and
	// GTK 4.22 segfaults destroying one that joined an application without
	// ever being realized: gtk_window_destroy -> the application's "remove"
	// -> gdk_wayland_toplevel_remove_from_session on a surface that was
	// never made.
	if d.app != nil {
		d.win.SetApplication(d.app)
	}
	d.win.Present()
}
