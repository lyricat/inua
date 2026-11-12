package filesui

import (
	"cmp"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/files"
	"github.com/lyricat/inua/internal/apps/files/disks"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// card is the one card over the window: a question with its buttons, or
// an entry, a list to pick from, a passphrase.
type card struct {
	buttons []cardButton
	def     int // the button Enter presses; -1 for none
	escape  func()
	edited  func(text string)
	// A list to move through with the arrows (jump, connect).
	items  []string
	labels []string
	pick   int
	picked func(item string)
	ptrs   []uintptr
	closed func() // when it closes: what a form forgets
	btns   []*gtk.Button
	busy   bool // its default button's action is under way
}

type cardButton struct {
	label  string
	class  string // suggested-action, destructive-action
	action func()
	aside  bool // at the start, apart from the rest; it leaves the card open
}

// cardParts says which of the card's parts a card shows.
type cardParts struct {
	title, text   string
	entry, secret bool
	entryText     string
	entryHint     string // the entry's placeholder, and the password's
	secretHint    string
	check         string
	checked       bool // the check is on at first
	list          bool
	listHeight    int  // the list's most, when not the usual
	form          bool // the card fills card_form itself
	mono          bool
	// selectTo is how many characters at the start of entryText are
	// selected at first, to be typed over; 0 puts the cursor at its end.
	selectTo int
}

func (w *window) openCard(c *card, parts cardParts) {
	w.closeCard()
	w.card = c
	w.cardTitle.SetText(parts.title)
	w.cardText.SetText(parts.text)
	w.cardText.SetVisible(parts.text != "")
	w.cardForm.SetVisible(parts.form)
	w.cardError.SetText("")
	w.cardError.SetVisible(false)
	w.cardEntry.SetVisible(parts.entry)
	setClass(&w.cardEntry.Widget, "mono-entry", parts.mono)
	w.cardEntry.SetPlaceholderText(parts.entryHint)
	w.cardSecret.SetVisible(parts.secret)
	w.cardSecret.SetText("")
	w.cardSecret.SetPropertyPlaceholderText(parts.secretHint)
	w.cardScroll.SetVisible(parts.list)
	w.cardScroll.SetMaxContentHeight(int32(cmp.Or(parts.listHeight, 340)))
	w.cardCheck.SetVisible(parts.check != "")
	w.cardCheck.SetLabel(parts.check)
	w.cardCheck.SetActive(parts.checked)
	for i, b := range c.buttons {
		btn := gtk.NewButtonWithLabel(b.label)
		btn.AddCssClass("outlined")
		if b.class != "" {
			btn.AddCssClass(b.class)
		}
		action := b.action
		typed := i == c.def && (parts.entry || parts.secret || parts.form)
		w.app.h.click(btn, func() {
			// A card that is typed in: its default button does what Enter
			// does, its action reading what was typed (closing the card
			// first would clear the password) and deciding whether the
			// card closes.
			if (typed || b.aside) && action != nil {
				action()
				return
			}
			w.closeCard()
			if action != nil {
				action()
			}
		})
		c.ptrs = append(c.ptrs, btn.GoPointer())
		c.btns = append(c.btns, btn)
		if b.aside {
			gtkx.Append(&w.cardAside, &btn.Widget)
		} else {
			gtkx.Append(&w.cardButtons, &btn.Widget)
		}
		if i == c.def {
			btn.AddCssClass("default")
		}
	}
	w.cardLayer.SetVisible(true)
	switch {
	case parts.entry:
		w.cardEntry.SetText(parts.entryText)
		w.cardEntry.GrabFocusWithoutSelecting()
		if parts.selectTo > 0 {
			w.cardEntry.SelectRegion(0, int32(parts.selectTo))
		} else {
			w.cardEntry.SetPosition(-1)
		}
	case parts.secret:
		w.cardSecret.GrabFocus()
	default:
		w.cardBox.GrabFocus()
	}
}

func (w *window) closeCard() {
	c := w.card
	if c == nil {
		return
	}
	w.card = nil
	w.app.h.forget(c.ptrs)
	if c.closed != nil {
		c.closed()
	}
	gtkx.Clear(&w.cardForm)
	gtkx.Clear(&w.cardAside)
	gtkx.Clear(&w.cardButtons)
	gtkx.Clear(&w.cardList)
	w.cardLayer.SetVisible(false)
	w.cardSecret.SetText("") // nothing typed stays
	w.cur().focus()
}

// cardKey handles the keys while a card shows: Esc closes it, Enter
// presses its default button, the arrows move through its list.
func (w *window) cardKey(keyval uint32, state gdk.ModifierType) bool {
	c := w.card
	switch keyval {
	case uint32(gdk.KEY_Escape):
		esc := c.escape
		w.closeCard()
		if esc != nil {
			esc()
		}
		return true
	case uint32(gdk.KEY_Return), uint32(gdk.KEY_KP_Enter):
		if w.focusIn(&w.cardEntry.Widget) || w.focusIn(&w.cardSecret.Widget) {
			return false // the entry's activate does it
		}
		w.cardDefault()
		return true
	case uint32(gdk.KEY_Down), uint32(gdk.KEY_Up):
		if len(c.items) == 0 {
			return false
		}
		if keyval == uint32(gdk.KEY_Down) {
			c.pick = min(c.pick+1, len(c.items)-1)
		} else {
			c.pick = max(c.pick-1, 0)
		}
		w.markPick()
		return true
	}
	if state&gdk.ControlMaskValue == 0 && len(c.buttons) > 0 && !w.cardEntry.GetVisible() && !w.cardSecret.GetVisible() && !w.cardForm.GetVisible() {
		// A question's buttons by their first letter: R(eplace), S(kip) …
		r := rune(gdk.KeyvalToUnicode(gdk.KeyvalToLower(keyval)))
		for _, b := range c.buttons {
			first, _ := utf8.DecodeRuneInString(strings.ToLower(b.label))
			if r != 0 && first == r {
				w.closeCard()
				if b.action != nil {
					b.action()
				}
				return true
			}
		}
	}
	return false
}

func (w *window) cardDefault() {
	c := w.card
	if c == nil {
		return
	}
	if len(c.items) > 0 && c.picked != nil {
		item := c.items[c.pick]
		picked := c.picked
		if w.cardEntry.GetVisible() && c.pick == 0 && c.labels[0] == "" {
			item = w.cardEntry.GetText()
		}
		w.closeCard()
		picked(item)
		return
	}
	if c.def >= 0 && c.def < len(c.buttons) && !c.busy {
		action := c.buttons[c.def].action
		// The action may keep the card open (a name taken): it decides.
		if action != nil {
			action()
		}
	}
}

func (w *window) cardEdited() {
	if c := w.card; c != nil && c.edited != nil {
		c.edited(w.cardEntry.GetText())
	}
}

// cardBusy shows the card at work: its default button says what it is
// doing, and it and the card's other actions are not pressed again, by a
// click or Enter, until cardBusy("") gives them back.
func (w *window) cardBusy(doing string) {
	if c := w.card; c != nil {
		w.cardBusyAt(c.def, doing)
	}
}

// cardBusyAt is cardBusy for button i.
func (w *window) cardBusyAt(i int, doing string) {
	c := w.card
	if c == nil || i < 0 || i >= len(c.btns) {
		return
	}
	c.busy = doing != ""
	for j, b := range c.btns {
		if c.buttons[j].action != nil {
			b.SetSensitive(!c.busy)
		}
	}
	if c.busy {
		c.btns[i].SetLabel(doing)
	} else {
		c.btns[i].SetLabel(c.buttons[i].label)
	}
}

func (w *window) cardFail(msg string) {
	w.cardError.SetText(msg)
	w.cardError.SetVisible(msg != "")
}

// setCardList fills the card's list; a click on an item picks it.
func (w *window) setCardList(items, labels, subs []string, icons ...*gio.IconBase) {
	c := w.card
	w.app.h.forget(c.ptrs[len(c.buttons):])
	c.ptrs = c.ptrs[:len(c.buttons)]
	gtkx.Clear(&w.cardList)
	c.items, c.labels, c.pick = items, labels, 0
	for i := range items {
		sub := ""
		if i < len(subs) {
			sub = subs[i]
		}
		b := gtk.NewButton()
		b.AddCssClass("files-card-item")
		t := text(labels[i], sub)
		if i < len(icons) && icons[i] != nil && icons[i].Ptr != 0 {
			row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
			// An app's icon from the desktop's theme, which the window's
			// does not inherit.
			img := gtk.NewImage()
			img.SetPixelSize(24)
			p := w.app.icons.theme.LookupByGicon(icons[i], 24, img.GetScaleFactor(), gtk.TextDirLtrValue, regular)
			img.SetFromPaintable(p)
			p.Unref()
			gtkx.Append(row, &img.Widget)
			t.SetHexpand(true)
			gtkx.Append(row, &t.Widget)
			b.SetChild(&row.Widget)
			row.Unref()
		} else {
			b.SetChild(&t.Widget)
		}
		item := items[i]
		w.app.h.click(b, func() {
			picked := c.picked
			w.closeCard()
			if picked != nil {
				picked(item)
			}
		})
		c.ptrs = append(c.ptrs, b.GoPointer())
		gtkx.Append(&w.cardList, &b.Widget)
	}
	w.cardScroll.SetVisible(len(items) > 0)
	w.markPick()
}

func (w *window) markPick() {
	c := w.card
	i := 0
	for ch := w.cardList.GetFirstChild(); ch != nil; i++ {
		setClass(ch, "selected", i == c.pick)
		next := ch.GetNextSibling()
		ch.Unref()
		ch = next
	}
}

// text is a title over a dim line.
func text(title, sub string) *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	box.SetValign(gtk.AlignCenterValue)
	t := gtk.NewLabel(title)
	t.AddCssClass("title")
	t.SetXalign(0)
	t.SetEllipsize(2)
	gtkx.Append(box, &t.Widget)
	if sub != "" {
		s := gtk.NewLabel(sub)
		s.AddCssClass("sub")
		s.SetXalign(0)
		s.SetEllipsize(2)
		gtkx.Append(box, &s.Widget)
	}
	return box
}

