package lightui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/sysmon"
	"github.com/lyricat/inua/internal/apps/tailscale"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// adminConsole is where a tailnet's devices are approved and managed.
const adminConsole = "https://login.tailscale.com/admin/machines"

// tsSwitch is one of This Device's settings: a title and a line over a
// switch, and the edit turning it on or off makes.
type tsSwitch struct {
	key, title, sub string
	on              func(tailscale.Prefs) bool
	edit            func(tailscale.Prefs, bool) tailscale.Edit
}

var tsSwitches = []tsSwitch{
	{"dns", i18n.M("Use Tailscale DNS"), i18n.M("MagicDNS names and the tailnet's DNS settings."),
		func(p tailscale.Prefs) bool { return p.CorpDNS },
		func(_ tailscale.Prefs, on bool) tailscale.Edit { return tailscale.Edit{CorpDNS: on, CorpDNSSet: true} }},
	{"routes", i18n.M("Use subnet routes"), i18n.M("Reach the networks other devices offer."),
		func(p tailscale.Prefs) bool { return p.RouteAll },
		func(_ tailscale.Prefs, on bool) tailscale.Edit {
			return tailscale.Edit{RouteAll: on, RouteAllSet: true}
		}},
	{"shields", i18n.M("Block incoming connections"), i18n.M("Other devices cannot reach this one; it can still reach them."),
		func(p tailscale.Prefs) bool { return p.ShieldsUp },
		func(_ tailscale.Prefs, on bool) tailscale.Edit {
			return tailscale.Edit{ShieldsUp: on, ShieldsUpSet: true}
		}},
	{"exit", i18n.M("Offer as exit node"), i18n.M("Others may send their traffic through this device, once the admin approves."),
		func(p tailscale.Prefs) bool { return p.OffersExit() },
		func(p tailscale.Prefs, on bool) tailscale.Edit { return p.OfferExit(on) }},
	{"ssh", i18n.M("Tailscale SSH"), i18n.M("Let the tailnet's SSH rules reach this device's SSH server."),
		func(p tailscale.Prefs) bool { return p.RunSSH },
		func(_ tailscale.Prefs, on bool) tailscale.Edit { return tailscale.Edit{RunSSH: on, RunSSHSet: true} }},
}

// tsDetail is the open device's page: its facts by key, updated in place.
type tsDetail struct {
	id      string
	facts   map[string]*gtk.Label
	exitBtn *gtk.Button
	dropBox *gtk.Box
}

type tailscaleApp struct {
	c                      *appui.Ctx
	client                 *tailscale.Client
	user                   string
	pages                  gtk.Stack
	detailReveal           gtk.Revealer
	stateTitle, stateSub   gtk.Label
	connect                gtk.Switch
	notice                 gtk.Box
	noticeText, note       gtk.Label
	noticeBtn              gtk.Button
	filter                 gtk.SearchEntry
	devices, detail, exits gtk.Box
	thisDevice             gtk.Box
	detailName, detailSub  gtk.Label
	st                     tailscale.Status
	prefs                  tailscale.Prefs
	stErr, prefsErr        error
	waiting                []tailscale.WaitingFile
	waitingErr             error
	open                   *tsDetail
	switches               map[string]*gtk.Switch
	fetching, filling      bool
	loggingIn              bool // opening the login page once it is known
	armed                  string
	armedAt, noteAt        time.Time
	noticeAction           func()
	onTick                 glib.SourceFunc
	onConnect, onSwitch    func(gtk.Switch, bool) bool
	onButton               func(gtk.Button)
	devicesShown           string // what the list shows, to rebuild it only on a change
	onFilter, onStopFilter func(gtk.SearchEntry)
	onPage                 func(uintptr, uintptr, uintptr)
	onDrop                 func(gtk.DropTarget, uintptr, float64, float64) bool
	onKey                  func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
	panel                  sidePanel
}

