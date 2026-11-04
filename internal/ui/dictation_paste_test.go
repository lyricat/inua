package ui

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/compositor/compositortest"
)

// A dictation is pasted, not typed: the words go on the clipboard, the
// window gets the paste shortcut by its id through the compositor, and the
// clipboard's own text is put back. wtype is not used at all: its synthetic
// keys arrive as other characters when an input method's virtual keyboard
// holds the keymap.
func TestDictationPastes(t *testing.T) {
	// A wtype that fails loudly if it is ever run.
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "wtype"), []byte("#!/bin/sh\ntouch "+filepath.Join(bin, "ran")+"\nexit 1\n"), 0o755)
	t.Setenv("PATH", bin+":/usr/bin:/bin")

	for _, c := range []struct{ class, want string }{
		{"org.telegram.desktop", "send-key 0x1 CTRL V"},
		{"kitty", "send-key 0x1 CTRL SHIFT V"},
	} {
		f := compositortest.New()
		var copied []string
		clip := []byte("before")
		typer := dictationTyper{
			target: func() (pasteTarget, error) {
				return pasteTarget{keys: f, window: compositor.Window{ID: "0x1", AppID: c.class}}, nil
			},
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
		if got := f.Actions(); !slices.Equal(got, []string{c.want}) {
			t.Errorf("%s: actions %q, want %q", c.class, got, c.want)
		}
	}
	if _, err := os.Stat(filepath.Join(bin, "ran")); err == nil {
		t.Error("dictation ran wtype")
	}

	// Without a way to send keys, or a focused window, nothing is pasted.
	typer := dictationTyper{target: func() (pasteTarget, error) { return pasteTarget{}, nil }, heard: func(string) {}}
	if err := typer.typeText("hello"); err == nil {
		t.Error("pasted without a compositor")
	}
}
