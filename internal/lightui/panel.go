package lightui

import (
	"context"
	"log"
	"os"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/gtkx"
)

// sidePanel is a panel sliding in at a window's right, which widens the
// window. A floating window keeps the size and place it grew to, so once
// the panel has slid away, Hyprland is asked to put the window back: its
// size from before, and its place from before, moved by as much as it was
// moved while the panel showed.
type sidePanel struct {
	win    *gtk.ApplicationWindow
	rev    *gtk.Revealer
	want   bool
	before *compositor.Rect // the window before the panel, while it shows
	grown  *compositor.Rect // and once the panel showed
	// onRevealed outlives the window, like every signal handler.
	onRevealed func(uintptr, uintptr, uintptr)
}

func (p *sidePanel) init(win *gtk.ApplicationWindow, rev *gtk.Revealer) {
	p.win, p.rev = win, rev
	p.onRevealed = func(uintptr, uintptr, uintptr) {
		before := p.before
		if before == nil {
			return
		}
		shown, grown := p.rev.GetChildRevealed(), p.grown
		go func() {
			c, err := placer()
			if err != nil {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			now, err := c.WindowOf(ctx, os.Getpid())
			if err != nil {
				log.Printf("panel: %v", err)
				return
			}
			if shown {
				gtkx.Post(func() {
					if p.before == before {
						p.grown = &now
					}
				})
				return
			}
			r := *before
			if grown != nil { // moved since it grew
				r.X += now.X - grown.X
				r.Y += now.Y - grown.Y
			}
			if err := c.PlaceWindow(ctx, os.Getpid(), r); err != nil {
				log.Printf("panel: %v", err)
			}
		}()
		if !shown {
			p.before, p.grown = nil, nil
		}
	}
	gtkx.ConnectNotify(rev, "child-revealed", &p.onRevealed)
}

// open shows the panel, once the window's place before it is known.
func (p *sidePanel) open() {
	if p.want {
		return
	}
	p.want = true
	go func() {
		var before *compositor.Rect
		if c, err := placer(); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			if r, err := c.WindowOf(ctx, os.Getpid()); err == nil {
				before = &r
			}
			cancel()
		}
		gtkx.Post(func() {
			if !p.want {
				return
			}
			if p.before == nil {
				p.before, p.grown = before, nil
			}
			p.rev.SetRevealChild(true)
		})
	}()
}

func (p *sidePanel) close() {
	p.want = false
	p.rev.SetRevealChild(false)
}

// placer is the compositor's window placement, when it has one.
func placer() (compositor.Placer, error) {
	b, err := compositor.Detect()
	if err != nil {
		return nil, err
	}
	p, ok := b.(compositor.Placer)
	if !ok || !b.Capabilities().Has(compositor.CapPlacement) {
		return nil, compositor.ErrUnsupported
	}
	return p, nil
}
