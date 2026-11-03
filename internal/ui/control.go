package ui

import (
	"fmt"
	"log"
	"math"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/layershell"
	"github.com/lyricat/inua/internal/portal"
	"github.com/lyricat/inua/internal/shell"
	"github.com/lyricat/inua/internal/system"
)

// revealMS matches transition-duration in center.blp.
const revealMS = 260

// control is the control center. Like the launcher, its window (control.ui)
// is built on every open and destroyed on close.
type control struct {
	open    bool
	closing bool // the card is sliding away; the window goes when it is done
	ticking bool // the brightness poll is running

	win      gtk.ApplicationWindow
	backdrop gtk.Box
	card     gtk.Box
	drop     drop
	refs     gtkx.Scope

	batIcon                          gtk.Image
	batLabel                         gtk.Label
	wifi, bt, saver, dnd, mute       gtk.Button
	wifiIcon, dndIcon, volIcon       gtk.Image
	brightIcon                       gtk.Image
	brightValue, volValue            gtk.Label
	wifiSub, btSub, saverSub, dndSub gtk.Label
	display, sound, media            gtk.Box
	brightness, volume               gtk.Scale
	mediaArt                         gtk.Image
	mediaTitle, mediaArtist          gtk.Label
	mediaPrev, mediaPlay, mediaNext  gtk.Button
	artURL                           string
	vpnRow                           gtk.Box
	vpn, vpnOpen                     gtk.Button
	vpnSub                           gtk.Label
	ticks                            int // of onControlTick, to read Tailscale every few

	// The list an arrow beside a tile opens: "wifi", "bt", or "" for none.
	wifiMore, btMore gtk.Button
	detailReveal     gtk.Revealer
	detailList       gtk.Box
	detail           string
	detailShown      string             // what the list shows, to lay it out only when it changes
	items            map[uintptr]string // the list's rows: an SSID or a device's path

	power       gtk.ToggleButton
	powerReveal gtk.Revealer
	powerLabels map[string]*gtk.Label // by action
	// Hibernate shows as logind last said (system.CanHibernate), asked
	// again off the main loop each time the control center opens.
	hibernate       *gtk.Button
	canHibernate    bool
	askingHibernate bool
	armed           string // the action waiting for its second click
	armGen          uintptr
}

func (s *shellUI) bindControlHandlers() {
	s.onControlButton = func(b gtk.Button) { s.controlAction(b.GetName()) }
	s.onSlider = func(r gtk.Range, _ gtk.ScrollType, v float64) bool {
		v = max(0, min(v, 100))
		switch r.GetName() {
		case "volume":
			s.svc.volume.Set(v / 100)
			s.control.volValue.SetText(shell.Percent(v / 100))
		case "brightness":
			if s.svc.brightness != nil {
				s.svc.brightness.Set(v)
			}
			s.control.brightValue.SetText(shell.Percent(v / 100))
			s.showBrightIcon(v)
		}
		return false // let the scale move
	}
	s.onBackdropPressed = func(g gtk.GestureClick, _ int32, x, y float64) {
		if s.barClick(s.barUnder(g.GetWidget()), x, y) {
			return // an entry on the bar, or the bar beside them
		}
		// Only clicks on the backdrop itself close; the card is its child.
		c := &s.control
		hit := c.backdrop.Pick(x, y, gtk.PickDefaultValue)
		if hit == nil {
			return
		}
		outside := hit.GoPointer() != c.card.GoPointer() && !hit.IsAncestor(&c.card.Widget)
		hit.Unref() // Pick returned a new reference
		if outside {
			s.closeControl()
		}
	}
	s.onPowerToggled = func(t gtk.ToggleButton) {
		s.control.powerReveal.SetRevealChild(t.GetActive())
		if !t.GetActive() {
			s.disarmPower()
		}
	}
	s.onPowerDisarm = func(gen uintptr) bool {
		if gen == s.control.armGen {
			s.disarmPower()
		}
		return false
	}
	s.onControlKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		if keyval == keyEscape {
			s.closeControl()
			return true
		}
		return false
	}
	s.onControlGone = func(uintptr) bool {
		c := &s.control
		c.drop.detach()
		destroyPanel(&c.win.Window)
		c.refs.Release()
		c.open, c.closing = false, false
		return false
	}
	s.onControlTick = func(uintptr) bool {
		c := &s.control
		if !c.open || c.closing {
			c.ticking = false
			return false
		}
		s.applyBrightness()
		if c.ticks++; c.ticks%6 == 0 { // every 3 s
			s.readTailscale()
		}
		return true
	}
}

