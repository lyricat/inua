// Package host is inua-term's host: the process that owns the terminals
// (their PTYs, their engines, their recent output) so that they outlive
// the window, and the protocol its clients speak over a Unix socket
// (docs/feat/inua-term.md, section 5). The window is one client; scripts,
// hooks and, later, a browser are others.
package host

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/lyricat/inua/internal/pinentry"
	"github.com/lyricat/inua/internal/term/model"
)

// Version is the protocol's; a client speaking another is turned away.
const Version = 1

// Frames on the socket: a u32 length (of what follows), a u8 kind, then
// the payload. Little-endian.
const (
	FrameControl byte = 0 // a JSON message
	FrameOutput  byte = 1 // host to client: u32 pane, then the program's bytes
	FrameInput   byte = 2 // client to host: u32 pane, then the bytes typed
)

// maxFrame bounds a frame, so a broken peer cannot make the other side
// allocate without end.
const maxFrame = 16 << 20

// Message is a control message, both ways. Requests carry an ID that
// their reply repeats; events have none.
type Message struct {
	Type string `json:"type"`
	ID   int    `json:"id,omitempty"`

	Version int    `json:"version,omitempty"` // hello, welcome
	Error   string `json:"error,omitempty"`   // a reply that failed
	// Sizes, in a welcome: the host tells its clients each size a pane
	// takes, in the output's order (an older host does not).
	Sizes bool `json:"sizes,omitempty"`
	// Snapshot, in an attach: the pane as it is now, drawn again at its
	// size, in place of its replay; in a welcome: the host makes them.
	Snapshot bool `json:"snapshot,omitempty"`
	Confirm  bool `json:"confirm,omitempty"` // close: explicit user consent

	Pane  int      `json:"pane,omitempty"`
	Argv  []string `json:"argv,omitempty"`   // create
	Dir   string   `json:"dir,omitempty"`    // create; event "pwd"
	Env   []string `json:"env,omitempty"`    // create
	Cols  int      `json:"cols,omitempty"`   // create, resize; event: the size taken
	Rows  int      `json:"rows,omitempty"`   // create, resize; event: the size taken
	CellW int      `json:"cell_w,omitempty"` // create, resize: a cell in pixels, for size reports
	CellH int      `json:"cell_h,omitempty"`
	Title string   `json:"title,omitempty"`
	Code  *int     `json:"code,omitempty"` // event "exit"
	Git   *Git     `json:"git,omitempty"`  // event: the pane's checkout, the zero Git for none
	// Clipboard is text the program put on the clipboard (OSC 52), or on
	// the primary selection when Primary; for the attached windows to set.
	Clipboard *string `json:"clipboard,omitempty"`
	Primary   bool    `json:"primary,omitempty"`

	// Workspaces and sessions: what a request is about, and their names.
	Workspace int     `json:"workspace,omitempty"`
	Session   int     `json:"session,omitempty"`
	Name      string  `json:"name,omitempty"`
	Split     string  `json:"split,omitempty"`     // split: "right" or "down"
	Op        string  `json:"op,omitempty"`        // layout: what to do
	Direction int     `json:"direction,omitempty"` // layout: a layout.Dir
	Step      float64 `json:"step,omitempty"`      // layout "resize"
	Path      string  `json:"path,omitempty"`      // layout "ratio": the split
	Ratio     float64 `json:"ratio,omitempty"`     // layout "ratio"
	To        int     `json:"to,omitempty"`        // move: the new place
	From      int     `json:"from,omitempty"`      // layout "swap": the pane dragged
	Active    bool    `json:"active,omitempty"`    // active
	// report: the agent's type, its state, what it asks or did.
	Agent      string    `json:"agent,omitempty"`
	AgentState string    `json:"agent_state,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	State      *State    `json:"state,omitempty"` // reply to state; event "state"
	UI         *model.UI `json:"ui,omitempty"`    // ui

	Panes  []PaneInfo `json:"panes,omitempty"`  // reply to list
	Colors *Colors    `json:"colors,omitempty"` // colors

	// Web, in a hello: the client is a web page (inua-termd serve's), one
	// the passphrases gpg asks for are relayed to (secrets.go).
	Web bool `json:"web,omitempty"`
	// Ask is the question inua-pinentry or inua-askpass asks (secret);
	// the host makes Secret of it, from the asking process (asker.Vouch),
	// for the pages (secret, the host's event). Asked names one (answer,
	// withdraw).
	Ask    *pinentry.Request `json:"ask,omitempty"`
	Secret *Secret           `json:"secret,omitempty"`
	Asked  int               `json:"asked,omitempty"`
	// Answer is a page's: AnswerOK (with Pin for a passphrase), AnswerNo
	// or AnswerCancel; Pin goes on in the reply to the secret.
	Answer string  `json:"answer,omitempty"`
	Pin    *string `json:"pin,omitempty"`
}

// Colors are the window's default colours, 0xRRGGBB: what programs are
// told when they ask (OSC 10, 11, 12, and dark or light).
type Colors struct {
	FG     uint32 `json:"fg"`
	BG     uint32 `json:"bg"`
	Cursor uint32 `json:"cursor"`
	// Palette is the 16 colours (OSC 4), when the window has them.
	Palette *[16]uint32 `json:"palette,omitempty"`
}

// State is everything the columns show: the workspaces, their sessions
// and layouts, and every pane.
type State struct {
	Model model.Model `json:"model"`
	Panes []PaneInfo  `json:"panes"`
}

// Layout ops (Message.Op of a "layout").
const (
	OpFocus    = "focus"    // Pane, or the neighbour in Direction
	OpSwap     = "swap"     // the focused pane (or From) with the neighbour in Direction, or with Pane
	OpResize   = "resize"   // the focused pane's edge in Direction, by Step
	OpEqualize = "equalize" //
	OpZoom     = "zoom"     // the focused pane to the whole session, and back
	OpPreset   = "preset"   // the next preset, or Name's
	OpRatio    = "ratio"    // the split at Path to Ratio
	OpMove     = "move"     // Pane (the focused one when 0) to session To, beside From (its focused one when 0)
)

// PaneInfo is what the host knows of a pane.
type PaneInfo struct {
	ID     int      `json:"id"`
	Argv   []string `json:"argv"`
	Title  string   `json:"title"`
	Dir    string   `json:"dir"` // the folder the shell reports (OSC 7), else where it started
	Cols   int      `json:"cols"`
	Rows   int      `json:"rows"`
	Exited bool     `json:"exited"`
	// Restored is set for a pane the host brought back, until it is typed
	// in.
	Restored bool `json:"restored,omitempty"`
	// Command is what runs in the pane's shell now, "" while it waits.
	Command string `json:"command,omitempty"`
	// The agent in the pane, when one runs: its type, name, mark and colour, its
	// state since when (Unix milliseconds), and what it asks.
	Agent     string `json:"agent,omitempty"`
	AgentName string `json:"agent_name,omitempty"`
	Icon      string `json:"icon,omitempty"`
	Color     string `json:"color,omitempty"` // the mark's, "#rrggbb"
	State     string `json:"state,omitempty"`
	Since     int64  `json:"since,omitempty"`
	Ask       string `json:"ask,omitempty"`
	Code      int    `json:"code"`
	Git       *Git   `json:"git,omitempty"` // the checkout the folder is in
}

// Message types.
const (
	MsgHello   = "hello"   // client: the first message; Version
	MsgWelcome = "welcome" // host: accepted; Version
	MsgReply   = "reply"   // host: to request ID; Error, or what it asked
	MsgList    = "list"    // client: the panes
	MsgCreate  = "create"  // client: a pane running Argv in Dir; reply Pane
	MsgAttach  = "attach"  // client: the pane's replay (or Snapshot), then its output
	MsgDetach  = "detach"  // client: no more of the pane's output
	MsgResize  = "resize"  // client: Cols × Rows, CellW × CellH
	MsgColors  = "colors"  // client: the Colors programs are told, for every pane
	MsgShell   = "shell"   // client: the shell new panes start, Argv[0]; none for $SHELL
	MsgQuit    = "quit"    // client: end the host, its sessions kept for the next
	MsgClose   = "close"   // client: end the pane's program and forget it
	MsgEvent   = "event"   // host: Title, Dir, Git or Code changed for Pane, or Clipboard set
	// The workspaces and sessions; each change sends every client a
	// "state" event.
	MsgState        = "state"         // client: the State; host: the State changed
	MsgOpen         = "open"          // client: the workspace for Dir, made (with a session) if new, shown; reply Workspace
	MsgNewSession   = "new_session"   // client: a session in Workspace, shown; reply Session
	MsgSplit        = "split"         // client: a pane beside Session's focused one; reply Pane
	MsgLayout       = "layout"        // client: Op on Session
	MsgShow         = "show"          // client: show Workspace, and Session (0: its last)
	MsgRename       = "rename"        // client: Name for Session, else Workspace
	MsgCloseSession = "close_session" // client: end Session's panes and forget it
	MsgRemove       = "remove"        // client: forget Workspace, ending its panes
	MsgMove         = "move"          // client: Workspace to place To
	MsgUI           = "ui"            // client: UI, how the window is
	MsgActive       = "active"        // client: its window is the active one (Active) or not
	MsgReport       = "report"        // client: an agent's state for Pane: Agent, AgentState, Title, Summary
	MsgRun          = "run"           // client: a session running Argv in Dir, in the Commands workspace; shown
	// What gpg asks, relayed to the web pages (secrets.go).
	MsgSecret   = "secret"   // client: Secret for the pages; reply Pin, or Error; host: Secret to a page
	MsgAnswer   = "answer"   // page: Answer (and Pin) for the secret Asked
	MsgWithdraw = "withdraw" // host: the secret Asked is answered or given up
)

// SocketPath is the host's socket: $XDG_RUNTIME_DIR/inua/term.sock.
func SocketPath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "inua", "term.sock")
}

// PasteDir is where files given to a pane are kept, for their paths to be
// typed: images pasted in the window, files uploaded from the web page.
// $XDG_RUNTIME_DIR/inua/term-paste goes with the session.
func PasteDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "inua", "term-paste")
	}
	return filepath.Join(os.TempDir(), "inua-term-paste")
}

// WriteFrame writes a frame: its kind, then the payload's parts. A
// client that relays frames (inua-term serve's) uses it with ReadFrame.
func WriteFrame(w io.Writer, kind byte, payload ...[]byte) error {
	n := 1
	for _, p := range payload {
		n += len(p)
	}
	if n > maxFrame {
		return fmt.Errorf("host: frame of %d bytes", n)
	}
	head := make([]byte, 5, 5+n)
	binary.LittleEndian.PutUint32(head, uint32(n))
	head[4] = kind
	buf := head
	for _, p := range payload {
		buf = append(buf, p...)
	}
	_, err := w.Write(buf)
	return err
}

// ReadFrame reads a frame: its kind, and the payload.
func ReadFrame(r io.Reader) (kind byte, payload []byte, err error) {
	var head [5]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return 0, nil, err
	}
	n := binary.LittleEndian.Uint32(head[:4])
	if n < 1 || n > maxFrame {
		return 0, nil, fmt.Errorf("host: frame of %d bytes", n)
	}
	payload = make([]byte, n-1)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	return head[4], payload, nil
}

func writeMessage(w io.Writer, m Message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return WriteFrame(w, FrameControl, b)
}

func paneBytes(pane int, b []byte) [][]byte {
	var id [4]byte
	binary.LittleEndian.PutUint32(id[:], uint32(pane))
	return [][]byte{id[:], b}
}

func splitPane(payload []byte) (int, []byte, error) {
	if len(payload) < 4 {
		return 0, nil, errors.New("host: short pane frame")
	}
	return int(binary.LittleEndian.Uint32(payload)), payload[4:], nil
}
