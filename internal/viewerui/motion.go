package viewerui

import (
	"math"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/viewer"
	"github.com/lyricat/inua/internal/gtkx"
)

// The picture eased into a new zoom or a new turn. The change itself is
// made at once, as without the motion; for motionTime a layer over the
// scroller draws the picture on its way there instead, from where it was
// to where it is now, and the picture itself is hidden. Where it is now
// is read again at each frame, so the motion follows the window while it
// takes the picture's new shape.

const motionTime = 220 * time.Millisecond

// motion is the picture on its way.
type motion struct {
	layer  *gtk.Widget // draws the picture while it moves
	on     bool
	tick   uint32
	start  time.Time
	from   placed
	onTick gtk.TickCallback
}

// placed is where the picture is drawn: its centre on the stage, logical
// pixels to an image pixel, and the angle it is turned by, in degrees
// clockwise, counted on round and round.
type placed struct{ cx, cy, k, angle float64 }

func (a *App) buildMotion() {
	m := &a.motion
	m.layer = gtkx.NewCanvas(&gtkx.Canvas{Snapshot: func(_ *gtk.Widget, s *gtk.Snapshot) { a.drawMoving(s) }})
	m.layer.SetCanTarget(false)
	m.layer.SetVisible(false)
	a.imagePage.AddOverlay(m.layer)
	m.onTick = func(uintptr, uintptr, uintptr) bool {
		if time.Since(m.start) >= motionTime {
			m.tick = 0
			a.stopMotion()
			return false
		}
		m.layer.QueueDraw()
		return true
	}
}

// animated makes a change to the picture's zoom or turn, eased from where
// it is drawn now, unless animations are off.
func (a *App) animated(change func()) {
	m := &a.motion
	if a.w == 0 || a.cur == nil || !a.imagePage.GetMapped() || !gtk.SettingsGetDefault().GetPropertyGtkEnableAnimations() {
		change()
		return
	}
	from := a.drawnAt()
	change()
	m.from, m.start = from, time.Now()
	if !m.on {
		m.on = true
		a.picture.SetOpacity(0)
		m.layer.SetVisible(true)
		m.tick = m.layer.AddTickCallback(&m.onTick, 0, &noDestroy)
	}
	m.layer.QueueDraw()
}

// stopMotion shows the picture itself again.
func (a *App) stopMotion() {
	m := &a.motion
	if !m.on {
		return
	}
	m.on = false
	if m.tick != 0 {
		m.layer.RemoveTickCallback(m.tick)
		m.tick = 0
	}
	m.layer.SetVisible(false)
	a.picture.SetOpacity(1)
}

// placedNow is where the picture stands as laid out: centred on the stage
// while it is smaller than it, else where the scroll has it.
func (a *App) placedNow() placed {
	vw, vh := a.view()
	sw, sh := a.shownSize(a.zoom)
	t := 0
	if a.cur != nil {
		t, _ = viewer.Orientation(a.cur.orient)
	}
	return placed{
		cx:    math.Max(0, (vw-sw)/2) - a.hadj.GetValue() + sw/2,
		cy:    math.Max(0, (vh-sh)/2) - a.vadj.GetValue() + sh/2,
		k:     a.zoom / a.scale(),
		angle: float64(90 * (t + a.spin)),
	}
}

// drawnAt is where the picture is drawn now: on its way, or laid out.
func (a *App) drawnAt() placed {
	m := &a.motion
	to := a.placedNow()
	if !m.on {
		return to
	}
	p := min(1, float64(time.Since(m.start))/float64(motionTime))
	return between(m.from, to, 1-math.Pow(1-p, 3)) // easing out
}

// between is the place a share p of the way from a to b: the scale
// evenly in its logarithm, the centre so that a point kept in place by a
// zoom stays there all the way.
func between(a, b placed, p float64) placed {
	k := math.Exp(math.Log(a.k) + (math.Log(b.k)-math.Log(a.k))*p)
	q := p
	if math.Abs(b.k-a.k) > 1e-6*a.k {
		q = (k - a.k) / (b.k - a.k)
	}
	return placed{
		cx:    a.cx + (b.cx-a.cx)*q,
		cy:    a.cy + (b.cy-a.cy)*q,
		k:     k,
		angle: a.angle + (b.angle-a.angle)*p,
	}
}

// drawMoving draws the picture where it is on its way.
func (a *App) drawMoving(s *gtk.Snapshot) {
	pic := a.picture.GetPaintable()
	if !a.motion.on || pic == nil || a.cur == nil || a.texW == 0 || a.cur.fullW == 0 {
		return
	}
	at := a.drawnAt()
	_, mirror := a.orientation()
	k := at.k * float64(a.cur.fullW) / float64(a.texW) // a preview is drawn as large as the image
	fx := k
	if mirror {
		fx = -k
	}
	s.Save()
	s.Translate(&graphene.Point{X: float32(at.cx), Y: float32(at.cy)})
	s.Rotate(float32(at.angle))
	s.Scale(float32(fx), float32(k))
	s.Translate(&graphene.Point{X: -float32(a.texW) / 2, Y: -float32(a.texH) / 2})
	(&gdk.PaintableBase{Ptr: pic.GoPointer()}).Snapshot(&s.Snapshot, float64(a.texW), float64(a.texH))
	s.Restore()
}

// noDestroy is a destroy notify with nothing to do, for GTK's callbacks.
var noDestroy glib.DestroyNotify = func(uintptr) {}
