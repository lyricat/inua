package shell

import (
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/hypr"
)

func overviewFixture() hypr.Snapshot {
	return hypr.Snapshot{
		Monitors:   []hypr.Monitor{{Name: "A"}, {Name: "B"}},
		Workspaces: []hypr.Workspace{{ID: 1, Name: "1", Monitor: "A", Focused: true}, {ID: 2, Name: "2", Monitor: "A"}, {ID: 4, Name: "4", Monitor: "B"}, {ID: 12, Name: "12", Monitor: "B"}},
		Toplevels: []hypr.Toplevel{
			{Address: "0x1", Class: "terminal", Title: "first", WorkspaceID: 1, Monitor: "A", Raw: map[string]any{"focusHistoryID": float64(0)}},
			{Address: "0x2", Class: "browser", Title: "second", WorkspaceID: 2, Monitor: "A", Raw: map[string]any{"focusHistoryID": float64(1)}},
			{Address: "0x4", Class: "editor", Title: "fourth", WorkspaceID: 4, Monitor: "B", Raw: map[string]any{"focusHistoryID": float64(2)}},
		},
		ActiveToplevel: "0x1", FocusedMonitor: "A",
	}
}
func overviewIDs(o *Overview, mon string) []int {
	var ids []int
	for _, c := range o.Cards {
		if c.Workspace.Monitor == mon && !c.New {
			ids = append(ids, c.Workspace.ID)
		}
	}
	return ids
}
func TestOverviewMRUFrozenAndSharedWithBar(t *testing.T) {
	snap := overviewFixture()
	o := Overview{MRU: true}
	o.Show(snap)
	snap.Toplevels[0].Raw["focusHistoryID"] = float64(4)
	snap.Toplevels[1].Raw["focusHistoryID"] = float64(0)
	snap.ActiveToplevel = "0x2"
	o.SetSnapshot(snap)
	if got := overviewIDs(&o, "A"); !slices.Equal(got, []int{1, 2}) {
		t.Fatalf("open order changed: %v", got)
	}
	if got := ids(o.BarWorkspaces(snap, "A", false)); !slices.Equal(got, []int{1, 2}) {
		t.Fatalf("bar disagrees: %v", got)
	}
	o.Close()
	o.SetSnapshot(snap)
	o.Show(snap)
	if got := overviewIDs(&o, "A"); !slices.Equal(got, []int{2, 1}) {
		t.Fatalf("closed MRU not promoted: %v", got)
	}
	if got := overviewIDs(&o, "B"); !slices.Equal(got, []int{4}) {
		t.Fatalf("MRU order within B: %v", got)
	}
}
func TestOverviewSelectionSurvivesWorkspaceRemoval(t *testing.T) {
	snap := overviewFixture()
	o := Overview{}
	o.Show(snap)
	for i, c := range o.Cards {
		if c.New && c.Workspace.Monitor == "B" {
			o.Selected = i
		}
	}
	snap.Workspaces = snap.Workspaces[1:]
	snap.Toplevels = snap.Toplevels[1:]
	o.SetSnapshot(snap)
	if c := o.Cards[o.Selected]; !c.New || c.Workspace.Monitor != "B" {
		t.Fatalf("new-card selection lost: %+v", c)
	}
}
func TestOverviewHidesEmptyWorkspacesButBarShowsThem(t *testing.T) {
	snap := overviewFixture()
	snap.Workspaces = append(snap.Workspaces, hypr.Workspace{ID: 3, Name: "3", Monitor: "A"})
	o := Overview{}
	o.Show(snap)
	if got := overviewIDs(&o, "A"); !slices.Equal(got, []int{1, 2}) {
		t.Fatalf("overview shows empty workspace: %v", got)
	}
	if got := ids(o.BarWorkspaces(snap, "A", false)); !slices.Equal(got, []int{1, 2, 3}) {
		t.Fatalf("bar hides empty workspace: %v", got)
	}
}
func TestOverviewMRUBarIncludesEmptyWorkspaces(t *testing.T) {
	snap := overviewFixture()
	snap.Workspaces = append(snap.Workspaces, hypr.Workspace{ID: 3, Name: "3", Monitor: "A"})
	o := Overview{MRU: true, History: []int{2, 1}}
	if got := ids(o.BarWorkspaces(snap, "A", false)); !slices.Equal(got, []int{2, 1, 3}) {
		t.Fatalf("MRU bar workspaces: %v", got)
	}
}
func TestOverviewNeverShowsEmptyWorkspacesAndKeepsMonitorGroups(t *testing.T) {
	snap := overviewFixture()
	snap.Workspaces = append(snap.Workspaces, hypr.Workspace{ID: 3, Name: "3", Monitor: "A"}, hypr.Workspace{ID: 10, Name: "10", Monitor: "B"})
	snap.Workspaces = append(snap.Workspaces, hypr.Workspace{ID: 22, Name: "22", Monitor: "B"})
	snap.Toplevels = append(snap.Toplevels, hypr.Toplevel{Address: "0x22", Class: "editor", Title: "last", WorkspaceID: 22, Monitor: "B", Raw: map[string]any{"focusHistoryID": float64(0)}})
	for _, mru := range []bool{false, true} {
		o := Overview{MRU: mru}
		o.Show(snap)
		wantB := []int{4, 10, 12, 22}
		if got := ids(o.BarWorkspaces(snap, "B", false)); !slices.Equal(got, wantB) {
			t.Fatalf("bar B (mru=%v): %v, want %v", mru, got, wantB)
		}
		lastMonitor := ""
		newSeen := map[string]bool{}
		for _, card := range o.Cards {
			mon := card.Workspace.Monitor
			if mon != lastMonitor && newSeen[mon] {
				t.Fatalf("monitor group resumed after another display: %+v", card)
			}
			if mon != lastMonitor {
				lastMonitor = mon
			}
			if card.New {
				newSeen[mon] = true
				continue
			}
			if len(card.Windows) == 0 {
				t.Fatalf("empty workspace displayed with MRU=%v: %+v", mru, card)
			}
		}
		if len(newSeen) != 2 {
			t.Fatalf("missing per-monitor New targets: %v", newSeen)
		}
		a, b := overviewIDs(&o, "A"), overviewIDs(&o, "B")
		if mru {
			if !slices.Equal(a, []int{1, 2}) || !slices.Equal(b, []int{22, 4}) {
				t.Fatalf("MRU leaked across monitors: A=%v B=%v", a, b)
			}
		} else if !slices.Equal(a, []int{1, 2}) || !slices.Equal(b, []int{4, 22}) {
			t.Fatalf("numeric order leaked across monitors: A=%v B=%v", a, b)
		}
	}
}

