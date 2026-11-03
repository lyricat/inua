// Package connect is Mobile Link (docs/mobile-link.md): phones and computers
// running KDE Connect, GSConnect or Valent paired with this computer. Its
// service (inua-connect --service) speaks the protocol (internal/kdeconnect)
// and does what devices ask: their batteries, pings and rings, the
// clipboard both ways, files and links shared, their notifications here
// and this computer's there, the players here driven from them, the
// pointer and keyboard, and commands. The window (internal/connectui)
// asks it over D-Bus; it imports no GTK.
package connect

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/apps/store"
	"github.com/lyricat/inua/internal/dbusexport"
	"github.com/lyricat/inua/internal/i18n"
	"github.com/lyricat/inua/internal/kdeconnect"
	"github.com/lyricat/inua/internal/system"
)

// The service's name and object on the session bus, and the app's ID
// (the window's, which owns that name).
const (
	BusName   = "io.github.lyricat.Inua.MobileLink.Service"
	BusPath   = dbus.ObjectPath("/io/github/lyricat/Inua/MobileLink/Service")
	BusIface  = "io.github.lyricat.Inua.MobileLink.Service"
	DesktopID = "io.github.lyricat.Inua.MobileLink"
)

// The packets.
const (
	typeBattery        = "kdeconnect.battery"
	typePing           = "kdeconnect.ping"
	typeFind           = "kdeconnect.findmyphone.request"
	typeClipboard      = "kdeconnect.clipboard"
	typeClipConnect    = "kdeconnect.clipboard.connect"
	typeShare          = "kdeconnect.share.request"
	typeShareUpdate    = "kdeconnect.share.request.update"
	typeNote           = "kdeconnect.notification"
	typeNoteRequest    = "kdeconnect.notification.request"
	typeNoteReply      = "kdeconnect.notification.reply"
	typeNoteAction     = "kdeconnect.notification.action"
	typeMpris          = "kdeconnect.mpris"
	typeMprisRequest   = "kdeconnect.mpris.request"
	typeMouse          = "kdeconnect.mousepad.request"
	typeKeyboardState  = "kdeconnect.mousepad.keyboardstate"
	typeCommand        = "kdeconnect.runcommand"
	typeCommandRequest = "kdeconnect.runcommand.request"
)

// incoming and outgoing are the packets this computer takes and sends.
var (
	incoming = []string{typeBattery, typePing, typeFind, typeClipboard, typeClipConnect, typeShare, typeShareUpdate,
		typeNote, typeNoteRequest, typeMprisRequest, typeMouse, typeCommandRequest}
	outgoing = []string{typeBattery, typePing, typeFind, typeClipboard, typeClipConnect, typeShare, typeShareUpdate,
		typeNote, typeNoteRequest, typeNoteReply, typeNoteAction, typeMpris, typeKeyboardState, typeCommand}
)

// The settings each paired device has; each is on unless set to "off".
const (
	SetClipboard     = "clipboard"     // the clipboard is shared both ways
	SetNotifications = "notifications" // this computer's notifications show there
	SetInput         = "input"         // it moves the pointer and types here
	SetCommands      = "commands"      // it runs the commands set up here
)

// Dir is where the service keeps its identity, devices and commands.
func Dir() string { return filepath.Join(store.Dir(), "connect") }

// Battery is a device's battery, as it last said.
type Battery struct {
	Charge   int  `json:"charge"`
	Charging bool `json:"charging"`
}

// DeviceView is a device as the window shows it.
type DeviceView struct {
	kdeconnect.Device
	Battery  *Battery        `json:"battery,omitempty"`
	Settings map[string]bool `json:"settings,omitempty"`
}

// Service is the protocol's end here, and what the devices are given.
type Service struct {
	core  *kdeconnect.Core
	bus   *dbus.Conn
	notes *notes
	ctx   context.Context

	mu        sync.Mutex
	batteries map[string]*Battery
	ready     map[string]bool // paired and reachable: was greeted
	pairNotes map[string]uint32
	myBattery system.BatteryState

	clip   *clipboardSync
	phone  *phoneNotes
	sender *noteForwarder
	media  *mediaControl
	input  *remoteInput
	runner *commands
	shares *shares
	watch  bool
}

// New makes the service; Run starts it.
func New(name string) (*Service, error) { return newService(name, true, nil) }

