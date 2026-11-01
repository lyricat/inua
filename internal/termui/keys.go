package termui

import (
	"errors"
	"log"
	"strings"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/agent"
	"github.com/lyricat/inua/internal/term/host"
	"github.com/lyricat/inua/internal/term/layout"
)

// inua-term's own keys are Ghostty's (docs/terminal.md): a workspace is
// Ghostty's window, a session its tab, a pane its split; everything else
// goes to the program. A chord is matched by its key without Shift (base),
// so Ctrl+Shift+1 is "1" whatever Shift makes of it on the layout.

// chord does the window's action for a chord of the settings' keys
// (term.toml [keys]), or the fixed ones (focusMods and the others, per
// system): an arrow moves the focus or resizes, an arrow after the swap
// key swaps, a digit shows a session or a workspace, and Ctrl+PgUp and
// PgDn step through the sessions. false leaves the key to the pane.
func (a *App) chord(p *termPane, base, keyval uint32, state gdk.ModifierType) bool {
	held := state & (gdk.ControlMaskValue | gdk.ShiftMaskValue | gdk.AltMaskValue | gdk.SuperMaskValue)
	if a.swapping {
		a.swapping = false
		if d, ok := arrow(keyval); ok {
			a.layoutOp(host.OpSwap, host.Message{Direction: int(d)})
			return true
		}
	}
	if d, ok := arrow(keyval); ok {
		switch held {
		case focusMods:
			a.layoutOp(host.OpFocus, host.Message{Direction: int(d)})
			return true
		case resizeMods:
			a.layoutOp(host.OpResize, host.Message{Direction: int(d), Step: 0.05})
			return true
		}
		return false
	}
	if held == gdk.ControlMaskValue {
		switch int32(keyval) {
		case gdk.KEY_Page_Up, gdk.KEY_KP_Page_Up:
			a.stepSession(-1)
			return true
		case gdk.KEY_Page_Down, gdk.KEY_KP_Page_Down:
			a.stepSession(1)
			return true
		}
	}
	if a.nthChord(base, held) {
		return true
	}
	action, ok := a.keymap[chordName(base, held)]
	if !ok {
		return false
	}
	return a.do(action, p)
}

// windowKeys takes the window's chords where no pane has the keys: a
// workspace without panes, the columns. A pane's keys reach it first.
func (a *App) windowKeys() {
	loadGObject()
	keys := gtk.NewEventControllerKey()
	a.onWindowKey = func(c gtk.EventControllerKey, keyval, keycode uint32, state gdk.ModifierType) bool {
		if a.sheet != nil {
			return false
		}
		var layout uint32
		if ev := gobj.currentEvent(c.GoPointer()); ev != 0 { // borrowed, no reference taken
			layout = gdk.KeyEventNewFromInternalPtr(ev).GetLayout()
		}
		var base uint32
		if !a.win.GetDisplay().TranslateKey(keycode, 0, int32(layout), &base, nil, nil, nil) {
			base = keyval
		}
		return a.chord(nil, base, keyval, keyMods(state))
	}
	keys.ConnectKeyPressed(&a.onWindowKey)
	a.win.AddController(&keys.EventController)
}

// chordName is a key as term.toml writes it: "ctrl+shift+t".
func chordName(base uint32, held gdk.ModifierType) string {
	return modNames(held) + strings.ToLower(gdk.KeyvalName(gdk.KeyvalToLower(base)))
}

// modOrder is the modifiers in the order a chord names them.
var modOrder = []struct {
	mask gdk.ModifierType
	name string
}{{gdk.ControlMaskValue, "ctrl"}, {gdk.ShiftMaskValue, "shift"}, {gdk.AltMaskValue, "alt"}, {gdk.SuperMaskValue, "super"}}

// keymapOf is the settings' keys by chord.
func keymapOf(keys map[string]string) map[string]string {
	m := map[string]string{}
	for action, k := range keys {
		if k != "" {
			m[k] = action
		}
	}
	return m
}

// do is an action of the window's, by its name in term.toml; p is the
// pane the key came from, for the pane's own.
func (a *App) do(action string, p *termPane) bool {
	switch action {
	case "new_session":
		a.newSession()
	case "new_workspace":
		a.newWorkspace()
	case "split_right":
		a.split(layout.Right)
	case "split_down":
		a.split(layout.Down)
	case "close_pane":
		a.closeFocused()
	case "swap":
		a.swapping = true
	case "zoom":
		a.layoutOp(host.OpZoom, host.Message{})
	case "equalize":
		a.layoutOp(host.OpEqualize, host.Message{})
	case "next_layout":
		a.layoutOp(host.OpPreset, host.Message{})
	case "previous_session":
		a.stepSession(-1)
	case "next_session":
		a.stepSession(1)
	case "next_waiting":
		a.nextWaiting()
	case "next_idle":
		a.nextIdle()
	case "agents":
		a.overview()
	case "fold_workspaces":
		a.toggleColumn(0)
	case "fold_sessions":
		a.toggleColumn(1)
	case "rename":
		a.renameSession(0)
	case "font_larger":
		a.zoomFont(1)
	case "font_smaller":
		a.zoomFont(-1)
	case "font_reset":
		a.zoomFont(0)
	case "palette":
		a.palette(p)
	case "keys":
		a.showKeys()
	case "settings":
		a.openSettings()
	case "restart_host":
		a.restartHost()
	case "copy_mode":
		if p == nil {
			return false
		}
		p.startCopyMode()
	case "copy", "paste", "find":
		if p == nil {
			return false
		}
		switch action {
		case "copy":
			p.copySelection(true)
		case "paste":
			p.pasteFrom(false)
		case "find":
			p.openFind()
		}
	default:
		if a.extra != nil && a.extra(action, p) {
			return true
		}
		return false
	}
	return true
}

