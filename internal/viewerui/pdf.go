package viewerui

import (
	"log"
	"math"
	"path/filepath"
	"sort"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gsk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/viewer"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// The PDF view: every page's frame one under another at the zoom, and the
// pages in view (and a little either side) drawn by poppler off the main
// thread, one at a time, at the screen's pixels. While a zoom settles the
// pages drawn are scaled; they are drawn again once it has.

const (
	pageGap    = 16        // between pages, as the layout's spacing
	pageMargin = 24        // around them
	pageTop    = 68        // above the first, clear of the tools
	pt100      = 96.0 / 72 // logical pixels per point at 100%
	pdfMinZoom = 0.1
	pdfMaxZoom = 8
)

type pdfView struct {
	a          *App
	scroller   gtk.ScrolledWindow
	box        gtk.Box
	hadj, vadj *gtk.Adjustment

	doc   *viewer.Doc
	info  viewer.PDFInfo
	gen   uintptr // bumps with each document: late pages are thrown away
	pg    []*pdfPage
	tops  []float64 // each page's top in the layout
	zoom  float64   // logical pixels per point
	fit   bool      // the widest page fills the width
	viewW int       // the width last fitted to
	cur   int       // the page in the middle of the view
	busy  bool      // a page is being drawn

	px, py       float64 // the pointer over the pages
	dragX, dragY float64
	pinchZoom    float64
	drawGen      uintptr

	onScrolled, onResized     func(gtk.Adjustment)
	onRefit, onDraw           glib.SourceFunc
	refitting                 bool
	onScroll                  func(gtk.EventControllerScroll, float64, float64) bool
	onMotion                  func(gtk.EventControllerMotion, float64, float64)
	onDragBegin, onDragUpdate func(gtk.GestureDrag, float64, float64)
	onPinchBegin              func(gtk.Gesture, uintptr)
	onPinch                   func(gtk.GestureZoom, float64)
}

// pdfPage is a page's frame, and its picture once drawn.
type pdfPage struct {
	frame *gtk.Fixed
	pic   *gtk.Picture
	w, h  float64 // in points
	tex   *gdk.Texture
	texW  int     // the texture's pixels across
	asked float64 // the screen pixels per point it was drawn for
}

func (p *pdfView) init(a *App, ui *gtkx.Builder) {
	p.a = a
	ui.Get("doc_scroller", &p.scroller)
	ui.Get("pages", &p.box)
	p.box.SetMarginTop(pageTop)
	p.box.SetMarginBottom(pageMargin)
	p.box.SetMarginStart(pageMargin)
	p.box.SetMarginEnd(pageMargin)
	p.hadj, p.vadj = p.scroller.GetHadjustment(), p.scroller.GetVadjustment()

	p.onScrolled = func(gtk.Adjustment) {
		if p.doc == nil {
			return
		}
		if c := p.middle(); c != p.cur {
			p.cur = c
			p.a.showDetails()
		}
		p.schedule()
	}
	p.vadj.ConnectValueChanged(&p.onScrolled)
	// A width fitted to follows the window, once the layout is done.
	p.onResized = func(gtk.Adjustment) {
		if p.fit && !p.refitting {
			p.refitting = true
			glib.IdleAdd(&p.onRefit, 0)
		}
	}
	p.onRefit = func(uintptr) bool {
		p.refitting = false
		if p.fit && p.doc != nil && int(p.hadj.GetPageSize()) != p.viewW {
			p.reset(true)
		}
		return false
	}
	p.hadj.ConnectChanged(&p.onResized)
	p.onDraw = func(gen uintptr) bool {
		if gen == p.drawGen {
			p.schedule()
		}
		return false
	}

	motion := gtk.NewEventControllerMotion()
	p.onMotion = func(_ gtk.EventControllerMotion, x, y float64) { p.px, p.py = x, y }
	motion.ConnectMotion(&p.onMotion)
	p.scroller.AddController(&motion.EventController)

	// The wheel scrolls; with Ctrl it zooms.
	scroll := gtk.NewEventControllerScroll(gtk.EventControllerScrollVerticalValue)
	scroll.SetPropagationPhase(gtk.PhaseCaptureValue)
	p.onScroll = func(s gtk.EventControllerScroll, _, dy float64) bool {
		if p.doc == nil || s.GetCurrentEventState()&gdk.ControlMaskValue == 0 {
			return false
		}
		if dy != 0 {
			p.zoomTo(p.zoom*wheelFactor(s, dy), p.px, p.py)
		}
		return true
	}
	scroll.ConnectScroll(&p.onScroll)
	p.scroller.AddController(&scroll.EventController)

	drag := gtk.NewGestureDrag()
	p.onDragBegin = func(gtk.GestureDrag, float64, float64) {
		p.dragX, p.dragY = p.hadj.GetValue(), p.vadj.GetValue()
	}
	p.onDragUpdate = func(_ gtk.GestureDrag, dx, dy float64) {
		p.hadj.SetValue(p.dragX - dx)
		p.vadj.SetValue(p.dragY - dy)
	}
	drag.ConnectDragBegin(&p.onDragBegin)
	drag.ConnectDragUpdate(&p.onDragUpdate)
	p.scroller.AddController(&drag.EventController)

	pinch := gtk.NewGestureZoom()
	p.onPinchBegin = func(gtk.Gesture, uintptr) { p.pinchZoom = p.zoom }
	p.onPinch = func(g gtk.GestureZoom, scale float64) {
		var x, y float64
		if !g.GetBoundingBoxCenter(&x, &y) {
			x, y = p.px, p.py
		}
		p.zoomTo(p.pinchZoom*scale, x, y)
	}
	pinch.ConnectBegin(&p.onPinchBegin)
	pinch.ConnectScaleChanged(&p.onPinch)
	p.scroller.AddController(&pinch.EventController)
}

// open reads a PDF off the main thread, then lays out its pages.
func (p *pdfView) open(path string) {
	p.close()
	gen := p.gen
	go func() {
		doc, err := viewer.OpenPDF(path)
		var info viewer.PDFInfo
		if err == nil {
			info = doc.Info()
		}
		gtkx.Post(func() {
			if gen != p.gen {
				if doc != nil {
					doc.Close()
				}
				return
			}
			if err != nil {
				p.a.failed(path, err)
				return
			}
			if doc.Pages() == 0 {
				doc.Close()
				p.a.showEmpty(i18n.Tf("%s has no pages.", filepath.Base(path)))
				return
			}
			p.doc, p.info, p.cur = doc, info, 0
			for i := range doc.Pages() {
				w, h := doc.PageSize(i)
				fr := gtk.NewFixed()
				fr.AddCssClass("viewer-page")
				fr.SetHalign(gtk.AlignCenterValue)
				pic := gtk.NewPicture()
				pic.SetCanShrink(false)
				pic.SetContentFit(gtk.ContentFitFillValue)
				fr.Put(&pic.Widget, 0, 0)
				pic.Unref() // the frame owns it, and the pages box the frame
				gtkx.Append(&p.box, &fr.Widget)
				p.pg = append(p.pg, &pdfPage{frame: fr, pic: pic, w: w, h: h})
			}
			p.reset(true)
			p.vadj.SetValue(0)
			p.a.showDetails()
		})
	}()
}

// close lets go of the document and its pages.
func (p *pdfView) close() {
	p.gen++
	for _, pg := range p.pg {
		p.forget(pg)
	}
	gtkx.Clear(&p.box)
	p.pg, p.tops = nil, nil
	if p.doc != nil {
		go p.doc.Close() // after a page being drawn
		p.doc = nil
	}
	p.info, p.cur = viewer.PDFInfo{}, 0
}

func (p *pdfView) pages() int { return len(p.pg) }

// reset fits the widest page to the width, or shows the pages at 100%.
func (p *pdfView) reset(fit bool) {
	if p.doc == nil {
		return
	}
	z := pt100
	vw := p.hadj.GetPageSize()
	if fit && vw > 0 {
		widest := 0.0
		for _, pg := range p.pg {
			widest = max(widest, pg.w)
		}
		z = (vw - 2*pageMargin) / widest
	}
	p.viewW = int(vw)
	p.zoomTo(z, p.hadj.GetPageSize()/2, 0)
	p.fit = fit
}

// step zooms in or out to the next stop.
func (p *pdfView) step(up bool) {
	if p.doc == nil {
		return
	}
	z := viewer.StepZoom(p.zoom/pt100, up) * pt100
	p.zoomTo(z, p.hadj.GetPageSize()/2, p.vadj.GetPageSize()/2)
}

// zoomTo zooms to z, keeping the point under (x, y) in the view where it
// is, and draws the pages again once the zoom settles.
func (p *pdfView) zoomTo(z, x, y float64) {
	if p.doc == nil {
		return
	}
	z = max(pdfMinZoom*pt100, min(z, pdfMaxZoom*pt100))
	// The point: a page, how far down it, and how far across the layout.
	i, fy := 0, 0.0
	if len(p.tops) > 0 {
		at := p.vadj.GetValue() + y
		i = p.pageAt(at)
		fy = (at - p.tops[i]) / math.Max(1, p.shown(p.pg[i].h))
	}
	fx := (p.hadj.GetValue() + x) / math.Max(1, p.hadj.GetUpper())
	p.zoom, p.fit = z, false
	p.layout()
	wide, high := p.size()
	place(p.hadj, wide, p.hadj.GetPageSize(), fx, x)
	vv := p.tops[i] + fy*p.shown(p.pg[i].h) - y
	vh := p.vadj.GetPageSize()
	p.vadj.Configure(math.Max(0, math.Min(vv, high-vh)), 0, math.Max(high, vh), vh/10, vh*0.9, vh)
	p.drawGen++
	glib.TimeoutAdd(150, &p.onDraw, p.drawGen)
}

// shown is a length in points as laid out.
func (p *pdfView) shown(pt float64) float64 { return math.Round(pt * p.zoom) }

// size is the layout's size.
func (p *pdfView) size() (w, h float64) {
	for _, pg := range p.pg {
		w = max(w, p.shown(pg.w))
	}
	n := len(p.pg)
	if n == 0 {
		return 0, 0
	}
	return w + 2*pageMargin, p.tops[n-1] + p.shown(p.pg[n-1].h) + pageMargin
}

// layout sizes the pages' frames for the zoom.
func (p *pdfView) layout() {
	p.tops = p.tops[:0]
	y := float64(pageTop)
	for _, pg := range p.pg {
		p.tops = append(p.tops, y)
		pg.frame.SetSizeRequest(int32(p.shown(pg.w)), int32(p.shown(pg.h)))
		p.fill(pg)
		y += p.shown(pg.h) + pageGap
	}
}

// fill scales a page's picture to its frame.
func (p *pdfView) fill(pg *pdfPage) {
	if pg.tex == nil || pg.texW == 0 {
		return
	}
	k := float32(p.shown(pg.w) / float64(pg.texW))
	t := gsk.NewTransform().Scale(k, k)
	pg.frame.SetChildTransform(&pg.pic.Widget, t)
	t.Unref()
}

// pageAt is the page at a height in the layout, or the one above the gap.
func (p *pdfView) pageAt(y float64) int {
	i := sort.Search(len(p.tops), func(i int) bool { return p.tops[i] > y }) - 1
	return max(0, min(i, len(p.tops)-1))
}

// middle is the page at the middle of the view.
func (p *pdfView) middle() int {
	return p.pageAt(p.vadj.GetValue() + p.vadj.GetPageSize()/2)
}

// goPage scrolls to a page's top.
func (p *pdfView) goPage(i int) {
	if len(p.tops) == 0 {
		return
	}
	i = max(0, min(i, len(p.tops)-1))
	p.vadj.SetValue(p.tops[i] - pageMargin/2)
}

// schedule draws the next page that wants it: the one in the middle, the
// others in view, then those beside them; and lets go of pages far away.
func (p *pdfView) schedule() {
	if p.busy || p.doc == nil || len(p.tops) == 0 {
		return
	}
	top, h := p.vadj.GetValue(), p.vadj.GetPageSize()
	first, last := p.pageAt(top), p.pageAt(top+h)
	lo, hi := max(0, first-2), min(len(p.pg)-1, last+2)
	for i, pg := range p.pg {
		if pg.tex != nil && (i < lo-2 || i > hi+2) {
			p.forget(pg)
		}
	}
	want := p.zoom * p.a.scale()
	order := []int{p.middle()}
	for i := first; i <= last; i++ {
		order = append(order, i)
	}
	for i := lo; i <= hi; i++ {
		order = append(order, i)
	}
	for _, i := range order {
		if pg := p.pg[i]; pg.tex == nil || math.Abs(pg.asked-want) > want*0.01 {
			p.draw(i, want)
			return
		}
	}
}

// draw draws a page off the main thread, then the next.
func (p *pdfView) draw(i int, want float64) {
	p.busy = true
	gen, doc := p.gen, p.doc
	go func() {
		px, _, err := doc.Render(i, want, cAlloc{})
		var tex *gdk.Texture
		if err == nil {
			tex = texture(px)
		}
		gtkx.Post(func() {
			p.busy = false
			if gen != p.gen {
				if tex != nil {
					tex.Unref()
				}
				p.schedule()
				return
			}
			pg := p.pg[i]
			if err != nil {
				log.Printf("viewer: page %d: %v", i+1, err)
				pg.asked = want // not again at this zoom
				p.schedule()
				return
			}
			if pg.tex != nil {
				pg.tex.Unref()
			}
			pg.tex, pg.texW, pg.asked = tex, px.W, want
			pg.pic.SetPaintable(tex)
			p.fill(pg)
			p.schedule()
		})
	}()
}

// forget lets go of a page's picture.
func (p *pdfView) forget(pg *pdfPage) {
	if pg.tex == nil {
		return
	}
	noPaintable(pg.pic)
	pg.tex.Unref()
	pg.tex, pg.texW, pg.asked = nil, 0, 0
}

// key takes the keys that move through a PDF: arrows and Page Up and
// Down by page, Space by a screen, Home and End to its ends.
func (p *pdfView) key(keyval uint32, shift bool) bool {
	if p.doc == nil {
		return false
	}
	line := 48.0
	screen := p.vadj.GetPageSize() * 0.9
	switch {
	case keyval == keyUp:
		p.vadj.SetValue(p.vadj.GetValue() - line)
	case keyval == keyDown:
		p.vadj.SetValue(p.vadj.GetValue() + line)
	case keyval == keyLeft || keyval == keyPageUp:
		p.goPage(p.cur - 1)
	case keyval == keyRight || keyval == keyPageDown:
		p.goPage(p.cur + 1)
	case keyval == ' ' && shift:
		p.vadj.SetValue(p.vadj.GetValue() - screen)
	case keyval == ' ':
		p.vadj.SetValue(p.vadj.GetValue() + screen)
	case keyval == keyHome:
		p.goPage(0)
	case keyval == keyEnd:
		p.goPage(len(p.pg) - 1)
	default:
		return false
	}
	return true
}
