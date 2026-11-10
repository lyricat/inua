package remoteui

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/apps/remotedesk"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gamestream"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// window is Remote Desktop's window: a library of This computer and the
// machines, the chosen one's page in a panel beside it, and a dialog to add
// a machine.
type window struct {
	c    *appui.Ctx
	win  *gtk.ApplicationWindow
	menu *appui.MenuBar

	sideFind, sideAdd                                       gtk.Button
	sideThis, sideMachines                                  gtk.FlowBox
	sideNone                                                gtk.Label
	thisTile                                                appui.LibraryTile
	panelMark                                               gtk.Box
	panelTitle, panelSub                                    gtk.Label
	sharingError, pendingError, clientsError                gtk.Label // each error where it happened
	machineError, streamError                               gtk.Label
	pages                                                   gtk.Stack
	sharingDesc, sharingNeedText, sharingNeedFix            gtk.Label
	sharingSwitch                                           gtk.Switch
	sharingVirtual                                          gtk.DropDown
	sharingNeed, sharingPendingSection, sharingPending      gtk.Box
	sharingClients                                          gtk.Box
	machineState                                            gtk.Label
	machinePinText, machinePin                              gtk.Label
	machineForget, machineQuit, machinePair, machineConnect gtk.Button
	machinePinBox                                           gtk.Box
	streamSize, streamFPS, streamCodec                      gtk.DropDown
	streamBitrate                                           gtk.SpinButton
	streamHostAudio                                         gtk.Switch
	streamCodecNote                                         gtk.Label
	addDialog                                               gtk.Window
	addAddr                                                 gtk.Entry
	addOK, addCancel                                        gtk.Button
	addError                                                gtk.Label

	settings *remotedesk.Settings
	sharing  *remotedesk.Sharing
	finder   *remotedesk.Finder
	id       *gamestream.Identity
	idErr    string // why there is no identity: every machine's page says it

	found    []remotedesk.Candidate
	selected string // "sharing", or a machine's address
	info     map[string]machineInfo
	errs     map[string]string  // what went wrong with each machine, by address
	pairing  context.CancelFunc // while pairing with the machine selected
	streams  map[string]*stream
	closed   bool
	cancel   context.CancelFunc

	// Rows made in Go, by their button.
	pendingRows map[uintptr]pendingRow        // → device waiting, and its PIN entry
	clientRows  map[uintptr]remotedesk.Client // → paired device
	loading     bool                          // filling the stream settings: not a change
	marking     bool                          // selecting the chosen tile: not the user's choice

	onClick    func(gtk.Button)
	onSwitch   func(gtk.Switch, bool) bool
	onSpin     func(gtk.SpinButton)
	onNotify   func(uintptr, uintptr, uintptr)
	onVirtual  func(uintptr, uintptr, uintptr)
	onClose    func(gtk.Window) bool
	onAddEnter func(gtk.Entry)
	onTile     func(gtk.FlowBox)
	onAddKey   func(gtk.EventControllerKey, uint32, uint32, gdk.ModifierType) bool
}

type pendingRow struct {
	p     remotedesk.Pairing
	entry *gtk.Entry
}

// machineInfo is what a machine said last, or that it did not answer.
type machineInfo struct {
	info  gamestream.ServerInfo
	err   error
	asked bool
}

