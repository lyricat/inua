package uikit

import (
	"fmt"
	"math"
	"sync"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gsk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
)

// App icons share one shape, the setting's (IconShape). The default is a
// square with continuous corners, whose curve eases out of the sides
// instead of meeting them as a circle's arc does (Apple's icons; Figma's
// "corner smoothing").
const (
	iconCorner = 0.2237 // the corners' radius
	iconSmooth = 0.6    // how far they ease into the sides
	iconPlate  = 0.78   // an icon of another shape, on a plate: its size
)

// IconShape is how an app icon is drawn in the dock, its drawer and the
// launcher.
type IconShape int

const (
	// IconShapeSquircle is the continuous-corner square.
	IconShapeSquircle IconShape = iota
	// IconShapeCircle is a circle filling the icon's square.
	IconShapeCircle
	// IconShapePebble is a soft, slightly uneven round square.
	IconShapePebble
	// IconShapeNone draws the icon as it is: no shape, plate or light.
	IconShapeNone
)

// IconShapeFromName reads a shape's name from the settings
// (config.IconShapes): "squircle", "circle", "pebble" or "none"; any other
// is the squircle.
func IconShapeFromName(name string) IconShape {
	switch name {
	case "circle":
		return IconShapeCircle
	case "pebble":
		return IconShapePebble
	case "none":
		return IconShapeNone
	default:
		return IconShapeSquircle
	}
}

// SetIconShape changes the shape every app icon is drawn in, the icons
// shown now and those made after; on GTK's thread.
func SetIconShape(shape IconShape) {
	appIcons.mu.Lock()
	defer appIcons.mu.Unlock()
	if shape == appIcons.shape {
		return
	}
	appIcons.shape = shape
	for _, a := range appIcons.by {
		a.shape = shape
		// The outline is kept for the icon's square; the new one is built
		// at the next draw, asked for now rather than when the icon moves.
		if a.path != nil {
			a.path.Unref()
			a.path = nil
		}
		a.canvas.QueueDraw()
	}
}

// An app icon's light, as fractions of the icon's side. Light from above
// catches its top edge as a fine bright line with a soft glow inside it,
// and its bottom edge as a fainter one; each thins out round the corners
// and fades along the sides. The face shades a little from top to bottom,
// and the icon casts a soft shadow below it.
const (
	iconEdgeLine = 0.008 // the bright line along an edge
	iconEdgeGlow = 0.03  // the glow inside it
	iconEdgeFade = 0.35  // how far along the sides an edge's light reaches
	iconEdgeFull = 128   // the side, in pixels, at which the edges' light is in full
	iconDrop     = 0.04  // how far the shadow falls below the icon
	iconDropBlur = 0.06  // the shadow's blur
)

// appIcon is one icon in its light: the image it holds, the canvas that
// draws them, how the image's art fits the shape, and the shape.
type appIcon struct {
	canvas *gtk.Widget
	img    *gtk.Image

	art    any // what the image shows, when fit was measured
	fit    artFit
	shape  IconShape
	path   *gsk.Path
	pathAt [3]float32 // its square: x, y, side
}

// artFit is where an icon's art lies in its square, in fractions of the
// side: the box its opaque pixels fill, and whether it fills the shape
// there (a square or a tile) or is another shape, which goes on a plate.
type artFit struct {
	x, y, w, h float32
	plate      bool
}

var appIcons struct {
	mu    sync.Mutex
	by    map[uintptr]*appIcon
	shape IconShape // SetIconShape's
}

