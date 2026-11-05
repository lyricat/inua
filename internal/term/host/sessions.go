package host

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/layout"
	"github.com/lyricat/inua/internal/term/model"
	"github.com/lyricat/inua/internal/term/shellint"
)

// The host keeps the workspaces and sessions (internal/term/model) under
// h.mu, beside the panes; every change sends each client the whole State,
// which is small. A pane's title and folder go to every client too, for
// the columns, as events.

// ShellEnv is what a pane's shell gets on top of the host's environment.
var ShellEnv = []string{"TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=inua-term"}

// SetShell sets the shell new panes start (term.toml's shell.program);
// "" is $SHELL.
func (h *Host) SetShell(program string) {
	h.mu.Lock()
	h.shellProg = program
	h.mu.Unlock()
}

// shell is the shell a new pane starts, which its SHELL names too.
func (h *Host) shell() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return cmp.Or(h.shellProg, os.Getenv("SHELL"), "/bin/sh")
}

// ShellEnviron is the environment for a pane's shell (and the quick
// terminal's): the program's own, without the host's variables, and
// ShellEnv. sudo asks through inua-askpass, installed beside it, when
// nothing else is named: an agent's sudo -A, or one with no terminal.
func ShellEnviron(shell string) []string {
	var env []string
	askpass := true
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "SUDO_ASKPASS=") {
			askpass = e == "SUDO_ASKPASS="
			if askpass {
				continue
			}
		}
		if !strings.HasPrefix(e, "INUA_TERM_") && !strings.HasPrefix(e, "SHELL=") {
			env = append(env, e)
		}
	}
	if self, err := executable(); askpass && err == nil {
		// sudo does not search PATH: its full path.
		if p := filepath.Join(filepath.Dir(self), "inua-askpass"); isProgram(p) {
			env = append(env, "SUDO_ASKPASS="+p)
		}
	}
	return append(append(env, "SHELL="+shell), ShellEnv...)
}

// executable is the program running, beside which inua's others are.
var executable = os.Executable

// isProgram is whether path is a file that runs.
func isProgram(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0
}

// tell sends every client a message.
func (h *Host) tell(m Message) {
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		c.send(FrameControl, b)
	}
}

// state is the State now.
func (h *Host) state() *State {
	h.mu.Lock()
	// A copy through JSON: the model goes on changing under h.mu.
	b, _ := json.Marshal(&h.m)
	h.mu.Unlock()
	s := &State{Panes: h.list()}
	json.Unmarshal(b, &s.Model)
	return s
}

// publish sends every client the State, and has it kept.
func (h *Host) publish() {
	h.tell(Message{Type: MsgState, State: h.state()})
	h.changed()
}

// used marks the session of a pane typed in as used, the first time,
// and a restored pane as the user's again.
func (h *Host) used(pane int) {
	h.mu.Lock()
	s, _ := h.m.PaneSession(pane)
	changed := s != nil && !s.Used
	if changed {
		s.Used = true
	}
	h.mu.Unlock()
	if p := h.pane(pane); p != nil {
		p.mu.Lock()
		if p.restored {
			p.restored, changed = false, true
		}
		p.mu.Unlock()
	}
	if changed {
		h.publish()
	}
	h.changed() // what is typed at the prompt is kept too
	h.seen(pane)
}

// seenFocus marks a session's focused pane seen.
func (h *Host) seenFocus(session int) {
	h.mu.Lock()
	s, _ := h.m.Session(session)
	focus := 0
	if s != nil {
		focus = s.Focus
	}
	h.mu.Unlock()
	h.seen(focus)
}

// startIn starts the user's shell in dir, sized as the pane it sits
// beside (size: a pane ID, or 0).
func (h *Host) startIn(dir string, size int) (int, error) {
	cols, rows := 80, 24
	if p := h.pane(size); p != nil {
		p.mu.Lock()
		cols, rows = p.cols, p.rows
		p.mu.Unlock()
	}
	sh := h.shell()
	argv, env := []string{sh}, ShellEnviron(sh)
	if h.ShellDir != "" {
		argv, env = shellint.Wrap(argv, env, h.ShellDir)
	}
	return h.start(argv, dir, env, cols, rows, 0, 0)
}

