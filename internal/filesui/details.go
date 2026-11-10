package filesui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// showDetails fills the details panel for the pane with the keys: its
// selection, or the folder itself when nothing is selected. The facts
// that take the disk (a stat, a text's first lines, a folder's size) are
// read on a goroutine; what it finds is dropped once the details show
// something else.
func (w *window) showDetails() {
	if !w.detailsReveal.GetRevealChild() {
		return
	}
	gen := w.detGen.Add(1)
	stale := func() bool { return gen != w.detGen.Load() }
	p := w.cur()
	sel := p.selected()
	gtkx.Clear(&w.details)

	switch {
	case len(sel) > 1:
		var size int64
		dirs := 0
		for _, i := range sel {
			if p.all[i].Dir {
				dirs++
			} else {
				size += p.all[i].Size
			}
		}
		w.detailIcon("edit-copy")
		w.detailTitle(i18n.Nf("%d item", "%d items", len(sel), len(sel)))
		w.detailFact(i18n.T("FOLDERS"), strconv.Itoa(dirs))
		w.detailFact(i18n.T("FILES"), strconv.Itoa(len(sel)-dirs))
		w.detailFact(i18n.T("SIZE OF THE FILES"), sizeText(size))
		return
	case len(sel) == 0 && p.mode != inFolder:
		w.detailTitle(map[mode]string{inTrash: i18n.T("Trash"), inSearch: i18n.T("Search")}[p.mode])
		w.detailFact(i18n.T("ITEMS"), strconv.Itoa(len(p.shown)))
		return
	}

	var path string
	var e files.Entry
	if len(sel) == 1 {
		e, path = p.all[sel[0]], p.path(sel[0])
	} else {
		path = p.dir
		e, _ = files.Stat(path)
	}
	k := w.app.kinds.of(&e)
	tex := w.app.thumbs.get(path, &e, p)
	if tex != nil {
		pic := gtk.NewPicture()
		pic.SetPaintable(tex)
		pic.SetCanShrink(true)
		pic.SetContentFit(gtk.ContentFitContainValue)
		pic.AddCssClass("files-preview")
		pic.SetSizeRequest(-1, 200)
		gtkx.Append(&w.details, &pic.Widget)
	} else {
		img := w.detailIcon("")
		if e.Dir {
			w.app.icons.set(img, w.app.folderIcon(path), 72)
		} else {
			w.app.icons.setKind(img, k, 72)
		}
	}
	w.detailTitle(e.Name)
	if p.mode == inTrash && len(sel) == 1 {
		t := p.trash[sel[0]]
		w.detailFact(i18n.T("FROM"), shortHome(filepath.Dir(t.Orig)))
		w.detailFact(i18n.T("TRASHED"), dateText(t.Deleted))
	}
	w.detailFact(i18n.T("KIND"), k.desc)
	if e.Link != "" {
		w.detailFact(i18n.T("LINKS TO"), e.Link)
	}
	sizeLabel := w.detailFact(i18n.T("SIZE"), "…")
	if !e.Dir {
		sizeLabel.SetText(sizeText(e.Size) + " (" + i18n.Nf("%s byte", "%s bytes", int(e.Size), groupDigits(e.Size)) + ")")
	}
	w.detailFact(i18n.T("MODIFIED"), longDate(e.ModTime))
	extra := w.detailBox()
	where := w.detailFact(i18n.T("WHERE"), shortHome(filepath.Dir(path)))
	where.SetSelectable(true)
	var preview, note *gtk.Label
	if !e.Dir && !e.Broken && (e.Mode.IsRegular() || e.Link != "") &&
		(gio.ContentTypeIsA(k.mime, "text/plain") || files.TextExt(e.Ext())) {
		preview = gtk.NewLabel("")
		preview.AddCssClass("files-text-preview")
		preview.SetXalign(0)
		preview.SetYalign(0)
		preview.SetWrap(true)
		preview.SetWrapMode(2)
		preview.SetSelectable(true)
		preview.SetVisible(false)
		gtkx.Append(&w.details, &preview.Widget)
		note = gtk.NewLabel("")
		note.AddCssClass("files-preview-note")
		note.SetXalign(0)
		note.SetWrap(true)
		note.SetWrapMode(2)
		note.SetVisible(false)
		gtkx.Append(&w.details, &note.Widget)
	}

	go func() {
		var facts [][2]string
		fi, err := os.Lstat(path)
		if err == nil {
			atime, ctime := files.Times(fi)
			facts = append(facts, [2]string{i18n.T("OPENED"), longDate(atime)}, [2]string{i18n.T("CHANGED"), longDate(ctime)})
			facts = append(facts, [2]string{i18n.T("PERMISSIONS"), permText(fi.Mode())})
			if uid, gid, ok := files.Owner(fi); ok {
				facts = append(facts, [2]string{i18n.T("OWNER"), ownerText(uid, gid)})
			}
		}
		size := ""
		if e.Dir {
			size = folderSize(path, stale)
		}
		var text files.TextPreview
		var textErr error
		if preview != nil && !stale() {
			text, textErr = files.ReadTextPreview(path)
		}
		gtkx.Post(func() {
			if stale() {
				return
			}
			if e.Dir {
				sizeLabel.SetText(size)
			}
			for _, f := range facts {
				w.detailFactIn(extra, f[0], f[1])
			}
			if preview != nil {
				showTextPreview(preview, note, text, textErr)
			}
		})
	}()
}