func (a *tailscaleApp) Build(c *appui.Ctx) *gtk.ApplicationWindow {
	a.c = c
	a.client = tailscale.NewClient("")
	if u, err := user.Current(); err == nil {
		a.user = u.Username
	}
	a.switches = map[string]*gtk.Switch{}
	win := &gtk.ApplicationWindow{}
	var back gtk.Button
	var detailPage gtk.Box
	ui := c.Load("tailscale.ui")
	ui.Get("tailscale", win)
	ui.Get("pages", &a.pages)
	ui.Get("detail_reveal", &a.detailReveal)
	ui.Get("state_title", &a.stateTitle)
	ui.Get("state_sub", &a.stateSub)
	ui.Get("connect", &a.connect)
	ui.Get("notice", &a.notice)
	ui.Get("notice_text", &a.noticeText)
	ui.Get("notice_button", &a.noticeBtn)
	ui.Get("note", &a.note)
	ui.Get("filter", &a.filter)
	ui.Get("devices", &a.devices)
	ui.Get("detail", &a.detail)
	ui.Get("detail_page", &detailPage)
	ui.Get("detail_name", &a.detailName)
	ui.Get("detail_sub", &a.detailSub)
	ui.Get("exits", &a.exits)
	ui.Get("this_device", &a.thisDevice)
	ui.Get("back", &back)
	ui.Close()

	a.onTick = func(uintptr) bool {
		a.fetch()
		if a.armed != "" && time.Since(a.armedAt) > 4*time.Second {
			a.armed = ""
			a.show()
		}
		if !a.noteAt.IsZero() && time.Since(a.noteAt) > 6*time.Second {
			a.say("", false)
		}
		return true
	}
	a.onConnect = func(_ gtk.Switch, on bool) bool {
		if !a.filling {
			a.edit(tailscale.Edit{WantRunning: on, WantRunningSet: true}, map[bool]string{true: i18n.T("Connecting…"), false: i18n.T("Disconnected.")}[on])
		}
		return false
	}
	a.connect.ConnectStateSet(&a.onConnect)
	a.onSwitch = func(sw gtk.Switch, on bool) bool {
		if a.filling {
			return false
		}
		key := strings.TrimPrefix(sw.GetName(), "pref:")
		if key == "lan" {
			a.edit(tailscale.Edit{ExitNodeAllowLANAccess: on, ExitNodeAllowLANAccessSet: true},
				map[bool]string{true: i18n.T("Local network reachable through the exit node."), false: i18n.T("Local network goes through the exit node too.")}[on])
			return false
		}
		for _, s := range tsSwitches {
			if s.key == key {
				a.edit(s.edit(a.prefs, on), i18n.Tf(map[bool]string{true: i18n.M("%s on."), false: i18n.M("%s off.")}[on], i18n.T(s.title)))
			}
		}
		return false
	}
	a.onButton = func(b gtk.Button) { a.button(b.GetName()) }
	a.noticeBtn.SetName("notice")
	a.noticeBtn.ConnectClicked(&a.onButton)
	back.SetName("back")
	back.ConnectClicked(&a.onButton)
	a.onFilter = func(gtk.SearchEntry) { a.showDevices() }
	a.filter.ConnectSearchChanged(&a.onFilter)
	a.onStopFilter = func(e gtk.SearchEntry) { e.SetText("") }
	a.filter.ConnectStopSearch(&a.onStopFilter)
	a.onPage = func(uintptr, uintptr, uintptr) {
		if a.pages.GetVisibleChildName() == "device" {
			a.fetchWaiting()
		}
		a.show()
	}
	gtkx.ConnectNotify(&a.pages, "visible-child", &a.onPage)

	// Files dropped on a device's page go to it with Taildrop. Move as
	// well as copy: Nautilus offers its files to be moved only, and they
	// are only read.
	drop := gtk.NewDropTarget(gdk.FileListGLibType(), gdk.ActionCopyValue|gdk.ActionMoveValue)
	a.onDrop = func(dt gtk.DropTarget, _ uintptr, _, _ float64) bool {
		if v := dt.GetValue(); v != nil {
			a.sendFiles(droppedPaths(v))
		}
		return true
	}
	drop.ConnectDrop(&a.onDrop)
	detailPage.AddController(&drop.EventController)
	a.panel.init(win, &a.detailReveal)

	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCaptureValue)
	a.onKey = func(_ gtk.EventControllerKey, keyval, _ uint32, state gdk.ModifierType) bool {
		switch {
		case keyval == uint32(gdk.KEY_Escape) && a.open != nil:
			a.closeDevice()
		case state&gdk.ControlMaskValue != 0 && (keyval == 'f' || keyval == 'F'):
			a.pages.SetVisibleChildName("devices")
			a.filter.GrabFocus()
		default:
			return false
		}
		return true
	}
	keys.ConnectKeyPressed(&a.onKey)
	win.AddController(&keys.EventController)

	a.fetch()
	menuWindow(c, win, []appui.MenuGroup{{Title: "View", Sections: []appui.MenuSection{{Actions: []appui.MenuAction{{Name: "tailscale-refresh", Label: "Refresh", Activate: func(gio.SimpleAction, uintptr) { a.fetch() }}, {Name: "tailscale-devices", Label: "Devices", Activate: func(gio.SimpleAction, uintptr) { a.pages.SetVisibleChildName("devices") }}, {Name: "tailscale-settings", Label: "This device", Activate: func(gio.SimpleAction, uintptr) { a.pages.SetVisibleChildName("this-device") }}}}}}})
	glib.TimeoutAdd(3000, &a.onTick, 0)
	return win
}