func openWindow(c *appui.Ctx) *window {
	w := &window{
		c: c, win: &gtk.ApplicationWindow{}, settings: remotedesk.LoadSettings(), sharing: remotedesk.NewSharing(),
		info: map[string]machineInfo{}, errs: map[string]string{}, streams: map[string]*stream{}, selected: "sharing",
		pendingRows: map[uintptr]pendingRow{}, clientRows: map[uintptr]remotedesk.Client{},
	}
	b := c.Load("remote.ui")
	b.Get("remote", w.win)
	for name, ptr := range map[string]gobject.Ptr{
		"side_find": &w.sideFind, "side_add": &w.sideAdd, "side_this": &w.sideThis, "side_none": &w.sideNone,
		"side_machines": &w.sideMachines, "pages": &w.pages, "panel_mark": &w.panelMark,
		"panel_title": &w.panelTitle, "panel_sub": &w.panelSub, "add_dialog": &w.addDialog, "add_cancel": &w.addCancel,
		"sharing_error": &w.sharingError, "pending_error": &w.pendingError, "clients_error": &w.clientsError,
		"machine_error": &w.machineError, "stream_error": &w.streamError,
		"sharing_desc": &w.sharingDesc, "sharing_need_text": &w.sharingNeedText, "sharing_need_fix": &w.sharingNeedFix,
		"sharing_switch": &w.sharingSwitch, "sharing_need": &w.sharingNeed, "sharing_pending_section": &w.sharingPendingSection,
		"sharing_pending": &w.sharingPending, "sharing_clients": &w.sharingClients, "sharing_virtual": &w.sharingVirtual,
		"machine_state":    &w.machineState,
		"machine_pin_text": &w.machinePinText, "machine_pin": &w.machinePin, "machine_forget": &w.machineForget,
		"machine_quit": &w.machineQuit, "machine_pair": &w.machinePair, "machine_connect": &w.machineConnect,
		"machine_pin_box": &w.machinePinBox, "stream_size": &w.streamSize, "stream_fps": &w.streamFPS,
		"stream_codec": &w.streamCodec, "stream_bitrate": &w.streamBitrate, "stream_host_audio": &w.streamHostAudio,
		"stream_codec_note": &w.streamCodecNote, "add_addr": &w.addAddr, "add_ok": &w.addOK, "add_error": &w.addError,
	} {
		b.Get(name, ptr)
	}
	b.Close()

	id, err := gamestream.LoadIdentity(remotedesk.IdentityDir())
	if err != nil {
		log.Printf("remote: identity: %v", err)
		w.idErr = i18n.Tf("This computer has no identity to pair with: %v", err)
	}
	w.id = id
	if id != nil {
		w.finder = remotedesk.NewFinder(id)
	}

	w.onClick = func(b gtk.Button) { w.clicked(b) }
	for _, btn := range []*gtk.Button{&w.sideFind, &w.sideAdd, &w.machineForget, &w.machineQuit,
		&w.machinePair, &w.machineConnect, &w.addOK, &w.addCancel} {
		btn.ConnectClicked(&w.onClick)
	}
	w.onSwitch = func(s gtk.Switch, on bool) bool { return w.switched(s, on) }
	w.sharingSwitch.ConnectStateSet(&w.onSwitch)
	w.streamHostAudio.ConnectStateSet(&w.onSwitch)
	w.onSpin = func(gtk.SpinButton) { w.streamChanged() }
	w.streamBitrate.ConnectValueChanged(&w.onSpin)
	w.onNotify = func(uintptr, uintptr, uintptr) { w.streamChanged() }
	for _, d := range []*gtk.DropDown{&w.streamSize, &w.streamFPS, &w.streamCodec} {
		gtkx.ConnectNotify(d, "selected", &w.onNotify)
	}
	w.onVirtual = func(uintptr, uintptr, uintptr) { w.virtualChanged() }
	gtkx.ConnectNotify(&w.sharingVirtual, "selected", &w.onVirtual)
	w.onAddEnter = func(gtk.Entry) { w.add() }
	w.addAddr.ConnectActivate(&w.onAddEnter)
	w.addDialog.SetTransientFor(&w.win.Window)
	w.addDialog.SetDestroyWithParent(true)
	w.onAddKey = func(_ gtk.EventControllerKey, keyval, _ uint32, _ gdk.ModifierType) bool {
		if keyval == uint32(gdk.KEY_Escape) {
			w.addDialog.SetVisible(false)
			return true
		}
		return false
	}
	addKeys := gtk.NewEventControllerKey()
	addKeys.ConnectKeyPressed(&w.onAddKey)
	w.addDialog.AddController(&addKeys.EventController)

	host, _ := os.Hostname()
	w.thisTile = appui.NewLibraryTile("sharing", "remote-sharing-symbolic", cmpOr(host, i18n.T("This computer")), "", true)
	w.sideThis.Append(&w.thisTile.Child.Widget)
	w.onTile = func(box gtk.FlowBox) {
		if name := appui.SelectedTile(&box); name != "" && !w.marking {
			w.choose(name)
		}
	}
	w.sideThis.ConnectSelectedChildrenChanged(&w.onTile)
	w.sideMachines.ConnectSelectedChildrenChanged(&w.onTile)
	w.onClose = func(gtk.Window) bool {
		w.closed = true
		w.cancel()
		if w.menu != nil {
			w.menu.Close()
		}
		if w.pairing != nil {
			w.pairing()
		}
		return false
	}
	w.win.ConnectCloseRequest(&w.onClose)

	w.menu = w.menuBar()

	w.loadStream()
	w.fillSide()
	if len(w.settings.Machines) > 0 {
		w.choose(w.settings.Machines[0].Addr)
	} else {
		w.choose("sharing")
	}
	var ctx context.Context
	ctx, w.cancel = context.WithCancel(context.Background())
	go w.watch(ctx)
	w.find()
	w.win.Present()
	return w
}