// newService is New, the core's configuration changed by tweak; without
// watch, this computer's clipboard, notifications and battery are not
// followed (tests).
func newService(name string, watch bool, tweak func(*kdeconnect.Config)) (*Service, error) {
	s := &Service{batteries: map[string]*Battery{}, ready: map[string]bool{}, pairNotes: map[string]uint32{}, watch: watch}
	cfg := kdeconnect.Config{
		Dir:      Dir(),
		Name:     name,
		Type:     deviceType(),
		Incoming: incoming,
		Outgoing: outgoing,
		Ports:    kdeconnect.DefaultPorts,
		Peers:    tailnetPeers,
		Changed:  s.changed,
		Packet:   s.packet,
	}
	if tweak != nil {
		tweak(&cfg)
	}
	core, err := kdeconnect.New(cfg)
	if err != nil {
		return nil, err
	}
	s.core = core
	s.clip = newClipboardSync(s)
	return s, nil
}

// ErrRunning is a service already on the bus.
var ErrRunning = errors.New("connect: the service is running already")

// Run serves until ctx ends.
func (s *Service) Run(ctx context.Context) error {
	s.ctx = ctx
	bus, err := dbus.ConnectSessionBus()
	if err != nil {
		return err
	}
	defer bus.Close()
	s.bus = bus
	reply, err := bus.RequestName(BusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return ErrRunning
	}
	s.notes = newNotes(bus)
	s.phone = newPhoneNotes(s)
	s.media = newMediaControl(s)
	s.input = newRemoteInput(s)
	s.runner = newCommands(s)
	s.shares = newShares(s)
	s.sender = newNoteForwarder(s)
	sigs := make(chan *dbus.Signal, 64)
	bus.Signal(sigs)
	go func() {
		for sig := range sigs {
			s.notes.signal(sig)
			s.media.signal(sig)
		}
	}()
	if err := s.core.Start(ctx); err != nil {
		return err
	}
	// Asked once it is all there.
	if err := dbusexport.Export(bus, api{s}, BusPath, BusIface); err != nil {
		return err
	}
	if s.watch {
		go s.clip.watch(ctx)
		go s.sender.run(ctx)
		go s.batteryLoop(ctx)
	}
	<-ctx.Done()
	s.core.Wait()
	s.input.close()
	return nil
}

// deviceType is laptop when this computer has a battery.
func deviceType() string {
	matches, _ := filepath.Glob("/sys/class/power_supply/BAT*")
	if len(matches) > 0 {
		return "laptop"
	}
	return "desktop"
}

// Setting says whether the paired device id has the setting on.
func (s *Service) Setting(id, key string) bool {
	t, ok := s.core.Store().Get(id)
	return ok && t.Settings[key] != "off"
}

// changed follows a device's change: a paired device that comes is
// greeted; a request to pair is shown.
func (s *Service) changed(d kdeconnect.Device) {
	s.mu.Lock()
	ready := d.Reachable && d.Pair == kdeconnect.Paired
	greet := ready && !s.ready[d.ID]
	if ready {
		s.ready[d.ID] = true
	} else {
		delete(s.ready, d.ID)
	}
	if !d.Reachable || d.Pair == kdeconnect.NotPaired {
		delete(s.batteries, d.ID)
	}
	pairNote, asked := s.pairNotes[d.ID]
	if d.Pair != kdeconnect.RequestedByPeer {
		delete(s.pairNotes, d.ID)
	}
	s.mu.Unlock()
	switch {
	case d.Pair == kdeconnect.RequestedByPeer && !asked:
		s.askPair(d)
	case d.Pair != kdeconnect.RequestedByPeer && asked:
		s.notes.Close(pairNote)
	}
	if greet {
		go s.greet(d)
	}
	s.emitChanged()
}

// askPair shows a device's request to pair, with the key both show.
func (s *Service) askPair(d kdeconnect.Device) {
	id := s.notes.Post(Note{
		App:     i18n.T("Mobile Link"),
		Icon:    DeviceIcon(d.Type),
		Title:   i18n.Tf("%s wants to pair", d.Name),
		Body:    i18n.Tf("Pair only if %s shows the key %s.", d.Name, d.Key),
		Actions: []Action{{"accept", i18n.T("Pair")}, {"reject", i18n.T("Refuse")}},
		Urgent:  true,
		Acted: func(key string) {
			switch key {
			case "accept":
				s.core.AcceptPair(d.ID)
			case "reject":
				s.core.Unpair(d.ID)
			}
		},
	})
	s.mu.Lock()
	s.pairNotes[d.ID] = id
	s.mu.Unlock()
}

