package model

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/lyricat/inua/internal/term/layout"
)

func TestWorkspacesAndSessions(t *testing.T) {
	var m Model
	w := m.AddWorkspace("/src/inua/", "", true)
	if w.Name != "inua" {
		t.Errorf("name %q", w.Name)
	}
	s1 := m.AddSession(w, "", 1)
	s2 := m.AddSession(w, "", 2)
	if s1.Name != "inua" || s2.Name != "Session 2" {
		t.Errorf("names %q %q", s1.Name, s2.Name)
	}
	other := m.AddWorkspace("/src/dotfiles", "dots", false)
	m.AddSession(other, "", 3)
	if m.Workspaces[0] != other {
		t.Error("a new workspace goes first")
	}
	if m.ByDir("/src/inua") != w {
		t.Error("by folder")
	}
	if err := m.Show(w.ID, s2.ID); err != nil || m.Shown != (Shown{w.ID, s2.ID}) {
		t.Errorf("show: %v %+v", err, m.Shown)
	}
	m.Show(other.ID, 0)
	m.Show(w.ID, 0) // back: the session it last showed
	if m.Shown.Session != s2.ID {
		t.Errorf("last session %+v", m.Shown)
	}
	if err := m.Show(other.ID, s1.ID); err == nil {
		t.Error("a session of another workspace")
	}
	if got := m.Panes(); !slices.Equal(got, []int{3, 1, 2}) {
		t.Errorf("panes %v", got)
	}
}

func TestSplitAndRemove(t *testing.T) {
	var m Model
	w := m.AddWorkspace("/w", "", false)
	s := m.AddSession(w, "", 1)
	m.Show(w.ID, s.ID)
	m.Split(s.ID, 2, layout.Right)
	m.Split(s.ID, 3, layout.Down) // beside 2, the focused one
	if s.Focus != 3 || !slices.Equal(s.Tree.Panes(), []int{1, 2, 3}) {
		t.Fatalf("focus %d, panes %v", s.Focus, s.Tree.Panes())
	}
	// Closing the focused pane focuses a neighbour.
	if gone, _ := m.RemovePane(3); gone || s.Focus != 2 {
		t.Errorf("after closing 3: gone %v, focus %d", gone, s.Focus)
	}
	m.RemovePane(1)
	// The last pane takes the session, and the workspace.
	sGone, wGone := m.RemovePane(2)
	if !sGone || !wGone || len(m.Workspaces) != 0 || m.Shown != (Shown{}) {
		t.Errorf("gone %v %v, %d workspaces, shown %+v", sGone, wGone, len(m.Workspaces), m.Shown)
	}
}

func TestRemoveShown(t *testing.T) {
	var m Model
	w := m.AddWorkspace("/w", "", true)
	a := m.AddSession(w, "", 1)
	b := m.AddSession(w, "", 2)
	m.Show(w.ID, a.ID)
	if gone, err := m.RemoveSession(a.ID); err != nil || gone {
		t.Fatalf("remove: %v %v", gone, err)
	}
	if m.Shown.Session != b.ID {
		t.Errorf("shown after removing a %+v", m.Shown)
	}
	// Its last session takes a workspace the user added too.
	if gone, _ := m.RemoveSession(b.ID); !gone || len(m.Workspaces) != 0 || m.Shown != (Shown{}) {
		t.Errorf("an added workspace without sessions: gone %v, %d, %+v", gone, len(m.Workspaces), m.Shown)
	}
}

func TestMovePane(t *testing.T) {
	var m Model
	w := m.AddWorkspace("/w", "", true)
	a := m.AddSession(w, "", 1)
	m.Split(a.ID, 2, layout.Right)
	b := m.AddSession(w, "", 3)
	m.Split(b.ID, 4, layout.Down) // b's focus is 4
	m.Show(w.ID, a.ID)
	// Beside the pane it was dropped on, focused, its session shown.
	if err := m.MovePane(2, b.ID, 3, layout.Right); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(a.Tree.Panes(), []int{1}) || !slices.Equal(b.Tree.Panes(), []int{3, 2, 4}) {
		t.Errorf("panes %v, %v", a.Tree.Panes(), b.Tree.Panes())
	}
	if b.Focus != 2 || m.Shown != (Shown{w.ID, b.ID}) {
		t.Errorf("focus %d, shown %+v", b.Focus, m.Shown)
	}
	// Beside the focused one when none is named; the session it leaves
	// empty goes.
	if err := m.MovePane(1, b.ID, 0, layout.Right); err != nil {
		t.Fatal(err)
	}
	if len(w.Sessions) != 1 || !slices.Equal(b.Tree.Panes(), []int{3, 2, 1, 4}) {
		t.Errorf("sessions %d, panes %v", len(w.Sessions), b.Tree.Panes())
	}
	if err := m.MovePane(9, b.ID, 0, layout.Right); err != ErrNotFound {
		t.Errorf("a pane that is not: %v", err)
	}
}

