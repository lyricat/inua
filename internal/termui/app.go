package termui

import (
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"
	"sync"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/dbusmenu"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/term/host"
	"github.com/lyricat/inua/internal/term/model"
	"github.com/lyricat/inua/internal/term/scheme"
	"github.com/lyricat/inua/internal/term/settings"
)

// App is inua-term's window, a client of the host: the workspaces in
// column 1, the shown workspace's sessions in column 2, and the shown
// session's panes tiled in column 3. What it shows is the host's State;
// it asks the host for every change and draws the State that comes back.
type App struct {
	c      *appui.Ctx
	win    *gtk.ApplicationWindow
	client *host.Client
	menu   *dbusmenu.Published // the menu the bar shows while this window has the focus

	state *host.State
	info  map[int]host.PaneInfo
	tiles map[int]*tile // the shown session's panes

	// live is the panes with a widget, for the client's goroutine, which
	// delivers their output.
	mu   sync.Mutex
	live map[int]*termPane

	cols         columns
	stage        *gtk.Overlay
	sheets       *gtk.Overlay // the whole window, for the sheets
	tilesW       *gtk.Box
	tiling       tiling
	sheet        *gtk.Box // the sheet up, while it asks
	closePending bool     // a close decision is being fetched
	// swapping is set after Ctrl+Shift+X, until the arrow.
	swapping bool
	// fold is the columns the user folded; peek those shown anyway while
	// the window is too narrow for them; uiDone is set once the kept
	// size and folds were applied.
	fold, peek [2]bool
	uiDone     bool
	// set is term.toml; keymap its keys by chord; fontStep the font's
	// size away from the settings' (Ctrl+= and Ctrl+-).
	set      settings.Settings
	keymap   map[string]string
	fontStep float64
	// used are the look and the shell in use, which the settings change
	// in a.set itself.
	used      settings.Look
	usedShell string
	// extra does the actions other files of the window add (the palette,
	// the settings); false for none of theirs.
	extra   func(action string, p *termPane) bool
	onBlink glib.SourceFunc

	prefs *prefs // the settings window, while open
	// firstRun is set when there was no term.toml: the guide asks for
	// the shell and the look before the host starts.
	firstRun bool
	// restarting is set while the host restarts (restartHost); restartWait
	// starts the new one.
	restarting  bool
	restartWait glib.SourceFunc
	guide       *guide
	// quick is set for the quick mode's window (quick.go), quickTerm its
	// pane and program.
	quick     bool
	quickTerm quickTerm
	paneDrag  paneDrag
	brands    brands

	onCloseOK, onCloseCancel func(gtk.Button)

	onSheetEsc    func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onPaletteFind func(uintptr, uintptr)
	onMenuItem    func(gio.SimpleAction, uintptr) // the Mac's menu bar (menubar.go)
	onSmoke       gtk.TickCallback
	onPaletteRun  func(gtk.SearchEntry)
	onPaletteRow  func(gtk.ListBox, uintptr)
	onPaletteKey  func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool

	onClose                    func(gtk.Window) bool
	onFolder                   gio.AsyncReadyCallback
	onRenameOK, onRenameCancel func(gtk.Button)
	onRenameActivate           func(gtk.Entry)
	onRenameKey                func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onTick                     glib.SourceFunc
	onOverviewRow              func(gtk.ListBox, uintptr)
	onOverviewKey              func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onActive                   func(uintptr, uintptr, uintptr)
	onWindowKey                func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onFree                     glib.SourceFunc
	// gone are the panes and windows closed whose callbacks are yet to
	// be freed.
	gone []gone
}

// Register makes inua-term an appui app, "term".
func Register() {
	appui.Register("term", "Term", i18n.M("Terminal"), func() appui.View { return &App{} })
}

// Prepare reads term.toml before the style loads: the window's theme may
// be its own.
func (a *App) Prepare(*appui.Ctx) {
	set, warnings, err := settings.Load(settings.Path())
	if err != nil {
		log.Printf("term: %v", err)
	}
	for _, w := range warnings {
		log.Printf("term: %s: %s", settings.Path(), w)
	}
	a.set, a.keymap = set, keymapOf(set.Keys)
	a.used, a.usedShell = set.Look, set.Shell.Program
	a.firstRun = !settings.Exists(settings.Path())
}

// Theme is the window's own theme, "" for inua's.
func (a *App) Theme() string { return a.set.Look.Theme }

