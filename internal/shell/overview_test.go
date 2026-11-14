package shell

import (
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/desktop"
)

// ws is a numbered workspace on an output.
func ws(id int, output string) compositor.Workspace {
	return compositor.Workspace{ID: strconv.Itoa(id), Name: strconv.Itoa(id), Output: output, Order: id}
}

// owin is a window on workspace id of an output, focused history-th last.
func owin(addr, class, title string, id int, output string, history int) compositor.Window {
	return compositor.Window{ID: addr, AppID: class, Title: title, Workspace: strconv.Itoa(id), Output: output, FocusRank: history + 1}
}

func overviewFixture() compositor.Snapshot {
	w1 := ws(1, "A")
	w1.Focused = true
	return compositor.Snapshot{
		Outputs:    []compositor.Output{{Name: "A"}, {Name: "B"}},
		Workspaces: []compositor.Workspace{w1, ws(2, "A"), ws(4, "B"), ws(12, "B")},
		Windows: []compositor.Window{
			owin("0x1", "terminal", "first", 1, "A", 0),
			owin("0x2", "browser", "second", 2, "A", 1),
			owin("0x4", "editor", "fourth", 4, "B", 2),
		},
		FocusedWindow: "0x1", FocusedOutput: "A",
	}
}

// numbered is the Overview with a Hyprland-like New: numbers from 1.
func numbered(mru bool) Overview {
	return Overview{MRU: mru, NewWorkspace: func(snap compositor.Snapshot, output string, unavailable func(string) bool) (compositor.Workspace, bool) {
		return compositor.NextNumbered(snap, output, unavailable, 1)
	}}
}

func overviewIDs(o *Overview, mon string) []string {
	var ids []string
	for _, c := range o.Cards {
		if c.Workspace.Output == mon && !c.New {
			ids = append(ids, c.Workspace.ID)
		}
	}
	return ids
}
func TestOverviewMRUFrozenAndSharedWithBar(t *testing.T) {
	snap := overviewFixture()
	o := numbered(true)
	o.Show(snap)
	snap.Windows[0].FocusRank = 5
	snap.Windows[1].FocusRank = 1
	snap.FocusedWindow = "0x2"
	o.SetSnapshot(snap)
	if got := overviewIDs(&o, "A"); !slices.Equal(got, []string{"1", "2"}) {
		t.Fatalf("open order changed: %v", got)
	}
	if got := ids(o.BarWorkspaces(snap, "A", false)); !slices.Equal(got, []string{"1", "2"}) {
		t.Fatalf("bar disagrees: %v", got)
	}
	o.Close()
	o.SetSnapshot(snap)
	o.Show(snap)
	if got := overviewIDs(&o, "A"); !slices.Equal(got, []string{"2", "1"}) {
		t.Fatalf("closed MRU not promoted: %v", got)
	}
	if got := overviewIDs(&o, "B"); !slices.Equal(got, []string{"4"}) {
		t.Fatalf("MRU order within B: %v", got)
	}
}
func TestOverviewSelectionSurvivesWorkspaceRemoval(t *testing.T) {
	snap := overviewFixture()
	o := numbered(false)
	o.Show(snap)
	for i, c := range o.Cards {
		if c.New && c.Workspace.Output == "B" {
			o.Selected = i
		}
	}
	snap.Workspaces = snap.Workspaces[1:]
	snap.Windows = snap.Windows[1:]
	o.SetSnapshot(snap)
	if c := o.Cards[o.Selected]; !c.New || c.Workspace.Output != "B" {
		t.Fatalf("new-card selection lost: %+v", c)
	}
}
func TestOverviewHidesEmptyWorkspacesButBarShowsThem(t *testing.T) {
	snap := overviewFixture()
	snap.Workspaces = append(snap.Workspaces, ws(3, "A"))
	o := numbered(false)
	o.Show(snap)
	if got := overviewIDs(&o, "A"); !slices.Equal(got, []string{"1", "2"}) {
		t.Fatalf("overview shows empty workspace: %v", got)
	}
	if got := ids(o.BarWorkspaces(snap, "A", false)); !slices.Equal(got, []string{"1", "2", "3"}) {
		t.Fatalf("bar hides empty workspace: %v", got)
	}
}
func TestOverviewMRUBarIncludesEmptyWorkspaces(t *testing.T) {
	snap := overviewFixture()
	snap.Workspaces = append(snap.Workspaces, ws(3, "A"))
	o := numbered(true)
	o.History = []string{"2", "1"}
	if got := ids(o.BarWorkspaces(snap, "A", false)); !slices.Equal(got, []string{"2", "1", "3"}) {
		t.Fatalf("MRU bar workspaces: %v", got)
	}
}
func TestOverviewNeverShowsEmptyWorkspacesAndKeepsMonitorGroups(t *testing.T) {
	snap := overviewFixture()
	snap.Workspaces = append(snap.Workspaces, ws(3, "A"), ws(10, "B"))
	snap.Workspaces = append(snap.Workspaces, ws(22, "B"))
	snap.Windows = append(snap.Windows, owin("0x22", "editor", "last", 22, "B", 0))
	for _, mru := range []bool{false, true} {
		o := numbered(mru)
		o.Show(snap)
		wantB := []string{"4", "10", "12", "22"}
		if got := ids(o.BarWorkspaces(snap, "B", false)); !slices.Equal(got, wantB) {
			t.Fatalf("bar B (mru=%v): %v, want %v", mru, got, wantB)
		}
		lastMonitor := ""
		newSeen := map[string]bool{}
		for _, card := range o.Cards {
			mon := card.Workspace.Output
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
			if !slices.Equal(a, []string{"1", "2"}) || !slices.Equal(b, []string{"22", "4"}) {
				t.Fatalf("MRU leaked across monitors: A=%v B=%v", a, b)
			}
		} else if !slices.Equal(a, []string{"1", "2"}) || !slices.Equal(b, []string{"4", "22"}) {
			t.Fatalf("numeric order leaked across monitors: A=%v B=%v", a, b)
		}
	}
}

