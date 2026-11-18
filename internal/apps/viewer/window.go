package viewer

import "math"

// The window takes the image's shape: an image fills it edge to edge,
// with nothing round it, unless it is far wider or taller than a window
// should be.

// Rect is a window's or a screen's place, in logical pixels.
type Rect struct{ X, Y, W, H int }

const (
	// WindowShare is how much of the screen, each way, the window may
	// take at most.
	WindowShare = 0.86
	// MaxWidth and MaxHeight are the window's largest size, however large
	// the screen: a 3:2 window, the size of a photo looked at.
	MaxWidth, MaxHeight = 1440, 960
	// WindowMinSide is the window's shortest side, which leaves room for
	// the toolbars: a small image grows to it.
	WindowMinSide = 360
	// MaxAspect is the widest (and, turned, the tallest) shape the
	// window takes; a wider image is fitted whole inside a window of
	// this shape, the rest left clear.
	MaxAspect = 2.4
	// coverSlack is how much a picture may overflow the window to fill
	// it: as much as the window's rounding to whole pixels leaves bare,
	// or a window made a little narrower by hand.
	coverSlack = 1.02
)

// WindowFor is the window for a w×h image (logical pixels at its actual
// size), on screen area, centred where cur is centred and kept on the
// screen: the image at its actual size, made smaller to fit the screen
// and MaxWidth×MaxHeight or larger to the shortest side, in its own shape, or MaxAspect's when
// it is wider or taller than that.
func WindowFor(w, h float64, cur, area Rect) Rect {
	if w <= 0 || h <= 0 || area.W <= 0 || area.H <= 0 {
		return cur
	}
	maxW := min(float64(area.W)*WindowShare, MaxWidth)
	maxH := min(float64(area.H)*WindowShare, MaxHeight)
	// The window's shape: the image's, within MaxAspect.
	aspect := max(1/MaxAspect, min(w/h, MaxAspect))
	// The image fitted to the screen, never above its actual size; the
	// window as wide as it (as tall, for a tall one), in its shape.
	z := min(1, maxW/w, maxH/h)
	iw, ih := w*z, h*z
	ww, wh := iw, iw/aspect
	if w/h < aspect {
		ww, wh = ih*aspect, ih
	}
	// A small one grows to the shortest side, as far as the screen lets.
	if s := min(ww, wh); s < WindowMinSide {
		g := min(WindowMinSide/s, maxW/ww, maxH/wh)
		ww, wh = ww*g, wh*g
	}
	r := Rect{W: int(math.Round(ww)), H: int(math.Round(wh))}
	r.W, r.H = min(r.W, area.W), min(r.H, area.H)
	cx, cy := cur.X+cur.W/2, cur.Y+cur.H/2
	if cur.W <= 0 || cur.H <= 0 {
		cx, cy = area.X+area.W/2, area.Y+area.H/2
	}
	r.X = max(area.X, min(cx-r.W/2, area.X+area.W-r.W))
	r.Y = max(area.Y, min(cy-r.H/2, area.Y+area.H-r.H))
	return r
}

// FillZoom is the zoom that fills a view of vw×vh screen pixels with a
// w×h image: whole, as large as it fits, a small image too; or covering
// the view when that cuts off no more than coverSlack, so a window of
// almost the image's shape shows no bare edge.
func FillZoom(w, h, vw, vh float64) float64 {
	if w <= 0 || h <= 0 || vw <= 0 || vh <= 0 {
		return 1
	}
	contain, cover := min(vw/w, vh/h), max(vw/w, vh/h)
	if cover <= contain*coverSlack {
		return ClampZoom(cover)
	}
	return ClampZoom(contain)
}
