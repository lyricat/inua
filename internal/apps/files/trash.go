package files

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The freedesktop Trash specification 1.0: an item goes to the home trash
// when it is on the home trash's filesystem, else to the trash at the top
// of its own mount ($topdir/.Trash/$uid when the admin made .Trash, else
// $topdir/.Trash-$uid). Each trash has files/ and info/NAME.trashinfo,
// which records where the item was and when it was trashed.

// Trashed is an item in a trash.
type Trashed struct {
	Name     string    // its name in the trash's files/
	Orig     string    // where it was
	Deleted  time.Time // when it was trashed
	Trash    string    // the trash folder that holds it
	Dir      bool
	Size     int64
	infoPath string
}

// Path is where the item is now.
func (t Trashed) Path() string { return filepath.Join(t.Trash, "files", t.Name) }

// HomeTrash is $XDG_DATA_HOME/Trash.
func HomeTrash() string { return filepath.Join(DataHome(), "Trash") }

// trashFor chooses the trash for path and the path to record in its
// info: absolute for the home trash, relative to the mount's top for a
// mount's trash, as the specification allows.
func trashFor(path string) (trash, recorded string, err error) {
	st, err := os.Lstat(path)
	if err != nil {
		return "", "", err
	}
	dev := devOf(st)
	home := HomeTrash()
	if err := os.MkdirAll(filepath.Join(home, "files"), 0o700); err == nil {
		if hs, err := os.Stat(home); err == nil && devOf(hs) == dev {
			return home, path, nil
		}
	}
	ms, err := ReadMounts()
	if err != nil {
		return "", "", err
	}
	m, ok := MountOf(ms, path)
	if !ok {
		return "", "", errors.New("no mount holds " + path)
	}
	top := m.Point
	uid := strconv.Itoa(os.Getuid())
	rel, _ := filepath.Rel(top, path)
	// $topdir/.Trash/$uid, when .Trash is a real sticky folder.
	admin := filepath.Join(top, ".Trash")
	if fi, err := os.Lstat(admin); err == nil && fi.IsDir() && fi.Mode()&fs.ModeSticky != 0 {
		t := filepath.Join(admin, uid)
		if err := makeTrash(t); err == nil {
			return t, rel, nil
		}
	}
	t := filepath.Join(top, ".Trash-"+uid)
	if err := makeTrash(t); err != nil {
		return "", "", fmt.Errorf("no trash on %s: %w", top, err)
	}
	return t, rel, nil
}

func makeTrash(t string) error {
	for _, d := range []string{t, filepath.Join(t, "files"), filepath.Join(t, "info")} {
		if err := os.Mkdir(d, 0o700); err != nil && !os.IsExist(err) {
			return err
		}
	}
	fi, err := os.Lstat(t)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return errors.New(t + " is not a folder")
	}
	return nil
}

func devOf(fi fs.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return st.Dev
	}
	return 0
}

// Trash moves path to the trash and returns where it went.
func Trash(path string) (Trashed, error) {
	path = filepath.Clean(path)
	trash, recorded, err := trashFor(path)
	if err != nil {
		return Trashed{}, err
	}
	if err := makeTrash(trash); err != nil {
		return Trashed{}, err
	}
	fi, _ := os.Lstat(path)
	now := time.Now()
	base := filepath.Base(path)
	// The info file is made first, exclusively: it reserves the name.
	for n := 1; ; n++ {
		name := base
		if n > 1 {
			name = numbered(base, n, fi != nil && fi.IsDir())
		}
		info := filepath.Join(trash, "info", name+".trashinfo")
		f, err := os.OpenFile(info, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return Trashed{}, err
		}
		_, werr := fmt.Fprintf(f, "[Trash Info]\nPath=%s\nDeletionDate=%s\n", escapeTrashPath(recorded), now.Format("2006-01-02T15:04:05"))
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		dest := filepath.Join(trash, "files", name)
		if werr == nil {
			if _, err := os.Lstat(dest); err == nil {
				// Left behind by a trash without its info: try another name.
				os.Remove(info)
				continue
			}
			werr = os.Rename(path, dest)
		}
		if werr != nil {
			os.Remove(info)
			return Trashed{}, werr
		}
		t := Trashed{Name: name, Orig: path, Deleted: now, Trash: trash, infoPath: info}
		if fi != nil {
			t.Dir, t.Size = fi.IsDir(), fi.Size()
		}
		return t, nil
	}
}

// escapeTrashPath escapes a path as the specification asks: as in a URI,
// keeping the slashes.
func escapeTrashPath(p string) string {
	return (&url.URL{Path: p}).EscapedPath()
}

// Trashes are the trash folders there are: the home one, then those at
// the top of the mounts.
func Trashes() []string {
	out := []string{HomeTrash()}
	ms, err := ReadMounts()
	if err != nil {
		return out
	}
	uid := strconv.Itoa(os.Getuid())
	seen := map[string]bool{HomeTrash(): true}
	for _, m := range ms {
		if pseudoFS[m.FSType] {
			continue
		}
		for _, t := range []string{filepath.Join(m.Point, ".Trash", uid), filepath.Join(m.Point, ".Trash-"+uid)} {
			if seen[t] {
				continue
			}
			seen[t] = true
			if fi, err := os.Stat(filepath.Join(t, "info")); err == nil && fi.IsDir() {
				out = append(out, t)
			}
		}
	}
	return out
}

