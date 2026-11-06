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

// BarWindow is the window the bar on a monitor shows: the focused window
// on the focused monitor, elsewhere the window last focused on that
// monitor's visible workspace.
func BarWindow(snap compositor.Snapshot, connector string) (compositor.Window, bool) {
	if !knownMonitor(snap, connector) || connector == snap.FocusedOutput {
		return snap.Window(snap.FocusedWindow)
	}
	ws := snap.ActiveWorkspace(connector)
	if ws == "" {
		return compositor.Window{}, false
	}
	best := -1
	var window compositor.Window
	for i, w := range snap.Windows {
		if w.Workspace != ws {
			continue
		}
		if h := focusHistory(w, len(snap.Windows)+i); best < 0 || h < best {
			window, best = w, h
		}
	}
	return window, best >= 0
}

// BarTitle is the title of the window the bar on a monitor shows.
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
