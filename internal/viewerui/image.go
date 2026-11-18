package viewerui

import (
	"math"
	"path/filepath"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gsk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/viewer"
	"github.com/lyricat/inua/internal/i18n"
)

// The image view: the picture at its own size on a canvas, scaled, turned
// and mirrored as a whole by a transform.

// showImage shows an image: at once if it is loaded, else as soon as a
// first look at it is, and then the images beside it load.
func (a *App) showImage(path string) {
	a.gen++
	if a.iter != nil {
		a.iter.Unref()
		a.iter = nil
	}
	s := a.shotFor(path)
	a.cur, a.prefetched = s, false
	a.turns, a.spin, a.fit, a.texW, a.texH, a.w, a.h = 0, 0, true, 0, 0, 0, 0
	a.stopMotion()
	a.keep(a.neighbours()...)
	if s.tex == nil && s.anim == nil {
		noPaintable(&a.picture)
	}
	a.display()
	a.load(s)
}

// neighbours are the shown image and the images beside it.
func (a *App) neighbours() []string {
	n := len(a.files)
	if n == 0 {
		return nil
	}
	near := []string{a.files[a.index]}
	for _, p := range []string{a.files[(a.index+1)%n], a.files[(a.index-1+n)%n]} {
		if viewer.IsImage(p) {
			near = append(near, p)
		}
	}
	return near
}

// shotChanged: a load has come further.
func (a *App) shotChanged(s *shot) {
	if s == a.cur {
		a.display()
	}
}

// display draws the shown image as far as it has loaded.
func (a *App) display() {
	s := a.cur
	if s == nil {
		return
	}
	switch {
	case s.anim != nil:
		a.empty.SetVisible(false)
		if a.iter == nil {
			a.iter = s.anim.GetIter(nil)
			a.showFrame()
		}
	case s.tex != nil:
		a.empty.SetVisible(false)
		a.picture.SetPaintable(s.tex)
		a.texW, a.texH = s.texW, s.texH
		a.geometry()
	case s.err != nil:
		a.showEmpty(i18n.Tf("Could not open %s.\n%v", filepath.Base(s.path), s.err))
	}
	a.setClear()
	a.showDetails()
	if (s.tex != nil || s.anim != nil) && !a.prefetched {
		a.prefetched = true
		for _, p := range a.neighbours()[1:] {
			a.load(a.shotFor(p))
		}
	}
}

// clearImage stops drawing an image.
func (a *App) clearImage() {
	noPaintable(&a.picture)
	if a.iter != nil {
		a.iter.Unref()
		a.iter = nil
	}
	a.cur = nil
	a.texW, a.texH, a.w, a.h = 0, 0, 0, 0
}

// showFrame shows the animation's frame and waits for the next.
func (a *App) showFrame() {
	pb := a.iter.GetPixbuf()
	if pb == nil {
		return
	}
	tex := gdk.NewTextureForPixbuf(pb)
	a.picture.SetPaintable(tex)
	tex.Unref()
	if w, h := int(pb.GetWidth()), int(pb.GetHeight()); w != a.texW || h != a.texH {
		a.texW, a.texH = w, h
		a.geometry()
	}
	if d := a.iter.GetDelayTime(); d >= 0 {
		glib.TimeoutAdd(uint32(max(d, 20)), &a.onFrame, a.gen)
	}
}

// orientation is how the shown image is drawn: mirrored or not, then
// turned by its EXIF orientation and the turns asked for.
func (a *App) orientation() (turns int, mirror bool) {
	if a.cur == nil {
		return 0, false
	}
	t, mirror := viewer.Orientation(a.cur.orient)
	return (t + a.turns) % 4, mirror
}

// geometry follows the shown image's size as drawn.
func (a *App) geometry() {
	s := a.cur
	if s == nil || s.fullW == 0 || a.texW == 0 {
		return
	}
	turns, _ := a.orientation()
	a.w, a.h = s.fullW, s.fullH
	if turns%2 == 1 {
		a.w, a.h = a.h, a.w
	}
	if a.fit {
		a.fitWindow()
	} else {
		a.applyZoom()
	}
	a.shapeWindow()
}

