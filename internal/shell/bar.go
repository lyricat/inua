package shell

import (
	"math"
	"slices"

	"github.com/lyricat/inua/internal/hypr"
)

// knownMonitor reports whether Hyprland has a monitor with the connector
// name GDK gave a bar. Without layer-shell (a nested or foreign compositor)
// the names can differ; such a bar shows everything.
func knownMonitor(snap hypr.Snapshot, connector string) bool {
	return slices.ContainsFunc(snap.Monitors, func(m hypr.Monitor) bool { return m.Name == connector })
}

// BarWorkspaces lists the workspaces the bar on a monitor shows: the normal
// (positive id) workspaces on that monitor, or on all monitors, by id.
func BarWorkspaces(snap hypr.Snapshot, connector string, all bool) []hypr.Workspace {
	all = all || !knownMonitor(snap, connector)
	var out []hypr.Workspace
	for _, w := range snap.Workspaces {
		if w.ID > 0 && (all || w.Monitor == connector) {
			out = append(out, w)
		}
	}
	slices.SortFunc(out, func(a, b hypr.Workspace) int { return a.ID - b.ID })
	return out
}

// BarTitle is the window title the bar on a monitor shows: the focused
// window on the focused monitor, elsewhere the window last focused on that
// monitor's visible workspace.
func BarTitle(snap hypr.Snapshot, connector string) string {
	return BarWindow(snap, connector).Title
}

// BarWindow is the window the bar on a monitor names: the focused window's
// on the focused monitor, elsewhere the window last focused on that
// monitor's visible workspace. The bar shows the application's icon and
// name from it, not the window's title. Its zero value is no window.
func BarWindow(snap hypr.Snapshot, connector string) hypr.Toplevel {
	if !knownMonitor(snap, connector) || connector == snap.FocusedMonitor {
		for _, t := range snap.Toplevels {
			if t.Address == snap.ActiveToplevel {
				return t
			}
		}
		return hypr.Toplevel{}
	}
	ws := 0
	for _, m := range snap.Monitors {
		if m.Name == connector {
			ws = m.ActiveWorkspaceID
		}
	}
	found, best := hypr.Toplevel{}, -1
	for i, t := range snap.Toplevels {
		if t.WorkspaceID != ws {
			continue
		}
		if h := focusHistory(t, len(snap.Toplevels)+i); best < 0 || h < best {
			found, best = t, h
		}
	}
	return found
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
