package lightui

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/calc"
	"github.com/lyricat/inua/internal/apps/store"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/uikit"
)

// calcApp keeps both fields editable. Formula edits refresh the preview;
// value edits become the expression without rewriting the field being edited.
// After =, typing at the formula's end starts anew or continues from the answer.
type calcApp struct {
	expr, result     gtk.Entry
	error            gtk.Label
	active           *gtk.Entry
	syncing          bool
	menu             *gtk.Popover
	paste            *calcPaste
	onChanged        func(uintptr, uintptr)
	onFocus          func(gtk.EventControllerFocus)
	onMenuPress      func(gtk.GestureClick, int32, float64, float64)
	sci, deg         gtk.ToggleButton
	scikeys, keys    gtk.Grid
	history          gtk.ToggleButton
	historyReveal    gtk.Revealer
	historyList      gtk.Box
	historyClear     gtk.Button
	text             string
	answered         bool
	answer           string
	past             []calc.Entry // newest first
	onKey            func(gtk.Button)
	onRecall         func(gtk.Button)
	onClearHistory   func(gtk.Button)
	onKeyPress       func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onSci, onDegrees func(gtk.ToggleButton)
	onHistory        func(gtk.ToggleButton)
	onClipText       gio.AsyncReadyCallback
}

// calcHistory is the history's file in the apps' data.
const calcHistory = "calc-history"

// The keys: label (or a Phosphor icon from ui/icons, with the label as its
// tooltip), input, style. Inputs starting with "op:" are commands.
var calcKeys = [][]struct{ label, input, style, icon string }{
	{{"AC", "op:clear", "fn", ""}, {i18n.M("Delete"), "op:back", "fn", "calc-backspace-symbolic"}, {"%", "%", "fn", ""}, {"÷", "÷", "op", ""}},
	{{"7", "7", "", ""}, {"8", "8", "", ""}, {"9", "9", "", ""}, {"×", "×", "op", ""}},
	{{"4", "4", "", ""}, {"5", "5", "", ""}, {"6", "6", "", ""}, {"−", "−", "op", ""}},
	{{"1", "1", "", ""}, {"2", "2", "", ""}, {"3", "3", "", ""}, {"+", "+", "op", ""}},
	{{i18n.M("Change sign"), "op:negate", "", "calc-plus-minus-symbolic"}, {"0", "0", "", ""}, {".", ".", "", ""}, {"=", "op:equals", "eq", ""}},
}

var calcSciKeys = [][]struct{ label, input string }{
	{{"(", "("}, {")", ")"}, {"xʸ", "^"}},
	{{"sin", "sin("}, {"cos", "cos("}, {"tan", "tan("}},
	{{"ln", "ln("}, {"log", "log("}, {"√", "√("}},
	{{"x²", "^2"}, {"π", "π"}, {"e", "e"}},
	{{"sin⁻¹", "asin("}, {"cos⁻¹", "acos("}, {"tan⁻¹", "atan("}},
}

