package system

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	"github.com/lyricat/inua/internal/dbusexport"
	"github.com/lyricat/inua/internal/dbustest"
)

// fakeWifi is NetworkManager with one Wi-Fi device, its access points and
// saved connections; activating a connection with the password "wrong"
// fails as a network turning it down does.
type fakeWifi struct {
	t    *testing.T
	conn *dbus.Conn
	dev  *prop.Properties

	mu           sync.Mutex
	saved        map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	order        []dbus.ObjectPath
	next         int
	disconnected bool
	scans        int
}

const (
	wifiDev = dbus.ObjectPath("/org/freedesktop/NetworkManager/Devices/1")
	nmSet   = dbus.ObjectPath("/org/freedesktop/NetworkManager/Settings")
)

type stateReason struct{ State, Reason uint32 }

func wifiConn(ssid, keyMgmt string, stamp uint64) map[string]map[string]dbus.Variant {
	s := map[string]map[string]dbus.Variant{
		"connection":      {"id": dbus.MakeVariant(ssid), "type": dbus.MakeVariant("802-11-wireless"), "timestamp": dbus.MakeVariant(stamp)},
		"802-11-wireless": {"ssid": dbus.MakeVariant([]byte(ssid)), "mode": dbus.MakeVariant("infrastructure")},
	}
	if keyMgmt != "" {
		s["802-11-wireless-security"] = map[string]dbus.Variant{"key-mgmt": dbus.MakeVariant(keyMgmt)}
	}
	return s
}

func newFakeWifi(t *testing.T, addr string) *fakeWifi {
	f := &fakeWifi{t: t, conn: service(t, addr, nmName), saved: map[dbus.ObjectPath]map[string]map[string]dbus.Variant{}, next: 10}
	export(t, f.conn, nmPath, prop.Map{nmName: {
		"WirelessEnabled":   {Value: true},
		"PrimaryConnection": {Value: dbus.ObjectPath("/org/freedesktop/NetworkManager/ActiveConnection/1")},
		"Devices":           {Value: []dbus.ObjectPath{wifiDev}},
	}})
	dbusexport.Export(f.conn, fakeNMMethods{f}, nmPath, nmName)
	f.dev = export(t, f.conn, wifiDev, prop.Map{
		nmName + ".Device": {
			"DeviceType":       {Value: uint32(2)},
			"State":            emitted(uint32(100)),
			"StateReason":      emitted(stateReason{100, 0}),
			"ActiveConnection": emitted(dbus.ObjectPath("/org/freedesktop/NetworkManager/ActiveConnection/1")),
			"Ip4Config":        {Value: dbus.ObjectPath("/org/freedesktop/NetworkManager/IP4Config/1")},
		},
		nmName + ".Device.Wireless": {
			"HwAddress": {Value: "AA:BB:CC:DD:EE:FF"},
			"AccessPoints": {Value: []dbus.ObjectPath{
				"/org/freedesktop/NetworkManager/AccessPoint/1", "/org/freedesktop/NetworkManager/AccessPoint/2",
				"/org/freedesktop/NetworkManager/AccessPoint/3", "/org/freedesktop/NetworkManager/AccessPoint/4",
				"/org/freedesktop/NetworkManager/AccessPoint/5",
			}},
		},
	})
	dbusexport.Export(f.conn, fakeWifiDeviceMethods{f}, wifiDev, nmName+".Device")
	dbusexport.Export(f.conn, fakeWirelessMethods{f}, wifiDev, nmName+".Device.Wireless")
	for i, ap := range []struct {
		ssid                  string
		strength              byte
		freq, flags, wpa, rsn uint32
	}{
		{"Home", 80, 5180, 1, 0, keyPSK},
		{"Home", 40, 2412, 1, 0, keyPSK},
		{"Cafe", 60, 2437, 0, 0, 0},
		{"Office", 30, 5200, 1, 0, key8021X},
		{"Guest", 50, 2462, 1, keyPSK, keyPSK | keySAE},
	} {
		export(t, f.conn, dbus.ObjectPath(fmt.Sprintf("/org/freedesktop/NetworkManager/AccessPoint/%d", i+1)), prop.Map{nmName + ".AccessPoint": {
			"Ssid": {Value: []byte(ap.ssid)}, "Strength": {Value: ap.strength}, "Frequency": {Value: ap.freq},
			"Flags": {Value: ap.flags}, "WpaFlags": {Value: ap.wpa}, "RsnFlags": {Value: ap.rsn},
		}})
	}
	export(t, f.conn, "/org/freedesktop/NetworkManager/IP4Config/1", prop.Map{nmName + ".IP4Config": {
		"AddressData": {Value: []map[string]dbus.Variant{{"address": dbus.MakeVariant("192.168.1.20"), "prefix": dbus.MakeVariant(uint32(24))}}},
		"Gateway":     {Value: "192.168.1.1"},
		"NameserverData": {Value: []map[string]dbus.Variant{
			{"address": dbus.MakeVariant("192.168.1.1")}, {"address": dbus.MakeVariant("1.1.1.1")},
		}},
	}})
	f.addActive(1, "/org/freedesktop/NetworkManager/Settings/1", 2)
	dbusexport.Export(f.conn, fakeSettingsMethods{f}, nmSet, nmName+".Settings")
	f.save("/org/freedesktop/NetworkManager/Settings/1", wifiConn("Home", "wpa-psk", 200))
	f.save("/org/freedesktop/NetworkManager/Settings/2", wifiConn("Home", "wpa-psk", 100)) // an older one for the same name
	f.save("/org/freedesktop/NetworkManager/Settings/3", wifiConn("Attic", "", 50))
	f.save("/org/freedesktop/NetworkManager/Settings/4", map[string]map[string]dbus.Variant{
		"connection": {"id": dbus.MakeVariant("Wired connection 1"), "type": dbus.MakeVariant("802-3-ethernet")},
	})
	return f
}

