package workshop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/compositor/compositortest"
	"github.com/lyricat/inua/internal/desk"
)

// pair connects two Conns.
func pair(t *testing.T, a, b Handler) (*Conn, *Conn) {
	ar, bw := io.Pipe()
	br, aw := io.Pipe()
	t.Cleanup(func() { aw.Close(); bw.Close() })
	return NewConn(ar, aw, a), NewConn(br, bw, b)
}

func TestRPC(t *testing.T) {
	var mu sync.Mutex
	var notes []string
	var server *Conn
	srv, c := pair(t, func(method string, p json.RawMessage) (any, error) {
		switch method {
		case "add":
			var xs []int
			json.Unmarshal(p, &xs)
			return xs[0] + xs[1], nil
		case "deny":
			return nil, &Error{CodeNotAllowed, "no"}
		case "fail":
			return nil, errors.New("broken")
		case "back": // a request that calls the other side while answering
			var n int
			err := server.Call(context.Background(), "double", 21, &n)
			return n, err
		case "note":
			var s string
			json.Unmarshal(p, &s)
			mu.Lock()
			notes = append(notes, s)
			mu.Unlock()
			return nil, nil
		}
		return nil, &Error{CodeNoMethod, "no such method"}
	}, func(method string, p json.RawMessage) (any, error) {
		var n int
		json.Unmarshal(p, &n)
		return n * 2, nil
	})
	server = srv
	ctx := context.Background()
	var sum int
	if err := c.Call(ctx, "add", []int{2, 3}, &sum); err != nil || sum != 5 {
		t.Errorf("add: %d %v", sum, err)
	}
	var e *Error
	if err := c.Call(ctx, "deny", nil, nil); !errors.As(err, &e) || e.Code != CodeNotAllowed {
		t.Errorf("deny: %v", err)
	}
	if err := c.Call(ctx, "fail", nil, nil); !errors.As(err, &e) || e.Code != CodeFailed || e.Message != "broken" {
		t.Errorf("fail: %v", err)
	}
	if err := c.Call(ctx, "nope", nil, nil); !errors.As(err, &e) || e.Code != CodeNoMethod {
		t.Errorf("nope: %v", err)
	}
	var n int
	if err := c.Call(ctx, "back", nil, &n); err != nil || n != 42 {
		t.Errorf("a call while answering: %d %v", n, err)
	}
	// Notifications keep their order.
	for i := range 50 {
		c.Notify("note", strings.Repeat("x", i))
	}
	c.Call(ctx, "add", []int{0, 0}, nil) // after the notes
	mu.Lock()
	defer mu.Unlock()
	if len(notes) != 50 {
		t.Fatalf("%d notes", len(notes))
	}
	for i, s := range notes {
		if len(s) != i {
			t.Fatalf("note %d out of order: %d", i, len(s))
		}
	}
}

