package files

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/apps/files/remote"
	"github.com/lyricat/inua/internal/apps/store"
)

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func names(es []Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Name
	}
	return out
}

func TestTrail(t *testing.T) {
	for _, c := range []struct {
		dir  string
		want []string
	}{
		{"/home/lyric/path_a/path_b", []string{"/home", "/home/lyric", "/home/lyric/path_a", "/home/lyric/path_a/path_b"}},
		{"/home/lyric/path_a/", []string{"/home", "/home/lyric", "/home/lyric/path_a"}},
		{"/etc", []string{"/etc"}},
		{"/", []string{"/"}},
	} {
		if got := Trail(c.dir); !slices.Equal(got, c.want) {
			t.Errorf("Trail(%q) = %q, want %q", c.dir, got, c.want)
		}
	}
}

func TestNatural(t *testing.T) {
	in := []string{"img10.png", "img2.png", "img1.png", "a.png", "img02b.png", "b.png", "img003.png"}
	slices.SortFunc(in, Natural)
	want := []string{"a.png", "b.png", "img1.png", "img2.png", "img02b.png", "img003.png", "img10.png"}
	if !slices.Equal(in, want) {
		t.Fatalf("got %v, want %v", in, want)
	}
}

func TestSortFoldersFirstAndReverse(t *testing.T) {
	old := time.Now().Add(-time.Hour)
	es := []Entry{newEntry("b.txt"), newEntry("Docs"), newEntry("A.txt"), newEntry("c.md"), newEntry("apps")}
	es[0].Size, es[2].Size, es[3].Size = 30, 10, 20
	es[0].ModTime, es[2].ModTime, es[3].ModTime = old, time.Now(), old.Add(-time.Hour)
	es[1].Dir, es[4].Dir = true, true

	Sort(es, Order{By: ByName})
	if got := names(es); !slices.Equal(got, []string{"apps", "Docs", "A.txt", "b.txt", "c.md"}) {
		t.Fatalf("by name: %v", got)
	}
	Sort(es, Order{By: BySize, Reverse: true})
	if got := names(es); !slices.Equal(got, []string{"Docs", "apps", "b.txt", "c.md", "A.txt"}) {
		t.Fatalf("by size, reversed (folders stay first): %v", got)
	}
	Sort(es, Order{By: ByKind, FilesFirst: true})
	if got := names(es); !slices.Equal(got, []string{"c.md", "A.txt", "b.txt", "apps", "Docs"}) {
		t.Fatalf("by kind, files first: %v", got)
	}
	Sort(es, Order{By: ByModified})
	if got := names(es); !slices.Equal(got[2:], []string{"c.md", "b.txt", "A.txt"}) {
		t.Fatalf("by modified: %v", got)
	}
}

func TestScore(t *testing.T) {
	cases := []struct{ better, worse string }{
		{"readme.md", "my-readme.md"},       // a prefix over a substring
		{"go.mod", "go.module.cache"},       // a shorter name wins a tie
		{"main_test.go", "maintenance.txt"}, // matches at word starts
	}
	q := map[string]string{"readme.md": "readme", "go.mod": "go.mod", "main_test.go": "mtest"}
	for _, c := range cases {
		b, w := Score(q[c.better], c.better), Score(q[c.better], c.worse)
		if b <= w {
			t.Errorf("%q: %s scored %d, not above %s's %d", q[c.better], c.better, b, c.worse, w)
		}
	}
	if Score("xyz", "readme.md") != 0 {
		t.Error("a query not in the name matched")
	}
	if Score("rdm", "readme.md") == 0 {
		t.Error("fuzzy letters in order did not match")
	}
	if Score("mdr", "readme.md") != 0 {
		t.Error("letters out of order matched")
	}
}

func TestFilterKeepsOrder(t *testing.T) {
	es := []Entry{newEntry("alpha"), newEntry("config.toml"), newEntry("conf"), newEntry("beta")}
	idx, best := Filter(es, "conf")
	if !slices.Equal(idx, []int{1, 2}) {
		t.Fatalf("matches %v", idx)
	}
	if idx[best] != 2 {
		t.Fatalf("best is %s, want the exact name", es[idx[best]].Name)
	}
	if idx, best := Filter(es, "zzz"); len(idx) != 0 || best != -1 {
		t.Fatalf("no match: %v %d", idx, best)
	}
	if idx, _ := Filter(es, ""); len(idx) != 4 {
		t.Fatalf("empty query: %v", idx)
	}
}