// say shows what went wrong in the error line l, beside what it is about;
// "" hides it.
func say(l *gtk.Label, msg string) {
	l.SetText(msg)
	l.SetVisible(msg != "")
}

// machineFail keeps what went wrong with the machine at addr, shown on
// its page; "" clears it.
func (w *window) machineFail(addr, msg string) {
	if msg == "" {
		delete(w.errs, addr)
	} else {
		w.errs[addr] = msg
	}
	if w.selected == addr {
		w.showMachine()
	}
}

func setClass(wd *gtk.Widget, class string, on bool) {
	if on {
		wd.AddCssClass(class)
	} else {
		wd.RemoveCssClass(class)
	}
}

// background runs f off the main thread, then done on it, unless the
// window closed meanwhile.
func (w *window) background(f func() error, done func(error)) {
	go func() {
		err := f()
		gtkx.Post(func() {
			if !w.closed {
				done(err)
			}
		})
	}()
}

// refreshThis keeps This computer's tile current while another page shows:
// whether it shares, and nothing more.
func (w *window) refreshThis(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	st := w.sharing.Status(ctx)
	gtkx.Post(func() {
		if !w.closed {
			w.showThis(st)
		}
	})
}

// showThis says on This computer's tile, and in the panel's head while its
// page shows, whether it shares.
func (w *window) showThis(st remotedesk.Status) {
	state := i18n.T("Not sharing")
	switch {
	case st.Running:
		state = i18n.T("Sharing its desktop")
	case w.sharing.On():
		state = i18n.T("Starting…")
	}
	w.thisTile.Sub.SetText(state)
	w.thisTile.SetOn(st.Running)
	if w.selected == "sharing" {
		w.showThisHead()
	}
}

// showThisHead puts This computer in the panel's head, as its tile says it.
func (w *window) showThisHead() {
	w.panelTitle.SetText(w.thisTile.Title.GetText())
	w.panelSub.SetText(w.thisTile.Sub.GetText())
	setClass(&w.panelMark.Widget, "on", w.thisTile.Mark.HasCssClass("on"))
}

