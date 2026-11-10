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

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/appui"
)

func TestCrumbTrail(t *testing.T) {
	type seg = crumbSeg
	home := "/home/me"
	for _, tc := range []struct {
		dir, home string
		want      []seg
	}{
		{"/", home, []seg{{label: "/", path: "/", current: true}}},
		{"/usr/share/a b", home, []seg{
			{label: "/", path: "/"},
			{label: "usr", path: "/usr"},
			{label: "share", path: "/usr/share"},
			{label: "a b", path: "/usr/share/a b", current: true},
		}},
		{home, home, []seg{{icon: "user-home-symbolic", path: home, current: true}}},
		{home + "/文件 夹/x", home + "/", []seg{
			{icon: "user-home-symbolic", path: home},
			{label: "文件 夹", path: home + "/文件 夹"},
			{label: "x", path: home + "/文件 夹/x", current: true},
		}},
		// A folder beside home whose name starts as home's is not in it.
		{"/home/me2/x", home, []seg{
			{label: "/", path: "/"},
			{label: "home", path: "/home"},
			{label: "me2", path: "/home/me2"},
			{label: "x", path: "/home/me2/x", current: true},
		}},
		{"/a", "/", []seg{{label: "/", path: "/"}, {label: "a", path: "/a", current: true}}},
	} {
		if got := crumbTrail(tc.dir, tc.home); !slices.Equal(got, tc.want) {
			t.Errorf("crumbTrail(%q, %q) = %+v, want %+v", tc.dir, tc.home, got, tc.want)
		}
	}
}

func TestFolderProblem(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a b", "文件")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if msg := folderProblem(dir); msg != "" {
		t.Errorf("folder: %q", msg)
	}
	if msg := folderProblem(file); !strings.Contains(msg, "is not a folder") {
		t.Errorf("file: %q", msg)
	}
	if msg := folderProblem(filepath.Join(dir, "gone")); !strings.Contains(msg, "does not exist") {
		t.Errorf("missing: %q", msg)
	}
	if os.Geteuid() != 0 { // root may go anywhere
		locked := filepath.Join(dir, "locked")
		if err := os.Mkdir(locked, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(locked, "in"), 0o700); err != nil {
			t.Fatal(err)
		}
		os.Chmod(locked, 0)
		t.Cleanup(func() { os.Chmod(locked, 0o700) })
		if msg := folderProblem(filepath.Join(locked, "in")); !strings.Contains(msg, "You may not open") {
			t.Errorf("unreadable: %q", msg)
		}
	}
}