// --- the cards ---

// askRename renames path, the name without its extension selected (a
// folder's whole: when path cannot be read, it is taken for a file).
func (w *window) askRename(p *pane, path string) {
	name := filepath.Base(path)
	fi, err := os.Stat(path) // a link to a folder is renamed as one
	folder := err == nil && fi.IsDir()
	var c *card
	c = &card{def: 1, buttons: []cardButton{
		{label: i18n.T("Cancel")},
		{label: i18n.T("Rename"), class: "suggested-action", action: func() {
			to := strings.TrimSpace(w.cardEntry.GetText())
			if msg := badName(to); msg != "" {
				w.cardFail(msg)
				return
			}
			if to == name {
				w.closeCard()
				return
			}
			dest := filepath.Join(filepath.Dir(path), to)
			if _, err := os.Lstat(dest); err == nil && !strings.EqualFold(to, name) {
				w.cardFail(i18n.Tf("“%s” is taken here.", to))
				return
			}
			w.closeCard()
			w.app.run(w, files.Job{Kind: files.Rename, Sources: []string{path}, Dest: dest})
		}},
	}}
	c.edited = func(t string) { w.cardFail(badName(strings.TrimSpace(t))) }
	w.openCard(c, cardParts{title: i18n.T("Rename"), entry: true, entryText: name,
		selectTo: files.RenameSelection(name, folder)})
}