func TestOverviewNavigationNormalizesLargeNegativeDelta(t *testing.T) {
	o := Overview{}
	o.Show(overviewFixture())
	o.Move(-1000)
	if o.Selected < 0 || o.Selected >= len(o.Cards) {
		t.Fatalf("invalid selection: %d", o.Selected)
	}
}
func TestOverviewSearchDoesNotFilterBarSlots(t *testing.T) {
	snap := overviewFixture()
	o := Overview{MRU: true}
	o.Show(snap)
	o.SetQuery("browser")
	if len(o.Cards) != 1 || o.Cards[0].Workspace.ID != 2 {
		t.Fatalf("search: %+v", o.Cards)
	}
	if got := ids(o.BarWorkspaces(snap, "A", false)); !slices.Equal(got, []int{1, 2}) {
		t.Fatalf("query changed slots: %v", got)
	}
}
func TestOverviewMRUEmptyFocusedMonitorSelectsNew(t *testing.T) {
	snap := overviewFixture()
	snap.FocusedMonitor = "B"
	snap.Workspaces[0].Focused = false
	snap.Workspaces[3].Focused = true
	snap.Toplevels = snap.Toplevels[:2]
	o := Overview{MRU: true}
	o.Show(snap)
	if c := o.Cards[o.Selected]; !c.New || c.Workspace.Monitor != "B" {
		t.Fatalf("wrong fallback selection: %+v", c)
	}
}

func TestOverviewSlotIgnoresSearchAndInvalidSlotIsNoop(t *testing.T) {
	snap := overviewFixture()
	o := Overview{MRU: true}
	o.Show(snap)
	o.SetQuery("browser")
	if c, ok := o.WorkspaceSlot(snap, "A", 1); !ok || c.Workspace.ID != 1 {
		t.Fatalf("slot followed query: %+v %v", c, ok)
	}
	if c, ok := o.WorkspaceSlot(snap, "B", 2); !ok || !c.New || c.Workspace.Monitor != "B" {
		t.Fatalf("new belongs to wrong display: %+v %v", c, ok)
	}
	if _, ok := o.WorkspaceSlot(snap, "B", 10); ok {
		t.Fatal("out-of-range slot resolved")
	}
	if o.Query != "browser" || len(o.Cards) != 1 {
		t.Fatal("slot lookup mutated search")
	}
}

func TestOverviewNewReservationsDoNotCollide(t *testing.T) {
	snap := overviewFixture()
	o := Overview{}
	now := time.Unix(100, 0)
	first := o.ReserveWorkspace(snap, false, now, "B")
	second := o.ReserveWorkspace(snap, false, now, "B")
	if first != 12 {
		t.Fatalf("empty workspace on B should be recycled: %d", first)
	}
	if second == first || second <= 0 {
		t.Fatalf("rapid New IDs collided: %d %d", first, second)
	}
	if expired := o.ReserveWorkspace(snap, false, now.Add(6*time.Second), "B"); expired != 12 {
		t.Fatalf("failed reservation did not expire: %d", expired)
	}
	empty := Overview{}
	if id := empty.ReserveWorkspace(hypr.Snapshot{}, true, now, ""); id != 11 {
		t.Fatalf("native reserved default slots: %d", id)
	}
}

func TestOverviewReservePrefersEmptyWorkspaceOnTargetMonitor(t *testing.T) {
	snap := overviewFixture()
	snap.Workspaces = append(snap.Workspaces, hypr.Workspace{ID: 3, Name: "3", Monitor: "A"})
	o := Overview{}
	now := time.Now()
	if id := o.ReserveWorkspace(snap, false, now, "A"); id != 3 {
		t.Fatalf("should recycle empty workspace on A: %d", id)
	}
}

