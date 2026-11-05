package termui

import (
	"fmt"
	"log"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/settings"
	"github.com/lyricat/inua/internal/uikit"
)

// The settings (Ctrl+,) edit term.toml in the settings window's
// language: an index of pages at the left, as Mail's settings have, and
// the page chosen, its sections of rows; each change saved and used at
// once. The
// host's own (shell integration, agents, remembering, replay) take hold
// when it starts again. A key is recorded by pressing it.

type prefs struct {
	win     *gtk.Window
	keycaps map[string]*gtk.Button // by action
	// recording is the action whose keys are being pressed; "" for none.
	recording string
	note      *gtk.Label
	// The index and the pages it shows, by name.
	index *gtk.ListBox
	pages *gtk.Stack
	web   *webPrefs // the Web Terminal page's controls

	cbs     []any // the callbacks GTK holds
	onClose func(gtk.Window) bool
	onKey   func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
}

func (a *App) openSettings() { a.openSettingsAt("", nil) }

// openSettingsAt opens the settings at a page ("" for the first, or the
// one shown), with the Web Terminal's error when one is given.
func (a *App) openSettingsAt(page string, webErr error) {
	if a.prefs == nil {
		a.buildSettings()
	}
	pr := a.prefs
	for i := int32(0); page != ""; i++ {
		r := pr.index.GetRowAtIndex(i)
		if r == nil {
			break
		}
		if r.GetName() == page {
			pr.index.SelectRow(r) // its page shows (onRow)
			break
		}
	}
	if webErr != nil && pr.web != nil {
		pr.web.fail(webErr)
	}
	pr.win.Present()
}

