package lightui

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/calc"
	"github.com/lyricat/inua/internal/apps/store"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// calcApp is the calculator. The expression is kept as shown (× ÷ −);
// the result below previews it while it is typed and becomes the answer
// on =. After =, a number starts a new expression and an operator goes on
// from the answer. Each answer goes into the history (calc-history.json),
// listed in a panel at the right that History slides in; an entry
// recalls its result into the expression. Ctrl+C copies the result,
// Ctrl+V types what is on the clipboard.
type calcApp struct {
	expr, result     gtk.Label
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
		a.show()
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
	a.onDegrees = func(gtk.ToggleButton) { a.show() }
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
	a.onClipText = func(_, result, _ uintptr) {
		clip := a.expr.GetClipboard()
		text, err := clip.ReadTextFinish(&gio.AsyncResultBase{Ptr: result})
		clip.Unref()
		if in, ok := calc.Pasted(text); err == nil && ok {
			a.input(in)
		}
	}
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCaptureValue) // before a focused button takes Enter
	keys.ConnectKeyPressed(&a.onKeyPress)
	win.AddController(&keys.EventController)
	a.show()
	menuWindow(c, win, []appui.MenuGroup{{Title: "Edit", Sections: []appui.MenuSection{{Actions: []appui.MenuAction{{Name: "calc-copy", Label: "Copy result", Activate: func(gio.SimpleAction, uintptr) { a.copy() }}, {Name: "calc-history", Label: "History", Activate: func(gio.SimpleAction, uintptr) { a.historyReveal.SetRevealChild(true) }}}}}}})
	return win
}

func (a *calcApp) ThemeChanged() {}

// typed turns a key press into input.
func (a *calcApp) typed(keyval uint32, state gdk.ModifierType) bool {
	if state&gdk.ControlMaskValue != 0 {
		switch gdk.KeyvalToLower(keyval) {
		case 'c':
			a.copy()
		case 'v':
			clip := a.expr.GetClipboard()
			clip.ReadTextAsync(nil, &a.onClipText, 0)
			clip.Unref()
		default:
			return false
		}
		return true
	}
	switch keyval {
	case 0xff0d, 0xff8d: // Return, KP_Enter
		a.input("op:equals")
		return true
	case 0xff08: // BackSpace
		a.input("op:back")
		return true
	case 0xff1b, 0xffff: // Escape, Delete
		a.input("op:clear")
		return true
	}
	r := rune(gdk.KeyvalToUnicode(keyval))
	in := map[rune]string{'*': "×", 'x': "×", '/': "÷", '-': "−", '=': "op:equals"}[r]
	if in == "" && r != 0 && strings.ContainsRune("0123456789.+%^()", r) {
		in = string(r)
	}
	if in == "" {
		return false
	}
	a.input(in)
	return true
}

// copy puts the result on the clipboard: the answer, or the value of what
// is typed; the expression when it has none yet.
func (a *calcApp) copy() {
	text := a.answer
	if !a.answered {
		text = a.text
		if v, err := calc.Eval(a.text, a.deg.GetActive()); err == nil {
			text = calc.Format(v)
		}
	}
	if text == "" {
		return
	}
	clip := a.expr.GetClipboard()
	clip.SetText(text)
	clip.Unref()
}

// input takes one key's input.
func (a *calcApp) input(in string) {
	switch in {
	case "op:clear":
		a.text, a.answered = "", false
	case "op:back":
		if a.answered {
			a.text, a.answered = "", false
			break
		}
		a.text = trimLast(a.text)
	case "op:negate":
		switch {
		case a.answered:
			a.text, a.answered = "−("+a.answer+")", false
		case strings.HasPrefix(a.text, "−(") && strings.HasSuffix(a.text, ")"):
			a.text = a.text[len("−(") : len(a.text)-1]
		case a.text != "":
			a.text = "−(" + a.text + ")"
		}
	case "op:equals":
		v, err := calc.Eval(a.text, a.deg.GetActive())
		if a.text == "" || err != nil {
			a.show()
			if a.text != "" {
				a.result.SetText(i18n.T("Error"))
				a.result.RemoveCssClass("preview")
			}
			return
		}
		a.answer = calc.Format(v)
		a.past = calc.Remember(a.past, calc.Entry{Expr: a.text, Result: a.answer})
		store.Save(calcHistory, a.past)
		a.fillHistory()
		a.expr.SetText(a.text + " =")
		a.result.SetText(a.answer)
		a.result.RemoveCssClass("preview")
		a.answered = true
		return
	default:
		if a.answered {
			if r, _ := utf8.DecodeRuneInString(in); strings.ContainsRune("+−×÷^%", r) {
				a.text = a.answer // go on from the answer
			} else {
				a.text = ""
			}
			a.answered = false
		}
		a.text += in
	}
	a.show()
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

// trimLast removes the last thing typed: a function name with its
// parenthesis counts as one.
func trimLast(s string) string {
	for _, f := range []string{"asin(", "acos(", "atan(", "sin(", "cos(", "tan(", "log(", "ln(", "√("} {
		if strings.HasSuffix(s, f) {
			return s[:len(s)-len(f)]
		}
	}
	_, size := utf8.DecodeLastRuneInString(s)
	return s[:len(s)-size]
}

// show puts the expression up, with its value as a preview.
func (a *calcApp) show() {
	if a.answered {
		return
	}
	a.expr.SetText(a.text)
	a.result.AddCssClass("preview")
	if a.text == "" {
		a.result.SetText("0")
		return
	}
	if v, err := calc.Eval(a.text, a.deg.GetActive()); err == nil {
		a.result.SetText(calc.Format(v))
	} else {
		a.result.SetText("")
	}
}
