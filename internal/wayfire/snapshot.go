package wayfire

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/lyricat/inua/internal/compositor"
)

// rect is a place in whole logical pixels. Wayfire writes one as numbers
// that are a hair off whole on a scaled output (2357.99995), which would
// lose a pixel when cut to an int.
type rect struct{ X, Y, Width, Height float64 }

func (r *rect) UnmarshalJSON(b []byte) error {
	var raw struct{ X, Y, Width, Height float64 }
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*r = rect{math.Round(raw.X), math.Round(raw.Y), math.Round(raw.Width), math.Round(raw.Height)}
	return nil
}

type cell struct {
	X          int `json:"x"`
	Y          int `json:"y"`
	GridWidth  int `json:"grid_width"`
	GridHeight int `json:"grid_height"`
}

type output struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Geometry  rect   `json:"geometry"`
	Workarea  rect   `json:"workarea"`
	WSet      int    `json:"wset-index"`
	Workspace cell   `json:"workspace"`
}

type view struct {
	ID         int    `json:"id"`
	PID        int    `json:"pid"`
	Title      string `json:"title"`
	AppID      string `json:"app-id"`
	Geometry   rect   `json:"geometry"`
	OutputID   int    `json:"output-id"`
	Output     string `json:"output-name"`
	LastFocus  int64  `json:"last-focus-timestamp"`
	Role       string `json:"role"`
	Mapped     bool   `json:"mapped"`
	Activated  bool   `json:"activated"`
	TiledEdge  int    `json:"tiled-edges"`
	Fullscreen bool   `json:"fullscreen"`
	Minimized  bool   `json:"minimized"`
	WSet       index  `json:"wset-index"`
	Type       string `json:"type"`
}

// index is a workspace set's index, -1 for a view in none (the shell's
// own surfaces). Wayfire 0.9 writes that one as 2^64-1, which no int
// takes, and one such view would lose the whole list.
type index int

func (i *index) UnmarshalJSON(b []byte) error {
	f, err := strconv.ParseFloat(string(b), 64)
	if err != nil || f < 0 || f > math.MaxInt32 {
		*i = -1
		return nil
	}
	*i = index(f)
	return nil
}

// state is one read of Wayfire.
type state struct {
	outputs []output
	views   []view
	focused int // focused output's id, 0 unknown
}

// workspaceID names a grid cell of a workspace set: "SET:X,Y".
func workspaceID(wset, x, y int) string { return fmt.Sprintf("%d:%d,%d", wset, x, y) }

// parseWorkspace reads a workspaceID back.
func parseWorkspace(id string) (wset, x, y int, err error) {
	set, xy, ok := strings.Cut(id, ":")
	xs, ys, ok2 := strings.Cut(xy, ",")
	if ok && ok2 {
		var e1, e2, e3 error
		wset, e1 = strconv.Atoi(set)
		x, e2 = strconv.Atoi(xs)
		y, e3 = strconv.Atoi(ys)
		if e1 == nil && e2 == nil && e3 == nil {
			return wset, x, y, nil
		}
	}
	return 0, 0, 0, fmt.Errorf("wayfire: workspace %q: want SET:X,Y", id)
}

func floorDiv(a, b float64) int {
	if b <= 0 {
		return 0
	}
	q := int(a / b)
	if a < 0 && float64(q)*b != a {
		q--
	}
	return q
}

// cellOf is the grid cell a view is on: its place is relative to the cell
// its output shows, a cell to the right being one output width further.
func cellOf(v view, o output) (x, y int) {
	x = o.Workspace.X + floorDiv(v.Geometry.X+v.Geometry.Width/2, o.Geometry.Width)
	y = o.Workspace.Y + floorDiv(v.Geometry.Y+v.Geometry.Height/2, o.Geometry.Height)
	x = max(0, min(x, max(o.Workspace.GridWidth, 1)-1))
	y = max(0, min(y, max(o.Workspace.GridHeight, 1)-1))
	return x, y
}

// isWindow tells an app's window from the shell's own surfaces and from
// what is not shown yet.
func isWindow(v view) bool {
	return v.Mapped && v.Role == "toplevel" && v.Type == "toplevel"
}