// nthChord shows the nth session (Alt+1…9, Ghostty's tabs) or workspace
// (Ctrl+Shift+1…9); fixed.
func (a *App) nthChord(base uint32, held gdk.ModifierType) bool {
	k := rune(gdk.KeyvalToUnicode(base))
	if k < '1' || k > '9' {
		return false
	}
	switch held {
	case sessionMods:
		a.nthSession(int(k - '1'))
	case workspaceMods:
		a.nthWorkspace(int(k - '1'))
	default:
		return false
	}
	return true
}

// arrow is an arrow key's direction.
func arrow(keyval uint32) (layout.Dir, bool) {
	switch int32(keyval) {
	case gdk.KEY_Left, gdk.KEY_KP_Left:
		return layout.Left, true
	case gdk.KEY_Right, gdk.KEY_KP_Right:
		return layout.RightOf, true
	case gdk.KEY_Up, gdk.KEY_KP_Up:
		return layout.Up, true
	case gdk.KEY_Down, gdk.KEY_KP_Down:
		return layout.Below, true
	}
	return 0, false
}

func (a *App) layoutOp(op string, m host.Message) {
	s := a.shown()
	if s == nil {
		return
	}
	session := s.ID
	a.ask(op, func(c *host.Client) error { return c.Layout(session, op, m) })
}

func (a *App) split(sp layout.Split) {
	s := a.shown()
	if s == nil {
		return
	}
	session := s.ID
	a.ask("split", func(c *host.Client) error {
		_, err := c.Split(session, string(sp))
		return err
	})
}

// closeFocused checks current host process state before offering to close.
func (a *App) closeFocused() {
	if a.sheet != nil || a.closePending || a.client == nil {
		return
	}
	s := a.shown()
	if s == nil {
		return
	}
	pane, client := s.Focus, a.client
	a.closePending = true
	go func() {
		err := client.ClosePane(pane)
		gtkx.Post(func() {
			a.closePending = false
			if errors.Is(err, host.ErrCloseConfirmation) {
				// Another sheet may have opened while the host inspected the process.
				if a.sheet == nil {
					a.confirmClosePane(pane)
				}
			} else if err != nil {
				log.Printf("term: close: %v", err)
			}
		})
	}()
}

func (a *App) confirmClosePane(pane int) {
	info := a.info[pane]
	title := i18n.T("Close pane?")
	message := i18n.T("A program may still be running. Closing the pane ends it.")
	if st := agent.State(info.State); info.Agent != "" && (st == agent.Working || st == agent.Blocked) {
		title = i18n.Tf("Close %s?", info.AgentName)
		message = i18n.Tf("It is working on “%s”; closing the pane ends it.", paneTitle(info))
		if st == agent.Blocked {
			message = i18n.Tf("It is waiting for you: %s. Closing the pane ends it.", info.Ask)
		}
	}

	sheet, done := a.sheetFrame(title, 420)
	why := gtk.NewLabel(message)
	why.AddCssClass("term-sheet-why")
	why.SetXalign(0)
	why.SetWrap(true)
	sheet.Append(&why.Widget)
	buttons := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	buttons.SetHalign(gtk.AlignEndValue)
	cancel := gtk.NewButtonWithLabel(i18n.T("Cancel"))
	cancel.AddCssClass("outlined")
	ok := gtk.NewButtonWithLabel(i18n.T("Close"))
	ok.AddCssClass("destructive-action")
	buttons.Append(&cancel.Widget)
	buttons.Append(&ok.Widget)
	sheet.Append(&buttons.Widget)
	a.onCloseCancel = func(gtk.Button) { done() }
	a.onCloseOK = func(gtk.Button) {
		done()
		a.ask("close", func(c *host.Client) error { return c.ConfirmClosePane(pane) })
	}
	cancel.ConnectClicked(&a.onCloseCancel)
	ok.ConnectClicked(&a.onCloseOK)
	cancel.GrabFocus()
}

// newSession adds a session to the shown workspace.
func (a *App) newSession() {
	w := a.shownWorkspace()
	if w == nil {
		return
	}
	id := w.ID
	a.ask("new session", func(c *host.Client) error {
		_, err := c.NewSession(id, "")
		return err
	})
}