func TestManifest(t *testing.T) {
	dir := t.TempDir()
	write := func(s string) {
		os.WriteFile(filepath.Join(dir, ManifestFile), []byte(s), 0o644)
	}
	write(`id = "dev.example.todo"
name = "Todo"
run = ["python3", "main.py"]
uses = ["clipboard"]
icon = "icon.svg"`)
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.AppID() != "io.github.lyricat.Inua.Workshop.dev_example_todo" || !m.May("clipboard") || m.May("run") || m.Width == 0 || m.IconPath() != "" {
		t.Errorf("manifest %+v (%s)", m, m.AppID())
	}
	for cat, want := range map[string]string{"Tools": "tools", "": "other", "spaceships": "other", " games ": "games"} {
		if got := (Manifest{Category: cat}).Group(); got != want {
			t.Errorf("category %q: %q, want %q", cat, got, want)
		}
	}
	for _, bad := range []string{
		`id = "../x"` + "\nname = \"x\"\nrun = [\"x\"]",
		`id = "x"` + "\nrun = [\"x\"]",
		`id = "x"` + "\nname = \"x\"",
		`id = "x"` + "\nname = \"x\"\nrun = [\"x\"]\nuses = [\"keyring\"]",
		`id = "x"` + "\nname = \"x\"\nrun = [\"x\"]\nicon = \"/etc/passwd\"",
		`id = "x"` + "\nname = \"x\"\nrun = [\"x\"]\ncolour = 1",
	} {
		write(bad)
		if _, err := Load(dir); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestInstall(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	var trashed []string
	old := trash
	trash = func(p string) error {
		trashed = append(trashed, p)
		return os.Rename(p, fmt.Sprintf("%s.trashed%d", p, len(trashed)))
	}
	t.Cleanup(func() { trash = old })

	src := filepath.Join(t.TempDir(), "my app")
	m, err := New(src, "My App", "python")
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "dev.example.my-app" || m.Name != "My App" || m.IconPath() == "" {
		t.Errorf("new: %+v", m)
	}
	if _, err := New(src, "again", "python"); err == nil {
		t.Error("made an app in a folder that is not empty")
	}
	os.MkdirAll(filepath.Join(src, ".git"), 0o755)

	var asked bool
	got, err := Install(src, func(m Manifest, replaces bool) bool { asked = !replaces; return true })
	if err != nil || !asked {
		t.Fatalf("install: %v (asked %v)", err, asked)
	}
	if got.Dir != filepath.Join(AppsDir(), "dev.example.my-app") || fileExists(filepath.Join(got.Dir, ".git")) {
		t.Errorf("installed at %s", got.Dir)
	}
	desk, _ := os.ReadFile(DesktopPath(got))
	if !strings.Contains(string(desk), "Exec=inua-apps workshop dev.example.my-app") || !strings.Contains(string(desk), "StartupWMClass="+got.AppID()) {
		t.Errorf("desktop entry:\n%s", desk)
	}
	if apps, broken := Installed(); len(apps) != 1 || len(broken) != 0 {
		t.Errorf("installed %v, broken %v", apps, broken)
	}
	if f, err := Find("dev.example.my-app"); err != nil || f.Dir != got.Dir {
		t.Errorf("find by id: %v %v", f.Dir, err)
	}
	if f, err := Find(src); err != nil || f.Dir != src {
		t.Errorf("find by folder: %v %v", f.Dir, err)
	}

	// Refused: nothing changes. Again: the old one goes to the trash.
	if _, err := Install(src, func(Manifest, bool) bool { return false }); err == nil {
		t.Error("installed though refused")
	}
	var replaced bool
	if _, err := Install(src, func(m Manifest, r bool) bool { replaced = r; return true }); err != nil || !replaced {
		t.Errorf("reinstall: %v %v", err, replaced)
	}
	if len(trashed) != 1 || trashed[0] != got.Dir {
		t.Errorf("trashed %v", trashed)
	}
	entries, _ := os.ReadDir(AppsDir())
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".incoming-") {
			t.Errorf("left %s", e.Name())
		}
	}

	if _, err := Remove("dev.example.my-app"); err != nil {
		t.Fatal(err)
	}
	if len(trashed) != 3 || trashed[2] != DesktopPath(got) {
		t.Errorf("remove trashed %v", trashed)
	}
	if _, err := Remove("../etc"); err == nil {
		t.Error("removed outside the apps")
	}
}

// fakeHost records what a session asks of its window.
type fakeHost struct {
	mu      sync.Mutex
	renders []Node
	copied  []string
	values  map[string]any
	state   map[string]any
	title   string
	changed chan struct{}
	deny    bool     // Confirm answers no
	asked   []string // what Confirm asked
}

func newFakeHost() *fakeHost {
	return &fakeHost{state: map[string]any{}, changed: make(chan struct{}, 100)}
}

