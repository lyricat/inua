package termui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/agent"
	"github.com/lyricat/inua/internal/term/host"
	"github.com/lyricat/inua/internal/term/model"
	"github.com/lyricat/inua/internal/uikit"
)

// Column 1 lists the workspaces (name, folder, the state of what runs in
// them); column 2 the shown workspace's sessions, each a section head
// over its panes. Both follow each State (fillWorkspaces, fillSessions).

type columns struct {
	col1, col2 *gtk.Box
	// wsScroll holds the workspaces: it takes column 1's height while
	// no agent runs, and its rows' height, to its most, while the agents
	// take the rest.
	wsScroll *gtk.ScrolledWindow
	list     *gtk.ListBox
	sessions *gtk.Box
	find     *gtk.SearchEntry
	// The head's menu and new workspace buttons, and the new session
	// after the sessions.
	menu, newWorkspace, newSession *gtk.Button
	// The grips that set the columns' widths (colwidth.go), the widths,
	// and the width of the column dragged when the drag began.
	grips                                [2]*gtk.Box
	width                                [2]int
	gripFrom                             int
	gripX                                float64 // where the drag began, in the window
	onGripBegin, onGripUpdate, onGripEnd [2]func(gtk.GestureDrag, float64, float64)
	// Column 1 as it slides, and its toggles: in its head, and at the
	// top of column 2 while it is hidden.
	col1Reveal               *gtk.Revealer
	sidebarHide, sidebarShow *gtk.Button
	sidebarShowHead          *gtk.Box
	onSidebar                func(gtk.Button)
	onSlid                   func(uintptr, uintptr, uintptr)
	// held is set while the terminals keep their sizes (holdSizes).
	held bool
	// The search's own row, and the button in the head that opens it.
	findReveal   *gtk.Revealer
	findButton   *gtk.ToggleButton
	onFindButton func(gtk.ToggleButton)
	// rowIDs is the workspace of each row of the list, in order, and
	// wsRows their widgets.
	rowIDs []int
	wsRows []wsRow
	// sessionShape is what column 2 was made for (fillSessions); heads
	// and paneRows its widgets, by session and by pane.
	sessionShape []int
	heads        map[int]*sessionHead
	paneRows     map[int]*paneRowWidgets
	// filling is set while the list is made, so selecting a row then
	// does not show it.
	filling bool
	// agents is the foot of column 1: every agent, in groups that fold
	// (agentlist.go).
	agents agentList
	// drag and sessionDrag reorder the two lists; rowMenu and
	// sessionMenu hold their context-menu callbacks.
	sessionDrag sessionDrag
	drag        wsDrag
	rowMenu     wsMenu
	sessionMenu sessionMenu

	onRow          func(gtk.ListBox, uintptr)
	onFind         func(uintptr, uintptr)
	onMenu         func(gtk.Button)
	headMenu       headMenu // the menu button's (headmenu.go)
	onNewWorkspace func(gtk.Button)
	onNewSession   func(gtk.Button)
	// clickAt is what each session head and pane row of column 2 shows,
	// by its button: the two click callbacks are the same for all (one
	// made for each row would never be freed).
	clickAt                  map[uintptr]dropSpot
	onHeadClick, onPaneClick func(gtk.Button)
	// lastHead is the session head clicked last, and when: a second
	// click on it soon after renames it (headClicked).
	lastHead   int
	lastHeadAt time.Time
	// times are the labels of the time in state, by pane.
	times []timed
}

type timed struct {
	label *gtk.Label
	pane  int
}

// updateTimes counts the agents' times in state on.
func (c *columns) updateTimes(a *App) {
	now := time.Now()
	for _, t := range c.times {
		if info := a.info[t.pane]; info.Agent != "" {
			t.label.SetText(since(info.Since, now))
		}
	}
	c.agents.updateTimes(a, now)
}