func (a *calcApp) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	win := &gtk.ApplicationWindow{}
	ui := c.Load("calculator.ui")
	ui.Get("calculator", win)
	ui.Get("expression", &a.expr)
	ui.Get("result", &a.result)
	ui.Get("error", &a.error)
	ui.Get("sci", &a.sci)
	ui.Get("deg", &a.deg)
	ui.Get("scikeys", &a.scikeys)
	ui.Get("keys", &a.keys)
	ui.Get("history", &a.history)
	ui.Get("history_reveal", &a.historyReveal)
	ui.Get("history_list", &a.historyList)
	ui.Get("history_clear", &a.historyClear)
	ui.Close()
	store.Load(calcHistory, &a.past)
	a.onRecall = func(b gtk.Button) {
		i, err := strconv.Atoi(b.GetName())
		if err != nil || i >= len(a.past) {
			return
		}
		a.text = calc.Recall(a.text, a.past[i].Result, a.answered)
		a.answered = false
		a.show(nil)
		a.active = &a.expr
		a.expr.GrabFocusWithoutSelecting()
		a.expr.SetPosition(-1)
	}
	a.onClearHistory = func(gtk.Button) {
		a.past = nil
		store.Save(calcHistory, a.past)
		a.fillHistory()
	}
	a.historyClear.ConnectClicked(&a.onClearHistory)
	a.onHistory = func(b gtk.ToggleButton) { a.historyReveal.SetRevealChild(b.GetActive()) }
	a.history.ConnectToggled(&a.onHistory)
	a.history.SetCanFocus(false)
	a.historyClear.SetCanFocus(false)
	a.fillHistory()

	a.onKey = func(b gtk.Button) { a.input(b.GetName()) }
	a.onSci = func(b gtk.ToggleButton) { a.scikeys.SetVisible(b.GetActive()) }
	a.onDegrees = func(gtk.ToggleButton) {
		if a.answered {
			return
		}
		// Preserve a value being edited, but refresh a focused preview.
		var source *gtk.Entry
		if a.active == &a.result && a.result.GetText() == a.text {
			source = &a.result
		}
		a.show(source)
	}
	a.onKeyPress = func(_ gtk.EventControllerKey, keyval, _ uint32, state gdk.ModifierType) bool {
		return a.typed(keyval, state)
	}
	key := func(grid *gtk.Grid, label, input, style, icon string, col, row int) {
		b := gtk.NewButtonWithLabel(label)
		if icon != "" {
			b = gtk.NewButtonFromIconName(icon)
			b.SetTooltipText(i18n.T(label))
		}
		b.AddCssClass("calc-key")
		if style != "" {
			b.AddCssClass(style)
		}
		b.SetName(input)
		b.SetFocusOnClick(false)
		b.ConnectClicked(&a.onKey)
		grid.Attach(&b.Widget, int32(col), int32(row), 1, 1)
		b.Unref() // the grid holds it
	}
	for r, row := range calcKeys {
		for col, k := range row {
			key(&a.keys, k.label, k.input, k.style, k.icon, col, r)
		}
	}
	for r, row := range calcSciKeys {
		for col, k := range row {
			key(&a.scikeys, k.label, k.input, "sci", "", col, r)
		}
	}
	a.sci.ConnectToggled(&a.onSci)
	a.deg.ConnectToggled(&a.onDegrees)
	a.sci.SetCanFocus(false)
	a.deg.SetCanFocus(false)
	a.active = &a.expr
	a.onChanged = func(ptr, _ uintptr) {
		e := &a.expr
		if ptr == a.result.GoPointer() {
			e = &a.result
		}
		a.edited(e)
	}
	a.onFocus = func(c gtk.EventControllerFocus) {
		w := c.GetWidget()
		defer w.Unref()
		a.active = &a.expr
		if w.GoPointer() == a.result.GoPointer() {
			a.active = &a.result
		}
	}
	a.onMenuPress = func(g gtk.GestureClick, _ int32, x, y float64) {
		w := g.GetWidget()
		defer w.Unref()
		e := &a.expr
		if w.GoPointer() == a.result.GoPointer() {
			e = &a.result
		}
		g.SetState(gtk.EventSequenceClaimedValue)
		a.editMenu(e, int32(x), int32(y))
	}
	a.onClipText = func(source, result, _ uintptr) {
		clip := &gdk.Clipboard{}
		clip.Ptr = source
		text, err := clip.ReadTextFinish(&gio.AsyncResultBase{Ptr: result})
		p := a.paste
		a.paste = nil
		// Do not overwrite edits made while the clipboard was being read.
		if p == nil || err != nil || p.target.GetText() != p.text || a.text != p.expression {
			return
		}
		if in, ok := calc.Pasted(text); ok {
			text, pos := calc.Edit(p.text, p.start, p.end, in, "", false)
			a.replace(p.target, text, pos)
		}
	}
	for _, e := range []*gtk.Entry{&a.expr, &a.result} {
		gtkx.ConnectChanged(e, &a.onChanged)
		focus := gtk.NewEventControllerFocus()
		focus.ConnectEnter(&a.onFocus)
		e.AddController(&focus.EventController)
		click := gtk.NewGestureClick()
		click.SetButton(3)
		click.SetPropagationPhase(gtk.PhaseCaptureValue)
		click.ConnectPressed(&a.onMenuPress)
		e.AddController(&click.EventController)
		keys := gtk.NewEventControllerKey()
		keys.SetPropagationPhase(gtk.PhaseCaptureValue)
		keys.ConnectKeyPressed(&a.onKeyPress)
		e.AddController(&keys.EventController)
	}
	a.show(nil)
	a.expr.GrabFocusWithoutSelecting()
	menuWindow(c, win, []appui.MenuGroup{{Title: "Edit", Sections: []appui.MenuSection{{Actions: []appui.MenuAction{{Name: "calc-copy", Label: "Copy result", Activate: func(gio.SimpleAction, uintptr) { a.copy(a.active) }}, {Name: "calc-history", Label: "History", Activate: func(gio.SimpleAction, uintptr) { a.historyReveal.SetRevealChild(true) }}}}}}})
	return win
}

func (a *calcApp) ThemeChanged() {}

