package files

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNewName(t *testing.T) {
	cases := []struct {
		rule RenameRule
		name string
		dir  bool
		i, n int
		want string
	}{
		{RenameRule{Mode: ReplaceText, Find: "img", Replace: "Photo"}, "IMG_0042.JPG", false, 0, 1, "Photo_0042.JPG"},
		{RenameRule{Mode: ReplaceText, Find: "img", Replace: "Photo", MatchCase: true}, "IMG_0042.JPG", false, 0, 1, "IMG_0042.JPG"},
		{RenameRule{Mode: ReplaceText, Find: `(\d+)-(\d+)`, Replace: "$2-$1", Regexp: true}, "shot 2024-10.png", false, 0, 1, "shot 10-2024.png"},
		{RenameRule{Mode: ReplaceText, Find: "$1", Replace: "x"}, "a$1b.txt", false, 0, 1, "axb.txt"}, // not a pattern
		{RenameRule{Mode: ReplaceText, Find: ".", Replace: "_"}, "a.b.tar.gz", false, 0, 1, "a_b.tar.gz"},
		{RenameRule{Mode: AddText, Text: "draft-"}, "notes.md", false, 0, 1, "draft-notes.md"},
		{RenameRule{Mode: AddText, Text: " (old)", After: true}, "notes.md", false, 0, 1, "notes (old).md"},
		{RenameRule{Mode: AddText, Text: " (old)", After: true}, "Photos.2024", true, 0, 1, "Photos.2024 (old)"},
		{RenameRule{Mode: Number, Text: "Beach"}, "IMG_1.jpg", false, 0, 12, "Beach 01.jpg"},
		{RenameRule{Mode: Number, Text: "Beach"}, "IMG_9.jpg", false, 11, 12, "Beach 12.jpg"},
		{RenameRule{Mode: Number, Start: 7}, "x.txt", false, 2, 3, "9.txt"},
		{RenameRule{Mode: ChangeCase, Case: Lower}, "README.MD", false, 0, 1, "readme.MD"},
		{RenameRule{Mode: ChangeCase, Case: Upper}, "readme.md", false, 0, 1, "README.md"},
		{RenameRule{Mode: ChangeCase, Case: Title}, "the QUICK-brown fox.txt", false, 0, 1, "The Quick-Brown Fox.txt"},
	}
	for _, c := range cases {
		re, err := c.rule.Compile()
		if err != nil {
			t.Fatal(err)
		}
		if got := c.rule.NewName(re, c.name, c.dir, c.i, c.n); got != c.want {
			t.Errorf("%+v %q: %q, want %q", c.rule, c.name, got, c.want)
		}
	}
	if _, err := (RenameRule{Mode: ReplaceText, Find: "(", Regexp: true}).Compile(); err == nil {
		t.Error("a pattern that is not one was taken")
	}
}

func TestPlan(t *testing.T) {
	dir := t.TempDir()
	tree(t, dir, map[string]string{"a.txt": "a", "b.txt": "b", "c.txt": "c", "taken.md": "t"})
	p := func(n string) string { return filepath.Join(dir, n) }

	// Names among the items: fine, they move at once.
	plan, err := Plan([]string{p("a.txt"), p("b.txt"), p("c.txt")}, RenameRule{Mode: ChangeCase, Case: Upper})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range plan {
		if r.Problem != "" {
			t.Errorf("%s: %s", r.From, r.Problem)
		}
	}
	// Two given one name.
	plan, _ = Plan([]string{p("a.txt"), p("b.txt")}, RenameRule{Mode: Number, Text: "same", Start: 1})
	if plan[0].Problem != "" || plan[1].Problem != "" {
		t.Errorf("numbered: %+v", plan)
	}
	plan, _ = Plan([]string{p("a.txt"), p("b.txt")}, RenameRule{Mode: ReplaceText, Find: `^.$`, Replace: "x", Regexp: true})
	if plan[0].Problem != ErrNameTwice.Error() || plan[1].Problem != ErrNameTwice.Error() {
		t.Errorf("twice: %+v", plan)
	}
	// Taken by a file not renamed.
	plan, _ = Plan([]string{p("c.txt")}, RenameRule{Mode: ReplaceText, Find: "c", Replace: "taken"})
	if plan[0].To != p("taken.txt") || plan[0].Problem != "" {
		t.Errorf("not taken: %+v", plan)
	}
	tree(t, dir, map[string]string{"taken.txt": "t"})
	plan, _ = Plan([]string{p("c.txt")}, RenameRule{Mode: ReplaceText, Find: "c", Replace: "taken"})
	if plan[0].Problem != ErrNameTaken.Error() {
		t.Errorf("taken: %+v", plan)
	}
	// Bad names.
	plan, _ = Plan([]string{p("c.txt")}, RenameRule{Mode: ReplaceText, Find: "c", Replace: "a/b"})
	if plan[0].Problem != ErrNameSlash.Error() {
		t.Errorf("slash: %+v", plan)
	}
}