func (f *fakeWifi) addActive(n int, conn dbus.ObjectPath, state uint32) dbus.ObjectPath {
	path := dbus.ObjectPath(fmt.Sprintf("/org/freedesktop/NetworkManager/ActiveConnection/%d", n))
	export(f.t, f.conn, path, prop.Map{nmName + ".Connection.Active": {
		"Connection": {Value: conn}, "State": {Value: state}, "Type": {Value: "802-11-wireless"}, "Id": {Value: ""},
		"SpecificObject": {Value: dbus.ObjectPath("/")},
	}})
	return path
}

func (f *fakeWifi) save(path dbus.ObjectPath, s map[string]map[string]dbus.Variant) {
	f.mu.Lock()
	f.saved[path] = s
	f.order = append(f.order, path)
	f.mu.Unlock()
	dbusexport.Export(f.conn, fakeConnMethods{f, path}, path, nmName+".Settings.Connection")
}

// activate brings a connection up, or fails it when its password is "wrong".
func (f *fakeWifi) activate(conn dbus.ObjectPath) dbus.ObjectPath {
	f.mu.Lock()
	f.next++
	n := f.next
	s := f.saved[conn]
	f.mu.Unlock()
	psk := value[string](s["802-11-wireless-security"], "psk")
	if psk == "wrong" {
		f.dev.SetMust(nmName+".Device", "StateReason", stateReason{120, 7})
		return f.addActive(n, conn, 4)
	}
	path := f.addActive(n, conn, 2)
	f.dev.SetMust(nmName+".Device", "ActiveConnection", path)
	return path
}

type fakeNMMethods struct{ f *fakeWifi }

func (m fakeNMMethods) ActivateConnection(conn, dev, ap dbus.ObjectPath) (dbus.ObjectPath, *dbus.Error) {
	return m.f.activate(conn), nil
}

func (m fakeNMMethods) AddAndActivateConnection(s map[string]map[string]dbus.Variant, dev, ap dbus.ObjectPath) (dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	m.f.mu.Lock()
	m.f.next++
	path := dbus.ObjectPath(fmt.Sprintf("/org/freedesktop/NetworkManager/Settings/%d", m.f.next))
	m.f.mu.Unlock()
	m.f.save(path, s)
	return path, m.f.activate(path), nil
}

type fakeWifiDeviceMethods struct{ f *fakeWifi }

func (m fakeWifiDeviceMethods) Disconnect() *dbus.Error {
	m.f.mu.Lock()
	m.f.disconnected = true
	m.f.mu.Unlock()
	return nil
}

type fakeWirelessMethods struct{ f *fakeWifi }

