package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/compositor"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/layershell"
)

type overviewDropTarget struct {
	ID      string
	Monitor string
	New     bool
	Scale   float64
}

func (s *shellUI) bindOverviewManagement() {
	o := &s.overview
	o.onPointer = func(c gtk.EventControllerMotion, x, y float64) {
		widget := c.GetWidget()
		if widget == nil {
			return
		}
		defer widget.Unref()
		views := []*workspaceOverview{o}
		for _, view := range o.views {
			views = append(views, view)
		}
		for _, view := range views {
			if view.win != nil && view.win.GoPointer() == widget.GoPointer() {
				s.selectOverviewInput(view.monitor)
				if o.killMode {
					view.killLayer.Move(&view.killGlyph.Widget, x+8, y+8)
				}
				break
			}
		}
	}
	o.onSurfacePress = func(g gtk.GestureClick, _ int32, _, _ float64) {
		s.overview.touched = true
		widget := g.GetWidget()
		if widget == nil {
			return
		}
		defer widget.Unref()
		views := []*workspaceOverview{o}
		for _, view := range o.views {
			views = append(views, view)
		}
		for _, view := range views {
			if view.win != nil && view.win.GoPointer() == widget.GoPointer() {
				s.selectOverviewInput(view.monitor)
				break
			}
		}
	}
	o.onDragPrepare = func(source gtk.DragSource, x, y float64) gdk.ContentProvider {
		o.dragX, o.dragY = x, y
		if o.killMode || o.movesPending > 0 {
			return gdk.ContentProvider{}
		}
		content := source.GetContent()
		if content == nil {
			return gdk.ContentProvider{}
		}
		return *content // transfer full to GTK
	}
	o.onDragBegin = func(source gtk.DragSource, _ uintptr) {
		widget := source.GetWidget()
		if widget == nil {
			return
		}
		defer widget.Unref()
		o.dragging = strings.TrimPrefix(widget.GetName(), "window:")
		log.Printf("overview: drag begin address=%s", o.dragging)
		preview := gtk.NewWidgetPaintable(widget)
		still := preview.GetCurrentImage()
		preview.Unref()
		source.SetIcon(still, int32(o.dragX), int32(o.dragY))
		gobject.ObjectNewFromInternalPtr(still.GoPointer()).Unref()
		widget.AddCssClass("dragging")
	}
	o.onDragEnd = func(source gtk.DragSource, _ uintptr, _ bool) {
		if widget := source.GetWidget(); widget != nil {
			widget.RemoveCssClass("dragging")
			widget.Unref()
		}
		log.Printf("overview: drag end address=%s", o.dragging)
		o.dragging = ""
		// Cross-monitor drops run through GTK's drag loop on two layer
		// surfaces. Rebuild only after that loop has unwound; compositor
		// events that arrive while a move is in flight must not Clear the
		// source/drop widgets or the shell freezes.
		gtkx.Post(func() {
			if o.st.Open && o.dragging == "" {
				s.renderOverview()
				s.drainOverviewFrames()
			}
		})
	}
	o.onWindowGesture = func(g gtk.GestureClick, _ int32, _, _ float64) {
		if g.GetCurrentButton() != 2 {
			return
		}
		widget := g.GetWidget()
		if widget == nil {
			return
		}
		defer widget.Unref()
		address := strings.TrimPrefix(widget.GetName(), "window:")
		s.overviewDo(i18n.T("close window"), func(ctx context.Context) error { return s.comp.CloseWindow(ctx, address) })
		g.SetState(gtk.EventSequenceClaimedValue)
	}
	o.onOverviewScroll = func(_ gtk.EventControllerScroll, _, dy float64) bool {
		if o.searchMode || o.dragging != "" {
			return false
		}
		delta := 1
		if dy < 0 {
			delta = -1
		}
		s.navigateOverview(delta)
		s.renderOverview()
		return true
	}
	o.onOverviewHover = func(c gtk.EventControllerMotion, _, _ float64) {
		if o.dragging != "" {
			return
		}
		widget := c.GetWidget()
		if widget == nil {
			return
		}
		defer widget.Unref()
		address := strings.TrimPrefix(widget.GetName(), "window:")
		for i, card := range o.st.Cards {
			for _, w := range card.Windows {
				if w.ID == address {
					o.st.Selected = i
					o.st.FocusedMon = card.Workspace.Output
					for ptr, dest := range o.targets {
						widget := gtk.Widget{}
						widget.Ptr = ptr
						if dest.ID == card.Workspace.ID && !dest.New {
							widget.AddCssClass("selected")
						} else {
							widget.RemoveCssClass("selected")
						}
					}
					if o.hint != nil {
						o.hint.SetText(i18n.Tf("%s · %s · Middle click: close", w.Title, w.AppID))
					}
					s.updateOverviewCapture()
					return
				}
			}
		}
	}
	o.onOverviewRight = func(g gtk.GestureClick, _ int32, _, _ float64) {
		if o.killMode {
			s.setOverviewKillMode(false)
		} else if o.searchMode {
			o.searchMode = false
			input := s.overviewInput()
			input.query.SetText("")
			input.root.GrabFocus()
		} else {
			s.closeOverview(true)
		}
		g.SetState(gtk.EventSequenceClaimedValue)
	}
}