// watch keeps the sharing page current (devices waiting to pair come and
// go) and the machine selected's state.
func (w *window) watch(ctx context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		var page string
		gtkx.Call(time.Second, func() bool { page = w.selected; return true })
		if page == "sharing" {
			w.refreshSharing(ctx)
		} else {
			w.refreshThis(ctx)
			w.ask(ctx, page)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// --- the library ---

// fillSide makes the machines' tiles: the saved ones, then those found and
// not saved, each marked while it is connected.
func (w *window) fillSide() {
	w.sideMachines.RemoveAll()
	saved := map[string]bool{}
	tile := func(name, sub, addr string) {
		t := appui.NewLibraryTile(addr, "remote-machine-symbolic", name, sub, true)
		_, on := w.streams[addr]
		t.SetOn(on)
		w.sideMachines.Append(&t.Child.Widget)
	}
	n := 0
	for _, m := range w.settings.Machines {
		saved[m.Addr] = true
		if m.ID != "" {
			saved["id:"+m.ID] = true
		}
		sub := i18n.T("Not paired")
		if m.Paired() {
			sub = i18n.T("Paired")
		}
		if _, on := w.streams[m.Addr]; on {
			sub = i18n.T("Connected")
		}
		tile(remotedesk.Label(m), sub, m.Addr)
		n++
	}
	for _, f := range w.found {
		if saved[f.Addr] || f.ID != "" && saved["id:"+f.ID] {
			continue
		}
		sub := i18n.T("Found on this network")
		if f.Tailnet {
			sub = i18n.T("Found on the tailnet")
		}
		tile(f.Name, sub, f.Addr)
		n++
	}
	w.sideMachines.SetVisible(n > 0)
	w.sideNone.SetVisible(n == 0)
	w.markSide()
}

// markSide selects the chosen one's tile, in whichever group holds it.
func (w *window) markSide() {
	w.marking = true
	defer func() { w.marking = false }()
	w.sideThis.UnselectAll()
	w.sideMachines.UnselectAll()
	if w.selected == "sharing" {
		w.sideThis.SelectChild(&w.thisTile.Child)
		return
	}
	for i := int32(0); ; i++ {
		t := w.sideMachines.GetChildAtIndex(i)
		if t == nil {
			return
		}
		if t.GetName() == w.selected {
			w.sideMachines.SelectChild(t)
			return
		}
	}
}

// showAdd asks for a machine's address in the add dialog.
func (w *window) showAdd() {
	say(&w.addError, "")
	w.addDialog.Present()
	w.addAddr.GrabFocus()
}

// find looks for machines sharing their desktop, on the local network
// and the tailnet.
func (w *window) find() {
	if w.finder == nil {
		return
	}
	w.sideFind.SetSensitive(false)
	var found []remotedesk.Candidate
	w.background(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		found = w.finder.Find(ctx)
		return nil
	}, func(error) {
		w.sideFind.SetSensitive(true)
		w.found = found
		w.fillSide()
	})
}

// choose shows a page: "sharing", or a machine's, by its address.
func (w *window) choose(page string) {
	if w.pairing != nil && page != w.selected {
		w.pairing()
		w.pairing = nil
	}
	w.selected = page
	switch page {
	case "sharing":
		w.pages.SetVisibleChildName("sharing")
		w.showThisHead()
		w.showSharing(remotedesk.Status{}, nil, nil, nil, false)
		go w.refreshSharing(context.Background())
	default:
		w.pages.SetVisibleChildName("machine")
		w.machinePinBox.SetVisible(false)
		w.showMachine()
		go w.ask(context.Background(), page)
	}
	w.markSide()
}

func (w *window) clicked(b gtk.Button) {
	switch b.GetName() {
	case "side_add":
		w.showAdd()
	case "add_cancel":
		w.addDialog.SetVisible(false)
	case "side_find":
		w.find()
	case "machine_connect":
		w.connect()
	case "machine_pair":
		w.pair()
	case "machine_quit":
		w.quit()
	case "machine_forget":
		w.forget()
	case "add_ok":
		w.add()
	case "pending_pair":
		if r, ok := w.pendingRows[b.GoPointer()]; ok {
			w.pairDevice(r)
		}
	case "client_unpair":
		if c, ok := w.clientRows[b.GoPointer()]; ok {
			w.unpairDevice(c)
		}
	}
}

func (w *window) switched(s gtk.Switch, on bool) bool {
	switch s.GetName() {
	case "stream_host_audio":
		if !w.loading {
			w.settings.Stream.HostAudio = on
			w.saveStream()
		}
		return false
	}
	// The sharing switch: on or off once Sunshine has started or stopped.
	if w.loading {
		return false
	}
	say(&w.sharingError, "")
	w.sharingSwitch.SetSensitive(false)
	w.background(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), remotedesk.StartTimeout)
		defer cancel()
		return w.sharing.SetOn(ctx, on)
	}, func(err error) {
		w.sharingSwitch.SetSensitive(true)
		if err != nil {
			say(&w.sharingError, i18n.Tf("Sharing did not start: %v", err))
			w.loading = true
			w.sharingSwitch.SetActive(false)
			w.sharingSwitch.SetState(false)
			w.loading = false
			return
		}
		w.sharingSwitch.SetState(on)
		go w.refreshSharing(context.Background())
	})
	return true // the state follows when Sunshine has
}

// virtualChanged keeps the virtual screen's setting, and adds or removes
// the screen now.
func (w *window) virtualChanged() {
	if w.loading {
		return
	}
	mode := ""
	if i := int(w.sharingVirtual.GetSelected()); i > 0 && i <= len(remotedesk.VirtualModes) {
		mode = remotedesk.VirtualModes[i-1]
	}
	say(&w.sharingError, "")
	if err := w.sharing.SetVirtual(mode); err != nil {
		say(&w.sharingError, err.Error())
		return
	}
	w.sharingVirtual.SetSensitive(false)
	w.background(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), remotedesk.StartTimeout)
		defer cancel()
		return w.sharing.SyncVirtual(ctx)
	}, func(err error) {
		w.sharingVirtual.SetSensitive(true)
		if err != nil {
			say(&w.sharingError, i18n.Tf("The virtual screen did not change: %v", err))
		}
		go w.refreshSharing(context.Background())
	})
}

// saveStream keeps the stream's settings, saying under them when it cannot.
func (w *window) saveStream() {
	if err := w.settings.Save(); err != nil {
		say(&w.streamError, i18n.Tf("The settings were not saved: %v", err))
	} else {
		say(&w.streamError, "")
	}
}

// --- this computer: sharing ---

func (w *window) refreshSharing(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	st := w.sharing.Status(ctx)
	var pending []remotedesk.Pairing
	var clients []remotedesk.Client
	var twice []string
	if st.Running {
		pending, _ = w.sharing.Pending(ctx)
		clients, _ = w.sharing.Clients(ctx)
		twice = w.sharing.Twice()
	}
	gtkx.Post(func() {
		if !w.closed {
			w.showSharing(st, pending, clients, twice, true)
		}
	})
}