func (a *App) buildColumns(ui *gtkx.Builder) {
	c := &a.cols
	c.col1, c.col2 = &gtk.Box{}, &gtk.Box{}
	c.list, c.sessions, c.find = &gtk.ListBox{}, &gtk.Box{}, &gtk.SearchEntry{}
	ui.Get("workspaces_col", c.col1)
	ui.Get("sessions_col", c.col2)
	ui.Get("workspaces", c.list)
	c.wsScroll = &gtk.ScrolledWindow{}
	ui.Get("workspaces_scroll", c.wsScroll)
	ui.Get("sessions", c.sessions)
	ui.Get("find", c.find)
	c.menu, c.newWorkspace, c.newSession = &gtk.Button{}, &gtk.Button{}, &gtk.Button{}
	ui.Get("menu", c.menu)
	ui.Get("new_workspace", c.newWorkspace)
	ui.Get("new_session", c.newSession)
	a.columnGrips(ui)
	c.tips(a)
	c.onRow = func(_ gtk.ListBox, row uintptr) {
		i := int(gtk.ListBoxRowNewFromInternalPtr(row).GetIndex())
		if c.filling || i < 0 || i >= len(c.rowIDs) {
			return
		}
		a.showWorkspace(c.rowIDs[i])
	}
	c.list.ConnectRowActivated(&c.onRow)
	c.startDrag(a)
	c.startSessionDrag(a)
	c.startMenu(a)
	c.startSessionMenu(a)
	c.agents.build(a, ui)
	c.onFind = func(uintptr, uintptr) { c.update(a) }
	gtkx.ConnectChanged(c.find, &c.onFind)
	c.onMenu = func(gtk.Button) { a.showHeadMenu() }
	c.menu.ConnectClicked(&c.onMenu)
	// A session head or pane row clicked: what it stands for, by its
	// button (makeSessions).
	clicked := func(b gtk.Button) (ws int, spot dropSpot, ok bool) {
		spot, ok = c.clickAt[b.GoPointer()]
		if !ok || a.state == nil {
			return 0, spot, false
		}
		if _, w := a.state.Model.Session(spot.session); w != nil {
			return w.ID, spot, true
		}
		return 0, spot, false
	}
	c.onHeadClick = func(b gtk.Button) {
		if ws, spot, ok := clicked(b); ok {
			c.headClicked(a, ws, spot.session)
		}
	}
	c.onPaneClick = func(b gtk.Button) {
		if ws, spot, ok := clicked(b); ok {
			a.showPane(ws, spot.session, spot.pane)
		}
	}
	c.onNewWorkspace = func(gtk.Button) { a.newWorkspace() }
	c.newWorkspace.ConnectClicked(&c.onNewWorkspace)
	c.onNewSession = func(gtk.Button) { a.newSession() }
	c.newSession.ConnectClicked(&c.onNewSession)
}

// tips name the head buttons' actions, with their keys as set.
func (c *columns) tips(a *App) {
	tip := func(b *gtk.Button, label, action string) {
		if k := a.set.Keys[action]; k != "" {
			label += " (" + keycaps(k) + ")"
		}
		b.SetTooltipText(label)
	}
	tip(c.menu, i18n.T("Menu"), "")
	tip(c.newWorkspace, i18n.T("New workspace"), "new_workspace")
	tip(c.newSession, i18n.T("New session"), "new_session")
	tip(c.sidebarHide, i18n.T("Hide the sidebar"), "fold_workspaces")
	tip(c.sidebarShow, i18n.T("Show the sidebar"), "fold_workspaces")
}

// update makes both columns from the State.
func (c *columns) update(a *App) {
	if a.state == nil {
		return
	}
	c.fillWorkspaces(a)
	c.fillSessions(a)
	c.agents.update(a, strings.ToLower(strings.TrimSpace(c.find.GetText())))
	c.wsScroll.SetVexpand(!c.agents.scroll.GetVisible())
}

// The rows stay while the lists keep their shape (the same workspaces,
// sessions and panes, in the same order): an update sets their text,
// marks and classes. An agent changes its title many times a second, and
// a row made anew under the pointer lost its hover, and a click pressed
// on the old one and released on the new.

