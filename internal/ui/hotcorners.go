package ui

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/store"
	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/layershell"
)

const (
	hotCornerPollMS = 80
	// hotCornerSize is how close, in logical pixels, the pointer must come
	// to the two edges that meet at a corner of the displays' whole canvas.
	hotCornerSize = 8
	// The ripple's surface: a square at the corner, its rings inside it.
	hotRippleSize = 160
	hotRingSize   = 64
	hotRingInset  = 12 // from the corner to the rings' box
	// hotDeskWorkspace holds every window while the desk shows; it is
	// never switched to, so its windows are out of sight.
	hotDeskWorkspace = "special:inua-desktop"
	hotDeskStore     = "hot-corner-desk"
)

// hotCornerHit is a corner of the whole canvas under the pointer: which
// corner, the action it has, and the display the pointer is on.
type hotCornerHit struct {
	corner  string
	action  string
	monitor string
}

// hotMovedWindow is a window Show desktop put away, and where it was.
type hotMovedWindow struct {
	ID        string `json:"id"`
	Workspace string `json:"workspace"`
}

// hotDesktopState is Show desktop: the windows it moved aside, to bring
// back where they were on the next trigger.
type hotDesktopState struct {
	shown   bool
	windows []hotMovedWindow
}

type hotCornerRipple struct {
	win    *gtk.ApplicationWindow
	fixed  *gtk.Fixed
	rings  [3]*gtk.Box
	corner string
}

type hotCornerState struct {
	tracking, querying bool
	recovered          bool
	generation         uint64
	current            hotCornerHit
	entered            time.Time
	fired              bool
	desktop            hotDesktopState
	ripple             hotCornerRipple
}

func (s *shellUI) bindHotCornerHandlers() {
	s.onHotCornerTick = func(uintptr) bool {
		h := &s.hotCorner
		if !s.hotCornerEnabled() {
			h.tracking = false
			s.resetHotCornerPointer()
			return false
		}
		if s.hypr == nil || s.comp == nil {
			h.tracking = false
			return false
		}
		pointer, ok := s.comp.(compositor.Pointer)
		if !ok {
			h.tracking = false
			return false
		}
		if !s.connected || h.querying {
			return true
		}
		h.querying = true
		generation := h.generation
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			x, y, err := pointer.CursorPos(ctx)
			gtkx.Post(func() {
				h.querying = false
				if generation != h.generation {
					return
				}
				if err != nil {
					s.updateHotCorner(hotCornerHit{}, time.Now())
					return
				}
				s.updateHotCorner(s.hotCornerAt(x, y), time.Now())
			})
		}()
		return true
	}
}

func (s *shellUI) hotCornerEnabled() bool {
	return s.hypr != nil && (hotCornersConfigured(s.cfg.HotCorners) || s.hotCorner.desktop.shown)
}

func hotCornersConfigured(c config.HotCorners) bool {
	return c.TopLeft != config.HotCornerOff || c.TopRight != config.HotCornerOff ||
		c.BottomLeft != config.HotCornerOff || c.BottomRight != config.HotCornerOff
}

func hotCornerHasDesktop(c config.HotCorners) bool {
	return c.TopLeft == config.HotCornerShowDesktop || c.TopRight == config.HotCornerShowDesktop ||
		c.BottomLeft == config.HotCornerShowDesktop || c.BottomRight == config.HotCornerShowDesktop
}

// syncHotCornerPolling starts the pointer poll when something asks for it:
// a corner with an action, or windows to bring back. One timer runs until
// nothing needs it.
func (s *shellUI) syncHotCornerPolling() {
	if s.onHotCornerTick == nil || !s.hotCornerEnabled() || s.hotCorner.tracking {
		return
	}
	s.hotCorner.tracking = true
	glib.TimeoutAdd(hotCornerPollMS, &s.onHotCornerTick, 0)
}

func (s *shellUI) resetHotCornerPointer() {
	s.hotCorner.generation++
	s.hotCorner.current = hotCornerHit{}
	s.hotCorner.entered = time.Time{}
	s.hotCorner.fired = false
	s.showHotCornerRipple(hotCornerHit{})
}

func (s *shellUI) hotCornerSettingsChanged(next config.HotCorners) {
	s.resetHotCornerPointer()
	if !hotCornerHasDesktop(next) {
		s.restoreHotDesk()
	}
	s.syncHotCornerPolling()
}

// hotCanvas is the rectangle the displays make together: their union, in
// the pointer's logical pixels. Corners are this rectangle's, not each
// display's.
func (s *shellUI) hotCanvas() (x0, y0, x1, y1 int, ok bool) {
	if len(s.snap.Outputs) == 0 {
		return 0, 0, 0, 0, false
	}
	for i, o := range s.snap.Outputs {
		w, h := hotOutputSize(o)
		if i == 0 {
			x0, y0, x1, y1 = o.X, o.Y, o.X+w, o.Y+h
			continue
		}
		x0, y0 = min(x0, o.X), min(y0, o.Y)
		x1, y1 = max(x1, o.X+w), max(y1, o.Y+h)
	}
	return x0, y0, x1, y1, true
}

