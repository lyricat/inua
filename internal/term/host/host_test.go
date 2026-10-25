package host

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/term/agent"
	"github.com/lyricat/inua/internal/term/layout"
	"github.com/lyricat/inua/internal/term/shellint"
	"github.com/lyricat/inua/internal/term/vt"
)

func init() {
	// The engine scripts/libghostty.sh built, for titles and folders.
	if os.Getenv("INUA_VT_LIB") == "" {
		if p, err := filepath.Abs(filepath.Join("..", "..", "..", "bin", vt.Library)); err == nil {
			os.Setenv("INUA_VT_LIB", p)
		}
	}
}

func startHost(t *testing.T) (*Host, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "inua", "term.sock")
	h, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	go h.Serve()
	t.Cleanup(h.Close)
	return h, path
}

// watcher collects a client's output and events.
type watcher struct {
	mu     sync.Mutex
	out    map[int]*strings.Builder
	events []Message
}

func newWatcher() *watcher { return &watcher{out: map[int]*strings.Builder{}} }

func (w *watcher) output(pane int, b []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.out[pane] == nil {
		w.out[pane] = &strings.Builder{}
	}
	w.out[pane].Write(b)
}

func (w *watcher) event(m Message) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.events = append(w.events, m)
}

func (w *watcher) text(pane int) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.out[pane] == nil {
		return ""
	}
	return w.out[pane].String()
}