// The menu of a path bar's button acts on that button's folder, in the
// pane it is in, and leaves the pane's folder, tab and selection alone.
func TestCrumbMenuActsOnItsFolder(t *testing.T) {
	if _, err := exec.LookPath("gtk4-broadwayd"); err != nil {
		t.Skip("gtk4-broadwayd not installed")
	}
	// GTK keeps the display it first opened for the whole process, so a
	// window test other than TestWindowStylesAfterReopen runs alone in a
	// process of its own.
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
	ancestor := filepath.Join(home, "a b")
	deep := filepath.Join(ancestor, "文件 夹")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "note.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// What the rows of a folder read need, as New makes it.
	v := &windowTestView{a: App{kinds: kinds{}, thumbs: newThumbs(), counts: newCounts(),
		users: map[uint32]string{}, groups: map[uint32]string{}, folders: map[string]string{}}}
	v.check = func(_ *appui.Ctx, w *window) {
		defer func() {
			for len(v.a.wins) > 0 {
				w := v.a.wins[0]
				if w.closeRequested() {
					t.Error("idle window refused to close")
				}
				w.win.Destroy()
			}
		}()
		p := w.cur()
		p.goTo(deep, false)
		es, err := files.ReadDir(deep)
		if err != nil {
			t.Fatal(err)
		}
		p.loaded(es, nil, []string{"note.txt"}, false)
		run := func(dir, label string) {
			t.Helper()
			for _, it := range w.crumbItems(p, dir) {
				if it.label == label {
					it.run()
					return
				}
			}
			t.Fatalf("the menu of %s has no %q", dir, label)
		}
		unmoved := func(step string) {
			t.Helper()
			if p.dir != deep || p.mode != inFolder || p.tab != 0 {
				t.Errorf("%s: pane at %q (mode %d, tab %d)", step, p.dir, p.mode, p.tab)
			}
			if got := p.selectedNames(); !slices.Equal(got, []string{"note.txt"}) {
				t.Errorf("%s: selection = %q", step, got)
			}
		}

		var crumbs int
		for c := p.crumbs.GetFirstChild(); c != nil; {
			if c.HasCssClass("files-crumb") {
				crumbs++
			}
			next := c.GetNextSibling()
			c.Unref()
			c = next
		}
		if want := len(crumbTrail(deep, home)); crumbs != want {
			t.Errorf("%d crumbs, want %d", crumbs, want)
		}

		run(ancestor, "Open in a new tab")
		if len(p.tabs) != 2 || p.tabs[1].key != ancestor {
			t.Errorf("tabs after a new tab: %d, second at %q", len(p.tabs), p.tabs[len(p.tabs)-1].key)
		}
		unmoved("new tab")

		run(ancestor, "Bookmark")
		if !files.Bookmarked(ancestor) || files.Bookmarked(deep) {
			t.Errorf("bookmarked: %s %v, %s %v", ancestor, files.Bookmarked(ancestor), deep, files.Bookmarked(deep))
		}
		run(ancestor, "Remove the bookmark")
		if files.Bookmarked(ancestor) {
			t.Error("the bookmark stayed")
		}
		unmoved("bookmark")

		run(ancestor, "New folder…")
		if w.card == nil || !strings.Contains(w.cardText.GetText(), "a b") {
			t.Errorf("the new folder's card: %v, %q", w.card != nil, w.cardText.GetText())
		}
		w.closeCard()
		unmoved("new folder")

		// Two panes: the other pane goes there; what an item says shows in
		// the pane its menu was in, even when the other has the keys.
		w.setDual(true)
		other := w.p[1-p.n]
		run(home, "Open in the other pane")
		if other.dir != home {
			t.Errorf("the other pane is at %q", other.dir)
		}
		unmoved("other pane")
		w.setActive(other)
		run(home, "Copy the folder's path")
		if w.cur() != p || !strings.Contains(p.statusLabel.GetText(), "Copied ~") {
			t.Errorf("after copying: pane %d has the keys, status %q", w.cur().n, p.statusLabel.GetText())
		}

		// A folder that went is said so, and nothing is made there.
		if err := os.RemoveAll(deep); err != nil {
			t.Fatal(err)
		}
		for _, label := range []string{"New folder…", "Paste into the folder", "Terminal here", "Tidy with AI…", "Bookmark"} {
			p.statusLabel.SetText("")
			run(deep, label)
			if w.card != nil || !strings.Contains(p.statusLabel.GetText(), "does not exist") {
				t.Errorf("%s in a folder that went: card %v, status %q", label, w.card != nil, p.statusLabel.GetText())
			}
			w.closeCard()
		}
		if files.Bookmarked(deep) {
			t.Error("a folder that went was bookmarked")
		}

		// The root offers nothing for the assistant to move; the trash is
		// not a folder.
		for _, it := range w.crumbItems(p, "/") {
			if it.label == "Tidy with AI…" {
				t.Error("the root offers tidying")
			}
		}
		var labels []string
		for _, it := range w.crumbItems(p, trashKey) {
			labels = append(labels, it.label)
		}
		if !slices.Equal(labels, []string{"Open in a new tab", "", "Empty Trash"}) {
			t.Errorf("the trash's menu: %q", labels)
		}
	}
	if code := appui.RunView("files", appui.IDBase+".FilesCrumbTest", "Files", v, true, nil, ""); code != 0 {
		t.Fatalf("application exit code = %d", code)
	}
}