// hotOutputSize is an output's size in the pointer's coordinates: the
// snapshot keeps the mode's physical pixels, and the compositor's cursor
// position is in logical ones.
func hotOutputSize(o compositor.Output) (w, h int) {
	scale := max(o.Scale, 1)
	w, h = int(math.Round(float64(o.Width)/scale)), int(math.Round(float64(o.Height)/scale))
	if o.Transform%2 == 1 {
		w, h = h, w
	}
	return w, h
}

func (s *shellUI) hotCornerAt(x, y int) hotCornerHit {
	x0, y0, x1, y1, ok := s.hotCanvas()
	if !ok || x < x0 || y < y0 || x > x1 || y > y1 {
		return hotCornerHit{}
	}
	corner, action := "", config.HotCornerOff
	switch {
	case x-x0 <= hotCornerSize && y-y0 <= hotCornerSize:
		corner, action = "top_left", s.cfg.HotCorners.TopLeft
	case x1-x <= hotCornerSize && y-y0 <= hotCornerSize:
		corner, action = "top_right", s.cfg.HotCorners.TopRight
	case x-x0 <= hotCornerSize && y1-y <= hotCornerSize:
		corner, action = "bottom_left", s.cfg.HotCorners.BottomLeft
	case x1-x <= hotCornerSize && y1-y <= hotCornerSize:
		corner, action = "bottom_right", s.cfg.HotCorners.BottomRight
	}
	if corner == "" || action == config.HotCornerOff {
		return hotCornerHit{}
	}
	monitor := s.snap.FocusedOutput
	for _, out := range s.snap.Outputs {
		if x >= out.X && y >= out.Y && x < out.X+out.Width && y < out.Y+out.Height {
			monitor = out.Name
			break
		}
	}
	return hotCornerHit{corner: corner, action: action, monitor: monitor}
}

func (s *shellUI) updateHotCorner(hit hotCornerHit, now time.Time) {
	h := &s.hotCorner
	if hit != h.current {
		h.current = hit
		h.entered = now
		h.fired = false
		s.showHotCornerRipple(hit)
	}
	if hit.action == "" || h.fired || now.Sub(h.entered) < time.Duration(s.cfg.HotCorners.Delay)*time.Millisecond {
		return
	}
	h.fired = true
	s.showHotCornerRipple(hotCornerHit{})
	// As on a Mac, one entry in a corner does one thing and the next entry
	// does its opposite: the overview opens and closes, the desk shows and
	// the windows come back where they were.
	log.Printf("hotcorner: %s (%s) on %s", hit.action, hit.corner, hit.monitor)
	switch hit.action {
	case config.HotCornerOverview:
		s.showOverviewOn(hit.monitor) // toggles closed while open
	case config.HotCornerShowDesktop:
		s.toggleHotCornerDesktop()
	}
}

// showHotCornerRipple draws the waiting ripple in the corner the pointer
// is in, or puts it away for an empty hit. It never takes the pointer: the
// surface's input region is empty.
func (s *shellUI) showHotCornerRipple(hit hotCornerHit) {
	if hit.corner == "" || !s.layer {
		if s.hotCorner.ripple.win != nil {
			s.hotCorner.ripple.win.SetVisible(false)
		}
		return
	}
	r := &s.hotCorner.ripple
	if r.win == nil {
		s.buildHotCornerRipple(r)
	}
	if r.corner != hit.corner {
		top := hit.corner == "top_left" || hit.corner == "top_right"
		left := hit.corner == "top_left" || hit.corner == "bottom_left"
		p := r.win.GoPointer()
		layershell.SetAnchor(p, layershell.EdgeTop, top)
		layershell.SetAnchor(p, layershell.EdgeBottom, !top)
		layershell.SetAnchor(p, layershell.EdgeLeft, left)
		layershell.SetAnchor(p, layershell.EdgeRight, !left)
		x := hotRingInset
		if !left {
			x = hotRippleSize - hotRingInset - hotRingSize
		}
		y := hotRingInset
		if !top {
			y = hotRippleSize - hotRingInset - hotRingSize
		}
		for i := range r.rings {
			r.fixed.Move(&r.rings[i].Widget, float64(x), float64(y))
		}
		r.corner = hit.corner
	}
	if mon := s.monitorByName(hit.monitor); mon != nil {
		layershell.SetMonitor(r.win.GoPointer(), mon.GoPointer())
	}
	if !r.win.GetVisible() {
		r.win.SetVisible(true)
		r.win.Present()
		gtkx.PassInput(&r.win.Window)
	}
}