func (h *fakeHost) Render(n Node) {
	h.mu.Lock()
	h.renders = append(h.renders, n)
	h.mu.Unlock()
	h.changed <- struct{}{}
}
func (h *fakeHost) SetValues(v map[string]any) {
	h.mu.Lock()
	h.values = v
	h.mu.Unlock()
}
func (h *fakeHost) State() map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]any{}
	for k, v := range h.state {
		out[k] = v
	}
	return out
}
func (h *fakeHost) Confirm(_ context.Context, title, text, _ string, _ bool) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.asked = append(h.asked, title+" "+text)
	return !h.deny, nil
}
func (h *fakeHost) Copy(text string, secret bool) error {
	h.mu.Lock()
	h.copied = append(h.copied, text)
	h.mu.Unlock()
	return nil
}
func (h *fakeHost) SetTitle(t string) { h.mu.Lock(); h.title = t; h.mu.Unlock() }
func (h *fakeHost) OpenFile(_ context.Context, title string, multiple bool) ([]string, error) {
	if multiple {
		return []string{"/tmp/a", "/tmp/b"}, nil
	}
	return []string{"/tmp/" + title}, nil
}
func (h *fakeHost) SaveFile(_ context.Context, _, name string) (string, error) { return "", nil }

// last waits for the next render and returns it flattened to text.
func (h *fakeHost) next(t *testing.T) string {
	t.Helper()
	select {
	case <-h.changed:
	case <-time.After(10 * time.Second):
		t.Fatal("no render")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	b, _ := json.Marshal(h.renders[len(h.renders)-1])
	return string(b)
}

const testApp = `from inua import app, ui, InuaError

@app.init
def start():
    app.state["n"] = app.store.get("n", 0)

@app.view
def view(state):
    return ui.page(ui.text("n=%d name=%s" % (state.get("n", 0), state.get("name", ""))),
                   ui.entry("name"), ui.text(state.get("note", "")),
                   ui.bar(ui.button("add", "Add", style="primary")))

@app.on("add", "click")
def add(ev):
    app.state["n"] = app.state.get("n", 0) + 1
    app.store["n"] = app.state["n"]
    app.call("clipboard.copy", text="copied %d" % app.state["n"])
    out = app.call("system.run", argv=["echo", "hi"])
    try:
        app.call("terminal.open", argv=["true"])
        app.state["note"] = "terminal allowed"
    except InuaError as e:
        app.state["note"] = "%s code=%d run=%s" % (e, e.code, out["stdout"].strip())
    app.call("window.setTitle", title="Added")
    app.set("name", "")

app.run()
`

func TestSession(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.py"), []byte(testApp), 0o644)
	os.WriteFile(filepath.Join(dir, ManifestFile), []byte(`id = "dev.test.app"
name = "Test"
run = ["python3", "main.py"]
uses = ["clipboard", "run"]`), 0o644)
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := newFakeHost()
	s := NewSession(m, h)
	if err := s.Start("en", "washi"); err != nil {
		_, log := s.Why()
		t.Fatalf("%v\n%s", err, log)
	}
	if r := h.next(t); !strings.Contains(r, "n=0 name=") || !strings.Contains(r, `"type":"bar"`) {
		t.Errorf("first render %s", r)
	}
	h.mu.Lock()
	h.state["name"] = "Ada"
	h.mu.Unlock()
	s.Event("change", "name", "Ada")
	if r := h.next(t); !strings.Contains(r, "name=Ada") {
		t.Errorf("after change %s", r)
	}
	s.Event("click", "add", nil)
	r := h.next(t)
	if !strings.Contains(r, "n=1") || !strings.Contains(r, `code=-32001 run=hi`) {
		t.Errorf("after add %s", r)
	}
	h.mu.Lock()
	if len(h.copied) != 1 || h.copied[0] != "copied 1" || h.title != "Added" || h.values["name"] != "" {
		t.Errorf("host: copied %v, title %q, values %v", h.copied, h.title, h.values)
	}
	h.mu.Unlock()
	if b, err := os.ReadFile(filepath.Join(DataDir("dev.test.app"), "store.json")); err != nil || !strings.Contains(string(b), `"n": 1`) {
		t.Errorf("store %s %v", b, err)
	}
	s.Stop()
	select {
	case <-s.Exited():
	case <-time.After(5 * time.Second):
		t.Fatal("did not end")
	}

	// The store is kept for the next run.
	s = NewSession(m, h)
	if err := s.Start("en", "washi"); err != nil {
		t.Fatal(err)
	}
	if r := h.next(t); !strings.Contains(r, "n=1") {
		t.Errorf("next run %s", r)
	}
	s.Stop()
}

// A program that is not a workshop app: Start says so, and Why has
// what it wrote.
func TestSessionBroken(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	m := Manifest{ID: "dev.test.broken", Name: "Broken", Run: []string{"sh", "-c", "echo oops >&2; exit 3"}, Dir: dir}
	s := NewSession(m, newFakeHost())
	if err := s.Start("en", ""); err == nil {
		t.Fatal("started")
	}
	<-s.Exited()
	if err, log := s.Why(); err == nil || !strings.Contains(log, "oops") {
		t.Errorf("why: %v %q", err, log)
	}
}

func TestWatch(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.py"), []byte("1"), 0o644)
	os.MkdirAll(filepath.Join(dir, "__pycache__"), 0o755)
	changed := make(chan struct{}, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Watch(ctx, dir, 20*time.Millisecond, func() { changed <- struct{}{} })
	time.Sleep(60 * time.Millisecond)
	os.WriteFile(filepath.Join(dir, "__pycache__", "a.pyc"), []byte("x"), 0o644)
	select {
	case <-changed:
		t.Error("a cache file counted as a change")
	case <-time.After(100 * time.Millisecond):
	}
	os.WriteFile(filepath.Join(dir, "a.py"), []byte("22"), 0o644)
	select {
	case <-changed:
	case <-time.After(2 * time.Second):
		t.Error("no change seen")
	}
}

// The examples in workshop/examples load, and their Python compiles.
func TestExamples(t *testing.T) {
	dirs, _ := filepath.Glob("../../workshop/examples/*")
	if len(dirs) == 0 {
		t.Fatal("no examples")
	}
	_, py := exec.LookPath("python3")
	for _, dir := range dirs {
		m, err := Load(dir)
		if err != nil {
			t.Errorf("%s: %v", dir, err)
			continue
		}
		if m.IconPath() == "" {
			t.Errorf("%s: no icon", m.ID)
		}
		if _, err := exec.LookPath("node"); err == nil && m.Run[0] == "node" {
			if out, err := exec.Command("node", "--check", filepath.Join(dir, m.Run[1])).CombinedOutput(); err != nil {
				t.Errorf("%s: %v %s", m.ID, err, out)
			}
		}
		if py == nil && m.Run[0] == "python3" {
			cmd := exec.Command("python3", "-c", "import ast,sys; ast.parse(open(sys.argv[1]).read())", filepath.Join(dir, m.Run[1]))
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("%s: %v %s", m.ID, err, out)
			}
		}
	}
}

// The shell app in docs/workshop.md works as written.
func TestShellApp(t *testing.T) {
	doc, err := os.ReadFile("../../docs/workshop.md")
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(doc), "```sh\n#!/bin/sh\n")
	script, _, ok2 := strings.Cut(rest, "```")
	if !ok || !ok2 {
		t.Fatal("no shell app in docs/workshop.md")
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "app.sh"), []byte(script), 0o755)
	h := newFakeHost()
	s := NewSession(Manifest{ID: "dev.test.sh", Name: "Sh", Run: []string{"sh", "app.sh"}, Dir: dir}, h)
	if err := s.Start("en", ""); err != nil {
		t.Fatal(err)
	}
	if r := h.next(t); !strings.Contains(r, "Hello from sh") {
		t.Errorf("render %s", r)
	}
	s.Stop()
	<-s.Exited()
}