func (a *tailscaleApp) ThemeChanged() {}

func (a *tailscaleApp) ShowPage(name string) {
	if a.pages.GetChildByName(name) != nil {
		a.pages.SetVisibleChildName(name)
	}
}

// --- reading the daemon ---

// fetch reads the status and settings off the main loop, then shows them.
func (a *tailscaleApp) fetch() {
	if a.fetching {
		return
	}
	a.fetching = true
	go func() {
		ctx, cancel := tailscale.Timeout()
		defer cancel()
		st, stErr := a.client.Status(ctx)
		var p tailscale.Prefs
		prefsErr := stErr
		if stErr == nil {
			p, prefsErr = a.client.Prefs(ctx)
		}
		gtkx.Post(func() {
			a.fetching = false
			a.st, a.stErr, a.prefs, a.prefsErr = st, stErr, p, prefsErr
			if a.loggingIn && st.AuthURL != "" {
				a.loggingIn = false
				openURL(st.AuthURL)
			}
			a.show()
		})
	}()
}

func (a *tailscaleApp) fetchWaiting() {
	go func() {
		ctx, cancel := tailscale.Timeout()
		defer cancel()
		fs, err := a.client.WaitingFiles(ctx)
		gtkx.Post(func() {
			a.waiting, a.waitingErr = fs, err
			a.showThisDevice()
		})
	}()
}

// operator reports whether this user may change Tailscale.
func (a *tailscaleApp) operator() bool {
	return os.Getuid() == 0 || (a.user != "" && a.prefs.OperatorUser == a.user)
}

// edit changes settings off the main loop and says done when it worked.
func (a *tailscaleApp) edit(e tailscale.Edit, done string) {
	go func() {
		ctx, cancel := tailscale.Timeout()
		defer cancel()
		p, err := a.client.EditPrefs(ctx, e)
		gtkx.Post(func() {
			if err != nil {
				a.fail(err)
			} else {
				a.prefs = p
				a.say(done, false)
			}
			a.show() // the switches as they really are
			a.fetch()
		})
	}()
}

func (a *tailscaleApp) fail(err error) {
	switch {
	case errors.Is(err, tailscale.ErrPermission):
		a.say(i18n.T("Not changed: allow this app to manage Tailscale first."), true)
	case errors.Is(err, tailscale.ErrNotRunning):
		a.say(i18n.T("Tailscale is not running."), true)
	default:
		a.say(err.Error(), true)
	}
}

// say shows a line about the last action under the header; "" hides it.
func (a *tailscaleApp) say(text string, bad bool) {
	a.noteAt = time.Time{}
	if text != "" {
		a.noteAt = time.Now()
	}
	a.note.SetText(text)
	a.note.SetVisible(text != "")
	if bad {
		a.note.AddCssClass("error")
	} else {
		a.note.RemoveCssClass("error")
	}
}

// --- showing ---

func (a *tailscaleApp) show() {
	a.showHeader()
	switch a.pages.GetVisibleChildName() {
	case "devices":
		if a.open != nil {
			a.showDevice()
		} else {
			a.showDevices()
		}
	case "exit":
		a.showExits()
	case "device":
		a.showThisDevice()
	}
}

func (a *tailscaleApp) showHeader() {
	state, on := a.st.State()
	if a.stErr != nil && !errors.Is(a.stErr, tailscale.ErrNotRunning) {
		state = i18n.T("Unavailable")
	}
	a.stateTitle.SetText(state)
	setClass(&a.stateTitle.Widget, "on", on)
	var sub []string
	if self := a.st.Self; self != nil && a.stErr == nil {
		sub = append(sub, self.Name())
		if ip := self.IPv4(); ip != "" && on {
			sub = append(sub, ip)
		}
		if on {
			sub = append(sub, i18n.Tf("%d of %d devices online", a.st.Online(), len(a.st.Peer)))
		}
	}
	a.stateSub.SetText(strings.Join(sub, " · "))
	a.filling = true
	a.connect.SetActive(a.prefs.WantRunning && a.stErr == nil)
	a.connect.SetSensitive(a.stErr == nil && a.st.BackendState != "NeedsLogin" && a.operator())
	a.filling = false

	// What is in the way, most important first.
	text, button := "", ""
	switch {
	case errors.Is(a.stErr, tailscale.ErrNotRunning):
		text, button = i18n.T("Tailscale's service is not running."), i18n.T("Start…")
		a.noticeAction = func() { a.runAsAdmin(i18n.T("Started Tailscale."), "systemctl", "start", "tailscaled") }
	case a.stErr != nil:
		text = a.stErr.Error()
	case a.st.BackendState == "NeedsLogin":
		text, button = i18n.T("This device is logged out of its tailnet."), i18n.T("Log In")
		a.noticeAction = a.logIn
	case a.st.BackendState == "NeedsMachineAuth":
		text, button = i18n.T("An admin of the tailnet has to approve this device."), i18n.T("Admin Console")
		a.noticeAction = func() { openURL(adminConsole) }
	case !a.operator():
		text, button = i18n.T("To change Tailscale from here, allow it once; it asks for an administrator's password."), i18n.T("Allow…")
		a.noticeAction = func() {
			a.runAsAdmin(i18n.T("This app may now manage Tailscale."), "tailscale", "set", "--operator="+a.user)
		}
	}
	for _, h := range a.st.Health {
		if text == "" {
			text = h
		}
	}
	a.notice.SetVisible(text != "")
	a.noticeText.SetText(text)
	a.noticeBtn.SetVisible(button != "")
	a.noticeBtn.SetLabel(button)
}

