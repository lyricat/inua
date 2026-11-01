package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lyricat/inua/internal/term/agent"
	"github.com/lyricat/inua/internal/term/model"
	"github.com/lyricat/inua/internal/term/pty"
	"github.com/lyricat/inua/internal/term/vt"
)

// ReplayLimit is how much of a pane's recent output the host keeps for a
// client attaching later.
const ReplayLimit = 4 << 20

// hostScrollback is the scrollback of the host's own engines, in lines.
const hostScrollback = 1000

// answersLen is how many answers to a pane's queries wait for the program
// to read its input; more are dropped, so a program that asks without
// reading cannot stop the host reading its output.
const answersLen = 64

// queueLen is how many frames wait for a slow client before the host
// gives up on it; it can attach again and get the replay.
const queueLen = 1024

// Host owns the panes and serves clients on a socket.
type Host struct {
	ln          net.Listener
	path        string // the socket's
	replayLimit int
	// ExitWhenEmpty ends Serve once the last pane has been closed.
	ExitWhenEmpty bool

	mu     sync.Mutex
	panes  map[int]*pane
	colors *Colors // the window's, for new panes
	m      model.Model
	// ShellDir holds the shell integration's scripts (shellint); "" for
	// none.
	ShellDir string
	// shellProg is the shell new panes start, "" for $SHELL (SetShell).
	shellProg string
	// Agents are the manifests panes' agents are known by; nil follows
	// none. Notify posts notifications for agents that need the user.
	Agents *agent.Set
	Notify bool
	// Desktop posts the notifications and brings a window up after one's
	// Show; nil, as on a computer with no desktop, posts none.
	Desktop Desktop
	// Remembering: the file, the forget patterns, whether commands are
	// kept, and the save waiting.
	saveFile     string
	forget       []string
	keepCommands bool
	saveTimer    *time.Timer
	// waiting are the kept sessions whose folders are not there yet;
	// waitIDs numbers their panes, below zero.
	waiting []waitingSession
	waitIDs int
	next    int
	clients map[*client]bool
	// asked are the secrets gpg asks the web pages, by their IDs.
	asked     map[int]*asked
	secretIDs int
	closed    bool
	done      chan struct{}
}

// pane is a program on a PTY, with the host's own engine for its state.
type pane struct {
	id   int
	host *Host
	argv []string
	dir  string

	pty  *pty.PTY
	term *vt.Terminal // nil without libinua-vt: no title, folder or answers then
	// answers go to the program in order, from their own goroutine.
	answers chan []byte

	mu       sync.Mutex
	replay   []byte
	title    string
	pwd      string
	git      Git // the checkout the folder is in
	cols     int
	rows     int
	exited   bool
	code     int
	attached map[*client]bool
	// wants are the sizes the clients asked for. A pane has one size: the
	// last asked for, by whichever screen looked at it last (a phone
	// opening it takes it to the phone's size); when that client lets go
	// (detaches, or goes), the pane takes the size the last of the others
	// asked for (back to the desktop's).
	wants map[*client]want
	// lastRun is the command that ran last; pending a restored command
	// put back at the prompt once the shell takes input; restored is set
	// until the user types.
	lastRun  string
	pending  string
	restored bool
	agent    paneAgent
}

// want is a size a client asked for, and when: seq orders the asks.
type want struct {
	cols, rows, cellW, cellH int
	seq                      uint64
}

// wantSeq orders the clients' asks for sizes, across panes.
var wantSeq atomic.Uint64

// client is a connection to the host.
type client struct {
	conn   net.Conn
	active bool          // its window is the active one
	web    bool          // a web page (inua-termd serve's)
	out    chan [][]byte // frames: kind byte first, then payload parts
	// mu guards out's closing: a frame is not sent to a closed queue.
	mu     sync.Mutex
	closed bool
}

// Listen makes a host on the socket at path: its folder is made private
// to the user, and a socket left by a host that is gone is replaced.
func Listen(path string) (*Host, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if c, err := net.Dial("unix", path); err == nil {
		c.Close()
		return nil, fmt.Errorf("host: %s: a host is running", path)
	}
	os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	return &Host{
		ln: ln, path: path, replayLimit: ReplayLimit,
		panes: map[int]*pane{}, clients: map[*client]bool{}, asked: map[int]*asked{}, done: make(chan struct{}),
	}, nil
}

