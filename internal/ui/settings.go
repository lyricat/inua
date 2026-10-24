package ui

import (
	"fmt"
	"log"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/clipboard"
	"github.com/lyricat/inua/internal/config"
	"github.com/lyricat/inua/internal/desktop"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
)

// settings is the settings window (settings.ui), a normal toplevel built
// on open and destroyed on close. Every change applies to the running
// shell at once (applyConfig) and is saved to the settings file shortly
// after the last one.
type settings struct {
	win     *gtk.ApplicationWindow // nil while closed
	overlay *gtk.Overlay           // over the window's page, for the Benchmark dialog; made on its first use
	refs    gtkx.Scope

	status, clockPreview  gtk.Label
	pages                 gtk.Stack
	index                 gtk.ListBox // the pages' list, beside them
	themes                gtk.FlowBox
	swatches              map[string]*gtk.Button // by theme, owned by the flow box
	swatchNames           []string               // the themes the swatches show
	themeTrash, iconTrash gtk.Button             // for the user's theme or icon set in use

	bgGenerate, title, tray, barPass, tpAccel, notifyOn, critFocus gtk.Switch
	noteHistory                                                    gtk.Switch
	bgOwn                                                          gtk.Box // the image's rows, hidden while inua draws
	bgAnother                                                      gtk.Button
	barOn, dockOn, dockHide, dockDrawer, dockFlushBar              gtk.Switch
	clipEnable, clipBar, clipPaste                                 gtk.Switch
	dockSize, dockPosition, barDensity                             gtk.DropDown
	dockPinned                                                     gtk.Box
	bgImage, bgColor, clock, terminal, lock, screenshot            gtk.Entry
	clipSensitive                                                  gtk.Entry
	bgFit, ws, launchMode, swap, caps, icons, appIcons, banner     gtk.DropDown
	clipLimit                                                      gtk.DropDown
	clipLimits                                                     []int // clipLimit's choices
	textScale                                                      gtk.DropDown
	textScales                                                     []int    // textScale's choices
	iconSets                                                       []string // icons' choices
	appIconSets                                                    []string // appIcons' choices
	tpSpeed, transparency, radius                                  gtk.Scale
	transparencyVal, radiusVal, clipStats                          gtk.Label
	clipClear, clipReveal                                          gtk.Button

	// Keyboard and touchpad: where the settings come from, and why they
	// would do nothing.
	kbState, kbReadout, kbWarn, tpState, tpValue, tpWarn gtk.Label
	notifyState                                          gtk.Label
	kbReset, tpReset                                     gtk.Button
	// The language, and the offer to restart the shell into a new one.
	language       gtk.DropDown
	langRestartRow gtk.Box
	langRestart    gtk.Button
	// hyprKb is Hyprland's kb_options while inua sets none, so the
	// controls start from what is in effect.
	hyprKb string

	sc       shortcutState     // the Shortcuts page
	ai       aiSettings        // the AI page
	about    aboutSettings     // the About page
	keyring  keyringSettings   // the Keyring page
	apps     appsSettings      // the Default Apps page
	displays displaysSettings  // the Displays page
	bt       bluetoothSettings // the Bluetooth page
	wifi     wifiSettings      // the Wi-Fi page
	voice    voiceSettings     // the Voice page
	power    powerSettings     // the Power page
	search   settingsSearch    // the search over the index

	sticky map[uintptr]*stickyHead // by the pages' vertical adjustments

	// Signal sources by pointer: entries and drop-downs share one handler.
	entries map[uintptr]*gtk.Entry
	drops   map[uintptr]*gtk.DropDown

	filling bool    // controls are being set from the settings; not an edit
	saveGen uintptr // the pending save; see onSettingsSave
	dirty   bool    // a save is pending
	dialog  *gtk.FileDialog
}

// Theme names as shown on the swatches.
var themeKanji = map[string]string{
	"yoru": "夜", "washi": "和紙", "aojashin": "青写真", "suimen": "水面",
	"rinko": "燐光", "bunko": "分光", "denko": "電光",
}

var (
	fitChoices   = config.Fits
	wsChoices    = []string{"monitor", "all"}
	barDensities = []string{"default", "compact"}
	modeChoices  = []string{"apps", "windows"}
	swapChoices  = []string{"", "left", "both"}
	capsChoices  = []string{"", "escape", "ctrl"}
)

