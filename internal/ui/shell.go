// Package ui is the GTK4 shell: bars, launcher, control center and tray on
// layer-shell surfaces. Layouts come from the ui/ directory at the top of
// the repository; state and rules come from internal/shell. Code here takes
// named widgets, fills them from that state, and feeds events back.
//
// Everything that touches GTK runs on the main thread. Services run on
// goroutines and hand their results over with gtkx.Post. puregotk's
// ownership and callback rules are in internal/gtkx; signal handlers are
// fields of shell, shared by all widgets.
package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/purego"
	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/hypr"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/ipc"
	"github.com/lyricat/inua/internal/layershell"
	"github.com/lyricat/inua/internal/shell"
	layouts "github.com/lyricat/inua/ui"
)

// Options configure Run.
type Options struct {
	// NoLayer uses normal windows, for compositors without layer-shell
	// (GNOME).
	NoLayer bool
	// UIDir, when set, makes layouts load from disk: the launcher re-reads
	// its .ui on every open, the rest is read at startup.
	UIDir string
	// Instance names the control socket.
	Instance string
	// Version is the release the shell is ("dev" when built from source),
	// for the daily check for a new one.
	Version string
	// Config is the settings file's content (internal/config), read from
	// ConfigPath; the settings window and `shell reload` write and reread
	// that file.
	Config     config.Config
	ConfigPath string
}

