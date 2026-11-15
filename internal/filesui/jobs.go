package filesui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// jobs are a window's operations: one runs, the rest wait their turn.
type jobs struct {
	running bool
	queue   []queued
	cancel  context.CancelFunc
}

type queued struct {
	job   files.Job
	quiet bool // an undo: not recorded to be undone again
	// done, when there is one, sees the job's error first: true when it
	// took care of it (an archive's password asked for).
	done func(err error) bool
}

// run queues an operation in w.
func (a *App) run(w *window, j files.Job) { w.queue(queued{job: j}) }

// runQuiet queues an operation that is not put on the undo list.
func (a *App) runQuiet(w *window, j files.Job) { w.queue(queued{job: j, quiet: true}) }

func (w *window) queue(q queued) {
	w.jobs.queue = append(w.jobs.queue, q)
	if !w.jobs.running {
		w.nextJob()
	}
}

func (w *window) nextJob() {
	if len(w.jobs.queue) == 0 {
		w.jobs.running = false
		w.jobsReveal.SetRevealChild(false)
		w.app.c.App.Release()
		if w.closing {
			w.win.Close()
		}
		return
	}
	if !w.jobs.running {
		w.app.c.App.Hold() // the operations finish even if the window closes
	}
	q := w.jobs.queue[0]
	w.jobs.queue = w.jobs.queue[1:]
	w.jobs.running = true
	ctx, cancel := context.WithCancel(context.Background())
	w.jobs.cancel = cancel
	job := q.job

	// Quick ones (a rename, a folder made) never show the bar.
	shown := false
	start := time.Now()
	w.jobLabel.SetText(describe(job, files.Progress{}))
	w.jobBar.SetFraction(0)
	w.jobRate.SetText("")
	w.app.h.later(250, func() {
		if w.jobs.running && w.jobs.cancel != nil && !shown {
			shown = true
			w.jobsReveal.SetRevealChild(true)
		}
	})
	r := files.Runner{
		Ask: func(c files.Conflict) (files.Answer, bool) {
			ch := make(chan answer, 1)
			gtkx.Post(func() { w.askConflict(c, ch) })
			got := <-ch
			return got.a, got.all
		},
		Report: func(pr files.Progress) {
			gtkx.Post(func() { w.showProgress(job, pr, start) })
		},
	}
	go func() {
		undo, err := r.Run(ctx, job)
		gtkx.Post(func() {
			cancel()
			w.jobs.cancel = nil
			w.jobDone(q, undo, err)
			w.nextJob()
		})
	}()
}

type answer struct {
	a   files.Answer
	all bool
}

func (w *window) cancelJob() {
	if w.jobs.cancel != nil {
		w.jobs.cancel()
	}
	w.jobs.queue = nil
}

func (w *window) showProgress(job files.Job, pr files.Progress, start time.Time) {
	w.jobLabel.SetText(describe(job, pr))
	switch {
	case pr.TotalBytes > 0:
		w.jobBar.SetFraction(min(1, float64(pr.Bytes)/float64(pr.TotalBytes)))
	case pr.TotalItems > 0:
		w.jobBar.SetFraction(min(1, float64(pr.Items)/float64(pr.TotalItems)))
	}
	elapsed := time.Since(start).Seconds()
	if pr.Bytes > 0 && elapsed > 0.5 {
		rate := float64(pr.Bytes) / elapsed
		text := sizeText(int64(rate)) + "/s"
		if left := float64(pr.TotalBytes-pr.Bytes) / rate; pr.TotalBytes > pr.Bytes && left >= 1 {
			text += " · " + i18n.Tf("%s left", durationText(time.Duration(left*float64(time.Second))))
		}
		w.jobRate.SetText(text)
	}
}

// describe says what a job is doing.
func describe(j files.Job, pr files.Progress) string {
	n := max(len(j.Sources), len(j.Trashed))
	what := ""
	if pr.Current != "" {
		what = filepath.Base(pr.Current)
	}
	count := ""
	if pr.TotalItems > 1 {
		count = fmt.Sprintf(" %d/%d", min(pr.Items+1, pr.TotalItems), pr.TotalItems)
	}
	dest := filepath.Base(j.Dest)
	switch j.Kind {
	case files.Copy:
		return i18n.Tf("Copying %s to %s", cmp.Or(what, i18n.Nf("%d item", "%d items", n, n)), dest) + count
	case files.Move, files.Gather:
		return i18n.Tf("Moving %s to %s", cmp.Or(what, i18n.Nf("%d item", "%d items", n, n)), dest) + count
	case files.MoveToTrash:
		return i18n.Tf("Moving %s to the trash", cmp.Or(what, i18n.Nf("%d item", "%d items", n, n))) + count
	case files.Delete:
		return i18n.Tf("Deleting %s", cmp.Or(what, i18n.Nf("%d item", "%d items", n, n))) + count
	case files.Untrash:
		return i18n.Tf("Restoring %s", cmp.Or(what, i18n.Nf("%d item", "%d items", n, n))) + count
	case files.Link:
		return i18n.Tf("Linking %s", cmp.Or(what, i18n.Nf("%d item", "%d items", n, n)))
	case files.Compress:
		return i18n.Tf("Compressing into %s", dest)
	case files.Extract:
		return i18n.Tf("Extracting %s", cmp.Or(what, i18n.Nf("%d archive", "%d archives", n, n))) + count
	}
	return what
}

func durationText(d time.Duration) string {
	switch {
	case d < time.Minute:
		return i18n.Tf("%d s", int(d.Seconds()))
	case d < time.Hour:
		return i18n.Tf("%d min", int(d.Minutes()+0.5))
	}
	return i18n.Tf("%d h %d min", int(d.Hours()), int(d.Minutes())%60)
}

// jobDone records what the job did for undo, selects what it made in
// the pane that shows where it went, and says what went wrong.
func (w *window) jobDone(q queued, undo files.Undo, err error) {
	j := q.job
	if !q.quiet {
		w.app.pushUndo(undo)
	}
	// What was made, by name, in the folders the panes show.
	var made []string
	for _, c := range undo.Created {
		made = append(made, c)
	}
	for _, m := range undo.Moves {
		made = append(made, m[1])
	}
	for _, m := range undo.Renames {
		made = append(made, m[1])
	}
	if len(made) > 0 {
		for _, p := range w.panes() {
			var names []string
			for _, m := range made {
				if filepath.Dir(m) == p.dir && p.mode == inFolder {
					names = append(names, filepath.Base(m))
				}
			}
			if len(names) > 0 {
				p.reloadSelecting(names)
			}
		}
	}
	if j.Kind == files.MoveToTrash && len(undo.Trashed) > 0 && err == nil {
		w.flash(i18n.Nf("Moved %d item to the trash (Ctrl+Z puts it back).", "Moved %d items to the trash (Ctrl+Z puts them back).", len(undo.Trashed), len(undo.Trashed)))
	}
	if err == nil || errors.Is(err, files.ErrCancelled) && !strings.Contains(err.Error(), "\n") {
		return
	}
	if q.done != nil && q.done(err) {
		return
	}
	w.showErrors(j, err)
}

// reloadSelecting reads the folder again and selects names in it.
func (p *pane) reloadSelecting(names []string) {
	gen := p.gen
	dir, order := p.dir, p.fv.Order
	go func() {
		es, err := files.ReadDir(dir)
		files.Sort(es, order)
		gtkx.Post(func() {
			if gen == p.gen {
				p.loaded(es, err, names, true)
				p.focus()
			}
		})
	}()
}
