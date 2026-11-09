package ui

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gsk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/layershell"
)

// focusRingColors is the fallback when the compositor cannot say what its
// focused border looks like.
const focusRingColors = "#89b4faff"

// focusRingColor is the compositor's active window border colour, so the
// ring around another monitor's window reads as the same highlight. A
// gradient uses its first stop; without Hyprland, or an answer, the
// default blue is used.
func (s *shellUI) loadFocusRingColor() {
	out := gdk.RGBA{}
	out.Parse(focusRingColors)
	if s.hypr == nil || s.hypr.Client == nil {
		s.focusRingColorValue, s.focusRingColorSet = out, true
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	reply, err := s.hypr.Client.Request(ctx, "j/getoption general:col.active_border")
	if err != nil {
		s.focusRingColorValue, s.focusRingColorSet = out, true
		return
	}
	var option struct {
		Gradient string `json:"gradient"`
		Color    string `json:"color"`
	}
	if json.Unmarshal(reply, &option) != nil {
		s.focusRingColorValue, s.focusRingColorSet = out, true
		return
	}
	spec := option.Gradient
	if spec != "" {
		if fields := strings.Fields(spec); len(fields) > 0 {
			spec = fields[0]
		}
	} else {
		spec = option.Color
	}
	if c, ok := parseBorderColor(spec); ok {
		out = c
	}
	s.focusRingColorValue, s.focusRingColorSet = out, true
}

// focusRingColor is the cached active border colour; it is read again when
// the settings reload.
func (s *shellUI) focusRingColor() gdk.RGBA {
	if !s.focusRingColorSet {
		s.loadFocusRingColor()
	}
	return s.focusRingColorValue
}

// parseBorderColor reads Hyprland's colour spellings: AARRGGBB for a
// gradient stop, 0xAARRGGBB, or rgba(RRGGBBAA).
func parseBorderColor(spec string) (gdk.RGBA, bool) {
	spec = strings.TrimSpace(spec)
	hex := func(v string, alpha uint64) (gdk.RGBA, bool) {
		n, err := strconv.ParseUint(v, 16, 32)
		if err != nil {
			return gdk.RGBA{}, false
		}
		return gdk.RGBA{
			Red:   float32((n>>16)&0xff) / 255,
			Green: float32((n>>8)&0xff) / 255,
			Blue:  float32(n&0xff) / 255,
			Alpha: float32(alpha) / 255,
		}, true
	}
	switch {
	case strings.HasPrefix(spec, "rgba(") && strings.HasSuffix(spec, ")"):
		v := strings.TrimSuffix(strings.TrimPrefix(spec, "rgba("), ")")
		if len(v) != 8 {
			return gdk.RGBA{}, false
		}
		alpha, err := strconv.ParseUint(v[6:], 16, 8)
		if err != nil {
			return gdk.RGBA{}, false
		}
		return hex(v[:6], alpha)
	case strings.HasPrefix(spec, "0x"):
		spec = spec[2:]
		if len(spec) != 8 {
			return gdk.RGBA{}, false
		}
		alpha, err := strconv.ParseUint(spec[:2], 16, 8)
		if err != nil {
			return gdk.RGBA{}, false
		}
		return hex(spec[2:], alpha)
	case len(spec) == 8:
		// AARRGGBB, as gradients are written.
		alpha, err := strconv.ParseUint(spec[:2], 16, 8)
		if err != nil {
			return gdk.RGBA{}, false
		}
		return hex(spec[2:], alpha)
	}
	return gdk.RGBA{}, false
}

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
		b.focusRing = newFocusRing(s.app, b.mon, s.focusRingColor())
	} else {
		b.focusRing.color = s.focusRingColor()
	}
	r := b.focusRing
	r.rect.Init(float32(left-out.X), float32(top-out.Y), float32(right-left), float32(bottom-top))
	r.canvas.QueueDraw()
	r.win.SetVisible(true)
}

func newFocusRing(app *gtk.Application, mon *gdk.Monitor, color gdk.RGBA) *focusRing {
	r := &focusRing{color: color}
	r.canvas = gtkx.NewCanvas(&gtkx.Canvas{
		Min: 1, Natural: 1,
		Snapshot: func(_ *gtk.Widget, snap *gtk.Snapshot) {
			if r.rect.GetWidth() <= 0 || r.rect.GetHeight() <= 0 {
				return
			}
			// Square like the compositor's own border: the last focused
			// window on another monitor must read as the same highlight.
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
