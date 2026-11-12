package files

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Renaming many: a rule gives each item a new name, its extension kept
// (a folder's whole name is its stem); Plan says what each becomes and
// what stops it; RenameMany does them all at once.

// RenameSelection is how many characters at the start of name renaming
// selects to be typed over, as GTK counts them (not bytes): all but the
// last extension and its dot, "archive.tar" of "archive.tar.gz". A
// folder's name is selected whole, and so is a name with no extension:
// one whose last dot leads it (".gitignore") or ends it ("notes."), or
// whose ending holds a space ("Mr. Smith"), which is a sentence's, not a
// file type's.
func RenameSelection(name string, isDir bool) int {
	all := utf8.RuneCountInString(name)
	i := strings.LastIndexByte(name, '.')
	if isDir || i < 0 || i == len(name)-1 {
		return all
	}
	stem, ext := name[:i], name[i+1:]
	if strings.Trim(stem, ".") == "" || strings.IndexFunc(ext, unicode.IsSpace) >= 0 {
		return all
	}
	return utf8.RuneCountInString(stem)
}

// RenameMode is how a rule renames.
type RenameMode int

const (
	ReplaceText RenameMode = iota // Find becomes Replace
	AddText                       // Text before or after the name
	Number                        // Text and a number, in the order given
	ChangeCase                    // lower, UPPER or Title Case
)

// Letter cases.
const (
	Lower = iota
	Upper
	Title
)

// RenameRule is a way to rename many items.
type RenameRule struct {
	Mode RenameMode

	Find, Replace string
	Regexp        bool // Find is a regular expression; Replace may use $1
	MatchCase     bool

	Text  string // added (AddText), or the name before the number (Number)
	After bool   // AddText: at the end rather than the start

	Start int // Number: the first number (1 when 0)

	Case int // ChangeCase
}

// Renamed is an item's new name, and why it cannot have it ("" when it
// can).
type Renamed struct {
	From, To string // paths
	Problem  string
}

// Plan errors, as the preview says them.
var (
	ErrNameTaken   = errors.New("taken by another file")
	ErrNameTwice   = errors.New("given to two items")
	ErrNameEmpty   = errors.New("empty")
	ErrNameSlash   = errors.New("holds “/”")
	ErrNameDots    = errors.New("not a name")
	ErrNameTooLong = errors.New("too long")
)

// Compile checks the rule: a regular expression that is not one.
func (r RenameRule) Compile() (*regexp.Regexp, error) {
	if r.Mode != ReplaceText || r.Find == "" {
		return nil, nil
	}
	pat := r.Find
	if !r.Regexp {
		pat = regexp.QuoteMeta(pat)
	}
	if !r.MatchCase {
		pat = "(?i)" + pat
	}
	return regexp.Compile(pat)
}

// NewName is name renamed by the rule, i its place in the items (from 0)
// of n. Folders keep no extension.
func (r RenameRule) NewName(re *regexp.Regexp, name string, isDir bool, i, n int) string {
	stem, ext := name, ""
	if !isDir {
		stem, ext = SplitExt(name)
	}
	switch r.Mode {
	case ReplaceText:
		if re != nil {
			if r.Regexp {
				stem = re.ReplaceAllString(stem, r.Replace)
			} else {
				stem = re.ReplaceAllLiteralString(stem, r.Replace)
			}
		}
	case AddText:
		if r.After {
			stem += r.Text
		} else {
			stem = r.Text + stem
		}
	case Number:
		start := r.Start
		if start == 0 {
			start = 1
		}
		width := len(strconv.Itoa(start + n - 1)) // 01…12, not 1…12
		num := fmt.Sprintf("%0*d", width, start+i)
		stem = num
		if r.Text != "" {
			stem = r.Text + " " + num
		}
	case ChangeCase:
		switch r.Case {
		case Lower:
			stem = strings.ToLower(stem)
		case Upper:
			stem = strings.ToUpper(stem)
		case Title:
			stem = titleCase(stem)
		}
	}
	return strings.TrimSpace(stem) + ext
}