func TestReadDir(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.txt"), "hello")
	write(t, filepath.Join(dir, ".dot"), "")
	write(t, filepath.Join(dir, "secret"), "")
	write(t, filepath.Join(dir, ".hidden"), "secret\n")
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	os.Symlink("sub", filepath.Join(dir, "to-sub"))
	os.Symlink("a.txt", filepath.Join(dir, "to-a"))
	os.Symlink("nowhere", filepath.Join(dir, "broken"))

	es, err := ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Entry{}
	for _, e := range es {
		by[e.Name] = e
	}
	if e := by["a.txt"]; e.Size != 5 || e.Dir || e.Ext() != "txt" {
		t.Errorf("a.txt: %+v", e)
	}
	if !by[".dot"].Hidden || !by["secret"].Hidden || by["a.txt"].Hidden {
		t.Error("hidden: dot files and .hidden's names")
	}
	if e := by["to-sub"]; !e.Dir || e.Link != "sub" {
		t.Errorf("link to a folder: %+v", e)
	}
	if e := by["to-a"]; e.Dir || e.Size != 5 {
		t.Errorf("link to a file takes its size: %+v", e)
	}
	if !by["broken"].Broken {
		t.Error("broken link")
	}
}

func TestReadDirMany(t *testing.T) {
	dir := t.TempDir()
	for i := range parallelFrom * 2 {
		write(t, filepath.Join(dir, fmt.Sprintf("f%04d", i)), "x")
	}
	es, err := ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != parallelFrom*2 {
		t.Fatalf("read %d", len(es))
	}
	for _, e := range es {
		if e.Size != 1 || e.ModTime.IsZero() {
			t.Fatalf("not stat'ed: %+v", e)
		}
	}
}

func TestHistory(t *testing.T) {
	var h History
	h.Visit("/a")
	h.Visit("/b")
	p, ok := h.Back("/c")
	if !ok || p != "/b" {
		t.Fatalf("back: %q", p)
	}
	p, _ = h.Back("/b")
	if p != "/a" || h.CanBack() {
		t.Fatalf("back again: %q", p)
	}
	p, _ = h.Forward("/a")
	if p != "/b" || !h.CanForward() {
		t.Fatalf("forward: %q", p)
	}
	h.Visit("/b")
	if h.CanForward() {
		t.Fatal("somewhere new forgets forward")
	}
}

func TestJumps(t *testing.T) {
	now := time.Now()
	var j Jumps
	for range 5 {
		j.Visit("/home/u/Code/inua", now)
	}
	j.Visit("/home/u/Code/inua-files", now)
	j.Visit("/home/u/Documents/invoices", now.Add(-30*24*time.Hour))
	got := j.Find("inua", now, 10)
	if len(got) != 2 || got[0] != "/home/u/Code/inua" {
		t.Fatalf("by name and frecency: %v", got)
	}
	if got := j.Find("docinv", now, 10); len(got) != 1 || got[0] != "/home/u/Documents/invoices" {
		t.Fatalf("by path: %v", got)
	}
	if got := j.Find("", now, 1); len(got) != 1 || got[0] != "/home/u/Code/inua" {
		t.Fatalf("empty: %v", got)
	}
	for range maxRank {
		j.Visit("/x", now)
	}
	if _, ok := j.Dirs["/home/u/Documents/invoices"]; ok {
		t.Error("a rank aged below 1 is forgotten")
	}
}

