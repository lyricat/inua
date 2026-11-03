package shell

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/hypr"
)

// Overview is the workspace and window overview shown above the compositor.
// It owns selection and focus rules; the GTK view only renders Cards and
// dispatches the returned action.
type Overview struct {
	Open         bool
	Cards        []OverviewCard
	AllCards     []OverviewCard
	Selected     int
	Query        string
	Previous     string
	FocusedMon   string
	MRU          bool
	History      []int
	snapshot     hypr.Snapshot
	frozenOrder  []int
	reservations map[int]time.Time
	moves        []overviewMove
}

// overviewMove is a window relocation the compositor has not yet confirmed.
// The view shows the destination immediately so a drop onto New does not flash
// an empty card while Hyprland creates and routes the workspace.
type overviewMove struct {
	Address   string
	Workspace int
	Monitor   string
	Source    int
	Until     time.Time
}

type OverviewCard struct {
	Workspace hypr.Workspace
	Windows   []hypr.Toplevel
	New       bool
}

func (o *Overview) Show(snap hypr.Snapshot) {
	o.frozenOrder = nil
	o.Open = true
	o.Query = ""
	o.snapshot = snap
	o.Previous = snap.ActiveToplevel
	o.FocusedMon = snap.FocusedMonitor
	o.refresh(snap)
	o.freezeOrder()
	o.Selected = 0
	for i, c := range o.Cards {
		if c.New && c.Workspace.Monitor == snap.FocusedMonitor {
			o.Selected = i
		}
	}
	for i, c := range o.Cards {
		if c.Workspace.Focused {
			o.Selected = i
			break
		}
	}
}

func (o *Overview) SetSnapshot(snap hypr.Snapshot) {
	if !o.Open {
		for _, w := range snap.Toplevels {
			if w.Address == snap.ActiveToplevel {
				o.History = slices.DeleteFunc(o.History, func(id int) bool { return id == w.WorkspaceID })
				o.History = append([]int{w.WorkspaceID}, o.History...)
				break
			}
		}
		o.snapshot = snap
		o.FocusedMon = snap.FocusedMonitor
		return
	}
	var selected OverviewCard
	if o.Selected >= 0 && o.Selected < len(o.Cards) {
		selected = o.Cards[o.Selected]
	}
	o.snapshot = snap
	o.FocusedMon = snap.FocusedMonitor
	o.refresh(snap)
	for i, c := range o.Cards {
		if c.Workspace.ID == selected.Workspace.ID && c.Workspace.Monitor == selected.Workspace.Monitor && c.New == selected.New {
			o.Selected = i
			return
		}
	}

	o.Selected = max(0, min(o.Selected, len(o.Cards)-1))
}

func (o *Overview) SetMRU(enabled bool) {
	if o.MRU == enabled {
		return
	}
	o.MRU = enabled
	o.frozenOrder = nil
	o.refresh(o.snapshot)
	o.freezeOrder()
}

func (o *Overview) SetQuery(query string) {
	o.Query = strings.TrimSpace(query)
	selected := o.SelectedAddress()
	o.filter()
	for i, c := range o.Cards {
		for _, w := range c.Windows {
			if selected != "" && w.Address == selected {
				o.Selected = i
				return
			}
		}
	}
	o.Selected = max(0, min(o.Selected, len(o.Cards)-1))
}

func (o *Overview) refresh(snap hypr.Snapshot) {
	o.settleMoves(snap, time.Now())
	workspaces, toplevels := o.projected(snap)
	o.AllCards = nil
	if len(workspaces) == 0 && len(toplevels) > 0 {
		// Wayfire: no tiling workspaces to group by, so one card per window.
		for i, window := range toplevels {
			workspace := hypr.Workspace{
				ID: i + 1, Name: strconv.Itoa(i + 1), Monitor: window.Monitor,
				Active: window.Activated, Focused: window.Activated,
			}
			o.AllCards = append(o.AllCards, OverviewCard{Workspace: workspace, Windows: []hypr.Toplevel{window}})
		}
	} else {
		for _, ws := range workspaces {
			if ws.ID <= 0 || ws.Monitor == "" {
				continue
			}
			card := OverviewCard{Workspace: ws}
			for _, w := range toplevels {
				if w.WorkspaceID == ws.ID {
					card.Windows = append(card.Windows, w)
				}
			}
			slices.SortFunc(card.Windows, func(a, b hypr.Toplevel) int {
				if a.Activated != b.Activated {
					if a.Activated {
						return -1
					}
					return 1
				}
				if a.Title < b.Title {
					return -1
				}
				if a.Title > b.Title {
					return 1
				}
				return 0
			})
			if len(card.Windows) > 0 {
				o.AllCards = append(o.AllCards, card)
			}
		}

		for _, mon := range snap.Monitors {
			o.AllCards = append(o.AllCards, OverviewCard{Workspace: hypr.Workspace{Monitor: mon.Name}, New: true})
		}
	}
	slices.SortFunc(o.AllCards, func(a, b OverviewCard) int {
		if a.Workspace.Monitor != b.Workspace.Monitor {
			if a.Workspace.Monitor < b.Workspace.Monitor {
				return -1
			}
			return 1
		}
		if a.New != b.New {
			if a.New {
				return 1
			}
			return -1
		}
		if o.MRU {
			order := o.History
			if o.Open && len(o.frozenOrder) > 0 {
				order = o.frozenOrder
			}
			ha, hb := slices.Index(order, a.Workspace.ID), slices.Index(order, b.Workspace.ID)
			if ha < 0 {
				ha = 100000 + min(workspaceHistory(a), 100000)
			}
			if hb < 0 {
				hb = 100000 + min(workspaceHistory(b), 100000)
			}
			if ha != hb {
				if ha < hb {
					return -1
				}
				return 1
			}
		}
		if a.Workspace.ID < b.Workspace.ID {
			return -1
		}
		if a.Workspace.ID > b.Workspace.ID {
			return 1
		}
		return 0
	})
	if o.Open && o.MRU && len(o.frozenOrder) > 0 {
		for _, card := range o.AllCards {
			if !card.New && !slices.Contains(o.frozenOrder, card.Workspace.ID) {
				o.frozenOrder = append(o.frozenOrder, card.Workspace.ID)
			}
		}
	}
	o.filter()
	if len(o.Cards) == 0 {
		o.Selected = 0
	} else {
		o.Selected = max(0, min(o.Selected, len(o.Cards)-1))
	}
}