func (s *shellUI) bindSettingsHandlers() {
	s.onSettingsEntry = func(ptr, _ uintptr) {
		if e := s.settings.entries[ptr]; e != nil && !s.settings.filling {
			s.settingsEntryChanged(e)
		}
	}
	s.onSettingsSwitch = func(sw gtk.Switch, on bool) bool {
		if s.settings.filling {
			return false
		}
		c := s.cfg
		switch sw.GetName() {
		case "bg_generate":
			c.Background.Generate = on
		case "title":
			c.Bar.Title = on
		case "tray":
			c.Bar.Tray = on
		case "bar_pass":
			c.Bar.Pass = on
		case "bar_enable":
			c.Bar.Enable = on
		case "dock_enable":
			c.Dock.Enable = on
		case "dock_drawer":
			c.Dock.Drawer = on
		case "dock_autohide":
			c.Dock.Autohide = on
		case "dock_flush_bar":
			c.Style.EdgeGaps = !on
		case "notify_enable":
			c.Notify.Enable = on
		case "critical_focus":
			c.Notify.CriticalInFocus = on
		case "notify_history":
			c.Notify.History = on
		case "about_daily":
			c.Notify.Updates = on
		case "keyring_notify":
			c.Keyring.Notify = on
		case "mcp_enable":
			c.MCP.Enable = on
		case "clip_enable":
			c.Clipboard.Enable = on
		case "clip_bar":
			c.Clipboard.Bar = on
		case "clip_paste":
			c.Clipboard.PasteOnClick = on
		case "lid_off":
			c.Displays.LidOff = on
		case "tp_accel":
			c.Input.TouchpadAccel = "flat"
			if on {
				c.Input.TouchpadAccel = "adaptive"
			}
		default:
			s.aiSwitch(sw.GetName(), on, &c)
		}
		s.changeSettings(c)
		return false // let the switch move
	}
	s.onSettingsDrop = func(ptr, _, _ uintptr) {
		d := s.settings.drops[ptr]
		if d == nil || s.settings.filling {
			return
		}
		i := int(d.GetSelected())
		c := s.cfg
		switch d.GetName() {
		case "bg_fit":
			c.Background.Fit = pick(fitChoices, i, c.Background.Fit)
		case "ws":
			c.Bar.Workspaces = pick(wsChoices, i, c.Bar.Workspaces)
		case "bar_density":
			c.Bar.Density = pick(barDensities, i, c.Bar.Density)
		case "launch_mode":
			c.Launcher.Mode = pick(modeChoices, i, c.Launcher.Mode)
		case "dock_size":
			if i >= 0 && i < len(config.DockSizes) {
				c.Dock.Size = config.DockSizes[i]
			}
		case "dock_position":
			c.Dock.Position = pick(config.DockPositions, i, c.Dock.Position)
		case "banner":
			if i >= 0 && i < len(config.BannerChoices) {
				c.Notify.Banner = config.BannerChoices[i]
			}
		case "clip_limit":
			if i >= 0 && i < len(s.settings.clipLimits) {
				c.Clipboard.Limit = s.settings.clipLimits[i]
			}
		case "icons":
			c.Style.Icons = pick(s.settings.iconSets, i, c.Style.Icons)
		case "app_icons":
			c.Style.AppIcons = pick(s.settings.appIconSets, i, c.Style.AppIcons)
		case "text_scale":
			if i >= 0 && i < len(s.settings.textScales) {
				c.Style.TextScale = s.settings.textScales[i]
			}
		case "language":
			c.Language = pick(config.Languages, i, c.Language)
		case "keyring_lock_after":
			if i >= 0 && i < len(keyringLockAfter) {
				c.Keyring.LockAfter = keyringLockAfter[i]
			}
		case "keyring_untrusted":
			c.Keyring.Untrusted = pick(keyringUntrusted, i, c.Keyring.Untrusted)
		case "mcp_policy":
			c.MCP.Policy = pick(config.MCPPolicies, i, c.MCP.Policy)
		case "swap":
			opts := shell.SetSwapCtrlAlt(s.kbOptions(), pick(swapChoices, i, ""))
			c.Input.KbOptions = &opts
		case "caps":
			opts := shell.SetCapsLock(s.kbOptions(), pick(capsChoices, i, ""))
			c.Input.KbOptions = &opts
		default:
			if strings.HasPrefix(d.GetName(), "ai") {
				s.aiDrop(d.GetName(), i, &c)
			}
		}
		s.changeSettings(c)
	}
	s.onSettingsButton = func(b gtk.Button) {
		name := b.GetName()
		if s.shortcutButton(name) || s.aiButton(name) || s.aboutButton(name) || s.keyringButton(name) || s.customButton(name) {
			return
		}
		c := s.cfg
		switch {
		case strings.HasPrefix(name, "theme:"):
			c.Style.Theme = strings.TrimPrefix(name, "theme:")
			s.changeSettings(c)
		case name == "bg_choose":
			s.chooseImage()
		case name == "bg_clear":
			c.Background.Image = ""
			s.changeSettings(c)
		case name == "bg_another":
			s.anotherPicture()
		case name == "clip_clear":
			_ = clipboard.Clear(s.clipboard.path)
			s.refreshClipboard()
			s.updateClipboardStats()
		case name == "clip_reveal":
			go revealFile(s.clipboard.path)
		case strings.HasPrefix(name, "pin-"):
			action, id, _ := strings.Cut(strings.TrimPrefix(name, "pin-"), ":")
			c.Dock.Pinned = movePinned(c.Dock.Pinned, id, action)
			s.changeSettings(c)
		case name == "lang_restart":
			if s.settings.dirty {
				s.saveSettings() // before the restart reads it
			}
			if err := s.scheduleRestart(); err != nil {
				s.setStatus(i18n.Tf("Not restarted: %s", err.Error()), true)
			}
		case name == "kb_reset":
			c.Input.KbOptions = nil
			s.changeSettings(c)
		case name == "tp_reset":
			c.Input.TouchpadSpeed, c.Input.TouchpadAccel = nil, ""
			s.changeSettings(c)
		}
	}
	s.onSettingsScale = func(r gtk.Range, _ gtk.ScrollType, v float64) bool {
		if s.settings.filling {
			return false
		}
		c := s.cfg
		if r.GetName() == "transparency" {
			val := int(math.Round(v))
			c.Style.Transparency = val
			s.settings.transparencyVal.SetText(i18n.Tf("%d%%", val))
			s.changeSettings(c)
			return false
		}
		if r.GetName() == "radius" {
			val := int(math.Round(v))
			c.Style.Radius = val
			s.settings.radiusVal.SetText(i18n.Tf("%d px", val))
			s.changeSettings(c)
			return false
		}
		v = math.Round(max(-1, min(v, 1))*20) / 20 // the steps of 0.05
		c.Input.TouchpadSpeed = &v
		s.changeSettings(c)
		return false // let the scale move
	}
	s.onSettingsScroll = s.updateSticky
	s.onSettingsClose = func(gtk.Window) bool {
		s.closeSettings()
		return true
	}
	s.onSettingsKey = func(_ gtk.EventControllerKey, keyval, keycode uint32, state gdk.ModifierType) bool {
		if s.recordKey(keyval, keycode, state) {
			return true
		}
		if keyval == keyEscape {
			// The dialog first, then a search typed.
			if !s.closeBench() && !s.closeWifiDetails() && !s.clearSettingsSearch() {
				s.closeSettings()
			}
			return true
		}
		if keyval == 'f' && state&gdk.ControlMaskValue != 0 {
			s.focusSettingsSearch()
			return true
		}
		return false
	}
	s.onRecordTimeout = func(gen uintptr) bool {
		if gen == s.settings.sc.recordGen {
			s.stopRecording()
		}
		return false
	}
	s.onSettingsSave = func(gen uintptr) bool {
		if gen == s.settings.saveGen {
			s.saveSettings()
		}
		return false
	}
	s.onImageChosen = func(_, result, _ uintptr) {
		st := &s.settings
		d := st.dialog
		st.dialog = nil
		if d == nil {
			return
		}
		defer d.Unref()
		file, err := d.OpenFinish(&gio.AsyncResultBase{Ptr: result})
		if err != nil || file == nil || st.win == nil {
			return // cancelled, or the window closed meanwhile
		}
		path := file.GetPath()
		unref(file)
		c := s.cfg
		c.Background.Image = shortenHome(path)
		s.changeSettings(c)
	}
}

