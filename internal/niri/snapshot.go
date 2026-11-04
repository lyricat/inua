package niri

import (
	"encoding/json"
	"sort"
	"strconv"

	"github.com/lyricat/inua/internal/compositor"
)

// The IPC's objects (niri-ipc), the fields inua reads.

type window struct {
	ID          uint64     `json:"id"`
	Title       *string    `json:"title"`
	AppID       *string    `json:"app_id"`
	PID         *int       `json:"pid"`
	WorkspaceID *uint64    `json:"workspace_id"`
	IsFocused   bool       `json:"is_focused"`
	IsFloating  bool       `json:"is_floating"`
	IsUrgent    bool       `json:"is_urgent"`
	Layout      layout     `json:"layout"`
	FocusStamp  *timestamp `json:"focus_timestamp"`
}

type layout struct {
	TilePos      *[2]float64 `json:"tile_pos_in_workspace_view"`
	WindowSize   [2]int      `json:"window_size"`
	WindowOffset [2]float64  `json:"window_offset_in_tile"`
}

type timestamp struct {
	Secs  uint64 `json:"secs"`
	Nanos uint32 `json:"nanos"`
}

func (t timestamp) less(u timestamp) bool {
	return t.Secs < u.Secs || t.Secs == u.Secs && t.Nanos < u.Nanos
}

type workspace struct {
	ID             uint64  `json:"id"`
	Idx            int     `json:"idx"`
	Name           *string `json:"name"`
	Output         *string `json:"output"`
	IsUrgent       bool    `json:"is_urgent"`
	IsActive       bool    `json:"is_active"`
	IsFocused      bool    `json:"is_focused"`
	ActiveWindowID *uint64 `json:"active_window_id"`
}

type output struct {
	Name        string `json:"name"`
	Make        string `json:"make"`
	Model       string `json:"model"`
	Modes       []mode `json:"modes"`
	CurrentMode *int   `json:"current_mode"`
	Logical     *struct {
		X, Y          int
		Width, Height int
		Scale         float64
		Transform     string
	} `json:"logical"`
}

type mode struct {
	Width, Height int
	RefreshRate   int `json:"refresh_rate"` // millihertz
}

// transforms are niri's Transform names as wl_output numbers them.
var transforms = map[string]int{"Normal": 0, "90": 1, "180": 2, "270": 3, "Flipped": 4, "Flipped90": 5, "Flipped180": 6, "Flipped270": 7}

// state is what the event stream has told us.
type state struct {
	outputs    map[string]output
	workspaces []workspace
	windows    map[uint64]window
	ready      bool // both WorkspacesChanged and WindowsChanged seen
	haveWs     bool
	haveWin    bool
}

func newState() *state {
	return &state{outputs: map[string]output{}, windows: map[uint64]window{}}
}

// apply folds one event in. It reports whether the snapshot may differ.
func (s *state) apply(ev map[string]json.RawMessage) bool {
	changed := false
	for name, data := range ev {
		switch name {
		case "WorkspacesChanged":
			var v struct{ Workspaces []workspace }
			if json.Unmarshal(data, &v) == nil {
				s.workspaces, s.haveWs, changed = v.Workspaces, true, true
			}
		case "WorkspaceActivated":
			var v struct {
				ID      uint64
				Focused bool
			}
			if json.Unmarshal(data, &v) != nil {
				continue
			}
			var out *string
			for _, w := range s.workspaces {
				if w.ID == v.ID {
					out = w.Output
				}
			}
			for i := range s.workspaces {
				w := &s.workspaces[i]
				if sameOutput(w.Output, out) {
					w.IsActive = w.ID == v.ID
				}
				if v.Focused {
					w.IsFocused = w.ID == v.ID
				}
			}
			changed = true
		case "WorkspaceUrgencyChanged":
			var v struct {
				ID     uint64
				Urgent bool
			}
			if json.Unmarshal(data, &v) == nil {
				for i := range s.workspaces {
					if s.workspaces[i].ID == v.ID {
						s.workspaces[i].IsUrgent = v.Urgent
					}
				}
				changed = true
			}
		case "WorkspaceActiveWindowChanged":
			var v struct {
				WorkspaceID    uint64  `json:"workspace_id"`
				ActiveWindowID *uint64 `json:"active_window_id"`
			}
			if json.Unmarshal(data, &v) == nil {
				for i := range s.workspaces {
					if s.workspaces[i].ID == v.WorkspaceID {
						s.workspaces[i].ActiveWindowID = v.ActiveWindowID
					}
				}
			}
		case "WindowsChanged":
			var v struct{ Windows []window }
			if json.Unmarshal(data, &v) == nil {
				s.windows = map[uint64]window{}
				for _, w := range v.Windows {
					s.windows[w.ID] = w
				}
				s.haveWin, changed = true, true
			}
		case "WindowOpenedOrChanged":
			var v struct{ Window window }
			if json.Unmarshal(data, &v) == nil {
				if v.Window.IsFocused {
					for id, w := range s.windows {
						w.IsFocused = false
						s.windows[id] = w
					}
				}
				s.windows[v.Window.ID] = v.Window
				changed = true
			}
		case "WindowClosed":
			var v struct{ ID uint64 }
			if json.Unmarshal(data, &v) == nil {
				delete(s.windows, v.ID)
				changed = true
			}
		case "WindowFocusChanged":
			var v struct{ ID *uint64 }
			if json.Unmarshal(data, &v) == nil {
				for id, w := range s.windows {
					w.IsFocused = v.ID != nil && *v.ID == id
					s.windows[id] = w
				}
				changed = true
			}
		case "WindowFocusTimestampChanged":
			var v struct {
				ID    uint64
				Stamp *timestamp `json:"focus_timestamp"`
			}
			if json.Unmarshal(data, &v) == nil {
				if w, ok := s.windows[v.ID]; ok {
					w.FocusStamp = v.Stamp
					s.windows[v.ID] = w
					changed = true
				}
			}
		case "WindowUrgencyChanged":
			var v struct {
				ID     uint64
				Urgent bool
			}
			if json.Unmarshal(data, &v) == nil {
				if w, ok := s.windows[v.ID]; ok {
					w.IsUrgent = v.Urgent
					s.windows[v.ID] = w
					changed = true
				}
			}
		case "WindowLayoutsChanged":
			var v struct {
				Changes [][2]json.RawMessage `json:"changes"`
			}
			if json.Unmarshal(data, &v) == nil {
				for _, c := range v.Changes {
					var id uint64
					var l layout
					if json.Unmarshal(c[0], &id) == nil && json.Unmarshal(c[1], &l) == nil {
						if w, ok := s.windows[id]; ok {
							w.Layout = l
							s.windows[id] = w
							changed = true
						}
					}
				}
			}
		}
	}
	s.ready = s.haveWs && s.haveWin
	return changed
}