// showSharing shows the sharing page; twice are the devices paired twice,
// which Sunshine refuses.
func (w *window) showSharing(st remotedesk.Status, pending []remotedesk.Pairing, clients []remotedesk.Client, twice []string, known bool) {
	on := w.sharing.On()
	name, _ := os.Hostname()
	if known {
		w.showThis(st)
	}
	switch {
	case st.Running:
		w.sharingDesc.SetText(i18n.Tf("Other devices see this computer as %s, on this network and the tailnet. A device pairs once, with a PIN typed here.", name))
	case on:
		w.sharingDesc.SetText(i18n.T("Sunshine is starting."))
	default:
		w.sharingDesc.SetText(i18n.T("Lets your other devices see and control this desktop, with Moonlight or Remote Desktop."))
	}
	w.loading = true
	w.sharingSwitch.SetActive(on)
	w.sharingSwitch.SetState(on && (st.Running || !known))
	w.sharingVirtual.SetSelected(uint32(slices.Index(remotedesk.VirtualModes, w.sharing.Virtual()) + 1))
	w.loading = false

	need, fix := "", ""
	switch st.Need {
	case remotedesk.NeedSunshine:
		need = i18n.T("Sharing needs Sunshine, which is not installed.")
		fix = i18n.T("Install the package for your system from github.com/LizardByte/Sunshine/releases (Ubuntu: sudo apt install ./sunshine_…_amd64.deb).")
	case remotedesk.NeedInput:
		need = i18n.T("The other device's keyboard and mouse cannot reach this desktop: /dev/uinput is not yours.")
		fix = "echo '" + remotedesk.UinputRule + "' | sudo tee /etc/udev/rules.d/60-sunshine.rules && sudo udevadm control --reload && sudo udevadm trigger && sudo modprobe uinput"
	case remotedesk.NeedDisplay:
		need = i18n.T("Sharing needs a graphical session to show.")
	}
	w.sharingNeed.SetVisible(need != "")
	w.sharingNeedText.SetText(need)
	w.sharingNeedFix.SetText(fix)
	w.sharingNeedFix.SetVisible(fix != "")
	w.sharingSwitch.SetSensitive(st.Need != remotedesk.NeedSunshine || on)

	// Keep a PIN being typed: rebuild the rows only when the devices change.
	same := len(pending) == len(w.pendingRows)
	for _, r := range w.pendingRows {
		found := false
		for _, p := range pending {
			found = found || p.ID == r.p.ID
		}
		same = same && found
	}
	if !same {
		gtkx.Clear(&w.sharingPending)
		w.pendingRows = map[uintptr]pendingRow{}
		for _, p := range pending {
			row := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
			row.AddCssClass("row")
			text := gtk.NewBox(gtk.OrientationVerticalValue, 2)
			text.SetHexpand(true)
			l := gtk.NewLabel(cmpOr(p.Name, i18n.T("A device")))
			l.AddCssClass("label")
			l.SetXalign(0)
			gtkx.Append(text, &l.Widget)
			s := gtk.NewLabel(i18n.Tf("at %s: type the PIN it shows", p.Address))
			s.AddCssClass("sub")
			s.SetXalign(0)
			gtkx.Append(text, &s.Widget)
			gtkx.Append(row, &text.Widget)
			e := gtk.NewEntry()
			e.SetMaxLength(4)
			e.SetWidthChars(6)
			e.SetPlaceholderText("PIN")
			e.SetInputPurpose(gtk.InputPurposeDigitsValue)
			e.SetValign(gtk.AlignCenterValue)
			gtkx.Append(row, &e.Widget)
			btn := gtk.NewButtonWithLabel(i18n.T("Pair"))
			btn.AddCssClass("suggested-action")
			btn.SetName("pending_pair")
			btn.SetValign(gtk.AlignCenterValue)
			btn.ConnectClicked(&w.onClick)
			w.pendingRows[btn.GoPointer()] = pendingRow{p: p, entry: e}
			gtkx.Append(row, &btn.Widget)
			gtkx.Append(&w.sharingPending, &row.Widget)
		}
	}
	w.sharingPendingSection.SetVisible(len(pending) > 0 || w.pendingError.GetVisible())

	gtkx.Clear(&w.sharingClients)
	w.clientRows = map[uintptr]remotedesk.Client{}
	if len(clients) == 0 {
		l := gtk.NewLabel(i18n.T("No device is paired yet."))
		l.AddCssClass("sub")
		l.AddCssClass("row")
		l.SetXalign(0)
		if !st.Running {
			l.SetText(i18n.T("The paired devices show while sharing is on."))
		}
		gtkx.Append(&w.sharingClients, &l.Widget)
	}
	for _, c := range clients {
		row := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
		row.AddCssClass("row")
		text := gtk.NewBox(gtk.OrientationVerticalValue, 2)
		text.SetHexpand(true)
		text.SetValign(gtk.AlignCenterValue)
		l := gtk.NewLabel(c.Name)
		l.AddCssClass("label")
		l.SetXalign(0)
		gtkx.Append(text, &l.Widget)
		if slices.Contains(twice, c.Name) {
			s := gtk.NewLabel(i18n.T("Paired twice, so it cannot connect: unpair each, then pair it again."))
			s.AddCssClass("sub")
			s.AddCssClass("remote-error")
			s.SetXalign(0)
			s.SetWrap(true)
			gtkx.Append(text, &s.Widget)
		}
		gtkx.Append(row, &text.Widget)
		btn := gtk.NewButtonWithLabel(i18n.T("Unpair"))
		btn.AddCssClass("outlined")
		btn.SetName("client_unpair")
		btn.SetValign(gtk.AlignCenterValue)
		btn.ConnectClicked(&w.onClick)
		w.clientRows[btn.GoPointer()] = c
		gtkx.Append(row, &btn.Widget)
		gtkx.Append(&w.sharingClients, &row.Widget)
	}
}

