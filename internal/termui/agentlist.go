package termui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/agent"
	"github.com/lyricat/inua/internal/uikit"
)

// The agents at the foot of column 1, its bottom left, as the web
// terminal's Agents tab lists them: every agent running, in three groups
// that fold as an accordion (one open at a time, or none), each under a
// head with its square, its name and how many it holds. Those that need
// you or are done first, open; those working and those idle after,
// folded. A row is a pane's row of column 2, and a click shows the pane.

// agentGroups are the groups, in order, with the states each holds.
var agentGroups = [3][]agent.State{
	{agent.Blocked, agent.Done},
	{agent.Working},
	{agent.Idle},
}

type agentList struct {
	scroll *gtk.ScrolledWindow
	groups [3]agentGroup
	// shape is the panes of each group the rows were made for, the
	// groups apart by -1; rows their widgets, by pane.
	shape []int
	rows  map[int]*paneRowWidgets
	// at is the agent each row stands for, by its button; heads the
	// group of each head, by its button.
	at            map[uintptr]agentAt
	heads         map[uintptr]int
	onHead, onRow func(gtk.Button)
	times         []timed
}

type agentGroup struct {
	head   *gtk.Button
	mark   *gtk.Box
	label  *gtk.Label
	count  *gtk.Label
	caret  *gtk.Image
	reveal *gtk.Revealer
	rows   *gtk.Box
	empty  *gtk.Box // what an open group shows with no agent in it
}

func (l *agentList) build(a *App, ui *gtkx.Builder) {
	l.scroll = &gtk.ScrolledWindow{}
	box := &gtk.Box{}
	ui.Get("agents_scroll", l.scroll)
	ui.Get("agents", box)
	l.heads, l.at = map[uintptr]int{}, map[uintptr]agentAt{}
	names := [3]string{i18n.T("Needs you"), i18n.T("Working"), i18n.T("Idle")}
	empty := [3]string{
		i18n.T("Nobody needs you. Stretch a little."),
		i18n.T("Nothing is cooking."),
		i18n.T("Nobody is slacking off."),
	}
	for i := range l.groups {
		g := &l.groups[i]
		g.head = gtk.NewButton()
		g.head.AddCssClass("term-agents-head")
		hb := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		g.mark = gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		g.mark.AddCssClass("mark")
		g.mark.SetValign(gtk.AlignCenterValue)
		hb.Append(&g.mark.Widget)
		g.label = gtk.NewLabel("")
		g.label.SetUseMarkup(true)
		g.label.SetMarkup(uikit.Bracketed(strings.ToUpper(names[i])))
		g.label.AddCssClass("term-session-label")
		g.label.SetEllipsize(3)
		g.label.SetXalign(0)
		g.label.SetHexpand(true)
		hb.Append(&g.label.Widget)
		g.count = gtk.NewLabel("")
		g.count.AddCssClass("term-agents-count")
		hb.Append(&g.count.Widget)
		g.caret = gtk.NewImage()
		g.caret.AddCssClass("term-agents-caret")
		hb.Append(&g.caret.Widget)
		g.head.SetChild(&hb.Widget)
		l.heads[g.head.GoPointer()] = i
		g.head.ConnectClicked(&l.onHead)
		box.Append(&g.head.Widget)
		body := gtk.NewBox(gtk.OrientationVerticalValue, 0)
		body.AddCssClass("term-agents-rows")
		g.empty = emptyRow(emptyIcons[i], empty[i])
		body.Append(&g.empty.Widget)
		g.rows = gtk.NewBox(gtk.OrientationVerticalValue, 0)
		body.Append(&g.rows.Widget)
		g.reveal = gtk.NewRevealer()
		g.reveal.SetTransitionType(gtk.RevealerTransitionTypeSlideDownValue)
		g.reveal.SetTransitionDuration(160)
		g.reveal.SetChild(&body.Widget)
		box.Append(&g.reveal.Widget)
		g.setOpen(i == 0)
	}
	l.onHead = func(b gtk.Button) {
		if i, ok := l.heads[b.GoPointer()]; ok {
			open := !l.groups[i].reveal.GetRevealChild()
			for k := range l.groups {
				l.groups[k].setOpen(k == i && open)
			}
		}
	}
	l.onRow = func(b gtk.Button) {
		if x, ok := l.at[b.GoPointer()]; ok {
			a.showPane(x.w.ID, x.s.ID, x.pane)
		}
	}
}

// emptyIcons are the groups' marks when empty: a break, a cold pot, no
// fish (摸鱼, slacking off).
var emptyIcons = [3]string{"term-empty-waiting-symbolic", "term-empty-working-symbolic", "term-empty-idle-symbolic"}

