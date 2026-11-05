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
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"codeberg.org/puregotk/purego"
	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/compositor"
	_ "github.com/lyricat/inua/internal/compositors" // the backends Detect knows
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/hypr"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/ipc"
	"github.com/lyricat/inua/internal/layershell"
	"github.com/lyricat/inua/internal/portalconf"
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

	// comp is the compositor this shell runs on (nil: none found); hypr
	// is the same backend when it is Hyprland, for what only Hyprland
	// has (Lua listeners, binds, displays, the virtual screen).
	comp compositor.Backend
	hypr *hypr.Backend
	// saidFallback: the login's fallback, if any, has been told (sayFallback).
	saidFallback bool
	caps         compositor.Capabilities
	snap         compositor.Snapshot
	connected    bool
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
	// The wheel over the bars' workspaces, and over the docks.
	workspaceWheel, dockWheel shell.WheelStep

	bars      map[uintptr]*bar // by GdkMonitor
	styles    styles
	clockGen  uintptr // the running clock timer; see restartClock
	launcher  launcher
	overview  workspaceOverview
	control   control
	clipboard clipboardPanel
	passwords passwordsPanel
	agenda    agenda
	widgets   widgetPage
	shot      shooter
	recording recorder
	lock      locker
	auth      authAgent   // the polkit agent
	portals   portals     // inua's portal backend (docs/portals.md)
	pass      passPrompts // gpg's passphrases (inua-pinentry)
	alarms    alarms
	settings  settings
	wifi      wifiState // the networks, while a list shows them
	tray      tray
	appmenu   appMenuState
	osd       osd
	dictation dictationCard // dictation as it goes
	sys       shell.System
	svc       services

	// Long-lived signal handlers, shared by all widgets (see package gtkx).
	onMonitorsChanged      func(uintptr, uint32, uint32, uint32, uintptr)
	onConnector            func(uintptr, uintptr, uintptr)
	onWorkspaceClicked     func(gtk.Button)
	onWorkspaceSettle      glib.SourceFunc
	onClock                glib.SourceFunc
	onKey                  func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onLauncherPick         func(gtk.GestureClick, int32, float64, float64)
	onLauncherCatch        func(gtk.GestureClick, int32, float64, float64)
	onLauncherMenuClosed   func(gtk.Popover)
	onLauncherGone         glib.SourceFunc
	onLauncherScrolled     func(gtk.Adjustment)
	onLauncherLaidOut      func(gtk.Adjustment)
	onLauncherFootLaidOut  func(gtk.Adjustment)
	onLauncherTick         gtk.TickCallback
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
	onPanelMotion          func(gtk.EventControllerMotion, float64, float64)
	onPanelLeave           func(gtk.EventControllerMotion)
	onBarPressed           func(gtk.GestureClick, int32, float64, float64)
	onBarReleased          func(gtk.GestureClick, int32, float64, float64)
	onBarScroll            func(gtk.EventControllerScroll, float64, float64) bool
	onWorkspaceScroll      func(gtk.EventControllerScroll, float64, float64) bool
	onDockScroll           func(gtk.EventControllerScroll, float64, float64) bool
	onSlider               func(gtk.Range, gtk.ScrollType, float64) bool
	onBackdropPressed      func(gtk.GestureClick, int32, float64, float64)
	onControlKey           func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onControlGone          glib.SourceFunc
	onControlTick          glib.SourceFunc
	onPowerToggled         func(gtk.ToggleButton)
	onPowerDisarm          glib.SourceFunc
	onTrayClicked          func(gtk.Button)
	onTrayPressed          func(gtk.GestureClick, int32, float64, float64)
	onTrayMenuClosed       func(gtk.Popover)
	onAppMenuClicked       func(gtk.Button)
	onAppMenuClosed        func(gtk.Popover)
	onSettingsEntry        func(uintptr, uintptr)
	onSettingsSwitch       func(gtk.Switch, bool) bool
	onAIAbility            func(gtk.ToggleButton)
	onAIModelSearch        func(gtk.SearchEntry)
	onAIModelPick          func(gtk.Button)
	onSettingsDrop         func(uintptr, uintptr, uintptr)
	onSettingsPage         func(uintptr, uintptr, uintptr)
	onDefaultApp           func(uintptr, uintptr, uintptr)
	onDisplayDrop          func(uintptr, uintptr, uintptr)
	onPowerDrop            func(uintptr, uintptr, uintptr) // the Power page's drop-downs
	onCompositorPick       func(gtk.ToggleButton)          // the Compositor page's choice
	onCompositorLogout     func(gtk.Button)
	onVoiceSwitch          func(gtk.Switch, bool) bool
	onVoiceMode            func(gtk.ToggleButton)
	onVoiceRuns            func(gtk.ToggleButton)
	onVoiceShow            func(uintptr, uintptr, uintptr)
	onSoundSlider          func(gtk.Range, gtk.ScrollType, float64) bool
	onSoundMute            func(gtk.Button)
	onSoundPlay            func(gtk.Button)
	onVoiceKeys            func(gtk.Button)
	onVoiceSetup           func(gtk.Button)
	onVoiceInstall         func(gtk.Button)
	onVoiceSite            func(gtk.Button)
	onVoiceAI              func(gtk.Button)
	onVoiceStart           func(gtk.Button)
	onVoxtypeFilter        func(uintptr, uintptr, uintptr)
	onVoxtypeRow           func(gtk.Button)
	onVoxtypeMore          func(gtk.Button)
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
	onThemeMenu            func(gtk.GestureClick, int32, float64, float64)
	onThemeMenuClosed      func(gtk.Popover)
	onIconSetMenu          func(gtk.GestureClick, int32, float64, float64)
	onPreviewKey           func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
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
	onShotCamera           func(gtk.Button)
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
	onWidgetsMark          func(gtk.Button)
	onWidgetsKey           func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	onWidgetsRevealed      func(uintptr, uintptr, uintptr)
	onWidgetButton         func(gtk.Button)
	onWidgetMenu           func(gtk.GestureClick, int32, float64, float64)
	onWidgetMenuClosed     func(gtk.Popover)
	onWidgetsAddActive     func(uintptr, uintptr, uintptr)
	onWidgetDrop           func(gtk.DropTarget, uintptr, float64, float64) bool
	onWidgetQuery          func(gtk.SearchEntry)
	onWidgetQueryActivate  func(gtk.SearchEntry)
	onWidgetDragBegin      func(gtk.GestureDrag, float64, float64)
	onWidgetDragUpdate     func(gtk.GestureDrag, float64, float64)
	onWidgetDragEnd        func(gtk.GestureDrag, float64, float64)
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
	onPassTextActivate     func(gtk.Entry)
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
	onDockMenuItem         func(gtk.Button)
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

	// The settings' file choosers through inua's picker, when it is on.
	restore := gtkx.ForcePortals(portalconf.PickerOn())
	app := gtk.NewApplication(shell.AppID, gio.GApplicationNonUniqueValue)
	s := &shellUI{opts: opts, cfg: opts.Config, app: app, bars: map[uintptr]*bar{}}
	s.pass.slot = make(chan struct{}, 1) // before the control socket serves
	activate := func(gio.Application) {
		restore() // the display is open
		s.start()
	}
	app.ConnectActivate(&activate)
	shutdown := func(gio.Application) { s.stopClipboardCapture() }
	app.ConnectShutdown(&shutdown)

	go func() {
		log.Printf("ipc: serving on %s", socketPath)
		err := ipc.ServeListener(context.Background(), ln, socketPath, func(ctx context.Context, req ipc.Request) ipc.Response {
			if req.Op == "call" && req.Target == "password" {
				return s.askPassword(ctx, req) // waits for the user, off the main loop
			}
			if req.Op == "call" && req.Target == "share" {
				return s.askShare(ctx, req) // as the password
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
		if !s.overviewAvailable() {
			return
		}
		s.showOverview()
		g.SetState(gtk.EventSequenceClaimedValue)
	}
	s.bindControlHandlers()
	s.bindOSDHandlers()
	s.bindAgendaHandlers()
	s.bindWidgetsHandlers()
	s.bindShotHandlers()
	s.bindShotDrawerHandlers()
	s.bindRecordHandlers()
	s.bindVoiceHandlers()
	s.bindTrayHandlers()
	s.bindSettingsHandlers()
	s.bindAppMenuHandlers()
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
	s.startPortals()
	s.startAppMenu()
	if s.cfg.Bar.Tray {
		s.startTray()
	}

	if b, err := compositor.Detect(); err != nil {
		log.Printf("compositor: %v", err)
	} else {
		s.comp = b
		s.setCaps(b.Capabilities())
		if h, ok := b.(*hypr.Backend); ok {
			s.hypr = h
			h.FreshWorkspace = s.freshWorkspace()
			h.OnEvent = s.onHyprEvent
		}
		s.overview.st.NewWorkspace = s.newWorkspace
		go b.Run(context.Background(), compHandler{s})
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

// barClick answers a press at x, y of a panel's window that covers the
// bar b (the bar's own, a panel hanging in it): an entry there opens its
// panel, the one open giving way, or closes its own; elsewhere on the bar
// the panels close. It reports whether the press was on the bar.
func (s *shellUI) barClick(b *bar, x, y float64) bool {
	if b == nil || !b.overIsland(y) {
		return false
	}
	end, _ := b.endReach(x, y)
	clock, _ := b.clockReach(x, y)
	switch {
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
	default:
		s.closeControl()
		s.closeAgenda()
		s.closePasswords()
		s.closeCenter()
		s.closeClipboard()
		s.closeDrawer()
	}
	return true
}

// barUnder is the bar of the monitor w's window is on.
func (s *shellUI) barUnder(w *gtk.Widget) *bar {
	native := w.GetNative()
	if native == nil {
		return nil
	}
	defer (&gobject.Object{Ptr: native.Ptr}).Unref() // GetNative returned a new reference
	surface := native.GetSurface()
	if surface == nil {
		return nil
	}
	mon := gdk.DisplayGetDefault().GetMonitorAtSurface(surface)
	if mon == nil {
		return nil
	}
	for _, b := range s.bars {
		if b.mon.GoPointer() == mon.GoPointer() {
			return b
		}
	}
	return nil
}

// followBar passes a panel's pointer over the bar on to it (onPanelMotion).
func (s *shellUI) followBar(w *gtk.Widget) {
	motion := gtk.NewEventControllerMotion()
	motion.ConnectEnter(&s.onPanelMotion)
	motion.ConnectMotion(&s.onPanelMotion)
	motion.ConnectLeave(&s.onPanelLeave)
	w.AddController(&motion.EventController)
}

func (s *shellUI) bindHandlers() {
	s.onMonitorsChanged = func(_ uintptr, _, removed, added uint32, _ uintptr) {
		log.Printf("monitors: %d removed, %d added", removed, added)
		s.syncBars()
		if s.cfg.Background.Generate {
			s.loadWallpaper() // drawn for the largest monitor, which may be another
		}
	}
	// The entries' reaches show hovered, a panel open or not.
	s.onBarMotion = func(c gtk.EventControllerMotion, x, y float64) {
		if b := s.barOf(c.GetWidget()); b != nil {
			b.hover(x, y)
		}
	}
	s.onBarLeave = func(c gtk.EventControllerMotion) {
		if b := s.barOf(c.GetWidget()); b != nil {
			b.hover(-1, -1)
		}
	}
	// A panel over the whole monitor, the bar too, passes the pointer on
	// to the bar under it (barClick).
	s.onPanelMotion = func(c gtk.EventControllerMotion, x, y float64) {
		if b := s.barUnder(c.GetWidget()); b != nil {
			b.hover(x, y)
		}
	}
	s.onPanelLeave = func(c gtk.EventControllerMotion) {
		if b := s.barUnder(c.GetWidget()); b != nil {
			b.hover(-1, -1)
		}
	}
	// While a panel hangs from the bar, its window's clicks are the
	// panel's (barClick for those on the bar).
	s.onBarPressed = func(g gtk.GestureClick, _ int32, _, _ float64) {
		if b := s.barOf(g.GetWidget()); b != nil {
			b.pressHeld = b.held > 0
		}
	}
	s.onBarReleased = func(g gtk.GestureClick, _ int32, x, y float64) {
		b := s.barOf(g.GetWidget())
		if b == nil || b.pressHeld || b.held > 0 {
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
		if b == nil || b.held > 0 || dy == 0 || !b.status.HasCssClass("hover") {
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
	// The wheel over a bar's workspaces goes to the next or the previous
	// one the bar shows.
	s.onWorkspaceScroll = func(c gtk.EventControllerScroll, _, dy float64) bool {
		for _, b := range s.bars {
			if w := c.GetWidget(); w != nil && w.GoPointer() == b.workspaces.GoPointer() {
				s.scrollWorkspaces(b, int(dy))
				return true
			}
		}
		return false
	}
	s.onWorkspaceClicked = func(b gtk.Button) {
		s.closeWidgets() // back to the workspace under it, or on to another
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
		s.tickWidgets(now)
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

// compHandler forwards the backend's callbacks to the main thread.
type compHandler struct{ s *shellUI }

func (h compHandler) OnSnapshot(snap compositor.Snapshot) { gtkx.Post(func() { h.s.apply(snap) }) }
func (h compHandler) OnCapabilities(caps compositor.Capabilities) {
	gtkx.Post(func() {
		h.s.setCaps(caps)
		h.s.hyprLua = h.s.hypr != nil && h.s.hypr.Lua()
		if h.s.settings.win != nil {
			h.s.showSourceWarning() // what the pages can do may have changed
		}
	})
}

// onHyprEvent takes Hyprland's raw events, for what the snapshot does not
// carry: the overview's custom events, screencasts, config reloads.
func (s *shellUI) onHyprEvent(e hypr.Event) {
	h := compHandler{s}
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
func (h compHandler) OnConnection(up bool, err error) {
	log.Printf("%s: connected=%v err=%v", h.s.comp.Name(), up, err)
	if up { // Hyprland may have started again, without the virtual screen
		h.s.followMonitors()
	}
	gtkx.Post(func() {
		h.s.connected = up
		if up {
			h.s.installOverviewKeys()
			if !h.s.saidFallback {
				h.s.saidFallback = true
				h.s.sayFallback()
			}
		}
		h.s.launcher.st.SetConnected(up)
		h.s.syncLauncher()
	})
}

func (s *shellUI) apply(snap compositor.Snapshot) {
	if s.hypr == nil { // Hyprland says so with events, and keeps its rules itself
		s.followOutputs(s.snap.Outputs, snap.Outputs)
	}
	s.snap = snap
	s.overview.st.SetSnapshot(snap)
	for _, b := range s.bars {
		b.setHypr(s, snap)
	}
	s.renderDocks()
	s.launcher.st.SetSnapshot(snap)
	s.syncLauncher()
	s.syncPortalApps()
	s.followWidgets()
	if !s.overview.st.Open {
		s.saveOverviewHistory()
		return
	}
	if s.overview.dragging != "" || s.overview.movesPending > 0 {
		return
	}
	s.scheduleOverviewRender()
}

// panelLayer is the layer of a panel the user calls up over their
// windows (the launcher, the overview, the clipboard, the passwords).
// Hyprland shows the top layer over a fullscreen window while it holds
// the keyboard; niri and Wayfire keep it under one, where the panel
// would open unseen and take no keys, so there it goes on the overlay.
func (s *shellUI) panelLayer() layershell.Layer {
	if s.comp != nil && s.hypr == nil {
		return layershell.LayerOverlay
	}
	return layershell.LayerTop
}

// setCaps keeps what the compositor can do, and takes the window commands
// it has no way to do out of the launcher.
func (s *shellUI) setCaps(caps compositor.Capabilities) {
	s.caps = caps
	var without []string
	if !caps.Has(compositor.CapFloating) {
		without = append(without, "float")
	}
	if !caps.Has(compositor.CapArrange) {
		without = append(without, "fullscreen", "center")
	}
	s.launcher.st.SetWithout(without...)
}

// followOutputs keeps up with the displays of a compositor that takes
// its settings from the shell: a display that came is given its rule (a
// rule for one that is away was passed over), and the Displays page shows
// them as they now are.
func (s *shellUI) followOutputs(old, now []compositor.Output) {
	if slices.Equal(old, now) {
		return
	}
	name := func(o compositor.Output) string { return o.Name }
	came := slices.ContainsFunc(now, func(o compositor.Output) bool {
		return !slices.ContainsFunc(old, func(p compositor.Output) bool { return name(p) == name(o) })
	})
	if came && len(old) > 0 && len(s.cfg.Displays.Monitors) > 0 {
		s.applyHyprland()
	}
	if s.settings.win != nil {
		s.showDisplays()
	}
}

// refocusPointer moves the pointer to where it is, once a panel that covers
// the screen has appeared. Hyprland leaves the pointer with the surface it
// was over until it moves, so a click without moving (a second tap on the
// clock to close the panel) would go nowhere.
func (s *shellUI) refocusPointer() {
	p, ok := s.comp.(compositor.Pointer)
	if !ok || !s.caps.Has(compositor.CapPointer) {
		return
	}
	go func() {
		time.Sleep(80 * time.Millisecond) // until the surface is on screen
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		x, y, err := p.CursorPos(ctx)
		if err != nil {
			return
		}
		if err := p.MoveCursor(ctx, x, y); err != nil {
			log.Printf("compositor: move cursor: %v", err)
		}
	}()
}

// dispatchSubmap enters a key mode (a submap), or leaves it with "" or
// "reset", where the compositor has them.
func (s *shellUI) dispatchSubmap(name string) {
	m, ok := s.comp.(compositor.Moder)
	if !ok {
		return
	}
	if name == "reset" {
		name = ""
	}
	s.compositorDo("mode "+name, func(ctx context.Context) error { return m.EnterMode(ctx, name) })
}

// compositorDo runs a compositor action in the background and logs its
// failure; what is a log line here is what the audit records as the
// shell's behaviour, the outcome showing up as a snapshot.
func (s *shellUI) compositorDo(what string, do func(ctx context.Context) error) {
	if s.comp == nil {
		log.Printf("compositor: not connected, dropping %s", what)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := do(ctx); err != nil {
			log.Printf("compositor: %s: %v", what, err)
		}
	}()
}

// focusWindow focuses a window by its id.
func (s *shellUI) focusWindow(id string) {
	s.compositorDo("focus window "+id, func(ctx context.Context) error { return s.comp.FocusWindow(ctx, id) })
}

// switchWorkspace shows a workspace by its id.
func (s *shellUI) switchWorkspace(id string) {
	s.compositorDo("focus workspace "+id, func(ctx context.Context) error { return s.comp.FocusWorkspace(ctx, id) })
}

// compositorName is the compositor's name as people write it.
func (s *shellUI) compositorName() string {
	if s.comp == nil {
		return "Hyprland"
	}
	switch name := s.comp.Name(); name {
	case "hyprland":
		return "Hyprland"
	case "wayfire":
		return "Wayfire"
	default:
		return name
	}
}

// unsupported reports whether the compositor the shell runs on cannot do
// c at all: a settings page then says so instead of offering controls
// that do nothing. Without a compositor (not connected yet) nothing is
// known to be missing.
func (s *shellUI) unsupported(c compositor.Capability) bool {
	return s.comp != nil && s.caps[c] == compositor.Unsupported
}

// hyprClient is the Hyprland client for what only Hyprland has; nil on
// another compositor.
func (s *shellUI) hyprClient() *hypr.Client {
	if s.hypr == nil {
		return nil
	}
	return s.hypr.Client
}

// hyprDispatch runs a Hyprland dispatcher in the background, the Lua form
// where Hyprland runs Lua; nothing on another compositor.
func (s *shellUI) hyprDispatch(classic, lua string) {
	if s.hypr == nil {
		log.Printf("hyprland: not here, dropping %q", classic)
		return
	}
	s.compositorDo(classic, func(ctx context.Context) error { return s.hypr.Dispatch(ctx, classic, lua) })
}

// closeWindow asks a window to close.
func (s *shellUI) closeWindow(id string) {
	s.compositorDo("close window "+id, func(ctx context.Context) error { return s.comp.CloseWindow(ctx, id) })
}

// overviewAvailable says whether the overview can be shown: the
// compositor is there, and on Hyprland it runs Lua (0.55 and later).
func (s *shellUI) overviewAvailable() bool {
	return s.comp != nil && (s.hypr == nil || s.hyprLua)
}

// freshWorkspace is where New workspaces are numbered from on Hyprland:
// past the ten the number keys go to directly when the slots are not
// the overview's.
func (s *shellUI) freshWorkspace() int {
	if s.cfg.Overview.MRU {
		return 1
	}
	return 11
}

// newWorkspace names the workspace a New drop on an output creates, as
// the compositor does; none when it cannot.
func (s *shellUI) newWorkspace(snap compositor.Snapshot, output string, unavailable func(string) bool) (compositor.Workspace, bool) {
	m, ok := s.comp.(compositor.WorkspaceMover)
	if !ok || !s.caps.Has(compositor.CapWorkspaces) {
		return compositor.Workspace{}, false
	}
	return m.NewWorkspace(snap, output, unavailable)
}

// focusedMonitor is the GDK monitor Hyprland has focused, nil if unknown.
func (s *shellUI) focusedMonitor() *gdk.Monitor {
	for _, b := range s.bars {
		if b.connector() == s.snap.FocusedOutput {
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
//	widgets  toggle|open|close|right|left
//	shell    state|close|reload|restart|snapshot DIR|tray-menu N
func (s *shellUI) request(req ipc.Request) ipc.Response {
	if req.Op == "show" {
		return ipc.Response{OK: true, Result: "launcher: toggle apps windows close (apps|windows QUERY…)\noverview: toggle open close\ncontrol: toggle open close\nsettings: toggle open close\nnotifications: toggle open close clear focus list dismiss invoke\nmedia: status play-pause next previous\nvoice: toggle start stop cancel\nsystem: status set (wifi bluetooth volume mute brightness focus saver) power (suspend reboot poweroff logout)\nscreenshot: area silent-area pick-window pick-screen screen window color record stop close\nlock: now unlock state\nclipboard: toggle open close\npass: toggle open close\ndrawer: toggle open close\nwidgets: toggle open close right left\nshell: state close reload restart snapshot tray-menu"}
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
	case "widgets":
		if !s.widgetsRequest(req.Function) {
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
		return s.voiceRequest(req.Function, req.Args)
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
		s.closeWidgets()
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
			len(s.snap.Windows), len(s.snap.Workspaces), s.control.open, len(s.tray.items), s.overview.st.Open, s.overview.dragging, surfaces, s.overview.killMode, s.overview.searchMode)}
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