func pick(choices []string, i int, current string) string {
	if i >= 0 && i < len(choices) {
		return choices[i]
	}
	return current
}

func (s *shellUI) openSettings() {
	st := &s.settings
	if st.win != nil {
		st.win.Present()
		return
	}
	st.win = &gtk.ApplicationWindow{}
	ui := s.load("settings.ui", &st.refs)
	ui.Get("settings", st.win)
	ui.Get("status", &st.status)
	ui.Get("pages", &st.pages)
	ui.Get("index", &st.index)
	ui.Get("themes", &st.themes)
	ui.Get("clock_preview", &st.clockPreview)
	ui.Get("title", &st.title)
	ui.Get("tray", &st.tray)
	ui.Get("bar_pass", &st.barPass)
	ui.Get("bg_image", &st.bgImage)
	ui.Get("bg_color", &st.bgColor)
	ui.Get("clock", &st.clock)
	ui.Get("terminal", &st.terminal)
	ui.Get("lock", &st.lock)
	ui.Get("screenshot", &st.screenshot)
	ui.Get("bg_fit", &st.bgFit)
	ui.Get("ws", &st.ws)
	ui.Get("bar_density", &st.barDensity)
	ui.Get("launch_mode", &st.launchMode)
	ui.Get("icons", &st.icons)
	ui.Get("app_icons", &st.appIcons)
	ui.Get("language", &st.language)
	ui.Get("text_scale", &st.textScale)
	ui.Get("transparency", &st.transparency)
	ui.Get("transparency_val", &st.transparencyVal)
	ui.Get("radius", &st.radius)
	ui.Get("radius_val", &st.radiusVal)
	st.textScales = nil // a new dropdown, without its choices
	st.iconSets, st.appIconSets = nil, nil
	ui.Get("lang_restart_row", &st.langRestartRow)
	ui.Get("lang_restart", &st.langRestart)
	ui.Get("banner", &st.banner)
	ui.Get("bar_enable", &st.barOn)
	ui.Get("dock_enable", &st.dockOn)
	ui.Get("dock_autohide", &st.dockHide)
	ui.Get("dock_flush_bar", &st.dockFlushBar)
	ui.Get("dock_drawer", &st.dockDrawer)
	ui.Get("dock_size", &st.dockSize)
	ui.Get("dock_position", &st.dockPosition)
	ui.Get("dock_pinned", &st.dockPinned)
	ui.Get("sc_inua", &st.sc.inua)
	ui.Get("sc_shots", &st.sc.shots)
	ui.Get("sc_custom", &st.sc.custom)
	ui.Get("sc_hypr", &st.sc.hypr)
	ui.Get("sc_recording", &st.sc.recordingNote)
	ui.Get("sc_warn", &st.sc.warn)
	ui.Get("sc_new_name", &st.sc.newName)
	ui.Get("sc_new_cmd", &st.sc.newCmd)
	var scAdd gtk.Button
	ui.Get("sc_add", &scAdd)
	scAdd.ConnectClicked(&s.onSettingsButton)
	ui.Get("notify_enable", &st.notifyOn)
	ui.Get("critical_focus", &st.critFocus)
	ui.Get("notify_history", &st.noteHistory)
	ui.Get("notify_state", &st.notifyState)
	ui.Get("clip_enable", &st.clipEnable)
	ui.Get("clip_bar", &st.clipBar)
	ui.Get("clip_paste", &st.clipPaste)
	ui.Get("clip_limit", &st.clipLimit)
	ui.Get("clip_sensitive", &st.clipSensitive)
	st.clipLimits = nil // a new dropdown, without its choices
	ui.Get("clip_stats", &st.clipStats)
	ui.Get("clip_clear", &st.clipClear)
	ui.Get("clip_reveal", &st.clipReveal)
	ui.Get("swap", &st.swap)
	ui.Get("caps", &st.caps)
	ui.Get("tp_speed", &st.tpSpeed)
	ui.Get("tp_accel", &st.tpAccel)
	ui.Get("kb_state", &st.kbState)
	ui.Get("kb_readout", &st.kbReadout)
	ui.Get("kb_warn", &st.kbWarn)
	ui.Get("tp_state", &st.tpState)
	ui.Get("tp_value", &st.tpValue)
	ui.Get("tp_warn", &st.tpWarn)
	ui.Get("kb_reset", &st.kbReset)
	ui.Get("tp_reset", &st.tpReset)
	var choose, clearImage gtk.Button
	ui.Get("bg_choose", &choose)
	ui.Get("bg_clear", &clearImage)
	ui.Get("bg_generate", &st.bgGenerate)
	ui.Get("bg_own", &st.bgOwn)
	ui.Get("bg_another", &st.bgAnother)
	var themeAdd, themeFolder, iconAdd, iconFolder gtk.Button
	ui.Get("theme_add", &themeAdd)
	ui.Get("theme_trash", &st.themeTrash)
	ui.Get("theme_folder", &themeFolder)
	ui.Get("iconset_add", &iconAdd)
	ui.Get("iconset_trash", &st.iconTrash)
	ui.Get("iconset_folder", &iconFolder)
	s.buildStickyHeads(ui)
	st.entries = map[uintptr]*gtk.Entry{}
	st.drops = map[uintptr]*gtk.DropDown{}
	s.getAISettings(ui)
	s.getAboutSettings(ui)
	s.getKeyringSettings(ui)
	s.getAppsSettings(ui)
	s.getDisplaySettings(ui)
	s.getWifiSettings(ui)
	s.getBluetoothSettings(ui)
	s.getPowerSettings(ui)
	s.getVoiceSettings(ui)
	s.buildSettingsIndex()
	s.getSettingsSearch(ui)
	ui.Close()

	for _, e := range []*gtk.Entry{&st.bgImage, &st.bgColor, &st.clock, &st.terminal, &st.lock, &st.screenshot, &st.clipSensitive} {
		st.entries[e.GoPointer()] = e
		gtkx.ConnectChanged(e, &s.onSettingsEntry)
	}
	for _, d := range []*gtk.DropDown{&st.bgFit, &st.ws, &st.barDensity, &st.launchMode, &st.swap, &st.caps, &st.icons, &st.appIcons, &st.textScale, &st.language, &st.banner, &st.dockSize, &st.dockPosition, &st.clipLimit} {
		st.drops[d.GoPointer()] = d
		gtkx.ConnectNotify(d, "selected", &s.onSettingsDrop)
	}
	for _, sw := range []*gtk.Switch{&st.bgGenerate, &st.title, &st.tray, &st.barPass, &st.tpAccel, &st.notifyOn, &st.critFocus, &st.noteHistory, &st.barOn, &st.dockOn, &st.dockHide, &st.dockDrawer, &st.dockFlushBar, &st.clipEnable, &st.clipBar, &st.clipPaste, &st.displays.lidOff} {
		sw.ConnectStateSet(&s.onSettingsSwitch)
	}
	for _, b := range []*gtk.Button{&choose, &clearImage, &st.bgAnother, &themeAdd, &st.themeTrash, &themeFolder, &iconAdd, &st.iconTrash, &iconFolder, &st.kbReset, &st.tpReset, &st.langRestart, &st.clipClear, &st.clipReveal} {
		b.ConnectClicked(&s.onSettingsButton)
	}
	st.tpSpeed.ConnectChangeValue(&s.onSettingsScale)
	st.transparency.ConnectChangeValue(&s.onSettingsScale)
	st.radius.ConnectChangeValue(&s.onSettingsScale)
	s.buildSwatches()
	st.hyprKb = s.hyprKbOptions()
	st.sc.binds = s.hyprBinds()
	s.showSourceWarning()

	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCaptureValue) // before entries and buttons, for recording
	keys.ConnectKeyPressed(&s.onSettingsKey)
	st.win.AddController(&keys.EventController)
	st.win.ConnectCloseRequest(&s.onSettingsClose)

	st.win.SetApplication(s.app)
	s.showSettings()
	s.setStatus("", false)
	st.win.Present()
}