func TestPlacesAndBookmarks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	os.Mkdir(filepath.Join(home, "Pictures"), 0o755)
	os.Mkdir(filepath.Join(home, "My Music"), 0o755)
	write(t, filepath.Join(home, ".config", "user-dirs.dirs"), `# written by xdg-user-dirs-update
XDG_DESKTOP_DIR="$HOME"
XDG_PICTURES_DIR="$HOME/Pictures"
XDG_MUSIC_DIR="$HOME/My Music"
XDG_VIDEOS_DIR="$HOME/Videos"
`)
	ps := Places()
	var got []string
	for _, p := range ps {
		got = append(got, p.Name)
	}
	if !slices.Equal(got, []string{"Home", "My Music", "Pictures"}) {
		t.Fatalf("places %v (desktop set to home and missing videos left out)", got)
	}

	// A bookmark of a user folder or of another bookmark is listed once.
	write(t, BookmarksPath(), "file://"+filepath.ToSlash(home)+"/Pictures\nfile:///srv\nfile:///srv Again\n")
	got = got[:0]
	for _, p := range Places() {
		got = append(got, p.Path)
	}
	if !slices.Equal(got, []string{home, filepath.Join(home, "My Music"), filepath.Join(home, "Pictures"), "/srv"}) {
		t.Fatalf("bookmarks not deduplicated: %v", got)
	}
	os.Remove(BookmarksPath())

	proj := filepath.Join(home, "My Project")
	if on, err := ToggleBookmark(proj); err != nil || !on {
		t.Fatalf("add: %v %v", on, err)
	}
	if b := read(t, BookmarksPath()); b != "file://"+filepath.ToSlash(home)+"/My%20Project\n" {
		t.Fatalf("bookmarks file %q", b)
	}
	bs := Bookmarks()
	if len(bs) != 1 || bs[0].Path != proj || bs[0].Name != "My Project" {
		t.Fatalf("read back %+v", bs)
	}
	write(t, BookmarksPath(), "sftp://box/srv Server\nfile:///tmp\n")
	bs = Bookmarks()
	if bs[0].Name != "Server" || bs[0].Path != "sftp://box/srv" || bs[1].Path != "/tmp" {
		t.Fatalf("labels and URIs: %+v", bs)
	}
	if on, _ := ToggleBookmark("/tmp"); on || Bookmarked("/tmp") {
		t.Fatal("remove")
	}
}

func TestMountinfo(t *testing.T) {
	const info = `22 1 259:2 / / rw,relatime shared:1 - ext4 /dev/nvme0n1p2 rw
24 22 0:5 / /proc rw - proc proc rw
30 22 259:1 / /boot/efi rw - vfat /dev/nvme0n1p1 rw
40 22 8:17 / /media/ann/USB\040STICK rw,nosuid shared:9 - vfat /dev/sdb1 rw
41 22 0:50 / /home/ann/remote rw master:3 - fuse.sshfs ann@box:/srv rw
42 22 0:51 / /run/user/1000/gvfs rw - fuse.gvfsd-fuse gvfsd-fuse rw
43 22 8:33 / /mnt/data ro - btrfs /dev/sdc1 ro
`
	ms := parseMountinfo(strings.NewReader(info))
	if len(ms) != 7 || ms[3].Point != "/media/ann/USB STICK" || !ms[6].ReadOnly {
		t.Fatalf("parsed %+v", ms)
	}
	var points []string
	for _, m := range UserMounts(ms, false) {
		points = append(points, m.Point)
	}
	if !slices.Equal(points, []string{"/home/ann/remote", "/mnt/data"}) {
		t.Fatalf("user mounts %v", points)
	}
	if m, _ := MountOf(ms, "/media/ann/USB STICK/photos/a.jpg"); m.FSType != "vfat" {
		t.Fatalf("mount of a path: %+v", m)
	}
	if m, _ := MountOf(ms, "/etc/hosts"); m.Point != "/" {
		t.Fatalf("root: %+v", m)
	}
}

func TestTrashAndRestore(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	f := filepath.Join(root, "work", "a b.txt")
	write(t, f, "x")
	tr, err := Trash(f)
	if err != nil {
		t.Fatal(err)
	}
	if exists(f) || !exists(tr.Path()) || tr.Trash != HomeTrash() {
		t.Fatalf("trashed to %+v", tr)
	}
	info := read(t, filepath.Join(HomeTrash(), "info", "a b.txt.trashinfo"))
	if !strings.Contains(info, "Path="+strings.ReplaceAll(filepath.ToSlash(f), " ", "%20")+"\n") || !strings.Contains(info, "DeletionDate=") {
		t.Fatalf("info:\n%s", info)
	}
	// The same name again gets a free one.
	write(t, f, "y")
	tr2, err := Trash(f)
	if err != nil || tr2.Name != "a b (2).txt" {
		t.Fatalf("second: %+v %v", tr2, err)
	}

	items := ListTrash(Trashes())
	if len(items) != 2 {
		t.Fatalf("listed %+v", items)
	}
	for _, it := range items {
		if it.Orig != f || it.Deleted.IsZero() {
			t.Fatalf("item %+v", it)
		}
	}
	if err := Restore(tr); err != nil || read(t, f) != "x" {
		t.Fatalf("restore: %v", err)
	}
	if err := Restore(tr2); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("restoring over a file: %v", err)
	}
	if err := EmptyTrash(Trashes()); err != nil || len(ListTrash(Trashes())) != 0 {
		t.Fatalf("empty: %v", err)
	}
}