// greet gives a paired device what it is given on coming: this computer's
// battery, clipboard, players, commands and keyboard, and asks for its
// notifications.
func (s *Service) greet(d kdeconnect.Device) {
	s.sendBattery(d.ID)
	if s.Setting(d.ID, SetClipboard) {
		s.clip.greet(d.ID)
	}
	if d.Takes(typeNoteRequest) {
		s.core.Send(d.ID, kdeconnect.NewPacket(typeNoteRequest, map[string]bool{"request": true}))
	}
	s.core.Send(d.ID, kdeconnect.NewPacket(typeKeyboardState, map[string]bool{"state": s.Setting(d.ID, SetInput)}))
	s.runner.sendList(d.ID)
	s.media.sendList(d.ID)
}

// packet does what a paired device's packet asks.
func (s *Service) packet(d kdeconnect.Device, p kdeconnect.Packet) {
	switch p.Type {
	case typeBattery:
		s.battery(d, p)
	case typePing:
		var b struct{ Message string }
		p.Decode(&b)
		body := b.Message
		if body == "" {
			body = i18n.T("Ping!")
		}
		s.notes.Post(Note{App: d.Name, Icon: DeviceIcon(d.Type), Title: d.Name, Body: body})
	case typeFind:
		s.ring(d)
	case typeClipboard, typeClipConnect:
		if s.Setting(d.ID, SetClipboard) {
			s.clip.received(p)
		}
	case typeShare:
		s.shares.received(d, p)
	case typeNote:
		s.phone.received(d, p)
	case typeNoteRequest:
		s.sender.request(d, p)
	case typeMprisRequest:
		s.media.request(d, p)
	case typeMouse:
		if s.Setting(d.ID, SetInput) {
			s.input.packet(p)
		}
	case typeCommandRequest:
		s.runner.request(d, p)
	}
}

// emitChanged tells the window the devices changed.
func (s *Service) emitChanged() {
	if s.bus != nil {
		s.bus.Emit(BusPath, BusIface+".Changed")
	}
}

// Devices are the devices, as the window shows them.
func (s *Service) Devices() []DeviceView {
	var out []DeviceView
	for _, d := range s.core.Devices() {
		v := DeviceView{Device: d}
		s.mu.Lock()
		if b := s.batteries[d.ID]; b != nil {
			c := *b
			v.Battery = &c
		}
		s.mu.Unlock()
		if d.Pair == kdeconnect.Paired {
			v.Settings = map[string]bool{}
			for _, k := range []string{SetClipboard, SetNotifications, SetInput, SetCommands} {
				v.Settings[k] = s.Setting(d.ID, k)
			}
		}
		out = append(out, v)
	}
	return out
}

// DeviceIcon is the icon for a device of type t.
func DeviceIcon(t string) string {
	switch t {
	case "phone", "tablet", "laptop", "tv":
		return "connect-" + t + "-symbolic"
	}
	return "connect-desktop-symbolic"
}

// tailnetPeers are the tailnet's machines online, to announce to: a
// phone there finds this computer though it is on another network.
func tailnetPeers(ctx context.Context) []net.IP {
	st, err := tailscaleStatus(ctx)
	if err != nil {
		return nil
	}
	var out []net.IP
	for _, p := range st.Peers("") {
		if !p.Online {
			continue
		}
		if ip := net.ParseIP(p.IPv4()); ip != nil {
			out = append(out, ip)
		}
	}
	return out
}

// api is the service on the bus, for the window.
type api struct{ s *Service }

func dbusErr(err error) *dbus.Error {
	if err == nil {
		return nil
	}
	return dbus.MakeFailedError(err)
}

// Devices are the devices, as JSON ([]DeviceView).
func (a api) Devices() (string, *dbus.Error) {
	b, _ := json.Marshal(a.s.Devices())
	return string(b), nil
}

// Info is this computer's name and ID, as JSON.
func (a api) Info() (string, *dbus.Error) {
	name, _ := os.Hostname()
	b, _ := json.Marshal(map[string]string{"id": a.s.core.ID(), "name": name})
	return string(b), nil
}