// settingsGroups are the index's groups, each a kicker over its pages, in
// the order a page is most likely looked for: the connections, then the
// devices, the desktop, the apps, and the system last.
var settingsGroups = []struct {
	title string // i18n.M: said in the user's language when shown
	pages []string
}{
	{i18n.M("Connections"), []string{"wifi", "bluetooth"}},
	{i18n.M("Devices"), []string{"displays", "keyboard", "mouse"}},
	{i18n.M("Desktop"), []string{"appearance", "bar", "launcher", "notifications", "shortcuts"}},
	{i18n.M("Input"), []string{"voice"}},
	{i18n.M("Apps"), []string{"apps", "ai", "clipboard", "keyring"}},
	{i18n.M("System"), []string{"power", "about"}},
}

// settingsPages are the pages' names in settings.ui, in the index's order.
var settingsPages = func() []string {
	var pages []string
	for _, g := range settingsGroups {
		pages = append(pages, g.pages...)
	}
	return pages
}()

// settingsPageAliases name the page that now holds a section that was once
// a page of its own.
var settingsPageAliases = map[string]string{
	"wallpaper": "appearance", "dock": "bar", "control": "bar", "touchpad": "mouse",
	"network": "wifi", "wi-fi": "wifi",
	"battery": "power", "lid": "power", "idle": "power", "sleep": "power", "suspend": "power",
}

