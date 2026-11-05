package hypr

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/lyricat/inua/internal/compositor"
)

// Rect is a window's place, as the contract has it.
type Rect = compositor.Rect

// NormalizeAddress turns "55d1c0" or "0x55D1C0" into "0x55d1c0".
func NormalizeAddress(a string) string {
	a = strings.ToLower(strings.TrimSpace(a))
	if a == "" {
		return ""
	}
	if !strings.HasPrefix(a, "0x") {
		a = "0x" + a
	}
	return a
}

type rawMonitor struct {
	ID              int     `json:"id"`
	Name            string  `json:"name"`
	Description     string  `json:"description"`
	X               int     `json:"x"`
	Y               int     `json:"y"`
	Width           int     `json:"width"`
	Height          int     `json:"height"`
	Scale           float64 `json:"scale"`
	Transform       int     `json:"transform"`
	RefreshRate     float64 `json:"refreshRate"`
	Focused         bool    `json:"focused"`
	Disabled        bool    `json:"disabled"`
	ActiveWorkspace struct {
		ID int `json:"id"`
	} `json:"activeWorkspace"`
	SpecialWorkspace struct {
		ID int `json:"id"`
	} `json:"specialWorkspace"`
}

type rawWorkspace struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Monitor       string `json:"monitor"`
	HasFullscreen bool   `json:"hasfullscreen"`
}

type rawClient struct {
	Address   string `json:"address"`
	PID       int    `json:"pid"`
	Mapped    bool   `json:"mapped"`
	Title     string `json:"title"`
	Class     string `json:"class"`
	Monitor   int    `json:"monitor"`
	Floating  bool   `json:"floating"`
	At        []int  `json:"at"`
	Size      []int  `json:"size"`
	Workspace struct {
		ID int `json:"id"`
	} `json:"workspace"`
	// FocusHistoryID is 0 for the window focused last; -1 when unset.
	FocusHistoryID *int `json:"focusHistoryID"`
}

func decodeList[T any](data []byte) ([]T, error) {
	var out []T
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// QueryResult holds the raw replies of one refresh.
type QueryResult struct {
	Monitors, Workspaces, Clients, ActiveWindow []byte
}

// Query fetches everything a snapshot needs. Requests are separate, so the
// result may straddle a change; the store re-queries on the next event.
func (c *Client) Query(ctx context.Context) (QueryResult, error) {
	var q QueryResult
	var err error
	for _, step := range []struct {
		cmd string
		dst *[]byte
	}{
		{"j/monitors", &q.Monitors},
		{"j/workspaces", &q.Workspaces},
		{"j/clients", &q.Clients},
		{"j/activewindow", &q.ActiveWindow},
	} {
		if *step.dst, err = c.Request(ctx, step.cmd); err != nil {
			return q, fmt.Errorf("%s: %w", step.cmd, err)
		}
	}
	return q, nil
}

// WorkspaceID is the id a Hyprland workspace has in a snapshot: its number
// as text. Special workspaces (negative numbers) are not listed, but a
// window on one carries its id.
func WorkspaceID(id int) string {
	if id == 0 {
		return ""
	}
	return strconv.Itoa(id)
}

// BuildSnapshot derives a snapshot from query replies. urgent holds toplevel
// addresses marked urgent by events since they were last activated.
func BuildSnapshot(q QueryResult, urgent map[string]bool) (compositor.Snapshot, error) {
	var s compositor.Snapshot
	mons, err := decodeList[rawMonitor](q.Monitors)
	if err != nil {
		return s, fmt.Errorf("monitors: %w", err)
	}
	wss, err := decodeList[rawWorkspace](q.Workspaces)
	if err != nil {
		return s, fmt.Errorf("workspaces: %w", err)
	}
	clients, err := decodeList[rawClient](q.Clients)
	if err != nil {
		return s, fmt.Errorf("clients: %w", err)
	}
	var active struct {
		Address string `json:"address"`
	}
	// j/activewindow replies "{}" when nothing is focused.
	if len(q.ActiveWindow) > 0 {
		if err := json.Unmarshal(q.ActiveWindow, &active); err != nil {
			return s, fmt.Errorf("activewindow: %w", err)
		}
	}
	s.FocusedWindow = NormalizeAddress(active.Address)

	// Disabled monitors stay while a workspace is on them and they have a
	// size (a laptop's screen off behind the lid); a disconnected one goes.
	workspaceMonitor := map[string]bool{}
	for _, w := range wss {
		workspaceMonitor[w.Monitor] = true
	}
	sort.Slice(mons, func(i, j int) bool { return mons[i].ID < mons[j].ID })
	monName := map[int]string{}
	shown := map[int]string{} // workspace id -> monitor name
	for _, m := range mons {
		hasGeometry := m.Width > 0 && m.Height > 0
		if m.Disabled && (!workspaceMonitor[m.Name] || !hasGeometry) {
			continue
		}
		monName[m.ID] = m.Name
		shown[m.ActiveWorkspace.ID] = m.Name
		if m.SpecialWorkspace.ID != 0 {
			shown[m.SpecialWorkspace.ID] = m.Name
		}
		if m.Focused {
			s.FocusedOutput = m.Name
		}
		s.Outputs = append(s.Outputs, compositor.Output{
			Name: m.Name, Description: m.Description,
			X: m.X, Y: m.Y, Width: m.Width, Height: m.Height, Scale: m.Scale,
			Transform: m.Transform, RefreshRate: m.RefreshRate, Focused: m.Focused,
			ActiveWorkspace: WorkspaceID(m.ActiveWorkspace.ID), SpecialWorkspace: WorkspaceID(m.SpecialWorkspace.ID),
		})
	}

	urgentWs := map[int]bool{}
	for _, c := range clients {
		if !c.Mapped {
			continue
		}
		addr := NormalizeAddress(c.Address)
		u := urgent[addr]
		if u {
			urgentWs[c.Workspace.ID] = true
		}
		w := compositor.Window{
			ID: addr, PID: c.PID, Title: c.Title, AppID: c.Class,
			Workspace: WorkspaceID(c.Workspace.ID), Output: monName[c.Monitor],
			Focused: addr == s.FocusedWindow, Urgent: u, Floating: c.Floating,
		}
		if c.FocusHistoryID != nil && *c.FocusHistoryID >= 0 {
			w.FocusRank = *c.FocusHistoryID + 1
		}
		if len(c.At) == 2 && len(c.Size) == 2 {
			w.Geometry = &compositor.Rect{X: c.At[0], Y: c.At[1], W: c.Size[0], H: c.Size[1]}
		}
		s.Windows = append(s.Windows, w)
	}

	for _, w := range wss {
		if w.ID <= 0 { // special workspaces are not places to go
			continue
		}
		mon, isShown := shown[w.ID]
		s.Workspaces = append(s.Workspaces, compositor.Workspace{
			ID: WorkspaceID(w.ID), Name: w.Name, Output: w.Monitor, Order: w.ID,
			Active:        isShown,
			Focused:       isShown && mon == s.FocusedOutput && s.FocusedOutput != "",
			HasFullscreen: w.HasFullscreen,
			Urgent:        urgentWs[w.ID],
		})
	}
	sort.Slice(s.Workspaces, func(i, j int) bool { return s.Workspaces[i].Order < s.Workspaces[j].Order })
	return s, nil
}