func (s *shellUI) setOverviewKillMode(armed bool) {
	o := &s.overview
	o.killMode = armed
	views := []*workspaceOverview{o}
	for _, view := range o.views {
		views = append(views, view)
	}
	for _, view := range views {
		if view.win == nil {
			continue
		}
		view.killGlyph.SetVisible(armed)
		if armed {
			view.win.SetCursorFromName("none")
			view.win.AddCssClass("kill-mode")
			view.hint.SetText(i18n.T("Click a window to force-terminate ONLY that client · Esc / right click cancels"))
		} else {
			view.win.SetCursorFromName("default")
			view.win.RemoveCssClass("kill-mode")
			view.hint.SetText(i18n.T("Arrows / Tab · select    Type or / · search    Middle click · close    Ctrl+Shift+X · kill    Drag · move"))
		}
	}
}

func (s *shellUI) nextOverviewWorkspaceID(monitor string) (string, bool) {
	if s.hypr != nil {
		s.hypr.FreshWorkspace = s.freshWorkspace()
	}
	return s.overview.st.ReserveWorkspace(s.snap, time.Now(), monitor)
}

// Drops never follow a window or switch the user's active workspace. A new
// target is allocated at commit time and routed by its owning monitor.
func (s *shellUI) moveOverviewWindow(address string, target overviewDropTarget, x, y float64) bool {
	o := &s.overview
	window, ok := s.snap.Window(address)
	if !ok {
		return false
	}
	id := target.ID
	if target.New {
		if id, ok = s.nextOverviewWorkspaceID(target.Monitor); !ok {
			return false
		}
	}
	log.Printf("overview: drop address=%s source=%s target=%s monitor=%s new=%v", address, window.Workspace, id, target.Monitor, target.New)
	if id == "" {
		return false
	}
	if id == window.Workspace {
		if !window.Floating || s.hypr == nil {
			return false
		}
		scale := max(0.001, target.Scale)
		px, py := int(max(0, (x-s.overview.dragX)/scale)), int(max(0, (y-s.overview.dragY)/scale))
		s.overviewDo(i18n.T("position floating window"), func(ctx context.Context) error {
			return s.hypr.Dispatch(ctx, fmt.Sprintf("movewindowpixel exact %d %d,address:%s", px, py, address),
				fmt.Sprintf(`hl.dsp.window.move({window=%q,x=%q,y=%q})`, "address:"+address, strconv.Itoa(px), strconv.Itoa(py)))
		})
		return true
	}
	if !s.caps.Has(compositor.CapMoveWithoutFollow) {
		return false
	}
	mover, _ := s.comp.(compositor.WorkspaceMover)
	monitor := target.Monitor
	source := window.Workspace
	o.st.BeginMove(address, id, monitor, source, time.Now())
	o.movesPending++
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.comp.MoveWindow(ctx, address, id, false); err != nil {
			log.Printf("overview move: %v", err)
		} else if monitor != "" && mover != nil {
			if err := mover.MoveWorkspace(ctx, id, monitor); err != nil {
				log.Printf("overview move monitor: %v", err)
			}
		}
		gtkx.Post(func() {
			if o.movesPending > 0 {
				o.movesPending--
			}
			if o.movesPending == 0 && o.st.Open {
				o.st.SetSnapshot(s.snap)
				s.updateOverviewCapture()
				s.renderOverview()
			}
		})
	}()
	return true
}

func (s *shellUI) navigateOverview(delta int) {
	o := &s.overview
	if !s.cfg.Overview.PerMonitor {
		o.st.Move(delta)
		return
	}
	indices := []int{}
	position := 0
	for i, card := range o.st.Cards {
		if card.Workspace.Output == o.st.FocusedMon {
			if i == o.st.Selected {
				position = len(indices)
			}
			indices = append(indices, i)
		}
	}
	if len(indices) == 0 {
		return
	}
	position = ((position+delta)%len(indices) + len(indices)) % len(indices)
	o.st.Selected = indices[position]
}

func (s *shellUI) overviewInput() *workspaceOverview {
	if view := s.overview.views[s.overview.inputMonitor]; view != nil {
		return view
	}
	return &s.overview
}