// badName says what is wrong with a file name, or "".
func badName(n string) string {
	switch {
	case n == "":
		return i18n.T("A name cannot be empty.")
	case n == "." || n == "..":
		return i18n.Tf("“%s” is not a name.", n)
	case strings.Contains(n, "/"):
		return i18n.T("A name cannot hold “/”.")
	case len(n) > 255:
		return i18n.T("The name is too long.")
	}
	return ""
}

// askNewName makes a folder or an empty file in dir.
func (w *window) askNewName(dir string, folder bool) {
	title, base := i18n.T("New file"), i18n.T("Untitled")
	if folder {
		title, base = i18n.T("New folder"), i18n.T("New Folder")
	}
	suggested := filepath.Base(files.FreeName(dir, base, folder))
	var c *card
	c = &card{def: 1, buttons: []cardButton{
		{label: i18n.T("Cancel")},
		{label: i18n.T("Make"), class: "suggested-action", action: func() {
			n := strings.TrimSpace(w.cardEntry.GetText())
			if msg := badName(n); msg != "" {
				w.cardFail(msg)
				return
			}
			dest := filepath.Join(dir, n)
			if _, err := os.Lstat(dest); err == nil {
				w.cardFail(i18n.Tf("“%s” is taken here.", n))
				return
			}
			w.closeCard()
			kind := files.Create
			if folder {
				kind = files.Mkdir
			}
			w.app.run(w, files.Job{Kind: kind, Dest: dest})
		}},
	}}
	c.edited = func(t string) { w.cardFail(badName(strings.TrimSpace(t))) }
	// Made elsewhere than the folder shown (from the path bar): say where.
	where := ""
	if p := w.cur(); p.mode != inFolder || p.dir != dir {
		where = i18n.Tf("In %s", shortHome(dir))
	}
	parts := cardParts{title: title, text: where, entry: true, entryText: suggested}
	if !folder { // a new folder's name has the cursor at its end
		parts.selectTo = files.RenameSelection(suggested, false)
	}
	w.openCard(c, parts)
}