// wsRow is a workspace's row in column 1.
// Its folder is its tooltip: one line keeps the rows low, for the agents
// under them.
type wsRow struct {
	box  *gtk.Box
	name *gtk.Label
	mark *gtk.Box
}

// sessionHead is a session's head in column 2.
type sessionHead struct {
	section      *gtk.Box
	button       *gtk.Button
	mark         *gtk.Box
	label, badge *gtk.Label
}

// paneRowWidgets is a pane's row in column 2.
type paneRowWidgets struct {
	button              *gtk.Button
	icon                *gtk.Image
	mark                *gtk.Box
	title, state, right *gtk.Label
}

func (c *columns) fillWorkspaces(a *App) {
	m := &a.state.Model
	query := strings.ToLower(strings.TrimSpace(c.find.GetText()))
	var ws []*model.Workspace
	shape := []int{}
	for _, w := range m.Workspaces {
		if query == "" || a.matches(w, query) {
			ws = append(ws, w)
			shape = append(shape, w.ID)
		}
	}
	c.filling = true
	defer func() { c.filling = false }()
	if !slices.Equal(shape, c.rowIDs) || len(c.wsRows) != len(ws) {
		c.list.RemoveAll()
		c.rowIDs, c.wsRows = shape, make([]wsRow, len(ws))
		for i := range ws {
			c.list.Append(&c.wsRows[i].make().Widget)
		}
		c.dragRows(query != "")
		for i := range c.rowIDs {
			if row := c.list.GetRowAtIndex(int32(i)); row != nil {
				c.menuRow(row)
			}
		}
		// Made anew mid-drag (a workspace came or went): the row dragged
		// is away still.
		if d := &c.drag; d.id != 0 {
			d.from = slices.Index(c.rowIDs, d.id)
			if row := c.list.GetRowAtIndex(int32(d.from)); row != nil {
				row.SetVisible(false)
			}
		}
	}
	for i, w := range ws {
		r := &c.wsRows[i]
		r.name.SetText(w.Name)
		r.box.SetTooltipText(shortDir(w.Dir))
		_, l := a.rollup(workspacePanes(w))
		paint(r.mark, nil, l)
		if w.ID == m.Shown.Workspace {
			if row := c.list.GetRowAtIndex(int32(i)); row != nil && !row.IsSelected() {
				c.list.SelectRow(row)
			}
		}
	}
}

// make is a workspace row's widgets, their content left to set.
func (r *wsRow) make() *gtk.Box {
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	row.AddCssClass("term-ws")
	r.box = row
	r.name = gtk.NewLabel("")
	r.name.AddCssClass("term-ws-name")
	r.name.SetXalign(0)
	r.name.SetEllipsize(3)
	r.name.SetHexpand(true)
	row.Append(&r.name.Widget)
	r.mark = gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	r.mark.AddCssClass("mark")
	r.mark.SetValign(gtk.AlignCenterValue)
	row.Append(&r.mark.Widget)
	return row
}

// classIf adds or takes away a widget's class.
func classIf(w *gtk.Widget, class string, on bool) {
	if on {
		w.AddCssClass(class)
	} else {
		w.RemoveCssClass(class)
	}
}

// matches says a workspace is what the search asks for: its name, its
// folder, or a pane title in it.
func (a *App) matches(w *model.Workspace, query string) bool {
	if strings.Contains(strings.ToLower(w.Name), query) || strings.Contains(strings.ToLower(w.Dir), query) {
		return true
	}
	for _, s := range w.Sessions {
		for _, id := range s.Tree.Panes() {
			if strings.Contains(strings.ToLower(paneTitle(a.info[id])), query) {
				return true
			}
		}
	}
	return false
}