func cmpOr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func (w *window) pairDevice(r pendingRow) {
	name := cmpOr(r.p.Name, r.p.Address)
	pin := strings.TrimSpace(r.entry.GetText())
	if len(pin) != 4 || strings.Trim(pin, "0123456789") != "" {
		w.pairingFail(i18n.T("A PIN is four digits."))
		return
	}
	w.pairingFail("")
	w.background(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return w.sharing.Pair(ctx, r.p, pin, name)
	}, func(err error) {
		var before *remotedesk.PairedBeforeError
		switch {
		case errors.Is(err, remotedesk.ErrPIN):
			w.pairingFail(i18n.Tf("That is not the PIN %s shows.", name))
		case errors.As(err, &before):
			w.pairingFail(i18n.Tf("%s was paired here before, as %s, and a device paired twice is refused. Its old pairing is removed: pair again from %s.",
				name, strings.Join(before.Before, ", "), name))
		case err != nil:
			w.pairingFail(i18n.Tf("%s was not paired: %v", name, err))
		}
		go w.refreshSharing(context.Background())
	})
}

// pairingFail says why a device was not paired, in the section of the
// devices waiting, which stays while it says so.
func (w *window) pairingFail(msg string) {
	say(&w.pendingError, msg)
	w.sharingPendingSection.SetVisible(msg != "" || len(w.pendingRows) > 0)
}

func (w *window) unpairDevice(c remotedesk.Client) {
	say(&w.clientsError, "")
	w.background(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return w.sharing.Unpair(ctx, c.UUID)
	}, func(err error) {
		if err != nil {
			say(&w.clientsError, i18n.Tf("%s was not unpaired: %v", c.Name, err))
		}
		go w.refreshSharing(context.Background())
	})
}

// --- a machine ---

// machine is the machine selected: saved, or found.
func (w *window) machine() (remotedesk.Machine, bool) {
	for _, m := range w.settings.Machines {
		if m.Addr == w.selected {
			return m, true
		}
	}
	for _, f := range w.found {
		if f.Addr == w.selected {
			return remotedesk.Machine{Name: f.Name, Addr: f.Addr, ID: f.ID}, false
		}
	}
	return remotedesk.Machine{Addr: w.selected}, false
}

func (w *window) host(m remotedesk.Machine) *gamestream.Host {
	h := &gamestream.Host{Addr: m.Addr, ID: w.id}
	if m.Cert != "" {
		h.Cert = []byte(m.Cert)
	}
	return h
}

// ask asks a machine what it is, and shows it if it is still selected.
func (w *window) ask(ctx context.Context, addr string) {
	var m remotedesk.Machine
	gtkx.Call(time.Second, func() bool {
		if w.selected == addr {
			m, _ = w.machine()
		}
		return true
	})
	if m.Addr == "" || w.id == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	info, err := w.host(m).Info(ctx)
	gtkx.Post(func() {
		if w.closed {
			return
		}
		w.info[addr] = machineInfo{info: info, err: err, asked: true}
		if w.selected == addr {
			w.showMachine()
		}
	})
}

