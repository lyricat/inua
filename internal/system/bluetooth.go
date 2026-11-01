package system

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const bluezName = "org.bluez"

// BluetoothState is the first adapter's state.
type BluetoothState struct {
	Available   bool // BlueZ is running and has an adapter
	Powered     bool
	Discovering bool     // searching for devices nearby
	Name        string   // the adapter's alias: what other devices see
	Connected   []string // aliases of connected devices, sorted
	// Devices are the adapter's paired devices and those found nearby,
	// paired first, then by name.
	Devices []BluetoothDevice
}

// BluetoothDevice is a device BlueZ knows: paired, or found nearby.
type BluetoothDevice struct {
	Path      string // BlueZ's object
	Address   string
	Name      string // the alias, or "" for a device found without a name
	Icon      string // BlueZ's freedesktop icon name ("audio-headset"), or ""
	Paired    bool
	Trusted   bool
	Connected bool
	// Battery is the level in percent, from BlueZ or else from the UPower
	// device with the device's address (a HID device's, which the kernel
	// reports); -1 for none reported.
	Battery int
}

// BluetoothPrompt is what BlueZ asks the user while pairing, through the
// shell's agent.
type BluetoothPrompt struct {
	Kind   PromptKind
	Device BluetoothDevice
	// Code is the passkey or PIN to show (PromptShow, PromptConfirm):
	// six digits for a passkey.
	Code string
	// Typed is how many digits of Code were typed on the device so far,
	// while PromptShow shows a passkey; -1 when BlueZ does not say.
	Typed int
}

// PromptKind is what a prompt asks.
type PromptKind int

const (
	// PromptShow shows Code, to type on the device (a keyboard); it needs
	// no answer and ends when pairing does.
	PromptShow PromptKind = iota
	// PromptConfirm asks whether the device shows Code too (a phone).
	PromptConfirm
	// PromptPIN asks for a PIN code (an older device), any text.
	PromptPIN
	// PromptPasskey asks for the six-digit number the device shows.
	PromptPasskey
	// PromptAuthorize asks whether a device may pair, or use a service
	// while not paired, without a code.
	PromptAuthorize
)

// BluetoothAnswer is the user's answer to a prompt: OK false rejects it.
type BluetoothAnswer struct {
	OK   bool
	Text string // the PIN or passkey typed, for PromptPIN and PromptPasskey
}

// Asker shows a prompt and returns the answer, or OK false when ctx ends
// first (BlueZ cancelled, or the request timed out). A PromptShow is shown
// until ctx ends; its answer is not used.
type Asker func(ctx context.Context, p BluetoothPrompt) BluetoothAnswer

// Bluetooth follows BlueZ, and answers its pairing requests while an
// Asker is set.
type Bluetooth struct {
	src source

	mu      sync.Mutex
	adapter dbus.ObjectPath
	devices map[dbus.ObjectPath]BluetoothDevice

	agent agent
}

// agentPath is where the shell's pairing agent is exported.
const agentPath = dbus.ObjectPath("/io/github/lyricat/inua/bluetooth/agent")

// Long calls: connecting and pairing wait for the device, which may need
// the user to do something on it.
const (
	connectLimit = 30 * time.Second
	pairLimit    = 90 * time.Second
)

// upowerDevices is where UPower's devices are.
const upowerDevices = dbus.ObjectPath("/org/freedesktop/UPower/devices")

func NewBluetooth(dial Dialer) *Bluetooth {
	b := &Bluetooth{src: source{dial: dial, name: bluezName, ignore: unshown, matches: [][]dbus.MatchOption{
		{dbus.WithMatchSender(bluezName), dbus.WithMatchInterface(propsIface)},
		{dbus.WithMatchSender(bluezName), dbus.WithMatchInterface("org.freedesktop.DBus.ObjectManager")},
		// UPower's devices, for the levels BlueZ does not have.
		{dbus.WithMatchSender(upowerName), dbus.WithMatchPathNamespace(upowerDevices), dbus.WithMatchInterface(propsIface)},
		{dbus.WithMatchSender(upowerName), dbus.WithMatchInterface(upowerName)},
		{dbus.WithMatchSender("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"), dbus.WithMatchArg(0, upowerName)},
	}}}
	b.agent.b = b
	return b
}