// turn rotates the image by a quarter turn clockwise (q 1) or the other
// way (q 3): only the transform changes, eased.
func (a *App) turn(q int) {
	if a.w == 0 {
		return
	}
	a.animated(func() {
		if q%4 == 3 {
			a.spin--
		} else {
			a.spin++
		}
		a.turns = (a.spin%4 + 4) % 4
		a.geometry()
	})
}

// fitWindow fills the window with the image, a small one too (FillZoom).
func (a *App) fitWindow() {
	if a.w == 0 {
		return
	}
	s := a.scale()
	vw, vh := a.view()
	a.viewW, a.viewH = int(vw), int(vh)
	a.fit = true
	if vw <= 0 || vh <= 0 {
		// Not laid out yet: show it small (never larger than the screen),
		// and fit it once the stage has a size (refit).
		a.zoom = viewer.FitZoom(float64(a.w), float64(a.h), 800*s, 600*s)
	} else {
		a.zoom = viewer.FillZoom(float64(a.w), float64(a.h), vw*s, vh*s)
	}
	a.applyZoom()
}

// view is the stage's size: the scroll range's page, which is right
// already while the window is being resized.
func (a *App) view() (float64, float64) {
	return a.hadj.GetPageSize(), a.vadj.GetPageSize()
}

// refit follows the stage's size while the image fits it.
func (a *App) refit() {
	if !a.fit || a.w == 0 {
		return
	}
	if vw, vh := a.view(); int(vw) != a.viewW || int(vh) != a.viewH {
		a.fitWindow()
	}
}

// shownSize is the image's size on the stage, in logical pixels.
func (a *App) shownSize(z float64) (float64, float64) {
	s := a.scale()
	return math.Max(1, math.Round(float64(a.w)*z/s)), math.Max(1, math.Round(float64(a.h)*z/s))
}

// applyZoom scales the picture as a whole, which keeps its shape, and
// mirrors and turns it as it should stand. A smaller preview is scaled up
// to the image's size.
func (a *App) applyZoom() {
	s := a.cur
	if s == nil || a.texW == 0 || s.fullW == 0 {
		return
	}
	turns, mirror := a.orientation()
	k := a.zoom / a.scale() * float64(s.fullW) / float64(a.texW)
	dx, dy := viewer.Placement(float64(a.texW), float64(a.texH), k, turns, mirror)
	fx := k
	if mirror {
		fx = -k
	}
	t := gsk.NewTransform().Translate(&graphene.Point{X: float32(dx), Y: float32(dy)}).
		Rotate(float32(90*turns)).Scale(float32(fx), float32(k))
	a.canvas.SetChildTransform(&a.picture.Widget, t)
	t.Unref()
	a.canvas.QueueResize() // its size is the scaled picture's
}

// zoomTo zooms to z, keeping the image's point under (x, y) on the stage
// where it is.
func (a *App) zoomTo(z float64, x, y float64) {
	if a.w == 0 {
		return
	}
	z = viewer.ClampZoom(z)
	vw, vh := a.view()
	w0, h0 := a.shownSize(a.zoom)
	// The point, as a share of the image; the image is centred while it is
	// smaller than the stage.
	fx := (a.hadj.GetValue() + x - math.Max(0, (vw-w0)/2)) / w0
	fy := (a.vadj.GetValue() + y - math.Max(0, (vh-h0)/2)) / h0
	fx, fy = math.Max(0, math.Min(fx, 1)), math.Max(0, math.Min(fy, 1))
	a.fit, a.zoom = false, z
	a.applyZoom()
	w1, h1 := a.shownSize(z)
	// Set the scroll range now, so the position holds through the layout.
	place(a.hadj, w1, vw, fx, x)
	place(a.vadj, h1, vh, fy, y)
}

// place sets a scroll range for content of a size, so that the point at
// share f of it is at `at` in the view.
func place(adj *gtk.Adjustment, content, view, f, at float64) {
	upper := math.Max(content, view)
	v := math.Max(0, math.Min(f*content-at+math.Max(0, (view-content)/2), upper-view))
	adj.Configure(v, 0, upper, view/10, view*0.9, view)
}

// toggleActual switches between fitting the window and actual size.
func (a *App) toggleActual(x, y float64) {
	if a.fit && a.zoom != 1 {
		a.zoomTo(1, x, y)
	} else {
		a.fitWindow()
	}
}