// buildSettings makes the settings window, not yet shown.
func (a *App) buildSettings() {
	pr := &prefs{keycaps: map[string]*gtk.Button{}}
	a.prefs = pr
	pr.win = gtk.NewWindow()
	pr.win.SetTitle(i18n.T("Terminal Settings"))
	pr.win.SetTransientFor(&a.win.Window)
	pr.win.SetDefaultSize(880, 640)
	for _, c := range []string{"app", "settings", "term", "term-prefs"} {
		pr.win.AddCssClass(c)
	}
	root := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	side := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	side.AddCssClass("index")
	side.SetSizeRequest(220, -1)
	iscroll := gtk.NewScrolledWindow()
	iscroll.SetVexpand(true)
	iscroll.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	pr.index = gtk.NewListBox()
	pr.index.SetSelectionMode(gtk.SelectionBrowseValue)
	iscroll.SetChild(&pr.index.Widget)
	side.Append(&iscroll.Widget)
	// What a key being recorded takes, and why a save failed: under the
	// index, in sight from every page.
	pr.note = gtk.NewLabel("")
	pr.note.AddCssClass("sub")
	pr.note.AddCssClass("term-prefs-note")
	pr.note.SetXalign(0)
	pr.note.SetWrap(true)
	side.Append(&pr.note.Widget)
	root.Append(&side.Widget)
	pr.pages = gtk.NewStack()
	pr.pages.SetHexpand(true)
	pr.pages.SetTransitionType(gtk.StackTransitionTypeCrossfadeValue)
	pr.pages.SetTransitionDuration(120)
	root.Append(&pr.pages.Widget)
	pr.win.SetChild(&root.Widget)
	onRow := func(_ gtk.ListBox, row uintptr) {
		if row != 0 {
			pr.pages.SetVisibleChildName(gtk.ListBoxRowNewFromInternalPtr(row).GetName())
		}
	}
	pr.cbs = append(pr.cbs, &onRow)
	pr.index.ConnectRowSelected(&onRow)
	s := &a.set
	save := func() {
		if err := settings.Save(settings.Path(), *s); err != nil {
			log.Printf("term: settings: %v", err)
			pr.say(err.Error())
			return
		}
		a.useSettings(*s)
	}

	page := pr.page("look", i18n.T("Look"), "preferences-desktop-appearance-symbolic")
	sec := pr.section(page, i18n.T("LOOK"), "")
	themes := themeChoices(a.c)
	theme := gtk.NewDropDownFromStrings(choiceLabels(themes))
	theme.SetSelected(uint32(choiceIndex(themes, s.Look.Theme)))
	pr.selected(theme, func(i uint) { s.Look.Theme = themes[i].value; save() })
	pr.row(sec, i18n.T("Window theme"), i18n.T("inua's theme, or one of its others for this window alone"), &theme.Widget)
	schemes := schemeChoices()
	sch := gtk.NewDropDownFromStrings(choiceLabels(schemes))
	sch.SetSelected(uint32(choiceIndex(schemes, s.Look.Scheme)))
	pr.selected(sch, func(i uint) { s.Look.Scheme = schemes[i].value; save() })
	pr.row(sec, i18n.T("Terminal colours"), i18n.T("The colours programs draw with: the window theme's, or a scheme"), &sch.Widget)

	sec = pr.section(page, i18n.T("TEXT"), "")
	family := gtk.NewEntry()
	family.SetText(s.Font.Family)
	family.SetWidthChars(18)
	pr.changed(family, func() {
		if f := strings.TrimSpace(family.GetText()); f != "" {
			s.Font.Family = f
			save()
		}
	})
	pr.row(sec, i18n.T("Font"), i18n.T("A monospace family; Pango falls back for CJK"), &family.Widget)
	size := gtk.NewSpinButtonWithRange(6, 48, 0.5)
	size.SetValue(s.Font.Size)
	pr.spin(size, func(v float64) { s.Font.Size = v; save() })
	pr.row(sec, i18n.T("Size"), i18n.T("In points; Ctrl+= and Ctrl+- change it in the window"), &size.Widget)

	sec = pr.section(page, i18n.T("CURSOR"), "")
	styles := []string{"block", "bar", "underline"}
	style := gtk.NewDropDownFromStrings([]string{i18n.T("Block"), i18n.T("Bar"), i18n.T("Underline")})
	for i, st := range styles {
		if st == s.Cursor.Style {
			style.SetSelected(uint32(i))
		}
	}
	pr.selected(style, func(i uint) { s.Cursor.Style = styles[i]; save() })
	pr.row(sec, i18n.T("Shape"), i18n.T("What a program resetting the cursor gets"), &style.Widget)
	pr.row(sec, i18n.T("Blink"), "", pr.toggle(s.Cursor.Blink, func(on bool) { s.Cursor.Blink = on; save() }))

	page = pr.page("terminal", i18n.T("Terminal"), "utilities-terminal-symbolic")
	sec = pr.section(page, i18n.T("TERMINAL"), "")
	pr.row(sec, i18n.T("Copy on select"), i18n.T("Selecting copies to the clipboard; the primary selection follows it either way"),
		pr.toggle(s.Terminal.CopyOnSelect, func(on bool) { s.Terminal.CopyOnSelect = on; save() }))
	pr.row(sec, i18n.T("Focus follows mouse"), i18n.T("Focus a pane when the pointer enters it in the active window"),
		pr.toggle(s.Terminal.FocusFollowsMouse, func(on bool) { s.Terminal.FocusFollowsMouse = on; save() }))
	sb := gtk.NewSpinButtonWithRange(0, 1000000, 1000)
	sb.SetValue(float64(s.Terminal.Scrollback))
	pr.spin(sb, func(v float64) { s.Terminal.Scrollback = int(v); save() })
	pr.row(sec, i18n.T("Scrollback"), i18n.T("Lines kept per pane"), &sb.Widget)
	replay := gtk.NewSpinButtonWithRange(1, 64, 1)
	replay.SetValue(float64(s.Terminal.Replay))
	pr.spin(replay, func(v float64) { s.Terminal.Replay = int(v); save() })
	pr.row(sec, i18n.T("Replay"), i18n.T("MiB of a pane's recent output kept for a window opened later"), &replay.Widget)

	page = pr.page("shell", i18n.T("Shell and agents"), "disk-agent-symbolic")
	sec = pr.section(page, i18n.T("SHELL AND AGENTS"), i18n.T("Below the shell: used when the terminal host starts again"))
	shells := shellChoices(s.Shell.Program)
	sh := gtk.NewDropDownFromStrings(choiceLabels(shells))
	sh.SetSelected(uint32(choiceIndex(shells, s.Shell.Program)))
	pr.selected(sh, func(i uint) { s.Shell.Program = shells[i].value; save() })
	pr.row(sec, i18n.T("Shell"), i18n.T("What new panes start; the panes open already keep theirs"), &sh.Widget)
	pr.row(sec, i18n.T("Shell integration"), i18n.T("bash, zsh and fish mark their prompts, so the commands are remembered"),
		pr.toggle(s.Shell.Integration, func(on bool) { s.Shell.Integration = on; save() }))
	pr.row(sec, i18n.T("Notifications"), i18n.T("When an agent needs you or is done in a pane you are not looking at"),
		pr.toggle(s.Agents.Notifications, func(on bool) { s.Agents.Notifications = on; save() }))
	pr.row(sec, i18n.T("Remember commands"), i18n.T("Put each pane's last command back at its prompt, not run"),
		pr.toggle(s.Remember.Commands, func(on bool) { s.Remember.Commands = on; save() }))
	forget := gtk.NewEntry()
	forget.SetText(strings.Join(s.Remember.Forget, ", "))
	forget.SetWidthChars(26)
	pr.changed(forget, func() {
		var pats []string
		for _, f := range strings.Split(forget.GetText(), ",") {
			if f = strings.TrimSpace(f); f != "" {
				pats = append(pats, f)
			}
		}
		s.Remember.Forget = pats
		save()
	})
	pr.row(sec, i18n.T("Never keep"), i18n.T("Commands matching these patterns, comma-separated"), &forget.Widget)

	pr.webSection(pr.page("web", i18n.T("Web terminal"), "connect-phone-symbolic"), s, save)

	page = pr.page("keys", i18n.T("Keys"), "preferences-desktop-keyboard-shortcuts-symbolic")
	sec = pr.section(page, i18n.T("KEYS"), i18n.T("Click a key, then press the new one; Backspace frees it"))
	for _, act := range settings.Actions {
		b := gtk.NewButtonWithLabel("")
		b.AddCssClass("keycap")
		b.SetValign(gtk.AlignCenterValue)
		pr.keycaps[act.Name] = b
		name := act.Name
		pr.click(b, func() { pr.record(a, name) })
		pr.row(sec, i18n.T(act.Label), "", &b.Widget)
	}
	pr.showKeys(a)

	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCaptureValue)
	pr.onKey = func(c gtk.EventControllerKey, keyval, keycode uint32, state gdk.ModifierType) bool {
		if pr.recording == "" {
			return false
		}
		return pr.press(a, keyval, keycode, keyMods(state))
	}
	keys.ConnectKeyPressed(&pr.onKey)
	pr.win.AddController(&keys.EventController)
	pr.onClose = func(gtk.Window) bool {
		a.prefs = nil
		a.freeLater(append(pr.cbs, &pr.onKey, &pr.onClose))
		return false
	}
	pr.win.ConnectCloseRequest(&pr.onClose)
}

