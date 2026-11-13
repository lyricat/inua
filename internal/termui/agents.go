package termui

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/agent"
	"github.com/lyricat/inua/internal/term/host"
	"github.com/lyricat/inua/internal/term/model"
)

// How the window shows agents (section 2): a 7 px square, filled in the
// state's colour while on and hollow when quiet, and the state in mono
// small capitals; the agent's own mark; the time in the state, counting
// up while it works. A blocked agent's row shows what it asks.

// look is how a pane's state shows: its label, its colour class (s-on,
// s-warn, s-ok, or "" for the quiet t2) and whether its square is filled.
type look struct {
	label  string
	class  string
	filled bool
}

func lookOf(info host.PaneInfo) look {
	switch {
	case info.Exited:
		return look{label: i18n.Tf("EXITED %d", info.Code)}
	case info.Restored:
		return look{label: i18n.T("RESTORED")}
	}
	switch agent.State(info.State) {
	case agent.Working:
		return look{i18n.T("WORKING"), "s-on", true}
	case agent.Blocked:
		return look{i18n.T("NEEDS YOU"), "s-warn", true}
	case agent.Done:
		return look{i18n.T("DONE"), "s-ok", true}
	}
	return look{label: i18n.T("IDLE")}
}

// paint gives a mark and a label a look: the mark the shared state of the
// same name (on, warn, ok), the label the colour class.
func paint(mark *gtk.Box, label *gtk.Label, l look) {
	for _, c := range []string{"s-on", "s-warn", "s-ok"} {
		mark.RemoveCssClass(strings.TrimPrefix(c, "s-"))
		if label != nil {
			label.RemoveCssClass(c)
		}
	}
	if l.filled && l.class != "" {
		mark.AddCssClass(strings.TrimPrefix(l.class, "s-"))
	}
	if label != nil {
		label.SetText(l.label)
		if l.class != "" {
			label.AddCssClass(l.class)
		}
	}
}

// iconOf is a pane's mark: its agent's, or the terminal's.
// An agent may have its own colours, NAME-light and NAME-dark for light
// and dark themes (Mister Morph's), shown instead of its one-colour mark.
func (a *App) iconOf(info host.PaneInfo) string {
	if info.Icon == "" {
		return "agent-shell-symbolic"
	}
	own := info.Icon + "-light"
	if parseHex(a.c.Colors["app"]).luma() < 0.5 {
		own = info.Icon + "-dark"
	}
	if gtk.IconThemeGetForDisplay(gdk.DisplayGetDefault()).HasIcon(own) {
		return own
	}
	return info.Icon + "-symbolic"
}

// brands colour the agents' marks, each its manifest's colour (a class of
// its own, term-brand-TYPE).
type brands struct {
	css   *gtk.CssProvider
	known map[string]string // colour by agent type
}

// brand gives a pane's mark its agent's colour while the agent runs,
// whatever it does; a shell's, or an agent's without a colour, keeps the
// mark's own.
func (a *App) brand(icon *gtk.Image, info host.PaneInfo) {
	classes := slices.DeleteFunc(icon.GetCssClasses(), func(c string) bool { return strings.HasPrefix(c, "term-brand") })
	if name := cssName(info.Agent); name != "" && info.Color != "" && !info.Exited {
		a.brands.add(name, info.Color)
		classes = append(classes, "term-brand", "term-brand-"+name)
	}
	icon.SetCssClasses(classes)
}

// add has the style colour an agent type's marks, once.
func (b *brands) add(name, color string) {
	if b.known[name] == color {
		return
	}
	if b.css == nil {
		b.css, b.known = gtk.NewCssProvider(), map[string]string{}
		gtk.StyleContextAddProviderForDisplay(gdk.DisplayGetDefault(), b.css, uint32(gtk.STYLE_PROVIDER_PRIORITY_APPLICATION))
	}
	b.known[name] = color
	var css strings.Builder
	for _, n := range slices.Sorted(maps.Keys(b.known)) {
		fmt.Fprintf(&css, "window.term image.term-brand.term-brand-%s { color: %s; }\n", n, b.known[n])
	}
	b.css.LoadFromString(css.String())
}

// cssName is an agent type as a class name may have it: letters, digits
// and dashes; "" for none left.
func cssName(t string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			return r
		case r >= 'A' && r <= 'Z':
			return r + 'a' - 'A'
		}
		return -1
	}, t)
}

