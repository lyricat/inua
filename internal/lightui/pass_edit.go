package lightui

import (
	"crypto/rand"
	"strconv"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/pass"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/uikit"
)

// The editor (docs/apps.md, Pass): a row for each line of the entry,
// the password's first, with a button that makes a new one; or, the other
// way, the whole text. Both show one draft (pass.Draft), the lines as
// written, so going from one to the other, and saving, keeps every byte
// that was not edited.

// The editor's two ways.
const (
	editRows = "rows"
	editText = "text"
)

// passEdit is the editor's widgets and its draft.
type passEdit struct {
	draft pass.Draft
	mode  string // editRows or editText

	rowsMode, textMode gtk.ToggleButton
	stack              gtk.Stack
	password           gtk.Entry
	reveal, generate   gtk.Button
	addLine            gtk.Button
	lines              gtk.Box
	// The entries of the lines after the password: lineEntries[i] is the
	// draft's line i+1.
	lineEntries []*gtk.Entry
	shown       bool // the password shows, not its dots
	filling     bool // the rows are being filled, not typed in

	onMode       func(gtk.ToggleButton)
	onChanged    func(uintptr, uintptr)
	onActivate   func(gtk.Entry)
	onMenuClosed func(gtk.Popover)
}

// editWidgets are the editor's widgets in pass.ui, for build to get.
func (a *passApp) editWidgets() map[string]gobject.Ptr {
	e := &a.ed
	return map[string]gobject.Ptr{
		"edit_rows": &e.rowsMode, "edit_text": &e.textMode, "edit_mode": &e.stack,
		"edit_password": &e.password, "edit_reveal": &e.reveal, "edit_generate": &e.generate,
		"edit_add_line": &e.addLine, "edit_lines": &e.lines,
	}
}

// bindEdit connects the editor, once its widgets are got.
func (a *passApp) bindEdit() {
	e := &a.ed
	e.onMode = func(b gtk.ToggleButton) {
		if e.filling || !b.GetActive() {
			return // the button let go of, or set from here
		}
		if b.GoPointer() == e.textMode.GoPointer() {
			a.editMode(editText)
		} else {
			a.editMode(editRows)
		}
	}
	e.onChanged = func(w, _ uintptr) {
		if e.filling {
			return
		}
		if i := a.lineOf(w); i >= 0 {
			a.setLine(i)
		}
	}
	e.onActivate = func(w gtk.Entry) {
		// Enter: a new line after this one.
		if i := a.lineOf(w.GoPointer()); i >= 0 {
			e.draft.Insert(i)
			gtkx.Post(func() { a.fillRows(i + 1) }) // after the entry's signal
		}
	}
	e.onMenuClosed = func(pop gtk.Popover) {
		gtkx.Post(func() { pop.Unparent() })
	}
	e.rowsMode.ConnectToggled(&e.onMode)
	e.textMode.ConnectToggled(&e.onMode)
	gtkx.ConnectChanged(&e.password, &e.onChanged)
	e.password.ConnectActivate(&e.onActivate)
	for _, b := range []*gtk.Button{&e.reveal, &e.generate, &e.addLine} {
		b.ConnectClicked(&a.onButton)
	}
	e.mode = editRows
}

// editButton answers the editor's buttons; false for another button.
func (a *passApp) editButton(name string) bool {
	e := &a.ed
	kind, arg, _ := strings.Cut(name, ":")
	switch kind {
	case "edit-reveal":
		e.shown = !e.shown
		a.showPassword()
	case "edit-generate":
		a.askGenerate()
	case "edit-add-line":
		last := len(e.draft.Lines) - 1
		e.draft.Insert(last)
		a.fillRows(last + 1)
	case "edit-remove-line":
		i, err := strconv.Atoi(arg)
		if err != nil || i < 1 || i >= len(e.draft.Lines) {
			return true
		}
		e.draft.Remove(i)
		a.fillRows(min(i, len(e.draft.Lines)-1))
	default:
		return false
	}
	return true
}

