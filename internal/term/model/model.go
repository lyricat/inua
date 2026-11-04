// Package model is what inua-term's host keeps besides the terminals:
// workspaces (a folder and a name), their sessions (a named tiled layout
// of panes), which session shows, and which pane has the focus. The host
// changes it under its own lock and sends it to every client; nothing
// here starts or ends a process. Pane IDs are the host's.
package model

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/lyricat/inua/internal/term/layout"
)

// Model is every workspace, in the order the user keeps them.
type Model struct {
	Workspaces []*Workspace `json:"workspaces"`
	// Shown is the workspace and session the windows show.
	Shown Shown `json:"shown"`
	// Next is the next workspace or session ID.
	Next int `json:"next"`
	// UI is the window's: its size and the columns the user folded.
	UI UI `json:"ui"`
}

// UI is how the window was left.
type UI struct {
	Width  int  `json:"width,omitempty"`
	Height int  `json:"height,omitempty"`
	Fold1  bool `json:"fold1,omitempty"` // column 1, the workspaces
	Fold2  bool `json:"fold2,omitempty"` // column 2, the sessions
	// The columns' widths as dragged; 0 for their first.
	Col1 int `json:"col1,omitempty"`
	Col2 int `json:"col2,omitempty"`
}

// Shown is a workspace and its session on screen; 0 for none.
type Shown struct {
	Workspace int `json:"workspace"`
	Session   int `json:"session"`
}

// Workspace is a project: a folder and a name, and its sessions.
type Workspace struct {
	ID       int        `json:"id"`
	Name     string     `json:"name"`
	Dir      string     `json:"dir"`
	Sessions []*Session `json:"sessions"`
	// Added is set for a workspace the user made: a restart keeps it
	// though none of its sessions was used.
	Added bool `json:"added,omitempty"`
	// Last is the session last shown in it.
	Last int `json:"last,omitempty"`
	// Commands is set for the workspace of the commands others start
	// (`inua-term -e`).
	Commands bool `json:"commands,omitempty"`
}

// Session is a named layout of panes.
type Session struct {
	ID    int          `json:"id"`
	Name  string       `json:"name"`
	Tree  *layout.Node `json:"tree"`
	Focus int          `json:"focus"`          // the pane with the focus
	Zoom  int          `json:"zoom,omitempty"` // a pane zoomed to the whole session
	// Used is set once something was typed or run in it: only used
	// sessions are remembered.
	Used bool `json:"used,omitempty"`
}

// ErrNotFound is for an ID the model does not have.
var ErrNotFound = errors.New("not found")

func (m *Model) id() int {
	m.Next++
	return m.Next
}

// Workspace is the workspace of that ID, nil for none.
func (m *Model) Workspace(id int) *Workspace {
	for _, w := range m.Workspaces {
		if w.ID == id {
			return w
		}
	}
	return nil
}

// Session is the session of that ID and its workspace.
func (m *Model) Session(id int) (*Session, *Workspace) {
	for _, w := range m.Workspaces {
		for _, s := range w.Sessions {
			if s.ID == id {
				return s, w
			}
		}
	}
	return nil, nil
}

// PaneSession is the session holding pane.
func (m *Model) PaneSession(pane int) (*Session, *Workspace) {
	for _, w := range m.Workspaces {
		for _, s := range w.Sessions {
			if s.Tree.Has(pane) {
				return s, w
			}
		}
	}
	return nil, nil
}

// ByDir is the workspace for a folder, nil for none.
func (m *Model) ByDir(dir string) *Workspace {
	dir = filepath.Clean(dir)
	for _, w := range m.Workspaces {
		if filepath.Clean(w.Dir) == dir {
			return w
		}
	}
	return nil
}

// AddWorkspace adds a workspace for dir, named after it unless name is
// given, first in the list.
func (m *Model) AddWorkspace(dir, name string, added bool) *Workspace {
	if name == "" {
		name = filepath.Base(filepath.Clean(dir))
	}
	w := &Workspace{ID: m.id(), Name: name, Dir: dir, Added: added}
	m.Workspaces = append([]*Workspace{w}, m.Workspaces...)
	return w
}

// AddSession adds a session of one pane to a workspace, named after it
// unless name is given ("Session 2"…).
func (m *Model) AddSession(w *Workspace, name string, pane int) *Session {
	if name == "" {
		name = fmt.Sprintf("Session %d", len(w.Sessions)+1)
		if len(w.Sessions) == 0 {
			name = w.Name
		}
	}
	s := &Session{ID: m.id(), Name: name, Tree: layout.Leaf(pane), Focus: pane}
	w.Sessions = append(w.Sessions, s)
	return s
}

// Show puts a session on screen, or its workspace's last session when
// session is 0.
func (m *Model) Show(workspace, session int) error {
	w := m.Workspace(workspace)
	if w == nil {
		return ErrNotFound
	}
	if session == 0 {
		session = w.Last
		if s, _ := m.Session(session); s == nil && len(w.Sessions) > 0 {
			session = w.Sessions[0].ID
		}
	}
	if session != 0 {
		if s, sw := m.Session(session); s == nil || sw != w {
			return ErrNotFound
		}
	}
	m.Shown = Shown{w.ID, session}
	w.Last = session
	return nil
}

