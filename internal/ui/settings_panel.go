package ui

import (
	"context"
	"log"
	"os"
	"time"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/graphene"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/gtkx"
)

// sidePanel is a panel sliding in at the Settings window's right: a
// profile's settings on the AI page, a read on the Keyring page. As in
// macOS (an inspector, Finder's preview pane), the window keeps its size:
// the panel slides in and the page beside it gives up the width, in one
// animation. Only when the page would get narrower than it can be does
// the window widen first, by what is missing: to its right while there is
// room, else moving left by the rest. Hidden, the panel gives the width
// back to the page; the window stays as it is. As the page's width
// changes, its text wraps anew: the row the panel is for keeps its place
// in the page's view meanwhile.
type sidePanel struct {
	rev    *gtk.Revealer
	scroll *gtk.ScrolledWindow // the page's
	width  int                 // the panel's
	want   bool                // shown, or on its way
	gen    int                 // the latest show; an earlier one's widening is moot

	anchor *gtk.Widget // the row kept in place while the panel slides
	offset float64     // its top below the view's top
	until  time.Time   // when the slide is over
	tick   uint32      // the frame callback keeping it; 0 for none
	// The callbacks outlive the window, like every signal handler.
	onTick     gtk.TickCallback
	onTickDone glib.DestroyNotify
}

func (s *shellUI) initPanel(p *sidePanel, rev *gtk.Revealer, scroll *gtk.ScrolledWindow, width int) {
	p.setAnchor(nil)
	p.rev, p.scroll, p.width, p.want, p.tick = rev, scroll, width, false, 0
	p.gen++
	if p.onTick == nil {
		p.onTick = func(uintptr, uintptr, uintptr) bool {
			p.keepAnchor()
			if time.Now().Before(p.until) {
				return true
			}
			p.tick = 0
			p.setAnchor(nil)
			return false
		}
		p.onTickDone = func(uintptr) {}
	}
}

// anchorTop is where the anchor's top is in the page, scrolled or not.
func (p *sidePanel) anchorTop() (float64, bool) {
	page := p.scroll.GetChild().GetFirstChild() // viewport > page box
	var r graphene.Rect
	if page == nil || p.anchor == nil || !p.anchor.ComputeBounds(page, &r) {
		return 0, false
	}
	return float64((*(*[4]float32)(unsafe.Pointer(&r)))[1]), true
}

// setAnchor holds the row kept in place: a list rebuilt meanwhile
// unparents it, and it is then simply not followed.
func (p *sidePanel) setAnchor(w *gtk.Widget) {
	if p.anchor != nil {
		p.anchor.Unref()
	}
	p.anchor = w
	if w != nil {
		w.Ref()
	}
}

// pin keeps anchor where it is in the view while the panel slides.
func (p *sidePanel) pin(anchor *gtk.Widget) {
	if p.scroll == nil || anchor == nil {
		return
	}
	p.setAnchor(anchor)
	top, ok := p.anchorTop()
	if !ok {
		p.setAnchor(nil)
		return
	}
	p.offset = top - p.scroll.GetVadjustment().GetValue()
	p.until = time.Now().Add(time.Duration(p.rev.GetTransitionDuration()+150) * time.Millisecond)
	if p.tick == 0 {
		p.tick = p.rev.AddTickCallback(&p.onTick, 0, &p.onTickDone)
	}
}

// keepAnchor scrolls the page so the anchor is where it was in the view.
func (p *sidePanel) keepAnchor() {
	if top, ok := p.anchorTop(); ok {
		p.scroll.GetVadjustment().SetValue(top - p.offset)
	}
}

// showPanel opens or closes the panel; anchor is the row it is for, kept
// in place in the page's view.
func (s *shellUI) showPanel(p *sidePanel, show bool, anchor *gtk.Widget) {
	if show == p.want {
		return
	}
	p.want = show
	p.gen++
	if !show || p.rev.GetRevealChild() {
		p.pin(anchor)
		p.rev.SetRevealChild(show)
		return
	}
	// What the page lacks beside the panel: the panel is its sibling.
	missing := 0
	if page := p.rev.GetPrevSibling(); page != nil {
		var least, natural int32
		page.Measure(gtk.OrientationHorizontalValue, -1, &least, &natural, nil, nil)
		missing = int(least) - (int(page.GetWidth()) - p.width)
	}
	placer, _ := s.comp.(compositor.Placer)
	if missing <= 0 || placer == nil || !s.caps.Has(compositor.CapPlacement) {
		p.pin(anchor)
		p.rev.SetRevealChild(true)
		return
	}
	gen := p.gen
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		pid := os.Getpid()
		r, room, err := placer.RoomRight(ctx, pid)
		if err == nil {
			x := r.X - max(0, missing-room) // left by what the right lacks
			err = placer.PlaceWindow(ctx, pid, compositor.Rect{X: x, Y: r.Y, W: r.W + missing, H: r.H})
		}
		if err != nil {
			log.Printf("settings: panel: %v", err)
		}
		gtkx.Post(func() {
			if p.gen == gen {
				p.pin(anchor)
				p.rev.SetRevealChild(true)
			}
		})
	}()
}