const testJSApp = `const { app, ui, InuaError } = require("inua");

app.init(async () => { app.state.n = await app.store.get("n", 0); });

app.view(state => ui.page(
  ui.text("n=" + state.n + " name=" + (state.name || "")),
  ui.entry("name"), ui.text(state.note || ""),
  ui.bar(ui.button("add", "Add", { style: "primary" }))));

app.on("add", "click", async () => {
  app.state.n += 1;
  await app.store.set("n", app.state.n);
  await app.call("clipboard.copy", { text: "copied " + app.state.n });
  const out = await app.call("system.run", { argv: ["echo", "hi"] });
  try {
    await app.call("terminal.open", { argv: ["true"] });
    app.state.note = "terminal allowed";
  } catch (e) {
    app.state.note = e.message + " code=" + e.code + " run=" + out.stdout.trim() + " isInuaError=" + (e instanceof InuaError);
  }
  await app.call("window.setTitle", { title: "Added" });
  app.set("name", "");
});

app.run();
`

// The JavaScript helper, under Node and under Bun, as the Python one.
func TestSessionJS(t *testing.T) {
	for _, rt := range []string{"node", "bun"} {
		t.Run(rt, func(t *testing.T) {
			if _, err := exec.LookPath(rt); err != nil {
				t.Skip("no " + rt)
			}
			t.Setenv("XDG_DATA_HOME", t.TempDir())
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "main.js"), []byte(testJSApp), 0o644)
			m := Manifest{ID: "dev.test.js", Name: "JS", Run: []string{rt, "main.js"}, Uses: []string{"clipboard", "run"}, Dir: dir}
			h := newFakeHost()
			s := NewSession(m, h)
			if err := s.Start("en", ""); err != nil {
				_, log := s.Why()
				t.Fatalf("%v\n%s", err, log)
			}
			if r := h.next(t); !strings.Contains(r, "n=0 name=") {
				t.Errorf("first render %s", r)
			}
			h.mu.Lock()
			h.state["name"] = "Ada"
			h.mu.Unlock()
			s.Event("change", "name", "Ada")
			if r := h.next(t); !strings.Contains(r, "name=Ada") {
				t.Errorf("after change %s", r)
			}
			s.Event("click", "add", nil)
			if r := h.next(t); !strings.Contains(r, "n=1") || !strings.Contains(r, "code=-32001 run=hi isInuaError=true") {
				t.Errorf("after add %s", r)
			}
			h.mu.Lock()
			if len(h.copied) != 1 || h.copied[0] != "copied 1" || h.title != "Added" || h.values["name"] != "" {
				t.Errorf("host: copied %v, title %q, values %v", h.copied, h.title, h.values)
			}
			h.mu.Unlock()
			s.Stop()
			<-s.Exited()
		})
	}
}