// unshown reports a change nothing shown depends on: a device's signal
// alone, which changes every moment while searching, or a UPower device's
// properties other than its level (a laptop battery's every few seconds).
func unshown(sig *dbus.Signal) bool {
	if sig.Name != propsIface+".PropertiesChanged" || len(sig.Body) < 3 {
		return false
	}
	changed, _ := sig.Body[1].(map[string]dbus.Variant)
	invalidated, _ := sig.Body[2].([]string)
	if iface, _ := sig.Body[0].(string); iface == upowerName+".Device" {
		_, level := changed["Percentage"]
		_, serial := changed["Serial"]
		return !level && !serial && !slices.Contains(invalidated, "Percentage") && !slices.Contains(invalidated, "Serial")
	}
	for name := range changed {
		if name != "RSSI" && name != "TxPower" && name != "ManufacturerData" && name != "ServiceData" {
			return false
		}
	}
	return len(changed) > 0 && len(invalidated) == 0
}

// SetAsker sets who answers pairing requests; nil rejects them. The agent
// registers with BlueZ once Run has connected.
func (b *Bluetooth) SetAsker(ask Asker) {
	b.agent.mu.Lock()
	b.agent.ask = ask
	b.agent.mu.Unlock()
}

// Run reports the state until ctx ends.
func (b *Bluetooth) Run(ctx context.Context, onChange func(BluetoothState)) {
	e := &emitter[BluetoothState]{f: onChange}
	b.src.run(ctx, func(ctx context.Context, conn *dbus.Conn) {
		b.agent.register(ctx, conn)
		s, adapter, devices := readBluetooth(ctx, conn)
		b.mu.Lock()
		b.adapter, b.devices = adapter, devices
		b.mu.Unlock()
		e.emit(s)
	})
}