func (w *window) confirmDelete(p *pane) {
	paths := p.selectedPaths()
	var items []files.Trashed
	if p.mode == inTrash {
		for _, i := range p.selected() {
			items = append(items, p.trash[i])
		}
	}
	n := len(paths)
	title := i18n.Nf("Delete %d item for good?", "Delete %d items for good?", n, n)
	if n == 1 {
		title = i18n.Tf("Delete “%s” for good?", p.all[p.selected()[0]].Name)
	}
	w.openCard(&card{def: 0, buttons: []cardButton{
		{label: i18n.T("Cancel")},
		{label: i18n.T("Delete"), class: "destructive-action", action: func() {
			if items != nil {
				go func() {
					for _, t := range items {
						files.Purge(t)
					}
				}()
				return
			}
			w.app.run(w, files.Job{Kind: files.Delete, Sources: paths})
		}},
	}}, cardParts{title: title, text: i18n.T("They do not go to the trash, and this cannot be undone.")})
}

func (w *window) confirmEmptyTrash() {
	w.openCard(&card{def: 0, buttons: []cardButton{
		{label: i18n.T("Cancel")},
		{label: i18n.T("Empty Trash"), class: "destructive-action", action: func() {
			go func() {
				err := files.EmptyTrash(files.Trashes())
				gtkx.Post(func() {
					if err != nil {
						w.flash(err.Error())
					}
					for _, ww := range w.app.wins {
						for _, p := range ww.panes() {
							if p.mode == inTrash {
								p.showTrash(false)
							}
						}
					}
				})
			}()
		}},
	}}, cardParts{title: i18n.T("Empty the trash?"), text: i18n.T("Everything in it is deleted for good, on every drive.")})
}

// askConflict asks what to do with a name taken, and answers on ch.
func (w *window) askConflict(c files.Conflict, ch chan<- answer) {
	if !w.win.GetVisible() {
		w.win.Present() // closed while it ran: back to ask
	}
	name := filepath.Base(c.Dst)
	where := filepath.Base(filepath.Dir(c.Dst))
	reply := func(a files.Answer) func() {
		return func() { ch <- answer{a, w.cardCheck.GetActive()} }
	}
	var text string
	sf, _ := os.Lstat(c.Src)
	df, _ := os.Lstat(c.Dst)
	if sf != nil && df != nil && !c.SrcDir && !c.DstDir {
		text = i18n.Tf("There: %s, %s. This one: %s, %s.", sizeText(df.Size()), dateText(df.ModTime()), sizeText(sf.Size()), dateText(sf.ModTime()))
	}
	buttons := []cardButton{
		{label: i18n.T("Cancel"), action: reply(files.Cancel)},
		{label: i18n.T("Skip"), action: reply(files.Skip)},
		{label: i18n.T("Keep both"), action: reply(files.KeepBoth)},
	}
	def := 3
	if c.SrcDir && c.DstDir {
		buttons = append(buttons, cardButton{label: i18n.T("Merge"), class: "suggested-action", action: reply(files.Merge)})
	} else {
		buttons = append(buttons, cardButton{label: i18n.T("Replace"), class: "destructive-action", action: reply(files.Replace)})
		def = 2
	}
	title := i18n.Tf("“%s” is already in %s", name, where)
	if c.SrcDir && c.DstDir {
		title = i18n.Tf("A folder “%s” is already in %s", name, where)
	}
	w.openCard(&card{def: def, buttons: buttons, escape: reply(files.Cancel)}, cardParts{
		title: title, text: text, check: i18n.T("Do the same for the rest"),
	})
}

