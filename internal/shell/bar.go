package shell

import (
	"math"
	"slices"

	"github.com/lyricat/inua/internal/compositor"
)

// knownMonitor reports whether the compositor has an output with the
// connector name GDK gave a bar. Without layer-shell (a nested or foreign
// compositor) the names can differ; such a bar shows everything.
func knownMonitor(snap compositor.Snapshot, connector string) bool {
	_, ok := snap.Output(connector)
	return ok
}

// BarWorkspaces lists the workspaces the bar on a monitor shows: those on
// that monitor, or on all monitors, in the compositor's order.
func BarWorkspaces(snap compositor.Snapshot, connector string, all bool) []compositor.Workspace {
	all = all || !knownMonitor(snap, connector)
	var out []compositor.Workspace
	for _, w := range snap.Workspaces {
		if all || w.Output == connector {
			out = append(out, w)
		}
	}
	slices.SortFunc(out, workspaceOrder)
	return out
}

// BarWindow is the window a monitor's bar speaks for: the focused window
// when it is that monitor's, elsewhere the window last focused on the
// monitor's visible workspace. A bar never takes the focused window of
// another monitor, so moving the focus across monitors leaves it unchanged;
// a snapshot keeps the four queries it takes apart, and the focused output
// may already be the new one when the active window is still the old one.
func BarWindow(snap compositor.Snapshot, connector string) (compositor.Window, bool) {
	if !knownMonitor(snap, connector) {
		return snap.Window(snap.FocusedWindow)
	}
	if w, ok := snap.Window(snap.FocusedWindow); ok && windowOnMonitor(snap, w, connector) {
		return w, true
	}
	ws := snap.ActiveWorkspace(connector)
	if ws == "" {
		return compositor.Window{}, false
	}
	var best compositor.Window
	rank := -1
	for i, t := range snap.Windows {
		if t.Workspace != ws {
			continue
		}
		if h := focusHistory(t, len(snap.Windows)+i); rank < 0 || h < rank {
			best, rank = t, h
		}
	}
	return best, rank >= 0
}

// windowOnMonitor reports whether w is on the monitor named connector. A
// window's output is set, or the workspace it is on says which monitor
// shows it.
func windowOnMonitor(snap compositor.Snapshot, w compositor.Window, connector string) bool {
	if w.Output != "" {
		return w.Output == connector
	}
	ws, ok := snap.Workspace(w.Workspace)
	return ok && ws.Output == connector
}

// BarTitle is the window title the bar on a monitor shows.
func BarTitle(snap compositor.Snapshot, connector string) string {
	w, _ := BarWindow(snap, connector)
	return w.Title
}

// BarReach finds which of the bar's buttons at its end, left to right, a
// point in the bar's window is in the reach of, and whether it is on the
// button itself (which takes its own clicks). Each reaches over the
// window's full height, up to the screen's edge, and across from halfway
// over the gap before it to halfway over the gap after it; the first from
// half of gap before it, the last to the window's right edge, the
// screen's corner. -1 for none.
func BarReach(buttons []Rect, gap, x, y float64) (i int, onButton bool) {
	for i, b := range buttons {
		left := b.X - gap/2
		if i > 0 {
			p := buttons[i-1]
			left = (p.X + p.W + b.X) / 2
		}
		right := math.Inf(1)
		if i+1 < len(buttons) {
			right = (b.X + b.W + buttons[i+1].X) / 2
		}
		if x >= left && x < right {
			return i, b.Contains(x, y)
		}
	}
	return -1, false
}