// AppIcon gives an app's icon the light of the dock and the launcher: a
// fine bright line along its top edge and a fainter one along its bottom,
// cut from the icon's own outline (its alpha against itself moved a pixel
// or two) so they fit any shape, a tile's arcs or a round icon alike, and
// a soft shadow below it, in the shape SetIconShape gives (IconShapeNone
// draws the image alone, without them). The icon's file is drawn as it
// is; the image keeps its own style (classes, CSS transforms and
// animations), and the light follows it.
//
// The image becomes the child of the widget returned, which takes its
// alignment, expansion, margins and visibility. With box, the image's
// parent (an icon in a layout), the widget takes the image's place there
// and the box owns it. With nil, AppIcon takes over the caller's reference
// to the new image, and the caller owns the widget's, as with a new
// widget (gtkx.Append): it puts the widget where the image would go.
func AppIcon(img *gtk.Image, box *gtk.Box) *gtk.Widget {
	a := &appIcon{img: img}
	a.canvas = gtkx.NewCanvas(&gtkx.Canvas{Snapshot: a.snapshot, Allocate: a.allocate, Measure: a.measure})
	(&gobject.Object{Ptr: a.canvas.GoPointer()}).RefSink() // a strong reference, as puregotk's constructors give
	a.canvas.AddCssClass("app-icon")
	appIcons.mu.Lock()
	a.shape = appIcons.shape
	if appIcons.by == nil {
		appIcons.by = map[uintptr]*appIcon{}
	}
	appIcons.by[a.canvas.GoPointer()] = a
	appIcons.mu.Unlock()
	a.canvas.ConnectDestroy(&onAppIconDestroy)

	w := &img.Widget
	c := a.canvas
	c.SetHalign(w.GetHalign())
	c.SetValign(w.GetValign())
	c.SetHexpand(w.GetHexpand())
	c.SetVexpand(w.GetVexpand())
	c.SetMarginTop(w.GetMarginTop())
	c.SetMarginBottom(w.GetMarginBottom())
	c.SetMarginStart(w.GetMarginStart())
	c.SetMarginEnd(w.GetMarginEnd())
	c.SetVisible(w.GetVisible())
	w.SetHalign(gtk.AlignFillValue)
	w.SetValign(gtk.AlignFillValue)
	w.SetMarginTop(0)
	w.SetMarginBottom(0)
	w.SetMarginStart(0)
	w.SetMarginEnd(0)
	w.SetVisible(true)

	if box != nil {
		w.Ref() // across its move
		prev := w.GetPrevSibling()
		box.Remove(w)
		box.InsertChildAfter(c, prev)
		c.Unref() // the box holds it
	}
	w.SetParent(c)
	w.Unref() // the canvas holds it
	return c
}

// onAppIconDestroy lets an icon's image go with it.
var onAppIconDestroy = func(w gtk.Widget) {
	appIcons.mu.Lock()
	a := appIcons.by[w.GoPointer()]
	delete(appIcons.by, w.GoPointer())
	appIcons.mu.Unlock()
	if a == nil {
		return
	}
	if a.img.GetParent() != nil {
		a.img.Unparent()
	}
	if a.path != nil {
		a.path.Unref()
		a.path = nil
	}
	gtkx.ForgetCanvas(a.canvas)
}

func (a *appIcon) measure(_ *gtk.Widget, o gtk.Orientation, forSize int) (int, int) {
	var m, n int32
	a.img.Measure(o, int32(forSize), &m, &n, nil, nil)
	return int(m), int(n)
}

func (a *appIcon) allocate(_ *gtk.Widget, width, height int) {
	a.img.Allocate(int32(width), int32(height), -1, nil)
}