// showDevices lists the devices the filter keeps.
func (a *tailscaleApp) showDevices() {
	peers := a.st.Peers(a.filter.GetText())
	now := time.Now()
	// The status comes every 3 s; rebuilding the same list would close a
	// row's open menu and reset the pointer's hover.
	var key strings.Builder
	fmt.Fprintf(&key, "%d|", len(a.st.Peer))
	for _, p := range peers {
		fmt.Fprintf(&key, "%s %s %v %s %s %v %v %s %s|", p.ID, p.Name(), p.Online, p.OS, p.Seen(now),
			p.ExitNode, p.ExitNodeOption, strings.Join(p.Tags, ","), p.IPv4()+p.MagicDNS())
	}
	if key.String() == a.devicesShown {
		return
	}
	a.devicesShown = key.String()
	gtkx.Clear(&a.devices)
	if len(peers) == 0 {
		msg := i18n.T("No device matches.")
		if len(a.st.Peer) == 0 {
			msg = i18n.T("No other devices in the tailnet yet.")
		}
		l := gtk.NewLabel(msg)
		l.AddCssClass("empty")
		l.SetXalign(0)
		gtkx.Append(&a.devices, &l.Widget)
		return
	}
	for _, p := range peers {
		inner := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		dot := gtk.NewBox(gtk.OrientationHorizontalValue, 0)
		dot.AddCssClass("mark") // filled in ok while it is online
		setClass(&dot.Widget, "ok", p.Online)
		dot.SetValign(gtk.AlignCenterValue)
		gtkx.Append(inner, &dot.Widget)
		parts := []string{p.OS, p.Seen(now)}
		if p.ExitNode {
			parts = append(parts, i18n.T("exit node in use"))
		} else if p.ExitNodeOption {
			parts = append(parts, i18n.T("exit node"))
		}
		parts = append(parts, p.Tags...)
		t := text(p.Name(), strings.Join(parts, " · "))
		t.SetHexpand(true)
		gtkx.Append(inner, &t.Widget)
		ip := gtk.NewLabel(p.IPv4())
		ip.AddCssClass("readout")
		gtkx.Append(inner, &ip.Widget)
		b := rowButton("open:"+p.ID, &inner.Widget, &a.onButton)
		b.SetTooltipText(i18n.T("Details, ping, exit node and sending files"))
		b.SetHexpand(true)
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 2)
		row.AddCssClass("ts-row")
		if a.open != nil && a.open.id == p.ID {
			row.AddCssClass("open") // its panel is showing
		}
		gtkx.Append(row, &b.Widget)
		gtkx.Append(&a.devices, &row.Widget)
	}
}

// copyPeer copies a device's IPv4 address, or its MagicDNS name.
func (a *tailscaleApp) copyPeer(p *tailscale.Peer, name bool) {
	if p == nil {
		return
	}
	s := p.IPv4()
	if name {
		s = cmpOr(p.MagicDNS(), p.Name())
	}
	go exec.Command("wl-copy", "--", s).Run()
	n := gio.NewNotification(i18n.T("Copied"))
	n.SetBody(s)
	a.c.App.SendNotification("copied", n) // replaces the last one
	n.Unref()
}

func (a *tailscaleApp) openDevice(id string) {
	if a.st.PeerByID(id) == nil {
		return
	}
	if a.open != nil && a.open.id == id { // its row again: the panel goes
		a.closeDevice()
		return
	}
	a.open = &tsDetail{id: id, facts: map[string]*gtk.Label{}}
	a.buildDevice()
	a.showDevice()
	a.panel.open()
	a.showDevices() // marks its row
}

func (a *tailscaleApp) closeDevice() {
	if a.open == nil {
		return
	}
	a.open = nil
	a.panel.close()
	a.showDevices()
}