// titleCase capitalises each word's first letter, lowering the rest.
func titleCase(s string) string {
	var b strings.Builder
	start := true
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if start {
				b.WriteRune(unicode.ToUpper(r))
			} else {
				b.WriteRune(unicode.ToLower(r))
			}
			start = false
		default:
			b.WriteRune(r)
			start = true
		}
	}
	return b.String()
}

// nameProblem says what is wrong with a file name, or nil.
func nameProblem(n string) error {
	switch {
	case n == "":
		return ErrNameEmpty
	case n == "." || n == "..":
		return ErrNameDots
	case strings.Contains(n, "/"):
		return ErrNameSlash
	case len(n) > 255 || !utf8.ValidString(n):
		return ErrNameTooLong
	}
	return nil
}

// Plan renames paths by the rule, in their order: what each becomes and
// what stops it. A name taken by a file outside paths, or given to two,
// is a problem; one taken by another of paths is not, as they all move
// at once.
func Plan(paths []string, rule RenameRule) ([]Renamed, error) {
	re, err := rule.Compile()
	if err != nil {
		return nil, err
	}
	out := make([]Renamed, len(paths))
	moving := map[string]bool{}
	for _, p := range paths {
		moving[p] = true
	}
	seen := map[string]int{}
	for i, p := range paths {
		fi, err := os.Lstat(p)
		isDir := err == nil && fi.IsDir()
		name := rule.NewName(re, filepath.Base(p), isDir, i, len(paths))
		to := filepath.Join(filepath.Dir(p), name)
		out[i] = Renamed{From: p, To: to}
		if perr := nameProblem(name); perr != nil {
			out[i].Problem = perr.Error()
			continue
		}
		if j, ok := seen[to]; ok {
			out[i].Problem = ErrNameTwice.Error()
			if out[j].Problem == "" {
				out[j].Problem = ErrNameTwice.Error()
			}
			continue
		}
		seen[to] = i
		if to != p && !moving[to] {
			if _, err := os.Lstat(to); err == nil && !sameFile(p, to) {
				out[i].Problem = ErrNameTaken.Error()
			}
		}
	}
	return out, nil
}

// sameFile says whether a and b are one file: a name changing case on a
// filesystem that does not mind case.
func sameFile(a, b string) bool {
	fa, err1 := os.Lstat(a)
	fb, err2 := os.Lstat(b)
	return err1 == nil && err2 == nil && os.SameFile(fa, fb)
}

// renameMany renames each Sources[i] to Dests[i]: all to hidden names
// first, then each to its own, so names swapped among them (a to b, b to
// a) go through. One that fails goes back to its old name.
func (r *Runner) renameMany(srcs, dsts []string) error {
	if len(srcs) != len(dsts) {
		return errors.New("rename: as many names as items")
	}
	type step struct{ src, tmp, dst string }
	var steps []step
	for i, src := range srcs {
		if src == dsts[i] {
			continue
		}
		r.tmpCount++
		tmp := filepath.Join(filepath.Dir(src), fmt.Sprintf(".%s.inua-%d-%d.rename", filepath.Base(src), os.Getpid(), r.tmpCount))
		if err := renameNoReplace(src, tmp); err != nil {
			r.fail(err)
			continue
		}
		steps = append(steps, step{src, tmp, dsts[i]})
	}
	for _, s := range steps {
		r.p.Current = s.src
		if err := renameNoReplace(s.tmp, s.dst); err != nil {
			r.fail(err)
			if err := renameNoReplace(s.tmp, s.src); err != nil {
				r.fail(fmt.Errorf("%s is left as %s: %w", filepath.Base(s.src), filepath.Base(s.tmp), err))
			}
			continue
		}
		r.undo.Renames = append(r.undo.Renames, [2]string{s.src, s.dst})
		r.step(0)
	}
	return nil
}