// Guard only the two observed standard mouse bindings, leaving custom user
// mouse shortcuts intact. Restore locally without a compositor reload.
func (s *shellUI) guardOverviewMouse(enabled bool) {
	o := &s.overview
	if s.hypr == nil || o.mouseGuard == enabled {
		return
	}
	o.mouseGuard = enabled
	client := s.hypr.Client
	func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if enabled {
			data, err := client.Request(ctx, "j/binds")
			if err != nil {
				return
			}
			var binds []struct {
				Key, Description string
				Modmask          int
				Mouse            bool
			}
			if json.Unmarshal(data, &binds) != nil {
				return
			}
			script := `_G.inuaOverviewMouseRestore = _G.inuaOverviewMouseRestore or {}; `
			for _, bind := range binds {
				if bind.Modmask != 64 {
					continue
				}
				action := ""
				if bind.Key == "mouse:272" && bind.Description == "Move window" {
					action = "drag"
				}
				if bind.Key == "mouse:273" && bind.Description == "Resize window" {
					action = "resize"
				}
				if action == "" {
					continue
				}
				expression := "SUPER + " + bind.Key
				restore := fmt.Sprintf(`hl.bind(%q,hl.dsp.window.%s(),{mouse=%t,description=%q})`, expression, action, bind.Mouse, bind.Description)
				script += fmt.Sprintf(`_G.inuaOverviewMouseRestore[%q]=%q;hl.unbind(%q);`, expression, restore, expression)
			}
			_, _ = client.Request(ctx, "eval "+script)
		} else {
			_, _ = client.Request(ctx, `eval if _G.inuaOverviewMouseRestore then for _,restore in pairs(_G.inuaOverviewMouseRestore) do local f=load(restore);if f then f() end end;_G.inuaOverviewMouseRestore=nil end`)
		}
	}()
}

// Dispatch before dismissing the exclusive layer. Closing first races the
// compositor's focus restoration and can make a click appear ineffective.
func (s *shellUI) activateOverview(monitor string, steps ...func(ctx context.Context) error) {
	if s.comp == nil {
		return
	}
	generation := s.overview.activation
	focuser, _ := s.comp.(compositor.OutputFocuser)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if monitor != "" && focuser != nil {
			if err := focuser.FocusOutput(ctx, monitor); err != nil {
				log.Printf("overview activate monitor: %v", err)
				return
			}
		}
		for _, step := range steps {
			if err := step(ctx); err != nil {
				log.Printf("overview activate: %v", err)
				return
			}
		}
		gtkx.Post(func() {
			if s.overview.st.Open && s.overview.activation == generation {
				s.closeOverview(false)
			}
		})
	}()
}

func (s *shellUI) overviewWindowMonitor(address string) string {
	for _, card := range s.overview.st.AllCards {
		for _, window := range card.Windows {
			if window.ID == address {
				return card.Workspace.Output
			}
		}
	}
	return ""
}

// overviewWorkspaceByName is the id and the monitor of the card named
// name (a workspace's name, or its id).
func (s *shellUI) overviewWorkspaceByName(name string) (id, monitor string) {
	for _, card := range s.overview.st.AllCards {
		if !card.New && (card.Workspace.Name == name || card.Workspace.ID == name) {
			return card.Workspace.ID, card.Workspace.Output
		}
	}
	return "", ""
}

func overviewHistoryPath() string {
	root := os.Getenv("XDG_STATE_HOME")
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".local/state")
	}
	return filepath.Join(root, "inua/overview-history.json")
}

// loadOverviewHistory reads the workspaces last used, as ids; a file from
// before ids were strings holds Hyprland's numbers, read as their text.
func (s *shellUI) loadOverviewHistory() {
	data, err := os.ReadFile(overviewHistoryPath())
	if err != nil {
		return
	}
	var raw []any
	if json.Unmarshal(data, &raw) == nil {
		s.overview.st.History = s.overview.st.History[:0]
		for _, v := range raw {
			switch id := v.(type) {
			case string:
				s.overview.st.History = append(s.overview.st.History, id)
			case float64:
				s.overview.st.History = append(s.overview.st.History, strconv.Itoa(int(id)))
			}
		}
	}
	s.overview.historyLast = string(data)
}
func (s *shellUI) saveOverviewHistory() {
	data, err := json.Marshal(s.overview.st.History)
	if err != nil || string(data) == s.overview.historyLast {
		return
	}
	s.overview.historyLast = string(data)
	path := overviewHistoryPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	if err := os.WriteFile(path+".tmp", data, 0o600); err == nil {
		_ = os.Rename(path+".tmp", path)
	}
}

func (s *shellUI) overviewKeyboardMode() layershell.KeyboardMode {
	// An exclusive layer can prevent pointer input reaching sibling overview
	// surfaces on other outputs. Let the compositor focus whichever surface
	// the user interacts with instead of grabbing the seat for one output,
	// where it focuses an on-demand surface as it appears (Hyprland).
	// Elsewhere (niri) such a surface waits for a click, and the overview
	// must take keys at once.
	if s.caps.Has(compositor.CapOnDemandFocus) {
		return layershell.KeyboardOnDemand
	}
	return layershell.KeyboardExclusive
}

// Track the physical surface under the pointer for keyboard navigation.
// Every surface remains OnDemand so choosing an input monitor never grabs
// pointer input away from the other displays.
func (s *shellUI) selectOverviewInput(monitor string) {
	o := &s.overview
	if monitor == "" || monitor == o.inputMonitor {
		return
	}
	o.inputMonitor = monitor
	if s.cfg.Overview.PerMonitor {
		o.st.FocusedMon = monitor
	}
	views := []*workspaceOverview{o}
	for _, view := range o.views {
		views = append(views, view)
	}
	for _, view := range views {
		if view.win == nil {
			continue
		}
		if view.monitor == monitor {
			if o.searchMode {
				view.query.GrabFocus()
			} else {
				view.root.GrabFocus()
			}
		}
	}
}