// The ES module wrapper gives the same app and ui.
func TestJSModule(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("no node")
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if err := WriteLib(); err != nil {
		t.Fatal(err)
	}
	script := `const { app, ui } = await import(process.env.INUA_WORKSHOP_LIB + "/node/inua.mjs");
console.log(JSON.stringify(ui.page({ scroll: false }, ui.section("S", { meta: "m" }, [ui.text("a"), null]))), typeof app.run);`
	cmd := exec.Command("node", "--input-type=module", "-e", script)
	cmd.Env = append(os.Environ(), "INUA_WORKSHOP_LIB="+LibDir())
	out, err := cmd.CombinedOutput()
	want := `{"type":"page","children":[{"type":"section","meta":"m","children":[{"type":"text","text":"a"}],"title":"S"}],"scroll":false} function`
	if err != nil || strings.TrimSpace(string(out)) != want {
		t.Errorf("%v\n%s\nwant %s", err, out, want)
	}
}

// A runtime the PATH lacks is found where version managers put it.
func TestFindRuntime(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	mk := func(dir, name string) {
		os.MkdirAll(filepath.Join(home, dir), 0o755)
		os.WriteFile(filepath.Join(home, dir, name), []byte("#!/bin/sh\n"), 0o755)
	}
	mk(".bun/bin", "bun")
	mk(".nvm/versions/node/v9.11.2/bin", "node")
	mk(".nvm/versions/node/v24.14.1/bin", "node")
	mk(".nvm/versions/node/v24.2.0/bin", "node")
	if d := findRuntime("bun"); d != filepath.Join(home, ".bun/bin") {
		t.Errorf("bun in %q", d)
	}
	if d := findRuntime("node"); d != filepath.Join(home, ".nvm/versions/node/v24.14.1/bin") {
		t.Errorf("node in %q", d)
	}
	if d := findRuntime("cobol"); d != "" {
		t.Errorf("cobol in %q", d)
	}
	if d := findRuntime("./main"); d != "" {
		t.Errorf("a path searched: %q", d)
	}
}