// folderOf is where a pane's shell is, or "" for no pane.
func (h *Host) folderOf(id int) string {
	p := h.pane(id)
	if p == nil {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.folder()
}

// handleSessions answers the workspace and session requests; false for
// another kind of message.
func (h *Host) handleSessions(c *client, m Message) bool {
	reply := Message{Type: MsgReply, ID: m.ID}
	var err error
	switch m.Type {
	case MsgState:
		reply.State = h.state()
		c.message(reply)
		return true
	case MsgOpen:
		reply.Workspace, err = h.open(m.Dir, m.Name)
	case MsgNewSession:
		reply.Session, err = h.newSession(m.Workspace, m.Name)
	case MsgSplit:
		reply.Pane, err = h.split(m.Session, layout.Split(m.Split))
	case MsgLayout:
		err = h.layout(m)
		if err == nil && m.Op == OpFocus {
			defer h.seenFocus(m.Session)
		}
	case MsgShow:
		h.mu.Lock()
		err = h.m.Show(m.Workspace, m.Session)
		h.mu.Unlock()
	case MsgRename:
		err = h.rename(m.Workspace, m.Session, m.Name)
	case MsgCloseSession:
		err = h.closeSession(m.Session)
	case MsgRemove:
		err = h.removeWorkspace(m.Workspace)
	case MsgMoveSession:
		h.mu.Lock()
		err = h.m.MoveSession(m.Workspace, m.Session, m.To)
		h.mu.Unlock()
		// Reconcile even a rejected optimistic move. Send the state before
		// the reply so the requesting window can release its preview then.
		if err == nil {
			h.publish()
		} else {
			h.tell(Message{Type: MsgState, State: h.state()})
		}
		reply.Error = errText(err)
		c.message(reply)
		return true
	case MsgMove:
		h.mu.Lock()
		err = h.m.Move(m.Workspace, m.To)
		h.mu.Unlock()
	case MsgRun:
		reply.Session, err = h.run(m.Argv, m.Dir)
	case MsgReport:
		err = h.report(m, peerPid(c.conn))
		c.message(Message{Type: MsgReply, ID: m.ID, Error: errText(err)})
		return true
	case MsgActive:
		h.mu.Lock()
		c.active = m.Active
		s, _ := h.m.Session(h.m.Shown.Session)
		focus := 0
		if s != nil {
			focus = s.Focus
		}
		h.mu.Unlock()
		c.message(reply)
		if m.Active && focus != 0 {
			h.seen(focus) // the window came back to it
		}
		return true
	case MsgUI:
		if m.UI == nil {
			err = errors.New("no ui")
			break
		}
		h.mu.Lock()
		h.m.UI = *m.UI
		h.mu.Unlock()
		// Kept, but nothing to show: no state goes out for it.
		c.message(reply)
		h.changed()
		return true
	default:
		return false
	}
	if err != nil {
		reply.Error = err.Error()
		c.message(reply)
		return true
	}
	c.message(reply)
	h.publish()
	return true
}

// open shows the workspace for dir, made with a session if there is
// none.
func (h *Host) open(dir, name string) (int, error) {
	if dir == "" {
		return 0, errors.New(i18n.T("No folder given."))
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return 0, errors.New(i18n.Tf("%s is not a folder.", dir))
	}
	h.mu.Lock()
	w := h.m.ByDir(dir)
	h.mu.Unlock()
	if w != nil {
		h.mu.Lock()
		defer h.mu.Unlock()
		return w.ID, h.m.Show(w.ID, 0)
	}
	pane, err := h.startIn(dir, 0)
	if err != nil {
		return 0, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	w = h.m.AddWorkspace(dir, name, true)
	s := h.m.AddSession(w, "", pane)
	return w.ID, h.m.Show(w.ID, s.ID)
}

// run starts a command in a session of its own, in the workspace for
// commands others start (`inua-term -e`), and shows it.
func (h *Host) run(argv []string, dir string) (int, error) {
	if len(argv) == 0 {
		return 0, errors.New("no command")
	}
	home, _ := os.UserHomeDir()
	if dir == "" {
		dir = home
	}
	pane, err := h.start(argv, dir, ShellEnviron(h.shell()), 80, 24, 0, 0)
	if err != nil {
		return 0, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	var w *model.Workspace
	for _, ws := range h.m.Workspaces {
		if ws.Commands {
			w = ws
		}
	}
	if w == nil {
		w = h.m.AddWorkspace(home, i18n.T("Commands"), false)
		w.Commands = true
	}
	s := h.m.AddSession(w, filepath.Base(argv[0]), pane)
	return s.ID, h.m.Show(w.ID, s.ID)
}

// newSession adds a session to a workspace, its shell in the workspace's
// folder, and shows it.
func (h *Host) newSession(workspace int, name string) (int, error) {
	h.mu.Lock()
	w := h.m.Workspace(workspace)
	h.mu.Unlock()
	if w == nil {
		return 0, model.ErrNotFound
	}
	pane, err := h.startIn(w.Dir, 0)
	if err != nil {
		return 0, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.m.AddSession(w, name, pane)
	return s.ID, h.m.Show(w.ID, s.ID)
}

// split starts a shell beside the session's focused pane, in its folder.
func (h *Host) split(session int, sp layout.Split) (int, error) {
	if sp != layout.Right && sp != layout.Down {
		return 0, fmt.Errorf("split %q", sp)
	}
	h.mu.Lock()
	s, w := h.m.Session(session)
	focus, dir := 0, ""
	if s != nil {
		focus, dir = s.Focus, w.Dir
	}
	h.mu.Unlock()
	if s == nil {
		return 0, model.ErrNotFound
	}
	pane, err := h.startIn(cmp.Or(h.folderOf(focus), dir), focus)
	if err != nil {
		return 0, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return pane, h.m.Split(session, pane, sp)
}

// layout changes a session's layout or focus.
func (h *Host) layout(m Message) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, _ := h.m.Session(m.Session)
	if s == nil {
		return model.ErrNotFound
	}
	d := layout.Dir(m.Direction)
	switch m.Op {
	case OpFocus:
		target := m.Pane
		if target == 0 {
			target = layout.Neighbor(s.Tree, s.Focus, d)
		}
		if target != 0 && s.Tree.Has(target) {
			s.Focus = target
			if s.Zoom != 0 {
				s.Zoom = target
			}
		}
	case OpSwap:
		// The focused pane, or the one dragged (From), with the neighbour
		// in Direction, or the pane it was dropped on (Pane).
		from, to := cmp.Or(m.From, s.Focus), m.Pane
		if to == 0 {
			to = layout.Neighbor(s.Tree, from, d)
		}
		if to != 0 && to != from && s.Tree.Has(from) && s.Tree.Has(to) {
			s.Tree = layout.Swap(s.Tree, from, to)
		}
	case OpResize:
		s.Tree = layout.Resize(s.Tree, s.Focus, d, cmp.Or(m.Step, 0.05))
	case OpEqualize:
		s.Tree = layout.Equalize(s.Tree)
	case OpZoom:
		if s.Zoom != 0 {
			s.Zoom = 0
		} else if len(s.Tree.Panes()) > 1 {
			s.Zoom = s.Focus
		}
	case OpPreset:
		name := m.Name
		if name == "" {
			// The next preset after the one the session has.
			i := slices.Index(layout.Presets, presetOf(s.Tree))
			name = layout.Presets[(i+1)%len(layout.Presets)]
		}
		if !slices.Contains(layout.Presets, name) {
			return fmt.Errorf("preset %q", name)
		}
		s.Tree = layout.Preset(s.Tree.Panes(), name)
		s.Zoom = 0
	case OpRatio:
		if !layout.SetRatioAt(s.Tree, m.Path, m.Ratio) {
			return fmt.Errorf("no split at %q", m.Path)
		}
	case OpMove:
		pane := cmp.Or(m.Pane, s.Focus)
		if !s.Tree.Has(pane) {
			return model.ErrNotFound
		}
		return h.m.MovePane(pane, m.To, m.From, layout.Right)
	default:
		return fmt.Errorf("layout %q", m.Op)
	}
	return nil
}

// presetOf is the preset a tree is laid out as, "" for none.
func presetOf(t *layout.Node) string {
	b, _ := json.Marshal(t)
	for _, p := range layout.Presets {
		if q, _ := json.Marshal(layout.Preset(t.Panes(), p)); string(q) == string(b) {
			return p
		}
	}
	return ""
}

func (h *Host) rename(workspace, session int, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("no name")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if session != 0 {
		s, _ := h.m.Session(session)
		if s == nil {
			return model.ErrNotFound
		}
		s.Name = name
		return nil
	}
	w := h.m.Workspace(workspace)
	if w == nil {
		return model.ErrNotFound
	}
	w.Name = name
	return nil
}

// closeSession ends a session's panes; the session goes with the last.
func (h *Host) closeSession(session int) error {
	h.mu.Lock()
	s, _ := h.m.Session(session)
	var panes []int
	if s != nil {
		panes = s.Tree.Panes()
		h.m.RemoveSession(session)
	}
	h.mu.Unlock()
	if s == nil {
		return model.ErrNotFound
	}
	h.endPanes(panes)
	return nil
}

func (h *Host) removeWorkspace(workspace int) error {
	h.mu.Lock()
	w := h.m.Workspace(workspace)
	var panes []int
	if w != nil {
		for _, s := range w.Sessions {
			panes = append(panes, s.Tree.Panes()...)
		}
		h.m.RemoveWorkspace(workspace)
	}
	h.mu.Unlock()
	if w == nil {
		return model.ErrNotFound
	}
	h.endPanes(panes)
	return nil
}

// endPanes ends panes no longer in the model; the host ends with the
// last when it should.
func (h *Host) endPanes(ids []int) {
	last := false
	for _, id := range ids {
		if p := h.pane(id); p != nil {
			last = h.closePane(p)
		}
	}
	if last && h.ExitWhenEmpty {
		go h.Close()
	}
}

// forgetPane takes a closed pane out of the model.
func (h *Host) forgetPane(id int) {
	h.mu.Lock()
	h.m.RemovePane(id)
	h.mu.Unlock()
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
