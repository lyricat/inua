package filesui

import (
	"os"
	"path/filepath"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/i18n"
)

// crumbSeg is a button of the path bar: the folder it stands for, and
// its text or icon.
type crumbSeg struct {
	label, icon, path string
	current           bool // the folder shown
}

// crumbTrail is the path bar's buttons for dir: home as a house when dir
// is in it, else the root as a slash, then one for each folder down to
// dir, which is the current one.
func crumbTrail(dir, home string) []crumbSeg {
	dir, home = filepath.Clean(dir), filepath.Clean(home)
	var segs []crumbSeg
	at, rest := "/", strings.TrimPrefix(dir, "/")
	if dir == home || strings.HasPrefix(dir, home+"/") {
		at, rest = home, strings.TrimPrefix(dir[len(home):], "/")
		segs = append(segs, crumbSeg{icon: "user-home-symbolic", path: home})
	} else {
		segs = append(segs, crumbSeg{label: "/", path: "/"})
	}
	for _, part := range strings.Split(rest, "/") {
		if part == "" {
			continue
		}
		at = filepath.Join(at, part)
		segs = append(segs, crumbSeg{label: part, path: at})
	}
	segs[len(segs)-1].current = true
	return segs
}

// crumbMenu opens the menu of the path bar's button at, which stands for
// target, at x, y in it. The pane stays where it is, its selection too.
func (p *pane) crumbMenu(at *gtk.Widget, target string, x, y float64) {
	p.w.setActive(p)
	var px, py float64
	at.TranslateCoordinates(&p.box.Widget, x, y, &px, &py)
	p.menu.show(func() []menuItem { return p.w.crumbItems(p, target) }, int32(px), int32(py))
}

// focusedCrumbMenu opens the menu of the path bar's button that has the
// keyboard (the menu key), under it, and says whether there was one.
func (p *pane) focusedCrumbMenu() bool {
	f := p.w.win.GetFocus()
	if f == nil {
		return false
	}
	defer f.Unref()
	segs := crumbTrail(p.dir, files.Home())
	if p.mode == inTrash {
		segs = []crumbSeg{{path: trashKey}}
	}
	i := 0
	for c := p.crumbs.GetFirstChild(); c != nil; {
		if c.HasCssClass("files-crumb") {
			if c.GoPointer() == f.GoPointer() && i < len(segs) {
				p.crumbMenu(c, segs[i].path, float64(c.GetWidth())/2, float64(c.GetHeight()))
				c.Unref()
				return true
			}
			i++
		}
		next := c.GetNextSibling()
		c.Unref() // GetFirstChild and GetNextSibling return a reference
		c = next
	}
	return false
}

// crumbItems are the menu of a path bar's button in p: each item acts on
// dir, the button's folder (or the trash), not on the selection or the
// folder p shows. What would remove or rename an ancestor is left to
// that folder's own row.
func (w *window) crumbItems(p *pane, dir string) []menuItem {
	sep := menuItem{}
	newTab := menuItem{label: i18n.T("Open in a new tab"), key: i18n.T("Middle click"), run: func() { p.newTab(dir, false) }}
	if dir == trashKey { // not a folder: what the sidebar's trash offers
		return []menuItem{newTab, sep, {label: i18n.T("Empty Trash"), run: w.confirmEmptyTrash}}
	}
	// Each item runs in p, whichever pane has the keys by then, so what
	// it says shows in p's status line.
	in := func(f func()) func() {
		return func() {
			w.setActive(p)
			f()
		}
	}
	// ready says why dir cannot be used, if it cannot.
	ready := func() bool {
		if msg := folderProblem(dir); msg != "" {
			w.flash(msg)
			return false
		}
		return true
	}
	newName := func(folder bool) func() {
		return in(func() {
			if ready() {
				w.askNewName(dir, folder)
			}
		})
	}
	copyPath := menuItem{label: i18n.T("Copy the folder's path"), run: in(func() { w.copyPath(dir) })}
	if w.pick != nil { // the picker opens one folder at a time and moves nothing
		return []menuItem{{label: i18n.T("New folder…"), run: newName(true)}, copyPath}
	}
	items := []menuItem{
		newTab,
		{label: i18n.T("Open in a new window"), run: func() { w.app.ShowFoldersNow([]string{dir}) }},
	}
	if w.dual {
		other := w.p[1-p.n]
		items = append(items, menuItem{label: i18n.T("Open in the other pane"), run: func() { other.goTo(dir, true) }})
	}
	items = append(items, sep,
		menuItem{label: i18n.T("New folder…"), run: newName(true)},
		menuItem{label: i18n.T("New file…"), run: newName(false)},
		menuItem{label: i18n.T("Paste into the folder"), run: in(func() {
			if ready() {
				w.app.paste(w, dir)
			}
		})},
		sep,
		menuItem{label: i18n.T("Terminal here"), run: in(func() {
			if ready() {
				w.terminal(dir)
			}
		})},
	)
	if dir != "/" { // not the whole system for the assistant to move about
		items = append(items, menuItem{label: i18n.T("Tidy with AI…"), run: in(func() {
			if ready() {
				w.tidy(dir)
			}
		})})
	}
	return append(items, copyPath,
		menuItem{label: w.bookmarkLabel(dir), run: in(func() {
			// A bookmark of a folder that went can still be removed.
			if files.Bookmarked(dir) || ready() {
				w.toggleBookmark(dir)
			}
		})},
	)
}

// folderProblem says why dir is not a folder to work in, or "" when it is.
func folderProblem(dir string) string {
	st, err := os.Stat(dir)
	switch {
	case err == nil && !st.IsDir():
		return i18n.Tf("%s is not a folder.", shortHome(dir))
	case err == nil:
		return ""
	case os.IsNotExist(err):
		return i18n.Tf("%s does not exist.", shortHome(dir))
	case os.IsPermission(err):
		return i18n.Tf("You may not open %s.", shortHome(dir))
	}
	return i18n.Tf("%s cannot be used: %v", shortHome(dir), err)
}