func (s *shellUI) toggleControl() {
	if s.control.open {
		s.closeControl()
	} else {
		s.openControl()
	}
}

func (s *shellUI) openControl() {
	s.closeClipboard()
	s.closePasswords()
	c := &s.control
	if c.open {
		return
	}
	s.closeCenter()
	s.closeAgenda()
	s.hideLevel() // the sliders show the levels now
	s.buildControl()
	c.open = true
	s.applyControl()
	s.applyBrightness()
	s.dropFromBar(&c.drop, nil, func(b *bar) *gtk.Widget { return &b.status.Widget })
	s.refocusPointer()
	for _, b := range s.bars {
		b.status.AddCssClass("open")
	}
	if !c.ticking {
		c.ticking = true
		glib.TimeoutAdd(500, &s.onControlTick, 0)
	}
	s.readDND()
	s.readTailscale()
}

func (s *shellUI) closeControl() {
	c := &s.control
	if !c.open || c.closing {
		return
	}
	c.closing = true
	c.drop.close(func() { s.onControlGone(0) })
	for _, b := range s.bars {
		b.status.RemoveCssClass("open")
	}
}

func (s *shellUI) buildControl() {
	c := &s.control
	ui := s.load("control.ui", &c.refs)
	ui.Get("control", &c.win)
	ui.Get("backdrop", &c.backdrop)
	ui.Get("card", &c.card)
	ui.Get("cc_bat_icon", &c.batIcon)
	ui.Get("cc_bat_label", &c.batLabel)
	ui.Get("wifi", &c.wifi)
	ui.Get("wifi_icon", &c.wifiIcon)
	ui.Get("wifi_sub", &c.wifiSub)
	ui.Get("bt", &c.bt)
	ui.Get("bt_sub", &c.btSub)
	ui.Get("saver", &c.saver)
	ui.Get("saver_sub", &c.saverSub)
	ui.Get("dnd", &c.dnd)
	ui.Get("dnd_icon", &c.dndIcon)
	ui.Get("dnd_sub", &c.dndSub)
	ui.Get("mute", &c.mute)
	ui.Get("display", &c.display)
	ui.Get("brightness", &c.brightness)
	ui.Get("sound", &c.sound)
	ui.Get("vol_icon", &c.volIcon)
	ui.Get("bright_icon", &c.brightIcon)
	ui.Get("vol_value", &c.volValue)
	ui.Get("bright_value", &c.brightValue)
	ui.Get("volume", &c.volume)
	ui.Get("media", &c.media)
	ui.Get("media_art", &c.mediaArt)
	ui.Get("media_title", &c.mediaTitle)
	ui.Get("media_artist", &c.mediaArtist)
	ui.Get("media_prev", &c.mediaPrev)
	ui.Get("media_play", &c.mediaPlay)
	ui.Get("media_next", &c.mediaNext)
	ui.Get("vpn_row", &c.vpnRow)
	ui.Get("vpn", &c.vpn)
	ui.Get("vpn_open", &c.vpnOpen)
	ui.Get("vpn_sub", &c.vpnSub)
	ui.Get("wifi_more", &c.wifiMore)
	ui.Get("bt_more", &c.btMore)
	ui.Get("detail_reveal", &c.detailReveal)
	ui.Get("detail", &c.detailList)
	c.detail, c.detailShown = "", ""
	c.items = map[uintptr]string{}
	var lock, screenshot, openSettings gtk.Button
	ui.Get("power", &c.power)
	ui.Get("power_reveal", &c.powerReveal)
	powerButtons := map[string]*gtk.Button{}
	c.powerLabels = map[string]*gtk.Label{}
	for _, a := range []string{"suspend", "hibernate", "reboot", "poweroff", "logout"} {
		b, l := &gtk.Button{}, &gtk.Label{}
		ui.Get(a, b)
		ui.Get(a+"_label", l)
		powerButtons[a], c.powerLabels[a] = b, l
	}
	c.hibernate = powerButtons["hibernate"]
	c.hibernate.SetVisible(c.canHibernate)
	s.askHibernate()
	c.armed = ""
	ui.Get("lock", &lock)
	ui.Get("screenshot", &screenshot)
	ui.Get("open_settings", &openSettings)
	ui.Close()
	c.artURL = ""

	for _, b := range []*gtk.Button{&lock, &screenshot, &openSettings, &c.wifi, &c.bt, &c.saver, &c.dnd, &c.mute, &c.mediaPrev, &c.mediaPlay, &c.mediaNext, &c.vpn, &c.vpnOpen, &c.wifiMore, &c.btMore} {
		b.ConnectClicked(&s.onControlButton)
	}
	for _, b := range powerButtons {
		b.ConnectClicked(&s.onControlButton)
	}
	c.power.ConnectToggled(&s.onPowerToggled)
	c.brightness.ConnectChangeValue(&s.onSlider)
	c.volume.ConnectChangeValue(&s.onSlider)

	click := gtk.NewGestureClick()
	click.ConnectPressed(&s.onBackdropPressed)
	c.backdrop.AddController(&click.EventController)

	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCaptureValue)
	keys.ConnectKeyPressed(&s.onControlKey)
	c.win.AddController(&keys.EventController)
	c.drop.attach(&c.win.Window, &keys.EventController, &c.backdrop, &c.card.Widget, layershell.LayerOverlay, true)

	c.drop.app = s.app // it joins the application only if it has to show
	w := c.win.GoPointer()
	if s.layer {
		layershell.InitForWindow(w)
		layershell.SetNamespace(w, "inua-control")
		layershell.SetLayer(w, layershell.LayerOverlay)
		layershell.SetKeyboardMode(w, layershell.KeyboardExclusive)
		for _, e := range []layershell.Edge{layershell.EdgeTop, layershell.EdgeBottom, layershell.EdgeLeft, layershell.EdgeRight} {
			layershell.SetAnchor(w, e, true)
		}
		layershell.SetExclusiveZone(w, -1) // cover the bar too
		if mon := s.focusedMonitor(); mon != nil {
			layershell.SetMonitor(w, mon.GoPointer())
		}
	} else {
		c.win.SetTitle(i18n.T("inua control center"))
		c.win.SetDefaultSize(420, 640)
	}
}

