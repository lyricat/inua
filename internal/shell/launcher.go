// Package shell holds the shell's state and rules, apart from any toolkit:
// what the launcher shows and does, how the bar summarizes compositor and
// system state. The GTK views in internal/ui only turn this state into
// widgets and feed events back. Everything here runs on the main thread.
package shell

import (
	"cmp"
	"slices"
	"strings"

	"github.com/lyricat/inua/internal/compositor"
	"github.com/lyricat/inua/internal/convert"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/i18n"
)

// Mode is what the launcher searches.
type Mode int

const (
	Apps Mode = iota
	Windows
)

// ParseMode accepts the names used on the control socket.
func ParseMode(s string) (Mode, bool) {
	switch s {
	case "apps":
		return Apps, true
	case "windows":
		return Windows, true
	}
	return 0, false
}

func (m Mode) String() string {
	if m == Windows {
		return "windows"
	}
	return "apps"
}

// Tag is the mode as a bracketed label in the launcher's head: [APPS].
func (m Mode) Tag() string {
	if m == Windows {
		return i18n.T("[WINDOWS]")
	}
	return i18n.T("[APPS]")
}

// Result is one launcher row: an app to launch, a window to focus, an
// answer or emoji to copy, a command to run, a web address to open, or a
// question for the assistant.
type Result struct {
	Title, Detail string
	Icon          string         // icon name: the app's icon, or the window class
	Glyph         string         // shown instead of the icon: an emoji or symbol
	Entry         *desktop.Entry // set for apps
	Address       string         // set for windows
	Copy          string         // set for answers and emoji: what Enter copies
	Command, Arg  string         // set for commands: Command's ID, and its argument
	URL           string         // set for quicklinks and web searches
	Fill          string         // Enter puts this in the search field instead
	Ask           string         // set for the assistant's row: the question

	score   int
	history int // windows: Hyprland's focus history, 0 = most recent
}

// ActionKind says what the view has to do after a launcher step.
type ActionKind int

const (
	None   ActionKind = iota
	Launch            // start Action.Entry
	Focus             // focuswindow Action.Address
	Copy              // put Action.Text on the clipboard
	Open              // open the web address Action.Text
	Run               // run command Action.Text with Action.Arg, on window Action.Address
	Fill              // put Action.Text in the search field (the query is set already)
	Ask               // ask the assistant Action.Text
)

// Action is a side effect for the view to carry out; the launcher itself
// never talks to Hyprland or starts processes.
type Action struct {
	Kind    ActionKind
	Entry   *desktop.Entry
	Address string
	Text    string
	Arg     string
}

// Launcher is the search interface: one window, two modes. It is open from
// Show until Cancel or a successful Execute.
type Launcher struct {
	Open     bool
	Mode     Mode
	Query    string
	Results  []Result
	Selected int
	// Version counts the searches: it changes whenever Results may have,
	// and only then, so a view rebuilds its results only when it does.
	Version int

	apps      []*desktop.Entry
	appsReady bool
	snap      compositor.Snapshot
	connected bool
	rates     *convert.Rates // exchange rates for answers; nil until read
	previous  string         // window focused before Show
	notice    string         // feedback about the last action, cleared by the next input
	webSearch string         // the web search's address, %s for the words
	links     []Quicklink
	armed     string // the command that asked for Enter again
	assistant bool   // the assistant is installed and on
}

// SetAssistant offers the assistant: a last row for any query, and a
// query starting with "?" asks it at once.
func (l *Launcher) SetAssistant(on bool) {
	l.assistant = on
	if l.Open && l.Mode == Apps {
		l.search(false)
	}
}

// SetWeb sets the web search and the quicklinks.
func (l *Launcher) SetWeb(search string, links []Quicklink) {
	l.webSearch, l.links = search, links
	if l.Open && l.Mode == Apps {
		l.search(false)
	}
}

// SetApps replaces the installed applications.
func (l *Launcher) SetApps(apps []*desktop.Entry) {
	l.apps, l.appsReady = apps, true
	if l.Open && l.Mode == Apps {
		l.search(false)
	}
}

// SetSnapshot replaces the compositor state. An open window list is
// refreshed, keeping the selected window selected while it exists.
func (l *Launcher) SetSnapshot(snap compositor.Snapshot) {
	l.snap = snap
	if l.Open && l.Mode == Windows {
		l.search(true)
	}
}