func readBluetooth(ctx context.Context, conn *dbus.Conn) (BluetoothState, dbus.ObjectPath, map[dbus.ObjectPath]BluetoothDevice) {
	ctx, cancel := context.WithTimeout(ctx, callLimit)
	defer cancel()
	var objects map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	err := conn.Object(bluezName, "/").CallWithContext(ctx, "org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&objects)
	if err != nil {
		return BluetoothState{}, "", nil
	}
	paths := make([]dbus.ObjectPath, 0, len(objects))
	for p := range objects {
		paths = append(paths, p)
	}
	slices.Sort(paths)

	var s BluetoothState
	var adapter dbus.ObjectPath
	for _, p := range paths {
		if a, ok := objects[p][bluezName+".Adapter1"]; ok && adapter == "" {
			adapter = p
			s.Available = true
			s.Powered = value[bool](a, "Powered")
			s.Discovering = value[bool](a, "Discovering")
			s.Name = value[string](a, "Alias")
		}
	}
	devices := map[dbus.ObjectPath]BluetoothDevice{}
	for _, p := range paths {
		d, ok := objects[p][bluezName+".Device1"]
		if !ok {
			continue
		}
		if owner := value[dbus.ObjectPath](d, "Adapter"); owner != "" && owner != adapter {
			continue // another adapter's
		}
		dev := BluetoothDevice{
			Path:      string(p),
			Address:   value[string](d, "Address"),
			Icon:      value[string](d, "Icon"),
			Paired:    value[bool](d, "Paired"),
			Trusted:   value[bool](d, "Trusted"),
			Connected: value[bool](d, "Connected"),
			Battery:   -1,
		}
		// A device found without a name has its address for an alias.
		if _, named := d["Name"]; named || dev.Paired {
			dev.Name = value[string](d, "Alias")
		}
		if bat, ok := objects[p][bluezName+".Battery1"]; ok {
			dev.Battery = int(value[byte](bat, "Percentage"))
		}
		devices[p] = dev
		s.Devices = append(s.Devices, dev)
		if dev.Connected {
			s.Connected = append(s.Connected, value[string](d, "Alias"))
		}
	}
	// The connected devices without BlueZ's level take UPower's.
	var levels map[string]int
	for i, dev := range s.Devices {
		if !dev.Connected || dev.Battery >= 0 {
			continue
		}
		if levels == nil {
			levels = upowerLevels(ctx, conn)
		}
		if l, ok := levels[strings.ToLower(dev.Address)]; ok {
			s.Devices[i].Battery = l
			dev.Battery = l
			devices[dbus.ObjectPath(dev.Path)] = dev
		}
	}
	slices.Sort(s.Connected)
	slices.SortStableFunc(s.Devices, func(a, b BluetoothDevice) int {
		if a.Paired != b.Paired {
			if a.Paired {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return s, adapter, devices
}

// upowerLevels are the levels of UPower's devices by serial, lower case:
// a Bluetooth HID device's serial is its address. Empty without UPower.
func upowerLevels(ctx context.Context, conn *dbus.Conn) map[string]int {
	levels := map[string]int{}
	var paths []dbus.ObjectPath
	if err := conn.Object(upowerName, "/org/freedesktop/UPower").CallWithContext(ctx, upowerName+".EnumerateDevices", 0).Store(&paths); err != nil {
		return levels
	}
	for _, p := range paths {
		props, err := getAll(ctx, conn.Object(upowerName, p), upowerName+".Device")
		if err != nil {
			continue
		}
		serial := strings.ToLower(value[string](props, "Serial"))
		if _, ok := props["Percentage"]; !ok || serial == "" {
			continue
		}
		levels[serial] = int(math.Round(value[float64](props, "Percentage")))
	}
	return levels
}

// adapterPath is the adapter, or an error without one.
func (b *Bluetooth) adapterPath() (dbus.ObjectPath, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.adapter == "" {
		return "", errors.New("system: no Bluetooth adapter")
	}
	return b.adapter, nil
}

// SetPowered turns the adapter on or off.
func (b *Bluetooth) SetPowered(on bool) error {
	adapter, err := b.adapterPath()
	if err != nil {
		return err
	}
	return b.src.setProp(bluezName, adapter, bluezName+".Adapter1", "Powered", on)
}

// SetDiscovering starts or stops searching for devices nearby, and makes
// the computer visible to them meanwhile. BlueZ ends a search the shell
// started when the shell goes.
func (b *Bluetooth) SetDiscovering(on bool) error {
	adapter, err := b.adapterPath()
	if err != nil {
		return err
	}
	if err := b.src.setProp(bluezName, adapter, bluezName+".Adapter1", "Discoverable", on); err != nil {
		log.Printf("system: bluetooth: discoverable: %v", err)
	}
	method := "StopDiscovery"
	if on {
		method = "StartDiscovery"
	}
	err = bluezError(b.src.call(bluezName, adapter, bluezName+".Adapter1."+method))
	var bt *BluetoothError
	if errors.As(err, &bt) && (bt.Name == bluezName+".Error.InProgress" || !on) {
		return nil // already searching; or not searching for the shell, so nothing to stop
	}
	return err
}

// device is the device at path, if BlueZ has it.
func (b *Bluetooth) device(path string) (BluetoothDevice, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	d, ok := b.devices[dbus.ObjectPath(path)]
	if !ok {
		return d, fmt.Errorf("system: no Bluetooth device %s", path)
	}
	return d, nil
}

// Connect connects a paired device, waiting for it.
func (b *Bluetooth) Connect(path string) error {
	if _, err := b.device(path); err != nil {
		return err
	}
	return b.longCall(dbus.ObjectPath(path), "Connect", connectLimit)
}

// Disconnect disconnects a device.
func (b *Bluetooth) Disconnect(path string) error {
	if _, err := b.device(path); err != nil {
		return err
	}
	return b.longCall(dbus.ObjectPath(path), "Disconnect", connectLimit)
}

// Pair pairs a device found nearby, then trusts it, so it connects again
// by itself, and connects it. BlueZ asks the agent for a code meanwhile.
func (b *Bluetooth) Pair(path string) error {
	d, err := b.device(path)
	if err != nil {
		return err
	}
	p := dbus.ObjectPath(path)
	if !d.Paired {
		err := b.longCall(p, "Pair", pairLimit)
		b.agent.cancelPrompt() // a code shown is done with
		if err != nil {
			return err
		}
	}
	if err := b.src.setProp(bluezName, p, bluezName+".Device1", "Trusted", true); err != nil {
		return err
	}
	return b.longCall(p, "Connect", connectLimit)
}

// CancelPair stops pairing a device.
func (b *Bluetooth) CancelPair(path string) error {
	return b.src.call(bluezName, dbus.ObjectPath(path), bluezName+".Device1.CancelPairing")
}

// Forget removes a device: it is no longer paired, and is found again
// only by searching.
func (b *Bluetooth) Forget(path string) error {
	adapter, err := b.adapterPath()
	if err != nil {
		return err
	}
	return b.src.call(bluezName, adapter, bluezName+".Adapter1.RemoveDevice", dbus.ObjectPath(path))
}

// longCall calls a Device1 method that waits for the device.
func (b *Bluetooth) longCall(path dbus.ObjectPath, method string, limit time.Duration) error {
	obj, err := b.src.object(bluezName, path)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	return bluezError(obj.CallWithContext(ctx, bluezName+".Device1."+method, 0).Err)
}

// BluetoothError is an error BlueZ returned: its name
// ("org.bluez.Error.AuthenticationFailed") and message.
type BluetoothError struct {
	Name, Message string
}

func (e *BluetoothError) Error() string { return e.Message }

// bluezError keeps what BlueZ said, without D-Bus's wrapping.
func bluezError(err error) error {
	var dbusErr dbus.Error
	if !errors.As(err, &dbusErr) {
		return err
	}
	msg := dbusErr.Name
	if len(dbusErr.Body) > 0 {
		if s, ok := dbusErr.Body[0].(string); ok && s != "" {
			msg = s
		}
	}
	return &BluetoothError{Name: dbusErr.Name, Message: msg}
}

// --- the pairing agent ---

// agent is org.bluez.Agent1: BlueZ calls it while pairing to show a code
// or ask for one. It asks through the Asker, one prompt at a time.
type agent struct {
	b *Bluetooth

	mu     sync.Mutex
	ask    Asker
	owner  string             // BlueZ's bus name the agent is registered with
	conn   *dbus.Conn         // the connection it is exported on
	cancel context.CancelFunc // ends the prompt shown
}

// register exports the agent on conn and registers it with BlueZ as the
// default one; again when BlueZ restarts.
func (a *agent) register(ctx context.Context, conn *dbus.Conn) {
	ctx, cancel := context.WithTimeout(ctx, callLimit)
	defer cancel()
	var owner string
	if err := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, bluezName).Store(&owner); err != nil {
		return // BlueZ is not running; the next read tries again
	}
	a.mu.Lock()
	same := a.owner == owner && a.conn == conn
	a.mu.Unlock()
	if same {
		return
	}
	if err := conn.Export(agentMethods{a}, agentPath, bluezName+".Agent1"); err != nil {
		log.Printf("system: bluetooth agent: %v", err)
		return
	}
	// Once for this BlueZ, whether it takes the agent or not.
	a.mu.Lock()
	a.owner, a.conn = owner, conn
	a.mu.Unlock()
	manager := conn.Object(bluezName, "/org/bluez")
	if err := manager.CallWithContext(ctx, bluezName+".AgentManager1.RegisterAgent", 0, agentPath, "KeyboardDisplay").Err; err != nil {
		log.Printf("system: bluetooth agent: %v", err)
		return
	}
	if err := manager.CallWithContext(ctx, bluezName+".AgentManager1.RequestDefaultAgent", 0, agentPath).Err; err != nil {
		log.Printf("system: bluetooth agent: default: %v", err)
	}
}

// prompt asks one thing, ending the prompt before it: BlueZ asks one at a
// time, so a new one means the old one is over. answered is false when
// the prompt ended without an answer.
func (a *agent) prompt(path dbus.ObjectPath, p BluetoothPrompt) (ans BluetoothAnswer, answered bool) {
	a.mu.Lock()
	ask := a.ask
	if a.cancel != nil {
		a.cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), pairLimit)
	a.cancel = cancel
	a.mu.Unlock()
	defer cancel()
	if ask == nil {
		return BluetoothAnswer{}, true // rejected: nobody to ask
	}
	if d, err := a.b.device(string(path)); err == nil {
		p.Device = d
	} else {
		p.Device = BluetoothDevice{Path: string(path), Battery: -1}
	}
	ans = ask(ctx, p)
	return ans, ctx.Err() == nil
}

// show shows a code until pairing ends, without waiting for it.
func (a *agent) show(path dbus.ObjectPath, p BluetoothPrompt) {
	go a.prompt(path, p)
}

func (a *agent) cancelPrompt() {
	a.mu.Lock()
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	a.mu.Unlock()
}

var (
	errRejected = dbus.NewError(bluezName+".Error.Rejected", []any{"Rejected"})
	errCanceled = dbus.NewError(bluezName+".Error.Canceled", []any{"Canceled"})
)

// reply is the agent's reply for an answer: rejected, or cancelled when
// the prompt went away without one.
func reply(a BluetoothAnswer, answered bool) *dbus.Error {
	switch {
	case !answered:
		return errCanceled
	case !a.OK:
		return errRejected
	}
	return nil
}

// agentMethods are the methods BlueZ calls, as godbus exports them; each
// call runs on its own goroutine.
type agentMethods struct{ a *agent }

func (m agentMethods) Release() *dbus.Error {
	m.a.cancelPrompt()
	m.a.mu.Lock()
	m.a.owner = ""
	m.a.mu.Unlock()
	return nil
}

func (m agentMethods) RequestPinCode(device dbus.ObjectPath) (string, *dbus.Error) {
	a, ok := m.a.prompt(device, BluetoothPrompt{Kind: PromptPIN, Typed: -1})
	if err := reply(a, ok); err != nil {
		return "", err
	}
	return a.Text, nil
}

func (m agentMethods) DisplayPinCode(device dbus.ObjectPath, pincode string) *dbus.Error {
	m.a.show(device, BluetoothPrompt{Kind: PromptShow, Code: pincode, Typed: -1})
	return nil
}

func (m agentMethods) RequestPasskey(device dbus.ObjectPath) (uint32, *dbus.Error) {
	a, ok := m.a.prompt(device, BluetoothPrompt{Kind: PromptPasskey, Typed: -1})
	if err := reply(a, ok); err != nil {
		return 0, err
	}
	var n uint32
	if _, err := fmt.Sscan(a.Text, &n); err != nil || n > 999999 {
		return 0, errRejected
	}
	return n, nil
}

func (m agentMethods) DisplayPasskey(device dbus.ObjectPath, passkey uint32, entered uint16) *dbus.Error {
	m.a.show(device, BluetoothPrompt{Kind: PromptShow, Code: fmt.Sprintf("%06d", passkey), Typed: int(entered)})
	return nil
}

func (m agentMethods) RequestConfirmation(device dbus.ObjectPath, passkey uint32) *dbus.Error {
	return reply(m.a.prompt(device, BluetoothPrompt{Kind: PromptConfirm, Code: fmt.Sprintf("%06d", passkey), Typed: -1}))
}

func (m agentMethods) RequestAuthorization(device dbus.ObjectPath) *dbus.Error {
	return reply(m.a.prompt(device, BluetoothPrompt{Kind: PromptAuthorize, Typed: -1}))
}

// AuthorizeService lets a paired device use a service (audio, input);
// BlueZ asks only for devices it does not trust.
func (m agentMethods) AuthorizeService(device dbus.ObjectPath, uuid string) *dbus.Error {
	if d, err := m.a.b.device(string(device)); err == nil && d.Paired {
		return nil
	}
	return reply(m.a.prompt(device, BluetoothPrompt{Kind: PromptAuthorize, Typed: -1}))
}

func (m agentMethods) Cancel() *dbus.Error {
	m.a.cancelPrompt()
	return nil
}