func workspaceHistory(c OverviewCard) int {
	best := int(^uint(0) >> 1)
	for _, w := range c.Windows {
		var n int
		switch v := w.Raw["focusHistoryID"].(type) {
		case float64:
			n = int(v)
		case int:
			n = v
		default:
			continue
		}
		if n < best {
			best = n
		}
	}
	return best
}

func (o *Overview) filter() {
	q := strings.ToLower(o.Query)
	o.Cards = o.Cards[:0]
	for _, c := range o.AllCards {
		if q == "" {
			o.Cards = append(o.Cards, c)
			continue
		}
		if c.New {
			continue
		}
		filtered := OverviewCard{Workspace: c.Workspace}
		for _, w := range c.Windows {
			if strings.Contains(strings.ToLower(w.Title), q) || strings.Contains(strings.ToLower(w.Class), q) {
				filtered.Windows = append(filtered.Windows, w)
			}
		}
		if len(filtered.Windows) > 0 {
			o.Cards = append(o.Cards, filtered)
		}
	}
}

func (o *Overview) SelectedAddress() string {
	if o.Selected < 0 || o.Selected >= len(o.Cards) || len(o.Cards[o.Selected].Windows) == 0 {
		return ""
	}
	return o.Cards[o.Selected].Windows[0].Address
}

func (o *Overview) Move(delta int) {
	if len(o.Cards) == 0 {
		return
	}
	o.Selected = ((o.Selected+delta)%len(o.Cards) + len(o.Cards)) % len(o.Cards)
}

func (o *Overview) freezeOrder() {
	if !o.Open || !o.MRU {
		return
	}
	for _, card := range o.AllCards {
		if !card.New {
			o.frozenOrder = append(o.frozenOrder, card.Workspace.ID)
		}
	}
}

func (o *Overview) Close() {
	o.Open = false
	o.frozenOrder = nil
	o.moves = nil
}

// BeginMove records a drop so the destination card shows the window and
// screenshot before Hyprland events arrive. The emptied source stays hidden.
func (o *Overview) BeginMove(address string, workspace int, monitor string, source int, now time.Time) {
	if address == "" || workspace <= 0 {
		return
	}
	o.moves = slices.DeleteFunc(o.moves, func(m overviewMove) bool { return m.Address == address })
	o.moves = append(o.moves, overviewMove{
		Address: address, Workspace: workspace, Monitor: monitor, Source: source, Until: now.Add(5 * time.Second),
	})
	if o.reservations == nil {
		o.reservations = map[int]time.Time{}
	}
	o.reservations[workspace] = now.Add(5 * time.Second)
	o.refresh(o.snapshot)
}

func (o *Overview) settleMoves(snap hypr.Snapshot, now time.Time) {
	o.moves = slices.DeleteFunc(o.moves, func(m overviewMove) bool {
		if !now.Before(m.Until) {
			return true
		}
		onWorkspace := false
		for _, w := range snap.Toplevels {
			if w.Address == m.Address && w.WorkspaceID == m.Workspace {
				onWorkspace = true
				break
			}
		}
		if !onWorkspace {
			return false
		}
		if m.Monitor == "" {
			return true
		}
		for _, ws := range snap.Workspaces {
			if ws.ID == m.Workspace {
				return ws.Monitor == m.Monitor
			}
		}
		return false
	})
}