// SetRates replaces the exchange rates answers use.
func (l *Launcher) SetRates(r *convert.Rates) {
	l.rates = r
	if l.Open && l.Mode == Apps {
		l.search(false)
	}
}

// SetConnected records whether Hyprland is reachable.
func (l *Launcher) SetConnected(up bool) {
	l.connected = up
	if l.Open && l.Mode == Windows {
		l.search(true)
	}
}

// Show opens the launcher in a mode with an empty query, remembering the
// focused window so Cancel can return to it. Showing an open launcher
// switches its mode and starts over.
func (l *Launcher) Show(m Mode) {
	if !l.Open {
		l.previous = l.snap.FocusedWindow
	}
	l.Open, l.Mode, l.Query, l.notice, l.armed = true, m, "", "", ""
	l.search(false)
}

// SetQuery updates the search text.
func (l *Launcher) SetQuery(q string) {
	if q == l.Query {
		return
	}
	l.Query, l.notice, l.armed = q, "", ""
	l.search(false)
}

// Move moves the selection by delta rows, stopping at either end.
func (l *Launcher) Move(delta int) {
	l.Selected = max(0, min(l.Selected+delta, len(l.Results)-1))
	if l.armed != "" {
		l.armed, l.notice = "", ""
	}
}

// Line is a line of the launcher's results: one result as a row, or, in
// apps mode, apps side by side as a line of a grid. Results Start to End,
// End excluded.
type Line struct {
	Start, End int
	Grid       bool
}

// RowCols is how many quick actions (answers, commands, the web search,
// the assistant) go side by side in apps mode.
const RowCols = 2

// Lines lays the results out. In apps mode results of a kind go side by
// side, apps cols to a line and the quick actions RowCols to a line, so
// the results keep their order; in windows mode each is a row of its own.
func (l *Launcher) Lines(cols int) []Line {
	var lines []Line
	for i := 0; i < len(l.Results); {
		if l.Mode != Apps || cols < 2 {
			lines = append(lines, Line{Start: i, End: i + 1})
			i++
			continue
		}
		app := l.Results[i].Entry != nil
		per := RowCols
		if app {
			per = cols
		}
		end := i
		for end < len(l.Results) && end-i < per && (l.Results[end].Entry != nil) == app {
			end++
		}
		lines = append(lines, Line{Start: i, End: end, Grid: true})
		i = end
	}
	return lines
}

// Pinned is where the rows after the apps begin, in apps mode with apps
// found: answers, commands, the web search and the assistant (appsFirst
// puts them there), which a view keeps in sight below the apps;
// len(Results) for none.
func (l *Launcher) Pinned() int {
	i := len(l.Results)
	if l.Mode != Apps {
		return i
	}
	for i > 0 && l.Results[i-1].Entry == nil {
		i--
	}
	if i == 0 {
		return len(l.Results)
	}
	return i
}

// lineOf is the index of the line holding result i, -1 for none.
func lineOf(lines []Line, i int) int {
	for n, ln := range lines {
		if i >= ln.Start && i < ln.End {
			return n
		}
	}
	return -1
}

// Step moves the selection on the laid-out results: dy lines down (up
// when negative), keeping the column in a grid, or dx along a grid line.
// It reports whether it moved or could: dx is not taken on a row of its
// own (windows mode), where the arrows edit the search instead.
func (l *Launcher) Step(lines []Line, dx, dy int) bool {
	cur := lineOf(lines, l.Selected)
	if cur < 0 {
		return false
	}
	if dx != 0 {
		if !lines[cur].Grid {
			return false
		}
		l.Move(dx)
		return true
	}
	next := max(0, min(cur+dy, len(lines)-1))
	to := lines[next].Start
	if lines[next].Grid && lines[cur].Grid {
		to = min(lines[next].Start+l.Selected-lines[cur].Start, lines[next].End-1)
	}
	l.Move(to - l.Selected)
	return true
}

// Cancel closes the launcher. Focus goes back to the window that had it
// when the launcher opened, if that window still exists; otherwise Hyprland
// keeps whatever focus it has.
func (l *Launcher) Cancel() Action {
	if !l.Open {
		return Action{}
	}
	l.Open = false
	if l.previous != "" && l.windowExists(l.previous) {
		return Action{Kind: Focus, Address: l.previous}
	}
	return Action{}
}