// showTextPreview shows the start of a text file, as plain text (never
// markup), with a note under it when it was cut, or the note alone saying
// why there is no text.
func showTextPreview(preview, note *gtk.Label, p files.TextPreview, err error) {
	msg := ""
	switch {
	case errors.Is(err, files.ErrNotText):
		msg = i18n.T("No preview: this is not UTF-8 text.")
	case errors.Is(err, files.ErrNotRegular):
		msg = i18n.T("No preview: this is not a regular file.")
	case errors.Is(err, fs.ErrNotExist):
		msg = i18n.T("The file is no longer there.")
	case errors.Is(err, fs.ErrPermission):
		msg = i18n.T("No permission to read the file.")
	case err != nil:
		var pe *fs.PathError
		if errors.As(err, &pe) {
			err = pe.Err
		}
		msg = i18n.Tf("Could not read the file: %v", err)
	case p.Text == "":
		msg = i18n.T("The file is empty.")
	case p.Cut == files.LineLimit:
		msg = i18n.Nf("Only the first %d line is shown.", "Only the first %d lines are shown.", files.PreviewLines, files.PreviewLines)
	case p.Cut == files.ByteLimit:
		msg = i18n.Tf("Only the first %d KiB are shown.", files.PreviewBytes>>10)
	}
	if err == nil && p.Text != "" {
		text := strings.ReplaceAll(p.Text, "\r\n", "\n")
		preview.SetText(strings.TrimSuffix(text, "\n"))
		preview.SetVisible(true)
	}
	if msg != "" {
		if err != nil && !errors.Is(err, files.ErrNotText) && !errors.Is(err, files.ErrNotRegular) {
			note.AddCssClass("error")
			note.SetSelectable(true)
		}
		note.SetText(msg)
		note.SetVisible(true)
	}
}

// detailIcon adds the panel's big icon, named or set later, and returns
// it.
func (w *window) detailIcon(icon string) *gtk.Image {
	img := gtk.NewImageFromIconName(icon)
	img.SetPixelSize(72)
	img.AddCssClass("files-details-icon")
	gtkx.Append(&w.details, &img.Widget)
	return img
}

func (w *window) detailTitle(t string) {
	l := gtk.NewLabel(t)
	l.AddCssClass("files-details-name")
	l.SetXalign(0)
	l.SetWrap(true)
	l.SetWrapMode(2)
	l.SetSelectable(true)
	gtkx.Append(&w.details, &l.Widget)
}

func (w *window) detailBox() *gtk.Box {
	b := gtk.NewBox(gtk.OrientationVerticalValue, 12)
	gtkx.Append(&w.details, &b.Widget)
	return b
}

// detailFact adds a fact: its name in small capitals over its value; the
// value's label is returned for filling later.
func (w *window) detailFact(name, value string) *gtk.Label {
	return w.detailFactIn(&w.details, name, value)
}

func (w *window) detailFactIn(box *gtk.Box, name, value string) *gtk.Label {
	b := gtk.NewBox(gtk.OrientationVerticalValue, 3)
	k := gtk.NewLabel(name)
	k.AddCssClass("kicker")
	k.SetXalign(0)
	gtkx.Append(b, &k.Widget)
	v := gtk.NewLabel(value)
	v.AddCssClass("files-details-value")
	v.SetXalign(0)
	v.SetWrap(true)
	v.SetWrapMode(2)
	gtkx.Append(b, &v.Widget)
	gtkx.Append(box, &b.Widget)
	return v
}

func longDate(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return i18n.Time(t, "Jan 2 2006 15:04")
}

func permText(m fs.FileMode) string {
	return fmt.Sprintf("%s  %04o", m.String(), m.Perm())
}

func ownerText(uid, gid uint32) string {
	u := strconv.Itoa(int(uid))
	if x, err := user.LookupId(u); err == nil {
		u = x.Username
	}
	g := strconv.Itoa(int(gid))
	if x, err := user.LookupGroupId(g); err == nil {
		g = x.Name
	}
	return u + " · " + g
}

func groupDigits(n int64) string {
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// folderSize adds up what is below dir, giving up when stop says so or
// after 200 000 items.
func folderSize(dir string, stop func() bool) string {
	var size int64
	var n int
	partial := false
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if n++; n > 200000 || n%1000 == 0 && stop() {
			partial = true
			return fs.SkipAll
		}
		if d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil {
				size += fi.Size()
			}
		}
		return nil
	})
	s := sizeText(size) + " · " + i18n.Nf("%d item", "%d items", n-1, n-1)
	if partial {
		s = i18n.Tf("more than %s", s)
	}
	return s
}