// buildDevice lays out the open device's page; showDevice fills it.
func (a *tailscaleApp) buildDevice() {
	v := a.open
	gtkx.Clear(&a.detail)
	// Two rows, to fit the panel: copying and ping, then the exit node.
	acts := gtk.NewBox(gtk.OrientationVerticalValue, 8)
	acts.AddCssClass("actions")
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	for _, b := range [][2]string{{"copy-ip", i18n.T("Copy IP")}, {"copy-name", i18n.T("Copy Name")}, {"ping", i18n.T("Ping")}} {
		btn := gtk.NewButtonWithLabel(b[1])
		btn.AddCssClass("outlined")
		btn.SetName(b[0])
		btn.ConnectClicked(&a.onButton)
		gtkx.Append(row, &btn.Widget)
	}
	gtkx.Append(acts, &row.Widget)
	exit := gtk.NewButtonWithLabel(i18n.T("Use as Exit Node"))
	exit.AddCssClass("outlined")
	exit.SetName("use-exit")
	exit.SetHalign(gtk.AlignStartValue)
	exit.ConnectClicked(&a.onButton)
	v.exitBtn = exit
	gtkx.Append(acts, &exit.Widget)
	gtkx.Append(&a.detail, &acts.Widget)

	section(&a.detail, i18n.T("DEVICE"))
	for _, k := range []string{i18n.T("MagicDNS"), i18n.T("IPv4"), i18n.T("IPv6"), i18n.T("System"), i18n.T("Status"), i18n.T("Connection"), i18n.T("Traffic"), i18n.T("Key expires"), i18n.T("Tags")} {
		v.facts[k] = fact(&a.detail, k, "", true)
	}
	section(&a.detail, i18n.T("SEND FILES"))
	drop := gtk.NewBox(gtk.OrientationVerticalValue, 4)
	drop.AddCssClass("ts-drop")
	l := gtk.NewLabel("")
	l.AddCssClass("sub")
	l.SetWrap(true)
	gtkx.Append(drop, &l.Widget)
	v.facts["drop"] = l
	v.dropBox = drop
	gtkx.Append(&a.detail, &drop.Widget)
}

func (a *tailscaleApp) showDevice() {
	v := a.open
	p := a.st.PeerByID(v.id)
	if p == nil {
		a.detailSub.SetText(i18n.T("NO LONGER IN THE TAILNET"))
		return
	}
	now := time.Now()
	a.detailName.SetText(p.Name())
	a.detailSub.SetText(strings.ToUpper(p.OS + " · " + p.Seen(now)))
	set := func(k, s string) {
		if l := v.facts[k]; l != nil && l.GetText() != s {
			l.SetText(s)
		}
	}
	var v4, v6 []string
	for _, ip := range p.TailscaleIPs {
		if strings.Contains(ip, ":") {
			v6 = append(v6, ip)
		} else {
			v4 = append(v4, ip)
		}
	}
	set(i18n.T("MagicDNS"), cmpOr(p.MagicDNS(), "—"))
	set(i18n.T("IPv4"), cmpOr(strings.Join(v4, "\n"), "—"))
	set(i18n.T("IPv6"), cmpOr(strings.Join(v6, "\n"), "—"))
	set(i18n.T("System"), i18n.Tf("%s · host name %s", p.OS, p.HostName))
	set(i18n.T("Status"), p.Seen(now))
	set(i18n.T("Connection"), p.Connection())
	set(i18n.T("Traffic"), fmt.Sprintf("received %s · sent %s", sysmon.Bytes(uint64(max(0, p.RxBytes))), sysmon.Bytes(uint64(max(0, p.TxBytes)))))
	set(i18n.T("Key expires"), cmpOr(p.Expiry(now), "never"))
	set(i18n.T("Tags"), cmpOr(strings.Join(p.Tags, ", "), "none"))
	set("drop", i18n.Tf("Drop files on this panel to send them to %s with Taildrop. They wait on it until saved.", p.Name()))
	v.exitBtn.SetVisible(p.ExitNodeOption)
	if p.ExitNode {
		v.exitBtn.SetLabel(i18n.T("Stop Using as Exit Node"))
	} else {
		v.exitBtn.SetLabel(i18n.T("Use as Exit Node"))
	}
}