func (m fakeWirelessMethods) RequestScan(map[string]dbus.Variant) *dbus.Error {
	m.f.mu.Lock()
	defer m.f.mu.Unlock()
	if m.f.scans++; m.f.scans > 1 {
		return dbus.NewError(nmName+".Device.NotAllowed", []any{"Scanning not allowed immediately following previous scan"})
	}
	return nil
}

type fakeSettingsMethods struct{ f *fakeWifi }

func (m fakeSettingsMethods) ListConnections() ([]dbus.ObjectPath, *dbus.Error) {
	m.f.mu.Lock()
	defer m.f.mu.Unlock()
	return append([]dbus.ObjectPath(nil), m.f.order...), nil
}

type fakeConnMethods struct {
	f    *fakeWifi
	path dbus.ObjectPath
}

// GetSettings leaves the secrets out, as NetworkManager does.
func (m fakeConnMethods) GetSettings() (map[string]map[string]dbus.Variant, *dbus.Error) {
	m.f.mu.Lock()
	defer m.f.mu.Unlock()
	out := map[string]map[string]dbus.Variant{}
	for k, v := range m.f.saved[m.path] {
		out[k] = map[string]dbus.Variant{}
		for kk, vv := range v {
			if kk != "psk" && kk != "wep-key0" {
				out[k][kk] = vv
			}
		}
	}
	return out, nil
}

func (m fakeConnMethods) Update(s map[string]map[string]dbus.Variant) *dbus.Error {
	m.f.mu.Lock()
	m.f.saved[m.path] = s
	m.f.mu.Unlock()
	return nil
}

func (m fakeConnMethods) Delete() *dbus.Error {
	m.f.mu.Lock()
	defer m.f.mu.Unlock()
	delete(m.f.saved, m.path)
	for i, p := range m.f.order {
		if p == m.path {
			m.f.order = append(m.f.order[:i], m.f.order[i+1:]...)
			break
		}
	}
	return nil
}

func (f *fakeWifi) savedNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var names []string
	for _, p := range f.order {
		if s := f.saved[p]; value[string](s["connection"], "type") == "802-11-wireless" {
			names = append(names, string(value[[]byte](s["802-11-wireless"], "ssid")))
		}
	}
	return names
}

func (f *fakeWifi) psk(ssid string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.order {
		if s := f.saved[p]; string(value[[]byte](s["802-11-wireless"], "ssid")) == ssid {
			return value[string](s["802-11-wireless-security"], "psk")
		}
	}
	return ""
}

func startWifi(t *testing.T) (*fakeWifi, *Network) {
	addr := dbustest.Start(t)
	f := newFakeWifi(t, addr)
	n := NewNetwork(dialer(addr))
	got := newStates[NetworkState]()
	run(t, func(ctx context.Context) { n.Run(ctx, got.add) })
	got.wait(t, "network", func(s NetworkState) bool { return s.Available })
	return f, n
}

func TestReadWifi(t *testing.T) {
	_, n := startWifi(t)
	l, err := n.ReadWifi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if l.Device != string(wifiDev) || l.Hardware != "AA:BB:CC:DD:EE:FF" || l.Address != "192.168.1.20" || l.Gateway != "192.168.1.1" || !slices.Equal(l.DNS, []string{"192.168.1.1", "1.1.1.1"}) {
		t.Errorf("device %+v", l)
	}
	// One a name: Home's stronger access point and the connection in use;
	// then by strength; the saved one out of range last.
	want := []WifiNetwork{
		{SSID: "Home", Strength: 80, Frequency: 5180, Security: "wpa", InRange: true, Saved: true, Connected: true,
			Conn: "/org/freedesktop/NetworkManager/Settings/1", AP: "/org/freedesktop/NetworkManager/AccessPoint/1"},
		{SSID: "Cafe", Strength: 60, Frequency: 2437, InRange: true, AP: "/org/freedesktop/NetworkManager/AccessPoint/3"},
		{SSID: "Guest", Strength: 50, Frequency: 2462, Security: "wpa", InRange: true, AP: "/org/freedesktop/NetworkManager/AccessPoint/5"},
		{SSID: "Office", Strength: 30, Frequency: 5200, Security: "enterprise", InRange: true, AP: "/org/freedesktop/NetworkManager/AccessPoint/4"},
		{SSID: "Attic", Saved: true, Conn: "/org/freedesktop/NetworkManager/Settings/3"},
	}
	if !reflect.DeepEqual(l.Networks, want) {
		t.Errorf("networks\n got %+v\nwant %+v", l.Networks, want)
	}
}

