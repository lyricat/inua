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
	"codeberg.org/puregotk/puregotk/v4/gtk"
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

// The New folder card opens with the entry focused and the whole name
// suggested selected (a numbered one too), from the key and from the
// menu. With several items selected the menu offers a folder for them:
// its card lists them and says they move; Esc, a name that is not one and
// a name taken change nothing; Make makes the folder and moves files and
// folders into it. A folder that cannot be made moves nothing, and an
// item that cannot move is named and the rest moved. Checked on a real
// GTK (a Broadway display).
func TestNewFolderCard(t *testing.T) {
	if _, err := exec.LookPath("gtk4-broadwayd"); err != nil {
		t.Skip("gtk4-broadwayd not installed")
	}
	// A process of its own, as TestRenameCardSelectsStem.
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
	dir := filepath.Join(home, "things")
	for _, n := range []string{"a.txt", "b.txt", "c.txt", "x.txt", "y.txt", "p.txt", "q.txt", "d/in.txt", "e/sub/deep.txt", "taken/old.txt"} {
		p := filepath.Join(dir, n)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const gather = "New folder with the selection…"

	v := &windowTestView{a: App{kinds: kinds{}, thumbs: newThumbs(), counts: newCounts(),
		users: map[uint32]string{}, groups: map[uint32]string{}, folders: map[string]string{}}}
	v.check = func(_ *appui.Ctx, w *window) {
		defer func() {
			os.Chmod(dir, 0o755)
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
		ctx := glib.MainContextDefault()
		spin := func() {
			for end := time.Now().Add(100 * time.Millisecond); time.Now().Before(end); {
				for ctx.Iteration(false) {
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
		// load shows the folder as it is now, with names selected.
		load := func(names ...string) {
			t.Helper()
			spin()
			es, err := files.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			files.Sort(es, p.fv.Order)
			p.loaded(es, nil, nil, false)
			spin()
			p.selectNames(names, true)
			got := p.selectedNames()
			slices.Sort(got)
			if !slices.Equal(got, names) {
				t.Fatalf("selected %q, want %q", got, names)
			}
		}
		// done waits for the operations to end.
		done := func() {
			t.Helper()
			for end := time.Now().Add(5 * time.Second); w.jobs.running && time.Now().Before(end); {
				ctx.Iteration(false)
				time.Sleep(time.Millisecond)
			}
			if w.jobs.running {
				t.Fatal("the operation did not end")
			}
			spin()
		}
		// menu presses the menu's item, as a click: the popover has the
		// focus and closes as the card opens.
		menu := func(label string) {
			t.Helper()
			p.showMenu(10, 10)
			spin()
			at := slices.IndexFunc(w.menuItems(p), func(it menuItem) bool { return it.label == label })
			if at < 0 {
				t.Fatalf("the menu has no %s", label)
			}
			btn := p.menu.box.GetFirstChild()
			for range at {
				next := btn.GetNextSibling()
				btn.Unref()
				btn = next
			}
			btn.GrabFocus()
			btn.Activate()
			btn.Unref()
			for end := time.Now().Add(2 * time.Second); w.card == nil && time.Now().Before(end); {
				ctx.Iteration(false)
				time.Sleep(time.Millisecond)
			}
		}
		has := func(label string) bool {
			return slices.ContainsFunc(w.menuItems(p), func(it menuItem) bool { return it.label == label })
		}
		// selectedAll checks the card: its title, the name suggested in
		// the entry, the focus there and the whole name selected.
		selectedAll := func(how, title, name string) {
			t.Helper()
			if w.card == nil || w.cardTitle.GetText() != title {
				t.Errorf("%s: no %s card", how, title)
				return
			}
			if got := w.cardEntry.GetText(); got != name {
				t.Errorf("%s: the entry reads %q, want %q", how, got, name)
			}
			if !w.focusIn(&w.cardEntry.Widget) {
				t.Errorf("%s: the entry does not have the focus", how)
			}
			var start, end int32
			w.cardEntry.GetSelectionBounds(&start, &end)
			if n := int32(utf8.RuneCountInString(name)); start != 0 || end != n {
				t.Errorf("%s: selected %d–%d of %q, want 0–%d", how, start, end, name, n)
			}
		}
		listed := func() []string {
			var out []string
			for ch := w.cardList.GetFirstChild(); ch != nil; {
				out = append(out, gtk.LabelNewFromInternalPtr(ch.GoPointer()).GetText())
				next := ch.GetNextSibling()
				ch.Unref()
				ch = next
			}
			return out
		}
		read := func(rel string) string {
			b, _ := os.ReadFile(filepath.Join(dir, rel))
			return string(b)
		}
		// unchanged: every item named is where it was, and no folder new.
		before, _ := os.ReadDir(dir)
		unchanged := func(when string) {
			t.Helper()
			now, _ := os.ReadDir(dir)
			name := func(e os.DirEntry) string { return e.Name() }
			if a, b := mapNames(before, name), mapNames(now, name); !slices.Equal(a, b) {
				t.Errorf("%s: the folder holds %q, held %q", when, b, a)
			}
		}

		// --- an ordinary new folder ---
		load()
		w.do("new-folder")
		selectedAll("key", "New folder", "New Folder")
		spin()
		selectedAll("key, then frames", "New folder", "New Folder")
		if w.cardScroll.GetVisible() || w.cardText.GetVisible() {
			t.Error("an ordinary new folder's card lists or says something")
		}
		w.cardKey(uint32(gdk.KEY_Escape), 0)
		spin()
		if w.card != nil {
			t.Error("Esc left the card open")
		}
		unchanged("Esc on a new folder")
		menu("New folder…")
		selectedAll("menu", "New folder", "New Folder")
		spin()
		selectedAll("menu, then frames", "New folder", "New Folder")
		w.cardDefault() // the name suggested, as it is
		done()
		if fi, err := os.Stat(filepath.Join(dir, "New Folder")); err != nil || !fi.IsDir() {
			t.Errorf("the new folder was not made: %v", err)
		}
		// The name taken now: the numbered one, selected whole.
		load()
		w.do("new-folder")
		selectedAll("numbered", "New folder", "New Folder (2)")
		w.cardEntry.SetText("typed")
		w.cardDefault()
		done()
		if fi, err := os.Stat(filepath.Join(dir, "typed")); err != nil || !fi.IsDir() {
			t.Errorf("the folder typed was not made: %v", err)
		}
		if _, err := os.Lstat(filepath.Join(dir, "New Folder (2)")); err == nil {
			t.Error("the name suggested was made beside the one typed")
		}
		// A new file's card is as it was.
		w.do("new-file")
		selectedAll("new file", "New file", "Untitled")
		w.closeCard()
		before, _ = os.ReadDir(dir)

		// --- a folder for the selection ---
		load()
		if has(gather) {
			t.Error("offered with nothing selected")
		}
		load("a.txt")
		if has(gather) {
			t.Error("offered for one item")
		}
		w.do("gather")
		if w.card != nil {
			t.Error("a card for one item")
			w.closeCard()
		}
		load("a.txt", "b.txt", "d")
		menu(gather)
		selectedAll("selection", "New folder", "New Folder (2)")
		spin()
		selectedAll("selection, then frames", "New folder", "New Folder (2)")
		if text := w.cardText.GetText(); !w.cardText.GetVisible() || !strings.Contains(text, "3 selected items are moved into the new folder") {
			t.Errorf("the card says %q", text)
		}
		// In the pane's order, folders marked.
		if got := listed(); !w.cardScroll.GetVisible() || !slices.Equal(got, []string{"d/", "a.txt", "b.txt"}) {
			t.Errorf("the card lists %q", got)
		}
		// The arrows are the entry's: the list is not one to pick from.
		w.cardKey(uint32(gdk.KEY_Down), 0)
		// Esc: nothing.
		w.cardEntry.SetText("never")
		w.cardKey(uint32(gdk.KEY_Escape), 0)
		done()
		if w.card != nil {
			t.Error("Esc left the card open")
		}
		unchanged("Esc")
		// Cancel: nothing.
		w.do("gather")
		w.cardEntry.SetText("never")
		w.card.btns[0].Activate() // as a click, a moment later
		for end := time.Now().Add(2 * time.Second); w.card != nil && time.Now().Before(end); {
			ctx.Iteration(false)
			time.Sleep(time.Millisecond)
		}
		done()
		if w.card != nil {
			t.Error("Cancel left the card open")
		}
		unchanged("Cancel")
		// Names that are not names, and one taken: the card stays and says so.
		w.do("gather")
		for _, bad := range []string{"", "  ", "..", "a/b", "taken", "a.txt", strings.Repeat("n", 256)} {
			w.cardEntry.SetText(bad)
			w.cardDefault()
			done()
			if w.card == nil || w.cardTitle.GetText() != "New folder" {
				t.Errorf("the name %q closed the card", bad)
				w.do("gather")
				continue
			}
			if !w.cardError.GetVisible() || w.cardError.GetText() == "" {
				t.Errorf("the name %q: nothing said", bad)
			}
			unchanged("the name " + bad)
		}
		if read("taken/old.txt") != "taken/old.txt" {
			t.Error("the folder whose name was typed was changed")
		}
		// Make: files and a folder go in; what was not selected stays.
		w.cardEntry.SetText("  Group ")
		w.cardDefault()
		done()
		if w.card != nil {
			t.Errorf("after Make: a card %q %q", w.cardTitle.GetText(), w.cardText.GetText())
			w.closeCard()
		}
		if read("Group/a.txt") != "a.txt" || read("Group/b.txt") != "b.txt" || read("Group/d/in.txt") != "d/in.txt" {
			t.Error("the items are not in the new folder")
		}
		now, _ := os.ReadDir(dir)
		if len(now) != len(before)-3+1 {
			t.Errorf("the folder holds %d items, want %d", len(now), len(before)-2)
		}
		if got := p.selectedNames(); !slices.Equal(got, []string{"Group"}) {
			t.Errorf("selected after: %q, want the new folder", got)
		}
		// Folders alone.
		load("e", "taken")
		w.do("gather")
		if got := listed(); !slices.Equal(got, []string{"e/", "taken/"}) {
			t.Errorf("the card lists %q", got)
		}
		w.cardEntry.SetText("Folders")
		w.cardDefault()
		done()
		if read("Folders/e/sub/deep.txt") != "e/sub/deep.txt" || read("Folders/taken/old.txt") != "taken/old.txt" || w.card != nil {
			t.Error("the folders are not in the new folder")
			w.closeCard()
		}
		// Undo: back where they were, the folder gone.
		w.app.undo(w)
		done()
		if read("e/sub/deep.txt") != "e/sub/deep.txt" || read("taken/old.txt") != "taken/old.txt" {
			t.Error("undo did not move the folders back")
		}
		if _, err := os.Lstat(filepath.Join(dir, "Folders")); err == nil {
			t.Error("undo left the folder")
		}

		// --- the items are the ones asked about ---
		// One goes while the card shows, and the selection changes: the
		// other still moves, and the card says which did not.
		load("x.txt", "y.txt")
		w.do("gather")
		if err := os.Remove(filepath.Join(dir, "x.txt")); err != nil {
			t.Fatal(err)
		}
		p.selectNames([]string{"c.txt"}, false)
		w.cardEntry.SetText("Part")
		w.cardDefault()
		done()
		if w.card == nil || w.cardTitle.GetText() != "Some items could not be moved into “Part”" {
			t.Errorf("an item not moved: the card %q", w.cardTitle.GetText())
		} else if text := w.cardText.GetText(); !strings.Contains(text, "x.txt") || strings.Contains(text, "y.txt") ||
			!strings.Contains(text, "left where they were") {
			t.Errorf("an item not moved: the card says %q", text)
		}
		w.closeCard()
		if read("Part/y.txt") != "y.txt" || read("c.txt") != "c.txt" {
			t.Error("y.txt should be in the folder and c.txt where it was")
		}
		if es, _ := os.ReadDir(filepath.Join(dir, "Part")); len(es) != 1 {
			t.Errorf("the folder holds %d items, want 1", len(es))
		}

		// --- a folder that cannot be made ---
		if os.Getuid() != 0 {
			load("p.txt", "q.txt")
			before, _ = os.ReadDir(dir)
			w.do("gather")
			if err := os.Chmod(dir, 0o500); err != nil {
				t.Fatal(err)
			}
			w.cardEntry.SetText("Denied")
			w.cardDefault()
			done()
			os.Chmod(dir, 0o755)
			if w.card == nil || w.cardTitle.GetText() != "The folder “Denied” could not be made" {
				t.Errorf("a folder not made: the card %q", w.cardTitle.GetText())
			} else if text := w.cardText.GetText(); !strings.Contains(text, "Nothing was moved.") || !strings.Contains(text, "permission denied") {
				t.Errorf("a folder not made: the card says %q", text)
			}
			w.closeCard()
			unchanged("a folder not made")
			if read("p.txt") != "p.txt" || read("q.txt") != "q.txt" {
				t.Error("a folder not made: the items changed")
			}
		}

		// --- not in the trash ---
		p.showTrash(false)
		spin()
		if has(gather) {
			t.Error("offered in the trash")
		}
		w.do("gather")
		if w.card != nil {
			t.Error("a card in the trash")
		}
	}
	if code := appui.RunView("files", appui.IDBase+".FilesNewFolderTest", "Files", v, true, nil, ""); code != 0 {
		t.Fatalf("application exit code = %d", code)
	}
}

func mapNames[T any](in []T, name func(T) string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = name(v)
	}
	return out
}