func (o *Overview) projected(snap hypr.Snapshot) ([]hypr.Workspace, []hypr.Toplevel) {
	workspaces := append([]hypr.Workspace(nil), snap.Workspaces...)
	toplevels := append([]hypr.Toplevel(nil), snap.Toplevels...)
	if len(o.moves) == 0 {
		return workspaces, toplevels
	}
	byAddr := make(map[string]int, len(toplevels))
	for i := range toplevels {
		byAddr[toplevels[i].Address] = i
	}
	for _, m := range o.moves {
		if i, ok := byAddr[m.Address]; ok {
			toplevels[i].WorkspaceID = m.Workspace
			if m.Monitor != "" {
				toplevels[i].Monitor = m.Monitor
			}
		}
		found := false
		for i := range workspaces {
			if workspaces[i].ID != m.Workspace {
				continue
			}
			if m.Monitor != "" {
				workspaces[i].Monitor = m.Monitor
			}
			found = true
			break
		}
		if !found {
			workspaces = append(workspaces, hypr.Workspace{
				ID: m.Workspace, Name: strconv.Itoa(m.Workspace), Monitor: m.Monitor,
			})
		}
	}
	return workspaces, toplevels
}

func (o *Overview) Select(index int) (hypr.Workspace, string, bool) {
	if index < 0 || index >= len(o.Cards) {
		return hypr.Workspace{}, "", false
	}
	o.Selected = index
	c := o.Cards[index]
	if c.New {
		return c.Workspace, "", true
	}
	if len(c.Windows) > 0 {
		return c.Workspace, c.Windows[0].Address, true
	}
	return c.Workspace, "", true
}

// BarWorkspaces is the workspaces a display shows in the bar: the bar's own
// list (BarWorkspaces above), in the order this model uses — the MRU history
// when it is on, the workspace's own order otherwise. A workspace with no
// windows keeps its mark: the bar says where the display can be, while the
// overview's cards say where the windows are.
func (o Overview) BarWorkspaces(snap hypr.Snapshot, connector string, all bool) []hypr.Workspace {
	workspaces := BarWorkspaces(snap, connector, all)
	if !o.MRU {
		return workspaces
	}
	slices.SortFunc(workspaces, func(a, b hypr.Workspace) int {
		ha, hb := slices.Index(o.History, a.ID), slices.Index(o.History, b.ID)
		if ha < 0 {
			ha = 100000 + min(workspaceHistory(OverviewCard{Workspace: a}), 100000)
		}
		if hb < 0 {
			hb = 100000 + min(workspaceHistory(OverviewCard{Workspace: b}), 100000)
		}
		if ha != hb {
			return ha - hb
		}
		return a.ID - b.ID
	})
	return workspaces
}

// WorkspaceSlot resolves the unfiltered visual slot, including New, without
// opening the overview or changing the current search/selection.
func (o Overview) WorkspaceSlot(snap hypr.Snapshot, monitor string, slot int) (OverviewCard, bool) {
	if slot < 1 {
		return OverviewCard{}, false
	}
	o.Query = ""
	o.Cards = nil
	o.AllCards = nil
	o.frozenOrder = slices.Clone(o.frozenOrder)
	o.moves = slices.Clone(o.moves)
	o.reservations = maps.Clone(o.reservations)
	o.refresh(snap)
	for _, card := range o.AllCards {
		if card.Workspace.Monitor != monitor {
			continue
		}
		slot--
		if slot == 0 {
			return card, true
		}
	}
	return OverviewCard{}, false
}

// ReserveWorkspace picks an ID for a New drop. Empty non-active Hyprland
// workspaces are recycled so IDs do not ratchet after every move; occupied,
// active, reserved and in-flight destinations stay unavailable. A recycled
// workspace is still moved onto preferMonitor by the drop path.
func (o *Overview) ReserveWorkspace(snap hypr.Snapshot, native bool, now time.Time, preferMonitor string) int {
	if o.reservations == nil {
		o.reservations = map[int]time.Time{}
	}
	o.settleMoves(snap, now)
	for reserved, deadline := range o.reservations {
		if !now.Before(deadline) {
			delete(o.reservations, reserved)
		}
	}
	unavailable := map[int]bool{}
	occupied := map[int]bool{}
	_, toplevels := o.projected(snap)
	for _, w := range toplevels {
		if w.WorkspaceID > 0 {
			occupied[w.WorkspaceID] = true
			unavailable[w.WorkspaceID] = true
		}
	}
	for _, m := range o.moves {
		unavailable[m.Workspace] = true
	}
	for reserved := range o.reservations {
		unavailable[reserved] = true
	}
	for _, mon := range snap.Monitors {
		if mon.ActiveWorkspaceID > 0 {
			unavailable[mon.ActiveWorkspaceID] = true
		}
	}
	for _, ws := range snap.Workspaces {
		if ws.Active && ws.ID > 0 {
			unavailable[ws.ID] = true
		}
	}
	prefer := 0
	recycle := 0
	for _, ws := range snap.Workspaces {
		if ws.ID <= 0 || occupied[ws.ID] || unavailable[ws.ID] {
			continue
		}
		if preferMonitor != "" && ws.Monitor == preferMonitor && prefer == 0 {
			prefer = ws.ID
		}
		if recycle == 0 || ws.ID < recycle {
			recycle = ws.ID
		}
	}
	id := prefer
	if id == 0 {
		id = recycle
	}
	if id == 0 {
		id = 1
		if native {
			id = 11
		}
		for unavailable[id] || occupied[id] {
			id++
		}
	}
	o.reservations[id] = now.Add(5 * time.Second)
	return id
}