// Serve accepts clients until Close, or until the last pane is closed
// with ExitWhenEmpty set.
func (h *Host) Serve() error {
	go func() {
		<-h.done
		h.ln.Close()
	}()
	go h.follow()
	for {
		c, err := h.ln.Accept()
		if err != nil {
			select {
			case <-h.done:
				return nil
			default:
				return err
			}
		}
		go h.serveConn(c)
	}
}

// Close stops the host: every program gets SIGHUP.
func (h *Host) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()
	h.Save() // the sessions as they are, before their programs end
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	if h.saveTimer != nil {
		h.saveTimer.Stop()
		h.saveTimer = nil
	}
	panes := h.panes
	h.panes = map[int]*pane{}
	clients := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()
	for _, p := range panes {
		p.pty.Close()
	}
	for _, c := range clients {
		c.finish() // what is queued (the reply to the last close) still goes out
	}
	close(h.done)
}

func (h *Host) serveConn(conn net.Conn) {
	c := &client{conn: conn, out: make(chan [][]byte, queueLen)}
	defer h.drop(c)
	// The first message is hello, with our version.
	kind, payload, err := ReadFrame(conn)
	var hello Message
	if err != nil || kind != FrameControl || json.Unmarshal(payload, &hello) != nil || hello.Type != MsgHello {
		return
	}
	if hello.Version != Version {
		writeMessage(conn, Message{Type: MsgReply, Error: fmt.Sprintf("protocol %d, the host speaks %d", hello.Version, Version)})
		return
	}
	if err := writeMessage(conn, Message{Type: MsgWelcome, Version: Version, Sizes: true, Snapshot: true}); err != nil {
		return
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	// A page is inua-termd serve's: no other program answers what is asked.
	c.web = hello.Web && isPage(conn)
	h.clients[c] = true
	h.mu.Unlock()
	go c.write()
	if c.web {
		h.secretsFor(c)
	}
	for {
		kind, payload, err := ReadFrame(conn)
		if err != nil {
			return
		}
		switch kind {
		case FrameControl:
			var m Message
			if err := json.Unmarshal(payload, &m); err != nil {
				return
			}
			h.handle(c, m)
		case FrameInput:
			id, b, err := splitPane(payload)
			if err != nil {
				return
			}
			if p := h.pane(id); p != nil {
				p.pty.Write(b)
				h.used(id)
			}
		default:
			return
		}
	}
}

// write sends the client's frames in order, and closes the connection
// when the queue is closed and empty.
func (c *client) write() {
	defer c.conn.Close()
	for f := range c.out {
		if err := WriteFrame(c.conn, f[0][0], f[1:]...); err != nil {
			c.close()
			return
		}
	}
}

// send queues a frame; a client too far behind is let go, and one gone
// gets nothing.
func (c *client) send(kind byte, payload ...[]byte) {
	f := append([][]byte{{kind}}, payload...)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	select {
	case c.out <- f:
	default:
		c.closeLocked(true)
	}
}

func (c *client) message(m Message) {
	b, err := json.Marshal(m)
	if err == nil {
		c.send(FrameControl, b)
	}
}

// close drops the client now.
func (c *client) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeLocked(true)
}

// finish lets the client's writer send what is queued, then close.
func (c *client) finish() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeLocked(false)
}

// closeLocked closes the queue once, and the connection now with conn;
// c.mu is held.
func (c *client) closeLocked(conn bool) {
	if c.closed {
		return
	}
	c.closed = true
	if conn {
		c.conn.Close()
	}
	close(c.out)
}

// drop forgets a client that went away.
func (h *Host) drop(c *client) {
	h.withdrawFrom(c)
	h.mu.Lock()
	delete(h.clients, c)
	panes := make([]*pane, 0, len(h.panes))
	for _, p := range h.panes {
		panes = append(panes, p)
	}
	h.mu.Unlock()
	for _, p := range panes {
		p.mu.Lock()
		delete(p.attached, c)
		p.mu.Unlock()
		p.letGo(c)
	}
	c.close()
}

func (h *Host) pane(id int) *pane {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.panes[id]
}

