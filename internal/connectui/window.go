package connectui

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/apps/connect"
	"github.com/lyricat/inua/internal/appui"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/kdeconnect"
)

// window is Mobile Link's window: the index of This computer and the
// devices, and the chosen one's page.
type window struct {
	c    *appui.Ctx
	win  *gtk.ApplicationWindow
	menu *appui.MenuBar

	sideThis, sideFind, sideAdd                         gtk.Button
	sideThisTitle, sideThisSub, status                  gtk.Label
	sideDevicesBox                                      gtk.Box
	stack                                               gtk.Stack
	thisDesc, thisName, commandsNote                    gtk.Label
	thisSwitch                                          gtk.Switch
	commands                                            gtk.Box
	commandsAdd, commandsSave                           gtk.Button
	deviceTitle, deviceState, deviceAddr                gtk.Label
	deviceKeyText, deviceKey                            gtk.Label
	deviceUnpair, devicePair                            gtk.Button
	deviceKeyBox, deviceActions, deviceAllow            gtk.Box
	devicePing, deviceRing, deviceFiles, deviceTextSend gtk.Button
	deviceText                                          gtk.Entry
	setClipboard, setNotifications, setInput, setCmds   gtk.Switch
	addAddr                                             gtk.Entry
	addOK                                               gtk.Button
	addError                                            gtk.Label

	running  bool
	devices  []connect.DeviceView
	selected string // "this", "add", or a device's ID
	closed   bool
	loading  bool // filling the switches: not a change
	cancel   context.CancelFunc
	dialog   *gtk.FileDialog

	cmdRows     []cmdRow
	deviceRows  map[uintptr]string     // a device's button → its ID
	sideButtons map[string]*gtk.Button // by ID, to mark the selected

	onClick  func(gtk.Button)
	onSwitch func(gtk.Switch, bool) bool
	onClose  func(gtk.Window) bool
	onEnter  func(gtk.Entry)
	onFiles  gio.AsyncReadyCallback
}

// cmdRow is a command being edited.
type cmdRow struct {
	key           string
	name, command *gtk.Entry
	remove        *gtk.Button
}

func openWindow(c *appui.Ctx) *window {
	w := &window{c: c, win: &gtk.ApplicationWindow{}, selected: "this", deviceRows: map[uintptr]string{}, sideButtons: map[string]*gtk.Button{}}
	b := c.Load("connect.ui")
	b.Get("connect", w.win)
	for name, ptr := range map[string]gobject.Ptr{
		"side_this": &w.sideThis, "side_find": &w.sideFind, "side_add": &w.sideAdd,
		"side_this_title": &w.sideThisTitle, "side_this_sub": &w.sideThisSub, "status": &w.status,
		"side_devices": &w.sideDevicesBox, "pages": &w.stack,
		"this_desc": &w.thisDesc, "this_name": &w.thisName, "this_switch": &w.thisSwitch,
		"commands": &w.commands, "commands_note": &w.commandsNote, "commands_add": &w.commandsAdd, "commands_save": &w.commandsSave,
		"device_title": &w.deviceTitle, "device_state": &w.deviceState, "device_addr": &w.deviceAddr,
		"device_key_text": &w.deviceKeyText, "device_key": &w.deviceKey, "device_unpair": &w.deviceUnpair,
		"device_pair": &w.devicePair, "device_key_box": &w.deviceKeyBox, "device_actions_section": &w.deviceActions,
		"device_allow_section": &w.deviceAllow, "device_ping": &w.devicePing, "device_ring": &w.deviceRing,
		"device_files": &w.deviceFiles, "device_text": &w.deviceText, "device_text_send": &w.deviceTextSend,
		"set_clipboard": &w.setClipboard, "set_notifications": &w.setNotifications, "set_input": &w.setInput,
		"set_commands": &w.setCmds, "add_addr": &w.addAddr, "add_ok": &w.addOK, "add_error": &w.addError,
	} {
		b.Get(name, ptr)
	}
	b.Close()

	w.onClick = func(b gtk.Button) { w.clicked(b) }
	for _, btn := range []*gtk.Button{&w.sideThis, &w.sideFind, &w.sideAdd, &w.commandsAdd, &w.commandsSave,
		&w.deviceUnpair, &w.devicePair, &w.devicePing, &w.deviceRing, &w.deviceFiles, &w.deviceTextSend, &w.addOK} {
		btn.ConnectClicked(&w.onClick)
	}
	w.onSwitch = func(s gtk.Switch, on bool) bool { return w.switched(s, on) }
	for _, s := range []*gtk.Switch{&w.thisSwitch, &w.setClipboard, &w.setNotifications, &w.setInput, &w.setCmds} {
		s.ConnectStateSet(&w.onSwitch)
	}
	w.onEnter = func(e gtk.Entry) {
		if e.GoPointer() == w.addAddr.GoPointer() {
			w.add()
		} else {
			w.sendText()
		}
	}
	w.addAddr.ConnectActivate(&w.onEnter)
	w.deviceText.ConnectActivate(&w.onEnter)
	w.onFiles = func(_, result, _ uintptr) { w.filesChosen(result) }
	w.onClose = func(gtk.Window) bool {
		w.closed = true
		w.cancel()
		if w.menu != nil {
			w.menu.Close()
		}
		return false
	}
	w.win.ConnectCloseRequest(&w.onClose)
	name, _ := os.Hostname()
	w.sideThisTitle.SetText(name)
	w.thisName.SetText(name)
	w.choose("this")
	var ctx context.Context
	ctx, w.cancel = context.WithCancel(context.Background())
	go w.watch(ctx)
	w.loadCommands()
	w.menu = w.menuBar()
	return w
}