func (c *columns) fillSessions(a *App) {
	if c.sessionDrag.id != 0 && strings.TrimSpace(c.find.GetText()) != "" {
		c.cancelSessionDrag()
	}
	w := c.sessionDrag.preview(a.shownWorkspace())
	c.newSession.SetVisible(w != nil)
	// The shape: the workspace, then each session and its panes.
	shape := []int{}
	if w != nil {
		shape = append(shape, w.ID)
		for _, s := range w.Sessions {
			shape = append(shape, -s.ID)
			shape = append(shape, s.Tree.Panes()...)
		}
	}
	if !slices.Equal(shape, c.sessionShape) {
		c.cancelSessionDrag()
		c.sessionShape = shape
		c.makeSessions(a, w)
	}
	if w == nil {
		return
	}
	shown := a.state.Model.Shown.Session
	for _, s := range w.Sessions {
		h := c.heads[s.ID]
		classIf(&h.button.Widget, "current", s.ID == shown)
		blocked, l := a.rollup(s.Tree.Panes())
		paint(h.mark, nil, l)
		h.label.SetMarkup(uikit.Bracketed(strings.ToUpper(s.Name)))
		setBadge(h.badge, blocked)
		for _, id := range s.Tree.Panes() {
			c.setPaneRow(a, c.paneRows[id], id, s.ID == shown && s.Focus == id)
		}
	}
}

// makeSessions makes column 2's heads and rows for a workspace's sessions,
// their content left to set.
func (c *columns) makeSessions(a *App, w *model.Workspace) {
	c.sessionDrag.rows = nil
	c.sessionDrag.gaps = nil
	for child := c.sessions.GetFirstChild(); child != nil; child = c.sessions.GetFirstChild() {
		c.sessions.Remove(child)
	}
	c.clickAt = map[uintptr]dropSpot{}
	c.times = c.times[:0]
	a.paneDrag.lit, a.paneDrag.spots = nil, map[uintptr]dropSpot{}
	c.heads, c.paneRows = map[int]*sessionHead{}, map[int]*paneRowWidgets{}
	if w == nil {
		return
	}
	for _, s := range w.Sessions {
		section := gtk.NewBox(gtk.OrientationVerticalValue, 0)
		section.AddCssClass("term-session")
		h := &sessionHead{button: gtk.NewButton(), section: section}
		h.button.AddCssClass("term-session-head")
		hb := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		h.mark = gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		h.mark.AddCssClass("mark")
		h.mark.SetValign(gtk.AlignCenterValue)
		hb.Append(&h.mark.Widget)
		h.label = gtk.NewLabel("")
		h.label.SetUseMarkup(true)
		h.label.AddCssClass("term-session-label")
		h.label.SetEllipsize(3)
		h.label.SetXalign(0)
		h.label.SetHexpand(true)
		hb.Append(&h.label.Widget)
		h.badge = newBadge()
		hb.Append(&h.badge.Widget)
		h.button.SetChild(&hb.Widget)
		c.clickAt[h.button.GoPointer()] = dropSpot{s.ID, 0}
		h.button.ConnectClicked(&c.onHeadClick)
		c.menuSessionHead(h.button)
		a.dropOn(&h.button.Widget, s.ID, 0)
		section.Append(&h.button.Widget)
		c.heads[s.ID] = h
		c.dragSessionHead(h)
		for _, id := range s.Tree.Panes() {
			r := makePaneRow()
			c.paneRows[id] = r
			c.times = append(c.times, timed{r.right, id})
			c.clickAt[r.button.GoPointer()] = dropSpot{s.ID, id}
			r.button.ConnectClicked(&c.onPaneClick)
			a.dropOn(&r.button.Widget, s.ID, id)
			section.Append(&r.button.Widget)
		}
		c.sessions.Append(&section.Widget)
		c.sessionDrag.rows = append(c.sessionDrag.rows, s.ID)
		c.sessionDrag.gaps = append(c.sessionDrag.gaps, wsGap{})
	}
}