func (h *Host) handle(c *client, m Message) {
	if h.handleSessions(c, m) || h.handleSecrets(c, m) {
		return
	}
	reply := Message{Type: MsgReply, ID: m.ID}
	fail := func(err error) {
		reply.Error = err.Error()
		c.message(reply)
	}
	switch m.Type {
	case MsgList:
		reply.Panes = h.list()
	case MsgColors:
		if m.Colors == nil {
			fail(errors.New("no colors"))
			return
		}
		h.setColors(*m.Colors)
	case MsgShell:
		program := ""
		if len(m.Argv) > 0 {
			program = m.Argv[0]
		}
		h.SetShell(program)
	case MsgQuit:
		// The reply goes out, then the host keeps its sessions and ends.
		c.message(reply)
		go h.Close()
		return
	case MsgCreate:
		id, err := h.create(m)
		if err != nil {
			fail(err)
			return
		}
		reply.Pane = id
	case MsgAttach, MsgDetach, MsgResize, MsgClose:
		p := h.pane(m.Pane)
		if p == nil {
			fail(fmt.Errorf("no pane %d", m.Pane))
			return
		}
		switch m.Type {
		case MsgAttach:
			p.attach(c, m.Snapshot) // the replay goes out before this reply
		case MsgDetach:
			p.mu.Lock()
			delete(p.attached, c)
			p.mu.Unlock()
			p.letGo(c)
		case MsgResize:
			if err := p.resize(m.Cols, m.Rows, m.CellW, m.CellH); err != nil {
				fail(err)
				return
			}
			p.mu.Lock()
			p.wants[c] = want{m.Cols, m.Rows, m.CellW, m.CellH, wantSeq.Add(1)}
			p.mu.Unlock()
		case MsgClose:
			if !m.Confirm && p.closeNeedsConfirmation() {
				fail(ErrCloseConfirmation)
				return
			}
			last := h.closePane(p)
			h.forgetPane(p.id)
			if last && h.ExitWhenEmpty {
				// The last pane: the reply goes out, then the host ends.
				reply.Pane = m.Pane
				c.message(reply)
				go h.Close()
				return
			}
			defer h.publish()
		}
		reply.Pane = m.Pane
	default:
		fail(fmt.Errorf("unknown request %q", m.Type))
		return
	}
	c.message(reply)
}

func (h *Host) list() []PaneInfo {
	h.mu.Lock()
	panes := make([]*pane, 0, len(h.panes))
	for _, p := range h.panes {
		panes = append(panes, p)
	}
	h.mu.Unlock()
	infos := make([]PaneInfo, 0, len(panes))
	for _, p := range panes {
		p.mu.Lock()
		infos = append(infos, PaneInfo{
			ID: p.id, Argv: p.argv, Title: p.title, Dir: p.folder(),
			Cols: p.cols, Rows: p.rows, Exited: p.exited, Code: p.code,
			Restored: p.restored, Command: p.agent.command, Git: p.gitInfo(),
			Agent: p.agent.t.Type, State: string(p.agent.t.State), Ask: p.agent.t.Ask,
		})
		if m := p.agent.m; m != nil {
			i := &infos[len(infos)-1]
			i.AgentName, i.Icon, i.Color, i.Since = m.Name, m.Icon, m.Color, p.agent.t.Since.UnixMilli()
			if p.agent.title != "" {
				i.Title = p.agent.title
			}
		}
		p.mu.Unlock()
	}
	return infos
}

func (h *Host) create(m Message) (int, error) {
	return h.start(m.Argv, m.Dir, m.Env, m.Cols, m.Rows, m.CellW, m.CellH)
}

// start runs argv in dir on a new pane of cols × rows. Its environment
// is env (nil: the host's) with INUA_TERM_PANE and INUA_TERM_SOCK, which
// `inua-term report` uses.
func (h *Host) start(argv []string, dir string, env []string, cols, rows, cellW, cellH int) (int, error) {
	if len(argv) == 0 {
		return 0, errors.New("no command")
	}
	if cols <= 0 || rows <= 0 {
		cols, rows = 80, 24
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return 0, errors.New("the host is closing")
	}
	h.next++
	id := h.next
	colors := h.colors
	h.mu.Unlock()
	if env == nil {
		env = os.Environ()
	}
	env = append(slices.Clip(env), "INUA_TERM_PANE="+strconv.Itoa(id), "INUA_TERM_SOCK="+h.path)
	pt, err := pty.Start(argv, dir, env, cols, rows)
	if err != nil {
		return 0, err
	}
	p := &pane{id: id, host: h, argv: argv, dir: dir, pty: pt, cols: cols, rows: rows, attached: map[*client]bool{}, wants: map[*client]want{}, answers: make(chan []byte, answersLen)}
	p.git = readGit(dir)
	if t, err := vt.New(cols, rows); err == nil {
		p.term = t
		// The host reads a screen's last rows; the windows keep the long
		// scrollback.
		t.SetDefaults(hostScrollback, vt.CursorBlock, false)
		t.Answer()
		t.SetCellSize(cellW, cellH)
		if colors != nil {
			t.SetColors(colors.FG, colors.BG, colors.Cursor)
			if colors.Palette != nil {
				t.SetPalette(*colors.Palette)
			}
		}
	} else {
		log.Printf("host: pane state off: %v", err)
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		pt.Close()
		return 0, errors.New("the host is closing")
	}
	h.panes[p.id] = p
	h.mu.Unlock()
	go h.read(p)
	go p.answer()
	return p.id, nil
}