// fail says what went wrong at the foot of the index; "" clears it.
func (w *window) fail(msg string) {
	w.status.SetText(msg)
	setClass(&w.status.Widget, "error", msg != "")
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

// call asks the service, off the main thread; done gets its error.
func (w *window) call(done func(error), method string, args ...any) {
	w.background(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return connect.Call(ctx, method, args...).Err
	}, func(err error) {
		if err != nil {
			w.fail(errText(err))
		}
		if done != nil {
			done(err)
		}
		go w.refresh()
	})
}

func errText(err error) string {
	if e, ok := err.(dbus.Error); ok && len(e.Body) > 0 {
		if s, ok := e.Body[0].(string); ok {
			return strings.TrimPrefix(s, "kdeconnect: ")
		}
	}
	return err.Error()
}

// watch keeps the window current: the service says when the devices
// change, and is asked now and then whether it runs.
func (w *window) watch(ctx context.Context) {
	sigs := make(chan *dbus.Signal, 16)
	if conn, err := dbus.SessionBus(); err == nil {
		conn.AddMatchSignal(dbus.WithMatchInterface(connect.BusIface), dbus.WithMatchMember("Changed"))
		conn.Signal(sigs)
		defer conn.RemoveSignal(sigs)
	}
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		w.refresh()
		select {
		case <-ctx.Done():
			return
		case <-sigs:
		case <-t.C:
		}
	}
}

// refresh asks the service for the devices, and shows them.
func (w *window) refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var js string
	err := connect.Call(ctx, "Devices").Store(&js)
	var list []connect.DeviceView
	if err == nil {
		json.Unmarshal([]byte(js), &list)
	}
	gtkx.Post(func() {
		if w.closed {
			return
		}
		w.running = err == nil
		w.devices = list
		w.fillSide()
		w.show()
	})
}

// --- the index ---