func (a *appIcon) snapshot(w *gtk.Widget, s *gtk.Snapshot) {
	img := &a.img.Widget
	icon := func() { w.SnapshotChild(img, s) }
	// The icon's square where the image draws it: centred in the image, at
	// its pixel size, moved and scaled as the image is (the dock's hover).
	var b graphene.Rect
	if a.img.GetStorageType() == gtk.ImageEmptyValue || !img.ComputeBounds(w, &b) {
		icon()
		return
	}
	bx, by, bw, bh := rectOf(&b)
	side := min(bw, bh)
	if px, h := a.img.GetPixelSize(), img.GetHeight(); px > 0 && h > 0 {
		side = min(side, float32(px)*bh/float32(h))
	}
	if side <= 0 {
		icon()
		return
	}
	x, y := bx+(bw-side)/2, by+(bh-side)/2
	var square graphene.Rect
	square.Init(x, y, side, side)
	if a.shape == IconShapeNone {
		icon()
		return
	}
	if art := a.showing(); art != a.art {
		if fit, ok := a.measureArt(w, &square); ok {
			a.art, a.fit = art, fit
		} else {
			icon()
			return
		}
	}
	if a.path == nil || a.pathAt != [3]float32{x, y, side} {
		if a.path != nil {
			a.path.Unref()
		}
		a.path, a.pathAt = gsk.PathParse(iconPath(a.shape, x, y, side)), [3]float32{x, y, side}
	}
	path := a.path
	shape := func() { s.AppendFill(path, gsk.FillRuleWindingValue, &gdk.RGBA{Red: 1, Green: 1, Blue: 1, Alpha: 1}) }

	// The art in the shape, casting its shadow. Art that fills its box is
	// stretched a hair past the shape so its own edge falls outside; art
	// of another shape sits on a plate.
	drop := []shadow{{a: 0.32, dy: max(1, side*iconDrop), radius: max(2, side*iconDropBlur)}}
	s.PushShadow(unsafe.Slice((*gsk.Shadow)(unsafe.Pointer(&drop[0])), 1), 1)
	s.PushFill(path, gsk.FillRuleWindingValue)
	f := a.fit
	scale := 1.02 / max(f.w, f.h)
	if f.plate {
		vertical(s, &square, y, y+side, []colorStop{{0, 0.98, 0.98, 0.99, 1}, {1, 0.89, 0.9, 0.92, 1}})
		scale = min(iconPlate/max(f.w, f.h), 2)
	}
	s.Save()
	s.Translate(&graphene.Point{X: x + side/2, Y: y + side/2})
	s.Scale(scale, scale)
	s.Translate(&graphene.Point{X: -(x + (f.x+f.w/2)*side), Y: -(y + (f.y+f.h/2)*side)})
	icon()
	s.Restore()
	s.Pop()
	s.Pop()

	// The face, a little lighter at the top and darker at the bottom.
	s.PushMask(gsk.MaskModeAlphaValue)
	shape()
	s.Pop()
	vertical(s, &square, y, y+side, []colorStop{
		{0, 1, 1, 1, 0.06}, {0.4, 1, 1, 1, 0}, {0.55, 0, 0, 0, 0}, {1, 0, 0, 0, 0.07},
	})
	s.Pop()

	// The edges' light: the shape where it is not when moved by d, a band
	// along the edges facing that way, coloured down (or up) the side.
	rim := func(d float32, stops []colorStop, from, to float32) {
		s.PushMask(gsk.MaskModeAlphaValue)
		shape()
		s.Pop()
		s.PushMask(gsk.MaskModeInvertedAlphaValue)
		s.Save()
		s.Translate(&graphene.Point{Y: d})
		shape()
		s.Restore()
		s.Pop()
		vertical(s, &square, from, to, stops)
		s.Pop()
		s.Pop()
	}
	// The light grows with the icon: in full at iconEdgeFull, half as
	// bright on the launcher's 64 px and fainter still on a small dock.
	k := min(1, side/iconEdgeFull)
	white := func(a0, a1 float32) []colorStop {
		return []colorStop{{0, 1, 1, 1, a0 * k}, {0.4, 1, 1, 1, a1 * k}, {1, 1, 1, 1, 0}}
	}
	line, glow := max(0.5, side*iconEdgeLine), max(1, side*iconEdgeGlow)
	top, bottom := y+side*iconEdgeFade, y+side*(1-iconEdgeFade)
	rim(glow, white(0.22, 0.08), y, top)
	rim(line, white(0.7, 0.3), y, top)
	rim(-glow, white(0.1, 0.04), y+side, bottom)
	rim(-line, white(0.35, 0.14), y+side, bottom)
}

// showing names what the image shows: its icon's name, or its icon or
// paintable object.
func (a *appIcon) showing() any {
	switch a.img.GetStorageType() {
	case gtk.ImageIconNameValue:
		return a.img.GetIconName()
	case gtk.ImageGiconValue:
		if g := a.img.GetGicon(); g != nil {
			return g.GoPointer()
		}
	case gtk.ImagePaintableValue:
		if p := a.img.GetPaintable(); p != nil {
			return p.GoPointer()
		}
	}
	return nil
}