// makePaneRow is a pane's row: its kind, its title, its state and folder,
// their content left to set.
func makePaneRow() *paneRowWidgets {
	r := &paneRowWidgets{button: gtk.NewButton()}
	r.button.AddCssClass("term-pane-row")
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	kind := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	kind.AddCssClass("term-kind-box")
	r.icon = gtk.NewImage()
	r.icon.SetPixelSize(15)
	r.icon.SetHexpand(true)
	r.icon.SetHalign(gtk.AlignCenterValue)
	kind.Append(&r.icon.Widget)
	kind.SetValign(gtk.AlignCenterValue)
	kind.SetHexpand(false)
	kind.SetSizeRequest(28, 28)
	row.Append(&kind.Widget)
	text := gtk.NewBox(gtk.OrientationVerticalValue, 3)
	text.SetHexpand(true)
	r.title = gtk.NewLabel("")
	r.title.AddCssClass("term-pane-title")
	r.title.SetXalign(0)
	r.title.SetEllipsize(3)
	text.Append(&r.title.Widget)
	line := gtk.NewBox(gtk.OrientationHorizontalValue, 6)
	r.mark = gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	r.mark.AddCssClass("mark")
	r.mark.SetValign(gtk.AlignCenterValue)
	line.Append(&r.mark.Widget)
	r.state = gtk.NewLabel("")
	r.state.AddCssClass("term-state")
	r.state.SetXalign(0)
	line.Append(&r.state.Widget)
	r.right = gtk.NewLabel("")
	r.right.AddCssClass("term-pane-dir")
	r.right.SetHexpand(true)
	r.right.SetXalign(1)
	r.right.SetEllipsize(1)
	line.Append(&r.right.Widget)
	text.Append(&line.Widget)
	row.Append(&text.Widget)
	r.button.SetChild(&row.Widget)
	return r
}

// setPaneRow shows a pane's info on its row; current is set for the
// pane with the focus in the session shown.
func (c *columns) setPaneRow(a *App, r *paneRowWidgets, id int, current bool) {
	info := a.info[id]
	classIf(&r.button.Widget, "current", current)
	r.icon.SetFromIconName(a.iconOf(info))
	a.brand(r.icon, info)
	r.title.SetText(paneTitle(info))
	paint(r.mark, r.state, lookOf(info))
	// An agent waiting for you: what it asks, in the state's place.
	asks := agent.State(info.State) == agent.Blocked && info.Ask != ""
	classIf(&r.button.Widget, "blocked", asks)
	classIf(&r.state.Widget, "term-ask", asks)
	r.state.SetHexpand(asks)
	if asks {
		r.state.SetText(info.Ask)
		r.state.SetEllipsize(3)
	} else {
		r.state.SetEllipsize(0)
	}
	if info.Agent != "" {
		r.right.SetText(since(info.Since, time.Now()))
	} else {
		r.right.SetText(shortDir(info.Dir))
	}
}

// showWorkspace shows a workspace, at the session it last showed.
func (a *App) showWorkspace(id int) {
	a.ask("show", func(c *host.Client) error { return c.Show(id, 0) })
}

func (a *App) showSession(ws, session int) {
	a.ask("show", func(c *host.Client) error { return c.Show(ws, session) })
}

// showPane shows a pane's session and focuses it.
func (a *App) showPane(ws, session, pane int) {
	a.ask("show", func(c *host.Client) error {
		if err := c.Show(ws, session); err != nil {
			return err
		}
		return c.Layout(session, host.OpFocus, host.Message{Pane: pane})
	})
}

// shortDir is a folder with the home folder as ~.
func shortDir(dir string) string {
	if dir == "" {
		return ""
	}
	if home, err := os.UserHomeDir(); err == nil {
		if dir == home {
			return "~"
		}
		if rel, ok := strings.CutPrefix(dir, home+string(filepath.Separator)); ok {
			return "~/" + rel
		}
	}
	return dir
}

func baseName(p string) string { return filepath.Base(p) }

// escape is text for Pango markup.
func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "'", "&apos;", `"`, "&quot;").Replace(s)
}

// headClicked shows a session; a second click on its head within the
// double-click time renames it. The clicks are timed here: showing the
// session makes column 2 anew, so the second click falls on a new head,
// which GTK counts as a first.
func (c *columns) headClicked(a *App, ws, session int) {
	within, _ := doubleClick()
	if c.lastHead == session && time.Since(c.lastHeadAt) < within {
		c.lastHead = 0
		a.renameSession(session)
		return
	}
	c.lastHead, c.lastHeadAt = session, time.Now()
	a.showSession(ws, session)
}