type shellUI struct {
	opts  Options
	cfg   config.Config
	app   *gtk.Application
	layer bool // compositor supports layer-shell and it is in use

	appsStamp string           // the applications folders as last scanned (desktop.Stamp)
	apps      []*desktop.Entry // the desktop entries as last scanned

	client    *hypr.Client
	snap      hypr.Snapshot
	connected bool
	// virtual asks for the virtual screen to follow the monitors again
	// (syncVirtual); a request already waiting covers a new one.
	virtual chan struct{}
	// hyprLua: Hyprland runs Lua (0.55 and later; probeLua). The overview
	// needs it, and focusing and switching use its dispatchers when there,
	// the classic ones otherwise.
	hyprLua   bool
	hyprApply hyprApply // writing hypr/inua.conf; see applyHyprland
	notes     notes
	dockState dockState
	icons     icons

	bars      map[uintptr]*bar // by GdkMonitor
	styles    styles
	clockGen  uintptr // the running clock timer; see restartClock
	launcher  launcher
	overview  workspaceOverview
	control   control
	clipboard clipboardPanel
	passwords passwordsPanel
	agenda    agenda
	shot      shooter
	recording recorder
	lock      locker
	auth      authAgent   // the polkit agent
	pass      passPrompts // gpg's passphrases (inua-pinentry)
	alarms    alarms
	settings  settings
	wifi      wifiState // the networks, while a list shows them
	tray      tray
	osd       osd
	sys       shell.System
	svc       services

	// Long-lived signal handlers, shared by all widgets (see package gtkx).
	onMonitorsChanged      func(uintptr, uint32, uint32, uint32, uintptr)
	onConnector            func(uintptr, uintptr, uintptr)
	onWorkspaceClicked     func(gtk.Button)
	onWorkspaceSettle      glib.SourceFunc
	onClock                glib.SourceFunc
	onKey                  func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onLauncherScroll       func(gtk.EventControllerScroll, float64, float64) bool
	onOverviewKey          func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onOverviewButton       func(gtk.Button)
	onWorkspaceOverview    func(gtk.GestureClick, int32, float64, float64)
	onOverviewCanvas       func(gtk.GestureClick, int32, float64, float64)
	onOverviewQuery        func(uintptr, uintptr)
	onOverviewDrop         func(gtk.DropTarget, uintptr, float64, float64) bool
	onOverviewMotion       func(gtk.DropTarget, float64, float64) gdk.DragAction
	onOverviewLeave        func(gtk.DropTarget)
	onOverviewFilterRender glib.SourceFunc
	onOverviewRender       glib.SourceFunc
	onQueryChanged         func(uintptr, uintptr)
	onQueryActivate        func(gtk.Entry)
	onQueryFocus           func(gtk.EventControllerFocus)
	onControlButton        func(gtk.Button)
	onControlItem          func(gtk.Button)
	onBarMotion            func(gtk.EventControllerMotion, float64, float64)
	onBarLeave             func(gtk.EventControllerMotion)
	onBarReleased          func(gtk.GestureClick, int32, float64, float64)
	onBarScroll            func(gtk.EventControllerScroll, float64, float64) bool
	onSlider               func(gtk.Range, gtk.ScrollType, float64) bool
	onBackdropPressed      func(gtk.GestureClick, int32, float64, float64)
	onControlKey           func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onControlGone          glib.SourceFunc
	onControlTick          glib.SourceFunc
	onPowerToggled         func(gtk.ToggleButton)
	onPowerDisarm          glib.SourceFunc
	onTrayClicked          func(gtk.Button)
	onTrayPressed          func(gtk.GestureClick, int32, float64, float64)
	onTrayMenuAction       func(gio.SimpleAction, uintptr)
	onTrayMenuClosed       func(gtk.Popover)
	onSettingsEntry        func(uintptr, uintptr)
	onSettingsSwitch       func(gtk.Switch, bool) bool
	onAIAbility            func(gtk.ToggleButton)
	onAIModelSearch        func(gtk.SearchEntry)
	onAIModelPick          func(gtk.Button)
	onSettingsDrop         func(uintptr, uintptr, uintptr)
	onSettingsPage         func(uintptr, uintptr, uintptr)
	onDefaultApp           func(uintptr, uintptr, uintptr)
	onVoiceCheck           func(gtk.Button)
	onDisplayDrop          func(uintptr, uintptr, uintptr)
	onPowerDrop            func(uintptr, uintptr, uintptr) // the Power page's drop-downs
	onVoiceSwitch          func(gtk.Switch, bool) bool
	onVoiceModel           func(gtk.Button)
	onDisplaySwitch        func(gtk.Switch, bool) bool
	onDisplayButton        func(gtk.Button)
	onBluetoothSwitch      func(gtk.Switch, bool) bool
	onBluetoothButton      func(gtk.Button)
	onBluetoothEntry       func(gtk.Entry)
	onWifiSwitch           func(gtk.Switch, bool) bool
	onWifiButton           func(gtk.Button)
	onWifiDetailsClose     func(gtk.Button) // the details dialog's Close, one for every opening
	onWifiEntry            func(gtk.PasswordEntry)
	onWifiPoll             glib.SourceFunc
	onSettingsIndex        func(gtk.ListBox, uintptr)
	onSettingsHeader       gtk.ListBoxUpdateHeaderFunc // the index's group kickers
	onSettingsHeaderDone   glib.DestroyNotify
	onSettingsSearch       func(gtk.SearchEntry)
	onSettingsSearchGo     func(gtk.SearchEntry)
	onSettingsSearchStop   func(gtk.SearchEntry)
	onSettingsResult       func(gtk.ListBox, uintptr)
	onSearchScroll         glib.SourceFunc
	onSearchFlash          glib.SourceFunc
	onAboutLink            func(gtk.Button)
	onSettingsButton       func(gtk.Button)
	onSettingsClose        func(gtk.Window) bool
	onSettingsKey          func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onSettingsScale        func(gtk.Range, gtk.ScrollType, float64) bool
	onSettingsScroll       func(gtk.Adjustment)
	onShotDragBegin        func(gtk.GestureDrag, float64, float64)
	onShotDragUpdate       func(gtk.GestureDrag, float64, float64)
	onShotDragEnd          func(gtk.GestureDrag, float64, float64)
	onShotMotion           func(gtk.EventControllerMotion, float64, float64)
	onShotLeave            func(gtk.EventControllerMotion)
	onShotKey              func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onShotMode             func(gtk.ToggleButton)
	onShotCancel           func(gtk.Button)
	onShotRecord           func(gtk.ToggleButton)
	onShotDrawer           func(gtk.Button)
	onShotItem             func(gtk.Button)
	onShotTake             func(gtk.Button)
	onShotDrawerClose      func(gtk.Button)
	onShotDrawerRecord     func(gtk.ToggleButton)
	onShotDrawerKey        func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onShotReopen           glib.SourceFunc
	onShotItemDragBegin    func(gtk.DragSource, uintptr)
	onShotItemDragCancel   func(gtk.DragSource, uintptr, gdk.DragCancelReason) bool
	onShotItemDragEnd      func(gtk.DragSource, uintptr, bool)
	onRecordTick           glib.SourceFunc
	onRecordStart          glib.SourceFunc
	onRecButton            func(gtk.Button)
	onClockButton          func(gtk.Button)
	onAgendaButton         func(gtk.Button)
	onAgendaWeather        func(gtk.Button)
	onAgendaPressed        func(gtk.GestureClick, int32, float64, float64)
	onAgendaKey            func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onAgendaGone           glib.SourceFunc
	onLockMonitor          func(uintptr, uintptr, uintptr)
	onLockLocked           func(uintptr, uintptr)
	onLockFailed           func(uintptr, uintptr)
	onLockUnlocked         func(uintptr, uintptr)
	onLockTick             glib.SourceFunc
	onLockActivate         func(gtk.PasswordEntry)
	onAuthOK               func(gtk.Button)
	onAuthCancel           func(gtk.Button)
	onAuthSwitch           func(gtk.Button)
	onAuthActivate         func(gtk.PasswordEntry)
	onAuthKey              func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onPassOK               func(gtk.Button)
	onPassCancel           func(gtk.Button)
	onPassNotOK            func(gtk.Button)
	onPassActivate         func(gtk.PasswordEntry)
	onPassKey              func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onPassMask             func(gtk.Button)
	onPassRow              func(gtk.Button)
	onAlarmTick            glib.SourceFunc
	onRecordTimeout        glib.SourceFunc
	onSettingsSave         glib.SourceFunc
	onNoteButton           func(gtk.Button)
	onNoteClicked          func(gtk.GestureClick, int32, float64, float64)
	onNotesSave            glib.SourceFunc
	onBannerTick           glib.SourceFunc
	onOSDHide              glib.SourceFunc
	onOSDGone              glib.SourceFunc
	onBannerGone           glib.SourceFunc
	onBannerEnter          func(gtk.EventControllerMotion, float64, float64)
	onBannerLeave          func(gtk.EventControllerMotion)
	onCenterPressed        func(gtk.GestureClick, int32, float64, float64)
	onCenterKey            func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onCenterGone           glib.SourceFunc
	onNotesButton          func(gtk.Button)
	onVoiceButton          func(gtk.Button)
	onDockClicked          func(gtk.Button)
	onClipboardButton      func(gtk.Button)
	onClipboardSearch      func(gtk.SearchEntry)
	onClipboardActivate    func(gtk.SearchEntry)
	onClipboardAction      func(gtk.Button)
	onClipboardHover       func(gtk.EventControllerMotion, float64, float64)
	onClipboardLeave       func(gtk.EventControllerMotion)
	onClipboardBackdrop    func(gtk.GestureClick, int32, float64, float64)
	onClipboardKey         func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onClipboardTick        glib.SourceFunc
	onClipboardGone        glib.SourceFunc
	onPasswordsButton      func(gtk.Button)
	onPasswordsSearch      func(gtk.SearchEntry)
	onPasswordsActivate    func(gtk.SearchEntry)
	onPasswordsAction      func(gtk.Button)
	onPasswordsBackdrop    func(gtk.GestureClick, int32, float64, float64)
	onPasswordsKey         func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onPasswordsTick        glib.SourceFunc
	onPasswordsForget      glib.SourceFunc
	onDrawerClicked        func(gtk.Button)
	onDrawerApp            func(gtk.Button)
	onDrawerSearch         func(gtk.SearchEntry)
	onDrawerBackdrop       func(gtk.GestureClick, int32, float64, float64)
	onDrawerKey            func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onDrawerGone           glib.SourceFunc
	onWorkshopTick         glib.SourceFunc
	onDockMenu             func(gtk.GestureClick, int32, float64, float64)
	onDockAction           func(gio.SimpleAction, uintptr)
	onDockMenuClosed       func(gtk.Popover)
	onDockEnter            func(gtk.EventControllerMotion, float64, float64)
	onDockLeave            func(gtk.EventControllerMotion)
	onDockHide             glib.SourceFunc
	onDockRevealed         func(uintptr, uintptr, uintptr)
	onDockLaunched         glib.SourceFunc
	onDockDragBegin        func(gtk.DragSource, uintptr)
	onDockDragEnd          func(gtk.DragSource, uintptr, bool)
	onDockDragCancel       func(gtk.DragSource, uintptr, gdk.DragCancelReason) bool
	onDockGapTick          glib.SourceFunc
	onDockDropMotion       func(gtk.DropTarget, float64, float64) gdk.DragAction
	onDockDropLeave        func(gtk.DropTarget)
	onDockDrop             func(gtk.DropTarget, uintptr, float64, float64) bool
	onImageChosen          gio.AsyncReadyCallback
	onCardSaved            gio.AsyncReadyCallback
	onCustomChosen         gio.AsyncReadyCallback // a theme or icon set to add
	onAfter                glib.SourceFunc        // runAfter's
	afterFns               map[uintptr]func()
	afterSeq               uintptr
}