// ListTrash lists the items in the given trash folders, by the infos
// they have; an item without its info is left out (the specification says
// to treat it as not there).
func ListTrash(trashes []string) []Trashed {
	var out []Trashed
	for _, t := range trashes {
		infos, err := os.ReadDir(filepath.Join(t, "info"))
		if err != nil {
			continue
		}
		top := ""
		if t != HomeTrash() {
			top = topOfTrash(t)
		}
		for _, in := range infos {
			name, ok := strings.CutSuffix(in.Name(), ".trashinfo")
			if !ok {
				continue
			}
			it, err := readTrashInfo(filepath.Join(t, "info", in.Name()))
			if err != nil {
				continue
			}
			it.Name, it.Trash = name, t
			if !filepath.IsAbs(it.Orig) {
				it.Orig = filepath.Join(top, it.Orig)
			}
			fi, err := os.Lstat(it.Path())
			if err != nil {
				continue
			}
			it.Dir, it.Size = fi.IsDir(), fi.Size()
			out = append(out, it)
		}
	}
	return out
}

// topOfTrash is the mount a trash folder belongs to: the parent of
// .Trash-$uid, or the grandparent of .Trash/$uid.
func topOfTrash(t string) string {
	parent := filepath.Dir(t)
	if filepath.Base(parent) == ".Trash" {
		return filepath.Dir(parent)
	}
	return parent
}

func readTrashInfo(path string) (Trashed, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Trashed{}, err
	}
	t := Trashed{infoPath: path}
	inGroup := false
	for line := range strings.SplitSeq(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inGroup = line == "[Trash Info]"
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !inGroup || !ok {
			continue
		}
		switch k {
		case "Path":
			if p, err := url.PathUnescape(v); err == nil {
				t.Orig = p
			} else {
				t.Orig = v
			}
		case "DeletionDate":
			t.Deleted, _ = time.ParseInLocation("2006-01-02T15:04:05", v, time.Local)
		}
	}
	if t.Orig == "" {
		return t, errors.New("no Path in " + path)
	}
	return t, nil
}

// ErrExists is returned when putting something where a file already is.
var ErrExists = fs.ErrExist

// Restore puts a trashed item back where it was, making its folder when
// it is gone; it fails with ErrExists when something has taken its place.
func Restore(t Trashed) error {
	if _, err := os.Lstat(t.Orig); err == nil {
		return fmt.Errorf("%s: %w", t.Orig, ErrExists)
	}
	if err := os.MkdirAll(filepath.Dir(t.Orig), 0o755); err != nil {
		return err
	}
	if err := os.Rename(t.Path(), t.Orig); err != nil {
		return err
	}
	return os.Remove(t.info())
}

func (t Trashed) info() string {
	if t.infoPath != "" {
		return t.infoPath
	}
	return filepath.Join(t.Trash, "info", t.Name+".trashinfo")
}

// Purge deletes a trashed item for good.
func Purge(t Trashed) error {
	if err := os.RemoveAll(t.Path()); err != nil {
		return err
	}
	return os.Remove(t.info())
}

// EmptyTrash deletes everything in the trash folders, and the infos left
// without their items.
func EmptyTrash(trashes []string) error {
	var first error
	for _, t := range trashes {
		for _, sub := range []string{"files", "info"} {
			es, err := os.ReadDir(filepath.Join(t, sub))
			if err != nil {
				continue
			}
			for _, e := range es {
				if err := os.RemoveAll(filepath.Join(t, sub, e.Name())); err != nil && first == nil {
					first = err
				}
			}
		}
		os.Remove(filepath.Join(t, "directorysizes"))
	}
	return first
}

// numbered is name with " (n)" before its extension: "notes (2).txt";
// folders keep dots in their names whole.
func numbered(name string, n int, dir bool) string {
	stem, ext := name, ""
	if !dir {
		stem, ext = SplitExt(name)
	}
	return fmt.Sprintf("%s (%d)%s", stem, n, ext)
}

// SplitExt splits a name before its extension, which numbered and copied
// names keep; a leading dot does not start one, and ".tar.gz" and the
// like stay together. Renaming one item selects by RenameSelection.
func SplitExt(name string) (stem, ext string) {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 || i == len(name)-1 {
		return name, ""
	}
	stem, ext = name[:i], name[i:]
	if j := strings.LastIndexByte(stem, '.'); j > 0 && strings.EqualFold(stem[j:], ".tar") {
		stem, ext = stem[:j], stem[j:]+ext
	}
	return stem, ext
}

// TrashEntries are trashed items as a folder's entries: by the names they
// had, dated when they were trashed.
func TrashEntries(items []Trashed) []Entry {
	out := make([]Entry, 0, len(items))
	for _, t := range items {
		e := newEntry(filepath.Base(t.Orig))
		if fi, err := os.Lstat(t.Path()); err == nil {
			fill(&e, t.Path(), fi)
		}
		e.ModTime = t.Deleted
		e.Hidden = false
		out = append(out, e)
	}
	return out
}

// SameDevice says whether two paths are on one filesystem, where moving
// is a rename.
func SameDevice(a, b string) bool {
	sa, err := os.Stat(a)
	if err != nil {
		return false
	}
	sb, err := os.Stat(b)
	return err == nil && devOf(sa) == devOf(sb)
}