// page adds a page, and its row in the index; its box comes back, for
// its sections. The first page is the one shown.
func (pr *prefs) page(name, title, icon string) *gtk.Box {
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNeverValue, gtk.PolicyAutomaticValue)
	page := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	page.AddCssClass("page")
	scroll.SetChild(&page.Widget)
	pr.pages.AddNamed(&scroll.Widget, name)

	box := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	img := gtk.NewImageFromIconName(icon)
	img.AddCssClass("index-icon")
	box.Append(&img.Widget)
	l := gtk.NewLabel(title)
	l.SetXalign(0)
	l.SetEllipsize(3)
	box.Append(&l.Widget)
	row := gtk.NewListBoxRow()
	row.SetName(name)
	row.SetChild(&box.Widget)
	pr.index.Append(&row.Widget)
	if pr.index.GetSelectedRow() == nil {
		pr.index.SelectRow(row)
	}
	return page
}

// section is a section head (mark, [LABEL], a note) over a body; its
// body comes back.
func (pr *prefs) section(page *gtk.Box, label, meta string) *gtk.Box {
	sec := uikit.NewSection(label, meta)
	page.Append(&sec.Box.Widget)
	return sec.Body
}

// row is a title and sub line with a control at the end.
func (pr *prefs) row(body *gtk.Box, title, sub string, control *gtk.Widget) {
	if body.GetFirstChild() != nil {
		sep := gtk.NewSeparator(gtk.OrientationHorizontalValue)
		body.Append(&sep.Widget)
	}
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 16)
	row.AddCssClass("row")
	text := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	text.SetHexpand(true)
	text.SetValign(gtk.AlignCenterValue)
	t := gtk.NewLabel(title)
	t.AddCssClass("title")
	t.SetXalign(0)
	text.Append(&t.Widget)
	if sub != "" {
		s := gtk.NewLabel(sub)
		s.AddCssClass("sub")
		s.SetXalign(0)
		s.SetWrap(true)
		text.Append(&s.Widget)
	}
	row.Append(&text.Widget)
	control.SetValign(gtk.AlignCenterValue)
	row.Append(control)
	body.Append(&row.Widget)
}