// typed handles calculator commands; GTK handles caret movement, selection,
// deletion, undo and ordinary text input in the focused entry.
func (a *calcApp) typed(keyval uint32, state gdk.ModifierType) bool {
	if a.menu != nil && a.menu.GetVisible() {
		return false
	}
	if state&(gdk.AltMaskValue|gdk.SuperMaskValue) != 0 {
		return false
	}
	if state&gdk.ControlMaskValue != 0 {
		switch gdk.KeyvalToLower(keyval) {
		case 'c':
			a.copy(a.active)
		case 'v':
			a.pasteInto(a.active)
		default:
			return false
		}
		return true
	}
	switch keyval {
	case 0xff0d, 0xff8d, 0xffbd, '=': // Return, KP_Enter, KP_Equal
		a.input("op:equals")
		return true
	case 0xff1b: // Escape
		a.input("op:clear")
		return true
	case 0xff63: // Shift+Insert
		if state&gdk.ShiftMaskValue != 0 {
			a.pasteInto(a.active)
			return true
		}
	case 0xff67: // Menu
		a.editMenu(a.active, 0, a.active.GetHeight())
		return true
	case 0xffc7: // Shift+F10
		if state&gdk.ShiftMaskValue != 0 {
			a.editMenu(a.active, 0, a.active.GetHeight())
			return true
		}
	}
	r := rune(gdk.KeyvalToUnicode(keyval))
	if r >= ' ' && a.answered && a.active == &a.expr {
		start, end := calcSelection(a.active)
		if start == end && end == utf8.RuneCountInString(a.text) {
			a.input(string(r))
			return true
		}
	}
	if r == 'x' { // retain the multiplication shortcut
		a.input("×")
		return true
	}
	return false
}

// calcSelection returns character offsets, as GTK expects, never byte offsets.
func calcSelection(e *gtk.Entry) (int, int) {
	start, end := e.GetPosition(), e.GetPosition()
	e.GetSelectionBounds(&start, &end)
	return int(start), int(end)
}

func (a *calcApp) copy(e *gtk.Entry) {
	text := e.GetText()
	var start, end int32
	if e.GetSelectionBounds(&start, &end) {
		text = e.GetChars(start, end)
	}
	if text != "" {
		clip := e.GetClipboard()
		clip.SetText(text)
		clip.Unref()
	}
}

type calcPaste struct {
	target           *gtk.Entry
	text, expression string
	start, end       int
}

func (a *calcApp) pasteInto(e *gtk.Entry) {
	if a.paste != nil {
		return
	}
	start, end := calcSelection(e)
	a.paste = &calcPaste{target: e, text: e.GetText(), expression: a.text, start: start, end: end}
	clip := e.GetClipboard()
	clip.ReadTextAsync(nil, &a.onClipText, 0)
	clip.Unref()
}

func (a *calcApp) editMenu(e *gtk.Entry, x, y int32) {
	if a.menu != nil {
		a.menu.Unparent()
	}
	e.GrabFocusWithoutSelecting()
	a.active = e
	start, end := calcSelection(e)
	// Popdown may return focus through GTK's normal entry focus handler.
	// Restore the selection captured when the menu opened before acting.
	restore := func() {
		e.GrabFocusWithoutSelecting()
		e.SelectRegion(int32(start), int32(end))
	}
	var cut, copyText, deleteText, selectAll, paste func()
	if e.GetText() != "" {
		copyText = func() { restore(); a.copy(e) }
		selectAll = func() { e.GrabFocusWithoutSelecting(); e.SelectRegion(0, -1) }
	}
	if start != end {
		deleteText = func() {
			restore()
			e.DeleteSelection()
		}
		cut = func() { restore(); a.copy(e); e.DeleteSelection() }
	}
	clip := e.GetClipboard()
	formats := clip.GetFormats()
	if formats.ContainGtype(gobject.TypeStringVal) || formats.ContainMimeType("text/plain") || formats.ContainMimeType("text/plain;charset=utf-8") {
		paste = func() { restore(); a.pasteInto(e) }
	}
	clip.Unref()
	a.menu = uikit.NewMenu([]uikit.Item{
		{Label: i18n.T("Cut"), Keys: "Ctrl X", Run: cut},
		{Label: i18n.T("Copy"), Keys: "Ctrl C", Run: copyText},
		{Label: i18n.T("Paste"), Keys: "Ctrl V", Run: paste},
		{Label: i18n.T("Delete"), Keys: "Delete", Run: deleteText},
		{Separator: true},
		{Label: i18n.T("Select all"), Keys: "Ctrl A", Run: selectAll},
	}, uikit.AtPointer)
	a.menu.SetParent(&e.Widget)
	a.menu.Unref() // the entry owns the popover
	a.menu.SetPointingTo(&gdk.Rectangle{X: x, Y: y, Width: 1, Height: 1})
	a.menu.Popup()
}

