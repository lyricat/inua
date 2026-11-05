package web

import (
	"encoding/json"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/sockdir"
	"github.com/lyricat/inua/internal/term/host"
	"github.com/lyricat/inua/internal/term/scheme"
)

// isolate keeps the tests from the user's settings.
func isolate(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(dir, "run"))
}

func page(t *testing.T) string {
	t.Helper()
	b, err := static.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The page's text is the list the catalogs get: nothing shown that is not
// translated, nothing listed that is not shown.
func TestTextListed(t *testing.T) {
	p := page(t)
	var shown []string
	for _, re := range []string{`\bTf?\("((?:[^"\\]|\\.)*)"`, `data-ta?="([^"]+)"`} {
		for _, m := range regexp.MustCompile(re).FindAllStringSubmatch(p, -1) {
			shown = append(shown, m[1])
		}
	}
	sort.Strings(shown)
	shown = slices.Compact(shown)
	listed := slices.Clone(pageText)
	sort.Strings(listed)
	if !slices.Equal(shown, listed) {
		t.Errorf("the page shows %q\nthe list has %q", shown, listed)
	}
	var plurals [][2]string
	for _, m := range regexp.MustCompile(`\bNf\("((?:[^"\\]|\\.)*)", "((?:[^"\\]|\\.)*)"`).FindAllStringSubmatch(p, -1) {
		if !slices.Contains(plurals, [2]string{m[1], m[2]}) {
			plurals = append(plurals, [2]string{m[1], m[2]})
		}
	}
	for _, pl := range plurals {
		if !slices.Contains(pagePlurals, pl) {
			t.Errorf("%q is not in pagePlurals", pl)
		}
	}
	if len(plurals) != len(pagePlurals) {
		t.Errorf("the page has %d forms by number, the list %d", len(plurals), len(pagePlurals))
	}
}

// The page asks nothing of the internet: its scripts and fonts are here.
func TestNothingFromTheInternet(t *testing.T) {
	if m := regexp.MustCompile(`(?:src|href)=["']?https?://|url\(["']?https?://|https://cdn|fonts\.googleapis`).FindString(page(t)); m != "" {
		t.Errorf("the page loads %q from the internet", m)
	}
	for _, f := range []string{"xterm.js", "xterm.css", "addon-webgl.js", "addon-unicode-graphemes.js", "geist-mono-latin-400-normal.woff2"} {
		if _, err := static.ReadFile("static/vendor/" + f); err != nil {
			t.Errorf("vendor/%s: %v (scripts/term-web.sh)", f, err)
		}
	}
}

// fakeHost is a host's socket that welcomes, then echoes the frames it
// gets back as control messages.
func fakeHost(t *testing.T) string {
	t.Helper()
	path := filepath.Join(sockdir.New(t), "term.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				kind, p, err := host.ReadFrame(conn)
				var hello host.Message
				if err != nil || kind != host.FrameControl || json.Unmarshal(p, &hello) != nil || hello.Type != host.MsgHello {
					return
				}
				welcome, _ := json.Marshal(host.Message{Type: host.MsgWelcome, Version: host.Version})
				host.WriteFrame(conn, host.FrameControl, welcome)
				for {
					kind, p, err := host.ReadFrame(conn)
					if err != nil {
						return
					}
					host.WriteFrame(conn, host.FrameControl, append([]byte{kind}, p...))
				}
			}()
		}
	}()
	return path
}

func server(t *testing.T) *httptest.Server {
	t.Helper()
	isolate(t)
	s := httptest.NewServer((&Server{Socket: fakeHost(t), Token: "secret"}).Handler())
	t.Cleanup(s.Close)
	return s
}

func get(t *testing.T, url string) *http.Response {
	t.Helper()
	r, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Body.Close() })
	return r
}

// Without the token, nothing but the page's fixed files.
func TestToken(t *testing.T) {
	s := server(t)
	for _, path := range []string{"/", "/?t=wrong", "/look.json", "/manifest.webmanifest", "/ws"} {
		if r := get(t, s.URL+path); r.StatusCode != http.StatusForbidden {
			t.Errorf("%s: %d, want 403", path, r.StatusCode)
		}
	}
	for _, path := range []string{"/?t=secret", "/look.json?t=secret", "/manifest.webmanifest?t=secret", "/vendor/xterm.js", "/icons/agent-shell-symbolic.svg"} {
		if r := get(t, s.URL+path); r.StatusCode != http.StatusOK {
			t.Errorf("%s: %d, want 200", path, r.StatusCode)
		}
	}
	if r := get(t, s.URL+"/icons/..%2fui.go"); r.StatusCode != http.StatusNotFound {
		t.Errorf("an icon outside the icons: %d", r.StatusCode)
	}
}