func TestOverviewNavigationNormalizesLargeNegativeDelta(t *testing.T) {
	o := numbered(false)
	o.Show(overviewFixture())
	o.Move(-1000)
	if o.Selected < 0 || o.Selected >= len(o.Cards) {
		t.Fatalf("invalid selection: %d", o.Selected)
	}
}
func TestOverviewSearchDoesNotFilterBarSlots(t *testing.T) {
	snap := overviewFixture()
	o := numbered(true)
	o.Show(snap)
	o.SetQuery("browser")
	if len(o.Cards) != 1 || o.Cards[0].Workspace.ID != "2" {
		t.Fatalf("search: %+v", o.Cards)
	}
	if got := ids(o.BarWorkspaces(snap, "A", false)); !slices.Equal(got, []string{"1", "2"}) {
		t.Fatalf("query changed slots: %v", got)
	}
}
func TestOverviewMRUEmptyFocusedMonitorSelectsNew(t *testing.T) {
	snap := overviewFixture()
	snap.FocusedOutput = "B"
	snap.Workspaces[0].Focused = false
	snap.Workspaces[3].Focused = true
	snap.Windows = snap.Windows[:2]
	o := numbered(true)
	o.Show(snap)
	if c := o.Cards[o.Selected]; !c.New || c.Workspace.Output != "B" {
		t.Fatalf("wrong fallback selection: %+v", c)
	}
}

