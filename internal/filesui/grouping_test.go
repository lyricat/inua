package filesui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lyricat/inua/internal/apps/files"
)

func TestModifiedGroupOrder(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.Local) // a Saturday
	cases := []struct {
		mod  time.Time
		want string
	}{
		{now.Add(24 * time.Hour), "Later"},
		{now, "Today"},
		{now.AddDate(0, 0, -1), "Yesterday"},
		{now.AddDate(0, 0, -3), "Earlier this week"},
		{now.AddDate(0, 0, -8), "Last week"},
		{now.AddDate(0, 0, -15), "Earlier this month"},
		{now.AddDate(0, -1, -2), "Last month"},
		{now.AddDate(0, -5, 0), "Earlier this year"},
		{now.AddDate(-1, 0, 0), "Older"},
		{time.Time{}, "Unknown date"},
	}
	last := -1
	for _, c := range cases {
		rank, label := modifiedGroup(c.mod, now)
		if label != c.want {
			t.Errorf("modifiedGroup(%v) = %q, want %q", c.mod, label, c.want)
		}
		if rank <= last {
			t.Errorf("modifiedGroup(%v) rank %d after %d", c.mod, rank, last)
		}
		last = rank
	}
}

func TestSizeGroupBoundaries(t *testing.T) {
	for _, c := range []struct {
		size int64
		want string
	}{
		{0, "Empty"},
		{1, "Under 1 MB"},
		{(1 << 20) - 1, "Under 1 MB"},
		{1 << 20, "1–100 MB"},
		{100<<20 - 1, "1–100 MB"},
		{100 << 20, "100 MB–1 GB"},
		{1<<30 - 1, "100 MB–1 GB"},
		{1 << 30, "1 GB or more"},
	} {
		if _, label := sizeGroup(c.size); label != c.want {
			t.Errorf("sizeGroup(%d) = %q, want %q", c.size, label, c.want)
		}
	}
}

// testEntries makes a folder and files and reads them back.
func testEntries(t *testing.T, folder string, names ...string) []files.Entry {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, folder), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	es, err := files.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return es
}

func byName(t *testing.T, es []files.Entry, name string) *files.Entry {
	t.Helper()
	for i := range es {
		if es[i].Name == name {
			return &es[i]
		}
	}
	t.Fatalf("no entry %q in %v", name, es)
	return nil
}

func TestGroupedSortKeepsFoldersAndSectionOrder(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.Local)
	es := testEntries(t, "Docs", "z.txt", "a.md", "old.zip", "y.txt")
	byName(t, es, "z.txt").ModTime = now
	byName(t, es, "a.md").ModTime = now
	byName(t, es, "y.txt").ModTime = now
	byName(t, es, "old.zip").ModTime = now.AddDate(0, -1, -2)
	ids, labels := sortGroupedEntries(es, files.Order{By: files.ByName, GroupBy: files.GroupModified}, now)
	got := make([]string, len(es))
	for i := range es {
		got[i] = es[i].Name
	}
	want := []string{"Docs", "a.md", "y.txt", "z.txt", "old.zip"}
	if !slices.Equal(got, want) {
		t.Fatalf("grouped order = %v, want %v", got, want)
	}
	wantIDs := []string{"folder", "today", "today", "today", "last month"}
	if !slices.Equal(ids, wantIDs) {
		t.Fatalf("group ids = %v, want %v", ids, wantIDs)
	}
	if labels[0] != "Folders" || labels[1] != "Today" || labels[4] != "Last month" {
		t.Fatalf("group labels = %v", labels)
	}
}

func TestGridSectionsStartOnSeparateRows(t *testing.T) {
	names := []string{"a", "b", "c", "d", "e"}
	for _, cols := range []int{1, 3, 5} {
		layout, slots, positions := gridSectionSlots(names, cols, func(pos int) string {
			switch pos {
			case 0:
				return "First"
			case 2:
				return "Second"
			}
			return ""
		})
		for i, pos := range positions {
			if slots[pos] != i || layout[pos] != names[i] {
				t.Fatalf("%d columns: file %q maps to slot %d: %q", cols, names[i], pos, layout[pos])
			}
		}
		for _, start := range []uint32{positions[0], positions[2]} {
			if int(start)%cols != 0 || !strings.HasPrefix(layout[int(start)-cols], "/") {
				t.Fatalf("%d columns: group starts in the middle of a row or without its header: %q", cols, layout)
			}
			for _, slot := range slots[int(start)-cols : start] {
				if slot != -1 {
					t.Fatalf("%d columns: a header slot points to a file", cols)
				}
			}
		}
	}
}

func TestGroupedTypeSortsSameExtensionsTogether(t *testing.T) {
	es := testEntries(t, "Folder", "b.txt", "a.md", "a.txt")
	ids, _ := sortGroupedEntries(es, files.Order{By: files.ByName, GroupBy: files.GroupType}, time.Now())
	if !es[0].Dir || ids[0] != "folder" {
		t.Fatalf("folders first: %+v, ids %v", es[0], ids)
	}
	txt := map[string]string{}
	for i := range es {
		if es[i].Name == "a.txt" || es[i].Name == "b.txt" {
			txt[es[i].Name] = ids[i]
		}
	}
	if txt["a.txt"] == "" || txt["a.txt"] != txt["b.txt"] {
		t.Fatalf("txt files in groups %v", txt)
	}
}