func (w *window) fillSide() {
	gtkx.Clear(&w.sideDevicesBox)
	w.deviceRows = map[uintptr]string{}
	w.sideButtons = map[string]*gtk.Button{}
	if w.running && len(w.devices) == 0 {
		l := gtk.NewLabel(i18n.T("None found yet: open KDE Connect on your phone."))
		l.AddCssClass("sub")
		l.AddCssClass("connect-none")
		l.SetXalign(0)
		l.SetWrap(true)
		gtkx.Append(&w.sideDevicesBox, &l.Widget)
	}
	// Paired first, then those found.
	for _, paired := range []bool{true, false} {
		for _, d := range w.devices {
			if (d.Pair == kdeconnect.Paired) != paired {
				continue
			}
			b := gtk.NewButton()
			b.AddCssClass("index-item")
			b.SetName("device")
			box := gtk.NewBox(gtk.OrientationHorizontalValue, 10)
			img := gtk.NewImageFromIconName(connect.DeviceIcon(d.Type))
			img.AddCssClass("index-icon")
			setClass(&img.Widget, "connect-away", !d.Reachable)
			gtkx.Append(box, &img.Widget)
			text := gtk.NewBox(gtk.OrientationVerticalValue, 0)
			text.SetHexpand(true)
			t := gtk.NewLabel(d.Name)
			t.AddCssClass("title")
			t.SetXalign(0)
			t.SetEllipsize(3) // end
			gtkx.Append(text, &t.Widget)
			s := gtk.NewLabel(connect.StateText(d))
			s.AddCssClass("sub")
			s.SetXalign(0)
			s.SetEllipsize(3)
			gtkx.Append(text, &s.Widget)
			gtkx.Append(box, &text.Widget)
			b.SetChild(&box.Widget)
			b.ConnectClicked(&w.onClick)
			w.deviceRows[b.GoPointer()] = d.ID
			w.sideButtons[d.ID] = b
			gtkx.Append(&w.sideDevicesBox, &b.Widget)
		}
	}
	w.markSide()
}

func (w *window) markSide() {
	setClass(&w.sideThis.Widget, "selected", w.selected == "this")
	setClass(&w.sideAdd.Widget, "selected", w.selected == "add")
	for id, b := range w.sideButtons {
		setClass(&b.Widget, "selected", id == w.selected)
	}
}

// choose shows a page: "this", "add", or a device's, by its ID.
func (w *window) choose(page string) {
	w.selected = page
	switch page {
	case "this", "add":
		w.stack.SetVisibleChildName(page)
		if page == "add" {
			w.addError.SetText("")
			w.addAddr.GrabFocus()
		}
	default:
		w.stack.SetVisibleChildName("device")
		w.deviceText.SetText("")
	}
	w.show()
	w.markSide()
}

func (w *window) clicked(b gtk.Button) {
	w.fail("")
	id := w.selected
	switch b.GetName() {
	case "side_this":
		w.choose("this")
	case "side_add":
		w.choose("add")
	case "side_find":
		w.call(nil, "Refresh")
	case "device":
		if id, ok := w.deviceRows[b.GoPointer()]; ok {
			w.choose(id)
		}
	case "device_pair":
		w.call(nil, "Pair", id)
	case "device_unpair":
		w.call(nil, "Unpair", id)
	case "device_ping":
		w.call(nil, "Ping", id)
	case "device_ring":
		w.call(nil, "Ring", id)
	case "device_files":
		w.chooseFiles()
	case "device_text_send":
		w.sendText()
	case "add_ok":
		w.add()
	case "commands_add":
		w.addCommandRow(connect.Command{})
	case "commands_save":
		w.saveCommands()
	case "command_remove":
		w.removeCommandRow(b.GoPointer())
	}
}

func (w *window) switched(s gtk.Switch, on bool) bool {
	if w.loading {
		return false
	}
	if s.GetName() == "this_switch" {
		w.thisSwitch.SetSensitive(false)
		w.background(func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return connect.SetOn(ctx, on)
		}, func(err error) {
			w.thisSwitch.SetSensitive(true)
			if err != nil {
				w.fail(i18n.Tf("Mobile Link did not start: %v", err))
			}
			go w.refresh()
		})
		return true // the state follows the service
	}
	w.call(nil, "SetSetting", w.selected, s.GetName(), on)
	return false
}

// --- the pages ---

func (w *window) device() (connect.DeviceView, bool) {
	for _, d := range w.devices {
		if d.ID == w.selected {
			return d, true
		}
	}
	return connect.DeviceView{}, false
}