// showErrors says what an operation could not do.
func (w *window) showErrors(j files.Job, err error) {
	lines := strings.Split(err.Error(), "\n")
	if len(lines) > 8 {
		more := len(lines) - 8
		lines = append(lines[:8], i18n.Nf("… and %d more", "… and %d more", more, more))
	}
	w.openCard(&card{def: 0, buttons: []cardButton{{label: i18n.T("Close"), class: "suggested-action"}}},
		cardParts{title: i18n.T("Some items could not be done"), text: strings.Join(lines, "\n")})
}

// --- jump: the folders used, by frecency ---

func (w *window) openJump() {
	c := &card{def: -1}
	c.picked = func(dir string) { w.cur().goTo(dir, true) }
	c.edited = func(q string) { w.fillJump(q) }
	w.openCard(c, cardParts{title: i18n.T("Jump to a folder"), entry: true, list: true})
	w.fillJump("")
}

func (w *window) fillJump(q string) {
	dirs := w.app.settings.Jumps.Find(q, time.Now(), 12)
	var items, labels, subs []string
	for _, d := range dirs {
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			w.app.settings.Jumps.Forget(d)
			continue
		}
		items = append(items, d)
		labels = append(labels, filepath.Base(d))
		subs = append(subs, shortHome(d))
	}
	w.setCardList(items, labels, subs)
}

// leave moves panes that are in dir (a drive or share going away) home.
func (w *window) leave(dir string) {
	for _, ww := range w.app.wins {
		for _, p := range ww.p {
			if p.dir == dir || strings.HasPrefix(p.dir, dir+"/") {
				p.goTo(files.Home(), false)
			}
		}
	}
}

// --- drives ---

// openVolume mounts a volume when it is not (asking a locked one's
// passphrase first) and opens it.
func (w *window) openVolume(v disks.Volume) {
	if v.Mounted() {
		w.cur().goTo(v.MountPoints[0], true)
		return
	}
	if v.Encrypted {
		w.askPassphrase(v)
		return
	}
	w.flash(i18n.Tf("Mounting %s…", v.Name()))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute) // polkit may ask
		defer cancel()
		where, err := w.app.disks.Mount(ctx, v)
		gtkx.Post(func() {
			if err != nil {
				w.driveError(v, err)
				return
			}
			w.cur().goTo(where, true)
			w.app.refreshVolumes()
		})
	}()
}

func (w *window) askPassphrase(v disks.Volume) {
	var c *card
	c = &card{def: 1, buttons: []cardButton{
		{label: i18n.T("Cancel")},
		{label: i18n.T("Unlock"), class: "suggested-action", action: func() {
			pass := w.cardSecret.GetText()
			w.cardFail("")
			w.cardBusy(i18n.T("Unlocking…"))
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				clear, err := w.app.disks.Unlock(ctx, v, pass)
				var where string
				if err == nil {
					// The filesystem inside, mounted.
					if vs, verr := w.app.disks.Volumes(ctx); verr == nil {
						for _, cv := range vs {
							if cv.Path == clear {
								where, err = w.app.disks.Mount(ctx, cv)
							}
						}
					}
				}
				gtkx.Post(func() {
					if w.card != c {
						return
					}
					if err != nil {
						w.cardBusy("")
						w.cardFail(i18n.T("That passphrase does not open it."))
						w.cardSecret.SetText("")
						return
					}
					w.closeCard()
					w.app.refreshVolumes()
					if where != "" {
						w.cur().goTo(where, true)
					}
				})
			}()
		}},
	}}
	w.openCard(c, cardParts{
		title:  i18n.Tf("Unlock %s", v.Name()),
		text:   i18n.T("The drive is encrypted. Its passphrase is not kept."),
		secret: true,
	})
}

// eject makes a drive safe to pull out.
func (w *window) eject(v disks.Volume) {
	for _, m := range v.MountPoints {
		w.leave(m)
	}
	w.flash(i18n.Tf("Ejecting %s…", v.Name()))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		err := w.app.disks.Eject(ctx, v)
		gtkx.Post(func() {
			if err != nil {
				w.driveError(v, err)
				return
			}
			if v.Drive.Removable {
				w.flash(i18n.Tf("%s can be removed now.", v.Name()))
			} else {
				w.flash(i18n.Tf("%s is unmounted.", v.Name()))
			}
			w.app.refreshVolumes()
		})
	}()
}

