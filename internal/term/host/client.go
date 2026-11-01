package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/lyricat/inua/internal/pinentry"
	"github.com/lyricat/inua/internal/term/model"
)

// Client is a connection to the host. Output and events arrive on the
// connection's own goroutine, through OnOutput and OnEvent, set before
// Dial returns them anything.
type Client struct {
	conn net.Conn

	wmu sync.Mutex // writes

	mu      sync.Mutex
	next    int
	pending map[int]chan Message
	err     error

	onOutput func(pane int, b []byte)
	onEvent  func(Message)
	done     chan struct{}
	// sizes is set when the host tells each size a pane takes.
	sizes bool
	// snapshots is set when the host makes snapshots.
	snapshots bool
}

// Dial connects to the host at path; onOutput and onEvent may be nil.
func Dial(path string, onOutput func(pane int, b []byte), onEvent func(Message)) (*Client, error) {
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, err
	}
	if err := writeMessage(conn, Message{Type: MsgHello, Version: Version}); err != nil {
		conn.Close()
		return nil, err
	}
	kind, payload, err := ReadFrame(conn)
	var m Message
	if err == nil && (kind != FrameControl || json.Unmarshal(payload, &m) != nil) {
		err = errors.New("host: no welcome")
	}
	if err == nil && m.Type != MsgWelcome {
		err = fmt.Errorf("host: %s", m.Error)
	}
	if err != nil {
		conn.Close()
		return nil, err
	}
	c := &Client{conn: conn, pending: map[int]chan Message{}, onOutput: onOutput, onEvent: onEvent, done: make(chan struct{}), sizes: m.Sizes, snapshots: m.Snapshot}
	go c.read()
	return c, nil
}

func (c *Client) read() {
	defer close(c.done)
	var err error
	for {
		var kind byte
		var payload []byte
		if kind, payload, err = ReadFrame(c.conn); err != nil {
			break
		}
		switch kind {
		case FrameOutput:
			id, b, e := splitPane(payload)
			if e != nil {
				err = e
				break
			}
			if c.onOutput != nil {
				c.onOutput(id, b)
			}
			continue
		case FrameControl:
			var m Message
			if e := json.Unmarshal(payload, &m); e != nil {
				err = e
				break
			}
			if m.Type == MsgReply {
				c.mu.Lock()
				ch := c.pending[m.ID]
				delete(c.pending, m.ID)
				c.mu.Unlock()
				if ch != nil {
					ch <- m
				}
			} else if c.onEvent != nil {
				c.onEvent(m)
			}
			continue
		}
		break
	}
	if err == nil {
		err = errors.New("host: unexpected frame")
	}
	c.mu.Lock()
	c.err = err
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()
}

// Done is closed when the connection ends.
func (c *Client) Done() <-chan struct{} { return c.done }

// Close ends the connection; the host keeps the panes.
func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) call(m Message) (Message, error) {
	ch := make(chan Message, 1)
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return Message{}, err
	}
	c.next++
	m.ID = c.next
	c.pending[m.ID] = ch
	c.mu.Unlock()
	c.wmu.Lock()
	err := writeMessage(c.conn, m)
	c.wmu.Unlock()
	if err != nil {
		return Message{}, err
	}
	r, ok := <-ch
	if !ok {
		return Message{}, errors.New("host: connection closed")
	}
	if r.Error != "" {
		return r, fmt.Errorf("host: %s", r.Error)
	}
	return r, nil
}

// AskSecret asks the web pages open what q asks (secrets.go), and
// waits: the answer (AnswerOK with the passphrase, AnswerNo or
// AnswerCancel), or "" when no page is open to ask. Closing the client
// takes the question back.
func (c *Client) AskSecret(q pinentry.Request) (answer, pin string, err error) {
	r, err := c.call(Message{Type: MsgSecret, Ask: &q})
	switch {
	case r.Error == ErrNoPage:
		return "", "", nil
	case r.Error == ErrCancelled:
		return AnswerCancel, "", nil
	case r.Error == ErrNotOK:
		return AnswerNo, "", nil
	case err != nil:
		return "", "", err
	}
	if r.Pin != nil {
		pin = *r.Pin
	}
	return AnswerOK, pin, nil
}

// Create starts argv in dir on a new pane of cols × rows.
func (c *Client) Create(argv []string, dir string, env []string, cols, rows int) (int, error) {
	r, err := c.call(Message{Type: MsgCreate, Argv: argv, Dir: dir, Env: env, Cols: cols, Rows: rows})
	return r.Pane, err
}

// List is the host's panes.
func (c *Client) List() ([]PaneInfo, error) {
	r, err := c.call(Message{Type: MsgList})
	return r.Panes, err
}

// Attach asks for a pane's output: its replay arrives through OnOutput
// before Attach returns, then what follows.
func (c *Client) Attach(pane int) error {
	_, err := c.call(Message{Type: MsgAttach, Pane: pane})
	return err
}

// AttachSnapshot is Attach with the pane as it is now, drawn again at its
// size, in place of its replay; on a host that makes none
// (TellsSnapshots), the replay.
func (c *Client) AttachSnapshot(pane int) error {
	_, err := c.call(Message{Type: MsgAttach, Pane: pane, Snapshot: true})
	return err
}

// TellsSnapshots reports whether the host makes snapshots.
func (c *Client) TellsSnapshots() bool { return c.snapshots }

