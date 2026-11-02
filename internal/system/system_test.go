package system

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	"github.com/lyricat/inua/internal/dbusexport"
	"github.com/lyricat/inua/internal/dbustest"
)

// states collects what a service reports.
type states[T any] struct{ ch chan T }

func newStates[T any]() *states[T] { return &states[T]{ch: make(chan T, 64)} }
func (s *states[T]) add(v T)       { s.ch <- v }

func (s *states[T]) wait(t *testing.T, what string, ok func(T) bool) T {
	t.Helper()
	timeout := time.After(5 * time.Second)
	var last T
	for {
		select {
		case v := <-s.ch:
			if ok(v) {
				return v
			}
			last = v
		case <-timeout:
			t.Fatalf("timed out waiting for %s; last state %+v", what, last)
		}
	}
}

func dialer(addr string) Dialer {
	return func() (*dbus.Conn, error) { return dbustest.Dial(addr) }
}

func run(t *testing.T, f func(context.Context)) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
}

// service connects as a fake service owning name.
func service(t *testing.T, addr, name string) *dbus.Conn {
	t.Helper()
	conn := dbustest.Conn(t, addr)
	if reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("own %s: %v %v", name, reply, err)
	}
	return conn
}

func export(t *testing.T, conn *dbus.Conn, path dbus.ObjectPath, props prop.Map) *prop.Properties {
	t.Helper()
	p, err := dbusexport.Properties(conn, path, props)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func emitted(v any) *prop.Prop { return &prop.Prop{Value: v, Emit: prop.EmitTrue} }

// --- NetworkManager ---

const nmPath = dbus.ObjectPath("/org/freedesktop/NetworkManager")

type fakeNM struct {
	conn   *dbus.Conn
	nm, ap *prop.Properties
}

func newFakeNM(t *testing.T, addr string) *fakeNM {
	f := &fakeNM{conn: service(t, addr, nmName)}
	f.nm = export(t, f.conn, nmPath, prop.Map{nmName: {
		"WirelessEnabled":         {Value: true, Writable: true, Emit: prop.EmitTrue},
		"WirelessHardwareEnabled": {Value: true},
		"PrimaryConnection":       emitted(dbus.ObjectPath("/org/freedesktop/NetworkManager/ActiveConnection/1")),
		"Devices":                 {Value: []dbus.ObjectPath{"/org/freedesktop/NetworkManager/Devices/1", "/org/freedesktop/NetworkManager/Devices/2"}},
	}})
	export(t, f.conn, "/org/freedesktop/NetworkManager/Devices/1", prop.Map{nmName + ".Device": {"DeviceType": {Value: uint32(1)}}})
	export(t, f.conn, "/org/freedesktop/NetworkManager/Devices/2", prop.Map{nmName + ".Device": {"DeviceType": {Value: uint32(2)}}})
	export(t, f.conn, "/org/freedesktop/NetworkManager/ActiveConnection/1", prop.Map{nmName + ".Connection.Active": {
		"Id":             {Value: "Home"},
		"Type":           {Value: "802-11-wireless"},
		"SpecificObject": {Value: dbus.ObjectPath("/org/freedesktop/NetworkManager/AccessPoint/7")},
	}})
	f.ap = export(t, f.conn, "/org/freedesktop/NetworkManager/AccessPoint/7", prop.Map{nmName + ".AccessPoint": {
		"Ssid":     {Value: []byte("Home 5G")},
		"Strength": emitted(byte(64)),
	}})
	export(t, f.conn, "/org/freedesktop/NetworkManager/ActiveConnection/2", prop.Map{nmName + ".Connection.Active": {
		"Id":             {Value: "Wired connection 1"},
		"Type":           {Value: "802-3-ethernet"},
		"SpecificObject": {Value: dbus.ObjectPath("/")},
	}})
	return f
}

func TestNetwork(t *testing.T) {
	addr := dbustest.Start(t)
	f := newFakeNM(t, addr)
	n := NewNetwork(dialer(addr))
	got := newStates[NetworkState]()
	run(t, func(ctx context.Context) { n.Run(ctx, got.add) })

	want := NetworkState{Available: true, WifiEnabled: true, WifiDevice: true, Kind: "wifi", Name: "Home 5G", Strength: 64}
	if s := got.wait(t, "wifi", func(s NetworkState) bool { return s.Available }); !reflect.DeepEqual(s, want) {
		t.Fatalf("state\n got %+v\nwant %+v", s, want)
	}

	f.ap.SetMust(nmName+".AccessPoint", "Strength", byte(30))
	got.wait(t, "new strength", func(s NetworkState) bool { return s.Strength == 30 })

	f.nm.SetMust(nmName, "PrimaryConnection", dbus.ObjectPath("/org/freedesktop/NetworkManager/ActiveConnection/2"))
	got.wait(t, "ethernet", func(s NetworkState) bool {
		return s.Kind == "ethernet" && s.Name == "Wired connection 1" && s.Strength == 0
	})

	f.nm.SetMust(nmName, "PrimaryConnection", dbus.ObjectPath("/"))
	got.wait(t, "offline", func(s NetworkState) bool { return s.Kind == "" && s.Name == "" })

	if err := n.SetWifi(false); err != nil {
		t.Fatal(err)
	}
	got.wait(t, "wifi off", func(s NetworkState) bool { return !s.WifiEnabled })
	if v := f.nm.GetMust(nmName, "WirelessEnabled"); v != false {
		t.Fatalf("WirelessEnabled = %v after SetWifi(false)", v)
	}
}

func TestNetworkServiceRestart(t *testing.T) {
	addr := dbustest.Start(t)
	got := newStates[NetworkState]()
	n := NewNetwork(dialer(addr))
	run(t, func(ctx context.Context) { n.Run(ctx, got.add) })
	got.wait(t, "unavailable", func(s NetworkState) bool { return !s.Available })

	f := newFakeNM(t, addr)
	got.wait(t, "available", func(s NetworkState) bool { return s.Available && s.Name == "Home 5G" })
	f.conn.Close()
	got.wait(t, "unavailable again", func(s NetworkState) bool { return !s.Available })
}

// --- UPower ---

func TestBattery(t *testing.T) {
	addr := dbustest.Start(t)
	conn := service(t, addr, upowerName)
	p := export(t, conn, displayDevice, prop.Map{upowerName + ".Device": {
		"IsPresent":  {Value: true},
		"Type":       {Value: uint32(2)},
		"Percentage": emitted(81.0),
		"State":      emitted(uint32(2)),
	}})
	b := NewBattery(dialer(addr))
	got := newStates[BatteryState]()
	run(t, func(ctx context.Context) { b.Run(ctx, got.add) })

	want := BatteryState{Present: true, Percent: 81}
	if s := got.wait(t, "battery", func(s BatteryState) bool { return s.Present }); s != want {
		t.Fatalf("state %+v, want %+v", s, want)
	}
	p.SetMust(upowerName+".Device", "State", uint32(1))
	got.wait(t, "charging", func(s BatteryState) bool { return s.Charging })
}

func TestPowerProfiles(t *testing.T) {
	addr := dbustest.Start(t)
	conn := service(t, addr, profilesName)
	profiles := []map[string]dbus.Variant{
		{"Profile": dbus.MakeVariant("power-saver"), "Driver": dbus.MakeVariant("amd_pmf")},
		{"Profile": dbus.MakeVariant("balanced"), "Driver": dbus.MakeVariant("amd_pmf")},
		{"Profile": dbus.MakeVariant("performance"), "Driver": dbus.MakeVariant("amd_pmf")},
	}
	export(t, conn, profilesPath, prop.Map{profilesName: {
		"ActiveProfile": {Value: "balanced", Writable: true, Emit: prop.EmitTrue},
		"Profiles":      {Value: profiles},
	}})
	p := NewPowerProfiles(dialer(addr))
	got := newStates[PowerProfileState]()
	run(t, func(ctx context.Context) { p.Run(ctx, got.add) })

	want := PowerProfileState{Available: true, Active: "balanced", Profiles: []string{"power-saver", "balanced", "performance"}}
	if s := got.wait(t, "profiles", func(s PowerProfileState) bool { return s.Available }); !reflect.DeepEqual(s, want) {
		t.Fatalf("state\n got %+v\nwant %+v", s, want)
	}
	if err := p.SetProfile("power-saver"); err != nil {
		t.Fatal(err)
	}
	got.wait(t, "power-saver", func(s PowerProfileState) bool { return s.Active == "power-saver" })
}

// --- MPRIS ---

type fakePlayer struct {
	conn  *dbus.Conn
	props *prop.Properties
	mu    sync.Mutex
	calls []string
}

type playerMethods struct{ f *fakePlayer }

func (m playerMethods) rec(s string) *dbus.Error {
	m.f.mu.Lock()
	m.f.calls = append(m.f.calls, s)
	m.f.mu.Unlock()
	return nil
}
func (m playerMethods) PlayPause() *dbus.Error { return m.rec("PlayPause") }
func (m playerMethods) Next() *dbus.Error      { return m.rec("Next") }
func (m playerMethods) Previous() *dbus.Error  { return m.rec("Previous") }

func newFakePlayer(t *testing.T, addr, name, identity, status, title string) *fakePlayer {
	f := &fakePlayer{conn: service(t, addr, mprisPrefix+name)}
	if err := dbusexport.Export(f.conn, playerMethods{f}, mprisPath, mprisPlayer); err != nil {
		t.Fatal(err)
	}
	f.props = export(t, f.conn, mprisPath, prop.Map{
		mprisRoot: {"Identity": {Value: identity}},
		mprisPlayer: {
			"PlaybackStatus": emitted(status),
			"Metadata": emitted(map[string]dbus.Variant{
				"xesam:title":  dbus.MakeVariant(title),
				"xesam:artist": dbus.MakeVariant([]string{"Artist A", "Artist B"}),
				"mpris:artUrl": dbus.MakeVariant("file:///tmp/art.png"),
			}),
			"CanGoNext":     {Value: true},
			"CanGoPrevious": {Value: false},
			"CanPlay":       {Value: true},
			"CanPause":      {Value: true},
		},
	})
	return f
}

func TestMediaPrefersPlaying(t *testing.T) {
	addr := dbustest.Start(t)
	newFakePlayer(t, addr, "aaa", "Paused Player", "Paused", "Old Song")
	playing := newFakePlayer(t, addr, "zzz", "Music", "Playing", "Song")
	m := NewMedia(dialer(addr))
	got := newStates[MediaState]()
	run(t, func(ctx context.Context) { m.Run(ctx, got.add) })

	want := MediaState{
		Available: true, Player: mprisPrefix + "zzz", Identity: "Music",
		Title: "Song", Artist: "Artist A, Artist B", ArtURL: "file:///tmp/art.png",
		Playing: true, CanNext: true, CanPlayPause: true,
	}
	if s := got.wait(t, "the playing player", func(s MediaState) bool { return s.Playing }); !reflect.DeepEqual(s, want) {
		t.Fatalf("state\n got %+v\nwant %+v", s, want)
	}

	if err := m.PlayPause(); err != nil {
		t.Fatal(err)
	}
	if err := m.Next(); err != nil {
		t.Fatal(err)
	}
	playing.mu.Lock()
	calls := playing.calls
	playing.mu.Unlock()
	if !reflect.DeepEqual(calls, []string{"PlayPause", "Next"}) {
		t.Fatalf("calls %v", calls)
	}

	// Pausing keeps the same player: no other one is playing.
	playing.props.SetMust(mprisPlayer, "PlaybackStatus", "Paused")
	if s := got.wait(t, "paused", func(s MediaState) bool { return !s.Playing }); s.Player != mprisPrefix+"zzz" {
		t.Fatalf("switched to %s after pausing", s.Player)
	}

	playing.conn.Close()
	got.wait(t, "the remaining player", func(s MediaState) bool { return s.Player == mprisPrefix+"aaa" })
}

func TestMediaNone(t *testing.T) {
	addr := dbustest.Start(t)
	m := NewMedia(dialer(addr))
	got := newStates[MediaState]()
	run(t, func(ctx context.Context) { m.Run(ctx, got.add) })
	if s := got.wait(t, "a state", func(MediaState) bool { return true }); s.Available {
		t.Fatalf("available with no players: %+v", s)
	}
	if err := m.PlayPause(); err == nil {
		t.Fatal("PlayPause with no player succeeded")
	}
	newFakePlayer(t, addr, "x", "X", "Stopped", "")
	got.wait(t, "the new player", func(s MediaState) bool { return s.Available })
}