// Run runs the shell until it is killed and returns the exit status. Call
// it before anything else touches GTK: with layer-shell it re-executes the
// process once to preload gtk4-layer-shell.
func Run(opts Options) int {
	if !opts.NoLayer {
		if err := layershell.EnsurePreloaded(); err != nil {
			log.Printf("preload: %v", err)
			return 1
		}
		if err := layershell.Load(layershell.Loader{Open: dlopen, Register: purego.RegisterLibFunc}); err != nil {
			log.Print(err)
			return 1
		}
		log.Printf("gtk4-layer-shell %s preloaded", layershell.Version())
	}

	socketPath := ipc.SocketPath(opts.Instance)
	ln, err := ipc.Listen(socketPath)
	if err != nil {
		log.Printf("ipc: %v", err)
		return 1
	}

	app := gtk.NewApplication(shell.AppID, gio.GApplicationNonUniqueValue)
	s := &shellUI{opts: opts, cfg: opts.Config, app: app, bars: map[uintptr]*bar{}}
	s.pass.slot = make(chan struct{}, 1) // before the control socket serves
	activate := func(gio.Application) { s.start() }
	app.ConnectActivate(&activate)
	shutdown := func(gio.Application) { s.stopClipboardCapture() }
	app.ConnectShutdown(&shutdown)

	go func() {
		log.Printf("ipc: serving on %s", socketPath)
		err := ipc.ServeListener(context.Background(), ln, socketPath, func(ctx context.Context, req ipc.Request) ipc.Response {
			if req.Op == "call" && req.Target == "password" {
				return s.askPassword(ctx, req) // waits for the user, off the main loop
			}
			resp, err := gtkx.Call(2*time.Second, func() ipc.Response { return s.request(req) })
			if err != nil {
				return ipc.Response{Error: err.Error()}
			}
			return resp
		})
		if err != nil {
			log.Printf("ipc: %v", err)
		}
	}()

	// GTK gets no Go flags.
	status := int(app.Run(1, os.Args[:1]))
	s.guardOverviewMouse(false)
	s.pauseOverviewKeys()
	return status
}