// showSettingsPage turns the open settings window to a page, reporting
// whether it exists.
func (s *shellUI) showSettingsPage(name string) bool {
	st := &s.settings
	if a, ok := settingsPageAliases[name]; ok {
		name = a
	}
	if st.win == nil || !slices.Contains(settingsPages, name) {
		return false
	}
	// At once: a crossfade begun before a new window's first frame stays
	// on the old page.
	st.pages.SetVisibleChildFull(name, gtk.StackTransitionTypeNoneValue)
	return true
}

func (s *shellUI) closeSettings() {
	st := &s.settings
	if st.win == nil {
		return
	}
	s.stopRecording()
	s.closeBench()
	s.closeWifiDetails()
	if st.dirty {
		s.saveSettings()
	}
	s.releaseSearch()
	st.win.Destroy()
	st.refs.Release()
	st.win, st.swatches, st.entries, st.drops, st.sticky, st.overlay = nil, nil, nil, nil, nil, nil
	// Closed with a pairing request on it: the request is turned down.
	if pr := st.bt.prompt; pr != nil {
		select {
		case pr.reply <- system.BluetoothAnswer{}:
		default:
		}
	}
	st.bt.code = nil
	s.syncBluetoothSearch()
}

// buildSwatches adds one button per theme: its ground with its accent and
// text colours, and its name.
func (s *shellUI) buildSwatches() {
	st := &s.settings
	st.themes.RemoveAll()
	st.swatches = map[string]*gtk.Button{}
	st.swatchNames = s.cfg.ThemeNames()
	for _, name := range st.swatchNames {
		btn := gtk.NewButton()
		btn.SetName("theme:" + name)
		btn.AddCssClass("swatch")
		btn.AddCssClass("sw-" + name)
		btn.SetTooltipText(name)

		box := gtk.NewBox(gtk.OrientationVerticalValue, 6)
		ground := gtk.NewBox(gtk.OrientationHorizontalValue, 4)
		ground.AddCssClass("ground")
		ground.SetValign(gtk.AlignStartValue)
		for _, chip := range []string{"chip-accent", "chip-text"} {
			c := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
			c.AddCssClass("chip")
			c.AddCssClass(chip)
			c.SetValign(gtk.AlignEndValue)
			gtkx.Append(ground, &c.Widget)
		}
		gtkx.Append(box, &ground.Widget)
		label := gtk.NewLabel(strings.TrimSpace(themeKanji[name] + "  " + name))
		label.SetXalign(0)
		gtkx.Append(box, &label.Widget)

		btn.SetChild(&box.Widget)
		box.Unref()
		btn.ConnectClicked(&s.onSettingsButton)
		st.themes.Append(&btn.Widget)
		btn.Unref() // the flow box holds it
		st.swatches[name] = btn
	}
	s.loadSwatchStyle()
}

// loadSwatchStyle colours the theme swatches from the theme files, whatever
// the current theme is. The user's themes may have changed since the
// last time, so opening the settings loads it again.
func (s *shellUI) loadSwatchStyle() {
	st := &s.styles
	if st.swatches == nil {
		st.swatches = gtk.NewCssProvider()
		gtk.StyleContextAddProviderForDisplay(gdk.DisplayGetDefault(), st.swatches, uint32(gtk.STYLE_PROVIDER_PRIORITY_APPLICATION))
	}
	var b strings.Builder
	for _, name := range s.cfg.ThemeNames() {
		c := shellThemeColors(s, name)
		fmt.Fprintf(&b, ".swatch.sw-%[1]s .ground { background-color: %[2]s; border: 1px solid %[3]s; }\n"+
			".swatch.sw-%[1]s .chip-accent { background-color: %[4]s; }\n"+
			".swatch.sw-%[1]s .chip-text { background-color: %[5]s; }\n",
			name, c["app"], c["line"], c["accent"], c["t0"])
	}
	st.swatches.LoadFromString(b.String())
}