func TestSplitExtAndNames(t *testing.T) {
	for in, want := range map[string][2]string{
		"a.txt": {"a", ".txt"}, ".bashrc": {".bashrc", ""}, "x.tar.gz": {"x", ".tar.gz"}, "noext": {"noext", ""}, "end.": {"end.", ""},
	} {
		if s, e := SplitExt(in); s != want[0] || e != want[1] {
			t.Errorf("%s: %q %q", in, s, e)
		}
	}
	dir := t.TempDir()
	write(t, filepath.Join(dir, "n.txt"), "")
	if got := filepath.Base(FreeName(dir, "n.txt", false)); got != "n (2).txt" {
		t.Fatalf("free %s", got)
	}
	write(t, filepath.Join(dir, "n (2).txt"), "")
	if got := filepath.Base(FreeName(dir, "n (2).txt", false)); got != "n (3).txt" {
		t.Fatalf("counts on: %s", got)
	}
	if got := filepath.Base(CopyName(dir, "n.txt", false)); got != "n (copy).txt" {
		t.Fatalf("copy name %s", got)
	}
	write(t, filepath.Join(dir, "n (copy).txt"), "")
	if got := filepath.Base(CopyName(dir, "n (copy).txt", false)); got != "n (copy 2).txt" {
		t.Fatalf("a copy of a copy: %s", got)
	}
	write(t, filepath.Join(dir, "n (copy 2).txt"), "")
	if got := filepath.Base(CopyName(dir, "n (copy 2).txt", false)); got != "n (copy 3).txt" {
		t.Fatalf("counts on: %s", got)
	}
	if got := filepath.Base(CopyName(dir, "n.txt", false)); got != "n (copy 3).txt" {
		t.Fatalf("the first free copy: %s", got)
	}
}

// tree writes files under dir: path → content.
func tree(t *testing.T, dir string, files map[string]string) {
	for p, c := range files {
		write(t, filepath.Join(dir, p), c)
	}
}

func run(t *testing.T, job Job, ask Asker) (Undo, error) {
	t.Helper()
	r := Runner{Ask: ask}
	return r.Run(context.Background(), job)
}

func TestCopyTreeAndUndo(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	tree(t, src, map[string]string{"d/a.txt": "aaa", "d/sub/b.txt": "bb", "c.txt": "c"})
	os.Symlink("a.txt", filepath.Join(src, "d", "link"))
	os.Chmod(filepath.Join(src, "c.txt"), 0o600)
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	os.Chtimes(filepath.Join(src, "c.txt"), old, old)
	os.Mkdir(dst, 0o755)

	var last Progress
	r := Runner{Report: func(p Progress) { last = p }}
	undo, err := r.Run(context.Background(), Job{Kind: Copy, Sources: []string{filepath.Join(src, "d"), filepath.Join(src, "c.txt")}, Dest: dst})
	if err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dst, "d", "sub", "b.txt")) != "bb" || read(t, filepath.Join(dst, "c.txt")) != "c" {
		t.Fatal("contents")
	}
	if l, _ := os.Readlink(filepath.Join(dst, "d", "link")); l != "a.txt" {
		t.Fatal("a link is copied as a link")
	}
	fi, _ := os.Stat(filepath.Join(dst, "c.txt"))
	if fi.Mode().Perm() != 0o600 || !fi.ModTime().Equal(old) {
		t.Fatalf("mode %v, time %v", fi.Mode(), fi.ModTime())
	}
	if last.Bytes != 6 || last.TotalBytes != 6 || last.Items != last.TotalItems {
		t.Fatalf("progress %+v", last)
	}
	es, _ := os.ReadDir(dst)
	for _, e := range es {
		if strings.HasSuffix(e.Name(), ".part") {
			t.Fatal("a part file was left")
		}
	}
	if len(undo.Created) != 2 {
		t.Fatalf("undo %+v", undo)
	}
	for _, j := range undo.Job() {
		if _, err := run(t, j, nil); err != nil {
			t.Fatal(err)
		}
	}
	if exists(filepath.Join(dst, "d")) || exists(filepath.Join(dst, "c.txt")) {
		t.Fatal("undo trashes the copies")
	}
}