func (a *App) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	if a.quick {
		return a.buildQuick(c)
	}
	a.c = c
	a.info, a.tiles, a.live = map[int]host.PaneInfo{}, map[int]*tile{}, map[int]*termPane{}
	a.win = &gtk.ApplicationWindow{}
	ui := c.Load("term.ui")
	ui.Get("term", a.win)
	a.buildColumns(ui)
	a.stage, a.sheets, a.tilesW = &gtk.Overlay{}, &gtk.Overlay{}, &gtk.Box{}
	ui.Get("stage", a.stage)
	ui.Get("sheets", a.sheets)
	ui.Get("tiles", a.tilesW)
	ui.Close()
	a.win.SetTitle(i18n.T("Terminal"))
	a.buildTiling()
	a.startPaneDrag()
	a.onFree = func(uintptr) bool { return a.freeGone() }
	a.onClose = func(gtk.Window) bool {
		a.closeWindow()
		return false
	}
	a.win.ConnectCloseRequest(&a.onClose)
	a.windowKeys()
	if runtime.GOOS == "darwin" {
		a.menuBar()
	}
	a.publishMenu()
	if os.Getenv("INUA_TERM_SMOKE") != "" {
		a.smokeTest()
	}
	a.startTicks()
	a.startBlink()
	// An agent in the pane shown is looked at while the window is active.
	a.onActive = func(uintptr, uintptr, uintptr) {
		active := a.win.IsActive()
		a.ask("active", func(c *host.Client) error { return c.SetActive(active) })
	}
	gtkx.ConnectNotify(a.win, "is-active", &a.onActive)
	if a.firstRun {
		// The shell is chosen before the first pane starts.
		a.openGuide(a.start)
	} else {
		a.start()
	}
	return a.win
}

// start reaches the host: off the main thread, the state after.
func (a *App) start() {
	shell := a.set.Shell.Program
	go func() {
		st, err := a.connect(shell)
		gtkx.Post(func() {
			if err != nil {
				a.fail(err)
				return
			}
			a.apply(st)
		})
	}()
}

// connect reaches the host, starting it if none runs, tells it the shell
// new panes start, and opens the home folder's workspace when there is
// none.
func (a *App) connect(shell string) (*host.State, error) {
	client, err := host.Connect(a.output, a.event)
	if err != nil {
		return nil, err
	}
	ready := make(chan host.Colors, 1)
	gtkx.Post(func() {
		a.client = client
		ready <- a.colors()
	})
	if err := client.SetColors(<-ready); err != nil {
		log.Printf("term: colors: %v", err)
	}
	if err := client.SetShell(shell); err != nil {
		log.Printf("term: shell: %v", err)
	}
	go func() {
		<-client.Done()
		gtkx.Post(func() {
			switch {
			case a.client != client:
			case a.restarting:
				a.hostEnded()
			default:
				log.Printf("term: the host ended; the window closes")
				a.win.Close() // its last pane closed
			}
		})
	}()
	st, err := client.State()
	if err != nil {
		return nil, err
	}
	if len(st.Model.Workspaces) == 0 {
		home, _ := os.UserHomeDir()
		if _, err := client.Open(home, ""); err != nil {
			return nil, err
		}
		return client.State()
	}
	if st.Model.Shown.Workspace == 0 {
		client.Show(st.Model.Workspaces[0].ID, 0)
		return client.State()
	}
	return st, nil
}

// ask sends the host a request off the main thread; a failure shows in
// the log.
func (a *App) ask(what string, f func(*host.Client) error) {
	client := a.client
	if client == nil {
		return
	}
	go func() {
		if err := f(client); err != nil {
			log.Printf("term: %s: %v", what, err)
		}
	}()
}

// paneFor is a pane with a widget, for the client's goroutine.
func (a *App) paneFor(id int) *termPane {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.live[id]
}

func (a *App) output(id int, b []byte) {
	if p := a.paneFor(id); p != nil {
		p.feed(b)
	}
}

// event is a message from the host, on the client's goroutine.
func (a *App) event(m host.Message) {
	switch {
	case m.Type == host.MsgState && m.State != nil:
		st := m.State
		gtkx.Post(func() { a.apply(st) })
	case m.Type == host.MsgEvent:
		if m.Cols > 0 && m.Rows > 0 {
			// In the output's order, as the host took it.
			if p := a.paneFor(m.Pane); p != nil {
				p.feedSize(m.Cols, m.Rows)
			}
			if m.Title == "" && m.Dir == "" && m.Git == nil && m.Code == nil && m.Clipboard == nil {
				return
			}
		}
		gtkx.Post(func() { a.paneEvent(m) })
	}
}

// paneEvent is a pane's title, folder, checkout, exit or clipboard write.
func (a *App) paneEvent(m host.Message) {
	info, ok := a.info[m.Pane]
	if !ok {
		return
	}
	if m.Title != "" {
		info.Title = m.Title
	}
	if m.Dir != "" {
		info.Dir = m.Dir
	}
	if m.Git != nil {
		info.Git = m.Git
		if m.Git.Root == "" {
			info.Git = nil
		}
	}
	if m.Code != nil {
		info.Exited, info.Code = true, *m.Code
	}
	a.info[m.Pane] = info
	if t := a.tiles[m.Pane]; t != nil {
		if m.Code != nil {
			t.pane.exit(*m.Code)
		}
		if m.Clipboard != nil {
			t.pane.setClipboard(*m.Clipboard, m.Primary)
		}
		t.update(a, info)
	}
	if m.Title != "" || m.Dir != "" || m.Code != nil {
		a.cols.update(a)
	}
}