// replace changes a field once, without preview feedback resetting its caret.
func (a *calcApp) replace(e *gtk.Entry, text string, pos int) {
	a.syncing = true
	e.SetText(text)
	e.SetPosition(int32(pos))
	a.syncing = false
	a.edited(e)
}

func (a *calcApp) edited(e *gtk.Entry) {
	if a.syncing {
		return
	}
	a.text, a.answered = e.GetText(), false
	a.show(e)
}

// input inserts keypad text at the last focused field's selection.
func (a *calcApp) input(in string) {
	e := a.active
	start, end := calcSelection(e)
	text := e.GetText()
	switch in {
	case "op:clear":
		a.text, a.answered = "", false
		a.show(nil)
		a.active = &a.expr
		a.expr.GrabFocusWithoutSelecting()
		a.expr.SetPosition(0)
		return
	case "op:back":
		if start == end {
			start = max(0, start-1)
		}
		text, start = calc.Edit(text, start, end, "", "", false)
	case "op:negate":
		if a.answered {
			text = a.answer
		}
		switch {
		case strings.HasPrefix(text, "−(") && strings.HasSuffix(text, ")"):
			text = text[len("−(") : len(text)-1]
		case text != "":
			text = "−(" + text + ")"
		}
		start = utf8.RuneCountInString(text)
	case "op:equals":
		if strings.TrimSpace(a.text) == "" {
			return
		}
		v, err := calc.Eval(a.text, a.deg.GetActive())
		if err != nil {
			a.error.SetText(i18n.T("Enter a valid expression."))
			a.error.SetVisible(true)
			return
		}
		a.answer, a.answered = calc.Format(v), true
		a.past = calc.Remember(a.past, calc.Entry{Expr: a.text, Result: a.answer})
		store.Save(calcHistory, a.past)
		a.fillHistory()
		a.show(nil)
		// Continue at the formula's end after evaluating either field.
		a.active = &a.expr
		a.expr.GrabFocusWithoutSelecting()
		a.expr.SetPosition(-1)
		return
	default:
		if in == "x" {
			in = "×"
		}
		text, start = calc.Edit(text, start, end, in, a.answer, a.answered && e == &a.expr)
	}
	e.GrabFocusWithoutSelecting()
	a.replace(e, text, start)
}

// fillHistory lists the history in its panel: each calculation a button
// that recalls its result.
func (a *calcApp) fillHistory() {
	gtkx.Clear(&a.historyList)
	a.historyClear.SetVisible(len(a.past) > 0)
	if len(a.past) == 0 {
		l := gtk.NewLabel(i18n.T("No calculations yet"))
		l.AddCssClass("calc-history-empty")
		gtkx.Append(&a.historyList, &l.Widget)
		return
	}
	for i, e := range a.past {
		col := gtk.NewBox(gtk.OrientationVerticalValue, 0)
		ex := gtk.NewLabel(e.Expr + " =")
		ex.AddCssClass("calc-history-expr")
		ex.SetXalign(1)
		ex.SetEllipsize(1) // start
		ex.SetMaxWidthChars(28)
		gtkx.Append(col, &ex.Widget)
		res := gtk.NewLabel(e.Result)
		res.AddCssClass("calc-history-result")
		res.SetXalign(1)
		res.SetEllipsize(1)
		res.SetMaxWidthChars(20)
		gtkx.Append(col, &res.Widget)
		b := gtk.NewButton()
		b.AddCssClass("calc-history-entry")
		b.SetName(strconv.Itoa(i))
		b.SetTooltipText(i18n.Tf("Use %s", e.Result))
		b.SetChild(&col.Widget)
		col.Unref()
		b.SetFocusOnClick(false) // Enter stays with the calculator
		b.ConnectClicked(&a.onRecall)
		gtkx.Append(&a.historyList, &b.Widget)
	}
}

// show never writes back into the source of an edit. In particular, a value
// such as "1." must stay as typed while its expression is updated.
func (a *calcApp) show(source *gtk.Entry) {
	a.syncing = true
	defer func() { a.syncing = false }()
	a.error.SetVisible(false)
	if source != &a.expr && a.expr.GetText() != a.text {
		a.expr.SetText(a.text)
		a.expr.SetPosition(-1)
	}
	if a.answered {
		a.result.RemoveCssClass("preview")
	} else {
		a.result.AddCssClass("preview")
	}
	if source == &a.result {
		return
	}
	preview := ""
	if strings.TrimSpace(a.text) == "" {
		preview = "0"
	} else if v, err := calc.Eval(a.text, a.deg.GetActive()); err == nil {
		preview = calc.Format(v)
	}
	if a.result.GetText() != preview {
		a.result.SetText(preview)
		a.result.SetPosition(-1)
	}
}