func dlopen(name string) (uintptr, error) {
	return purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
}

// readUI returns a layout file, from Options.UIDir if set.
func (s *shellUI) readUI(name string) string {
	if s.opts.UIDir != "" {
		data, err := os.ReadFile(filepath.Join(s.opts.UIDir, name))
		if err == nil {
			return string(data)
		}
		log.Printf("ui: %v; using the embedded copy", err)
	}
	data, err := layouts.FS.ReadFile(name)
	if err != nil {
		log.Fatal(err)
	}
	return string(data)
}

// load instantiates a layout; a broken layout is a programming error.
func (s *shellUI) load(name string, scope *gtkx.Scope) *gtkx.Builder {
	b, err := gtkx.Load(s.readUI(name), scope)
	if err != nil {
		log.Fatalf("%s: %v", name, err)
	}
	return b
}

func (s *shellUI) start() {
	// A desktop shell must survive having no windows while outputs change
	// or while restoring a lock before its desktop surfaces can be created.
	s.app.Hold()
	s.loadStyles()
	s.loadWallpaper()

	s.layer = !s.opts.NoLayer && layershell.Supported()
	if !s.opts.NoLayer && !s.layer {
		log.Printf("compositor has no wlr-layer-shell; falling back to normal windows")
	}
	s.bindHandlers()
	s.onAfter = func(id uintptr) bool {
		f := s.afterFns[id]
		delete(s.afterFns, id)
		if f != nil {
			f()
		}
		return false
	}
	s.bindLauncherHandlers()
	s.bindOverviewHandlers()
	s.onWorkspaceOverview = func(g gtk.GestureClick, _ int32, _, _ float64) {
		if !s.hyprLua {
			return
		}
		s.showOverview()
		g.SetState(gtk.EventSequenceClaimedValue)
	}
	s.bindControlHandlers()
	s.bindOSDHandlers()
	s.bindAgendaHandlers()
	s.bindShotHandlers()
	s.bindShotDrawerHandlers()
	s.bindRecordHandlers()
	s.bindVoiceHandlers()
	s.bindTrayHandlers()
	s.bindSettingsHandlers()
	s.bindAboutHandlers()
	s.bindCardHandlers()
	s.bindWifiHandlers()
	s.bindControlDetailHandlers()
	s.bindNotifyHandlers()
	s.bindDockHandlers()
	s.bindDrawerHandlers()
	s.bindClipboardHandlers()
	s.bindPasswordsHandlers()
	s.bindLockHandlers()
	s.applyLauncherWeb()
	s.bindPolkitHandlers()
	s.bindPassHandlers()
	if s.lockMarked() { // the screen was locked when the last shell went away
		if err := s.lockScreen(); err != nil {
			log.Printf("lock: %v", err)
		}
	}

	monitors := gdk.DisplayGetDefault().GetMonitors()
	gtkx.ConnectItemsChanged(monitors, &s.onMonitorsChanged)
	s.syncBars()
	log.Printf("started: layer=%v bars=%s", s.layer, strings.Join(s.barNames(), ","))
	s.startNotify()
	s.startAlarms()
	s.startUpdates()
	s.startSharing()
	s.startConnect()
	s.startWebTerm()
	s.startSystem()
	s.startAssistant()
	s.startClipboardCapture()
	s.checkPasswordsStore()
	if s.layer { // elsewhere (GNOME) the desktop has its own agent
		s.startPolkit()
	}
	if s.cfg.Bar.Tray {
		s.startTray()
	}

	if paths, err := hypr.PathsFromEnv(); err != nil {
		log.Printf("hyprland: %v", err)
	} else {
		s.client = hypr.NewClient(paths)
		go hypr.NewStore(s.client, hyprHandler{s}).Run(context.Background())
	}
	// Bring hypr/inua.conf up to date with config.toml (edited while the
	// shell was not running); Hyprland reloads only if it changed.
	s.applyHyprland()
	s.scanApps()
	s.restartClock()
}

// restartClock redraws the clock now and starts a new timer chain.
func (s *shellUI) restartClock() {
	s.clockGen++
	s.onClock(s.clockGen)
}