func TestConnectWifi(t *testing.T) {
	f, n := startWifi(t)
	ctx := context.Background()
	find := func(ssid string) WifiNetwork {
		t.Helper()
		l, err := n.ReadWifi(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, nw := range l.Networks {
			if nw.SSID == ssid {
				return nw
			}
		}
		t.Fatalf("no network %q", ssid)
		return WifiNetwork{}
	}

	// A new network is saved with its password and comes up.
	if err := n.ConnectWifi(ctx, find("Guest"), "secret"); err != nil {
		t.Fatal(err)
	}
	if got := f.psk("Guest"); got != "secret" {
		t.Errorf("Guest's password %q", got)
	}
	if nw := find("Guest"); !nw.Saved || !nw.Connected {
		t.Errorf("Guest after connecting %+v", nw)
	}

	// An open network needs no password.
	if err := n.ConnectWifi(ctx, find("Cafe"), ""); err != nil {
		t.Fatal(err)
	}

	// A wrong password says so, and is not kept.
	nw := find("Office")
	nw.Security = "wpa" // as if it took a password
	err := n.ConnectWifi(ctx, nw, "wrong")
	var we *WifiError
	if !errors.As(err, &we) || !we.WrongPassword() {
		t.Fatalf("wrong password: %v", err)
	}
	if names := f.savedNames(); slices.Contains(names, "Office") {
		t.Errorf("saved after a wrong password: %v", names)
	}

	// A saved network takes a new password.
	if err := n.ConnectWifi(ctx, find("Home"), "new"); err != nil {
		t.Fatal(err)
	}
	if got := f.psk("Home"); got != "new" {
		t.Errorf("Home's password %q", got)
	}

	// An enterprise network is not set up here.
	if err := n.ConnectWifi(ctx, find("Office"), "x"); !errors.Is(err, ErrEnterprise) {
		t.Errorf("enterprise: %v", err)
	}
}

func TestForgetAndDisconnectWifi(t *testing.T) {
	f, n := startWifi(t)
	ctx := context.Background()
	if err := n.ForgetWifi(ctx, "Home"); err != nil {
		t.Fatal(err)
	}
	if names := f.savedNames(); !reflect.DeepEqual(names, []string{"Attic"}) {
		t.Errorf("saved after forgetting Home: %v", names)
	}
	if err := n.DisconnectWifi(ctx); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	disconnected := f.disconnected
	f.mu.Unlock()
	if !disconnected {
		t.Error("the device was not disconnected")
	}
	// A scan soon after another is turned down, and that is no error.
	for range 2 {
		if err := n.ScanWifi(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAPSecurity(t *testing.T) {
	for _, c := range []struct {
		flags, wpa, rsn uint32
		want            string
	}{
		{0, 0, 0, ""},
		{apPrivacy, 0, 0, "wep"},
		{apPrivacy, keyPSK, keyPSK, "wpa"},
		{apPrivacy, 0, keySAE, "sae"},
		{apPrivacy, 0, keyPSK | keySAE, "wpa"},
		{0, 0, keyOWE, "owe"},
		{apPrivacy, 0, key8021X, "enterprise"},
	} {
		if got := apSecurity(c.flags, c.wpa, c.rsn); got != c.want {
			t.Errorf("apSecurity(%#x, %#x, %#x) = %q, want %q", c.flags, c.wpa, c.rsn, got, c.want)
		}
	}
}

// DNS servers from NameserverData, or from an older NetworkManager's
// Nameservers: numbers, each an address in network byte order.
func TestNameservers(t *testing.T) {
	data := map[string]dbus.Variant{"NameserverData": dbus.MakeVariant([]map[string]dbus.Variant{{"address": dbus.MakeVariant("9.9.9.9")}})}
	if got := nameservers(data); !slices.Equal(got, []string{"9.9.9.9"}) {
		t.Errorf("NameserverData: %v", got)
	}
	// 192.168.1.1, as NetworkManager stores it on a little-endian machine.
	old := map[string]dbus.Variant{"Nameservers": dbus.MakeVariant([]uint32{0x0101a8c0})}
	if got := nameservers(old); !slices.Equal(got, []string{"192.168.1.1"}) {
		t.Errorf("Nameservers: %v", got)
	}
	if got := nameservers(map[string]dbus.Variant{}); got != nil {
		t.Errorf("none: %v", got)
	}
}
