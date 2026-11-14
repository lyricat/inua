//go:build linux

package filesui

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/appui"
)

// The Rename card opens with the entry focused and the name but its last
// extension selected; a folder's name whole. Opened from F2 and from the
// menu, whose closing popover must not take the focus or the selection
// back. Checked on a real GTK (a Broadway display).
func TestRenameCardSelectsStem(t *testing.T) {
	if _, err := exec.LookPath("gtk4-broadwayd"); err != nil {
		t.Skip("gtk4-broadwayd not installed")
	}
	// GTK keeps the display it first opened for the whole process: this
	// runs in a process of its own, as TestCrumbMenuActsOnItsFolder does.
	if os.Getenv("INUA_FILESUI_TEST") != t.Name() {
		cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$", "-test.v")
		cmd.Env = append(os.Environ(), "INUA_FILESUI_TEST="+t.Name())
		out, err := cmd.CombinedOutput()
		switch {
		case err != nil:
			t.Fatalf("%v\n%s", err, out)
		case strings.Contains(string(out), "--- SKIP"):
			t.Skipf("%s", out)
		}
		return
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	home := startBroadway(t)
	dir := filepath.Join(home, "names")
	cases := []struct {
		name, want   string // want: what is selected
		folder, menu bool   // menu: opened from the menu too
	}{
		{"abc.jpg", "abc", false, true},
		{"archive.tar.gz", "archive.tar", false, false},
		{"noext", "noext", false, false},
		{"notes.", "notes.", false, false},
		{"照片 cafe\u0301 👍🏽.jpeg", "照片 cafe\u0301 👍🏽", false, true}, // an accent, a skin tone: characters of their own
		{"my.folder", "my.folder", true, true},
		{"linked.d", "linked.d", true, false}, // a link to a folder
	}
	for _, c := range cases {
		p := filepath.Join(dir, c.name)
		var err error
		switch {
		case c.name == "linked.d":
			err = os.Symlink("my.folder", p)
		case c.folder:
			err = os.MkdirAll(p, 0o755)
		default:
			if err = os.MkdirAll(dir, 0o755); err == nil {
				err = os.WriteFile(p, nil, 0o644)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
	}

	v := &windowTestView{a: App{kinds: kinds{}, thumbs: newThumbs(), counts: newCounts(),
		users: map[uint32]string{}, groups: map[uint32]string{}, folders: map[string]string{}}}
	v.check = func(_ *appui.Ctx, w *window) {
		defer func() {
			for len(v.a.wins) > 0 {
				w := v.a.wins[0]
				w.closeCard()
				if w.closeRequested() {
					t.Error("idle window refused to close")
				}
				w.win.Destroy()
			}
		}()
		w.win.Present()
		p := w.cur()
		p.goTo(dir, false)
		es, err := files.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		p.loaded(es, nil, nil, false)
		// spin lets GTK draw and settle for a while, as after a key.
		spin := func() {
			ctx := glib.MainContextDefault()
			for end := time.Now().Add(100 * time.Millisecond); time.Now().Before(end); {
				for ctx.Iteration(false) {
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
		spin()
		check := func(how, name, want string) {
			t.Helper()
			if w.card == nil || w.cardTitle.GetText() != "Rename" {
				t.Errorf("%s %q: no Rename card", how, name)
				return
			}
			if got := w.cardEntry.GetText(); got != name {
				t.Errorf("%s %q: the entry reads %q", how, name, got)
			}
			if !w.focusIn(&w.cardEntry.Widget) {
				t.Errorf("%s %q: the entry does not have the focus", how, name)
			}
			var start, end int32
			w.cardEntry.GetSelectionBounds(&start, &end)
			if n := int32(utf8.RuneCountInString(want)); start != 0 || end != n {
				r := []rune(name)
				sel := "?"
				if start >= 0 && start <= end && int(end) <= len(r) {
					sel = string(r[start:end])
				}
				t.Errorf("%s %q: selected %d–%d %q, want 0–%d %q", how, name, start, end, sel, n, want)
			}
		}
		for _, c := range cases {
			p.selectNames([]string{c.name}, true)
			if got := p.selectedNames(); !slices.Equal(got, []string{c.name}) {
				t.Errorf("selected %q, want %q", got, c.name)
				return
			}

			// F2.
			w.do("rename")
			check("F2", c.name, c.want)
			spin()
			check("F2, then frames", c.name, c.want)
			w.closeCard()
			spin()
			if !c.menu {
				continue
			}

			// The menu: its popover has the focus, and closes as the card
			// opens.
			p.showMenu(10, 10)
			spin()
			at := slices.IndexFunc(w.menuItems(p), func(it menuItem) bool { return it.label == "Rename…" })
			if at < 0 {
				t.Errorf("%q: the menu has no Rename…", c.name)
				return
			}
			btn := p.menu.box.GetFirstChild() // a child for each item, separators too
			for range at {
				next := btn.GetNextSibling()
				btn.Unref()
				btn = next
			}
			btn.GrabFocus()
			btn.Activate() // as a click, a moment later: the popover closes, then the card opens
			btn.Unref()
			ctx := glib.MainContextDefault()
			for end := time.Now().Add(2 * time.Second); w.card == nil && time.Now().Before(end); {
				ctx.Iteration(false)
				time.Sleep(time.Millisecond)
			}
			check("menu", c.name, c.want)
			spin()
			check("menu, then frames", c.name, c.want)
			w.closeCard()
			spin()
		}

		// Esc leaves the name as it was; renaming renames to what was typed.
		p.selectNames([]string{"abc.jpg"}, true)
		w.do("rename")
		w.cardEntry.SetText("abe.jpg")
		w.cardKey(uint32(gdk.KEY_Escape), 0)
		if w.card != nil {
			t.Error("Esc left the Rename card open")
		}
		spin()
		if _, err := os.Stat(filepath.Join(dir, "abc.jpg")); err != nil {
			t.Errorf("Esc renamed abc.jpg: %v", err)
		}
		w.do("rename")
		w.cardEntry.SetText("abd.jpg")
		w.cardDefault()
		ctx := glib.MainContextDefault()
		for end := time.Now().Add(5 * time.Second); w.jobs.running && time.Now().Before(end); {
			ctx.Iteration(false)
			time.Sleep(time.Millisecond)
		}
		if _, err := os.Stat(filepath.Join(dir, "abd.jpg")); err != nil || w.jobs.running {
			t.Errorf("abc.jpg was not renamed to abd.jpg: %v (still running %v)", err, w.jobs.running)
		}
	}
	if code := appui.RunView("files", appui.IDBase+".FilesRenameTest", "Files", v, true, nil, ""); code != 0 {
		t.Fatalf("application exit code = %d", code)
	}
}