// Execute returns the action for the selected result. The launcher stays
// open until the view reports the outcome with Done. A window that closed
// since the list was shown yields no action: the list is refreshed and a
// notice says why.
func (l *Launcher) Execute() Action {
	if !l.Open || l.Selected >= len(l.Results) {
		return Action{}
	}
	r := l.Results[l.Selected]
	switch {
	case r.Ask != "":
		return Action{Kind: Ask, Text: r.Ask}
	case r.Copy != "":
		return Action{Kind: Copy, Text: r.Copy}
	case r.Fill != "":
		l.SetQuery(r.Fill)
		return Action{Kind: Fill, Text: r.Fill}
	case r.URL != "":
		return Action{Kind: Open, Text: r.URL}
	case r.Command != "":
		if c, ok := CommandByID(r.Command); ok && c.Confirm != "" && l.armed != r.Command {
			l.armed = r.Command
			l.notice = i18n.T(c.Confirm)
			return Action{}
		}
		l.armed = ""
		return Action{Kind: Run, Text: r.Command, Arg: r.Arg, Address: l.previous}
	}
	if r.Entry != nil {
		return Action{Kind: Launch, Entry: r.Entry}
	}
	if !l.windowExists(r.Address) {
		l.search(false)
		l.notice = i18n.T("That window has closed")
		return Action{}
	}
	return Action{Kind: Focus, Address: r.Address}
}

// Done reports the outcome of an executed action: success closes the
// launcher, a failure stays open and is shown.
func (l *Launcher) Done(err error) {
	if err != nil {
		l.notice = err.Error()
		return
	}
	l.Open = false
}

// Message is the line shown with the results: feedback about the last
// action, or why the list is empty. "" when there is nothing to say.
func (l *Launcher) Message() string {
	if l.notice != "" {
		return l.notice
	}
	if len(l.Results) > 0 {
		return ""
	}
	switch {
	case l.Mode == Apps && l.assistant && strings.HasPrefix(strings.TrimSpace(l.Query), "?"):
		return i18n.T("Ask the assistant anything")
	case l.Mode == Apps && strings.HasPrefix(strings.TrimSpace(l.Query), ":"):
		return i18n.T("No emoji or symbol matches")
	case l.Mode == Apps && !l.appsReady:
		return i18n.T("Reading apps…")
	case l.Mode == Apps:
		return i18n.T("No app matches")
	case !l.connected:
		return i18n.T("Not connected to Hyprland")
	case len(l.snap.Windows) == 0:
		return i18n.T("No windows open")
	}
	return i18n.T("No window matches")
}

// Failed reports whether Message is about a failed action rather than an
// empty list.
func (l *Launcher) Failed() bool { return l.notice != "" && l.armed == "" }

// windowTitle is a window's title, or its class; "" when it is gone.
func (l *Launcher) windowTitle(addr string) string {
	for _, t := range l.snap.Windows {
		if t.ID == addr {
			return cmp.Or(t.Title, t.AppID)
		}
	}
	return ""
}

func (l *Launcher) windowExists(addr string) bool {
	_, ok := l.snap.Window(addr)
	return ok
}