func TestCopyIntoItsOwnFolder(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.txt"), "a")
	if _, err := run(t, Job{Kind: Copy, Sources: []string{filepath.Join(dir, "a.txt")}, Dest: dir}, nil); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dir, "a (copy).txt")) != "a" {
		t.Fatal("pasted beside itself")
	}
	os.Mkdir(filepath.Join(dir, "f"), 0o755)
	_, err := run(t, Job{Kind: Copy, Sources: []string{dir}, Dest: filepath.Join(dir, "f")}, nil)
	if !errors.Is(err, ErrIntoItself) {
		t.Fatalf("into itself: %v", err)
	}
}

func TestConflicts(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	tree(t, src, map[string]string{"a.txt": "new", "b.txt": "new", "d/x": "new", "d/y": "new"})
	tree(t, dst, map[string]string{"a.txt": "old", "b.txt": "old", "d/x": "old"})
	srcs := []string{filepath.Join(src, "a.txt"), filepath.Join(src, "b.txt"), filepath.Join(src, "d")}

	answers := map[string]Answer{"a.txt": Replace, "b.txt": KeepBoth, "d": Merge, "x": Skip}
	var asked []string
	_, err := run(t, Job{Kind: Copy, Sources: srcs, Dest: dst}, func(c Conflict) (Answer, bool) {
		asked = append(asked, filepath.Base(c.Dst))
		return answers[filepath.Base(c.Dst)], false
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(asked, []string{"a.txt", "b.txt", "d", "x"}) {
		t.Fatalf("asked %v", asked)
	}
	for p, want := range map[string]string{"a.txt": "new", "b.txt": "old", "b (2).txt": "new", "d/x": "old", "d/y": "new"} {
		if got := read(t, filepath.Join(dst, p)); got != want {
			t.Errorf("%s: %q, want %q", p, got, want)
		}
	}

	// An answer for all is not asked again; cancelling stops.
	n := 0
	_, err = run(t, Job{Kind: Copy, Sources: srcs[:2], Dest: dst}, func(Conflict) (Answer, bool) { n++; return Skip, true })
	if err != nil || n != 1 {
		t.Fatalf("for all: asked %d, %v", n, err)
	}
	_, err = run(t, Job{Kind: Copy, Sources: srcs[:2], Dest: dst}, func(Conflict) (Answer, bool) { return Cancel, false })
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestMoveAndUndo(t *testing.T) {
	root := t.TempDir()
	tree(t, root, map[string]string{"a/f.txt": "f", "a/g/h": "h", "b/keep": ""})
	undo, err := run(t, Job{Kind: Move, Sources: []string{filepath.Join(root, "a", "f.txt"), filepath.Join(root, "a", "g")}, Dest: filepath.Join(root, "b")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(root, "a", "f.txt")) || read(t, filepath.Join(root, "b", "g", "h")) != "h" {
		t.Fatal("moved")
	}
	for _, j := range undo.Job() {
		if _, err := run(t, j, nil); err != nil {
			t.Fatal(err)
		}
	}
	if !exists(filepath.Join(root, "a", "f.txt")) || !exists(filepath.Join(root, "a", "g", "h")) || exists(filepath.Join(root, "b", "g")) {
		t.Fatal("undo moves back")
	}
}

func TestMoveMerge(t *testing.T) {
	root := t.TempDir()
	tree(t, root, map[string]string{"src/d/a": "a", "src/d/b": "new", "dst/d/b": "old"})
	_, err := run(t, Job{Kind: Move, Sources: []string{filepath.Join(root, "src", "d")}, Dest: filepath.Join(root, "dst")},
		func(c Conflict) (Answer, bool) {
			if c.SrcDir {
				return Merge, false
			}
			return Skip, false
		})
	if err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(root, "dst", "d", "a")) != "a" || read(t, filepath.Join(root, "dst", "d", "b")) != "old" {
		t.Fatal("merged, skipping b")
	}
	if !exists(filepath.Join(root, "src", "d", "b")) {
		t.Fatal("the skipped item stays where it was")
	}
}

func TestRenameTrashDeleteCreate(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	write(t, filepath.Join(root, "a"), "a")
	write(t, filepath.Join(root, "b"), "b")
	if _, err := run(t, Job{Kind: Rename, Sources: []string{filepath.Join(root, "a")}, Dest: filepath.Join(root, "b")}, nil); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("rename over a file: %v", err)
	}
	undo, err := run(t, Job{Kind: Rename, Sources: []string{filepath.Join(root, "a")}, Dest: filepath.Join(root, "c")}, nil)
	if err != nil || read(t, filepath.Join(root, "c")) != "a" {
		t.Fatalf("rename: %v", err)
	}
	run(t, undo.Job()[0], nil)
	if !exists(filepath.Join(root, "a")) {
		t.Fatal("undo rename")
	}

	undo, err = run(t, Job{Kind: MoveToTrash, Sources: []string{filepath.Join(root, "a")}}, nil)
	if err != nil || exists(filepath.Join(root, "a")) {
		t.Fatalf("trash: %v", err)
	}
	run(t, undo.Job()[0], nil)
	if read(t, filepath.Join(root, "a")) != "a" {
		t.Fatal("undo trash restores")
	}

	if _, err := run(t, Job{Kind: Mkdir, Dest: filepath.Join(root, "new")}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, Job{Kind: Create, Dest: filepath.Join(root, "new", "f")}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, Job{Kind: Delete, Sources: []string{filepath.Join(root, "new")}}, nil); err != nil || exists(filepath.Join(root, "new")) {
		t.Fatalf("delete: %v", err)
	}
}

func TestCancelLeavesNoPart(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "src"), "data")
	os.Mkdir(filepath.Join(root, "out"), 0o755)

	// Cancelled before it starts: nothing is made.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var r Runner
	if _, err := r.Run(ctx, Job{Kind: Copy, Sources: []string{filepath.Join(root, "src")}, Dest: filepath.Join(root, "out")}); !errors.Is(err, ErrCancelled) {
		t.Fatalf("err %v", err)
	}
	// Stopped half way through writing: the part file goes.
	r.ctx = context.Background()
	err := r.place(filepath.Join(root, "out", "f"), false, func(tmp string) error {
		os.WriteFile(tmp, []byte("half"), 0o644)
		return ErrCancelled
	})
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("place: %v", err)
	}
	if es, _ := os.ReadDir(filepath.Join(root, "out")); len(es) != 0 {
		t.Fatalf("left %v", es)
	}
}