func TestMove(t *testing.T) {
	var m Model
	a := m.AddWorkspace("/a", "", true)
	b := m.AddWorkspace("/b", "", true)
	c := m.AddWorkspace("/c", "", true) // c b a
	m.Move(c.ID, 2)
	if m.Workspaces[0] != b || m.Workspaces[1] != a || m.Workspaces[2] != c {
		t.Errorf("order %s %s %s", m.Workspaces[0].Name, m.Workspaces[1].Name, m.Workspaces[2].Name)
	}
}

func TestMoveSession(t *testing.T) {
	for _, tt := range []struct {
		name         string
		from, before int
		want         []int
	}{
		{"up", 2, 0, []int{2, 0, 1}},
		{"down", 0, 2, []int{1, 0, 2}},
		{"end", 0, -1, []int{1, 2, 0}},
		{"same", 1, 1, []int{0, 1, 2}},
		{"already before", 0, 1, []int{0, 1, 2}},
		{"already last", 2, -1, []int{0, 1, 2}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var m Model
			w := m.AddWorkspace("/project", "", true)
			sessions := []*Session{m.AddSession(w, "one", 1), m.AddSession(w, "two", 2), m.AddSession(w, "three", 3)}
			m.Split(sessions[1].ID, 4, layout.Down)
			sessions[1].Zoom, sessions[1].Used = 4, true
			m.Show(w.ID, sessions[1].ID)
			before, _ := json.Marshal(&m)
			trees := []*layout.Node{sessions[0].Tree, sessions[1].Tree, sessions[2].Tree}
			target := 0
			if tt.before >= 0 {
				target = sessions[tt.before].ID
			}
			if err := m.MoveSession(w.ID, sessions[tt.from].ID, target); err != nil {
				t.Fatal(err)
			}
			for i, index := range tt.want {
				if w.Sessions[i] != sessions[index] {
					t.Fatalf("session %d: got %d, want %d", i, w.Sessions[i].ID, sessions[index].ID)
				}
			}
			for i, s := range sessions {
				if s.Tree != trees[i] {
					t.Fatal("replaced a pane tree")
				}
			}
			// Undo only the order: all other state must remain byte-for-byte.
			w.Sessions = sessions
			after, _ := json.Marshal(&m)
			if string(before) != string(after) {
				t.Fatalf("changed state beyond order:\n%s\n%s", before, after)
			}
		})
	}
}

func TestMoveSessionRejectsMissingAndForeignIDs(t *testing.T) {
	var m Model
	w := m.AddWorkspace("/project", "", true)
	a := m.AddSession(w, "", 1)
	b := m.AddSession(w, "", 2)
	other := m.AddWorkspace("/other", "", true)
	foreign := m.AddSession(other, "", 3)
	empty := m.AddWorkspace("/empty", "", true)
	removed := m.AddSession(w, "", 4)
	m.RemoveSession(removed.ID)
	for _, move := range [][3]int{
		{0, a.ID, b.ID}, {999, a.ID, b.ID}, {w.ID, 0, b.ID},
		{w.ID, removed.ID, b.ID}, {w.ID, a.ID, removed.ID},
		{w.ID, foreign.ID, b.ID}, {w.ID, a.ID, foreign.ID},
		{empty.ID, a.ID, 0}, {w.ID, a.ID, -1},
	} {
		before, _ := json.Marshal(&m)
		if err := m.MoveSession(move[0], move[1], move[2]); err != ErrNotFound {
			t.Errorf("move %v: %v", move, err)
		}
		after, _ := json.Marshal(&m)
		if string(before) != string(after) {
			t.Fatalf("invalid move %v changed state", move)
		}
	}
	if err := m.MoveSession(other.ID, foreign.ID, 0); err != nil || len(other.Sessions) != 1 || other.Sessions[0] != foreign {
		t.Fatalf("single session: %v", err)
	}
	var emptyModel Model
	if err := emptyModel.MoveSession(1, 2, 0); err != ErrNotFound {
		t.Fatal(err)
	}
}