// answer writes the engine's answers to the program until it ends.
func (p *pane) answer() {
	for b := range p.answers {
		p.pty.Write(b)
	}
}

// setColors applies the window's colours to every pane, and to new ones.
func (h *Host) setColors(c Colors) {
	h.mu.Lock()
	h.colors = &c
	panes := make([]*pane, 0, len(h.panes))
	for _, p := range h.panes {
		panes = append(panes, p)
	}
	h.mu.Unlock()
	for _, p := range panes {
		p.mu.Lock()
		if p.term != nil {
			p.term.SetColors(c.FG, c.BG, c.Cursor)
			if c.Palette != nil {
				p.term.SetPalette(*c.Palette)
			}
		}
		p.mu.Unlock()
	}
}

// read follows a pane's program until it ends.
func (h *Host) read(p *pane) {
	buf := make([]byte, 64*1024)
	for {
		n, err := p.pty.Read(buf)
		if n > 0 {
			// Its own copy: the clients' queues hold it while buf is read into again.
			p.output(append([]byte(nil), buf[:n]...), h.replayLimit)
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("host: pane %d: %v", p.id, err)
			}
			break
		}
	}
	close(p.answers)
	code, _ := p.pty.Wait()
	p.mu.Lock()
	p.exited, p.code = true, code
	p.mu.Unlock()
	h.tell(Message{Type: MsgEvent, Pane: p.id, Code: &code})
	h.closeExited(p, code)
}

// exitGrace is how long a pane its program left by a signal waits before
// it closes: a logout hangs the shells up as it ends the host, and the
// host keeps their sessions first.
var exitGrace = time.Second

// closeExited closes a session's pane once its program ended, as a
// terminal's split closes; the layout closes up, a session without panes
// goes, and the host ends with its last pane when it should. A pane
// outside the sessions (Create) stays, exited, for its client.
func (h *Host) closeExited(p *pane, code int) {
	if code < 0 {
		time.Sleep(exitGrace)
	}
	h.mu.Lock()
	s, _ := h.m.PaneSession(p.id)
	if h.closed || s == nil {
		h.mu.Unlock()
		return
	}
	h.m.RemovePane(p.id)
	h.mu.Unlock()
	h.publish()
	h.endPanes([]int{p.id})
}

// output keeps, feeds and sends what the program wrote, and tells the
// clients when its title or folder changed.
func (p *pane) output(b []byte, limit int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.replay = append(p.replay, b...)
	if over := len(p.replay) - limit; over > 0 {
		p.replay = append(p.replay[:0], p.replay[over:]...)
	}
	for c := range p.attached {
		c.send(FrameOutput, paneBytes(p.id, b)...)
	}
	if p.term == nil {
		return
	}
	p.term.Write(b)
	// The checkout is read again in a new folder, and at each prompt:
	// a command run may have changed the branch.
	look := false
	if marks := p.term.Prompts(); marks != nil {
		p.prompts(marks)
		look = slices.ContainsFunc(marks, func(m vt.Prompt) bool { return m.Kind == vt.PromptStart })
	}
	if ns := p.term.Notifications(); ns != nil {
		p.host.programNotes(p, ns)
	}
	p.host.readScreenLater(p)
	if a := p.term.Answers(); a != nil && !p.exited {
		select {
		case p.answers <- a:
		default:
			log.Printf("host: pane %d does not read its input: an answer dropped", p.id)
		}
	}
	if text, primary, ok := p.term.Clipboard(); ok {
		p.broadcast(Message{Type: MsgEvent, Pane: p.id, Clipboard: &text, Primary: primary})
	}
	if t := p.term.Title(); t != p.title {
		p.title = t
		p.host.tell(Message{Type: MsgEvent, Pane: p.id, Title: t})
	}
	if d := p.term.Pwd(); d != p.pwd {
		p.pwd = d
		p.host.tell(Message{Type: MsgEvent, Pane: p.id, Dir: p.folder()})
		look = true
	}
	if look {
		p.lookGit()
	}
}