func TestClipboard(t *testing.T) {
	paths := []string{"/home/u/a b.txt", "/tmp/x"}
	got, cut := ParseClipboard(GnomeCopied(paths, true))
	if !cut || !slices.Equal(got, paths) {
		t.Fatalf("gnome: %v %v", got, cut)
	}
	got, cut = ParseClipboard(URIList(paths))
	if cut || !slices.Equal(got, paths) {
		t.Fatalf("uri-list: %v %v", got, cut)
	}
	got, _ = ParseClipboard("# comment\nsftp://box/x\n/plain/path\n")
	if !slices.Equal(got, []string{"/plain/path"}) {
		t.Fatalf("plain and remote: %v", got)
	}
}

func TestWatch(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changed, gone := make(chan struct{}, 10), make(chan struct{}, 1)
	if err := Watch(ctx, dir, func() { changed <- struct{}{} }, func() { gone <- struct{}{} }); err != nil {
		t.Fatal(err)
	}
	for i := range 50 { // a burst is one change
		write(t, filepath.Join(dir, fmt.Sprint(i)), "")
	}
	select {
	case <-changed:
	case <-time.After(2 * time.Second):
		t.Fatal("no change reported")
	}
	time.Sleep(3 * settle)
	if n := len(changed); n > 1 {
		t.Fatalf("a burst reported %d more times", n)
	}
	os.RemoveAll(dir)
	select {
	case <-gone:
	case <-time.After(2 * time.Second):
		t.Fatal("deleting the folder was not reported")
	}
}