// lineOf is the draft's line an entry of the rows holds; -1 for none.
func (a *passApp) lineOf(w uintptr) int {
	e := &a.ed
	if w == e.password.GoPointer() {
		return 0
	}
	for i, l := range e.lineEntries {
		if l.GoPointer() == w {
			return i + 1
		}
	}
	return -1
}

// setLine puts what a row holds in the draft. A paste of several lines
// becomes as many rows, made once the paste is done.
func (a *passApp) setLine(i int) {
	e := &a.ed
	var text string
	if i == 0 {
		text = e.password.GetText()
	} else {
		text = e.lineEntries[i-1].GetText()
	}
	n := len(e.draft.Lines)
	e.draft.Set(i, text)
	if added := len(e.draft.Lines) - n; added > 0 {
		gtkx.Post(func() { a.fillRows(i + added) })
	}
}

// openEditor opens the editor on a text, in its rows.
func (a *passApp) openEditor(name, content string) {
	e := &a.ed
	a.editTitle.SetText(name)
	a.editError.SetVisible(false)
	e.draft = pass.NewDraft(content)
	e.shown = false
	a.showPassword()
	a.fillText()
	a.setEditMode(editRows)
	a.fillRows(0)
	a.detail.SetVisibleChildName("edit")
}

// clearEditor forgets the draft and empties both ways of editing it.
func (a *passApp) clearEditor() {
	e := &a.ed
	e.draft = pass.Draft{}
	e.filling = true
	e.password.SetText("")
	e.filling = false
	gtkx.Clear(&e.lines)
	e.lineEntries = nil
	buf := a.editor.GetBuffer()
	buf.SetText("", 0)
	buf.SetModified(false)
}

func (a *passApp) closeEditor() {
	a.clearEditor()
	if a.editNew {
		a.editNew = false
		a.showEmpty(i18n.T("Choose an entry."))
		return
	}
	a.detail.SetVisibleChildName("entry")
}

func (a *passApp) saveEditor() {
	if a.busy {
		return
	}
	content := a.editorText()
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	name := a.selected
	if a.editNew {
		name = a.newName
	}
	a.busy = true
	s := a.store
	go func() {
		err := s.Insert(name, content)
		gtkx.Post(func() {
			a.busy = false
			if err != nil {
				a.editError.SetText(err.Error())
				a.editError.SetVisible(true)
				return
			}
			a.editNew = false
			a.clearEditor()
			a.reload(name)
		})
	}()
}

// editorText is the draft's text, with what was typed in the text
// editor.
func (a *passApp) editorText() string {
	a.readText()
	return a.ed.draft.Text()
}

// setEditorText replaces the draft (a one-time code added) and shows it
// both ways.
func (a *passApp) setEditorText(text string) {
	a.ed.draft = pass.NewDraft(text)
	a.fillText()
	a.fillRows(-1)
}

// editMode turns the editor to its rows or its text, with the draft as
// the other way left it.
func (a *passApp) editMode(mode string) {
	e := &a.ed
	if mode == e.mode {
		return
	}
	if mode == editRows {
		a.readText()
		a.fillRows(0)
	} else {
		a.fillText()
	}
	a.setEditMode(mode)
	a.focusEditor(-1)
}

// setEditMode shows a way of editing, and its button pressed.
func (a *passApp) setEditMode(mode string) {
	e := &a.ed
	e.mode = mode
	e.filling = true
	e.rowsMode.SetActive(mode == editRows)
	e.textMode.SetActive(mode == editText)
	e.filling = false
	e.stack.SetVisibleChildName(mode)
}