// broadcast tells the attached clients; p.mu is held.
func (p *pane) broadcast(m Message) {
	for c := range p.attached {
		c.message(m)
	}
}

// folder is where the pane's shell is: what it reported (OSC 7), else
// where it started. p.mu is held.
func (p *pane) folder() string {
	if p.pwd != "" {
		if u, err := url.Parse(p.pwd); err == nil && u.Scheme == "file" && u.Path != "" {
			return u.Path
		}
	}
	return p.dir
}

// attach sends the replay and then everything after it, with nothing in
// between lost or sent twice.
func (p *pane) attach(c *client, snapshot bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Its size first: the client's engine takes it before the replay.
	if p.cols > 0 && p.rows > 0 {
		c.message(Message{Type: MsgEvent, Pane: p.id, Cols: p.cols, Rows: p.rows})
	}
	// A snapshot is the screen and the scrollback as the engine has them,
	// rewrapped at every resize: the replay is the output as it came, for
	// whatever size each part was meant for, and draws only as it did if
	// the size never changed.
	var b []byte
	if snapshot && p.term != nil {
		var err error
		if b, err = p.term.Snapshot(); err != nil {
			log.Printf("host: pane %d: snapshot: %v", p.id, err)
			b = nil
		}
	}
	if b == nil {
		b = append([]byte(nil), p.replay...)
	}
	if len(b) > 0 {
		c.send(FrameOutput, paneBytes(p.id, b)...)
	}
	p.attached[c] = true
	c.message(Message{Type: MsgEvent, Pane: p.id, Title: p.title, Dir: p.folder(), Git: &p.git})
	if p.exited {
		code := p.code
		c.message(Message{Type: MsgEvent, Pane: p.id, Code: &code})
	}
}

// letGo forgets the size a client asked for; when it was the pane's, the
// pane takes the size the last of the others asked for.
func (p *pane) letGo(c *client) {
	p.mu.Lock()
	w, ok := p.wants[c]
	delete(p.wants, c)
	var back want
	for _, o := range p.wants {
		if o.seq > back.seq {
			back = o
		}
	}
	// Its size, unless another asked after it.
	current := ok && w.seq > back.seq
	p.mu.Unlock()
	if !current || back.seq == 0 || (back.cols == w.cols && back.rows == w.rows) {
		return
	}
	if err := p.resize(back.cols, back.rows, back.cellW, back.cellH); err != nil {
		log.Printf("host: pane %d back to %dx%d: %v", p.id, back.cols, back.rows, err)
	}
}

func (p *pane) resize(cols, rows, cellW, cellH int) error {
	if cols <= 0 || rows <= 0 {
		return fmt.Errorf("size %dx%d", cols, rows)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cols, p.rows = cols, rows
	if p.term != nil {
		p.term.Resize(cols, rows)
		if cellW > 0 && cellH > 0 {
			p.term.SetCellSize(cellW, cellH)
		}
	}
	// The clients' engines take the size here, between the output before
	// and the output after, as this one did: the program's output on its
	// way, meant for the old size, reaches them at the old size.
	p.broadcast(Message{Type: MsgEvent, Pane: p.id, Cols: cols, Rows: rows})
	if p.exited {
		return nil
	}
	return p.pty.Resize(cols, rows)
}

// closePane ends a pane's program and forgets the pane; it reports
// whether that was the last one.
func (h *Host) closePane(p *pane) bool {
	h.mu.Lock()
	delete(h.panes, p.id)
	empty := len(h.panes) == 0
	h.mu.Unlock()
	p.pty.Close()
	p.mu.Lock()
	if p.term != nil {
		p.term.Close()
		p.term = nil
	}
	p.mu.Unlock()
	return empty
}

// SetReplayLimit is how much of each pane's recent output the host keeps
// for a client attaching later.
func (h *Host) SetReplayLimit(n int) {
	if n > 0 {
		h.replayLimit = n
	}
}
