package viewerui

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lyricat/inua/internal/apps/viewer"
	"github.com/lyricat/inua/internal/compositor"
)

// The window takes the picture's shape (viewer.WindowFor), so the
// picture fills it with nothing round it: once its size is known, and
// again when it turns. A floating window only, and not one full screen:
// a tiled window keeps the place the layout gives it.

// shaping is the window's shape asked for.
type shaping struct {
	asked string        // the picture and size it was asked for
	gen   atomic.Uint64 // bumps with each ask: an older one is dropped
	mu    sync.Mutex    // one ask at a time, in order
}

// shapeWindow asks for the window's shape for the picture shown, once
// for each picture and size.
func (a *App) shapeWindow() {
	if a.kind != viewer.Image || a.cur == nil || a.w == 0 || a.edit.on || a.win.IsFullscreen() {
		return
	}
	key := fmt.Sprintf("%s %d×%d", a.cur.path, a.w, a.h)
	if key == a.shape.asked {
		return
	}
	a.shape.asked = key
	gen := a.shape.gen.Add(1)
	w, h := float64(a.w), float64(a.h)
	go func() {
		a.shape.mu.Lock()
		defer a.shape.mu.Unlock()
		if a.shape.gen.Load() != gen {
			return
		}
		if err := shapeWindow(w, h); err != nil {
			log.Printf("viewer: window shape: %v", err)
		}
	}()
}

// shapeWindow gives this process's window the shape for a w×h picture,
// when it floats.
func shapeWindow(w, h float64) error {
	b, err := compositor.Detect()
	if err != nil {
		return nil // no compositor we know: the window as it is
	}
	placer, ok := b.(compositor.Placer)
	snapper, ok2 := b.(compositor.Snapshotter)
	if !ok || !ok2 || !b.Capabilities().Has(compositor.CapPlacement) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	snap, err := snapper.Snapshot(ctx)
	if err != nil {
		return err
	}
	pid := os.Getpid()
	var mine []compositor.Window
	for _, win := range snap.Windows {
		if win.PID == pid {
			mine = append(mine, win)
		}
	}
	// A dialog open too: which is which is not known by the process.
	if len(mine) != 1 || !mine[0].Floating || mine[0].Geometry == nil {
		return nil
	}
	out, ok := snap.Output(mine[0].Output)
	if !ok {
		return nil
	}
	scale := out.Scale
	if scale <= 0 {
		scale = 1
	}
	cur := viewer.Rect(*mine[0].Geometry)
	r := viewer.WindowFor(w/scale, h/scale, cur, viewer.Rect{X: out.X, Y: out.Y, W: out.Width, H: out.Height})
	if r == cur {
		return nil
	}
	return placer.PlaceWindow(ctx, pid, compositor.Rect(r))
}