// show fills the page shown.
func (w *window) show() {
	on := connect.On()
	switch {
	case w.running:
		w.sideThisSub.SetText(i18n.T("Visible to your devices"))
		w.thisDesc.SetText(i18n.T("Devices running KDE Connect, GSConnect or Valent find this computer on this network and the tailnet, and pair with it."))
	case on:
		w.sideThisSub.SetText(i18n.T("Starting…"))
		w.thisDesc.SetText(i18n.T("The service is starting."))
	default:
		w.sideThisSub.SetText(i18n.T("Off"))
		w.thisDesc.SetText(i18n.T("Share the clipboard and files with your phone, see its notifications and battery here, and use it as a touchpad or a remote."))
	}
	w.loading = true
	w.thisSwitch.SetActive(on || w.running)
	w.thisSwitch.SetState(w.running)
	w.loading = false
	w.sideFind.SetSensitive(w.running)
	w.commandsNote.SetText(i18n.T("A device lists them and runs one with a tap, here, as you."))

	if w.selected == "this" || w.selected == "add" {
		return
	}
	d, ok := w.device()
	if !ok {
		w.deviceTitle.SetText("")
		w.deviceState.SetText(i18n.T("The device is gone."))
		w.deviceAddr.SetText("")
		for _, wd := range []*gtk.Widget{&w.deviceUnpair.Widget, &w.devicePair.Widget, &w.deviceKeyBox.Widget,
			&w.deviceActions.Widget, &w.deviceAllow.Widget} {
			wd.SetVisible(false)
		}
		return
	}
	w.deviceTitle.SetText("[" + strings.ToUpper(d.Name) + "]")
	addr := ""
	if d.Addr != nil {
		addr = d.Addr.String()
	}
	w.deviceAddr.SetText(addr)
	paired := d.Pair == kdeconnect.Paired
	ready := paired && d.Reachable
	state := connect.StateText(d)
	w.devicePair.SetVisible(!paired && d.Pair != kdeconnect.Requested)
	w.deviceUnpair.SetVisible(paired || d.Pair != kdeconnect.NotPaired)
	w.deviceKeyBox.SetVisible(d.Key != "")
	switch d.Pair {
	case kdeconnect.RequestedByPeer:
		w.devicePair.SetLabel(i18n.T("Pair"))
		w.deviceUnpair.SetLabel(i18n.T("Refuse"))
		w.deviceKeyText.SetText(i18n.Tf("%s asks to pair. Pair only if it shows this key:", d.Name))
	case kdeconnect.Requested:
		w.deviceUnpair.SetLabel(i18n.T("Cancel"))
		w.deviceKeyText.SetText(i18n.Tf("Accept on %s if it shows this key:", d.Name))
	case kdeconnect.Paired:
		w.deviceUnpair.SetLabel(i18n.T("Unpair"))
	default:
		w.devicePair.SetLabel(i18n.T("Pair"))
		state = i18n.T("Not paired: pair to share with it.")
	}
	w.deviceKey.SetText(d.Key)
	if paired && !d.Reachable {
		state += " — " + i18n.T("it connects again when it is on this network or the tailnet")
	}
	w.deviceState.SetText(state)
	w.deviceActions.SetVisible(ready)
	w.deviceAllow.SetVisible(paired)
	w.devicePing.SetSensitive(d.Takes("kdeconnect.ping"))
	w.deviceRing.SetSensitive(d.Takes("kdeconnect.findmyphone.request"))
	w.deviceFiles.SetSensitive(d.Takes("kdeconnect.share.request"))
	w.loading = true
	for _, s := range []*gtk.Switch{&w.setClipboard, &w.setNotifications, &w.setInput, &w.setCmds} {
		on := d.Settings[s.GetName()]
		s.SetActive(on)
		s.SetState(on)
	}
	w.loading = false
}

// --- sending ---

func (w *window) sendText() {
	text := strings.TrimSpace(w.deviceText.GetText())
	if text == "" {
		return
	}
	w.call(func(err error) {
		if err == nil {
			w.deviceText.SetText("")
		}
	}, "ShareText", w.selected, text)
}