// showExits lists None and the peers offering to be an exit node.
func (a *tailscaleApp) showExits() {
	gtkx.Clear(&a.exits)
	section(&a.exits, i18n.T("SEND THIS DEVICE'S TRAFFIC THROUGH"))
	rows := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	rows.AddCssClass("rows")
	add := func(id, title, sub string, current bool) {
		inner := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
		t := text(title, sub)
		t.SetHexpand(true)
		gtkx.Append(inner, &t.Widget)
		mark := gtk.NewLabel("")
		if current {
			mark.SetText(i18n.T("IN USE"))
			mark.AddCssClass("kicker")
			mark.AddCssClass("ts-current")
		}
		gtkx.Append(inner, &mark.Widget)
		gtkx.Append(rows, &rowButton("exit:"+id, &inner.Widget, &a.onButton).Widget)
	}
	add("", i18n.T("None"), i18n.T("Traffic goes out from this device as usual."), a.prefs.ExitNodeID == "")
	now := time.Now()
	nodes := a.st.ExitNodes()
	for _, p := range nodes {
		add(p.ID, p.Name(), p.OS+" · "+p.Seen(now), a.prefs.ExitNodeID == p.ID)
	}
	gtkx.Append(&a.exits, &rows.Widget)
	if len(nodes) == 0 {
		l := gtk.NewLabel(i18n.T("No device in the tailnet offers to be an exit node. Turn on Offer as Exit Node on one, then approve it in the admin console."))
		l.AddCssClass("empty")
		l.SetXalign(0)
		l.SetWrap(true)
		gtkx.Append(&a.exits, &l.Widget)
	}
	if a.prefs.ExitNodeID != "" {
		row := a.switchRow("lan", i18n.T("Allow local network access"),
			i18n.T("Reach printers and other devices on this network while the exit node carries the rest."),
			a.prefs.ExitNodeAllowLANAccess, a.operator())
		gtkx.Append(&a.exits, &row.Widget)
	}
}

// showThisDevice lays out This Device once, then keeps its switches and
// facts current.
func (a *tailscaleApp) showThisDevice() {
	gtkx.Clear(&a.thisDevice)
	self := a.st.Self
	section(&a.thisDevice, i18n.T("THIS DEVICE"))
	if self != nil {
		fact(&a.thisDevice, i18n.T("Name"), self.Name(), true)
		fact(&a.thisDevice, i18n.T("MagicDNS"), cmpOr(self.MagicDNS(), "—"), true)
		fact(&a.thisDevice, i18n.T("Addresses"), cmpOr(strings.Join(self.TailscaleIPs, "\n"), "—"), true)
	}
	if t := a.st.CurrentTailnet; t != nil {
		fact(&a.thisDevice, i18n.T("Tailnet"), t.Name, true)
	}
	section(&a.thisDevice, i18n.T("SETTINGS"))
	rows := gtk.NewBox(gtk.OrientationVerticalValue, 0)
	rows.AddCssClass("rows")
	for _, s := range tsSwitches {
		row := a.switchRow("pref:"+s.key, i18n.T(s.title), i18n.T(s.sub), s.on(a.prefs), a.prefsErr == nil && a.operator())
		gtkx.Append(rows, &row.Widget)
	}
	gtkx.Append(&a.thisDevice, &rows.Widget)

	section(&a.thisDevice, i18n.T("RECEIVED FILES"))
	switch {
	case errors.Is(a.waitingErr, tailscale.ErrPermission):
		a.line(&a.thisDevice, i18n.T("Files sent to this device show here once this app may manage Tailscale."))
	case a.waitingErr != nil:
		a.line(&a.thisDevice, a.waitingErr.Error())
	case len(a.waiting) == 0:
		a.line(&a.thisDevice, i18n.T("None waiting. Files other devices send with Taildrop show here."))
	default:
		for _, f := range a.waiting {
			fact(&a.thisDevice, sysmon.Bytes(uint64(max(0, f.Size))), f.Name, true)
		}
		a.buttons(&a.thisDevice, [][2]string{{"save-files", i18n.T("Save to Downloads")}})
	}

	section(&a.thisDevice, i18n.T("ACCOUNT"))
	logout := i18n.T("Log Out")
	if a.armed == "logout" {
		logout = i18n.T("Sure?")
	}
	a.buttons(&a.thisDevice, [][2]string{{"logout", logout}, {"admin", i18n.T("Admin Console")}})
	foot := gtk.NewLabel("")
	foot.AddCssClass("foot")
	gtkx.Append(&a.thisDevice, &foot.Widget)
}

func (a *tailscaleApp) line(box *gtk.Box, s string) {
	l := gtk.NewLabel(s)
	l.AddCssClass("empty")
	l.SetXalign(0)
	l.SetWrap(true)
	gtkx.Append(box, &l.Widget)
}

func (a *tailscaleApp) buttons(box *gtk.Box, bs [][2]string) {
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	row.AddCssClass("actions")
	for _, b := range bs {
		btn := gtk.NewButtonWithLabel(b[1])
		btn.AddCssClass("outlined")
		if b[0] == "logout" && a.armed == "logout" {
			btn.AddCssClass("danger")
		}
		btn.SetName(b[0])
		btn.ConnectClicked(&a.onButton)
		gtkx.Append(row, &btn.Widget)
	}
	gtkx.Append(box, &row.Widget)
}