func TestSearch(t *testing.T) {
	root := t.TempDir()
	tree(t, root, map[string]string{
		"a/report-2024.pdf": "", "a/b/c/report-final.odt": "", "notes.txt": "",
		".hide/report.txt": "", "node_modules/report.js": "",
	})
	var mu sync.Mutex
	var got []string
	Search(context.Background(), root, "report", SearchOptions{}, func(hs []Hit) {
		mu.Lock()
		defer mu.Unlock()
		for _, h := range hs {
			rel, _ := filepath.Rel(root, h.Path())
			got = append(got, rel)
		}
	})
	slices.Sort(got)
	if !slices.Equal(got, []string{"a/b/c/report-final.odt", "a/report-2024.pdf"}) {
		t.Fatalf("found %v", got)
	}
	got = nil
	Search(context.Background(), root, "report", SearchOptions{Hidden: true, Limit: 1}, func(hs []Hit) { got = append(got, hs[0].Entry.Name) })
	if len(got) != 1 {
		t.Fatalf("limit: %v", got)
	}
}

func BenchmarkReadSort(b *testing.B) {
	dir := b.TempDir()
	for i := range 20000 {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("File %d.txt", i)), nil, 0o644)
	}
	b.ResetTimer()
	for range b.N {
		es, err := ReadDir(dir)
		if err != nil {
			b.Fatal(err)
		}
		Sort(es, Order{By: ByName})
	}
}

func BenchmarkFilter(b *testing.B) {
	es := make([]Entry, 100000)
	for i := range es {
		es[i] = newEntry(fmt.Sprintf("Document %d final draft.odt", i))
	}
	b.ResetTimer()
	for range b.N {
		Filter(es, "dfd")
	}
}

func TestThumbnail(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	src := image.NewRGBA(image.Rect(0, 0, 800, 400))
	for i := range src.Pix {
		src.Pix[i] = 0xff
	}
	path := filepath.Join(root, "wide.png")
	f, _ := os.Create(path)
	png.Encode(f, src)
	f.Close()
	fi, _ := os.Stat(path)

	img, err := Thumbnail(path, fi.ModTime())
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != ThumbSize || b.Dy() != ThumbSize/2 {
		t.Fatalf("size %v", b)
	}
	data, err := os.ReadFile(ThumbPath(path))
	if err != nil {
		t.Fatalf("not cached: %v", err)
	}
	text := pngText(data)
	if text["Thumb::URI"] != URIOfPath(path) || text["Thumb::MTime"] != fmt.Sprint(fi.ModTime().Unix()) {
		t.Fatalf("text %v", text)
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatalf("the cached PNG with its text does not decode: %v", err)
	}
	if _, ok := readThumb(ThumbPath(path), fi.ModTime()); !ok {
		t.Fatal("a fresh cache entry is read back")
	}
	if _, ok := readThumb(ThumbPath(path), fi.ModTime().Add(time.Second)); ok {
		t.Fatal("a stale cache entry is read")
	}
}

func TestStatAttributes(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	write(t, p, "abc")
	os.Chmod(p, 0o640)
	opened := time.Date(2021, 5, 6, 7, 8, 9, 0, time.UTC)
	os.Chtimes(p, opened, time.Now())
	e, err := Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Lstat(p)
	if e.Mode != fi.Mode() || e.Size != 3 || !e.ModTime.Equal(fi.ModTime()) {
		t.Fatalf("mode %v size %d mtime %v; lstat %v %v", e.Mode, e.Size, e.ModTime, fi.Mode(), fi.ModTime())
	}
	if !e.Opened.Equal(opened) || e.UID != uint32(os.Getuid()) {
		t.Fatalf("opened %v uid %d", e.Opened, e.UID)
	}
	if !e.Created.IsZero() && e.Created.After(time.Now().Add(time.Minute)) {
		t.Fatalf("created %v", e.Created)
	}
	os.Mkdir(filepath.Join(dir, "d"), 0o755)
	os.Symlink("d", filepath.Join(dir, "l"))
	if e, _ := Stat(filepath.Join(dir, "d")); !e.Dir || e.Mode&fs.ModeDir == 0 {
		t.Fatalf("folder %+v", e)
	}
	if e, _ := Stat(filepath.Join(dir, "l")); !e.Dir || e.Mode&fs.ModeSymlink == 0 || e.Link != "d" {
		t.Fatalf("link %+v", e)
	}
}

func TestSortByAttributes(t *testing.T) {
	a, b := newEntry("a"), newEntry("b")
	a.Opened, b.Opened = time.Unix(200, 0), time.Unix(100, 0)
	a.Mode, b.Mode = 0o644, 0o600
	a.UID, b.UID = 2, 1
	for by, want := range map[SortBy]string{ByOpened: "b", ByPerms: "b", ByOwner: "b", ByCreated: "a"} {
		es := []Entry{a, b}
		Sort(es, Order{By: by})
		if es[0].Name != want {
			t.Errorf("by %s: %s first", by, es[0].Name)
		}
	}
}