// since is how long a pane has been in its state: "42s", "2m 14s",
// "1h 5m", "3d".
func since(ms int64, now time.Time) string {
	if ms == 0 {
		return ""
	}
	d := now.Sub(time.UnixMilli(ms))
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", max(int(d.Seconds()), 0))
	case d < time.Hour:
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// rollup is what a set of panes adds up to: how many agents wait for
// you, and the look of their square (warn while one waits, the accent
// while one works, hollow when quiet).
func (a *App) rollup(panes []int) (blocked int, l look) {
	working := false
	for _, id := range panes {
		switch agent.State(a.info[id].State) {
		case agent.Blocked:
			blocked++
		case agent.Working:
			working = true
		}
	}
	switch {
	case blocked > 0:
		return blocked, look{class: "s-warn", filled: true}
	case working:
		return 0, look{class: "s-on", filled: true}
	}
	return 0, look{}
}

func workspacePanes(w *model.Workspace) []int {
	var out []int
	for _, s := range w.Sessions {
		out = append(out, s.Tree.Panes()...)
	}
	return out
}

// newBadge is a count of agents waiting, in warn, hidden until set.
func newBadge() *gtk.Label {
	b := gtk.NewLabel("")
	b.AddCssClass("term-badge")
	b.SetValign(gtk.AlignCenterValue)
	b.SetTooltipText(i18n.T("Agents that need you"))
	b.SetVisible(false)
	return b
}

// setBadge shows n on a badge, hidden for none.
func setBadge(b *gtk.Label, n int) {
	b.SetVisible(n > 0)
	b.SetText(fmt.Sprint(n))
}

// agents are every pane with an agent, as column 1 and 2 order them,
// with their workspace and session.
func (a *App) agents() []agentAt {
	var out []agentAt
	if a.state == nil {
		return nil
	}
	for _, w := range a.state.Model.Workspaces {
		for _, s := range w.Sessions {
			for _, id := range s.Tree.Panes() {
				if a.info[id].Agent != "" {
					out = append(out, agentAt{w, s, id})
				}
			}
		}
	}
	return out
}

type agentAt struct {
	w    *model.Workspace
	s    *model.Session
	pane int
}

// nextWaiting shows the next agent that needs you after the focused
// pane, in any workspace (Ctrl+Shift+J); then the next one done.
func (a *App) nextWaiting() { a.nextIn(agent.Blocked, agent.Done) }

// nextIdle is Warcraft's idle worker: the next agent that needs you, then
// one done, then one idle, after the one shown (Ctrl+`).
func (a *App) nextIdle() { a.nextIn(agent.Blocked, agent.Done, agent.Idle) }

// nextIn shows the next agent in the first of states any is in, after
// the pane shown, in column 1 and 2's order.
func (a *App) nextIn(states ...agent.State) {
	all := a.agents()
	cur := -1
	if s := a.shown(); s != nil {
		for i, x := range all {
			if x.pane == s.Focus {
				cur = i
			}
		}
	}
	for _, want := range states {
		for k := 1; k <= len(all); k++ {
			x := all[(cur+k+len(all))%len(all)]
			if agent.State(a.info[x.pane].State) == want {
				a.showPane(x.w.ID, x.s.ID, x.pane)
				return
			}
		}
	}
}

// startTicks counts the times in state up once a second, while the window
// shows any.
func (a *App) startTicks() {
	a.onTick = func(uintptr) bool {
		if a.state == nil {
			return true
		}
		a.cols.updateTimes(a)
		return true
	}
	glibTimeout(1000, &a.onTick)
}

// overview lists every agent in every workspace (Ctrl+Shift+A); Enter or
// a click shows one.
func (a *App) overview() {
	if a.sheet != nil {
		return
	}
	all := a.agents()
	sheet := gtk.NewBox(gtk.OrientationVerticalValue, 10)
	sheet.AddCssClass("term-sheet")
	sheet.AddCssClass("term-overview")
	sheet.SetHalign(gtk.AlignCenterValue)
	sheet.SetValign(gtk.AlignCenterValue)
	sheet.SetSizeRequest(520, -1)
	title := gtk.NewLabel(i18n.T("Agents"))
	title.AddCssClass("term-sheet-title")
	title.SetXalign(0)
	sheet.Append(&title.Widget)
	list := gtk.NewListBox()
	list.AddCssClass("term-overview-list")
	if len(all) == 0 {
		none := gtk.NewLabel(i18n.T("No agent runs in any workspace."))
		none.AddCssClass("term-sheet-why")
		none.SetXalign(0)
		sheet.Append(&none.Widget)
	}
	for _, x := range all {
		info := a.info[x.pane]
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		row.AddCssClass("term-overview-row")
		icon := gtk.NewImageFromIconName(a.iconOf(info))
		icon.SetPixelSize(15)
		a.brand(icon, info)
		row.Append(&icon.Widget)
		text := gtk.NewBox(gtk.OrientationVerticalValue, 2)
		text.SetHexpand(true)
		t := gtk.NewLabel(paneTitle(info))
		t.SetXalign(0)
		t.SetEllipsize(3)
		where := gtk.NewLabel(x.w.Name + " · " + x.s.Name)
		where.AddCssClass("term-pane-dir")
		where.SetXalign(0)
		text.Append(&t.Widget)
		text.Append(&where.Widget)
		row.Append(&text.Widget)
		mark := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		mark.AddCssClass("mark")
		mark.SetValign(gtk.AlignCenterValue)
		state := gtk.NewLabel("")
		state.AddCssClass("term-state")
		paint(mark, state, lookOf(info))
		row.Append(&mark.Widget)
		row.Append(&state.Widget)
		list.Append(&row.Widget)
	}
	sheet.Append(&list.Widget)
	done := func() {
		a.sheets.RemoveOverlay(&sheet.Widget)
		a.sheet = nil
		if s := a.shown(); s != nil {
			if t := a.tiles[s.Focus]; t != nil {
				t.pane.widget.GrabFocus()
			}
		}
	}
	a.onOverviewRow = func(_ gtk.ListBox, row uintptr) {
		i := int(gtk.ListBoxRowNewFromInternalPtr(row).GetIndex())
		done()
		if i >= 0 && i < len(all) {
			a.showPane(all[i].w.ID, all[i].s.ID, all[i].pane)
		}
	}
	list.ConnectRowActivated(&a.onOverviewRow)
	keys := gtk.NewEventControllerKey()
	a.onOverviewKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		if int32(keyval) == gdk.KEY_Escape {
			done()
			return true
		}
		return false
	}
	keys.ConnectKeyPressed(&a.onOverviewKey)
	sheet.AddController(&keys.EventController)
	a.sheet = sheet
	a.sheets.AddOverlay(&sheet.Widget)
	if r := list.GetRowAtIndex(0); r != nil {
		r.GrabFocus()
	} else {
		sheet.GrabFocus()
	}
}

func glibTimeout(ms uint32, f *glib.SourceFunc) { glib.TimeoutAdd(ms, f, 0) }