func sameOutput(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func id(n uint64) string { return strconv.FormatUint(n, 10) }

// snapshot builds the contract's view of the state.
func (s *state) snapshot() compositor.Snapshot {
	var snap compositor.Snapshot
	wsOutput := map[uint64]string{}
	active := map[string]string{} // output -> active workspace id
	for _, w := range s.workspaces {
		wsOutput[w.ID] = str(w.Output)
		if w.IsActive {
			active[str(w.Output)] = id(w.ID)
		}
		if w.IsFocused {
			snap.FocusedOutput = str(w.Output)
		}
	}

	names := make([]string, 0, len(s.outputs))
	for name := range s.outputs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		o := s.outputs[name]
		if o.Logical == nil {
			continue // off
		}
		out := compositor.Output{
			Name: o.Name, Description: o.Make + " " + o.Model,
			X: o.Logical.X, Y: o.Logical.Y, Width: o.Logical.Width, Height: o.Logical.Height,
			Scale: o.Logical.Scale, Transform: transforms[o.Logical.Transform],
			Focused: o.Name == snap.FocusedOutput, ActiveWorkspace: active[o.Name],
		}
		if o.CurrentMode != nil && *o.CurrentMode >= 0 && *o.CurrentMode < len(o.Modes) {
			out.RefreshRate = float64(o.Modes[*o.CurrentMode].RefreshRate) / 1000
		}
		snap.Outputs = append(snap.Outputs, out)
	}

	for _, w := range s.workspaces {
		name := str(w.Name)
		if name == "" {
			name = strconv.Itoa(w.Idx)
		}
		snap.Workspaces = append(snap.Workspaces, compositor.Workspace{
			ID: id(w.ID), Name: name, Output: str(w.Output), Order: w.Idx,
			Active: w.IsActive, Focused: w.IsFocused, Urgent: w.IsUrgent,
		})
	}
	sort.SliceStable(snap.Workspaces, func(i, j int) bool {
		a, b := snap.Workspaces[i], snap.Workspaces[j]
		if a.Output != b.Output {
			return a.Output < b.Output
		}
		return a.Order < b.Order
	})

	ids := make([]uint64, 0, len(s.windows))
	for wid := range s.windows {
		ids = append(ids, wid)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	// Focus ranks: the most recently focused first.
	stamped := make([]uint64, 0, len(ids))
	for _, wid := range ids {
		if s.windows[wid].FocusStamp != nil {
			stamped = append(stamped, wid)
		}
	}
	sort.SliceStable(stamped, func(i, j int) bool {
		return s.windows[stamped[j]].FocusStamp.less(*s.windows[stamped[i]].FocusStamp)
	})
	rank := map[uint64]int{}
	for i, wid := range stamped {
		rank[wid] = i + 1
	}
	for _, wid := range ids {
		w := s.windows[wid]
		cw := compositor.Window{
			ID: id(w.ID), Title: str(w.Title), AppID: str(w.AppID),
			Focused: w.IsFocused, Urgent: w.IsUrgent, Floating: w.IsFloating, FocusRank: rank[wid],
		}
		if w.PID != nil {
			cw.PID = *w.PID
		}
		if w.WorkspaceID != nil {
			cw.Workspace = id(*w.WorkspaceID)
			cw.Output = wsOutput[*w.WorkspaceID]
		}
		if w.IsFocused {
			snap.FocusedWindow = cw.ID
		}
		if w.Layout.TilePos != nil {
			if o, ok := s.outputs[cw.Output]; ok && o.Logical != nil {
				cw.Geometry = &compositor.Rect{
					X: o.Logical.X + int(w.Layout.TilePos[0]+w.Layout.WindowOffset[0]),
					Y: o.Logical.Y + int(w.Layout.TilePos[1]+w.Layout.WindowOffset[1]),
					W: w.Layout.WindowSize[0], H: w.Layout.WindowSize[1],
				}
			}
		}
		snap.Windows = append(snap.Windows, cw)
	}
	return snap
}