func (s *shellUI) bindHandlers() {
	s.onMonitorsChanged = func(_ uintptr, _, removed, added uint32, _ uintptr) {
		log.Printf("monitors: %d removed, %d added", removed, added)
		s.syncBars()
		if s.cfg.Background.Generate {
			s.loadWallpaper() // drawn for the largest monitor, which may be another
		}
	}
	s.onBarMotion = func(c gtk.EventControllerMotion, x, y float64) {
		if b := s.barOf(c.GetWidget()); b != nil {
			end, _ := b.endReach(x, y)
			toggle(&b.passwords.Widget, "hover", end == "passwords")
			toggle(&b.clipboard.Widget, "hover", end == "clipboard")
			toggle(&b.notes.Widget, "hover", end == "notes")
			toggle(&b.status.Widget, "hover", end == "status")
			clock, _ := b.clockReach(x, y)
			toggle(&b.clockButton.Widget, "hover", clock && end == "")
		}
	}
	s.onBarLeave = func(c gtk.EventControllerMotion) {
		if b := s.barOf(c.GetWidget()); b != nil {
			for _, w := range []*gtk.Widget{&b.passwords.Widget, &b.clipboard.Widget, &b.notes.Widget, &b.status.Widget, &b.clockButton.Widget} {
				toggle(w, "hover", false)
			}
		}
	}
	s.onBarReleased = func(g gtk.GestureClick, _ int32, x, y float64) {
		b := s.barOf(g.GetWidget())
		if b == nil {
			return
		}
		// The buttons take their own clicks; those around them are theirs too.
		end, onEnd := b.endReach(x, y)
		clock, onClock := b.clockReach(x, y)
		switch {
		case onEnd || (end == "" && onClock):
		case end == "passwords":
			s.onPasswordsButton(b.passwords)
		case end == "clipboard":
			s.onClipboardButton(b.clipboard)
		case end == "notes":
			s.toggleCenter()
		case end == "status":
			s.controlAction("status")
		case clock:
			s.toggleAgenda()
		}
	}

	// The wheel in the status button's reach, which the motion handler
	// marks as hovered, adjusts the volume.
	s.onBarScroll = func(c gtk.EventControllerScroll, _, dy float64) bool {
		b := s.barOf(c.GetWidget())
		if b == nil || dy == 0 || !b.status.HasCssClass("hover") {
			return false
		}
		s.stepVolume(int(dy))
		return true
	}

	// GDK lists a new monitor before it knows its name; the bar can only
	// pick its workspaces once the name is there.
	s.onConnector = func(mon, _, _ uintptr) {
		if b := s.bars[mon]; b != nil {
			log.Printf("bar: monitor is %q", b.connector())
			b.setHypr(s, s.snap)
		}
	}
	s.onWorkspaceClicked = func(b gtk.Button) {
		// Hyprland answers an error for switching to the current workspace.
		if id := b.GetName(); !s.isFocusedWorkspace(id) {
			s.switchWorkspace(id)
		}
	}
	s.onWorkspaceSettle = func(uintptr) bool {
		for _, b := range s.bars {
			clear(b.fresh)
			b.styleWorkspaces()
		}
		return false
	}
	// Redraw the clock just after each minute (or second) starts. Each
	// timer carries the generation it belongs to; restartClock starts a
	// new one, and the old one stops at its next tick.
	s.onClock = func(gen uintptr) bool {
		if gen != s.clockGen {
			return false
		}
		now := time.Now()
		for _, b := range s.bars {
			b.setClock(s, now)
		}
		step := time.Minute
		if config.HasSeconds(s.cfg.Bar.Clock) {
			step = time.Second
		}
		next := now.Truncate(step).Add(step).Sub(now) + 20*time.Millisecond
		glib.TimeoutAdd(uint32(next.Milliseconds()), &s.onClock, gen)
		return false
	}
}

func (s *shellUI) isFocusedWorkspace(id string) bool {
	for _, w := range s.snap.Workspaces {
		if fmt.Sprint(w.ID) == id {
			return w.Focused
		}
	}
	return false
}

// hyprHandler forwards store callbacks to the main thread.
type hyprHandler struct{ s *shellUI }

