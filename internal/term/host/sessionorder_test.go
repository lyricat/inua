package host

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/lyricat/inua/internal/term/model"
)

func TestMoveSessionStateAndPersistence(t *testing.T) {
	h, path := startHost(t)
	h.SetShell("/bin/sh")
	file := filepath.Join(t.TempDir(), "term.json")
	if err := h.Remember(file, nil, false); err != nil {
		t.Fatal(err)
	}
	watch := newWatcher()
	c := dial(t, path, watch)
	observer := newWatcher()
	dial(t, path, observer)
	workspace, err := c.Open(t.TempDir(), "project")
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.NewSession(workspace, "second")
	if err != nil {
		t.Fatal(err)
	}
	third, err := c.NewSession(workspace, "third")
	if err != nil {
		t.Fatal(err)
	}
	foreignWorkspace, err := c.Open(t.TempDir(), "other")
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.State()
	if err != nil {
		t.Fatal(err)
	}
	first := st.Model.Workspace(workspace).Sessions[0].ID
	foreign := st.Model.Workspace(foreignWorkspace).Sessions[0].ID
	if _, err := c.Split(second, "right"); err != nil {
		t.Fatal(err)
	}
	if err := c.Show(workspace, second); err != nil {
		t.Fatal(err)
	}
	// Used sessions survive a restart under the existing persistence rule.
	h.mu.Lock()
	for _, s := range h.m.Workspace(workspace).Sessions {
		s.Used = true
	}
	h.mu.Unlock()
	before := h.state()
	processes := map[int]int{}
	for _, p := range before.Panes {
		processes[p.ID] = h.pane(p.ID).pty.Pid()
	}
	want := []int{third, first, second}
	if err := c.MoveSession(workspace, third, first); err != nil {
		t.Fatal(err)
	}
	// The state must reach the requester before the reply: the UI releases
	// its optimistic order when this call returns.
	watch.mu.Lock()
	var last *State
	for _, event := range watch.events {
		if event.Type == MsgState {
			last = event.State
		}
	}
	watch.mu.Unlock()
	if last == nil || !slices.Equal(sessionIDs(last.Model.Workspace(workspace)), want) {
		t.Fatal("reply preceded the reordered state")
	}
	observer.waitFor(t, "session order broadcast", func() bool {
		observer.mu.Lock()
		defer observer.mu.Unlock()
		for _, event := range observer.events {
			if event.Type == MsgState && slices.Equal(sessionIDs(event.State.Model.Workspace(workspace)), want) {
				return true
			}
		}
		return false
	})
	after := h.state()
	if after.Model.Shown != before.Model.Shown || after.Model.Workspace(workspace).Last != before.Model.Workspace(workspace).Last {
		t.Fatal("changed selection")
	}
	for _, s := range before.Model.Workspace(workspace).Sessions {
		got, _ := after.Model.Session(s.ID)
		if !reflect.DeepEqual(got, s) {
			t.Fatalf("changed session %d", s.ID)
		}
	}
	for id, pid := range processes {
		p := h.pane(id)
		if p == nil || p.pty.Pid() != pid {
			t.Fatalf("replaced process in pane %d", id)
		}
	}
	// Invalid requests return an error and authoritative state before it.
	for _, move := range [][3]int{
		{workspace, first, 999}, {workspace, 999, first}, {999, first, second},
		{workspace, foreign, first}, {workspace, first, foreign},
	} {
		watch.mu.Lock()
		count := len(watch.events)
		watch.mu.Unlock()
		if err := c.MoveSession(move[0], move[1], move[2]); err == nil {
			t.Fatalf("accepted %v", move)
		}
		watch.mu.Lock()
		var reconciled *State
		for _, event := range watch.events[count:] {
			if event.Type == MsgState {
				reconciled = event.State
			}
		}
		watch.mu.Unlock()
		if reconciled == nil || !reflect.DeepEqual(reconciled.Model, after.Model) {
			t.Fatalf("missing rejection reconciliation for %v", move)
		}
	}
	h.Save()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var saved Saved
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sessionIDs(saved.Model.Workspace(workspace)), want) {
		t.Fatal("order not saved")
	}
	restored, _ := startHost(t)
	restored.SetShell("/bin/sh")
	if err := restored.Remember(file, nil, false); err != nil {
		t.Fatal(err)
	}
	if got := restored.state(); !slices.Equal(sessionIDs(got.Model.Workspace(workspace)), want) || got.Model.Shown != before.Model.Shown {
		t.Fatal("order or selection not restored")
	}
}

func sessionIDs(w *model.Workspace) []int {
	if w == nil {
		return nil
	}
	ids := make([]int, len(w.Sessions))
	for i, s := range w.Sessions {
		ids[i] = s.ID
	}
	return ids
}
