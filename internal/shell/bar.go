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

// BarSpot is a thing shown on the bar, in the bar's order from the left.
type BarSpot struct {
	Rect Rect
	// Reach is a panel's button, whose reach goes past it (BarReach).
	Reach bool
	// Break is what the reaches do not run across: a space, the clock,
	// the workspaces, the app's title or menu.
	Break bool
}

// BarReach finds which of the bar's buttons with a reach a point in the
// bar's window is in the reach of, as an index into spots, and whether
// it is on the button itself (which takes its own clicks). Each reaches
// over the window's full height, up to the screen's edge, and across to
// halfway over the gap to the next button with a reach, when nothing that
// breaks the reaches is between them; otherwise half the gap past it. A
// button that is the first thing on the bar reaches to the window's left
// edge, the last to its right edge: the screen's corners. -1 for none.
func BarReach(spots []BarSpot, gap, x, y float64) (i int, onButton bool) {
	// neighbour is the button with a reach that the reach of the one at i
	// joins in the direction step, -1 for none.
	neighbour := func(i, step int) int {
		for j := i + step; j >= 0 && j < len(spots); j += step {
			switch {
			case spots[j].Break:
				return -1
			case spots[j].Reach:
				return j
			}
		}
		return -1
	}
	for i, sp := range spots {
		if !sp.Reach {
			continue
		}
		b := sp.Rect
		left := b.X - gap/2
		switch p := neighbour(i, -1); {
		case p >= 0:
			left = (spots[p].Rect.X + spots[p].Rect.W + b.X) / 2
		case i == 0:
			left = math.Inf(-1)
		}
		right := b.X + b.W + gap/2
		switch n := neighbour(i, 1); {
		case n >= 0:
			right = (b.X + b.W + spots[n].Rect.X) / 2
		case i == len(spots)-1:
			right = math.Inf(1)
		}
		if x >= left && x < right {
			return i, b.Contains(x, y)
		}
	}
	return -1, false
}