// Detach stops a pane's output.
func (c *Client) Detach(pane int) error {
	_, err := c.call(Message{Type: MsgDetach, Pane: pane})
	return err
}

// Resize sets a pane's size; this client is the one typing in it.
func (c *Client) Resize(pane, cols, rows, cellW, cellH int) error {
	_, err := c.call(Message{Type: MsgResize, Pane: pane, Cols: cols, Rows: rows, CellW: cellW, CellH: cellH})
	return err
}

// SetColors tells the host the colours programs are told when they ask.
func (c *Client) SetColors(colors Colors) error {
	_, err := c.call(Message{Type: MsgColors, Colors: &colors})
	return err
}

// SetShell tells the host the shell new panes start; "" is $SHELL.
func (c *Client) SetShell(program string) error {
	m := Message{Type: MsgShell}
	if program != "" {
		m.Argv = []string{program}
	}
	_, err := c.call(m)
	return err
}

// Quit ends the host as a logout does: it keeps the sessions for the next
// host, then ends every pane's program.
func (c *Client) Quit() error {
	_, err := c.call(Message{Type: MsgQuit})
	return err
}

// TellsSizes reports whether the host tells each size a pane takes
// (Message.Cols and Rows in an event), in the output's order.
func (c *Client) TellsSizes() bool { return c.sizes }

// ClosePane closes an idle shell or exited pane. Running or unknown
// programs return ErrCloseConfirmation without changing the pane.
func (c *Client) ClosePane(pane int) error {
	r, err := c.call(Message{Type: MsgClose, Pane: pane})
	if r.Error == ErrCloseConfirmation.Error() {
		return ErrCloseConfirmation
	}
	return err
}

// ConfirmClosePane closes a pane after explicit user confirmation.
func (c *Client) ConfirmClosePane(pane int) error {
	_, err := c.call(Message{Type: MsgClose, Pane: pane, Confirm: true})
	return err
}

// Input sends what was typed to a pane.
func (c *Client) Input(pane int, b []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return WriteFrame(c.conn, FrameInput, paneBytes(pane, b)...)
}

// State is the workspaces, sessions and panes now; afterwards each
// change comes as an event of type MsgState.
func (c *Client) State() (*State, error) {
	r, err := c.call(Message{Type: MsgState})
	if err == nil && r.State == nil {
		err = errors.New("host: no state")
	}
	return r.State, err
}

// Run starts argv in dir in a session of its own, in the Commands
// workspace, and shows it.
func (c *Client) Run(argv []string, dir string) (int, error) {
	r, err := c.call(Message{Type: MsgRun, Argv: argv, Dir: dir})
	return r.Session, err
}

// Open shows the workspace for dir, made with a session if new.
func (c *Client) Open(dir, name string) (int, error) {
	r, err := c.call(Message{Type: MsgOpen, Dir: dir, Name: name})
	return r.Workspace, err
}

// NewSession adds a session to a workspace and shows it.
func (c *Client) NewSession(workspace int, name string) (int, error) {
	r, err := c.call(Message{Type: MsgNewSession, Workspace: workspace, Name: name})
	return r.Session, err
}

// Split starts a shell beside the session's focused pane: split is
// "right" or "down".
func (c *Client) Split(session int, split string) (int, error) {
	r, err := c.call(Message{Type: MsgSplit, Session: session, Split: split})
	return r.Pane, err
}

// Layout does op (OpFocus…) on a session; m carries what the op needs.
func (c *Client) Layout(session int, op string, m Message) error {
	m.Type, m.Session, m.Op = MsgLayout, session, op
	_, err := c.call(m)
	return err
}

// Show shows a workspace's session (0: the last it showed).
func (c *Client) Show(workspace, session int) error {
	_, err := c.call(Message{Type: MsgShow, Workspace: workspace, Session: session})
	return err
}

// Rename names a session, or a workspace when session is 0.
func (c *Client) Rename(workspace, session int, name string) error {
	_, err := c.call(Message{Type: MsgRename, Workspace: workspace, Session: session, Name: name})
	return err
}

// CloseSession ends a session's panes and forgets it.
func (c *Client) CloseSession(session int) error {
	_, err := c.call(Message{Type: MsgCloseSession, Session: session})
	return err
}

// RemoveWorkspace forgets a workspace, ending its panes.
func (c *Client) RemoveWorkspace(workspace int) error {
	_, err := c.call(Message{Type: MsgRemove, Workspace: workspace})
	return err
}

// SetActive tells the host whether this client's window is the active
// one: an agent in the pane it shows is looked at.
func (c *Client) SetActive(active bool) error {
	_, err := c.call(Message{Type: MsgActive, Active: active})
	return err
}

// SetUI tells the host how the window is, to be kept.
func (c *Client) SetUI(ui model.UI) error {
	_, err := c.call(Message{Type: MsgUI, UI: &ui})
	return err
}

// MoveWorkspace puts a workspace at place to in the list.
func (c *Client) MoveWorkspace(workspace, to int) error {
	_, err := c.call(Message{Type: MsgMove, Workspace: workspace, To: to})
	return err
}

// Report tells the host what the agent in a pane does: its type, its
// state (working, blocked, done, idle; "" to keep it), its title and what
// it asks or did.
func (c *Client) Report(pane int, agentType, state, title, summary string) error {
	_, err := c.call(Message{Type: MsgReport, Pane: pane, Agent: agentType, AgentState: state, Title: title, Summary: summary})
	return err
}