// switchRow is a title and a line beside a switch named name, which
// onSwitch hears.
func (a *tailscaleApp) switchRow(name, title, sub string, on, sensitive bool) *gtk.Box {
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 12)
	row.AddCssClass("row")
	row.AddCssClass("ts-switch-row")
	box := gtk.NewBox(gtk.OrientationVerticalValue, 2)
	box.SetHexpand(true)
	box.SetValign(gtk.AlignCenterValue)
	for _, l := range [][2]string{{title, "title"}, {sub, "sub"}} {
		lb := gtk.NewLabel(l[0])
		lb.AddCssClass(l[1])
		lb.SetXalign(0)
		lb.SetWrap(true)
		gtkx.Append(box, &lb.Widget)
	}
	gtkx.Append(row, &box.Widget)
	sw := gtk.NewSwitch()
	sw.SetName(name)
	sw.SetValign(gtk.AlignCenterValue)
	a.filling = true
	sw.SetActive(on)
	a.filling = false
	sw.SetSensitive(sensitive)
	sw.ConnectStateSet(&a.onSwitch)
	gtkx.Append(row, &sw.Widget)
	return row
}

// setClass adds or removes a style class.
func setClass(w *gtk.Widget, class string, on bool) {
	if on {
		w.AddCssClass(class)
	} else {
		w.RemoveCssClass(class)
	}
}

// --- actions ---

func (a *tailscaleApp) button(name string) {
	switch {
	case name == "notice":
		if a.noticeAction != nil {
			a.noticeAction()
		}
	case name == "back":
		a.closeDevice()
	case strings.HasPrefix(name, "open:"):
		a.openDevice(strings.TrimPrefix(name, "open:"))
	case strings.HasPrefix(name, "exit:"):
		id := strings.TrimPrefix(name, "exit:")
		if id == "" {
			a.edit(tailscale.UseExitNode(""), i18n.T("No exit node: traffic goes out from this device."))
		} else if p := a.st.PeerByID(id); p != nil {
			a.edit(tailscale.UseExitNode(id), i18n.Tf("Traffic now goes through %s.", p.Name()))
		}
	case name == "copy-ip", name == "copy-name":
		a.copyPeer(a.openPeer(), name == "copy-name")
	case name == "ping":
		a.ping()
	case name == "use-exit":
		if p := a.openPeer(); p != nil {
			if p.ExitNode {
				a.edit(tailscale.UseExitNode(""), i18n.Tf("Stopped using %s as exit node.", p.Name()))
			} else {
				a.edit(tailscale.UseExitNode(p.ID), i18n.Tf("Traffic now goes through %s.", p.Name()))
			}
		}
	case name == "save-files":
		a.saveFiles()
	case name == "logout":
		if a.armed != "logout" {
			a.armed, a.armedAt = "logout", time.Now()
			a.showThisDevice()
			return
		}
		a.armed = ""
		go func() {
			ctx, cancel := tailscale.Timeout()
			defer cancel()
			err := a.client.Logout(ctx)
			gtkx.Post(func() {
				if err != nil {
					a.fail(err)
				} else {
					a.say(i18n.T("Logged out."), false)
				}
				a.fetch()
			})
		}()
	case name == "admin":
		openURL(adminConsole)
	}
}

func (a *tailscaleApp) openPeer() *tailscale.Peer {
	if a.open == nil {
		return nil
	}
	return a.st.PeerByID(a.open.id)
}

func (a *tailscaleApp) ping() {
	p := a.openPeer()
	if p == nil {
		return
	}
	name, ip := p.Name(), p.IPv4()
	a.say(i18n.Tf("Pinging %s…", name), false)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		r, err := a.client.Ping(ctx, ip)
		gtkx.Post(func() {
			if err != nil {
				a.fail(err)
				return
			}
			a.say("", false)
			// The answer comes as a notification, which replaces the last.
			var n *gio.Notification
			if r.Err != "" {
				n = gio.NewNotification(i18n.Tf("%s did not answer", name))
				n.SetBody(r.Err)
			} else {
				n = gio.NewNotification(i18n.Tf("%s answered in %.0f ms", name, r.LatencySeconds*1000))
				n.SetBody(i18n.Tf("Direct, %s", r.Endpoint))
				if r.Endpoint == "" {
					n.SetBody(i18n.Tf("Relayed through %s", r.DERPRegionCode))
				}
			}
			a.c.App.SendNotification("ping", n)
			n.Unref()
		})
	}()
}