func (s *shellUI) buildHotCornerRipple(r *hotCornerRipple) {
	r.fixed = gtk.NewFixed()
	r.fixed.AddCssClass("hotcorner-ripple")
	r.fixed.SetCanTarget(false)
	r.fixed.SetSizeRequest(hotRippleSize, hotRippleSize)
	for i := range r.rings {
		b := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		b.AddCssClass("hotcorner-ring")
		b.AddCssClass(fmt.Sprintf("hotcorner-ring-%d", i+1))
		b.SetCanTarget(false)
		b.SetSizeRequest(hotRingSize, hotRingSize)
		r.fixed.Put(&b.Widget, hotRingInset, hotRingInset)
		r.rings[i] = b
	}
	r.win = gtk.NewApplicationWindow(s.app)
	r.win.AddCssClass("hotcorner")
	r.win.SetDecorated(false)
	r.win.SetDefaultSize(hotRippleSize, hotRippleSize)
	r.win.SetChild(&r.fixed.Widget)
	r.win.SetVisible(false)
	p := r.win.GoPointer()
	layershell.InitForWindow(p)
	layershell.SetNamespace(p, "inua-hotcorner")
	layershell.SetLayer(p, layershell.LayerOverlay)
	layershell.SetKeyboardMode(p, layershell.KeyboardNone)
	layershell.SetExclusiveZone(p, -1) // at the very corner, over the bar
}

// toggleHotCornerDesktop shows the desk, or brings the windows back, as a
// Mac's Show Desktop does: the workspaces stay as they are, and the
// windows are put away and returned where they were.
func (s *shellUI) toggleHotCornerDesktop() {
	if s.hotCorner.desktop.shown {
		s.restoreHotDesk()
		return
	}
	s.showHotDesk()
}

func (s *shellUI) showHotDesk() {
	if s.comp == nil || s.hypr == nil || s.hotCorner.desktop.shown {
		return
	}
	// Only the windows the displays show now: those on other workspaces
	// are out of sight already.
	active := map[string]bool{}
	for _, out := range s.snap.Outputs {
		active[out.ActiveWorkspace] = true
	}
	var moved []hotMovedWindow
	for _, w := range s.snap.Windows {
		if w.Workspace == "" || strings.HasPrefix(w.Workspace, "special:") || !active[w.Workspace] {
			continue
		}
		moved = append(moved, hotMovedWindow{ID: w.ID, Workspace: w.Workspace})
	}
	s.hotCorner.desktop = hotDesktopState{shown: true, windows: moved}
	if len(moved) > 0 {
		if err := store.Save(hotDeskStore, moved); err != nil {
			log.Printf("hotcorner: saving the desk: %v", err)
		}
	}
	for _, w := range moved {
		s.compositorDo("hide window "+w.ID, func(ctx context.Context) error {
			return s.comp.MoveWindow(ctx, w.ID, hotDeskWorkspace, false)
		})
	}
	log.Printf("hotcorner: desk shown (%d windows)", len(moved))
}

func (s *shellUI) restoreHotDesk() {
	d := &s.hotCorner.desktop
	if !d.shown {
		return
	}
	moved := d.windows
	s.hotCorner.desktop = hotDesktopState{}
	if err := store.Save(hotDeskStore, []hotMovedWindow(nil)); err != nil {
		log.Printf("hotcorner: clearing the desk's store: %v", err)
	}
	for _, w := range moved {
		if _, ok := s.snap.Window(w.ID); !ok {
			continue // closed meanwhile
		}
		s.compositorDo("bring back window "+w.ID, func(ctx context.Context) error {
			return s.comp.MoveWindow(ctx, w.ID, w.Workspace, false)
		})
	}
	log.Printf("hotcorner: desk back (%d windows)", len(moved))
}

// recoverHotDesk brings back the windows a shell that stopped while the
// desk showed left in the hidden workspace.
func (s *shellUI) recoverHotDesk(snap compositor.Snapshot) {
	h := &s.hotCorner
	if h.recovered {
		return
	}
	h.recovered = true
	var moved []hotMovedWindow
	if err := store.Load(hotDeskStore, &moved); err != nil || len(moved) == 0 {
		return
	}
	if s.comp == nil {
		return
	}
	_ = store.Save(hotDeskStore, []hotMovedWindow(nil))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		for _, w := range moved {
			if _, ok := snap.Window(w.ID); !ok {
				continue
			}
			if err := s.comp.MoveWindow(ctx, w.ID, w.Workspace, false); err != nil {
				log.Printf("hotcorner: recovering window %s: %v", w.ID, err)
			}
		}
	}()
	log.Printf("hotcorner: recovered %d windows from a hidden desk", len(moved))
}