// readText takes what was typed in the text editor into the draft. A
// text left as it was filled stays the draft it was made from.
func (a *passApp) readText() {
	buf := a.editor.GetBuffer()
	if !buf.GetModified() {
		return
	}
	var start, end gtk.TextIter
	buf.GetBounds(&start, &end)
	a.ed.draft = pass.NewDraft(buf.GetText(&start, &end, true))
	buf.SetModified(false)
}

// fillText puts the draft in the text editor.
func (a *passApp) fillText() {
	buf := a.editor.GetBuffer()
	buf.SetText(a.ed.draft.Text(), -1)
	buf.SetModified(false)
}

// fillRows makes a row for each line of the draft and focuses line
// focus's (none for -1).
func (a *passApp) fillRows(focus int) {
	e := &a.ed
	if len(e.draft.Lines) == 0 {
		e.draft = pass.NewDraft("")
	}
	e.filling = true
	if e.password.GetText() != e.draft.Password() {
		e.password.SetText(e.draft.Password())
	}
	gtkx.Clear(&e.lines)
	e.lineEntries = e.lineEntries[:0]
	for i, l := range e.draft.Lines[1:] {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
		row.AddCssClass("pass-edit-line-row")
		entry := gtk.NewEntry()
		entry.AddCssClass("pass-edit-line")
		entry.SetHexpand(true)
		entry.SetPlaceholderText(i18n.T("Blank line"))
		entry.SetText(l.Text)
		gtkx.ConnectChanged(entry, &e.onChanged)
		entry.ConnectActivate(&e.onActivate)
		gtkx.Append(row, &entry.Widget)
		e.lineEntries = append(e.lineEntries, entry)
		gtkx.Append(row, &plainIcon("edit-remove-line:"+strconv.Itoa(i+1), "list-remove-symbolic", i18n.T("Remove the line"), &a.onButton).Widget)
		gtkx.Append(&e.lines, &row.Widget)
	}
	e.filling = false
	if e.mode == editRows {
		a.focusEditor(focus)
	}
}

// focusEditor puts the cursor at the end of line i's row, or in the
// text; -1 for where it opens: the password, the text's start.
func (a *passApp) focusEditor(i int) {
	e := &a.ed
	if e.mode == editText {
		a.editor.GrabFocus()
		return
	}
	w := &e.password
	if i > 0 && i <= len(e.lineEntries) {
		w = e.lineEntries[i-1]
	}
	w.GrabFocusWithoutSelecting()
	w.SetPosition(-1)
}

// showPassword shows the password's row as its dots or its characters.
func (a *passApp) showPassword() {
	e := &a.ed
	e.password.SetVisibility(e.shown)
	if e.shown {
		e.reveal.SetIconName("view-conceal-symbolic")
		e.reveal.SetTooltipText(i18n.T("Hide"))
	} else {
		e.reveal.SetIconName("view-reveal-symbolic")
		e.reveal.SetTooltipText(i18n.T("Show"))
	}
}

// askGenerate makes a new password; over one already written, once a
// menu under the button is answered.
func (a *passApp) askGenerate() {
	e := &a.ed
	if e.draft.Password() == "" {
		a.generatePassword()
		return
	}
	pop := uikit.NewMenu([]uikit.Item{
		{Label: i18n.T("Replace the password with a new one"), Run: a.generatePassword},
	}, uikit.Below)
	pop.SetParent(&e.generate.Widget)
	pop.ConnectClosed(&e.onMenuClosed)
	pop.Unref() // the button holds it
	pop.Popup()
}

// generatePassword puts a new password, from crypto/rand, in the first
// line, the other lines as they are; if none can be made, says so under
// the editor, the draft as it was.
func (a *passApp) generatePassword() {
	e := &a.ed
	p, err := pass.NewPassword(rand.Reader)
	if err != nil {
		a.editError.SetText(err.Error())
		a.editError.SetVisible(true)
		return
	}
	a.editError.SetVisible(false)
	e.draft.Set(0, p)
	e.filling = true
	e.password.SetText(p)
	e.filling = false
	a.focusEditor(0)
}