func TestToggleColumn(t *testing.T) {
	s := &Settings{Columns: []SortBy{BySize, ByKind}}
	s.ToggleColumn(ByOwner)
	s.ToggleColumn(ByModified)
	if !slices.Equal(s.Columns, []SortBy{BySize, ByModified, ByKind, ByOwner}) {
		t.Fatalf("on, in their order: %v", s.Columns)
	}
	s.ToggleColumn(BySize)
	if s.ShowsColumn(BySize) || len(s.Columns) != 3 {
		t.Fatalf("off: %v", s.Columns)
	}
}

func TestColumnWidthsAreHeldToWhatAColumnNeeds(t *testing.T) {
	s := &Settings{}
	if s.ColumnWidth(ByName) != 0 || s.ColumnWidth(BySize) != 0 {
		t.Fatal("a column never dragged has a width")
	}
	s.SetColumnWidth(BySize, 120)
	s.SetColumnWidth(ByKind, 3)
	s.SetColumnWidth(ByName, 40)
	s.SetColumnWidth(ByModified, 1e6)
	s.SetColumnWidth("colour", 200)
	want := map[SortBy]int{BySize: 120, ByKind: MinColumnWidth, ByName: MinNameWidth, ByModified: MaxColumnWidth}
	if !maps.Equal(s.Widths, want) || !s.modified {
		t.Fatalf("Widths = %v, want %v", s.Widths, want)
	}
	s.modified = false
	s.SetColumnWidth(BySize, 120)
	s.ResetColumnWidth(ByOwner)
	if s.modified {
		t.Error("a width unchanged marks the settings changed")
	}
	s.ResetColumnWidth(ByName)
	if s.ColumnWidth(ByName) != 0 || !s.modified {
		t.Errorf("the name keeps %d after a reset", s.ColumnWidth(ByName))
	}
}

func TestColumnWidthsReadBackAndBadOnesForgotten(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	s := LoadSettings()
	s.SetColumnWidth(ByName, 300)
	s.SetColumnWidth(ByOwner, 70)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if got := LoadSettings().Widths; !maps.Equal(got, map[SortBy]int{ByName: 300, ByOwner: 70}) {
		t.Fatalf("read back %v", got)
	}

	data := `{"columns": ["size"], "widths": {"name": 90, "size": 100, "kind": -4, "modified": 99999, "colour": 80, "owner": 0}}`
	write(t, store.Path("files"), data)
	if got := LoadSettings().Widths; !maps.Equal(got, map[SortBy]int{BySize: 100}) {
		t.Errorf("kept %v, want only size's", got)
	}
}

func TestServersUsedBeforeBecomeRemoteDisks(t *testing.T) {
	s := &Settings{
		Remote:  []remote.Disk{{Name: "nas", Address: "smb://nas/media"}},
		Servers: []string{"sftp://ann@box/srv", "smb://nas/media", "mtp://phone/", "smb://nas2/media"},
	}
	s.adoptServers()
	want := []remote.Disk{
		{Name: "nas", Address: "smb://nas/media"},
		{Name: "box", Address: "sftp://ann@box/srv"},
		{Name: "media", Address: "smb://nas2/media"},
	}
	if !slices.Equal(s.Remote, want) || s.Servers != nil || !s.modified {
		t.Errorf("Remote = %+v, Servers = %v", s.Remote, s.Servers)
	}
	s.SaveDisk(remote.Disk{Name: "box", Address: "sftp://bob@box/srv"})
	s.ForgetDisk("nas")
	if len(s.Remote) != 2 || s.Remote[0].Address != "sftp://bob@box/srv" {
		t.Errorf("after saving and forgetting: %+v", s.Remote)
	}
}

func TestIsLocalURITakesRelativePaths(t *testing.T) {
	for s, want := range map[string]bool{
		"/srv": true, "~/Downloads": true, "file:///srv": true,
		".": true, "Downloads": true, "../notes/todo.txt": true,
		"sftp://box/srv": false, "smb://nas/media": false, "trash:": false,
	} {
		if got := IsLocalURI(s); got != want {
			t.Errorf("IsLocalURI(%q) = %v, want %v", s, got, want)
		}
	}
}