func (w *watcher) waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func dial(t *testing.T, path string, w *watcher) *Client {
	t.Helper()
	c, err := Dial(path, w.output, w.event)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestPane(t *testing.T) {
	_, path := startHost(t)
	w := newWatcher()
	c := dial(t, path, w)
	dir := t.TempDir()
	id, err := c.Create([]string{"sh", "-c", "echo hello; stty size; read x; echo got $x; read y"}, dir, nil, 90, 20)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Attach(id); err != nil {
		t.Fatal(err)
	}
	w.waitFor(t, "hello", func() bool { return strings.Contains(w.text(id), "20 90") })
	if err := c.Input(id, []byte("there\n")); err != nil {
		t.Fatal(err)
	}
	w.waitFor(t, "the echo of input", func() bool { return strings.Contains(w.text(id), "got there") })

	panes, err := c.List()
	if err != nil || len(panes) != 1 || panes[0].ID != id || panes[0].Dir != dir || panes[0].Cols != 90 || panes[0].Exited {
		t.Fatalf("list %+v, %v", panes, err)
	}
	if err := c.Resize(id, 100, 30, 0, 0); err != nil {
		t.Fatal(err)
	}
	if panes, _ := c.List(); panes[0].Cols != 100 || panes[0].Rows != 30 {
		t.Errorf("after resize %+v", panes[0])
	}
}

// A client hears each size a pane takes in the output's order: on
// attaching, before the replay; on a resize, between the output before
// and the output after.
func TestSizesInOrder(t *testing.T) {
	_, path := startHost(t)
	var mu sync.Mutex
	var log []string
	add := func(s string) {
		mu.Lock()
		log = append(log, s)
		mu.Unlock()
	}
	c, err := Dial(path, func(_ int, b []byte) { add("out " + string(b)) }, func(m Message) {
		if m.Cols > 0 {
			add(fmt.Sprintf("size %dx%d", m.Cols, m.Rows))
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.TellsSizes() {
		t.Fatal("the host does not tell sizes")
	}
	id, err := c.Create([]string{"sh", "-c", "echo one; read x; echo two; read y"}, t.TempDir(), nil, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	// The output starts before the attach: it comes as the replay.
	time.Sleep(200 * time.Millisecond)
	if err := c.Attach(id); err != nil {
		t.Fatal(err)
	}
	if err := c.Resize(id, 100, 30, 0, 0); err != nil {
		t.Fatal(err)
	}
	c.Input(id, []byte("\n"))
	at := func(s string) int {
		mu.Lock()
		defer mu.Unlock()
		for i, l := range log {
			if strings.Contains(l, s) {
				return i
			}
		}
		return -1
	}
	deadline := time.Now().Add(5 * time.Second)
	for at("two") < 0 {
		if time.Now().After(deadline) {
			t.Fatalf("no output after the resize: %q", log)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !(at("size 80x24") < at("one") && at("one") < at("size 100x30") && at("size 100x30") < at("two")) {
		t.Errorf("out of order: %q", log)
	}
}

// The shell the window sets is what new panes start, and their SHELL.
func TestSetShell(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	_, path := startHost(t)
	w := newWatcher()
	c := dial(t, path, w)
	sh := filepath.Join(t.TempDir(), "myshell")
	os.WriteFile(sh, []byte("#!/bin/sh\necho \"in $0 as $SHELL\"\nread x\n"), 0o755)
	if err := c.SetShell(sh); err != nil {
		t.Fatal(err)
	}
	c.Open(t.TempDir(), "")
	st, _ := c.State()
	id := st.Model.Workspaces[0].Sessions[0].Focus
	c.Attach(id)
	want := "in " + sh + " as " + sh
	w.waitFor(t, want, func() bool { return strings.Contains(w.text(id), want) })
}

// A client attaching later gets what came before; two clients both get
// what comes after.
func TestReplayAndTwoClients(t *testing.T) {
	_, path := startHost(t)
	w1 := newWatcher()
	c1 := dial(t, path, w1)
	id, err := c1.Create([]string{"sh", "-c", "echo before; read x; echo after $x; read y"}, "", nil, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	c1.Attach(id)
	w1.waitFor(t, "before", func() bool { return strings.Contains(w1.text(id), "before") })
	c1.Close() // the window goes; the pane stays

	w2 := newWatcher()
	c2 := dial(t, path, w2)
	if err := c2.Attach(id); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w2.text(id), "before") {
		t.Errorf("the replay was not there when Attach returned: %q", w2.text(id))
	}
	w3 := newWatcher()
	c3 := dial(t, path, w3)
	c3.Attach(id)
	c2.Input(id, []byte("both\n"))
	for _, w := range []*watcher{w2, w3} {
		w.waitFor(t, "after", func() bool { return strings.Contains(w.text(id), "after both") })
	}
	if n := strings.Count(w2.text(id), "before"); n != 1 {
		t.Errorf("the replay came %d times", n)
	}
}

// A program that ends leaves its pane, with its code, until closed; the
// host ends with its last pane.
func TestExitAndClose(t *testing.T) {
	h, path := startHost(t)
	h.ExitWhenEmpty = true
	w := newWatcher()
	c := dial(t, path, w)
	id, _ := c.Create([]string{"sh", "-c", "exit 7"}, "", nil, 80, 24)
	c.Attach(id)
	w.waitFor(t, "the exit", func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		for _, m := range w.events {
			if m.Pane == id && m.Code != nil && *m.Code == 7 {
				return true
			}
		}
		return false
	})
	if panes, _ := c.List(); len(panes) != 1 || !panes[0].Exited || panes[0].Code != 7 {
		t.Errorf("after exit %+v", panes)
	}
	if err := c.ClosePane(id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.done:
	case <-time.After(3 * time.Second):
		t.Fatal("the host stays after its last pane")
	}
}

// The title and folder the shell sends come as events.
func TestTitleAndFolder(t *testing.T) {
	if err := vt.Load(); err != nil {
		t.Skip(err)
	}
	_, path := startHost(t)
	w := newWatcher()
	c := dial(t, path, w)
	id, _ := c.Create([]string{"sh", "-c", `printf '\033]2;✳ Refactor\007\033]7;file://host/tmp/work\033\\'; read x`}, "", nil, 80, 24)
	c.Attach(id)
	w.waitFor(t, "the title and folder", func() bool {
		panes, _ := c.List()
		return len(panes) == 1 && panes[0].Title == "✳ Refactor" && panes[0].Dir == "/tmp/work"
	})
}

// A pane's checkout comes with its folder, and is read again at the
// next prompt: a command may have changed the branch.
func TestGitEvents(t *testing.T) {
	if err := vt.Load(); err != nil {
		t.Skip(err)
	}
	repo := t.TempDir()
	write(t, filepath.Join(repo, ".git", "HEAD"), "ref: refs/heads/main\n")
	_, path := startHost(t)
	w := newWatcher()
	c := dial(t, path, w)
	script := `printf '\033]7;file://host%s\033\\'; read x; printf '\033]133;A\007'; read y`
	id, _ := c.Create([]string{"sh", "-c", fmt.Sprintf(script, repo)}, t.TempDir(), nil, 80, 24)
	c.Attach(id)
	branch := func(want string) func() bool {
		return func() bool {
			panes, _ := c.List()
			return len(panes) == 1 && panes[0].Git != nil && panes[0].Git.Root == repo && panes[0].Git.Branch == want
		}
	}
	w.waitFor(t, "the checkout of the new folder", branch("main"))
	write(t, filepath.Join(repo, ".git", "HEAD"), "ref: refs/heads/dev\n")
	c.Input(id, []byte("\n"))
	w.waitFor(t, "the branch read again at the prompt", branch("dev"))
	w.waitFor(t, "the event", func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return slices.ContainsFunc(w.events, func(m Message) bool { return m.Git != nil && m.Git.Branch == "dev" })
	})
}

func TestVersion(t *testing.T) {
	_, path := startHost(t)
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	writeMessage(conn, Message{Type: MsgHello, Version: Version + 1})
	_, payload, err := ReadFrame(conn)
	var m Message
	if err != nil || json.Unmarshal(payload, &m) != nil || m.Type == MsgWelcome || m.Error == "" {
		t.Errorf("another version was welcomed: %+v, %v", m, err)
	}
}

// A second host on the same socket is refused; a stale socket is not in
// the way.
func TestListen(t *testing.T) {
	_, path := startHost(t)
	if _, err := Listen(path); err == nil {
		t.Error("a second host on a live socket")
	}
	stale := filepath.Join(t.TempDir(), "term.sock")
	os.WriteFile(stale, nil, 0o600)
	h, err := Listen(stale)
	if err != nil {
		t.Fatal(err)
	}
	h.Close()
	if fi, err := os.Stat(filepath.Dir(path)); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("socket folder %v %v", fi.Mode(), err)
	}
}

// The host answers a program's queries, with no window attached: device
// attributes, and the window's colours once it said them.
func TestAnswers(t *testing.T) {
	if err := vt.Load(); err != nil {
		t.Skip(err)
	}
	_, path := startHost(t)
	w := newWatcher()
	c := dial(t, path, w)
	if err := c.SetColors(Colors{FG: 0xe0e0e0, BG: 0x0b1012, Cursor: 0x7fb4ca}); err != nil {
		t.Fatal(err)
	}
	// The answers come as input; od shows them as text.
	script := `stty raw -echo; printf '\033[c'; head -c 12 | od -An -c; printf '\033]11;?\033\\'; head -c 24 | od -An -c; stty sane`
	id, err := c.Create([]string{"sh", "-c", script}, "", nil, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	c.Attach(id)
	clean := func() string { return strings.Join(strings.Fields(w.text(id)), "") }
	w.waitFor(t, "the answers", func() bool { return strings.Contains(clean(), "rgb:0b0b/1010/1212") })
	if got := clean(); !strings.Contains(got, `033[?62;22;52c`) {
		t.Errorf("device attributes not answered: %q", got)
	}
}

// A program's clipboard write (OSC 52) reaches the attached clients.
func TestClipboardEvent(t *testing.T) {
	if err := vt.Load(); err != nil {
		t.Skip(err)
	}
	_, path := startHost(t)
	got := make(chan Message, 4)
	c, err := Dial(path, func(int, []byte) {}, func(m Message) {
		if m.Clipboard != nil {
			got <- m
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	id, err := c.Create([]string{"sh", "-c", `read x; printf '\033]52;c;aGVsbG8=\a'; read y`}, "", nil, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Attach(id); err != nil {
		t.Fatal(err)
	}
	c.Input(id, []byte("\n")) // attached: now the write
	select {
	case m := <-got:
		if *m.Clipboard != "hello" || m.Primary || m.Pane != id {
			t.Errorf("event %+v (%q)", m, *m.Clipboard)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no clipboard event")
	}
}

// Workspaces and sessions: opening a folder makes one with a shell,
// splits start beside the focused pane, every client hears of changes,
// and closing the panes takes them away.
func TestSessions(t *testing.T) {
	h, path := startHost(t)
	h.ExitWhenEmpty = false
	states := make(chan *State, 32)
	c, err := Dial(path, nil, func(m Message) {
		if m.Type == MsgState {
			states <- m.State
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	t.Setenv("SHELL", "/bin/sh")
	dir := t.TempDir()
	ws, err := c.Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.State()
	if err != nil {
		t.Fatal(err)
	}
	m := st.Model
	if len(m.Workspaces) != 1 || m.Workspaces[0].ID != ws || len(m.Workspaces[0].Sessions) != 1 || m.Shown.Workspace != ws {
		t.Fatalf("after open: %+v", m)
	}
	sess := m.Workspaces[0].Sessions[0]
	first := sess.Focus
	if again, _ := c.Open(dir, ""); again != ws {
		t.Errorf("opening the folder again: %d, want %d", again, ws)
	}
	second, err := c.Split(sess.ID, "right")
	if err != nil {
		t.Fatal(err)
	}
	third, err := c.Split(sess.ID, "down")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Layout(sess.ID, OpFocus, Message{Direction: int(layout.Left)}); err != nil {
		t.Fatal(err)
	}
	st, _ = c.State()
	s := st.Model.Workspaces[0].Sessions[0]
	if got := s.Tree.Panes(); !slices.Equal(got, []int{first, second, third}) || s.Focus != first {
		t.Errorf("panes %v, focus %d", got, s.Focus)
	}
	if len(st.Panes) != 3 {
		t.Errorf("%d panes listed", len(st.Panes))
	}
	if err := c.Layout(sess.ID, OpPreset, Message{Name: "rows"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Layout(sess.ID, OpZoom, Message{}); err != nil {
		t.Fatal(err)
	}
	c.Rename(0, sess.ID, "review")
	select {
	case <-states:
	case <-time.After(5 * time.Second):
		t.Fatal("no state event")
	}
	st, _ = c.State()
	s = st.Model.Workspaces[0].Sessions[0]
	if s.Zoom != first || s.Name != "review" || s.Tree.Split != layout.Down {
		t.Errorf("zoom %d, name %q, split %q", s.Zoom, s.Name, s.Tree.Split)
	}
	// Typing marks the session used.
	c.Input(second, []byte("\n"))
	deadline := time.Now().Add(5 * time.Second)
	for st, _ = c.State(); !st.Model.Workspaces[0].Sessions[0].Used; st, _ = c.State() {
		if time.Now().After(deadline) {
			t.Fatal("not marked used")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := c.CloseSession(sess.ID); err != nil {
		t.Fatal(err)
	}
	st, _ = c.State()
	// Its last session gone, the workspace goes.
	if w := st.Model.Workspaces; len(w) != 0 || len(st.Panes) != 0 {
		t.Errorf("after closing: %+v, %d panes", w, len(st.Panes))
	}
}

// Remembering: a host keeps the session used and its panes' last
// commands; the next one starts them again in their folders with the
// commands at the prompt, not run.
func TestRemember(t *testing.T) {
	if err := vt.Load(); err != nil {
		t.Skip(err)
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, ".bashrc"), []byte("PS1='$ '\n"), 0o600)
	t.Setenv("HOME", home)
	t.Setenv("SHELL", bash)
	shellDir := t.TempDir()
	if err := shellint.Install(shellDir); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "term.json")
	work := t.TempDir()

	h, path := startHost(t)
	h.ExitWhenEmpty = false
	h.ShellDir = shellDir
	if err := h.Remember(file, nil, true); err != nil {
		t.Fatal(err)
	}
	w := newWatcher()
	c := dial(t, path, w)
	if _, err := c.Open(work, ""); err != nil {
		t.Fatal(err)
	}
	st, _ := c.State()
	s := st.Model.Workspaces[0].Sessions[0]
	first := s.Focus
	second, _ := c.Split(s.ID, "right")
	for _, id := range []int{first, second} {
		c.Attach(id)
	}
	w.waitFor(t, "prompts", func() bool { return strings.Contains(w.text(first), "$ ") && strings.Contains(w.text(second), "$ ") })
	c.Input(first, []byte("git rebase -i origin/ma"))
	c.Input(second, []byte("echo ran\r"))
	w.waitFor(t, "the typing", func() bool {
		return strings.Contains(w.text(first), "origin/ma") && strings.Count(w.text(second), "ran") >= 2
	})
	h.Close()
	b, _ := os.ReadFile(file)
	var saved Saved
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatalf("saved %s: %v", b, err)
	}
	if got := saved.Panes[first].Command; got != "git rebase -i origin/ma" {
		t.Errorf("typed, not run: %q", got)
	}
	if got := saved.Panes[second].Command; got != "echo ran" {
		t.Errorf("run last: %q", got)
	}

	// The next host.
	h2, path2 := startHost(t)
	h2.ExitWhenEmpty = false
	h2.ShellDir = shellDir
	if err := h2.Remember(file, nil, true); err != nil {
		t.Fatal(err)
	}
	w2 := newWatcher()
	c2 := dial(t, path2, w2)
	st, err = c2.State()
	if err != nil || len(st.Model.Workspaces) != 1 || len(st.Model.Workspaces[0].Sessions) != 1 {
		t.Fatalf("restored %+v, %v", st, err)
	}
	panes := st.Model.Workspaces[0].Sessions[0].Tree.Panes()
	if len(panes) != 2 {
		t.Fatalf("panes %v", panes)
	}
	real, _ := filepath.EvalSymlinks(work) // macOS's temporary folders are behind /var → /private/var
	for _, p := range st.Panes {
		if !p.Restored || (p.Dir != work && p.Dir != real) {
			t.Errorf("pane %+v", p)
		}
	}
	for _, id := range panes {
		c2.Attach(id)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("restored panes show %q and %q", w2.text(panes[0]), w2.text(panes[1]))
		}
	})
	w2.waitFor(t, "the commands at the prompt", func() bool {
		// bash shows a paste inverted: the command, after the input mark.
		return strings.Contains(w2.text(panes[0]), "git rebase -i origin/ma") && strings.Contains(w2.text(panes[1]), "echo ran")
	})
	time.Sleep(300 * time.Millisecond)
	if strings.Count(w2.text(panes[1]), "ran") != 1 {
		t.Errorf("the restored command ran: %q", w2.text(panes[1]))
	}
	// Typing makes it the user's.
	c2.Input(panes[0], []byte(" "))
	deadline := time.Now().Add(3 * time.Second)
	for {
		st, _ = c2.State()
		restored := false
		for _, p := range st.Panes {
			restored = restored || p.ID == panes[0] && p.Restored
		}
		if !restored {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("still restored after typing")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestForget(t *testing.T) {
	for _, c := range []struct {
		cmd  string
		want bool
	}{
		{"mysql --password=x", true},
		{"curl 'x?token=abc'", true},
		{"export SECRET_KEY=1", true},
		{"git push", false},
	} {
		if got := forgotten(c.cmd, DefaultForget); got != c.want {
			t.Errorf("forgotten(%q) = %v", c.cmd, got)
		}
	}
}

// An agent in a pane: known by its process, its state read from its
// screen; when it ends, the pane is a shell again.
func TestAgents(t *testing.T) {
	if err := vt.Load(); err != nil {
		t.Skip(err)
	}
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "claude"), []byte(`#!/bin/sh
printf '\033[2J\033[H'
printf '* Writing the test (3s - esc to interrupt)\n'
read go
printf '\033[2J\033[H'
printf 'Bash command\n  rm -rf build\nDo you want to proceed?\n> 1. Yes\n'
read answer
`), 0o755)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("SHELL", "/bin/sh")
	h, path := startHost(t)
	h.ExitWhenEmpty = false
	h.Agents, _ = agent.Load("")
	c := dial(t, path, newWatcher())
	if _, err := c.Open(t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	st, _ := c.State()
	id := st.Model.Workspaces[0].Sessions[0].Focus
	pane := func() PaneInfo {
		st, _ := c.State()
		for _, p := range st.Panes {
			if p.ID == id {
				return p
			}
		}
		return PaneInfo{}
	}
	wait := func(what string, ok func(PaneInfo) bool) PaneInfo {
		t.Helper()
		deadline := time.Now().Add(6 * time.Second)
		for {
			p := pane()
			if ok(p) {
				return p
			}
			if time.Now().After(deadline) {
				t.Fatalf("waiting for %s: %+v", what, p)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	c.Input(id, []byte("claude\n"))
	p := wait("working", func(p PaneInfo) bool { return p.State == "working" })
	if p.Agent != "claude" || p.AgentName != "Claude Code" || p.Icon != "agent-claude" || p.Since == 0 {
		t.Errorf("agent %+v", p)
	}
	c.Input(id, []byte("\n"))
	p = wait("blocked", func(p PaneInfo) bool { return p.State == "blocked" })
	if p.Ask != "Do you want to proceed?" {
		t.Errorf("asks %q", p.Ask)
	}
	c.Input(id, []byte("1\n"))
	wait("the shell again", func(p PaneInfo) bool { return p.Agent == "" })
}

// fakeDesktop takes the host's notifications and raises.
type fakeDesktop struct {
	mu     sync.Mutex
	notes  [][2]string
	shows  []func()
	raised chan struct{}
}

func (f *fakeDesktop) Notify(summary, body string, show func()) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notes = append(f.notes, [2]string{summary, body})
	f.shows = append(f.shows, show)
	return nil
}

func (f *fakeDesktop) Raise() { f.raised <- struct{}{} }

// A blocked agent in a pane nobody looks at posts a notification; its
// Show brings the pane up.
func TestNotify(t *testing.T) {
	if err := vt.Load(); err != nil {
		t.Skip(err)
	}
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\nprintf 'Allow command?\\n  $ rm -rf x\\n'\nread x\n"), 0o755)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("SHELL", "/bin/sh")
	h, path := startHost(t)
	h.ExitWhenEmpty = false
	h.Agents, _ = agent.Load("")
	h.Notify = true
	desk := &fakeDesktop{raised: make(chan struct{}, 1)}
	h.Desktop = desk
	c := dial(t, path, newWatcher())
	ws, _ := c.Open(t.TempDir(), "")
	st, _ := c.State()
	first := st.Model.Workspaces[0].Sessions[0]
	id := first.Focus
	other, _ := c.NewSession(ws, "") // the window shows another session
	_ = other
	c.Input(id, []byte("codex\n"))
	deadline := time.Now().Add(6 * time.Second)
	for {
		desk.mu.Lock()
		n := len(desk.notes)
		desk.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no notification")
		}
		time.Sleep(50 * time.Millisecond)
	}
	desk.mu.Lock()
	got, show := desk.notes[0], desk.shows[0]
	desk.mu.Unlock()
	if got != [2]string{"Codex needs you", "Allow command?"} {
		t.Errorf("notification %q", got)
	}
	// Show: the pane's session comes up, and a window.
	show()
	select {
	case <-desk.raised:
	case <-time.After(5 * time.Second):
		t.Fatal("no window raised")
	}
	st, _ = c.State()
	if st.Model.Shown.Session != first.ID {
		t.Errorf("shown %+v, want session %d", st.Model.Shown, first.ID)
	}
}

// TestHelperReport is an agent's hook in a pane, for TestReport: it
// reports INUA_HELPER_REPORT ("agent|state|title|summary") for the pane
// it runs in, and prints "reported:" and the error.
func TestHelperReport(t *testing.T) {
	spec := os.Getenv("INUA_HELPER_REPORT")
	if spec == "" {
		t.Skip("run by TestReport in a pane")
	}
	f := append(strings.SplitN(spec, "|", 4), "", "", "")
	pane, _ := strconv.Atoi(os.Getenv("INUA_TERM_PANE"))
	c, err := Dial(os.Getenv("INUA_TERM_SOCK"), nil, nil)
	if err == nil {
		err = c.Report(pane, f[0], f[1], f[2], f[3])
		c.Close()
	}
	fmt.Printf("reported:%v;\n", err)
}

// An agent reporting itself: its state and title are what it says, and
// its screen is not read for them. Only what runs in the pane reports for
// it.
func TestReport(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	h, path := startHost(t)
	h.ExitWhenEmpty = false
	h.Agents, _ = agent.Load("")
	w := newWatcher()
	c := dial(t, path, w)
	c.Open(t.TempDir(), "")
	st, _ := c.State()
	id := st.Model.Workspaces[0].Sessions[0].Focus
	c.Attach(id)
	// The agent, as far as the process check sees a program not known,
	// runs its hook for each line typed, until "end".
	exe, _ := os.Executable()
	c.Input(id, []byte(`sh -c 'while IFS= read -r l && [ "$l" != end ]; do INUA_HELPER_REPORT="$l" `+exe+` -test.run=^TestHelperReport$; done'`+"\n"))
	time.Sleep(1200 * time.Millisecond)
	reports := 0
	report := func(spec string) string {
		t.Helper()
		reports++
		c.Input(id, []byte(spec+"\n"))
		w.waitFor(t, "a report", func() bool { return strings.Count(w.text(id), "reported:") >= reports })
		out := w.text(id)
		for range reports - 1 {
			_, out, _ = strings.Cut(out, "reported:")
		}
		_, out, _ = strings.Cut(out, "reported:")
		res, _, _ := strings.Cut(out, ";")
		return res
	}
	if res := report("|working||"); res == "<nil>" {
		t.Error("a report without an agent")
	}
	if err := c.Report(id, "claude", "working", "", ""); err == nil {
		t.Error("a report from outside the pane")
	}
	if res := report("claude|working|Fix the flaky test|"); res != "<nil>" {
		t.Fatal(res)
	}
	pane := func() PaneInfo {
		st, _ := c.State()
		for _, p := range st.Panes {
			if p.ID == id {
				return p
			}
		}
		return PaneInfo{}
	}
	if p := pane(); p.Agent != "claude" || p.State != "working" || p.Title != "Fix the flaky test" || p.AgentName != "Claude Code" {
		t.Errorf("working %+v", p)
	}
	report("claude|blocked||Claude needs your permission to use Bash")
	if p := pane(); p.State != "blocked" || p.Ask != "Claude needs your permission to use Bash" {
		t.Errorf("blocked %+v", p)
	}
	report("claude|done||")
	if p := pane(); p.State != "done" {
		t.Errorf("done %+v", p)
	}
	if res := report("claude|sleeping||"); res == "<nil>" {
		t.Error("an unknown state")
	}
	// The process check keeps a reporting agent while it runs, and
	// forgets it once the shell is back.
	time.Sleep(300 * time.Millisecond)
	if p := pane(); p.Agent != "claude" {
		t.Errorf("forgotten %+v", p)
	}
	c.Input(id, []byte("end\n"))
	deadline := time.Now().Add(5 * time.Second)
	for pane().Agent != "" {
		if time.Now().After(deadline) {
			t.Fatal("not forgotten after it ended")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A pane dropped on another swaps with it; a restored agent comes back
// as its resume command.
func TestSwapDroppedAndResume(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	h, path := startHost(t)
	h.ExitWhenEmpty = false
	h.Agents, _ = agent.Load("")
	c := dial(t, path, newWatcher())
	c.Open(t.TempDir(), "")
	st, _ := c.State()
	s := st.Model.Workspaces[0].Sessions[0]
	first := s.Focus
	second, _ := c.Split(s.ID, "right")
	third, _ := c.Split(s.ID, "down")
	if err := c.Layout(s.ID, OpSwap, Message{From: first, Pane: third}); err != nil {
		t.Fatal(err)
	}
	st, _ = c.State()
	if got := st.Model.Workspaces[0].Sessions[0].Tree.Panes(); !slices.Equal(got, []int{third, second, first}) {
		t.Errorf("after the drop %v", got)
	}
	for in, want := range map[string]string{"claude": "claude --continue", "codex --full-auto": "codex resume --last", "vim notes": "vim notes", "": ""} {
		if got := h.resume(in); got != want {
			t.Errorf("resume(%q) = %q, want %q", in, got, want)
		}
	}
}

// A client attaching with a snapshot gets the pane as the engine has it,
// rewrapped at its size, where the replay is the output as it came.
func TestSnapshotAttach(t *testing.T) {
	if err := vt.Load(); err != nil {
		t.Skip(err)
	}
	_, path := startHost(t)
	w1 := newWatcher()
	c1 := dial(t, path, w1)
	line := strings.Repeat("0123456789", 6)
	id, err := c1.Create([]string{"sh", "-c", "echo " + line + "; read x"}, "", nil, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	c1.Attach(id)
	w1.waitFor(t, "the line", func() bool { return strings.Contains(w1.text(id), line) })
	if err := c1.Resize(id, 20, 24, 0, 0); err != nil {
		t.Fatal(err)
	}

	w2 := newWatcher()
	c2 := dial(t, path, w2)
	if !c2.TellsSnapshots() {
		t.Fatal("the host does not say it makes snapshots")
	}
	if err := c2.AttachSnapshot(id); err != nil {
		t.Fatal(err)
	}
	// Drawn again in a terminal of the pane's size, it is the line in
	// three rows of 20.
	term, err := vt.New(20, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	term.Write([]byte(w2.text(id)))
	got, _ := term.Text(false)
	if want := line[:20] + "\n" + line[20:40] + "\n" + line[40:]; !strings.HasPrefix(got, want) {
		t.Errorf("drawn from the snapshot: %q, want it to begin %q", got, want)
	}

	// Without asking, the replay as before.
	w3 := newWatcher()
	c3 := dial(t, path, w3)
	c3.Attach(id)
	if !strings.Contains(w3.text(id), line) {
		t.Errorf("the replay: %q", w3.text(id))
	}
}

// A pane takes the size the last client asked for; when that client
// detaches or goes, the size the last of the others asked for.
func TestSizeLastLooked(t *testing.T) {
	_, path := startHost(t)
	desk := dial(t, path, newWatcher())
	id, err := desk.Create([]string{"sh", "-c", "read x"}, "", nil, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	size := func() (int, int) {
		panes, _ := desk.List()
		return panes[0].Cols, panes[0].Rows
	}
	want := func(what string, cols, rows int) {
		t.Helper()
		if c, r := size(); c != cols || r != rows {
			t.Errorf("%s: %dx%d, want %dx%d", what, c, r, cols, rows)
		}
	}
	desk.Attach(id)
	desk.Resize(id, 100, 30, 0, 0)

	phone := dial(t, path, newWatcher())
	phone.Resize(id, 50, 40, 0, 0)
	phone.Attach(id)
	want("the phone looking", 50, 40)
	phone.Detach(id)
	want("the phone gone to another pane", 100, 30)

	phone.Resize(id, 50, 40, 0, 0)
	phone.Attach(id)
	// The desktop asks again (its window resized): it is the last to.
	desk.Resize(id, 120, 30, 0, 0)
	want("the desktop asking again", 120, 30)
	phone.Detach(id)
	want("the phone letting go of a size no longer the pane's", 120, 30)

	phone.Resize(id, 50, 40, 0, 0)
	want("the phone looking again", 50, 40)
	phone.Close()
	deadline := time.Now().Add(5 * time.Second)
	for c, r := size(); (c != 120 || r != 30) && time.Now().Before(deadline); c, r = size() {
		time.Sleep(10 * time.Millisecond)
	}
	want("the phone's connection gone", 120, 30)
}