// emptyRow is an empty group's row: a pane row's shape, dashed, its mark
// and words where a row's kind and title are, so the edges line up.
func emptyRow(icon, says string) *gtk.Box {
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
	row.AddCssClass("term-agents-empty")
	// As wide as a row's kind box with its border, and not wider: its
	// mark would have it grow (makePaneRow).
	kind := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
	kind.SetSizeRequest(30, 30)
	kind.SetValign(gtk.AlignCenterValue)
	img := gtk.NewImageFromIconName(icon)
	img.SetPixelSize(15)
	img.SetHexpand(true)
	img.SetHalign(gtk.AlignCenterValue)
	kind.Append(&img.Widget)
	kind.SetHexpand(false)
	row.Append(&kind.Widget)
	label := gtk.NewLabel(says)
	label.AddCssClass("empty")
	label.SetXalign(0)
	label.SetWrap(true)
	label.SetHexpand(true)
	label.SetValign(gtk.AlignCenterValue)
	row.Append(&label.Widget)
	return row
}

// setOpen opens or folds a group, its caret pointing down while open.
func (g *agentGroup) setOpen(open bool) {
	g.reveal.SetRevealChild(open)
	if open {
		g.caret.SetFromIconName("pan-down-symbolic")
		g.head.SetTooltipText(i18n.T("Fold"))
	} else {
		g.caret.SetFromIconName("go-next-symbolic")
		g.head.SetTooltipText(i18n.T("Open"))
	}
}

// grouped is each group's agents, those the search asks for: waiting
// before done, and the latest to change state first in each.
func (l *agentList) grouped(a *App, query string) [3][]agentAt {
	var out [3][]agentAt
	for _, x := range a.agents() {
		info := a.info[x.pane]
		if info.Exited {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(paneTitle(info)), query) &&
			!strings.Contains(strings.ToLower(x.w.Name), query) {
			continue
		}
		st := agent.State(info.State)
		i := slices.IndexFunc(agentGroups[:], func(g []agent.State) bool { return slices.Contains(g, st) })
		if i < 0 {
			i = 2
		}
		out[i] = append(out[i], x)
	}
	for i := range out {
		order := agentGroups[i]
		slices.SortStableFunc(out[i], func(x, y agentAt) int {
			sx, sy := a.info[x.pane], a.info[y.pane]
			if d := slices.Index(order, agent.State(sx.State)) - slices.Index(order, agent.State(sy.State)); d != 0 {
				return d
			}
			return int(sy.Since - sx.Since)
		})
	}
	return out
}

// update lists the agents again; the rows stay while the groups hold
// the same panes in the same order (as column 2's, columns.go).
func (l *agentList) update(a *App, query string) {
	groups := l.grouped(a, query)
	shape := []int{}
	for _, g := range groups {
		for _, x := range g {
			shape = append(shape, x.pane)
		}
		shape = append(shape, -1)
	}
	if !slices.Equal(shape, l.shape) {
		l.shape = shape
		l.make(a, groups)
	}
	total := 0
	var focus int
	if s := a.shown(); s != nil {
		focus = s.Focus
	}
	for i, g := range groups {
		total += len(g)
		lg := &l.groups[i]
		lg.count.SetText(fmt.Sprint(len(g)))
		lg.empty.SetVisible(len(g) == 0)
		head := headLook(a, i, g)
		paint(lg.mark, nil, head)
		for _, c := range []string{"s-warn", "s-on", "s-ok"} {
			classIf(&lg.count.Widget, c, i == 0 && head.class == c)
		}
		for _, x := range g {
			a.cols.setPaneRow(a, l.rows[x.pane], x.pane, x.pane == focus)
		}
	}
	l.scroll.SetVisible(total > 0)
}

// headLook is a group's square: the first's in warn while all wait for
// you, in the accent while all are done, in ok while some wait and some
// are done (its count the same colour); the second's in the accent while
// any works; the idle's hollow.
func headLook(a *App, i int, g []agentAt) look {
	switch {
	case len(g) == 0 || i == 2:
		return look{}
	case i == 1:
		return look{class: "s-on", filled: true}
	}
	waiting := 0
	for _, x := range g {
		if agent.State(a.info[x.pane].State) == agent.Blocked {
			waiting++
		}
	}
	switch waiting {
	case len(g):
		return look{class: "s-warn", filled: true}
	case 0:
		return look{class: "s-on", filled: true}
	}
	return look{class: "s-ok", filled: true}
}

// make makes the groups' rows, their content left to set.
func (l *agentList) make(a *App, groups [3][]agentAt) {
	clear(l.at)
	l.rows = map[int]*paneRowWidgets{}
	l.times = l.times[:0]
	for i, g := range groups {
		box := l.groups[i].rows
		for child := box.GetFirstChild(); child != nil; child = box.GetFirstChild() {
			box.Remove(child)
		}
		for _, x := range g {
			r := makePaneRow()
			r.button.SetTooltipText(x.w.Name + " › " + x.s.Name)
			l.rows[x.pane] = r
			l.at[r.button.GoPointer()] = x
			l.times = append(l.times, timed{r.right, x.pane})
			r.button.ConnectClicked(&l.onRow)
			box.Append(&r.button.Widget)
		}
	}
}

// updateTimes counts the agents' times in state on.
func (l *agentList) updateTimes(a *App, now time.Time) {
	for _, t := range l.times {
		if info := a.info[t.pane]; info.Agent != "" {
			t.label.SetText(since(info.Since, now))
		}
	}
}