// newWorkspace asks for a folder and opens its workspace.
func (a *App) newWorkspace() {
	d := gtk.NewFileDialog()
	d.SetTitle(i18n.T("Choose the workspace's folder"))
	d.SetModal(true)
	a.onFolder = func(_, result, _ uintptr) {
		f, err := d.SelectFolderFinish(&gio.AsyncResultBase{Ptr: result})
		if err != nil || f == nil {
			return
		}
		dir := f.GetPath()
		if dir == "" {
			return
		}
		a.ask("open", func(c *host.Client) error {
			_, err := c.Open(dir, "")
			return err
		})
	}
	d.SelectFolder(&a.win.Window, nil, &a.onFolder, 0)
}

// nthWorkspace shows the workspace at place i in column 1.
func (a *App) nthWorkspace(i int) {
	if a.state == nil || i >= len(a.state.Model.Workspaces) {
		return
	}
	a.showWorkspace(a.state.Model.Workspaces[i].ID)
}

// nthSession shows the shown workspace's session at place i; Alt+9 is
// the last, as Ghostty's last tab.
func (a *App) nthSession(i int) {
	w := a.shownWorkspace()
	if w == nil || len(w.Sessions) == 0 {
		return
	}
	if i == 8 {
		i = len(w.Sessions) - 1
	}
	if i >= len(w.Sessions) {
		return
	}
	a.showSession(w.ID, w.Sessions[i].ID)
}

// stepSession shows the previous (-1) or next session of the workspace.
func (a *App) stepSession(dir int) {
	w := a.shownWorkspace()
	if w == nil || len(w.Sessions) == 0 {
		return
	}
	i := 0
	for j, s := range w.Sessions {
		if s.ID == a.state.Model.Shown.Session {
			i = j
		}
	}
	i = (i + dir + len(w.Sessions)) % len(w.Sessions)
	a.showSession(w.ID, w.Sessions[i].ID)
}

// renameSession asks for a session's new name (0: the shown one's).
func (a *App) renameSession(id int) {
	s := a.shown()
	if id != 0 && a.state != nil {
		s, _ = a.state.Model.Session(id)
	}
	if s == nil {
		return
	}
	session := s.ID
	a.renameSheet(i18n.T("Rename the session"), s.Name, func(c *host.Client, name string) error {
		return c.Rename(0, session, name)
	})
}

// renameSheet asks for a new name over the window, name in it to start
// with; Enter or Rename gives it to rename, Escape or Cancel keeps the
// old one.
func (a *App) renameSheet(title, name string, rename func(c *host.Client, name string) error) {
	if a.sheet != nil {
		return
	}
	sheet := gtk.NewBox(gtk.OrientationVerticalValue, 10)
	sheet.AddCssClass("term-sheet")
	sheet.SetHalign(gtk.AlignCenterValue)
	sheet.SetValign(gtk.AlignCenterValue)
	sheet.SetSizeRequest(360, -1)
	t := gtk.NewLabel(title)
	t.AddCssClass("term-sheet-title")
	t.SetXalign(0)
	sheet.Append(&t.Widget)
	entry := gtk.NewEntry()
	entry.SetText(name)
	sheet.Append(&entry.Widget)
	buttons := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	buttons.SetHalign(gtk.AlignEndValue)
	cancel := gtk.NewButtonWithLabel(i18n.T("Cancel"))
	cancel.AddCssClass("outlined")
	ok := gtk.NewButtonWithLabel(i18n.T("Rename"))
	ok.AddCssClass("suggested-action")
	buttons.Append(&cancel.Widget)
	buttons.Append(&ok.Widget)
	sheet.Append(&buttons.Widget)
	done := func(ok bool) {
		name := entry.GetText()
		a.sheets.RemoveOverlay(&sheet.Widget)
		a.sheet = nil
		if shown := a.shown(); shown != nil {
			if t := a.tiles[shown.Focus]; t != nil {
				t.pane.widget.GrabFocus()
			}
		}
		if ok && name != "" {
			a.ask("rename", func(c *host.Client) error { return rename(c, name) })
		}
	}
	a.onRenameOK = func(gtk.Button) { done(true) }
	a.onRenameCancel = func(gtk.Button) { done(false) }
	a.onRenameActivate = func(gtk.Entry) { done(true) }
	ok.ConnectClicked(&a.onRenameOK)
	cancel.ConnectClicked(&a.onRenameCancel)
	entry.ConnectActivate(&a.onRenameActivate)
	keys := gtk.NewEventControllerKey()
	a.onRenameKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		if int32(keyval) == gdk.KEY_Escape {
			done(false)
			return true
		}
		return false
	}
	keys.ConnectKeyPressed(&a.onRenameKey)
	sheet.AddController(&keys.EventController)
	a.sheet = sheet
	a.sheets.AddOverlay(&sheet.Widget)
	entry.GrabFocus()
	entry.SelectRegion(0, -1)
}