func TestOverviewSlotIgnoresSearchAndInvalidSlotIsNoop(t *testing.T) {
	snap := overviewFixture()
	o := numbered(true)
	o.Show(snap)
	o.SetQuery("browser")
	if c, ok := o.WorkspaceSlot(snap, "A", 1); !ok || c.Workspace.ID != "1" {
		t.Fatalf("slot followed query: %+v %v", c, ok)
	}
	if c, ok := o.WorkspaceSlot(snap, "B", 2); !ok || !c.New || c.Workspace.Output != "B" {
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
	o := numbered(false)
	now := time.Unix(100, 0)
	first, _ := o.ReserveWorkspace(snap, now, "B")
	second, _ := o.ReserveWorkspace(snap, now, "B")
	if first != "12" {
		t.Fatalf("empty workspace on B should be recycled: %s", first)
	}
	if second == first || second == "" {
		t.Fatalf("rapid New IDs collided: %s %s", first, second)
	}
	if expired, _ := o.ReserveWorkspace(snap, now.Add(6*time.Second), "B"); expired != "12" {
		t.Fatalf("failed reservation did not expire: %s", expired)
	}
	var none Overview
	if id, ok := none.ReserveWorkspace(snap, now, "B"); ok || id != "" {
		t.Fatalf("without a compositor naming workspaces: %q %v", id, ok)
	}
}

func TestOverviewReservePrefersEmptyWorkspaceOnTargetMonitor(t *testing.T) {
	snap := overviewFixture()
	snap.Workspaces = append(snap.Workspaces, ws(3, "A"))
	o := numbered(false)
	now := time.Now()
	if id, _ := o.ReserveWorkspace(snap, now, "A"); id != "3" {
		t.Fatalf("should recycle empty workspace on A: %s", id)
	}
}

func TestOverviewPendingMoveOccupiesNewCardOnTargetMonitor(t *testing.T) {
	snap := overviewFixture()
	o := numbered(false)
	o.Show(snap)
	now := time.Now()
	id, _ := o.ReserveWorkspace(snap, now, "A")
	o.BeginMove("0x4", id, "A", "4", now)
	var moved OverviewCard
	for _, c := range o.Cards {
		if !c.New && c.Workspace.ID == id {
			moved = c
		}
		if !c.New && c.Workspace.ID == "4" {
			t.Fatalf("emptied source workspace still shown: %+v", c)
		}
	}
	if moved.Workspace.Output != "A" || len(moved.Windows) != 1 || moved.Windows[0].ID != "0x4" {
		t.Fatalf("pending New drop did not occupy target: %+v", moved)
	}
	if c, ok := o.WorkspaceSlot(snap, "A", 4); !ok || !c.New || c.Workspace.Output != "A" {
		t.Fatalf("target monitor lost its trailing New card: %+v %v", c, ok)
	}
	snap.Windows[2].Workspace = id
	snap.Windows[2].Output = "A"
	found := false
	for i, w := range snap.Workspaces {
		if w.ID == id {
			snap.Workspaces[i].Output = "A"
			found = true
		}
	}
	if !found {
		n, _ := strconv.Atoi(id)
		snap.Workspaces = append(snap.Workspaces, ws(n, "A"))
	}
	o.SetSnapshot(snap)
	if len(o.moves) != 0 {
		t.Fatalf("settled move still pending: %+v", o.moves)
	}
}

func TestOverviewCountsWindowByWorkspaceIDEvenWhenClientMonitorDiffers(t *testing.T) {
	snap := overviewFixture()
	snap.Windows = append(snap.Windows, owin("0x3", "floating", "pinned", 3, "B", -1))
	snap.Workspaces = append(snap.Workspaces, ws(3, "A"))
	o := numbered(false)
	o.Show(snap)
	if _, ok := o.WorkspaceSlot(snap, "A", 2); !ok {
		t.Fatal("occupied workspace was hidden because the client monitor differed")
	}
}

func TestOverviewNumberSlotsStayOnFocusedMonitorWithMRUOff(t *testing.T) {
	snap := overviewFixture()
	snap.Workspaces = append(snap.Workspaces, ws(3, "A"), ws(10, "B"))
	snap.Windows = append(snap.Windows,
		owin("0x3", "term", "third", 3, "A", -1),
		owin("0x10", "term", "tenth", 10, "B", -1))
	o := numbered(false)
	for slot, want := range []string{"4", "10"} {
		card, ok := o.WorkspaceSlot(snap, "B", slot+1)
		if !ok || card.Workspace.ID != want || card.Workspace.Output != "B" {
			t.Fatalf("B slot %d: %+v %v", slot+1, card, ok)
		}
	}
	if card, ok := o.WorkspaceSlot(snap, "B", 3); !ok || !card.New || card.Workspace.Output != "B" {
		t.Fatalf("B new-workspace slot escaped monitor: %+v %v", card, ok)
	}
	for slot, want := range []string{"1", "2", "3"} {
		card, ok := o.WorkspaceSlot(snap, "A", slot+1)
		if !ok || card.Workspace.ID != want || card.Workspace.Output != "A" {
			t.Fatalf("A slot %d: %+v %v", slot+1, card, ok)
		}
	}
}

func TestOverviewSearchWindowsAndApps(t *testing.T) {
	snap := overviewFixture()
	o := numbered(false)
	o.Apps = []*desktop.Entry{
		{ID: "firefox.desktop", Name: "Firefox", GenericName: "Web Browser", ExecString: "firefox"},
		{ID: "code.desktop", Name: "Visual Studio Code", GenericName: "Text Editor", ExecString: "code"},
	}
	o.Show(snap)

	// Initially no search query
	if len(o.MatchedWindows) != 0 || len(o.MatchedApps) != 0 || o.SearchSelected != -1 {
		t.Fatalf("expected empty search at start")
	}

	// Search for "fire" -> should match Firefox in apps
	o.SetQuery("fire")
	if len(o.MatchedApps) == 0 || o.MatchedApps[0].Title != "Firefox" {
		t.Fatalf("expected Firefox app match, got %+v", o.MatchedApps)
	}
	if o.SearchSelected != 0 {
		t.Fatalf("expected search selected 0, got %d", o.SearchSelected)
	}
	sel, ok := o.SelectedSearchResult()
	if !ok || sel.Title != "Firefox" {
		t.Fatalf("expected selected search result to be Firefox, got %+v %v", sel, ok)
	}

	// Search for "second" -> should match browser window
	o.SetQuery("second")
	if len(o.MatchedWindows) == 0 || o.MatchedWindows[0].Title != "second" {
		t.Fatalf("expected window match 'second', got %+v", o.MatchedWindows)
	}

	// Navigation in search
	o.MoveSearch(1)
	if o.SearchSelected != 0 { // only 1 result total
		t.Fatalf("expected search selected 0, got %d", o.SearchSelected)
	}

	// Clear query
	o.SetQuery("")
	if len(o.MatchedWindows) != 0 || len(o.MatchedApps) != 0 || o.SearchSelected != -1 {
		t.Fatalf("expected empty search after clear")
	}
}