// sendFiles sends dropped files to the open device.
func (a *tailscaleApp) sendFiles(paths []string) {
	p := a.openPeer()
	if p == nil || len(paths) == 0 {
		return
	}
	id, name := p.ID, p.Name()
	a.say(i18n.Tf("Sending %s to %s…", plural(len(paths), "file", "files"), name), false)
	go func() {
		var errs []error
		for _, path := range paths {
			f, err := os.Open(path)
			if err == nil {
				var st os.FileInfo
				if st, err = f.Stat(); err == nil {
					if st.IsDir() {
						err = fmt.Errorf("%s is a folder", filepath.Base(path))
					} else {
						err = a.client.SendFile(context.Background(), id, filepath.Base(path), f, st.Size())
					}
				}
				f.Close()
			}
			if err != nil {
				errs = append(errs, err)
			}
		}
		gtkx.Post(func() {
			if err := errors.Join(errs...); err != nil {
				a.fail(err)
				return
			}
			a.say(i18n.Tf("Sent %s to %s.", plural(len(paths), "file", "files"), name), false)
		})
	}()
}

// saveFiles moves the received files to Downloads.
func (a *tailscaleApp) saveFiles() {
	files := a.waiting
	go func() {
		dir := downloadsDir()
		var errs []error
		n := 0
		for _, wf := range files {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				errs = append(errs, err)
				break
			}
			path := freeName(dir, wf.Name)
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
			if err == nil {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				err = a.client.SaveFile(ctx, wf.Name, f)
				cancel()
				if cerr := f.Close(); err == nil {
					err = cerr
				}
			}
			if err != nil {
				errs = append(errs, err)
				continue
			}
			n++
		}
		gtkx.Post(func() {
			if err := errors.Join(errs...); err != nil {
				a.fail(err)
			} else {
				a.say(i18n.Tf("Saved %s to %s.", plural(n, "file", "files"), dir), false)
			}
			a.fetchWaiting()
		})
	}()
}

// logIn starts logging in and opens the login page once the daemon gives
// its address.
func (a *tailscaleApp) logIn() {
	a.loggingIn = true
	a.say(i18n.T("Opening the login page…"), false)
	go func() {
		ctx, cancel := tailscale.Timeout()
		defer cancel()
		err := a.client.LoginInteractive(ctx)
		gtkx.Post(func() {
			if err != nil {
				a.loggingIn = false
				a.fail(err)
			}
			a.fetch()
		})
	}()
}

// runAsAdmin runs a command through pkexec, which asks for an
// administrator's password.
func (a *tailscaleApp) runAsAdmin(done string, cmd ...string) {
	go func() {
		path, err := exec.LookPath(cmd[0])
		if err == nil {
			var out []byte
			out, err = exec.Command("pkexec", append([]string{path}, cmd[1:]...)...).CombinedOutput()
			if err != nil {
				var exit *exec.ExitError
				if strings.Contains(string(out), "authentication agent") {
					// Nothing in the session can ask for the password.
					err = errors.New(i18n.Tf("No password prompt is running in this session (a polkit agent); run in a terminal: sudo %s", strings.Join(cmd, " ")))
				} else if errors.As(err, &exit) && (exit.ExitCode() == 126 || exit.ExitCode() == 127) {
					err = errors.New(i18n.T("not done: no administrator's password was given"))
				} else if s := strings.TrimSpace(string(out)); s != "" {
					err = errors.New(s)
				}
			}
		}
		gtkx.Post(func() {
			if err != nil {
				a.say(err.Error(), true)
			} else {
				a.say(done, false)
			}
			a.fetch()
		})
	}()
}

func openURL(u string) {
	go func() {
		if err := exec.Command("xdg-open", u).Run(); err != nil {
			log.Printf("tailscale: open %s: %v", u, err)
		}
	}()
}

// droppedPaths are the local files in a dropped GdkFileList. (The list
// is the drop's; the GSList of its files is ours, a few bytes not freed.)
func droppedPaths(v *gobject.Value) []string {
	boxed := v.GetBoxed()
	if boxed == 0 {
		return nil
	}
	var paths []string
	files := (*gdk.FileList)(unsafe.Add(unsafe.Pointer(nil), boxed)) // as viewerload.go does
	for l := files.GetFiles(); l != nil; l = l.Next {
		if p := (&gio.FileBase{Ptr: l.Data}).GetPath(); p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}

func downloadsDir() string {
	if out, err := exec.Command("xdg-user-dir", "DOWNLOAD").Output(); err == nil {
		if d := strings.TrimSpace(string(out)); d != "" {
			return d
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}

// freeName is a path in dir for name that is not taken: "a.png", then
// "a (2).png" and so on.
func freeName(dir, name string) string {
	name = filepath.Base(name)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	p := filepath.Join(dir, name)
	for i := 2; ; i++ {
		if _, err := os.Lstat(p); errors.Is(err, os.ErrNotExist) {
			return p
		}
		p = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
	}
}