// Without the token, or with a wrong one, the page is a card that asks
// for it, saying so when it was wrong, and nothing of the terminals.
func TestAskToken(t *testing.T) {
	s := server(t)
	for path, wrong := range map[string]bool{"/": false, "/?t=wrong": true} {
		r := get(t, s.URL+path)
		b, _ := io.ReadAll(r.Body)
		body := string(b)
		if r.StatusCode != http.StatusForbidden || !strings.Contains(body, `<form id="f"`) || strings.Contains(body, "window.INUA") {
			t.Errorf("%s: %d, not the token card:\n%s", path, r.StatusCode, body)
		}
		if got := strings.Contains(body, "That token is not right"); got != wrong {
			t.Errorf("%s: says the token is wrong: %v, want %v", path, got, wrong)
		}
	}
}

// The page comes with its text and the home folder put in.
func TestPageData(t *testing.T) {
	s := server(t)
	r := get(t, s.URL+"/?t=secret")
	var b strings.Builder
	buf := make([]byte, 1<<16)
	for {
		n, err := r.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	if !strings.Contains(b.String(), `window.INUA = {`) || !strings.Contains(b.String(), `"NEEDS YOU":"NEEDS YOU"`) {
		t.Errorf("the page's data is not in it")
	}
	// Mister Morph's own icons are named, and served, for its rows.
	if !strings.Contains(b.String(), `"own":["agent-mistermorph"]`) {
		t.Errorf("the agents' own icons are not in the page's data")
	}
	for _, v := range []string{"light", "dark"} {
		if r := get(t, s.URL+"/icons/agent-mistermorph-"+v+".svg"); r.StatusCode != http.StatusOK {
			t.Errorf("icon %s: %s", v, r.Status)
		}
	}
}

func dialWS(t *testing.T, s *httptest.Server, origin string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	h := http.Header{}
	if origin != "" {
		h.Set("Origin", origin)
	}
	return websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(s.URL, "http")+"/ws?t=secret", h)
}

// The bridge says hello for the page, then relays frames both ways; the
// page sends control messages and input only.
func TestBridge(t *testing.T) {
	s := server(t)
	ws, _, err := dialWS(t, s, s.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	_, msg, err := ws.ReadMessage()
	var m host.Message
	if err != nil || msg[0] != host.FrameControl || json.Unmarshal(msg[1:], &m) != nil || m.Type != host.MsgWelcome {
		t.Fatalf("no welcome: %q %v", msg, err)
	}
	ws.WriteMessage(websocket.BinaryMessage, []byte{host.FrameInput, 1, 0, 0, 0, 'x'})
	_, msg, err = ws.ReadMessage()
	if err != nil || string(msg) != string([]byte{host.FrameControl, host.FrameInput, 1, 0, 0, 0, 'x'}) {
		t.Errorf("the input relayed: %q %v", msg, err)
	}
	// Output is the host's to send, not the page's.
	ws.WriteMessage(websocket.BinaryMessage, []byte{host.FrameOutput, 1, 0, 0, 0})
	if _, _, err := ws.ReadMessage(); err == nil {
		t.Error("the bridge took an output frame from the page")
	}
}

// A page on another site does not get a WebSocket, token or not.
func TestOrigin(t *testing.T) {
	s := server(t)
	if ws, r, err := dialWS(t, s, "http://elsewhere.example"); err == nil {
		ws.Close()
		t.Error("a WebSocket from another origin")
	} else if r == nil || r.StatusCode != http.StatusForbidden {
		t.Errorf("another origin: %v", err)
	}
}

// The look has every theme inua has and every scheme inua-term has, the
// window's theme (inua's default without settings) among them.
func TestLook(t *testing.T) {
	isolate(t)
	l := readLook()
	if l.Theme != config.Default().Style.Theme {
		t.Errorf("theme %q", l.Theme)
	}
	for _, name := range config.Themes {
		i := slices.IndexFunc(l.Themes, func(th theme) bool { return th.Name == name })
		if i < 0 {
			t.Errorf("no theme %s", name)
			continue
		}
		if tok := l.Themes[i].Tokens; tok["app"] == "" || tok["accent"] == "" {
			t.Errorf("theme %s without its tokens: %v", name, tok)
		}
	}
	if len(l.Schemes) != len(scheme.All) {
		t.Fatalf("%d schemes, want %d", len(l.Schemes), len(scheme.All))
	}
	for _, s := range l.Schemes {
		if len(s.Palette) != 16 || !strings.HasPrefix(s.BG, "#") {
			t.Errorf("scheme %s: %+v", s.Name, s)
		}
	}
}

// The web app's icon is a PNG of the size asked.
func TestAppIcon(t *testing.T) {
	s := server(t)
	r := get(t, s.URL+"/icon-192.png")
	img, err := png.Decode(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 192 || b.Dy() != 192 {
		t.Errorf("%v", b)
	}
}