func (h hyprHandler) OnSnapshot(snap hypr.Snapshot) { gtkx.Post(func() { h.s.apply(snap) }) }
func (h hyprHandler) OnEvent(e hypr.Event) {
	h.s.overviewEvent(e)
	if e.Name == "screencast" { // "1,OWNER" when a screencast starts, "0,…" when it ends
		on := strings.HasPrefix(e.Data, "1")
		gtkx.Post(func() { h.s.passCapture(func(m *shell.PassMask) { m.Screencast(on) }) })
	}
	switch e.Name {
	case "monitoradded", "monitorremoved", "configreloaded":
		h.s.followMonitors()
		gtkx.Post(h.s.showDisplays)
	}
	switch e.Name {
	case "monitoradded", "monitorremoved":
		// A display plugged in or out with the lid closed turns the
		// built-in one off or on (lid_off).
		gtkx.Post(h.s.applyHyprland)
	}
	if e.Name == "configreloaded" {
		// inua.conf may have changed what is in effect.
		gtkx.Post(func() {
			h.s.refreshHyprInput()
			h.s.installOverviewKeys()
			if h.s.overview.st.Open {
				h.s.overview.mouseGuard = false
				h.s.guardOverviewMouse(true)
			}
		})
	}
}
func (h hyprHandler) OnConnection(up bool, err error) {
	log.Printf("hyprland: connected=%v err=%v", up, err)
	if up { // Hyprland may have started again, without the virtual screen
		h.s.followMonitors()
	}
	gtkx.Post(func() {
		h.s.connected = up
		if up {
			h.s.installOverviewKeys()
		}
		h.s.launcher.st.SetConnected(up)
		h.s.syncLauncher()
	})
}

func (s *shellUI) apply(snap hypr.Snapshot) {
	s.snap = snap
	s.overview.st.SetSnapshot(snap)
	for _, b := range s.bars {
		b.setHypr(s, snap)
	}
	s.renderDocks()
	s.launcher.st.SetSnapshot(snap)
	s.syncLauncher()
	if !s.overview.st.Open {
		s.saveOverviewHistory()
		return
	}
	if s.overview.dragging != "" || s.overview.movesPending > 0 {
		return
	}
	s.scheduleOverviewRender()
}

// dispatch runs a Hyprland dispatcher in the background; the outcome shows
// up as events.
// refocusPointer moves the pointer to where it is, once a panel that covers
// the screen has appeared. Hyprland leaves the pointer with the surface it
// was over until it moves, so a click without moving (a second tap on the
// clock to close the panel) would go nowhere.
func (s *shellUI) refocusPointer() {
	if s.client == nil {
		return
	}
	lua := s.hyprLua
	go func() {
		time.Sleep(80 * time.Millisecond) // until the surface is on screen
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		b, err := s.client.Request(ctx, "j/cursorpos")
		if err != nil {
			return
		}
		var p struct{ X, Y float64 }
		if json.Unmarshal(b, &p) != nil {
			return
		}
		if lua {
			err = s.client.LuaDispatch(ctx, fmt.Sprintf("hl.dsp.cursor.move({ x = %d, y = %d })", int(p.X), int(p.Y)))
		} else {
			err = s.client.Dispatch(ctx, fmt.Sprintf("movecursor %d %d", int(p.X), int(p.Y)))
		}
		if err != nil {
			log.Printf("hyprland: movecursor: %v", err)
		}
	}()
}

// dispatchSubmap enters a submap, or leaves it with "reset": in Lua where
// Hyprland runs it, else with the classic dispatcher.
func (s *shellUI) dispatchSubmap(name string) {
	if !s.hyprLua {
		s.dispatch("submap " + name)
		return
	}
	if s.client == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.client.LuaDispatch(ctx, fmt.Sprintf("hl.dsp.submap(%q)", name)); err != nil {
			log.Printf("hyprland: submap %s: %v", name, err)
		}
	}()
}

func (s *shellUI) dispatch(arg string) {
	if s.client == nil {
		log.Printf("hyprland: not connected, dropping %q", arg)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.client.Dispatch(ctx, arg); err != nil {
			log.Printf("hyprland: %s: %v", arg, err)
		}
	}()
}

// dispatchEither runs a Lua dispatcher expression (hl.dsp.…) when Hyprland
// runs Lua, else the classic dispatcher; lua "" always runs the classic.
func (s *shellUI) dispatchEither(classic, lua string) {
	if !s.hyprLua || lua == "" {
		s.dispatch(classic)
		return
	}
	if s.client == nil {
		log.Printf("hyprland: not connected, dropping %q", lua)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.client.LuaDispatch(ctx, lua); err != nil {
			log.Printf("hyprland: %v", err)
		}
	}()
}

// focusWindow focuses a window by address ("0x…").
func (s *shellUI) focusWindow(addr string) {
	s.dispatchEither("focuswindow address:"+addr, fmt.Sprintf(`hl.dsp.focus({window=%q})`, "address:"+addr))
}

// switchWorkspace shows a workspace by ID. Only numeric IDs have a Lua
// form here (a quoted one would name a new workspace).
func (s *shellUI) switchWorkspace(id string) {
	lua := ""
	if _, err := strconv.Atoi(id); err == nil {
		lua = "hl.dsp.focus({workspace=" + id + "})"
	}
	s.dispatchEither("workspace "+id, lua)
}

// focusedMonitor is the GDK monitor Hyprland has focused, nil if unknown.
func (s *shellUI) focusedMonitor() *gdk.Monitor {
	for _, b := range s.bars {
		if b.connector() == s.snap.FocusedMonitor {
			return b.mon
		}
	}
	return nil
}

