package files

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Kind is what an operation does.
type Kind int

const (
	Copy        Kind = iota // Sources into the folder Dest
	Move                    // Sources into the folder Dest
	Link                    // symbolic links to Sources in the folder Dest
	MoveToTrash             // Sources to the trash
	Delete                  // Sources, for good
	Rename                  // Sources[0] to the path Dest
	Mkdir                   // a folder at Dest
	Create                  // an empty file at Dest
	Untrash                 // the trashed items Trashed back where they were
	Compress                // Sources into the archive Dest (archive.go)
	Extract                 // each archive of Sources taken apart in the folder Dest
	RenameMany              // each of Sources to the path at its place in Dests (rename.go)
	Gather                  // a new folder at Dest, Sources moved into it
)

// Job is an operation on files.
type Job struct {
	Kind    Kind
	Sources []string
	Dest    string
	Dests   []string  // for RenameMany
	Trashed []Trashed // for Untrash
	Archive ArchiveOptions
}

// Progress is how far a job has gone.
type Progress struct {
	Bytes, TotalBytes int64
	Items, TotalItems int
	Current           string // the item at hand
	Started           time.Time
}

// Conflict is a name taken where an item is to go.
type Conflict struct {
	Src, Dst       string
	SrcDir, DstDir bool
}

// Answer is what to do with a conflict.
type Answer int

const (
	Cancel   Answer = iota
	Skip            // leave this one
	Replace         // put the new one in place of the old
	KeepBoth        // give the new one a free name: "name (2).ext"
	Merge           // folders: copy or move the contents in, asking again inside
)

// Asker is asked about each conflict; all says to give the same answer to
// the rest of the job's conflicts of the same kind (file or folder).
type Asker func(Conflict) (a Answer, all bool)

// Undo is what puts a finished job back.
type Undo struct {
	Moves   [][2]string // from, to: moved back
	Renames [][2]string // from, to: renamed back, all at once
	Trashed []Trashed   // restored
	Created []string    // trashed
}

// Empty says whether there is nothing to undo.
func (u Undo) Empty() bool {
	return len(u.Moves) == 0 && len(u.Renames) == 0 && len(u.Trashed) == 0 && len(u.Created) == 0
}

// Job is the job that undoes u. A move is moved back by name, so a
// conflict there (something took the name since) is asked about as usual.
func (u Undo) Job() []Job {
	var out []Job
	for i := len(u.Moves) - 1; i >= 0; i-- {
		m := u.Moves[i]
		out = append(out, Job{Kind: Rename, Sources: []string{m[1]}, Dest: m[0]})
	}
	if len(u.Renames) > 0 {
		back := Job{Kind: RenameMany}
		for _, rn := range u.Renames {
			back.Sources = append(back.Sources, rn[1])
			back.Dests = append(back.Dests, rn[0])
		}
		out = append(out, back)
	}
	if len(u.Trashed) > 0 {
		out = append(out, Job{Kind: Untrash, Trashed: u.Trashed})
	}
	if len(u.Created) > 0 {
		out = append(out, Job{Kind: MoveToTrash, Sources: u.Created})
	}
	return out
}

// ErrCancelled is returned when a job was cancelled.
var ErrCancelled = context.Canceled

// NotMadeError is a Gather job's folder that could not be made: nothing
// was moved.
type NotMadeError struct{ Err error }

func (e *NotMadeError) Error() string { return e.Err.Error() }
func (e *NotMadeError) Unwrap() error { return e.Err }

// ErrIntoItself is returned for copying or moving a folder into itself.
var ErrIntoItself = errors.New("a folder cannot go into itself")

// Runner runs a job, reporting progress and asking about conflicts.
type Runner struct {
	Ask    Asker
	Report func(Progress) // at most every reportEvery, and at the end

	ctx      context.Context
	p        Progress
	last     time.Time
	allFile  Answer // the answer given to all, per kind; Cancel when none
	allDir   Answer
	undo     Undo
	errs     []error
	tmpCount int
}

const reportEvery = 100 * time.Millisecond

// chunk is how much a copy moves between checks for cancelling and
// reports; copy_file_range moves each chunk in the kernel.
const chunk = 16 << 20