func (a api) Pair(id string) *dbus.Error   { return dbusErr(a.s.core.RequestPair(id)) }
func (a api) Accept(id string) *dbus.Error { return dbusErr(a.s.core.AcceptPair(id)) }
func (a api) Unpair(id string) *dbus.Error { return dbusErr(a.s.core.Unpair(id)) }

// Ping sends the device a ping.
func (a api) Ping(id string) *dbus.Error {
	return dbusErr(a.s.core.Send(id, kdeconnect.NewPacket(typePing, nil)))
}

// Ring makes the device ring, to be found.
func (a api) Ring(id string) *dbus.Error {
	return dbusErr(a.s.core.Send(id, kdeconnect.NewPacket(typeFind, nil)))
}

// Share sends the device files.
func (a api) Share(id string, paths []string) *dbus.Error {
	return dbusErr(a.s.shares.sendFiles(id, paths))
}

// ShareText sends the device text, or a link.
func (a api) ShareText(id, text string) *dbus.Error {
	return dbusErr(a.s.shares.sendText(id, text))
}

// Refresh announces this computer again.
func (a api) Refresh() *dbus.Error {
	go a.s.core.Announce(a.s.ctx)
	return nil
}

// AddAddress announces this computer to the device at addr, for one the
// broadcast does not reach.
func (a api) AddAddress(addr string) *dbus.Error {
	ips, err := net.LookupIP(strings.TrimSpace(addr))
	if err != nil {
		return dbusErr(err)
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			a.s.core.AnnounceTo(v4)
			return nil
		}
	}
	return dbusErr(errors.New("connect: no IPv4 address for " + addr))
}

// SetSetting turns a paired device's setting on or off.
func (a api) SetSetting(id, key string, on bool) *dbus.Error {
	v := "on"
	if !on {
		v = "off"
	}
	err := a.s.core.Store().Update(id, func(t *kdeconnect.Trusted) {
		if t.Settings == nil {
			t.Settings = map[string]string{}
		}
		t.Settings[key] = v
	})
	if err == nil && key == SetInput {
		a.s.core.Send(id, kdeconnect.NewPacket(typeKeyboardState, map[string]bool{"state": on}))
	}
	a.s.emitChanged()
	return dbusErr(err)
}

// Commands are the commands devices may run, as JSON ([]Command).
func (a api) Commands() (string, *dbus.Error) {
	b, _ := json.Marshal(a.s.runner.list())
	return string(b), nil
}

// SetCommands replaces the commands, from JSON ([]Command).
func (a api) SetCommands(js string) *dbus.Error {
	var cmds []Command
	if err := json.Unmarshal([]byte(js), &cmds); err != nil {
		return dbusErr(err)
	}
	return dbusErr(a.s.runner.set(cmds))
}

// Clipboard is the selection copied here, from the clipboard's watcher.
func (a api) Clipboard(text string) *dbus.Error {
	a.s.clip.copied(text)
	return nil
}

// Reply answers a device's notification, by its reply ID.
func (a api) Reply(id, replyID, message string) *dbus.Error {
	return dbusErr(a.s.core.Send(id, kdeconnect.NewPacket(typeNoteReply, map[string]string{"requestReplyId": replyID, "message": message})))
}

// Call asks the service, a method of BusIface with args, from another
// process: the window and the command line.
func Call(ctx context.Context, method string, args ...any) *dbus.Call {
	conn, err := dbus.SessionBus()
	if err != nil {
		return &dbus.Call{Err: err}
	}
	return conn.Object(BusName, BusPath).CallWithContext(ctx, BusIface+"."+method, 0, args...)
}

// StateText says where a device is: paired or not, there or away, its
// battery.
func StateText(d DeviceView) string {
	var s string
	switch {
	case d.Pair == kdeconnect.RequestedByPeer:
		s = i18n.T("Wants to pair")
	case d.Pair == kdeconnect.Requested:
		s = i18n.T("Pairing…")
	case d.Pair == kdeconnect.Paired && d.Reachable:
		s = i18n.T("Connected")
	case d.Pair == kdeconnect.Paired:
		s = i18n.T("Away")
	default:
		s = i18n.T("Not paired")
	}
	if d.Battery != nil {
		if d.Battery.Charging {
			s += " · " + i18n.Tf("%d%%, charging", d.Battery.Charge)
		} else {
			s += " · " + i18n.Tf("%d%%", d.Battery.Charge)
		}
	}
	return s
}