// sortedBars lists the bars by monitor name.
func (s *shellUI) sortedBars() []*bar {
	return slices.SortedFunc(maps.Values(s.bars), func(a, b *bar) int { return strings.Compare(a.connector(), b.connector()) })
}

func (s *shellUI) barNames() []string {
	var names []string
	for _, b := range s.sortedBars() {
		names = append(names, b.connector())
	}
	return names
}

// --- control socket ---

// request serves the control socket on the main thread:
//
//	launcher toggle|apps|windows|close
//	control  toggle|open|close
//	settings toggle|open [PAGE]|close
//	notifications toggle|open|close|clear|focus [on|off]|list|dismiss ID|invoke ID [KEY]
//	system   status|set KEY VALUE|power ACTION
//	media    status|play-pause|next|previous
//	shell    state|close|reload|restart|snapshot DIR|tray-menu N
func (s *shellUI) request(req ipc.Request) ipc.Response {
	if req.Op == "show" {
		return ipc.Response{OK: true, Result: "launcher: toggle apps windows close (apps|windows QUERY…)\noverview: toggle open close\ncontrol: toggle open close\nsettings: toggle open close\nnotifications: toggle open close clear focus list dismiss invoke\nmedia: status play-pause next previous\nvoice: toggle start stop cancel\nsystem: status set (wifi bluetooth volume mute brightness focus saver) power (suspend reboot poweroff logout)\nscreenshot: area silent-area pick-window pick-screen screen window color record stop close\nlock: now unlock state\nclipboard: toggle open close\npass: toggle open close\ndrawer: toggle open close\nshell: state close reload restart snapshot tray-menu"}
	}
	if req.Op != "call" {
		return ipc.Response{Error: "unknown op " + req.Op}
	}
	bad := ipc.Response{Error: fmt.Sprintf("unknown function %s %s", req.Target, req.Function)}
	if (s.lock.locked || s.lock.locking) && req.Target != "lock" && req.Target != "shell" {
		return ipc.Response{Error: "the screen is locked"}
	}
	switch req.Target {
	case "launcher":
		switch req.Function {
		case "toggle":
			if s.launcher.st.Open {
				s.cancelLauncher()
			} else {
				m, _ := shell.ParseMode(s.cfg.Launcher.Mode) // validated by config.Load
				s.showLauncher(m)
			}
		case "close":
			s.cancelLauncher()
		default:
			m, ok := shell.ParseMode(req.Function)
			if !ok {
				return bad
			}
			s.showLauncher(m)
			if len(req.Args) > 0 { // a query to start with
				s.launcher.query.SetText(strings.Join(req.Args, " "))
				s.launcher.query.SetPosition(-1)
			}
		}
	case "overview":
		switch req.Function {
		case "toggle":
			s.showOverview()
		case "open":
			if !s.overview.st.Open {
				s.showOverview()
			}
		case "close":
			s.closeOverview(true)
		default:
			return bad
		}
	case "control":
		switch req.Function {
		case "toggle":
			s.toggleControl()
		case "open":
			s.openControl()
		case "close":
			s.closeControl()
		default:
			return bad
		}
	case "calendar":
		switch req.Function {
		case "toggle":
			s.toggleAgenda()
		case "open":
			s.openAgenda()
		case "close":
			s.closeAgenda()
		default:
			return bad
		}
	case "clipboard":
		switch req.Function {
		case "toggle":
			if s.clipboard.open {
				s.closeClipboard()
			} else {
				s.openClipboard(nil)
			}
		case "open":
			s.openClipboard(nil)
		case "close":
			s.closeClipboard()
		default:
			return bad
		}
	case "pass":
		switch req.Function {
		case "toggle":
			if s.passwords.open {
				s.closePasswords()
			} else {
				s.openPasswords(nil)
			}
		case "open":
			s.openPasswords(nil)
		case "close":
			s.closePasswords()
		default:
			return bad
		}
	case "drawer":
		if !s.drawerRequest(req.Function) {
			return bad
		}
	case "lock":
		switch req.Function {
		case "now":
			if err := s.lockScreen(); err != nil {
				return ipc.Response{Error: "lock: " + err.Error()}
			}
		case "unlock":
			s.unlockScreen()
		case "state":
			return ipc.Response{OK: true, Result: fmt.Sprint(s.lock.locked || s.lock.locking)}
		default:
			return bad
		}
	case "screenshot":
		if !s.screenshotRequest(req.Function) {
			return bad
		}
	case "system":
		return s.systemRequest(req.Function, req.Args)
	case "media":
		return s.mediaRequest(req.Function)
	case "voice":
		return s.voiceRequest(req.Function)
	case "notifications":
		switch req.Function {
		case "list":
			return s.notificationList()
		case "dismiss", "invoke":
			return s.noteRequest(req.Function, req.Args)
		case "toggle":
			s.toggleCenter()
		case "open":
			s.openCenter()
		case "close":
			s.closeCenter()
		case "clear":
			if s.notes.open { // as Clear All does: they slide away
				var keys []string
				for _, g := range s.notes.c.Groups() {
					keys = append(keys, g.Key)
				}
				s.clearAnimated(keys)
			} else {
				s.clearNotes(s.notes.c.ClearAll())
			}
		case "focus":
			on := !s.notes.c.Focus
			if len(req.Args) == 1 {
				on = req.Args[0] == "on"
			}
			s.setFocus(on)
		default:
			return bad
		}
	case "settings":
		switch req.Function {
		case "toggle":
			if s.settings.win != nil {
				s.closeSettings()
			} else {
				s.openSettings()
			}
		case "open":
			s.openSettings()
			if len(req.Args) == 1 && !s.showSettingsPage(req.Args[0]) {
				return ipc.Response{Error: "unknown settings page " + req.Args[0] + " (" + strings.Join(settingsPages, ", ") + ")"}
			}
		case "close":
			s.closeSettings()
		default:
			return bad
		}
	case "shell":
		return s.shellRequest(req.Function, req.Args)
	default:
		return ipc.Response{Error: "unknown target " + req.Target}
	}
	return ipc.Response{OK: true}
}

