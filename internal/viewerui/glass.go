package viewerui

import (
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/uikit"
)

// The glass under the tools floating over the file: the stage below them,
// blurred and cut to their corners, under their tint and hairline (CSS's
// .viewer-glass). The compositor blurs only what is behind the window,
// so the glass draws the stage itself, from a paintable of it.

const (
	// glassRadius is .viewer-glass's border-radius: an app icon's corners
	// (22.37% of the side) on a bar 44 px tall.
	glassRadius = 10
	// glassBlur is the blur's radius, twice its standard deviation.
	glassBlur = 36
)

// glassUnder puts the glass under box, a .viewer-glass alone in its
// parent box: a canvas takes its place and holds it.
func (a *App) glassUnder(box *gtk.Box) {
	if a.stagePaint == nil {
		a.stagePaint = gtk.NewWidgetPaintable(&a.stage.Widget)
		// The stage drew anew: so does the glass over it.
		a.onStageDrawn = func(uintptr, uintptr) {
			for _, g := range a.glasses {
				g.QueueDraw()
			}
		}
		gobject.SignalConnect(a.stagePaint.GoPointer(), "invalidate-contents", glib.NewCallback(&a.onStageDrawn))
		a.onGlassGone = func(w gtk.Widget) {
			if c := w.GetFirstChild(); c != nil {
				c.Unparent()
			}
			gtkx.ForgetCanvas(&w)
		}
	}
	child := &box.Widget
	parent := gtk.Box{}
	child.GetParent().Cast(&parent)
	var canvas *gtk.Widget
	canvas = gtkx.NewCanvas(&gtkx.Canvas{
		Measure: func(_ *gtk.Widget, o gtk.Orientation, forSize int) (int, int) {
			var m, n int32
			child.Measure(o, int32(forSize), &m, &n, nil, nil)
			return int(m), int(n)
		},
		Allocate: func(_ *gtk.Widget, w, h int) { child.Allocate(int32(w), int32(h), -1, nil) },
		Snapshot: func(w *gtk.Widget, s *gtk.Snapshot) { a.drawGlass(w, child, s) },
	})
	canvas.ConnectDestroy(&a.onGlassGone)
	(&gobject.Object{Ptr: child.GoPointer()}).Ref()
	parent.Remove(child)
	child.SetParent(canvas)
	(&gobject.Object{Ptr: child.GoPointer()}).Unref() // the canvas holds it
	parent.Append(canvas)
	a.glasses = append(a.glasses, canvas)
}

// drawGlass draws the stage under the canvas, blurred and cut to the
// glass's corners, then the tools on it.
func (a *App) drawGlass(w, child *gtk.Widget, s *gtk.Snapshot) {
	width, height := float64(w.GetWidth()), float64(w.GetHeight())
	var at graphene.Rect
	if width > 0 && height > 0 && a.stage.ComputeBounds(w, &at) {
		b := *(*[4]float32)(unsafe.Pointer(&at)) // x, y, width, height
		s.PushRoundedClip(uikit.RoundedRect(0, 0, width, height, glassRadius))
		s.PushBlur(glassBlur)
		s.Save()
		s.Translate(&graphene.Point{X: b[0], Y: b[1]})
		a.stagePaint.Snapshot(&s.Snapshot, float64(b[2]), float64(b[3]))
		s.Restore()
		s.Pop()
		s.Pop()
	}
	w.SnapshotChild(child, s)
}