func setOn(b *gtk.Button, on bool) {
	if on {
		b.AddCssClass("on")
	} else {
		b.RemoveCssClass("on")
	}
}

// applyControl shows the latest service state.
func (s *shellUI) applyControl() {
	c := &s.control
	st := &s.sys

	c.batIcon.SetVisible(st.Bat.Present)
	if st.Bat.Present {
		c.batIcon.SetFromIconName(shell.BatteryIcon(st.Bat))
		c.batLabel.SetText(shell.BatteryText(st.Bat))
	} else {
		c.batLabel.SetText(i18n.Time(time.Now(), "Monday, January 2"))
	}

	n := st.Net
	c.wifi.SetSensitive(n.Available && n.WifiDevice)
	setOn(&c.wifi, n.WifiEnabled && n.WifiDevice)
	sub, icon := shell.WifiTile(n)
	c.wifiSub.SetText(sub)
	c.wifiIcon.SetFromIconName(icon)

	c.bt.SetSensitive(st.BT.Available)
	setOn(&c.bt, st.BT.Powered)
	c.btSub.SetText(shell.BluetoothTile(st.BT))

	c.saver.SetSensitive(st.Prof.Available)
	setOn(&c.saver, st.Prof.Active == "power-saver")
	c.saverSub.SetText(shell.SaverTile(st.Prof))

	s.applyFocusTile()

	c.vpnRow.SetVisible(st.TS.Installed)
	tsSub, tsOn, _ := shell.TailscaleTile(st.TS)
	setOn(&c.vpn, tsOn)
	c.vpnSub.SetText(tsSub)
	s.showControlDetail()

	a := st.Audio
	c.sound.SetSensitive(a.Available)
	c.mute.SetSensitive(a.Available)
	setOn(&c.mute, a.Muted)
	c.volIcon.SetFromIconName(shell.VolumeIcon(a))
	if _, held := s.svc.volume.Held(); !held { // a drag or a scroll the service has not caught up with
		c.volume.SetValue(a.Volume * 100)
		c.volValue.SetText(shell.VolumeText(a))
	}

	c.display.SetVisible(s.svc.backlight != nil)

	m := st.Media
	c.media.SetVisible(m.Available)
	if m.Available {
		title, artist := shell.MediaText(m)
		c.mediaTitle.SetText(title)
		c.mediaArtist.SetText(artist)
		c.mediaArtist.SetVisible(artist != "")
		c.mediaPlay.SetIconName(shell.PlayIcon(m.Playing))
		c.mediaPlay.SetSensitive(m.CanPlayPause)
		c.mediaPrev.SetSensitive(m.CanPrev)
		c.mediaNext.SetSensitive(m.CanNext)
		if m.ArtURL != c.artURL {
			c.artURL = m.ArtURL
			if path := localPath(m.ArtURL); path != "" {
				c.mediaArt.SetFromFile(path)
				c.mediaArt.SetPixelSize(48)
			} else {
				c.mediaArt.SetFromIconName("audio-x-generic-symbolic")
				c.mediaArt.SetPixelSize(22)
			}
		}
	}
}