// artRenderer draws an icon's art off screen to measure it.
var artRenderer struct {
	once sync.Once
	r    *gsk.Renderer
}

// measureArt finds how the image's art fits the shape, from its pixels in
// square; false when it cannot be drawn yet.
func (a *appIcon) measureArt(w *gtk.Widget, square *graphene.Rect) (artFit, bool) {
	artRenderer.once.Do(func() {
		r := gsk.NewCairoRenderer()
		if ok, _ := r.RealizeForDisplay(gdk.DisplayGetDefault()); ok {
			artRenderer.r = &r.Renderer
		}
	})
	if artRenderer.r == nil {
		return artFit{}, false
	}
	snap := gtk.NewSnapshot()
	w.SnapshotChild(&a.img.Widget, snap)
	node := snap.FreeToNode()
	if node == nil {
		return artFit{}, false
	}
	defer node.Unref()
	tex := artRenderer.r.RenderTexture(node, square)
	if tex == nil {
		return artFit{}, false
	}
	defer tex.Unref()
	n, m := int(tex.GetWidth()), int(tex.GetHeight())
	if n < 8 || m < 8 {
		return artFit{}, false
	}
	px := make([]byte, n*m*4)
	tex.Download(px, uint(n*4))
	return fitArt(n, m, func(i, j int) float32 { return float32(px[(j*n+i)*4+3]) / 255 }), true
}

// fitArt measures art n by m pixels from its alpha: the box of its opaque
// pixels, and whether the art fills the shape there.
func fitArt(n, m int, alpha func(i, j int) float32) artFit {
	x0, y0, x1, y1 := n, m, -1, -1
	for j := range m {
		for i := range n {
			if alpha(i, j) > 0.5 {
				x0, y0, x1, y1 = min(x0, i), min(y0, j), max(x1, i), max(y1, j)
			}
		}
	}
	if x1 < 0 {
		return artFit{0, 0, 1, 1, true}
	}
	f := artFit{
		x: float32(x0) / float32(n), y: float32(y0) / float32(m),
		w: float32(x1-x0+1) / float32(n), h: float32(y1-y0+1) / float32(m),
	}
	// A square or a tile: its box nearly square, most of the icon, and
	// opaque all round just inside the shape, its corners too.
	f.plate = max(f.w, f.h) < 0.7 || math.Abs(float64(f.w-f.h)) > 0.06
	at := func(u, v float32) float32 {
		i := int((f.x + u*f.w) * float32(n))
		j := int((f.y + v*f.h) * float32(m))
		return alpha(min(max(i, 0), n-1), min(max(j, 0), m-1))
	}
	const in, corner = 0.06, 0.125
	for _, t := range []float32{0.2, 0.5, 0.8} {
		for _, uv := range [][2]float32{{t, in}, {t, 1 - in}, {in, t}, {1 - in, t}} {
			if at(uv[0], uv[1]) < 0.8 {
				f.plate = true
			}
		}
	}
	for _, uv := range [][2]float32{{corner, corner}, {1 - corner, corner}, {corner, 1 - corner}, {1 - corner, 1 - corner}} {
		if at(uv[0], uv[1]) < 0.8 {
			f.plate = true
		}
	}
	return f
}

// iconPath is the shape's outline in the square at x, y, side long.
func iconPath(shape IconShape, x, y, side float32) string {
	switch shape {
	case IconShapeCircle:
		r := float64(side) / 2
		cx, cy := float64(x)+r, float64(y)+r
		return fmt.Sprintf("M %g %g a %g %g 0 1 0 %g 0 a %g %g 0 1 0 %g 0 Z",
			cx-r, cy, r, r, 2*r, r, r, -2*r)
	case IconShapePebble:
		return pebble(x, y, side)
	}
	return squircle(x, y, side)
}