func (w *window) showMachine() {
	m, saved := w.machine()
	mi := w.info[m.Addr]
	_, streaming := w.streams[m.Addr]
	w.panelTitle.SetText(remotedesk.Label(m))
	w.panelSub.SetText(m.Addr)
	setClass(&w.panelMark.Widget, "on", streaming)
	paired := m.Paired() && (!mi.asked || mi.err != nil || mi.info.Paired())
	state := i18n.T("Asking…")
	switch {
	case streaming:
		state = i18n.T("Connected: its desktop is in a window of its own.")
	case !mi.asked:
	case mi.err != nil:
		state = i18n.T("It does not answer: it is off, or not sharing its desktop.")
	case !mi.info.Sunshine():
		state = i18n.T("It shares with Nvidia's GameStream, which Remote Desktop does not speak.")
	case m.Paired() && !mi.info.Paired():
		state = i18n.T("It has forgotten this computer: pair again.")
	case !m.Paired():
		state = i18n.T("Not paired: pair once, with a PIN typed on it.")
	case mi.info.Busy():
		state = i18n.T("Paired. Its desktop is being streamed; connecting joins in.")
	default:
		state = i18n.T("Paired, and ready.")
	}
	w.machineState.SetText(state)
	reachable := mi.asked && mi.err == nil && mi.info.Sunshine()
	w.machineConnect.SetVisible(paired)
	w.machineConnect.SetSensitive(!streaming && (reachable || !mi.asked))
	w.machinePair.SetVisible(!paired || mi.asked && mi.err == nil && !mi.info.Paired())
	w.machinePair.SetSensitive(reachable || w.pairing != nil)
	if w.pairing != nil {
		w.machinePair.SetLabel(i18n.T("Cancel"))
	} else {
		w.machinePair.SetLabel(i18n.T("Pair"))
	}
	w.machineQuit.SetVisible(paired && reachable && mi.info.Busy())
	w.machineForget.SetVisible(saved)
	say(&w.machineError, cmpOr(w.idErr, w.errs[m.Addr]))
}

// pair pairs with the machine selected: it shows a PIN to type there,
// and waits until it is typed.
func (w *window) pair() {
	if w.pairing != nil {
		w.pairing()
		w.pairing = nil
		w.machinePinBox.SetVisible(false)
		w.showMachine()
		return
	}
	m, _ := w.machine()
	if w.id == nil {
		return
	}
	delete(w.errs, m.Addr)
	h := w.host(m)
	h.Cert = nil
	pin := gamestream.NewPIN()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	w.pairing = cancel
	w.machinePinText.SetText(i18n.Tf("On %s, open Remote Desktop → Sharing (or Sunshine's PIN page) and type:", remotedesk.Label(m)))
	w.machinePin.SetText(pin)
	w.machinePinBox.SetVisible(true)
	w.showMachine()
	name, _ := os.Hostname()
	w.background(func() error {
		return h.Pair(ctx, pin, cmpOr(name, "inua"))
	}, func(err error) {
		cancel()
		if w.pairing == nil { // cancelled, or another page chosen
			return
		}
		w.pairing = nil
		w.machinePinBox.SetVisible(false)
		label := remotedesk.Label(m)
		switch {
		case err == nil:
			info, _ := w.info[m.Addr]
			m.Cert = string(h.Cert)
			m.Name = cmpOr(info.info.Hostname, m.Name)
			m.ID = cmpOr(info.info.UniqueID, m.ID)
			w.settings.Keep(m)
			if err := w.settings.Save(); err != nil {
				w.machineFail(m.Addr, i18n.Tf("Paired, but not saved, so not kept after this window: %v", err))
			}
			w.fillSide()
		case errors.Is(err, gamestream.ErrWrongPIN):
			w.machineFail(m.Addr, i18n.T("The PIN typed there was not this one. Pair again."))
		case errors.Is(err, gamestream.ErrRefused):
			w.machineFail(m.Addr, i18n.Tf("%s took the PIN, then refused this computer: it had this computer paired already, perhaps under another name, and refuses a computer paired twice. If it shares with inua, it has removed the old pairing: pair again. Otherwise unpair this computer there first.", label))
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		default:
			w.machineFail(m.Addr, i18n.Tf("Pairing failed: %v", err))
		}
		go w.ask(context.Background(), m.Addr)
	})
}

func (w *window) quit() {
	m, _ := w.machine()
	w.machineFail(m.Addr, "")
	w.background(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return w.host(m).Quit(ctx)
	}, func(err error) {
		if err != nil {
			w.machineFail(m.Addr, i18n.Tf("Its session did not end: %v", err))
		}
		go w.ask(context.Background(), m.Addr)
	})
}

func (w *window) forget() {
	m, _ := w.machine()
	if s, ok := w.streams[m.Addr]; ok {
		s.close()
	}
	w.settings.Forget(m.Addr)
	if err := w.settings.Save(); err != nil {
		w.machineFail(m.Addr, i18n.Tf("Not forgotten: the list of machines was not saved: %v", err))
		return
	}
	delete(w.errs, m.Addr)
	go func() { // the machine may forget this computer too
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		w.host(m).Unpair(ctx)
	}()
	w.fillSide()
	w.choose("sharing")
}