// apply shows a State: the columns, and the shown session's tiles.
func (a *App) apply(st *host.State) {
	a.state = st
	if !a.uiDone {
		a.uiDone = true
		ui := st.Model.UI
		if ui.Width > 0 && ui.Height > 0 {
			a.win.SetDefaultSize(int32(ui.Width), int32(ui.Height))
		}
		a.fold = [2]bool{ui.Fold1, ui.Fold2}
		a.setColWidth(0, ui.Col1)
		a.setColWidth(1, ui.Col2)
		// As it was left, at once; it slides from now on.
		d := a.cols.col1Reveal.GetTransitionDuration()
		a.cols.col1Reveal.SetTransitionDuration(0)
		a.applyFolds()
		a.cols.col1Reveal.SetTransitionDuration(d)
	}
	a.info = map[int]host.PaneInfo{}
	for _, p := range st.Panes {
		a.info[p.ID] = p
	}
	a.cols.update(a)
	a.syncTiles()
	if s := a.shown(); s != nil {
		a.win.SetTitle(s.Name + " — " + i18n.T("Terminal"))
	} else {
		a.win.SetTitle(i18n.T("Terminal"))
	}
}

// shown is the session on screen, nil for none.
func (a *App) shown() *model.Session {
	if a.state == nil {
		return nil
	}
	s, _ := a.state.Model.Session(a.state.Model.Shown.Session)
	return s
}

// shownWorkspace is the workspace on screen, nil for none.
func (a *App) shownWorkspace() *model.Workspace {
	if a.state == nil {
		return nil
	}
	return a.state.Model.Workspace(a.state.Model.Shown.Workspace)
}

func (a *App) fail(err error) {
	log.Printf("term: %v", err)
	label := gtk.NewLabel(err.Error())
	label.SetWrap(true)
	label.AddCssClass("term-error")
	a.stage.SetChild(&label.Widget)
}

// closeWindow leaves the panes running on the host, and has it keep how
// the window is.
func (a *App) closeWindow() {
	log.Printf("term: the window closes")
	a.menu.Close()
	a.menu = nil
	if a.client != nil && a.uiDone {
		a.client.SetUI(a.uiNow())
	}
	for id := range a.tiles {
		a.dropTile(id)
	}
	if a.client != nil {
		client := a.client
		a.client = nil
		client.Close()
	}
}

func (a *App) ThemeChanged() {
	a.schemeChanged()
	// Marks in an agent's own colours follow the theme's light or dark.
	for id, t := range a.tiles {
		t.update(a, a.info[id])
	}
	a.cols.update(a)
	if a.guide != nil {
		a.guide.themeChanged(a)
	}
}

// schemeChanged gives the panes, and the host for new ones, the
// terminals' colours.
func (a *App) schemeChanged() {
	sc := a.scheme()
	for _, p := range a.panes() {
		p.setScheme(sc, a.c.Colors["warn"])
	}
	colors := a.colors()
	a.ask("colors", func(c *host.Client) error { return c.SetColors(colors) })
}

// scheme is the terminals' colours: the settings' scheme, else the
// theme's.
func (a *App) scheme() scheme.Scheme {
	if sc, ok := scheme.Find(a.set.Look.Scheme); ok {
		return sc
	}
	return themeScheme(a.c.Colors)
}

// colors are the terminals', for programs that ask.
func (a *App) colors() host.Colors {
	sc := a.scheme()
	return host.Colors{FG: sc.FG, BG: sc.BG, Cursor: sc.Cursor, Palette: &sc.Palette}
}

// paneBackend is a pane on the host.
type paneBackend struct {
	app *App
	id  int
}

func (b paneBackend) Input(p []byte) error {
	if b.app.client == nil {
		return nil
	}
	return b.app.client.Input(b.id, p)
}

// Resize asks the host for a size; the pane's engine takes it when the
// host says it did (feedSize), or now, from a host that does not say.
func (b paneBackend) Resize(cols, rows, cellW, cellH int) error {
	c := b.app.client
	if c == nil {
		return errNoHost
	}
	b.app.ask("resize", func(c *host.Client) error { return c.Resize(b.id, cols, rows, cellW, cellH) })
	if !c.TellsSizes() {
		return errNoSizes
	}
	return nil
}

var (
	errNoHost  = errors.New("no host")
	errNoSizes = errors.New("the host does not tell sizes")
)

// smokeTest quits once the window has drawn a frame, saying so: CI's
// check that the app starts where no one looks at it (INUA_TERM_SMOKE).
func (a *App) smokeTest() {
	ticks := 0
	a.onSmoke = func(uintptr, uintptr, uintptr) bool {
		if ticks++; ticks < 2 { // the second frame: the first was drawn
			return true
		}
		fmt.Println("inua-term: a frame drawn") // stdout: stderr may be the log
		a.c.App.Quit()
		return false
	}
	a.win.AddTickCallback(&a.onSmoke, 0, &noDestroy)
}