// Names swapped among the items go through, and undo puts them back in
// one go.
func TestRenameManySwapAndUndo(t *testing.T) {
	dir := t.TempDir()
	tree(t, dir, map[string]string{"a.txt": "A", "b.txt": "B", "c.txt": "C"})
	p := func(n string) string { return filepath.Join(dir, n) }
	undo, err := run(t, Job{Kind: RenameMany, Sources: []string{p("a.txt"), p("b.txt"), p("c.txt")},
		Dests: []string{p("b.txt"), p("a.txt"), p("d.txt")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, p("a.txt")) != "B" || read(t, p("b.txt")) != "A" || read(t, p("d.txt")) != "C" || exists(p("c.txt")) {
		t.Fatal("not renamed")
	}
	jobs := undo.Job()
	if len(jobs) != 1 || jobs[0].Kind != RenameMany {
		t.Fatalf("undo: %+v", jobs)
	}
	if _, err := run(t, jobs[0], nil); err != nil {
		t.Fatal(err)
	}
	if read(t, p("a.txt")) != "A" || read(t, p("b.txt")) != "B" || read(t, p("c.txt")) != "C" || exists(p("d.txt")) {
		t.Fatal("not put back")
	}
	if es, _ := os.ReadDir(dir); len(es) != 3 {
		t.Fatalf("left: %v", es)
	}
}

// A name taken meanwhile: that item keeps its name, the rest go on, and
// no hidden name is left.
func TestRenameManyTakenMeanwhile(t *testing.T) {
	dir := t.TempDir()
	tree(t, dir, map[string]string{"a.txt": "A", "b.txt": "B", "there.txt": "T"})
	p := func(n string) string { return filepath.Join(dir, n) }
	undo, err := run(t, Job{Kind: RenameMany, Sources: []string{p("a.txt"), p("b.txt")},
		Dests: []string{p("there.txt"), p("bee.txt")}}, nil)
	if err == nil {
		t.Fatal("no error for the name taken")
	}
	if read(t, p("a.txt")) != "A" || read(t, p("there.txt")) != "T" || read(t, p("bee.txt")) != "B" {
		t.Fatal("wrong names")
	}
	if !slices.Equal(undo.Renames, [][2]string{{p("b.txt"), p("bee.txt")}}) {
		t.Fatalf("undo: %+v", undo.Renames)
	}
	es, _ := os.ReadDir(dir)
	for _, e := range es {
		if strings.HasPrefix(e.Name(), ".") {
			t.Fatalf("left %s", e.Name())
		}
	}
}

// Renaming one item selects its name but the last extension; positions
// are characters, as GTK counts them.
func TestRenameSelection(t *testing.T) {
	for _, c := range []struct {
		name string
		dir  bool
		want string // what is selected
	}{
		{"abc.jpg", false, "abc"},
		{"archive.tar.gz", false, "archive.tar"},
		{"a.b.c.d", false, "a.b.c"},
		{"README", false, "README"},
		{".gitignore", false, ".gitignore"},
		{"..hidden", false, "..hidden"},
		{".config.json", false, ".config"},
		{"notes.", false, "notes."},
		{"a.b.", false, "a.b."},
		{"...", false, "..."},
		{".", false, "."},
		{"", false, ""},
		{"Mr. Smith", false, "Mr. Smith"},
		{"Mr. Smith.txt", false, "Mr. Smith"},
		{"my.folder", true, "my.folder"},
		{"photos.2024.d", true, "photos.2024.d"},
		{".config", true, ".config"},
		{"noext", true, "noext"},
		{"照片.jpg", false, "照片"},
		{"café.txt", false, "café"}, // e and a combining accent: two characters
		{"👍🏽 ok.png", false, "👍🏽 ok"}, // an emoji and its skin tone: two characters
		{"résumé.pdf.zip", false, "résumé.pdf"},
		{"имя.файл.txt", false, "имя.файл"},
	} {
		got := RenameSelection(c.name, c.dir)
		if want := utf8.RuneCountInString(c.want); got != want {
			sel := "(past the end)"
			if r := []rune(c.name); got <= len(r) {
				sel = string(r[:got])
			}
			t.Errorf("%q (folder %v): selects %d characters, %q; want %d, %q", c.name, c.dir, got, sel, want, c.want)
		}
	}
}
