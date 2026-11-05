package termui

import (
	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/host"
	"github.com/lyricat/inua/internal/uikit"
)

// Session heads share callbacks, including when column 2 is rebuilt.
type sessionMenu struct {
	onPress func(gtk.GestureClick, int32, float64, float64)
	onKey   func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onShut  func(gtk.Popover)
}

func (c *columns) startSessionMenu(a *App) {
	m := &c.sessionMenu
	m.onPress = func(g gtk.GestureClick, _ int32, x, y float64) {
		w := g.GetWidget()
		if w == nil {
			return
		}
		defer w.Unref()
		g.SetState(gtk.EventSequenceClaimedValue)
		c.showSessionMenu(a, w, &gdk.Rectangle{X: int32(x), Y: int32(y), Width: 1, Height: 1})
	}
	m.onKey = func(keys gtk.EventControllerKey, keyval, _ uint32, state gdk.ModifierType) bool {
		menuKey := int32(keyval) == gdk.KEY_Menu ||
			int32(keyval) == gdk.KEY_F10 && state&gdk.ShiftMaskValue != 0
		if !menuKey {
			return false
		}
		w := keys.GetWidget()
		if w == nil {
			return false
		}
		defer w.Unref()
		if !w.HasFocus() {
			return false
		}
		c.showSessionMenu(a, w, nil)
		return true
	}
	m.onShut = func(p gtk.Popover) {
		p.Ref() // keep it alive if its head disappears before the idle callback
		gtkx.Post(func() {
			p.Unparent()
			p.Unref()
		})
	}
}

func (c *columns) menuSessionHead(head *gtk.Button) {
	click := gtk.NewGestureClick()
	click.SetButton(3)
	click.ConnectPressed(&c.sessionMenu.onPress)
	head.AddController(&click.EventController)
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(&c.sessionMenu.onKey)
	head.AddController(&keys.EventController)
}

// Resolve the head once; neither action follows the active session.
func (c *columns) showSessionMenu(a *App, w *gtk.Widget, at *gdk.Rectangle) {
	if w == nil || a.state == nil || a.sheet != nil {
		return
	}
	spot, ok := c.clickAt[w.GoPointer()]
	if !ok || spot.pane != 0 {
		return
	}
	id := spot.session
	if s, _ := a.state.Model.Session(id); s == nil {
		return
	}
	pop := uikit.NewMenu([]uikit.Item{
		{Label: i18n.T("Rename…"), Run: func() { a.renameSession(id) }},
		{Label: i18n.T("Close…"), Run: func() { a.closeSession(id) }},
	}, uikit.AtPointer)
	pop.SetPosition(gtk.PosBottomValue)
	if at != nil {
		pop.SetPointingTo(at)
	}
	pop.SetParent(w)
	pop.ConnectClosed(&c.sessionMenu.onShut)
	pop.Unref() // the head holds it
	pop.Popup()
}

// Closing even a single-pane session always asks first.
func (a *App) closeSession(id int) {
	if a.state == nil || a.sheet != nil {
		return
	}
	s, _ := a.state.Model.Session(id)
	if s == nil {
		return
	}
	sheet, done := a.sheetFrame(i18n.Tf("Close %s?", s.Name), 420)
	why := gtk.NewLabel(i18n.T("All panes in this session close, and every program running in them quits."))
	why.AddCssClass("term-sheet-why")
	why.SetXalign(0)
	why.SetWrap(true)
	gtkx.Append(sheet, &why.Widget)
	buttons := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	buttons.SetHalign(gtk.AlignEndValue)
	cancel := gtk.NewButtonWithLabel(i18n.T("Cancel"))
	cancel.AddCssClass("outlined")
	ok := gtk.NewButtonWithLabel(i18n.T("Close"))
	ok.AddCssClass("destructive-action")
	gtkx.Append(buttons, &cancel.Widget)
	gtkx.Append(buttons, &ok.Widget)
	gtkx.Append(sheet, &buttons.Widget)
	a.onCloseCancel = func(gtk.Button) { done() }
	a.onCloseOK = func(gtk.Button) {
		done()
		if a.state == nil {
			return
		}
		if s, _ := a.state.Model.Session(id); s == nil {
			return
		}
		a.ask("close session", func(c *host.Client) error { return c.CloseSession(id) })
	}
	cancel.ConnectClicked(&a.onCloseCancel)
	ok.ConnectClicked(&a.onCloseOK)
	cancel.GrabFocus()
}