func TestOverviewPendingMoveOccupiesNewCardOnTargetMonitor(t *testing.T) {
	snap := overviewFixture()
	o := Overview{}
	o.Show(snap)
	now := time.Now()
	id := o.ReserveWorkspace(snap, false, now, "A")
	o.BeginMove("0x4", id, "A", 4, now)
	var moved OverviewCard
	for _, c := range o.Cards {
		if !c.New && c.Workspace.ID == id {
			moved = c
		}
		if !c.New && c.Workspace.ID == 4 {
			t.Fatalf("emptied source workspace still shown: %+v", c)
		}
	}
	if moved.Workspace.Monitor != "A" || len(moved.Windows) != 1 || moved.Windows[0].Address != "0x4" {
		t.Fatalf("pending New drop did not occupy target: %+v", moved)
	}
	if c, ok := o.WorkspaceSlot(snap, "A", 4); !ok || !c.New || c.Workspace.Monitor != "A" {
		t.Fatalf("target monitor lost its trailing New card: %+v %v", c, ok)
	}
	snap.Toplevels[2].WorkspaceID = id
	snap.Toplevels[2].Monitor = "A"
	found := false
	for i, ws := range snap.Workspaces {
		if ws.ID == id {
			snap.Workspaces[i].Monitor = "A"
			found = true
		}
	}
	if !found {
		snap.Workspaces = append(snap.Workspaces, hypr.Workspace{ID: id, Name: strconv.Itoa(id), Monitor: "A"})
	}
	o.SetSnapshot(snap)
	if len(o.moves) != 0 {
		t.Fatalf("settled move still pending: %+v", o.moves)
	}
}

func TestOverviewCountsWindowByWorkspaceIDEvenWhenClientMonitorDiffers(t *testing.T) {
	snap := overviewFixture()
	snap.Toplevels = append(snap.Toplevels, hypr.Toplevel{Address: "0x3", Class: "floating", Title: "pinned", WorkspaceID: 3, Monitor: "B"})
	snap.Workspaces = append(snap.Workspaces, hypr.Workspace{ID: 3, Name: "3", Monitor: "A"})
	o := Overview{}
	o.Show(snap)
	if _, ok := o.WorkspaceSlot(snap, "A", 2); !ok {
		t.Fatal("occupied workspace was hidden because the client monitor differed")
	}
}

func TestOverviewNumberSlotsStayOnFocusedMonitorWithMRUOff(t *testing.T) {
	snap := overviewFixture()
	snap.Workspaces = append(snap.Workspaces, hypr.Workspace{ID: 3, Name: "3", Monitor: "A"}, hypr.Workspace{ID: 10, Name: "10", Monitor: "B"})
	snap.Toplevels = append(snap.Toplevels,
		hypr.Toplevel{Address: "0x3", Class: "term", Title: "third", WorkspaceID: 3, Monitor: "A"},
		hypr.Toplevel{Address: "0x10", Class: "term", Title: "tenth", WorkspaceID: 10, Monitor: "B"})
	o := Overview{MRU: false}
	for slot, want := range []int{4, 10} {
		card, ok := o.WorkspaceSlot(snap, "B", slot+1)
		if !ok || card.Workspace.ID != want || card.Workspace.Monitor != "B" {
			t.Fatalf("B slot %d: %+v %v", slot+1, card, ok)
		}
	}
	if card, ok := o.WorkspaceSlot(snap, "B", 3); !ok || !card.New || card.Workspace.Monitor != "B" {
		t.Fatalf("B new-workspace slot escaped monitor: %+v %v", card, ok)
	}
	for slot, want := range []int{1, 2, 3} {
		card, ok := o.WorkspaceSlot(snap, "A", slot+1)
		if !ok || card.Workspace.ID != want || card.Workspace.Monitor != "A" {
			t.Fatalf("A slot %d: %+v %v", slot+1, card, ok)
		}
	}
}

func TestOverviewGroupsWindowsByAppWithoutWorkspaces(t *testing.T) {
	snap := hypr.Snapshot{
		Toplevels: []hypr.Toplevel{
			{Address: "wf:3", Class: "files", Title: "Files A", Monitor: "HDMI-A-1"},
			{Address: "wf:5", Class: "files", Title: "Files B", Monitor: "HDMI-A-1", Activated: true},
		},
		ActiveToplevel: "wf:5", FocusedMonitor: "HDMI-A-1",
	}
	var o Overview
	o.Show(snap)
	if len(o.Cards) != 1 || len(o.Cards[0].Windows) != 1 {
		t.Fatalf("cards = %+v", o.Cards)
	}
	if o.Cards[o.Selected].Windows[0].Address != "wf:5" {
		t.Fatalf("selected = %+v", o.Cards[o.Selected])
	}
	if o.SelectedAddress() != "wf:5" {
		t.Fatalf("selected address = %q", o.SelectedAddress())
	}
}
