package ui

import (
	"context"
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
	// hotCornerPollMS is how often the pointer's position is queried.
	// 20 ms (50 Hz) gives silky smooth tracking and immediate response.
	hotCornerPollMS = 20
	// hotCornerSize is how close, in logical pixels, the pointer must come
	// to the corner of a display to trigger it.
	hotCornerSize = 24
	// hotCornerHUDSize is the width and height of the corner indicator surface.
	hotCornerHUDSize = 64
	hotCornerFadeMS  = 150
	// hotDeskWorkspace holds every window while the desk shows; it is
	// never switched to, so its windows are out of sight.
	hotDeskWorkspace = "special:inua-desktop"
	hotDeskStore     = "hot-corner-desk"
)

// hotCornerHit is a corner of a display under the pointer: which corner,
// the action it has, and the monitor the pointer is on.
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

type hotCornerHUD struct {
	win     *gtk.ApplicationWindow
	rev     *gtk.Revealer
	card    *gtk.Box
	icon    *gtk.Image
	level   *gtk.LevelBar
	corner  string
	monitor string
	action  string
	gen     uintptr
}

type hotCornerState struct {
	tracking, querying bool
	recovered          bool
	generation         uint64
	current            hotCornerHit
	entered            time.Time
	fired              bool
	desktop            hotDesktopState
	hud                hotCornerHUD
}

func (s *shellUI) bindHotCornerHandlers() {
	s.onHotCornerGone = func(gen uintptr) bool {
		hud := &s.hotCorner.hud
		if hud.win != nil && gen == hud.gen && hud.rev != nil && !hud.rev.GetChildRevealed() {
			hud.win.SetVisible(false)
		}
		return false
	}
	s.onHotCornerDismiss = func(gen uintptr) bool {
		hud := &s.hotCorner.hud
		if gen == hud.gen {
			s.hideHotCornerHUD()
		}
		return false
	}
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
	s.hideHotCornerHUD()
}

func (s *shellUI) hotCornerSettingsChanged(next config.HotCorners) {
	s.resetHotCornerPointer()
	if !hotCornerHasDesktop(next) {
		s.restoreHotDesk()
	}
	s.syncHotCornerPolling()
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
	if len(s.snap.Outputs) == 0 {
		return hotCornerHit{}
	}
	for _, out := range s.snap.Outputs {
		w, h := hotOutputSize(out)
		if x < out.X || y < out.Y || x > out.X+w || y > out.Y+h {
			continue
		}
		relX := x - out.X
		relY := y - out.Y
		corner, action := "", config.HotCornerOff
		switch {
		case relX <= hotCornerSize && relY <= hotCornerSize:
			corner, action = "top_left", s.cfg.HotCorners.TopLeft
		case (w-relX) <= hotCornerSize && relY <= hotCornerSize:
			corner, action = "top_right", s.cfg.HotCorners.TopRight
		case relX <= hotCornerSize && (h-relY) <= hotCornerSize:
			corner, action = "bottom_left", s.cfg.HotCorners.BottomLeft
		case (w-relX) <= hotCornerSize && (h-relY) <= hotCornerSize:
			corner, action = "bottom_right", s.cfg.HotCorners.BottomRight
		}
		if corner != "" && action != config.HotCornerOff {
			return hotCornerHit{corner: corner, action: action, monitor: out.Name}
		}
	}
	return hotCornerHit{}
}

func (s *shellUI) updateHotCorner(hit hotCornerHit, now time.Time) {
	h := &s.hotCorner
	if hit != h.current {
		h.current = hit
		h.entered = now
		h.fired = false
		if hit.corner == "" {
			s.hideHotCornerHUD()
		} else {
			s.showHotCornerHUD(hit)
		}
	}
	if hit.action == "" || h.fired {
		return
	}
	delay := time.Duration(s.cfg.HotCorners.Delay) * time.Millisecond
	elapsed := now.Sub(h.entered)
	if delay > 0 {
		progress := min(float64(elapsed)/float64(delay), 1.0)
		s.updateHotCornerProgress(progress)
	}
	if elapsed < delay {
		return
	}
	h.fired = true
	s.pulseHotCornerHUD()
	log.Printf("hotcorner: %s (%s) on %s", hit.action, hit.corner, hit.monitor)
	if s.onHotCornerAction != nil {
		s.onHotCornerAction(hit)
		return
	}
	switch hit.action {
	case config.HotCornerOverview:
		s.showOverviewOn(hit.monitor)
	case config.HotCornerShowDesktop:
		s.toggleHotCornerDesktop()
	}
}