// showSettings sets every control from the current settings.
func (s *shellUI) showSettings() {
	st := &s.settings
	if st.win == nil {
		return
	}
	st.filling = true
	defer func() { st.filling = false }()
	c := s.cfg
	if !slices.Equal(st.swatchNames, c.ThemeNames()) {
		s.buildSwatches() // a theme was added or taken away
	}
	for name, btn := range st.swatches {
		toggle(&btn.Widget, "on", name == s.theme())
	}
	st.themeTrash.SetSensitive(c.IsCustomTheme(s.theme()))
	st.iconTrash.SetSensitive(c.IconSetDir() != "")
	st.bgGenerate.SetActive(c.Background.Generate)
	st.bgOwn.SetVisible(!c.Background.Generate)
	st.bgAnother.SetVisible(c.Background.Generate)
	st.title.SetActive(c.Bar.Title)
	st.tray.SetActive(c.Bar.Tray)
	st.barPass.SetActive(c.Bar.Pass)
	setEntry(&st.bgImage, c.Background.Image)
	setEntry(&st.bgColor, c.Background.Color)
	setEntry(&st.clock, c.Bar.Clock)
	setEntry(&st.terminal, c.Launcher.Terminal)
	setEntry(&st.lock, c.Control.Lock)
	setEntry(&st.screenshot, c.Control.Screenshot)
	// Only when it says otherwise: "pass, " being typed is "pass".
	if !slices.Equal(sourceList(st.clipSensitive.GetText()), c.Clipboard.SensitiveSources) {
		st.clipSensitive.SetText(strings.Join(c.Clipboard.SensitiveSources, ", "))
	}
	st.bgFit.SetSelected(uint32(max(0, slices.Index(fitChoices, c.Background.Fit))))
	st.ws.SetSelected(uint32(max(0, slices.Index(wsChoices, c.Bar.Workspaces))))
	st.launchMode.SetSelected(uint32(max(0, slices.Index(modeChoices, c.Launcher.Mode))))
	st.barDensity.SetSelected(uint32(max(0, slices.Index(barDensities, c.Bar.Density))))
	st.transparency.SetValue(float64(c.Style.Transparency))
	st.transparencyVal.SetText(i18n.Tf("%d%%", c.Style.Transparency))
	st.radius.SetValue(float64(c.Style.Radius))
	st.radiusVal.SetText(i18n.Tf("%d px", c.Style.Radius))
	s.showIconSets()
	s.showTextScale()
	st.language.SetSelected(uint32(max(0, slices.Index(config.Languages, c.Language))))
	// The shell chose its language when it started.
	st.langRestartRow.SetVisible(c.Language != s.opts.Config.Language)
	s.showInput()
	s.showWifi()
	s.showBluetooth()
	s.showPower()
	s.fillVoiceSettings()
	s.showDisplays()
	s.showNotifySettings()
	s.showDockSettings()
	s.showClipboardSettings()
	s.showShortcuts()
	s.showAISettings()
	s.showAboutSettings()
	st.keyring.notify.SetActive(c.Keyring.Notify)
	s.showKeyringSettings()
	st.clockPreview.SetText(config.Strftime(c.Bar.Clock, time.Now()))
	for _, e := range st.entries {
		e.RemoveCssClass("invalid")
	}
}

// showTextScale offers the usual text sizes, and the one in the file if it
// is another.
// showIconSets offers the built-in icon sets, the user's and the
// installed icon themes for the shell's icons, and the installed themes
// for app icons.
func (s *shellUI) showIconSets() {
	st := &s.settings
	sets := s.cfg.IconSetNames()
	names := slices.Clone(sets)
	names[0], names[1] = i18n.T("Phosphor"), i18n.T("System theme") // config.IconSets
	apps, appNames := []string{"system"}, []string{i18n.T("System theme")}
	for _, t := range config.IconThemes() {
		if !slices.Contains(sets, t.Name) { // a set of the user's of its name wins
			sets, names = append(sets, t.Name), append(names, t.Title)
		}
		apps, appNames = append(apps, t.Name), append(appNames, t.Title)
	}
	if !slices.Equal(sets, st.iconSets) {
		list := gtk.NewStringList(names)
		st.icons.SetModel(list)
		list.Unref()
		st.iconSets = sets
	}
	if !slices.Equal(apps, st.appIconSets) {
		list := gtk.NewStringList(appNames)
		st.appIcons.SetModel(list)
		list.Unref()
		st.appIconSets = apps
	}
	st.icons.SetSelected(uint32(max(0, slices.Index(sets, s.cfg.Style.Icons))))
	st.appIcons.SetSelected(uint32(max(0, slices.Index(apps, s.cfg.Style.AppIcons))))
}

func (s *shellUI) showTextScale() {
	st := &s.settings
	scale := s.cfg.Style.TextScale
	scales := slices.Clone(config.TextScales)
	if !slices.Contains(scales, scale) {
		scales = append(scales, scale)
		slices.Sort(scales)
	}
	if !slices.Equal(scales, st.textScales) {
		names := make([]string, len(scales))
		for i, n := range scales {
			names[i] = i18n.Tf("%d%%", n)
		}
		list := gtk.NewStringList(names)
		st.textScale.SetModel(list)
		list.Unref()
		st.textScales = scales
	}
	st.textScale.SetSelected(uint32(slices.Index(scales, scale)))
}

func (s *shellUI) showClipboardSettings() {
	st := &s.settings
	c := s.cfg.Clipboard
	st.clipEnable.SetActive(c.Enable)
	st.clipBar.SetActive(c.Bar)
	st.clipPaste.SetActive(c.PasteOnClick)
	// The usual limits, and the one in the file if it is another.
	limits := slices.Clone(config.ClipboardLimitChoices)
	if !slices.Contains(limits, c.Limit) {
		limits = append(limits, c.Limit)
		slices.Sort(limits)
	}
	if !slices.Equal(limits, st.clipLimits) {
		names := make([]string, len(limits))
		for i, n := range limits {
			names[i] = i18n.Nf("%d entry", "%d entries", n, n)
		}
		list := gtk.NewStringList(names)
		st.clipLimit.SetModel(list)
		list.Unref()
		st.clipLimits = limits
	}
	st.clipLimit.SetSelected(uint32(slices.Index(limits, c.Limit)))
	s.updateClipboardStats()
}