func TestNewJS(t *testing.T) {
	m, err := New(filepath.Join(t.TempDir(), "hi"), "Hi", "js")
	if err != nil || m.Run[0] != "node" || !fileExists(filepath.Join(m.Dir, "main.js")) {
		t.Errorf("new js: %+v %v", m, err)
	}
	if _, err := New(t.TempDir(), "x", "cobol"); err == nil {
		t.Error("made an app in a language with no template")
	}
}

const deskApp = `from inua import app, ui, InuaError

@app.init
def start():
    app.subscribe("system")

@app.view
def view(state):
    return ui.page(ui.text(state.get("out", "")))

def attempt(method, **params):
    try:
        return repr(app.call(method, **params))
    except InuaError as e:
        return "error %d %s" % (e.code, e)

@app.on("go", "click")
def go(ev):
    app.state["out"] = " | ".join([
        attempt("list_windows"),
        attempt("open_app", app="x"),
        attempt("close_window", window="Mail"),
        attempt("dialog.openFile", title="pick"),
        attempt("events.subscribe", topics=["clipboard"]),
    ])

@app.watch("system")
def changed(ev):
    app.state["out"] = "wifi " + str(ev.value["wifi"])

app.run()
`

// inua's desk functions as an app calls them: by name, needing their
// use; those that always ask ask in the window, and a no declines; a
// topic subscribed to sends events.
func TestSessionDesk(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.py"), []byte(deskApp), 0o644)
	m := Manifest{ID: "dev.test.desk", Name: "Desk", Run: []string{"python3", "main.py"}, Dir: dir, Uses: []string{"windows", "system"}}
	var mu sync.Mutex
	wifi := "true"
	comp := compositortest.New()
	comp.Snap = compositor.Snapshot{Windows: []compositor.Window{{ID: "0xa", Title: "Mail", AppID: "mail", Workspace: "1", Focused: true}}, FocusedWindow: "0xa"}
	closed := func() int { return len(comp.Actions()) }
	d := desk.Desktop{
		Call: func(context.Context, string, string, ...string) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			return `{"wifi":` + wifi + `}`, nil
		},
		Compositor: comp,
	}
	h := newFakeHost()
	h.deny = true
	s := NewSession(m, h)
	s.Tools, s.WatchEvery = desk.Tools(d), 20*time.Millisecond
	if err := s.Start("en", ""); err != nil {
		_, log := s.Why()
		t.Fatalf("%v\n%s", err, log)
	}
	h.next(t)
	s.Event("click", "go", nil)
	r := h.next(t)
	for _, want := range []string{"'title': 'Mail'", "error -32001 not allowed: \\\"apps\\\"", "error -32000 declined", "'/tmp/pick'", "error -32001 not allowed: \\\"clipboard-history\\\""} {
		if !strings.Contains(r, want) {
			t.Errorf("no %s in %s", want, r)
		}
	}
	h.mu.Lock()
	if len(h.asked) != 1 || h.asked[0] != "Let Desk do this? Closing Mail" || closed() != 0 {
		t.Errorf("asked %q, closed %d", h.asked, closed())
	}
	h.mu.Unlock()

	mu.Lock()
	wifi = "false"
	mu.Unlock()
	if r := h.next(t); !strings.Contains(r, "wifi False") {
		t.Errorf("after the change %s", r)
	}
	s.Stop()
	<-s.Exited()
}

// Every desk function's use is one an app can name, and so is every
// topic's.
func TestDeskUses(t *testing.T) {
	for _, tool := range desk.Tools(desk.Desktop{}) {
		if tool.Use != "" && !slices.Contains(Uses, tool.Use) {
			t.Errorf("%s: use %q is not in Uses", tool.Name, tool.Use)
		}
	}
	for name, tp := range desk.Topics {
		if !slices.Contains(Uses, tp.Use) {
			t.Errorf("topic %s: use %q is not in Uses", name, tp.Use)
		}
	}
}