func (w *window) chooseFiles() {
	if w.dialog != nil {
		return
	}
	d := gtk.NewFileDialog()
	d.SetTitle(i18n.T("Send files"))
	w.dialog = d
	d.OpenMultiple(&w.win.Window, nil, &w.onFiles, 0)
}

func (w *window) filesChosen(result uintptr) {
	d := w.dialog
	w.dialog = nil
	if d == nil {
		return
	}
	defer d.Unref()
	files, err := d.OpenMultipleFinish(&gio.AsyncResultBase{Ptr: result})
	if err != nil || files == nil {
		return // cancelled
	}
	var paths []string
	for i := range files.GetNItems() {
		f := &gio.FileBase{Ptr: files.GetItem(i)}
		if p := f.GetPath(); p != "" {
			paths = append(paths, p)
		}
		(&gobject.Object{Ptr: f.GoPointer()}).Unref()
	}
	(&gobject.Object{Ptr: files.GoPointer()}).Unref()
	if len(paths) > 0 && !w.closed {
		w.call(nil, "Share", w.selected, paths)
	}
}

func (w *window) add() {
	addr := strings.TrimSpace(w.addAddr.GetText())
	if addr == "" {
		return
	}
	w.addError.SetText("")
	w.call(func(err error) {
		if err != nil {
			w.addError.SetText(errText(err))
			return
		}
		w.addAddr.SetText("")
		w.addError.SetText(i18n.Tf("Asked %s to connect.", addr))
	}, "AddAddress", addr)
}

// --- the commands ---

func (w *window) loadCommands() {
	var cmds []connect.Command
	w.background(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var js string
		err := connect.Call(ctx, "Commands").Store(&js)
		if err == nil {
			json.Unmarshal([]byte(js), &cmds)
		}
		return err
	}, func(err error) {
		gtkx.Clear(&w.commands)
		w.cmdRows = nil
		for _, c := range cmds {
			w.addCommandRow(c)
		}
	})
}

func (w *window) addCommandRow(c connect.Command) {
	row := gtk.NewBox(gtk.OrientationHorizontalValue, 8)
	row.AddCssClass("row")
	name := gtk.NewEntry()
	name.SetPlaceholderText(i18n.T("Name"))
	name.SetText(c.Name)
	name.SetWidthChars(14)
	gtkx.Append(row, &name.Widget)
	cmd := gtk.NewEntry()
	cmd.SetPlaceholderText(i18n.T("What it runs: systemctl suspend"))
	cmd.SetText(c.Command)
	cmd.SetHexpand(true)
	cmd.AddCssClass("connect-command")
	gtkx.Append(row, &cmd.Widget)
	rm := gtk.NewButtonFromIconName("user-trash-symbolic")
	rm.AddCssClass("plain")
	rm.SetName("command_remove")
	rm.SetTooltipText(i18n.T("Remove"))
	rm.ConnectClicked(&w.onClick)
	gtkx.Append(row, &rm.Widget)
	gtkx.Append(&w.commands, &row.Widget)
	w.cmdRows = append(w.cmdRows, cmdRow{key: c.Key, name: name, command: cmd, remove: rm})
	if c.Name == "" {
		name.GrabFocus()
	}
}

func (w *window) removeCommandRow(btn uintptr) {
	for i, r := range w.cmdRows {
		if r.remove.GoPointer() == btn {
			if p := r.remove.GetParent(); p != nil {
				w.commands.Remove(p)
			}
			w.cmdRows = append(w.cmdRows[:i], w.cmdRows[i+1:]...)
			return
		}
	}
}

func (w *window) saveCommands() {
	cmds := []connect.Command{}
	for _, r := range w.cmdRows {
		c := connect.Command{Key: r.key, Name: strings.TrimSpace(r.name.GetText()), Command: strings.TrimSpace(r.command.GetText())}
		if c.Name == "" && c.Command == "" {
			continue
		}
		cmds = append(cmds, c)
	}
	js, _ := json.Marshal(cmds)
	w.call(func(err error) {
		if err == nil {
			w.loadCommands()
		}
	}, "SetCommands", string(js))
}