func (s *shellUI) updateClipboardStats() {
	st := &s.settings
	if st.win == nil {
		return
	}
	entries, _ := clipboard.Read(s.clipboard.path)
	texts, imgs := 0, 0
	for _, e := range entries {
		if e.Type == "image" {
			imgs++
		} else {
			texts++
		}
	}
	size := clipboard.Size(s.clipboard.path)
	st.clipStats.SetText(i18n.Tf("%s (%s, %s) · %s",
		i18n.Nf("%d entry", "%d entries", len(entries), len(entries)),
		i18n.Nf("%d text", "%d texts", texts, texts),
		i18n.Nf("%d image", "%d images", imgs, imgs),
		i18n.Tf("%.1f KB", float64(size)/1024)))
}

// sourceList is the names in a comma-separated list, without blanks.
func sourceList(text string) []string {
	names := []string{}
	for name := range strings.SplitSeq(text, ",") {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// setEntry changes an entry's text only if it differs, so the caret stays
// where it is while the user types.
func setEntry(e *gtk.Entry, text string) {
	if e.GetText() != text {
		e.SetText(text)
	}
}

// settingsEntryChanged applies a text setting. Text that is not valid yet
// (a half-typed colour, a path to nothing) is marked and not applied.
func (s *shellUI) settingsEntryChanged(e *gtk.Entry) {
	st := &s.settings
	text := e.GetText()
	c := s.cfg
	valid := true
	if strings.HasPrefix(e.GetName(), "ai-key:") {
		return // saved with its button
	}
	switch e.GetName() {
	case "clock":
		valid = strings.TrimSpace(text) != ""
		c.Bar.Clock = text
		st.clockPreview.SetText(config.Strftime(text, time.Now()))
	case "terminal":
		c.Launcher.Terminal = text
	case "lock":
		c.Control.Lock = text
	case "screenshot":
		c.Control.Screenshot = text
	case "clip_sensitive":
		c.Clipboard.SensitiveSources = sourceList(text)
	case "bg_color":
		valid = config.ValidColor(text)
		c.Background.Color = text
	default:
		if strings.HasPrefix(e.GetName(), "ai-") {
			valid = s.aiEntryChanged(e.GetName(), text, &c)
		}
	case "bg_image":
		c.Background.Image = text
		if p := c.ImagePath(); p != "" {
			info, err := os.Stat(p)
			valid = err == nil && !info.IsDir()
		}
	}
	toggle(&e.Widget, "invalid", !valid)
	if valid {
		s.changeSettings(c)
	}
}

// changeSettings applies c and schedules saving it.
func (s *shellUI) changeSettings(c config.Config) {
	s.applyConfig(c)
	st := &s.settings
	st.dirty = true
	st.saveGen++
	glib.TimeoutAdd(400, &s.onSettingsSave, st.saveGen)
}

func (s *shellUI) saveSettings() {
	st := &s.settings
	st.dirty = false
	if err := config.Save(s.opts.ConfigPath, s.cfg); err != nil {
		log.Printf("settings: %v", err)
		s.setStatus(i18n.Tf("Not saved: %s", err.Error()), true)
		return
	}
	s.setStatus("", false)
	s.applyHyprland()
	s.applyKeyRenames()
}

func (s *shellUI) setStatus(text string, failed bool) {
	st := &s.settings
	if st.win == nil {
		return
	}
	st.status.SetText(text)
	st.status.SetVisible(text != "")
	toggle(&st.status.Widget, "error", failed)
}

// chooseImage asks for a wallpaper with the portal's (or GTK's) file
// chooser; onImageChosen takes the answer.
func (s *shellUI) chooseImage() {
	st := &s.settings
	if st.dialog != nil {
		return
	}
	d := gtk.NewFileDialog()
	d.SetTitle(i18n.T("Choose a wallpaper"))
	filter := gtk.NewFileFilter()
	filter.SetName(i18n.T("Images"))
	filter.AddMimeType("image/*")
	d.SetDefaultFilter(filter)
	filter.Unref()
	if p := s.cfg.ImagePath(); p != "" {
		f := gio.FileNewForPath(p)
		d.SetInitialFile(f)
		unref(f)
	} else if home, err := os.UserHomeDir(); err == nil {
		f := gio.FileNewForPath(filepath.Join(home, "Pictures"))
		d.SetInitialFolder(f)
		unref(f)
	}
	st.dialog = d
	d.Open(&st.win.Window, nil, &s.onImageChosen, 0)
}

// unref drops a reference to an object puregotk gives no Unref method,
// such as a GFile.
func unref(o gobject.Ptr) { (&gobject.Object{Ptr: o.GoPointer()}).Unref() }

// shortenHome writes a path under the home directory as "~/...".
func shortenHome(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home+"/") {
		return "~/" + strings.TrimPrefix(p, home+"/")
	}
	return p
}

// showNotifySettings sets the notification controls, and says whether inua
// shows notifications now.
func (s *shellUI) showNotifySettings() {
	st := &s.settings
	c := s.cfg.Notify
	st.notifyOn.SetActive(c.Enable)
	st.critFocus.SetActive(c.CriticalInFocus)
	st.noteHistory.SetActive(c.History)
	st.banner.SetSelected(uint32(max(0, slices.Index(config.BannerChoices, c.Banner))))
	switch {
	case !c.Enable:
		st.notifyState.SetText(i18n.T("Off: another notification daemon can show them"))
	case s.notesNative():
		st.notifyState.SetText(i18n.T("Banners, then the notification center (the bell in the bar, or Super+N)"))
	default:
		st.notifyState.SetText(i18n.T("Waiting: another notification daemon is running"))
	}
}

// showDockSettings sets the bar and dock controls and lists the pinned
// apps, each with buttons to move or remove it.
func (s *shellUI) showDockSettings() {
	st := &s.settings
	c := s.cfg
	st.barOn.SetActive(c.Bar.Enable)
	st.dockOn.SetActive(c.Dock.Enable)
	st.dockHide.SetActive(c.Dock.Autohide)
	st.dockFlushBar.SetActive(!c.Style.EdgeGaps)
	st.dockDrawer.SetActive(c.Dock.Drawer)
	st.dockSize.SetSelected(uint32(max(0, slices.Index(config.DockSizes, c.Dock.Size))))
	st.dockPosition.SetSelected(uint32(max(0, slices.Index(config.DockPositions, c.Dock.Position))))
	gtkx.Clear(&st.dockPinned)
	if len(c.Dock.Pinned) == 0 {
		l := gtk.NewLabel(i18n.T("Nothing pinned"))
		l.AddCssClass("sub")
		l.SetXalign(0)
		l.AddCssClass("row")
		gtkx.Append(&st.dockPinned, &l.Widget)
	}
	for i, id := range c.Dock.Pinned {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
		row.AddCssClass("row")
		img := gtk.NewImage()
		name := id
		if e := s.dockState.apps.ByID(id); e != nil {
			setIcon(img, e.Icon)
			name = e.Name
		} else {
			img.SetFromIconName("application-x-executable-symbolic")
			name = i18n.Tf("%s (not installed)", id)
		}
		img.SetPixelSize(24)
		gtkx.Append(row, &img.Widget)
		l := gtk.NewLabel(name)
		l.AddCssClass("title")
		l.SetXalign(0)
		l.SetHexpand(true)
		gtkx.Append(row, &l.Widget)
		for _, b := range []struct {
			action, icon, tip string
			ok                bool
		}{
			{"up", "pan-up-symbolic", i18n.T("Move earlier"), i > 0},
			{"down", "pan-down-symbolic", i18n.T("Move later"), i < len(c.Dock.Pinned)-1},
			{"remove", "window-close-symbolic", i18n.T("Remove from the dock"), true},
		} {
			btn := gtk.NewButtonFromIconName(b.icon)
			btn.AddCssClass("plain")
			btn.SetName("pin-" + b.action + ":" + id)
			btn.SetTooltipText(b.tip)
			btn.SetSensitive(b.ok)
			btn.SetValign(gtk.AlignCenterValue)
			btn.ConnectClicked(&s.onSettingsButton)
			gtkx.Append(row, &btn.Widget)
		}
		gtkx.Append(&st.dockPinned, &row.Widget)
	}
}

// movePinned moves a pinned app left ("up"), right ("down"), or removes it.
func movePinned(pinned []string, id, action string) []string {
	out := slices.Clone(pinned)
	i := slices.Index(out, id)
	switch {
	case i < 0:
	case action == "remove":
		out = slices.Delete(out, i, i+1)
	case action == "up" && i > 0:
		out[i-1], out[i] = out[i], out[i-1]
	case action == "down" && i < len(out)-1:
		out[i], out[i+1] = out[i+1], out[i]
	}
	return out
}

// revealFile shows path selected in the file manager, else opens its
// folder; a file not made yet shows its folder. It waits on the bus: run
// it off the main thread.
func revealFile(path string) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("reveal: %v", err)
		return
	}
	if _, err := os.Stat(path); err != nil {
		showInFileManager("ShowFolders", dir)
		return
	}
	showInFileManager("ShowItems", path)
}

// showInFileManager asks a running file manager to show target
// (org.freedesktop.FileManager1's ShowItems or ShowFolders). With none
// running it starts inua-files rather than let D-Bus start one: D-Bus
// starts it with the activation environment, which may name another
// Wayland display (a nested session's), and the window never shows.
func showInFileManager(method, target string) {
	if conn, err := system.SessionBus(); err == nil {
		var running bool
		conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, "org.freedesktop.FileManager1").Store(&running)
		if running {
			uri := (&url.URL{Scheme: "file", Path: target}).String()
			err = conn.Object("org.freedesktop.FileManager1", "/org/freedesktop/FileManager1").
				Call("org.freedesktop.FileManager1."+method, 0, []string{uri}, "").Err
		}
		conn.Close()
		if running && err == nil {
			return
		}
	}
	// inua-files opens a folder, or shows a file selected in its folder.
	if bin, err := exec.LookPath("inua-files"); err == nil {
		if err := desktop.Spawn([]string{bin, target}, ""); err == nil {
			return
		}
	}
	dir := target
	if method == "ShowItems" {
		dir = filepath.Dir(target)
	}
	if err := exec.Command("xdg-open", dir).Run(); err != nil {
		log.Printf("reveal: %v", err)
	}
}