func (s *shellUI) shellRequest(fn string, args []string) ipc.Response {
	switch fn {
	case "close":
		s.cancelLauncher()
		s.closeOverview(false)
		s.closeControl()
		s.closeAgenda()
		if s.tray.menu != nil {
			s.tray.menu.Popdown()
		}
	case "reload":
		return s.reload()
	case "restart":
		if err := s.scheduleRestart(); err != nil {
			return ipc.Response{Error: "restart: " + err.Error()}
		}
	case "tray-menu":
		n := 0
		if len(args) == 1 {
			fmt.Sscan(args[0], &n)
		}
		if err := s.trayMenu(n); err != nil {
			return ipc.Response{Error: err.Error()}
		}
	case "snapshot":
		if len(args) != 1 {
			return ipc.Response{Error: "usage: snapshot DIR"}
		}
		files, err := s.snapshot(args[0])
		if err != nil {
			return ipc.Response{Error: err.Error()}
		}
		return ipc.Response{OK: true, Result: strings.Join(files, "\n")}
	case "state":
		l := &s.launcher
		surfaces := 0
		if s.overview.st.Open {
			surfaces = len(s.overview.views) + 1
		}
		return ipc.Response{OK: true, Result: fmt.Sprintf(
			"pid=%d layer=%v layershell=%s bars=%s hyprland=%v launcher.visible=%v launcher.focus=%v launcher.mode=%s launcher.results=%d launcher.message=%q launcher.opens=%d toplevels=%d workspaces=%d control.open=%v tray=%d overview.open=%v overview.dragging=%q overview.surfaces=%d overview.kill=%v overview.search=%v",
			os.Getpid(), s.layer, layershell.Version(), strings.Join(s.barNames(), ","), s.connected,
			l.st.Open, l.hasFocus, l.st.Mode, len(l.st.Results), l.st.Message(), l.opens,
			len(s.snap.Toplevels), len(s.snap.Workspaces), s.control.open, len(s.tray.items), s.overview.st.Open, s.overview.dragging, surfaces, s.overview.killMode, s.overview.searchMode)}
	default:
		return ipc.Response{Error: "unknown function shell " + fn}
	}
	return ipc.Response{OK: true}
}

// runAfter runs f on the main loop in ms milliseconds.
func (s *shellUI) runAfter(ms uint32, f func()) {
	if s.afterFns == nil {
		s.afterFns = map[uintptr]func(){}
	}
	s.afterSeq++
	s.afterFns[s.afterSeq] = f
	glib.TimeoutAdd(ms, &s.onAfter, s.afterSeq)
}

// scanApps reads the desktop entries, for the launcher, the dock and the
// notifications, off the main loop.
func (s *shellUI) scanApps() {
	s.appsStamp = desktop.Stamp(desktop.DataDirs())
	go func() {
		locale := os.Getenv("LANG")
		if selected := i18n.Lang(); selected != "" {
			locale = selected
		}
		entries := desktop.Scan(desktop.DataDirs(), locale)
		gtkx.Post(func() {
			s.apps = entries
			s.launcher.st.SetApps(entries)
			s.dockApps(entries)
			s.notes.apps = map[string]*desktop.Entry{}
			for _, e := range entries {
				s.notes.apps[e.ID] = e
				s.notes.apps[strings.ToLower(e.ID)] = e
			}
			s.syncLauncher()
			log.Printf("desktop: %d entries", len(entries))
		})
	}()
}

// freshenApps scans the desktop entries again when an applications folder
// changed since: an app installed or removed (a workshop app among them)
// shows without a restart.
func (s *shellUI) freshenApps() {
	if desktop.Stamp(desktop.DataDirs()) != s.appsStamp {
		s.scanApps()
	}
}