// search rebuilds the results. keep holds on to the selected window across
// a refresh; otherwise the best match is selected.
func (l *Launcher) search(keep bool) {
	l.Version++
	var keepAddr string
	if keep && l.Selected < len(l.Results) {
		keepAddr = l.Results[l.Selected].Address
	}
	q := strings.ToLower(strings.TrimSpace(l.Query))
	best := 0 // the result selected: the best match
	if l.Mode == Apps {
		if e, ok := strings.CutPrefix(q, ":"); ok { // emoji and symbols only
			l.Results = emojiResults(e)
			l.Selected = 0
			return
		}
		if question, ok := strings.CutPrefix(strings.TrimSpace(l.Query), "?"); ok && l.assistant {
			l.Results = askResults(strings.TrimSpace(question), i18n.T("Enter asks"))
			l.Selected = 0
			return
		}
		// Apps and commands, best first; on a tie the app.
		l.Results = append(searchApps(l.apps, q), searchCommands(q, l.windowTitle(l.previous))...)
		slices.SortStableFunc(l.Results, func(a, b Result) int { return b.score - a.score })
		// A question ("10 km to mi", "2^10") answers first, then a
		// quicklink or a web address; the web search comes last.
		first, last := webResults(l.Query, l.webSearch, l.links)
		l.Results = append(append(first, l.Results...), last...)
		if l.assistant && q != "" {
			l.Results = append(l.Results, askResults(strings.TrimSpace(l.Query), i18n.T("or start with ?"))...)
		}
		if a, ok := convert.Evaluate(l.Query, l.rates); ok {
			l.Results = append([]Result{{Title: a.Text, Detail: i18n.Tf("%s · Enter copies", a.Detail), Icon: "accessories-calculator-symbolic", Copy: a.Copy}}, l.Results...)
		}
		l.Results, best = appsFirst(l.Results)
	} else {
		l.Results = searchWindows(l.snap, q)
	}
	l.Selected = best
	if keepAddr != "" {
		if i := slices.IndexFunc(l.Results, func(r Result) bool { return r.Address == keepAddr }); i >= 0 {
			l.Selected = i
			return
		}
	}
	// Like Alt-Tab: with no query the windows are in focus order, and the
	// one to switch to is the previous one, not the one already focused.
	if l.Mode == Windows && q == "" && len(l.Results) > 1 && l.Results[0].Address == l.snap.FocusedWindow {
		l.Selected = 1
	}
}

// appsFirst puts the apps together first, for a view's grid, and the rest
// (answers, commands, the web search, the assistant) after them in their
// order; best is where the best match, the first before, went.
func appsFirst(rs []Result) (out []Result, best int) {
	out = make([]Result, 0, len(rs))
	for _, r := range rs {
		if r.Entry != nil {
			out = append(out, r)
		}
	}
	apps := len(out)
	for _, r := range rs {
		if r.Entry == nil {
			out = append(out, r)
		}
	}
	if len(rs) > 0 && rs[0].Entry == nil {
		best = apps // the first of the rest: they keep their order
	}
	return out, best
}

// AssistantIcon is the assistant's app icon (config/apps).
const AssistantIcon = "io.github.lyricat.Inua.Assistant"

// askResults is the assistant's row for a question; none for no question.
func askResults(question, hint string) []Result {
	if question == "" {
		return nil
	}
	return []Result{{Title: i18n.T("Ask the assistant"), Detail: question + " · " + hint, Icon: AssistantIcon, Ask: question}}
}

func searchApps(apps []*desktop.Entry, q string) []Result {
	var rs []Result
	for _, e := range apps {
		if e.NoDisplay {
			continue
		}
		score := 1
		if q != "" {
			score = rank(q, e.Name, append([]string{e.GenericName, e.ID}, e.Keywords...)...)
		}
		if score == 0 {
			continue
		}
		detail := cmp.Or(e.GenericName, e.Comment)
		rs = append(rs, Result{Title: e.Name, Detail: detail, Icon: e.Icon, Entry: e, score: score})
	}
	slices.SortStableFunc(rs, func(a, b Result) int {
		return cmp.Or(b.score-a.score, cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)))
	})
	return rs
}

func searchWindows(snap compositor.Snapshot, q string) []Result {
	var rs []Result
	for i, t := range snap.Windows {
		score := 1
		if q != "" {
			score = rank(q, t.Title, t.AppID)
		}
		if score == 0 {
			continue
		}
		detail := t.AppID
		if t.Workspace != "" {
			detail = i18n.Tf("%s · workspace %s", t.AppID, workspaceName(snap, t.Workspace))
		}
		rs = append(rs, Result{
			Title: cmp.Or(t.Title, t.AppID), Detail: detail, Icon: t.AppID,
			Address: t.ID, score: score, history: focusHistory(t, len(snap.Windows)+i),
		})
	}
	slices.SortStableFunc(rs, func(a, b Result) int {
		return cmp.Or(b.score-a.score, a.history-b.history)
	})
	return rs
}

// focusHistory is a window's focus rank (1 = focused last); windows
// without one sort after all that have one, in the compositor's order.
func focusHistory(t compositor.Window, fallback int) int {
	if t.FocusRank > 0 {
		return t.FocusRank
	}
	return fallback
}

func workspaceName(snap compositor.Snapshot, id string) string {
	if w, ok := snap.Workspace(id); ok && w.Name != "" {
		return w.Name
	}
	return id
}
