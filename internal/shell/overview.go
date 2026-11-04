package shell

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/compositor"
)

// Overview is the workspace and window overview shown above the compositor.
// It owns selection and focus rules; the GTK view only renders Cards and
// dispatches the returned action.
type Overview struct {
	Open       bool
	Cards      []OverviewCard
	AllCards   []OverviewCard
	Selected   int
	Query      string
	Previous   string
	FocusedMon string
	MRU        bool
	History    []string // workspace ids, the most recently focused first
	// NewWorkspace names the workspace a New drop on an output creates
	// (compositor.WorkspaceMover); nil: no New.
	NewWorkspace func(snap compositor.Snapshot, output string, unavailable func(id string) bool) (compositor.Workspace, bool)

	snapshot     compositor.Snapshot
	frozenOrder  []string
	reservations map[string]time.Time
	moves        []overviewMove
}

// overviewMove is a window relocation the compositor has not yet confirmed.
// The view shows the destination immediately so a drop onto New does not flash
// an empty card while the compositor creates and routes the workspace.
type overviewMove struct {
	Window    string
	Workspace string
	Monitor   string
	Source    string
	Until     time.Time
}

type OverviewCard struct {
	Workspace compositor.Workspace
	Windows   []compositor.Window
	New       bool
}

func (o *Overview) Show(snap compositor.Snapshot) {
	o.frozenOrder = nil
	o.Open = true
	o.Query = ""
	o.snapshot = snap
	o.Previous = snap.FocusedWindow
	o.FocusedMon = snap.FocusedOutput
	o.refresh(snap)
	o.freezeOrder()
	o.Selected = 0
	for i, c := range o.Cards {
		if c.New && c.Workspace.Output == snap.FocusedOutput {
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

func (o *Overview) SetSnapshot(snap compositor.Snapshot) {
	if !o.Open {
		if w, ok := snap.Window(snap.FocusedWindow); ok && w.Workspace != "" {
			o.History = slices.DeleteFunc(o.History, func(id string) bool { return id == w.Workspace })
			o.History = append([]string{w.Workspace}, o.History...)
		}
		o.snapshot = snap
		o.FocusedMon = snap.FocusedOutput
		return
	}
	var selected OverviewCard
	if o.Selected >= 0 && o.Selected < len(o.Cards) {
		selected = o.Cards[o.Selected]
	}
	o.snapshot = snap
	o.FocusedMon = snap.FocusedOutput
	o.refresh(snap)
	for i, c := range o.Cards {
		if c.Workspace.ID == selected.Workspace.ID && c.Workspace.Output == selected.Workspace.Output && c.New == selected.New {
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
			if selected != "" && w.ID == selected {
				o.Selected = i
				return
			}
		}
	}
	o.Selected = max(0, min(o.Selected, len(o.Cards)-1))
}

func (o *Overview) refresh(snap compositor.Snapshot) {
	o.settleMoves(snap, time.Now())
	workspaces, windows := o.projected(snap)
	o.AllCards = nil
	for _, ws := range workspaces {
		if ws.ID == "" || ws.Output == "" {
			continue
		}
		card := OverviewCard{Workspace: ws}
		for _, w := range windows {
			if w.Workspace == ws.ID {
				card.Windows = append(card.Windows, w)
			}
		}
		slices.SortFunc(card.Windows, func(a, b compositor.Window) int {
			if a.Focused != b.Focused {
				if a.Focused {
					return -1
				}
				return 1
			}
			return strings.Compare(a.Title, b.Title)
		})
		if len(card.Windows) > 0 {
			o.AllCards = append(o.AllCards, card)
		}
	}

	if o.NewWorkspace != nil {
		for _, mon := range snap.Outputs {
			o.AllCards = append(o.AllCards, OverviewCard{Workspace: compositor.Workspace{Output: mon.Name}, New: true})
		}
	}
	slices.SortFunc(o.AllCards, func(a, b OverviewCard) int {
		if a.Workspace.Output != b.Workspace.Output {
			return strings.Compare(a.Workspace.Output, b.Workspace.Output)
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
		return workspaceOrder(a.Workspace, b.Workspace)
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

// workspaceOrder sorts workspaces as the compositor orders them, ids
// deciding between equals.
func workspaceOrder(a, b compositor.Workspace) int {
	if a.Order != b.Order {
		return a.Order - b.Order
	}
	return strings.Compare(a.ID, b.ID)
}

// workspaceHistory is how long ago a card's workspace was in use: the best
// focus rank of its windows, the largest int when none has one.
func workspaceHistory(c OverviewCard) int {
	best := int(^uint(0) >> 1)
	for _, w := range c.Windows {
		if w.FocusRank > 0 && w.FocusRank < best {
			best = w.FocusRank
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
			if strings.Contains(strings.ToLower(w.Title), q) || strings.Contains(strings.ToLower(w.AppID), q) {
				filtered.Windows = append(filtered.Windows, w)
			}
		}
		if len(filtered.Windows) > 0 {
			o.Cards = append(o.Cards, filtered)
		}
	}
}

// SelectedAddress is the id of the selected card's first window, "" for
// none.
func (o *Overview) SelectedAddress() string {
	if o.Selected < 0 || o.Selected >= len(o.Cards) || len(o.Cards[o.Selected].Windows) == 0 {
		return ""
	}
	return o.Cards[o.Selected].Windows[0].ID
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
// screenshot before the compositor's events arrive. The emptied source
// stays hidden.
func (o *Overview) BeginMove(window, workspace, monitor, source string, now time.Time) {
	if window == "" || workspace == "" {
		return
	}
	o.moves = slices.DeleteFunc(o.moves, func(m overviewMove) bool { return m.Window == window })
	o.moves = append(o.moves, overviewMove{
		Window: window, Workspace: workspace, Monitor: monitor, Source: source, Until: now.Add(5 * time.Second),
	})
	if o.reservations == nil {
		o.reservations = map[string]time.Time{}
	}
	o.reservations[workspace] = now.Add(5 * time.Second)
	o.refresh(o.snapshot)
}

func (o *Overview) settleMoves(snap compositor.Snapshot, now time.Time) {
	o.moves = slices.DeleteFunc(o.moves, func(m overviewMove) bool {
		if !now.Before(m.Until) {
			return true
		}
		w, ok := snap.Window(m.Window)
		if !ok || w.Workspace != m.Workspace {
			return false
		}
		if m.Monitor == "" {
			return true
		}
		ws, ok := snap.Workspace(m.Workspace)
		return ok && ws.Output == m.Monitor
	})
}

func (o *Overview) projected(snap compositor.Snapshot) ([]compositor.Workspace, []compositor.Window) {
	workspaces := append([]compositor.Workspace(nil), snap.Workspaces...)
	windows := append([]compositor.Window(nil), snap.Windows...)
	if len(o.moves) == 0 {
		return workspaces, windows
	}
	byID := make(map[string]int, len(windows))
	for i := range windows {
		byID[windows[i].ID] = i
	}
	for _, m := range o.moves {
		if i, ok := byID[m.Window]; ok {
			windows[i].Workspace = m.Workspace
			if m.Monitor != "" {
				windows[i].Output = m.Monitor
			}
		}
		found := false
		for i := range workspaces {
			if workspaces[i].ID != m.Workspace {
				continue
			}
			if m.Monitor != "" {
				workspaces[i].Output = m.Monitor
			}
			found = true
			break
		}
		if !found {
			order, _ := strconv.Atoi(m.Workspace)
			workspaces = append(workspaces, compositor.Workspace{
				ID: m.Workspace, Name: m.Workspace, Output: m.Monitor, Order: order,
			})
		}
	}
	return workspaces, windows
}

// Select selects a card and says what activating it means: its
// workspace, and its first window's id when it has one.
func (o *Overview) Select(index int) (compositor.Workspace, string, bool) {
	if index < 0 || index >= len(o.Cards) {
		return compositor.Workspace{}, "", false
	}
	o.Selected = index
	c := o.Cards[index]
	if c.New {
		return c.Workspace, "", true
	}
	if len(c.Windows) > 0 {
		return c.Workspace, c.Windows[0].ID, true
	}
	return c.Workspace, "", true
}

// BarWorkspaces is the workspaces a display shows in the bar: the bar's own
// list (BarWorkspaces above), in the order this model uses — the MRU history
// when it is on, the workspace's own order otherwise. A workspace with no
// windows keeps its mark: the bar says where the display can be, while the
// overview's cards say where the windows are.
func (o Overview) BarWorkspaces(snap compositor.Snapshot, connector string, all bool) []compositor.Workspace {
	workspaces := BarWorkspaces(snap, connector, all)
	if !o.MRU {
		return workspaces
	}
	slices.SortFunc(workspaces, func(a, b compositor.Workspace) int {
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
		return workspaceOrder(a, b)
	})
	return workspaces
}

// WorkspaceSlot resolves the unfiltered visual slot, including New, without
// opening the overview or changing the current search/selection.
func (o Overview) WorkspaceSlot(snap compositor.Snapshot, monitor string, slot int) (OverviewCard, bool) {
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
		if card.Workspace.Output != monitor {
			continue
		}
		slot--
		if slot == 0 {
			return card, true
		}
	}
	return OverviewCard{}, false
}

// ReserveWorkspace names the workspace a New drop on preferMonitor goes
// to, asking the compositor's NewWorkspace with everything reserved, in
// flight or shown kept out; the id is held for five seconds. false when
// the compositor cannot name one.
func (o *Overview) ReserveWorkspace(snap compositor.Snapshot, now time.Time, preferMonitor string) (string, bool) {
	if o.NewWorkspace == nil {
		return "", false
	}
	if o.reservations == nil {
		o.reservations = map[string]time.Time{}
	}
	o.settleMoves(snap, now)
	for reserved, deadline := range o.reservations {
		if !now.Before(deadline) {
			delete(o.reservations, reserved)
		}
	}
	unavailable := map[string]bool{}
	for _, m := range o.moves {
		unavailable[m.Workspace] = true
	}
	for reserved := range o.reservations {
		unavailable[reserved] = true
	}
	_, windows := o.projected(snap)
	projected := snap
	projected.Windows = windows
	ws, ok := o.NewWorkspace(projected, preferMonitor, func(id string) bool { return unavailable[id] })
	if !ok {
		return "", false
	}
	o.reservations[ws.ID] = now.Add(5 * time.Second)
	return ws.ID, true
}