// Run runs job until it ends or ctx is cancelled. It returns what undoes
// what was done (also when it stopped early) and the errors met, joined:
// one item failing does not stop the rest.
func (r *Runner) Run(ctx context.Context, job Job) (Undo, error) {
	r.ctx, r.undo, r.errs = ctx, Undo{}, nil
	r.allFile, r.allDir = Cancel, Cancel
	r.p = Progress{Started: time.Now()}
	switch job.Kind {
	case Copy, Move, Gather:
		r.count(job.Sources)
	case Compress:
		// compress counts, as it starts
	case Extract:
		for _, s := range job.Sources {
			if fi, err := os.Stat(s); err == nil {
				r.p.TotalBytes += fi.Size()
			}
		}
		r.p.TotalItems = len(job.Sources)
	default:
		r.p.TotalItems = max(len(job.Sources), len(job.Trashed), 1)
	}
	r.report(true)
	err := r.run(job)
	r.report(true)
	if err != nil {
		r.errs = append(r.errs, err)
	}
	return r.undo, errors.Join(r.errs...)
}

func (r *Runner) run(job Job) error {
	switch job.Kind {
	case Copy, Move, Link:
		for _, src := range job.Sources {
			if err := r.cancelled(); err != nil {
				return err
			}
			src = filepath.Clean(src)
			dst := filepath.Join(job.Dest, filepath.Base(src))
			if job.Kind != Link && (job.Dest == src || strings.HasPrefix(job.Dest, src+"/")) {
				r.fail(fmt.Errorf("%s: %w", src, ErrIntoItself))
				continue
			}
			if job.Kind == Copy && filepath.Dir(src) == filepath.Clean(job.Dest) {
				// Pasted where it is: a copy beside it.
				fi, err := os.Lstat(src)
				if err != nil {
					r.fail(err)
					continue
				}
				dst = CopyName(job.Dest, filepath.Base(src), fi.IsDir())
			} else if job.Kind != Copy && filepath.Dir(src) == filepath.Clean(job.Dest) {
				continue // moved or linked to where it is
			}
			var err error
			switch job.Kind {
			case Copy:
				err = r.copyTop(src, dst)
			case Move:
				err = r.moveTop(src, dst)
			case Link:
				err = r.link(src, dst)
			}
			if err != nil {
				return err
			}
		}
	case MoveToTrash:
		for _, src := range job.Sources {
			if err := r.cancelled(); err != nil {
				return err
			}
			r.p.Current = src
			t, err := Trash(src)
			if err != nil {
				r.fail(err)
				continue
			}
			r.undo.Trashed = append(r.undo.Trashed, t)
			r.step(0)
		}
	case Untrash:
		for _, t := range job.Trashed {
			if err := r.cancelled(); err != nil {
				return err
			}
			r.p.Current = t.Orig
			if err := Restore(t); err != nil {
				r.fail(err)
				continue
			}
			r.undo.Created = append(r.undo.Created, t.Orig)
			r.step(0)
		}
	case Delete:
		for _, src := range job.Sources {
			if err := r.cancelled(); err != nil {
				return err
			}
			r.p.Current = src
			if err := r.removeAll(src); err != nil {
				r.fail(err)
			}
			r.step(0)
		}
	case Rename:
		if len(job.Sources) != 1 {
			return errors.New("rename takes one item")
		}
		src := job.Sources[0]
		r.p.Current = src
		if err := renameNoReplace(src, job.Dest); err != nil {
			return err
		}
		r.undo.Moves = append(r.undo.Moves, [2]string{src, job.Dest})
		r.step(0)
	case RenameMany:
		if err := r.renameMany(job.Sources, job.Dests); err != nil {
			return err
		}
	case Mkdir:
		r.p.Current = job.Dest
		if err := os.Mkdir(job.Dest, 0o755); err != nil {
			return err
		}
		r.undo.Created = append(r.undo.Created, job.Dest)
		r.step(0)
	case Gather:
		// The folder is made first, and only by this job: when it cannot
		// be (the name taken, no permission), nothing moves. It stays when
		// some items could not go in; nothing in it is ever written over.
		if err := os.Mkdir(job.Dest, 0o755); err != nil {
			return &NotMadeError{err}
		}
		r.undo.Created = append(r.undo.Created, job.Dest)
		for _, src := range job.Sources {
			if err := r.cancelled(); err != nil {
				return err
			}
			src = filepath.Clean(src)
			dst := filepath.Join(job.Dest, filepath.Base(src))
			if job.Dest == src || strings.HasPrefix(job.Dest, src+"/") {
				r.fail(fmt.Errorf("%s: %w", src, ErrIntoItself))
				continue
			}
			if _, err := os.Lstat(dst); err == nil {
				// Two sources of one name, or someone else's file: left.
				r.fail(&os.LinkError{Op: "rename", Old: src, New: dst, Err: fs.ErrExist})
				continue
			}
			if err := r.moveTop(src, dst); err != nil {
				return err
			}
		}
	case Compress:
		if err := r.compress(job.Sources, job.Dest, job.Archive); err != nil {
			return err
		}
	case Extract:
		for _, src := range job.Sources {
			if err := r.cancelled(); err != nil {
				return err
			}
			if err := r.extract(src, job.Dest, job.Archive.Password); err != nil {
				if errors.Is(err, ErrCancelled) {
					return err
				}
				r.fail(fmt.Errorf("%s: %w", filepath.Base(src), err))
			}
		}
	case Create:
		r.p.Current = job.Dest
		f, err := os.OpenFile(job.Dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		f.Close()
		r.undo.Created = append(r.undo.Created, job.Dest)
		r.step(0)
	}
	return nil
}

func (r *Runner) fail(err error) { r.errs = append(r.errs, err) }

func (r *Runner) cancelled() error {
	if r.ctx.Err() != nil {
		return ErrCancelled
	}
	return nil
}

func (r *Runner) step(bytes int64) {
	r.p.Items++
	r.p.Bytes += bytes
	r.report(false)
}

func (r *Runner) report(force bool) {
	if r.Report == nil {
		return
	}
	if now := time.Now(); force || now.Sub(r.last) >= reportEvery {
		r.last = now
		r.Report(r.p)
	}
}

// count totals the items and bytes below the sources, for the progress.
func (r *Runner) count(srcs []string) {
	for _, s := range srcs {
		filepath.WalkDir(s, func(_ string, d fs.DirEntry, err error) error {
			if r.ctx.Err() != nil {
				return r.ctx.Err()
			}
			if err != nil {
				return nil
			}
			r.p.TotalItems++
			if d.Type().IsRegular() {
				if fi, err := d.Info(); err == nil {
					r.p.TotalBytes += fi.Size()
				}
			}
			return nil
		})
	}
}

// resolve asks about dst being taken, unless an answer was given for all.
func (r *Runner) resolve(src, dst string) (Answer, error) {
	sfi, err := os.Lstat(src)
	if err != nil {
		return Cancel, err
	}
	dfi, err := os.Lstat(dst)
	if err != nil {
		return Replace, nil // free (the caller still writes without replacing)
	}
	c := Conflict{Src: src, Dst: dst, SrcDir: sfi.IsDir(), DstDir: dfi.IsDir()}
	both := c.SrcDir && c.DstDir
	if both && r.allDir != Cancel {
		return r.allDir, nil
	}
	if !both && r.allFile != Cancel {
		return r.allFile, nil
	}
	if r.Ask == nil {
		return Skip, nil
	}
	a, all := r.Ask(c)
	if a == Merge && !both {
		a = Replace
	}
	if all {
		if both {
			r.allDir = a
		} else {
			r.allFile = a
		}
	}
	if a == Cancel {
		return Cancel, ErrCancelled
	}
	return a, nil
}

// copyTop copies one source and records it for undo.
func (r *Runner) copyTop(src, dst string) error {
	created, err := r.copyAny(src, dst)
	if created != "" {
		r.undo.Created = append(r.undo.Created, created)
	}
	return err
}

// copyAny copies src to dst, which may be taken, and returns the new
// item's path when one was made (not for a merge into a folder that was
// there). Only cancelling is returned as an error; others are recorded.
func (r *Runner) copyAny(src, dst string) (string, error) {
	if err := r.cancelled(); err != nil {
		return "", err
	}
	a, err := r.resolve(src, dst)
	if errors.Is(err, ErrCancelled) {
		return "", err
	}
	if err != nil {
		r.fail(err)
		return "", nil
	}
	sfi, _ := os.Lstat(src)
	_, taken := os.Lstat(dst)
	switch {
	case a == Skip:
		r.skip(src, sfi)
		return "", nil
	case a == KeepBoth && taken == nil:
		dst = FreeName(filepath.Dir(dst), filepath.Base(dst), sfi.IsDir())
	}
	r.p.Current = src
	switch {
	case sfi.IsDir():
		merging := a == Merge && taken == nil
		if !merging {
			if a == Replace && taken == nil {
				if err := r.removeAll(dst); err != nil {
					r.fail(err)
					return "", nil
				}
			}
			if err := os.Mkdir(dst, 0o700); err != nil {
				r.fail(err)
				return "", nil
			}
		}
		r.step(0)
		es, err := os.ReadDir(src)
		if err != nil {
			r.fail(err)
		}
		for _, e := range es {
			if _, err := r.copyAny(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return "", err
			}
		}
		if !merging {
			os.Chmod(dst, sfi.Mode().Perm())
			os.Chtimes(dst, time.Time{}, sfi.ModTime())
			return dst, nil
		}
		return "", nil
	case sfi.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err == nil {
			err = r.place(dst, a == Replace, func(tmp string) error { return os.Symlink(target, tmp) })
		}
		if err != nil {
			r.fail(err)
			return "", nil
		}
		r.step(0)
		return dst, nil
	case sfi.Mode().IsRegular():
		err := r.place(dst, a == Replace, func(tmp string) error { return r.copyFile(src, tmp, sfi) })
		if errors.Is(err, ErrCancelled) {
			return "", err
		}
		if err != nil {
			r.fail(err)
			return "", nil
		}
		r.p.Items++
		r.report(false)
		return dst, nil
	default:
		r.fail(fmt.Errorf("%s: not a regular file, folder or link; left out", src))
		return "", nil
	}
}

// skip counts a skipped item and what is below it as done.
func (r *Runner) skip(src string, fi fs.FileInfo) {
	if fi == nil || !fi.IsDir() {
		if fi != nil && fi.Mode().IsRegular() {
			r.p.Bytes += fi.Size()
		}
		r.step(0)
		return
	}
	filepath.WalkDir(src, func(_ string, d fs.DirEntry, err error) error {
		if err == nil {
			r.p.Items++
			if d.Type().IsRegular() {
				if fi, err := d.Info(); err == nil {
					r.p.Bytes += fi.Size()
				}
			}
		}
		return nil
	})
	r.report(false)
}

// place makes an item at a temporary name beside dst with make, then
// renames it to dst: a failed or cancelled copy leaves no half file.
// Without replace, a dst taken meanwhile is not overwritten.
func (r *Runner) place(dst string, replace bool, make func(tmp string) error) error {
	r.tmpCount++
	tmp := filepath.Join(filepath.Dir(dst), fmt.Sprintf(".%s.inua-%d-%d.part", filepath.Base(dst), os.Getpid(), r.tmpCount))
	if err := make(tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	if replace {
		if fi, err := os.Lstat(dst); err == nil && fi.IsDir() {
			if err := os.RemoveAll(dst); err != nil {
				os.Remove(tmp)
				return err
			}
		}
		err := os.Rename(tmp, dst)
		if err != nil {
			os.Remove(tmp)
		}
		return err
	}
	if err := renameNoReplace(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// copyFile copies a regular file's bytes, mode and times. On Linux each
// chunk is a copy_file_range (a reflink on Btrfs and XFS).
func (r *Runner) copyFile(src, dst string, fi fs.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	for {
		if err := r.cancelled(); err != nil {
			out.Close()
			return err
		}
		n, err := io.CopyN(out, in, chunk)
		r.p.Bytes += n
		r.report(false)
		if err == io.EOF {
			break
		}
		if err != nil {
			out.Close()
			return err
		}
	}
	if err := out.Chmod(fi.Mode().Perm()); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(dst, time.Time{}, fi.ModTime())
}

// moveTop moves one source and records it for undo.
func (r *Runner) moveTop(src, dst string) error {
	if err := r.cancelled(); err != nil {
		return err
	}
	a, err := r.resolve(src, dst)
	if errors.Is(err, ErrCancelled) {
		return err
	}
	if err != nil {
		r.fail(err)
		return nil
	}
	sfi, _ := os.Lstat(src)
	_, taken := os.Lstat(dst)
	switch {
	case a == Skip:
		r.skip(src, sfi)
		return nil
	case a == KeepBoth && taken == nil:
		dst = FreeName(filepath.Dir(dst), filepath.Base(dst), sfi.IsDir())
	case a == Merge && taken == nil:
		// Each child moved in on its own, asked about again; the emptied
		// source folder is removed.
		r.step(0)
		es, err := os.ReadDir(src)
		if err != nil {
			r.fail(err)
			return nil
		}
		for _, e := range es {
			if err := r.moveTop(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		os.Remove(src) // stays when something in it was skipped
		return nil
	case a == Replace && taken == nil:
		if err := r.removeAll(dst); err != nil {
			r.fail(err)
			return nil
		}
	}
	r.p.Current = src
	err = renameNoReplace(src, dst)
	if errors.Is(err, syscall.EXDEV) {
		// Another filesystem: copy, then remove what was copied.
		created, err := r.copyAny(src, dst)
		if err != nil {
			if created != "" {
				r.removeAll(created)
			}
			return err
		}
		if created == "" {
			return nil // failed, recorded
		}
		if err := r.removeAll(src); err != nil {
			r.fail(err)
		}
		r.undo.Moves = append(r.undo.Moves, [2]string{src, dst})
		return nil
	}
	if err != nil {
		r.fail(err)
		return nil
	}
	r.undo.Moves = append(r.undo.Moves, [2]string{src, dst})
	r.skip(dst, sfi) // counts the items moved at once
	return nil
}

func (r *Runner) link(src, dst string) error {
	r.p.Current = src
	if _, err := os.Lstat(dst); err == nil {
		dst = FreeName(filepath.Dir(dst), filepath.Base(dst), false)
	}
	if err := os.Symlink(src, dst); err != nil {
		r.fail(err)
		return nil
	}
	r.undo.Created = append(r.undo.Created, dst)
	r.step(0)
	return nil
}

// removeAll deletes path and what is below it, giving way to cancelling
// between items.
func (r *Runner) removeAll(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if fi.IsDir() {
		es, err := os.ReadDir(path)
		if err != nil && !os.IsPermission(err) {
			return err
		}
		for _, e := range es {
			if err := r.cancelled(); err != nil {
				return err
			}
			if err := r.removeAll(filepath.Join(path, e.Name())); err != nil {
				return err
			}
		}
	}
	return os.Remove(path)
}

// renameNoReplace renames without overwriting what is at to; on a
// filesystem without RENAME_NOREPLACE it checks first.
func renameNoReplace(from, to string) error {
	err := unix.Renameat2(unix.AT_FDCWD, from, unix.AT_FDCWD, to, unix.RENAME_NOREPLACE)
	if err == nil {
		return nil
	}
	if errors.Is(err, unix.EEXIST) {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: fs.ErrExist}
	}
	if !errors.Is(err, unix.EINVAL) && !errors.Is(err, unix.ENOSYS) {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	if _, err := os.Lstat(to); err == nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: fs.ErrExist}
	}
	return os.Rename(from, to)
}

// FreeName is name in dir, numbered when taken: "notes.txt", then
// "notes (2).txt" and on.
func FreeName(dir, name string, isDir bool) string {
	p := filepath.Join(dir, name)
	if _, err := os.Lstat(p); err != nil {
		return p
	}
	stem := name
	if !isDir {
		stem, _ = SplitExt(name)
	}
	// "notes (2).txt" counts on from 2 rather than making "notes (2) (2).txt".
	n := 2
	if i := strings.LastIndex(stem, " ("); i > 0 && strings.HasSuffix(stem, ")") {
		var k int
		if _, err := fmt.Sscanf(stem[i:], " (%d)", &k); err == nil && k > 1 {
			name = stem[:i] + name[len(stem):]
			n = k + 1
		}
	}
	for ; ; n++ {
		p = filepath.Join(dir, numbered(name, n, isDir))
		if _, err := os.Lstat(p); err != nil {
			return p
		}
	}
}

// CopyName is the name for a copy of name made in its own folder:
// "notes (copy).txt", then "notes (copy 2).txt".
func CopyName(dir, name string, isDir bool) string {
	stem, ext := name, ""
	if !isDir {
		stem, ext = SplitExt(name)
	}
	// A copy of a copy counts on: "notes (copy 2)", not "notes (copy) (copy)".
	n := 1
	if i := strings.LastIndex(stem, " (copy"); i > 0 && strings.HasSuffix(stem, ")") {
		label := stem[i+len(" (copy") : len(stem)-1]
		if label == "" {
			stem, n = stem[:i], 2
		} else if k, err := strconv.Atoi(strings.TrimSpace(label)); err == nil && strings.HasPrefix(label, " ") && k > 1 {
			stem, n = stem[:i], k+1
		}
	}
	for ; ; n++ {
		label := "copy"
		if n > 1 {
			label = fmt.Sprintf("copy %d", n)
		}
		p := filepath.Join(dir, fmt.Sprintf("%s (%s)%s", stem, label, ext))
		if _, err := os.Lstat(p); err != nil {
			return p
		}
	}
}
