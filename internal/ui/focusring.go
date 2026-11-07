package ui

import (
	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gsk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/layershell"
)

const focusRingColor = "#89b4faff"

// focusRing is a transparent, input-free layer surface over one monitor.
// It outlines that monitor's last focused window while another monitor has
// compositor focus; the compositor draws the focused window's own border.
type focusRing struct {
	win    *gtk.ApplicationWindow
	canvas *gtk.Widget
	rect   graphene.Rect
	color  gdk.RGBA
}

func (b *bar) setFocusRing(s *shellUI, snap compositor.Snapshot, selected compositor.Window, ok bool) {
	if !s.layer || s.hypr == nil || !ok || selected.ID == snap.FocusedWindow || selected.Geometry == nil {
		b.hideFocusRing()
		return
	}
	out, found := snap.Output(b.connector())
	if !found {
		b.hideFocusRing()
		return
	}
	g := selected.Geometry
	left, top := max(g.X, out.X), max(g.Y, out.Y)
	right, bottom := min(g.X+g.W, out.X+out.Width), min(g.Y+g.H, out.Y+out.Height)
	if right <= left || bottom <= top {
		b.hideFocusRing()
		return
	}
	if b.focusRing == nil {
		b.focusRing = newFocusRing(s.app, b.mon)
	}
	r := b.focusRing
	r.rect.Init(float32(left-out.X), float32(top-out.Y), float32(right-left), float32(bottom-top))
	r.canvas.QueueDraw()
	r.win.SetVisible(true)
}

func newFocusRing(app *gtk.Application, mon *gdk.Monitor) *focusRing {
	r := &focusRing{}
	r.color.Parse(focusRingColor)
	r.canvas = gtkx.NewCanvas(&gtkx.Canvas{
		Min: 1, Natural: 1,
		Snapshot: func(_ *gtk.Widget, snap *gtk.Snapshot) {
			if r.rect.GetWidth() <= 0 || r.rect.GetHeight() <= 0 {
				return
			}
			var outline gsk.RoundedRect
			outline.InitFromRect(&r.rect, 0)
			colors := [4]gdk.RGBA{r.color, r.color, r.color, r.color}
			snap.AppendBorder(&outline, [4]float32{2, 2, 2, 2}, colors)
		},
	})
	r.canvas.SetHexpand(true)
	r.canvas.SetVexpand(true)
	r.canvas.SetCanTarget(false)
	r.canvas.SetFocusable(false)
	r.win = gtk.NewApplicationWindow(app)
	r.win.AddCssClass("focus-ring")
	r.win.SetDecorated(false)
	r.win.SetChild(r.canvas)
	p := r.win.GoPointer()
	layershell.InitForWindow(p)
	layershell.SetNamespace(p, "inua-focus-ring")
	layershell.SetLayer(p, layershell.LayerOverlay)
	layershell.SetMonitor(p, mon.GoPointer())
	layershell.SetKeyboardMode(p, layershell.KeyboardNone)
	layershell.SetExclusiveZone(p, -1)
	for _, e := range []layershell.Edge{layershell.EdgeTop, layershell.EdgeBottom, layershell.EdgeLeft, layershell.EdgeRight} {
		layershell.SetAnchor(p, e, true)
	}
	r.win.Present()
	gtkx.PassInput(&r.win.Window)
	return r
}

func (b *bar) hideFocusRing() {
	if b.focusRing != nil {
		b.focusRing.win.SetVisible(false)
	}
}

func (b *bar) dropFocusRing() {
	if b.focusRing == nil {
		return
	}
	gtkx.ForgetCanvas(b.focusRing.canvas)
	b.focusRing.win.Destroy()
	b.focusRing = nil
}