// pebble is a round square with corners of different radii, as a stone
// worn smooth: straight sides and plain arcs, without the squircle's
// easing.
func pebble(x, y, side float32) string {
	S := float64(side)
	r := [4]float64{S * 0.27, S * 0.43, S * 0.31, S * 0.45} // top left, top right, bottom right, bottom left
	X, Y := float64(x), float64(y)
	return fmt.Sprintf("M %g %g "+
		"L %g %g A %g %g 0 0 1 %g %g "+
		"L %g %g A %g %g 0 0 1 %g %g "+
		"L %g %g A %g %g 0 0 1 %g %g "+
		"L %g %g A %g %g 0 0 1 %g %g Z",
		X+r[0], Y,
		X+S-r[1], Y, r[1], r[1], X+S, Y+r[1],
		X+S, Y+S-r[2], r[2], r[2], X+S-r[2], Y+S,
		X+r[3], Y+S, r[3], r[3], X, Y+S-r[3],
		X, Y+r[0], r[0], r[0], X+r[0], Y)
}

// squircle is the icon's shape in the square at x, y, side long, as an
// SVG path: four corners of radius iconCorner smoothed by iconSmooth, as
// Figma draws them (figma-squircle's getPathParamsForCorner).
func squircle(x, y, side float32) string {
	r := float64(side) * iconCorner
	sm := iconSmooth
	p := min((1+sm)*r, float64(side)/2)
	rad := func(deg float64) float64 { return deg * math.Pi / 180 }
	arcMeasure := 90 * (1 - sm)
	arc := math.Sin(rad(arcMeasure/2)) * r * math.Sqrt2
	alpha := (90 - arcMeasure) / 2
	p3p4 := r * math.Tan(rad(alpha/2))
	beta := 45 * sm
	c := p3p4 * math.Cos(rad(beta))
	d := c * math.Tan(rad(beta))
	b := (p - arc - c - d) / 3
	a := 2 * b
	S := float64(side)
	e := S - 2*p
	return fmt.Sprintf("M %g %g "+
		"c %g 0 %g 0 %g %g a %g %g 0 0 1 %g %g c %g %g %g %g %g %g "+
		"l 0 %g "+
		"c 0 %g 0 %g %g %g a %g %g 0 0 1 %g %g c %g %g %g %g %g %g "+
		"l %g 0 "+
		"c %g 0 %g 0 %g %g a %g %g 0 0 1 %g %g c %g %g %g %g %g %g "+
		"l 0 %g "+
		"c 0 %g 0 %g %g %g a %g %g 0 0 1 %g %g c %g %g %g %g %g %g Z",
		float64(x)+S-p, float64(y),
		a, a+b, a+b+c, d, r, r, arc, arc, d, c, d, b+c, d, a+b+c,
		e,
		a, a+b, -d, a+b+c, r, r, -arc, arc, -c, d, -(b + c), d, -(a + b + c), d,
		-e,
		-a, -(a + b), -(a + b + c), -d, r, r, -arc, -arc, -d, -c, -d, -(b + c), -d, -(a + b + c),
		-e,
		-a, -(a + b), d, -(a + b + c), r, r, arc, -arc, c, -d, b+c, -d, a+b+c, -d)
}

// colorStop is GSK's colour stop as GSK lays it out, an offset and the
// colour inline (puregotk's gsk.ColorStop holds a pointer instead).
type colorStop struct{ offset, r, g, b, a float32 }

// shadow is GSK's shadow as GSK lays it out, the colour inline (as with
// colorStop) and its offset and blur.
type shadow struct{ r, g, b, a, dx, dy, radius float32 }

// vertical fills bounds with a linear gradient from y0 to y1.
func vertical(s *gtk.Snapshot, bounds *graphene.Rect, y0, y1 float32, stops []colorStop) {
	x, _, _, _ := rectOf(bounds)
	gsStops := unsafe.Slice((*gsk.ColorStop)(unsafe.Pointer(&stops[0])), len(stops))
	s.AppendLinearGradient(bounds, &graphene.Point{X: x, Y: y0}, &graphene.Point{X: x, Y: y1}, gsStops, uint(len(stops)))
}

// rectOf is a graphene rectangle's origin and size (puregotk's
// graphene.Rect holds them as opaque words).
func rectOf(r *graphene.Rect) (x, y, w, h float32) {
	v := (*[4]float32)(unsafe.Pointer(r))
	return v[0], v[1], v[2], v[3]
}