func (pr *prefs) toggle(on bool, f func(bool)) *gtk.Widget {
	sw := gtk.NewSwitch()
	sw.SetActive(on)
	cb := func(uintptr, uintptr, uintptr) { f(sw.GetActive()) }
	pr.cbs = append(pr.cbs, &cb)
	gtkx.ConnectNotify(sw, "active", &cb)
	return &sw.Widget
}

func (pr *prefs) spin(b *gtk.SpinButton, f func(float64)) {
	cb := func(gtk.SpinButton) { f(b.GetValue()) }
	pr.cbs = append(pr.cbs, &cb)
	b.ConnectValueChanged(&cb)
}

func (pr *prefs) selected(d *gtk.DropDown, f func(uint)) {
	cb := func(uintptr, uintptr, uintptr) { f(uint(d.GetSelected())) }
	pr.cbs = append(pr.cbs, &cb)
	gtkx.ConnectNotify(d, "selected", &cb)
}

// changed runs f when an entry's text is set: Enter, or leaving it.
func (pr *prefs) changed(e *gtk.Entry, f func()) {
	act := func(gtk.Entry) { f() }
	pr.cbs = append(pr.cbs, &act)
	e.ConnectActivate(&act)
	focus := gtk.NewEventControllerFocus()
	leave := func(gtk.EventControllerFocus) { f() }
	pr.cbs = append(pr.cbs, &leave)
	focus.ConnectLeave(&leave)
	e.AddController(&focus.EventController)
}

func (pr *prefs) click(b *gtk.Button, f func()) {
	cb := func(gtk.Button) { f() }
	pr.cbs = append(pr.cbs, &cb)
	b.ConnectClicked(&cb)
}

func (pr *prefs) say(s string) { pr.note.SetText(s) }

// showKeys writes every action's keys on its keycap.
func (pr *prefs) showKeys(a *App) {
	for name, b := range pr.keycaps {
		b.RemoveCssClass("waiting")
		k := keycaps(a.set.Keys[name])
		if k == "" {
			k = i18n.T("None")
		}
		b.SetLabel(k)
	}
}

// record waits for an action's new keys.
func (pr *prefs) record(a *App, action string) {
	pr.showKeys(a)
	pr.recording = action
	b := pr.keycaps[action]
	b.AddCssClass("waiting")
	b.SetLabel(i18n.T("Press keys…"))
	pr.say(i18n.T("Escape cancels; Backspace frees the action's keys."))
}

// press takes a key while recording: a chord with Ctrl, Alt or Super;
// Escape cancels, Backspace frees the action. A chord another action has
// moves to this one, and that one is freed.
func (pr *prefs) press(a *App, keyval, keycode uint32, state gdk.ModifierType) bool {
	switch int32(keyval) {
	case gdk.KEY_Shift_L, gdk.KEY_Shift_R, gdk.KEY_Control_L, gdk.KEY_Control_R, gdk.KEY_Alt_L, gdk.KEY_Alt_R, gdk.KEY_Super_L, gdk.KEY_Super_R, gdk.KEY_ISO_Level3_Shift:
		return true // a modifier alone: wait for the key
	}
	held := state & (gdk.ControlMaskValue | gdk.ShiftMaskValue | gdk.AltMaskValue | gdk.SuperMaskValue)
	action := pr.recording
	pr.recording = ""
	s := &a.set
	switch {
	case held == 0 && int32(keyval) == gdk.KEY_Escape:
		pr.say("")
	case held == 0 && int32(keyval) == gdk.KEY_BackSpace:
		s.Keys[action] = ""
		pr.say("")
	case held&(gdk.ControlMaskValue|gdk.AltMaskValue|gdk.SuperMaskValue) == 0:
		pr.say(i18n.T("A key needs Ctrl, Alt or Super with it: the others are the programs'."))
	default:
		var base uint32
		if !pr.win.GetDisplay().TranslateKey(keycode, 0, 0, &base, nil, nil, nil) {
			base = keyval
		}
		chord := chordName(base, held)
		note := ""
		for other, k := range s.Keys {
			if k == chord && other != action {
				s.Keys[other] = ""
				note = i18n.Tf("%s was %s's; that one has no keys now.", keycaps(chord), actionLabel(other))
			}
		}
		s.Keys[action] = chord
		pr.say(note)
	}
	pr.showKeys(a)
	if err := settings.Save(settings.Path(), *s); err != nil {
		pr.say(err.Error())
		return true
	}
	a.useSettings(*s)
	return true
}

func actionLabel(name string) string {
	for _, act := range settings.Actions {
		if act.Name == name {
			return i18n.T(act.Label)
		}
	}
	return fmt.Sprint(name)
}