func (w *window) driveError(v disks.Volume, err error) {
	switch {
	case errors.Is(err, disks.ErrBusy):
		w.flash(i18n.Tf("%s is in use: close the files open on it first.", v.Name()))
	case errors.Is(err, disks.ErrNotAuthorized):
		w.flash(i18n.T("Not allowed."))
	default:
		w.flash(i18n.Tf("%s: %v", v.Name(), err))
	}
}

// --- the keys ---

func (w *window) showKeys() {
	groups := []struct {
		title string
		keys  [][2]string
	}{
		{i18n.T("Moving"), [][2]string{
			{"Enter", i18n.T("Open")},
			{"Backspace, Alt+↑", i18n.T("Parent folder")},
			{"Alt+← / Alt+→", i18n.T("Back / forward")},
			{"← / →", i18n.T("In the columns: the folder above, the folder selected")},
			{"Alt+Home", i18n.T("Home")},
			{"Alt+1 … Alt+9", i18n.T("The sidebar's places")},
			{"Ctrl+L", i18n.T("Type a path or a server")},
			{"Ctrl+P", i18n.T("Jump to a folder used before")},
			{"Ctrl+F", i18n.T("Search below this folder")},
			{i18n.T("Letters"), i18n.T("Filter this folder; Esc clears")},
		}},
		{i18n.T("Files"), [][2]string{
			{"Ctrl+C / Ctrl+X / Ctrl+V", i18n.T("Copy / cut / paste")},
			{"Ctrl+Shift+C", i18n.T("Copy the paths (none selected: the folder's)")},
			{"F2", i18n.T("Rename")},
			{"Delete", i18n.T("Move to Trash")},
			{"Shift+Delete", i18n.T("Delete for good")},
			{"Ctrl+Shift+N / Ctrl+Alt+N", i18n.T("New folder / new file")},
			{"Ctrl+Z", i18n.T("Undo")},
			{"F5 / F6", i18n.T("Copy / move to the other pane")},
			{"Shift+Enter", i18n.T("Open with…")},
			{"Ctrl+D", i18n.T("Bookmark")},
			{"Ctrl+Alt+T", i18n.T("Terminal here")},
		}},
		{i18n.T("Window"), [][2]string{
			{"Ctrl+T / Ctrl+W", i18n.T("New tab / close tab")},
			{"Ctrl+Tab, Ctrl+PgDn / PgUp", i18n.T("Next / previous tab")},
			{i18n.T("Middle click"), i18n.T("A folder in a new tab")},
			{"Ctrl+1 / Ctrl+2 / Ctrl+3", i18n.T("List / grid / columns")},
			{"Ctrl+H", i18n.T("Hidden files")},
			{"F3, Tab", i18n.T("Two panes, the other pane")},
			{"F9", i18n.T("Sidebar")},
			{"Space, Ctrl+I", i18n.T("Details")},
			{"Ctrl++ / Ctrl+− / Ctrl+0, Ctrl+wheel", i18n.T("Bigger / smaller / normal")},
			{"Ctrl+N, Ctrl+Q", i18n.T("New window, close it")},
			{"Ctrl+R", i18n.T("Reload")},
		}},
	}
	c := &card{def: 0, buttons: []cardButton{{label: i18n.T("Close"), class: "suggested-action"}}}
	w.openCard(c, cardParts{title: i18n.T("Keys"), list: true, listHeight: 600})
	for _, g := range groups {
		h := gtk.NewLabel(g.title)
		h.AddCssClass("kicker")
		h.AddCssClass("files-keys-head")
		h.SetXalign(0)
		gtkx.Append(&w.cardList, &h.Widget)
		for _, k := range g.keys {
			row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
			row.AddCssClass("files-keys-row")
			key := gtk.NewLabel(k[0])
			key.AddCssClass("readout")
			key.AddCssClass("files-key")
			key.SetXalign(0)
			gtkx.Append(row, &key.Widget)
			what := gtk.NewLabel(k[1])
			what.SetXalign(0)
			what.SetHexpand(true)
			gtkx.Append(row, &what.Widget)
			gtkx.Append(&w.cardList, &row.Widget)
		}
	}
	w.cardScroll.SetVisible(true)
}