func (s state) snapshot() compositor.Snapshot {
	var snap compositor.Snapshot
	byID := map[int]output{}
	outs := append([]output(nil), s.outputs...)
	sort.Slice(outs, func(i, j int) bool { return outs[i].ID < outs[j].ID })
	for _, o := range outs {
		byID[o.ID] = o
		focused := o.ID == s.focused
		if focused {
			snap.FocusedOutput = o.Name
		}
		// The geometry is logical already; the scale is GDK's to know.
		snap.Outputs = append(snap.Outputs, compositor.Output{
			Name: o.Name, X: int(o.Geometry.X), Y: int(o.Geometry.Y),
			Width: int(o.Geometry.Width), Height: int(o.Geometry.Height), Scale: 1,
			Focused: focused, ActiveWorkspace: workspaceID(o.WSet, o.Workspace.X, o.Workspace.Y),
		})
		for y := 0; y < o.Workspace.GridHeight; y++ {
			for x := 0; x < o.Workspace.GridWidth; x++ {
				n := y*o.Workspace.GridWidth + x
				active := x == o.Workspace.X && y == o.Workspace.Y
				snap.Workspaces = append(snap.Workspaces, compositor.Workspace{
					ID: workspaceID(o.WSet, x, y), Name: strconv.Itoa(n + 1), Output: o.Name, Order: n,
					Active: active, Focused: active && focused,
				})
			}
		}
	}

	views := make([]view, 0, len(s.views))
	for _, v := range s.views {
		if isWindow(v) {
			views = append(views, v)
		}
	}
	sort.SliceStable(views, func(i, j int) bool { return views[i].ID < views[j].ID })
	ranked := append([]view(nil), views...)
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].LastFocus > ranked[j].LastFocus })
	rank := map[int]int{}
	for i, v := range ranked {
		if v.LastFocus > 0 {
			rank[v.ID] = i + 1
		}
	}
	for _, v := range views {
		w := compositor.Window{
			ID: strconv.Itoa(v.ID), PID: v.PID, Title: v.Title, AppID: v.AppID, Output: v.Output,
			Focused: v.Activated, Floating: v.TiledEdge == 0, FocusRank: rank[v.ID],
		}
		if w.Title == "nil" { // what Wayfire reports before a title is set
			w.Title = ""
		}
		if o, ok := byID[v.OutputID]; ok {
			x, y := cellOf(v, o)
			w.Workspace = workspaceID(int(v.WSet), x, y)
			// The place within the window's own cell, in global pixels.
			w.Geometry = &compositor.Rect{
				X: int(o.Geometry.X+v.Geometry.X) - (x-o.Workspace.X)*int(o.Geometry.Width),
				Y: int(o.Geometry.Y+v.Geometry.Y) - (y-o.Workspace.Y)*int(o.Geometry.Height),
				W: int(v.Geometry.Width), H: int(v.Geometry.Height),
			}
		}
		if v.Activated {
			snap.FocusedWindow = w.ID
		}
		snap.Windows = append(snap.Windows, w)
	}
	return snap
}

// read asks Wayfire for the outputs, the views and the focused output.
func (c *Client) read(ctx context.Context) (state, error) {
	var st state
	body, err := c.Call(ctx, "window-rules/list-outputs", nil)
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(body, &st.outputs); err != nil {
		return st, fmt.Errorf("wayfire: list-outputs: %w", err)
	}
	if body, err = c.Call(ctx, "window-rules/list-views", nil); err != nil {
		return st, err
	}
	if err := json.Unmarshal(body, &st.views); err != nil {
		return st, fmt.Errorf("wayfire: list-views: %w", err)
	}
	// No output is focused before the first one appears: not an error.
	if body, err = c.Call(ctx, "window-rules/get-focused-output", nil); err == nil {
		var r struct {
			Info *struct{ ID int } `json:"info"`
		}
		if json.Unmarshal(body, &r) == nil && r.Info != nil {
			st.focused = r.Info.ID
		}
	}
	return st, nil
}