// Split adds pane next to the session's focused pane and focuses it.
func (m *Model) Split(session, pane int, s layout.Split) error {
	ss, _ := m.Session(session)
	if ss == nil {
		return ErrNotFound
	}
	ss.Tree = layout.SplitPane(ss.Tree, ss.Focus, pane, s)
	ss.Focus, ss.Zoom = pane, 0
	return nil
}

// RemovePane takes a closed pane out of its session; a session left
// with none goes, and with it a workspace left with none. It reports
// what went, for the host to show another.
func (m *Model) RemovePane(pane int) (sessionGone, workspaceGone bool) {
	s, w := m.PaneSession(pane)
	if s == nil {
		return false, false
	}
	next := 0
	if s.Focus == pane {
		// The pane taking its place, the side its sibling is on.
		if d, ok := layout.Toward(s.Tree, pane); ok {
			next = layout.Neighbor(s.Tree, pane, d)
		}
	}
	s.Tree = layout.Remove(s.Tree, pane)
	if s.Zoom == pane {
		s.Zoom = 0
	}
	if s.Tree == nil {
		return true, m.removeSession(s, w)
	}
	if s.Focus == pane {
		if next == 0 {
			next = s.Tree.Panes()[0]
		}
		s.Focus = next
	}
	return false, false
}

// MovePane takes a pane out of its session into another, split beside
// pane beside there (that session's focused pane when 0), focused and
// shown. The session it leaves goes when it was its last pane, and that
// session's workspace when it was its last session.
func (m *Model) MovePane(pane, session, beside int, sp layout.Split) error {
	from, _ := m.PaneSession(pane)
	to, w := m.Session(session)
	if from == nil || to == nil {
		return ErrNotFound
	}
	if from == to {
		return nil
	}
	if beside == 0 || !to.Tree.Has(beside) {
		beside = to.Focus
	}
	m.RemovePane(pane)
	to.Tree = layout.SplitPane(to.Tree, beside, pane, sp)
	to.Focus, to.Zoom = pane, 0
	return m.Show(w.ID, to.ID)
}

// RemoveSession forgets a session (its panes are the host's to close);
// it reports whether its workspace went too.
func (m *Model) RemoveSession(session int) (workspaceGone bool, err error) {
	s, w := m.Session(session)
	if s == nil {
		return false, ErrNotFound
	}
	return m.removeSession(s, w), nil
}

func (m *Model) removeSession(s *Session, w *Workspace) bool {
	i := slices.Index(w.Sessions, s)
	w.Sessions = slices.Delete(w.Sessions, i, i+1)
	if m.Shown.Session == s.ID {
		m.Shown.Session = 0
		if len(w.Sessions) > 0 {
			m.Shown.Session = w.Sessions[min(i, len(w.Sessions)-1)].ID
		}
		w.Last = m.Shown.Session
	}
	if len(w.Sessions) > 0 {
		return false
	}
	m.RemoveWorkspace(w.ID)
	return true
}

// RemoveWorkspace forgets a workspace and its sessions.
func (m *Model) RemoveWorkspace(id int) {
	i := slices.IndexFunc(m.Workspaces, func(w *Workspace) bool { return w.ID == id })
	if i < 0 {
		return
	}
	m.Workspaces = slices.Delete(m.Workspaces, i, i+1)
	if m.Shown.Workspace == id {
		m.Shown = Shown{}
		if len(m.Workspaces) > 0 {
			w := m.Workspaces[min(i, len(m.Workspaces)-1)]
			m.Shown = Shown{Workspace: w.ID, Session: w.Last}
			if s, _ := m.Session(w.Last); s == nil && len(w.Sessions) > 0 {
				m.Shown.Session = w.Sessions[0].ID
			}
		}
	}
}

// Panes are every pane in every session.
func (m *Model) Panes() []int {
	var out []int
	for _, w := range m.Workspaces {
		for _, s := range w.Sessions {
			out = append(out, s.Tree.Panes()...)
		}
	}
	return out
}

// Move is a workspace's new place in the list.
func (m *Model) Move(workspace, to int) error {
	i := slices.IndexFunc(m.Workspaces, func(w *Workspace) bool { return w.ID == workspace })
	if i < 0 {
		return ErrNotFound
	}
	w := m.Workspaces[i]
	m.Workspaces = slices.Delete(m.Workspaces, i, i+1)
	to = min(max(to, 0), len(m.Workspaces))
	m.Workspaces = slices.Insert(m.Workspaces, to, w)
	return nil
}

// MoveSession puts session before another session in the same workspace;
// before 0 puts it last. Validate both IDs before changing the order so
// a removed target or a session from another workspace cannot move it.
func (m *Model) MoveSession(workspace, session, before int) error {
	w := m.Workspace(workspace)
	if w == nil {
		return ErrNotFound
	}
	from := slices.IndexFunc(w.Sessions, func(s *Session) bool { return s.ID == session })
	to := len(w.Sessions)
	if before != 0 {
		to = slices.IndexFunc(w.Sessions, func(s *Session) bool { return s.ID == before })
	}
	if from < 0 || to < 0 {
		return ErrNotFound
	}
	if from == to || from+1 == to {
		return nil
	}
	s := w.Sessions[from]
	if from < to {
		to--
	}
	w.Sessions = slices.Delete(w.Sessions, from, from+1)
	w.Sessions = slices.Insert(w.Sessions, to, s)
	return nil
}
