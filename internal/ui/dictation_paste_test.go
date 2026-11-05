package ui

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lyricat/inua/internal/hypr"
	"github.com/lyricat/inua/internal/sockdir"
)

// fakeHypr serves Hyprland's request socket: the focused window, and the
// dispatchers it is sent, recorded.
type fakeHypr struct {
	dir        string
	dispatched []string
	active     string
}

func newFakeHypr(t *testing.T, active string) *fakeHypr {
	t.Helper()
	f := &fakeHypr{dir: sockdir.New(t), active: active}
	ln, err := net.Listen("unix", filepath.Join(f.dir, ".socket.sock"))
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
				buf := make([]byte, 4096)
				n, _ := conn.Read(buf)
				cmd := string(buf[:n])
				switch {
				case cmd == "j/activewindow":
					io.WriteString(conn, f.active)
				case strings.HasPrefix(cmd, "dispatch "):
					f.dispatched = append(f.dispatched, cmd[len("dispatch "):])
					io.WriteString(conn, "ok")
				default:
					io.WriteString(conn, "unknown request")
				}
			}()
		}
	}()
	return f
}

// A dictation is pasted, not typed: the words go on the clipboard, the
// window gets the paste shortcut by its address, and the clipboard's own
// text is put back. wtype is not used at all: its synthetic keys arrive as
// other characters when an input method's virtual keyboard holds the keymap.
func TestDictationPastes(t *testing.T) {
	// A wtype that fails loudly if it is ever run.
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "wtype"), []byte("#!/bin/sh\ntouch "+filepath.Join(bin, "ran")+"\nexit 1\n"), 0o755)
	t.Setenv("PATH", bin+":/usr/bin:/bin")

	for _, c := range []struct{ class, want string }{
		{"org.telegram.desktop", `hl.dsp.send_shortcut({ mods = "CTRL", key = "V", window = "address:0x1" })`},
		{"kitty", `hl.dsp.send_shortcut({ mods = "CTRL SHIFT", key = "V", window = "address:0x1" })`},
	} {
		f := newFakeHypr(t, `{"address":"0x1","class":"`+c.class+`"}`)
		client := hypr.NewClient(hypr.Paths{Request: filepath.Join(f.dir, ".socket.sock")})
		var copied []string
		clip := []byte("before")
		typer := dictationTyper{
			hypr:      func() (hyprNow, error) { return hyprNow{client: client, lua: true}, nil },
			heard:     func(string) {},
			clipboard: func() []byte { return clip },
			copy:      func(b []byte) error { copied = append(copied, string(b)); clip = b; return nil },
		}
		if err := typer.typeText("hello"); err != nil {
			t.Fatal(err)
		}
		if len(copied) != 2 || copied[0] != "hello " || copied[1] != "before" {
			t.Errorf("%s: clipboard copies %q", c.class, copied)
		}
		if len(f.dispatched) != 1 || f.dispatched[0] != c.want {
			t.Errorf("%s: dispatched %q, want %q", c.class, f.dispatched, c.want)
		}
	}
	if _, err := os.Stat(filepath.Join(bin, "ran")); err == nil {
		t.Error("dictation ran wtype")
	}
}