// showHotCornerHUD displays the subtle glass HUD at the hit corner.
// It never takes the pointer: the surface's input region is empty.
func (s *shellUI) showHotCornerHUD(hit hotCornerHit) {
	if hit.corner == "" || !s.layer {
		s.hideHotCornerHUD()
		return
	}
	hud := &s.hotCorner.hud
	if hud.win == nil {
		s.buildHotCornerHUD(hud)
	}
	hud.gen++

	if hud.action != hit.action {
		hud.action = hit.action
		iconName := "view-grid-symbolic"
		if hit.action == config.HotCornerShowDesktop {
			iconName = "user-desktop-symbolic"
		}
		hud.icon.SetFromIconName(iconName)
	}

	if hud.corner != hit.corner || hud.monitor != hit.monitor {
		for _, c := range []string{"corner-top-left", "corner-top-right", "corner-bottom-left", "corner-bottom-right"} {
			hud.card.RemoveCssClass(c)
		}
		cornerClass := "corner-" + strings.ReplaceAll(hit.corner, "_", "-")
		hud.card.AddCssClass(cornerClass)

		top := hit.corner == "top_left" || hit.corner == "top_right"
		left := hit.corner == "top_left" || hit.corner == "bottom_left"
		p := hud.win.GoPointer()
		layershell.SetAnchor(p, layershell.EdgeTop, top)
		layershell.SetAnchor(p, layershell.EdgeBottom, !top)
		layershell.SetAnchor(p, layershell.EdgeLeft, left)
		layershell.SetAnchor(p, layershell.EdgeRight, !left)

		if left {
			hud.card.SetHalign(gtk.AlignStartValue)
		} else {
			hud.card.SetHalign(gtk.AlignEndValue)
		}
		if top {
			hud.card.SetValign(gtk.AlignStartValue)
		} else {
			hud.card.SetValign(gtk.AlignEndValue)
		}

		if mon := s.monitorByName(hit.monitor); mon != nil {
			layershell.SetMonitor(p, mon.GoPointer())
		}
		hud.corner = hit.corner
		hud.monitor = hit.monitor
	}

	hud.card.RemoveCssClass("triggered")
	hud.card.AddCssClass("active")
	if hud.level != nil {
		hud.level.SetValue(0)
	}

	if !hud.win.GetVisible() {
		hud.win.SetVisible(true)
		hud.win.Present()
		gtkx.PassInput(&hud.win.Window)
	}
	hud.rev.SetRevealChild(true)
}

func (s *shellUI) hideHotCornerHUD() {
	hud := &s.hotCorner.hud
	if hud.win == nil || !hud.win.GetVisible() {
		return
	}
	hud.gen++
	gen := hud.gen
	if hud.card != nil {
		hud.card.RemoveCssClass("active")
		hud.card.RemoveCssClass("triggered")
	}
	if hud.rev != nil {
		hud.rev.SetRevealChild(false)
	}
	glib.TimeoutAdd(hotCornerFadeMS, &s.onHotCornerGone, gen)
}

func (s *shellUI) updateHotCornerProgress(progress float64) {
	hud := &s.hotCorner.hud
	if hud.level != nil {
		hud.level.SetValue(progress)
	}
}

func (s *shellUI) pulseHotCornerHUD() {
	hud := &s.hotCorner.hud
	if hud.card == nil {
		return
	}
	hud.card.AddCssClass("triggered")
	if hud.level != nil {
		hud.level.SetValue(1.0)
	}
	hud.gen++
	glib.TimeoutAdd(120, &s.onHotCornerDismiss, hud.gen)
}

func (s *shellUI) buildHotCornerHUD(hud *hotCornerHUD) {
	hud.rev = gtk.NewRevealer()
	hud.rev.AddCssClass("hotcorner-revealer")
	hud.rev.SetTransitionType(gtk.RevealerTransitionTypeCrossfadeValue)
	hud.rev.SetTransitionDuration(hotCornerFadeMS)

	hud.card = gtk.NewBox(gtk.OrientationVerticalValue, 4)
	hud.card.AddCssClass("hotcorner-card")
	hud.card.SetCanTarget(false)

	hud.icon = gtk.NewImage()
	hud.icon.AddCssClass("hotcorner-icon")
	hud.icon.SetPixelSize(22)
	hud.icon.SetCanTarget(false)
	hud.card.Append(&hud.icon.Widget)

	hud.level = gtk.NewLevelBarForInterval(0, 1)
	hud.level.AddCssClass("hotcorner-bar")
	hud.level.SetCanTarget(false)
	hud.level.SetSizeRequest(28, 2)
	hud.card.Append(&hud.level.Widget)

	hud.rev.SetChild(&hud.card.Widget)

	hud.win = gtk.NewApplicationWindow(s.app)
	hud.win.AddCssClass("hotcorner")
	hud.win.SetDecorated(false)
	hud.win.SetDefaultSize(hotCornerHUDSize, hotCornerHUDSize)
	hud.win.SetChild(&hud.rev.Widget)
	hud.win.SetVisible(false)

	p := hud.win.GoPointer()
	layershell.InitForWindow(p)
	layershell.SetNamespace(p, "inua-hotcorner")
	layershell.SetLayer(p, layershell.LayerOverlay)
	layershell.SetKeyboardMode(p, layershell.KeyboardNone)
	layershell.SetExclusiveZone(p, -1)
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