// applyBrightness reads the panel brightness; brightness keys change it
// without any event to listen to, so the control center polls while open.
func (s *shellUI) applyBrightness() {
	c := &s.control
	if s.svc.backlight == nil {
		return
	}
	if _, held := s.svc.brightness.Held(); held {
		return
	}
	if p, err := s.svc.backlight.Percent(); err == nil {
		c.brightness.SetValue(p)
		c.brightValue.SetText(shell.Percent(p / 100))
		s.showBrightIcon(p)
	}
}

// showBrightIcon brightens the display icon with the screen, in eleven
// steps (style.css: .bright-icon.lv0 … lv10).
func (s *shellUI) showBrightIcon(percent float64) {
	brightLevel(&s.control.brightIcon.Widget, percent)
}

// brightLevel brightens a .bright-icon with the screen, 0–100, in eleven
// steps (lv0–lv10); a negative percent takes the steps off.
func brightLevel(w *gtk.Widget, percent float64) {
	level := -1
	if percent >= 0 {
		level = int(math.Round(min(percent, 100) / 10))
	}
	for i := range 11 {
		toggle(w, fmt.Sprintf("lv%d", i), i == level)
	}
}

func localPath(u string) string {
	if !strings.HasPrefix(u, "file://") {
		return ""
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return parsed.Path
}

func (s *shellUI) readDND() {
	if !system.DNDAvailable() {
		return
	}
	go func() {
		on, err := system.DND()
		logErr("dnd", err)
		gtkx.Post(func() {
			s.sys.DND = on
			if s.control.open {
				s.applyControl()
			}
		})
	}()
}

// controlAction runs a control center button (and the bar's status button).
func (s *shellUI) controlAction(name string) {
	st := s.sys
	v := &s.svc
	switch name {
	case "status":
		s.toggleControl()
	case "wifi":
		go func() { logErr("wifi", v.net.SetWifi(!st.Net.WifiEnabled)) }()
	case "bt":
		go func() { logErr("bluetooth", v.bt.SetPowered(!st.BT.Powered)) }()
	case "saver":
		target := shell.SaverTarget(st.Prof)
		go func() { logErr("power profile", v.prof.SetProfile(target)) }()
	case "dnd":
		if s.notesNative() {
			s.setFocus(!s.notes.c.Focus)
			return
		}
		on := !s.sys.DND
		s.sys.DND = on // show it now; readDND corrects a failure
		s.applyControl()
		go func() {
			logErr("dnd", system.SetDND(on))
			gtkx.Post(s.readDND)
		}()
	case "vpn":
		s.toggleTailscale()
	case "vpn-open":
		s.closeControl()
		s.openLightApp(tailscaleApp, "tailscale")
	case "wifi-more":
		s.toggleDetail("wifi")
	case "bt-more":
		s.toggleDetail("bt")
	case "wifi-settings", "bt-settings":
		s.closeControl()
		s.openSettings()
		s.showSettingsPage(map[string]string{"wifi-settings": "wifi", "bt-settings": "bluetooth"}[name])
	case "mute":
		go func() { logErr("mute", v.audio.ToggleMute()) }()
	case "prev":
		go func() { logErr("media", v.media.Previous()) }()
	case "play":
		go func() { logErr("media", v.media.PlayPause()) }()
	case "next":
		go func() { logErr("media", v.media.Next()) }()
	case "settings":
		s.closeControl()
		s.openSettings()
	case "lock":
		s.closeControl()
		go run(s.cfg.Control.Lock)
	case "suspend", "hibernate", "reboot", "poweroff", "logout":
		s.powerAction(name)
	case "screenshot":
		s.closeControl()
		go func() {
			time.Sleep((shell.DropCloseMS + 150) * time.Millisecond) // out of the picture first
			run(s.cfg.Control.Screenshot)
		}()
	default:
		log.Printf("control: unknown button %q", name)
	}
}

func run(script string) {
	if out, err := exec.Command("sh", "-c", script).CombinedOutput(); err != nil {
		log.Printf("%s: %v %s", script, err, strings.TrimSpace(string(out)))
	}
}

// powerLabel is each power action's label, and what it asks before going.
var powerLabel = map[string][2]string{
	"suspend":   {i18n.M("Suspend"), ""},
	"hibernate": {i18n.M("Hibernate"), i18n.M("Hibernate?")},
	"reboot":    {i18n.M("Restart"), i18n.M("Restart?")},
	"poweroff":  {i18n.M("Shut down"), i18n.M("Shut down?")},
	"logout":    {i18n.M("Log out"), i18n.M("Log out?")},
}

// askHibernate asks logind whether the machine can hibernate, off the main
// loop, and shows Hibernate or not as it says.
func (s *shellUI) askHibernate() {
	c := &s.control
	if c.askingHibernate {
		return
	}
	c.askingHibernate = true
	go func() {
		can, err := system.CanHibernate(system.SystemBus)
		if err != nil {
			log.Printf("control: %v", err)
		}
		gtkx.Post(func() {
			c.askingHibernate = false
			c.canHibernate = can
			if c.open && c.hibernate != nil {
				c.hibernate.SetVisible(can)
			}
		})
	}()
}

// powerAction suspends at once (locking the screen first); hibernate, restart,
// shut down and log out wait for a second click within a few seconds.
func (s *shellUI) powerAction(name string) {
	c := &s.control
	if ask := powerLabel[name][1]; ask != "" && c.armed != name {
		s.disarmPower()
		c.armed = name
		text := i18n.T(ask)
		if endsSession(name) {
			// Apps holding the session through the portal say so first.
			if names := s.holdingLogout(); len(names) > 0 {
				text = i18n.Tf("%s is busy. %s", strings.Join(names, ", "), text)
			}
			s.portalSessionState(portal.SessionQueryEnd)
		}
		c.powerLabels[name].SetText(text)
		c.powerLabels[name].GetParent().GetParent().AddCssClass("armed")
		c.armGen++
		glib.TimeoutAdd(4000, &s.onPowerDisarm, c.armGen)
		return
	}
	s.disarmPower()
	s.closeControl()
	s.doPower(name)
}

// endsSession is whether a power action ends the session.
func endsSession(name string) bool {
	return name == "reboot" || name == "poweroff" || name == "logout"
}

// doPower suspends or hibernates (locking first), restarts, shuts down or logs out.
func (s *shellUI) doPower(name string) {
	if endsSession(name) {
		s.portalSessionState(portal.SessionEnding)
	}
	switch name {
	case "suspend", "hibernate":
		if err := s.lockScreen(); err != nil {
			log.Printf("%s: not locking: %v", name, err)
		}
		go func() {
			time.Sleep(700 * time.Millisecond) // the lock screen is up first
			// The user asked: an app's lock holds off idle suspend, not
			// this one (one that forgot its lock would block it for good).
			run("systemctl " + name + " --check-inhibitors=no")
		}()
	case "reboot":
		go run("systemctl reboot")
	case "poweroff":
		go run("systemctl poweroff")
	case "logout":
		// Hyprland 0.55 runs a Lua configuration, where a dispatcher is an
		// expression; the classic text form is rejected there.
		s.dispatchEither("exit", "hl.dsp.exit()")
	}
}

// disarmPower puts an action waiting for its second click back.
func (s *shellUI) disarmPower() {
	c := &s.control
	if c.armed == "" {
		return
	}
	if endsSession(c.armed) {
		s.portalSessionState(portal.SessionRunning)
	}
	if l := c.powerLabels[c.armed]; l != nil {
		l.SetText(i18n.T(powerLabel[c.armed][0]))
		l.GetParent().GetParent().RemoveCssClass("armed")
	}
	c.armed = ""
}