// add adds a machine by its address, once it answers there.
func (w *window) add() {
	addr := strings.TrimSpace(w.addAddr.GetText())
	if w.id == nil {
		say(&w.addError, w.idErr)
	}
	if addr == "" || w.id == nil {
		return
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(strings.Trim(addr, "[]"), strconv.Itoa(gamestream.DefaultPort))
	}
	w.addOK.SetSensitive(false)
	var info gamestream.ServerInfo
	w.background(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var err error
		info, err = (&gamestream.Host{Addr: addr, ID: w.id}).Info(ctx)
		return err
	}, func(err error) {
		w.addOK.SetSensitive(true)
		if err != nil {
			say(&w.addError, i18n.Tf("Nothing shares a desktop at %s: %v", addr, err))
			return
		}
		w.settings.Keep(remotedesk.Machine{Name: info.Hostname, Addr: addr, ID: info.UniqueID})
		if err := w.settings.Save(); err != nil {
			say(&w.addError, i18n.Tf("Added, but not saved, so not kept after this window: %v", err))
			return
		}
		w.addAddr.SetText("")
		w.addDialog.SetVisible(false)
		w.fillSide()
		w.choose(addr)
	})
}

// --- the stream's settings ---

var sizes = [][2]int{{0, 0}, {1280, 720}, {1920, 1080}, {2560, 1440}, {3840, 2160}}
var fpses = []int{30, 60, 90, 120}
var codecs = []string{"h264", "hevc", "av1"}

func (w *window) loadStream() {
	st := w.settings.Stream
	w.loading = true
	defer func() { w.loading = false }()
	for i, s := range sizes {
		if s[0] == st.Width && s[1] == st.Height {
			w.streamSize.SetSelected(uint32(i))
		}
	}
	for i, f := range fpses {
		if f == st.FPS {
			w.streamFPS.SetSelected(uint32(i))
		}
	}
	for i, c := range codecs {
		if c == st.Codec {
			w.streamCodec.SetSelected(uint32(i))
		}
	}
	w.streamBitrate.SetValue(float64(max(st.Bitrate, 2000)) / 1000)
	w.streamHostAudio.SetActive(st.HostAudio)
	w.streamHostAudio.SetState(st.HostAudio)
	w.codecNote()
}

func (w *window) streamChanged() {
	if w.loading {
		return
	}
	st := &w.settings.Stream
	if i := int(w.streamSize.GetSelected()); i < len(sizes) {
		st.Width, st.Height = sizes[i][0], sizes[i][1]
	}
	if i := int(w.streamFPS.GetSelected()); i < len(fpses) {
		st.FPS = fpses[i]
	}
	if i := int(w.streamCodec.GetSelected()); i < len(codecs) {
		st.Codec = codecs[i]
	}
	st.Bitrate = int(w.streamBitrate.GetValue()) * 1000
	w.codecNote()
	w.saveStream()
}

// codecNote says whether this computer decodes the codec with its GPU.
func (w *window) codecNote() {
	codec := map[string]string{"h264": "h264", "hevc": "h265", "av1": "av1"}[w.settings.Stream.Codec]
	dec := decoderFor(codec)
	switch {
	case dec == "":
		w.streamCodecNote.SetText(i18n.T("This computer cannot decode it: choose another."))
	case strings.HasPrefix(dec, "avdec") || strings.HasPrefix(dec, "dav1d") || dec == "av1dec":
		w.streamCodecNote.SetText(i18n.T("Decoded by the processor here: slower than the graphics card."))
	default:
		w.streamCodecNote.SetText(i18n.T("Decoded by the graphics card here. HEVC and AV1 look better than H.264 at the same bitrate."))
	}
}

// connect opens the selected machine's desktop in a window of its own.
func (w *window) connect() {
	m, _ := w.machine()
	if s, ok := w.streams[m.Addr]; ok {
		s.win.Present()
		return
	}
	delete(w.errs, m.Addr)
	s := openStream(w, m)
	if s == nil {
		return
	}
	w.streams[m.Addr] = s
	w.fillSide()
	w.showMachine()
}

// ended forgets a stream whose window closed.
func (w *window) ended(s *stream, err error) {
	if w.streams[s.m.Addr] == s {
		delete(w.streams, s.m.Addr)
	}
	if err != nil {
		w.errs[s.m.Addr] = i18n.Tf("The connection ended: %v", err)
	}
	if !w.closed {
		w.fillSide()
		if w.selected == s.m.Addr {
			w.showMachine()
		}
	}
}
