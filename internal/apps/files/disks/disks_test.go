package disks

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/dbusexport"
	"github.com/lyricat/inua/internal/dbustest"
)

// fakeUDisks serves the parts of UDisks2 the client uses: the object
// manager, Filesystem.Mount/Unmount, Encrypted.Unlock/Lock and
// Drive.Eject/PowerOff. Its objects are plain maps the test edits.
type fakeUDisks struct {
	conn  *dbus.Conn
	mu    sync.Mutex
	objs  managed
	calls []string
	deny  bool // answer NotAuthorized
}

func bytez(s string) []byte { return append([]byte(s), 0) }

// GetManagedObjects answers with a copy: the reply is marshalled after
// the lock is let go, while a call may change the objects.
func (f *fakeUDisks) GetManagedObjects() (managed, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := managed{}
	for p, ifs := range f.objs {
		out[p] = map[string]map[string]dbus.Variant{}
		for name, props := range ifs {
			out[p][name] = maps.Clone(props)
		}
	}
	return out, nil
}

// set changes the fake's state under its lock.
func (f *fakeUDisks) set(change func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change()
}

func (f *fakeUDisks) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeUDisks) record(call string) {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
}

type fsObj struct {
	f    *fakeUDisks
	path dbus.ObjectPath
}

func (o fsObj) Mount(map[string]dbus.Variant) (string, *dbus.Error) {
	o.f.record("mount " + string(o.path))
	o.f.mu.Lock()
	deny := o.f.deny
	o.f.mu.Unlock()
	if deny {
		return "", dbus.NewError("org.freedesktop.UDisks2.Error.NotAuthorizedDismissed", []any{"dismissed"})
	}
	where := "/media/u/" + string(o.path[len(o.path)-4:])
	o.f.mu.Lock()
	o.f.objs[o.path][ifFS]["MountPoints"] = dbus.MakeVariant([][]byte{bytez(where)})
	o.f.mu.Unlock()
	return where, nil
}

func (o fsObj) Unmount(map[string]dbus.Variant) *dbus.Error {
	o.f.record("unmount " + string(o.path))
	o.f.mu.Lock()
	o.f.objs[o.path][ifFS]["MountPoints"] = dbus.MakeVariant([][]byte{})
	o.f.mu.Unlock()
	return nil
}

type cryptObj struct {
	f    *fakeUDisks
	path dbus.ObjectPath
}

func (o cryptObj) Unlock(pass string, _ map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	o.f.record("unlock " + string(o.path))
	if pass != "secret" {
		return "", dbus.NewError("org.freedesktop.UDisks2.Error.Failed", []any{"wrong passphrase"})
	}
	return "/org/freedesktop/UDisks2/block_devices/dm_2d0", nil
}

func (o cryptObj) Lock(map[string]dbus.Variant) *dbus.Error {
	o.f.record("lock " + string(o.path))
	return nil
}

type driveObj struct {
	f    *fakeUDisks
	path dbus.ObjectPath
}

func (o driveObj) Eject(map[string]dbus.Variant) *dbus.Error {
	o.f.record("eject " + string(o.path))
	return nil
}

func (o driveObj) PowerOff(map[string]dbus.Variant) *dbus.Error {
	o.f.record("poweroff " + string(o.path))
	return nil
}

const (
	stick = dbus.ObjectPath("/org/freedesktop/UDisks2/drives/Kingston_DT_1")
	nvme  = dbus.ObjectPath("/org/freedesktop/UDisks2/drives/Samsung_990")
	sdb1  = dbus.ObjectPath("/org/freedesktop/UDisks2/block_devices/sdb1")
	sdb2  = dbus.ObjectPath("/org/freedesktop/UDisks2/block_devices/sdb2")
	dm0   = dbus.ObjectPath("/org/freedesktop/UDisks2/block_devices/dm_2d0")
	root1 = dbus.ObjectPath("/org/freedesktop/UDisks2/block_devices/nvme0n1p2")
	data1 = dbus.ObjectPath("/org/freedesktop/UDisks2/block_devices/nvme0n1p3")
	swap  = dbus.ObjectPath("/org/freedesktop/UDisks2/block_devices/nvme0n1p4")
)

func v(x any) dbus.Variant { return dbus.MakeVariant(x) }

func block(drive dbus.ObjectPath, dev, usage, typ, label string, system bool) map[string]dbus.Variant {
	return map[string]dbus.Variant{
		"Device": v(bytez(dev)), "PreferredDevice": v(bytez(dev)), "Drive": v(drive),
		"IdUsage": v(usage), "IdType": v(typ), "IdLabel": v(label), "IdUUID": v("uuid-" + dev),
		"Size": v(uint64(32_000_000_000)), "HintSystem": v(system), "HintIgnore": v(false),
		"HintName": v(""), "ReadOnly": v(false), "CryptoBackingDevice": v(dbus.ObjectPath("/")),
	}
}

func fsIf(points ...string) map[string]dbus.Variant {
	raw := [][]byte{}
	for _, p := range points {
		raw = append(raw, bytez(p))
	}
	return map[string]dbus.Variant{"MountPoints": v(raw)}
}

func startFake(t *testing.T) (*fakeUDisks, *Client) {
	t.Helper()
	addr := dbustest.Start(t)
	srv := dbustest.Conn(t, addr)
	f := &fakeUDisks{conn: srv, objs: managed{
		stick: {ifDrive: {"Vendor": v("Kingston"), "Model": v("DataTraveler"), "ConnectionBus": v("usb"),
			"Removable": v(true), "MediaRemovable": v(false), "Ejectable": v(true), "CanPowerOff": v(true)}},
		nvme: {ifDrive: {"Vendor": v("Samsung"), "Model": v("990"), "ConnectionBus": v("nvme"),
			"Removable": v(false), "MediaRemovable": v(false), "Ejectable": v(false), "CanPowerOff": v(false)}},
		sdb1: {ifBlock: block(stick, "/dev/sdb1", "filesystem", "vfat", "PHOTOS", false), ifFS: fsIf("/media/u/PHOTOS"), ifPart: {}},
		sdb2: {ifBlock: block(stick, "/dev/sdb2", "crypto", "crypto_LUKS", "", false),
			ifCrypt: {"CleartextDevice": v(dbus.ObjectPath("/"))}},
		root1: {ifBlock: block(nvme, "/dev/nvme0n1p2", "filesystem", "ext4", "", true), ifFS: fsIf("/")},
		data1: {ifBlock: block(nvme, "/dev/nvme0n1p3", "filesystem", "ext4", "data", false), ifFS: fsIf()},
		swap:  {ifBlock: block(nvme, "/dev/nvme0n1p4", "other", "swap", "", true)},
	}}
	if err := dbusexport.Export(srv, f, root, ifObjects); err != nil {
		t.Fatal(err)
	}
	for _, p := range []dbus.ObjectPath{sdb1, root1, data1, dm0} {
		dbusexport.Export(srv, fsObj{f, p}, p, ifFS)
	}
	dbusexport.Export(srv, cryptObj{f, sdb2}, sdb2, ifCrypt)
	for _, p := range []dbus.ObjectPath{stick, nvme} {
		dbusexport.Export(srv, driveObj{f, p}, p, ifDrive)
	}
	if _, err := srv.RequestName(service, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	return f, New(dbustest.Conn(t, addr))
}

func TestVolumes(t *testing.T) {
	_, c := startFake(t)
	vs, err := c.Volumes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, v := range vs {
		got = append(got, v.Device)
	}
	// The stick's first (removable), the system's root and swap left out.
	if !slices.Equal(got, []string{"/dev/sdb1", "/dev/sdb2", "/dev/nvme0n1p3"}) {
		t.Fatalf("volumes %v", got)
	}
	photos := vs[0]
	if photos.Name() != "PHOTOS" || !photos.Mounted() || photos.MountPoints[0] != "/media/u/PHOTOS" || !photos.Drive.Removable || photos.Drive.Bus != "usb" {
		t.Fatalf("stick %+v", photos)
	}
	if vs[1].Name() != "Kingston DataTraveler 32 GB" || !vs[1].Encrypted {
		t.Fatalf("locked container %+v (%s)", vs[1], vs[1].Name())
	}
}

func TestMountUnlockEject(t *testing.T) {
	f, c := startFake(t)
	ctx := context.Background()
	vs, _ := c.Volumes(ctx)
	data := vs[2]
	where, err := c.Mount(ctx, data)
	if err != nil || where != "/media/u/"+string(data1[len(data1)-4:]) {
		t.Fatalf("mount: %q %v", where, err)
	}
	if clear, err := c.Unlock(ctx, vs[1], "secret"); err != nil || clear != dm0 {
		t.Fatalf("unlock: %v %v", clear, err)
	}
	if _, err := c.Unlock(ctx, vs[1], "wrong"); err == nil {
		t.Fatal("a wrong passphrase unlocked")
	}

	f.set(func() { f.calls = nil })
	if err := c.Eject(ctx, vs[0]); err != nil {
		t.Fatal(err)
	}
	// Unmounts the stick's filesystem, then ejects and powers it off; the
	// NVMe drive is not touched.
	if got := f.called(); !slices.Equal(got, []string{"unmount " + string(sdb1), "eject " + string(stick), "poweroff " + string(stick)}) {
		t.Fatalf("calls %v", got)
	}

	f.set(func() { f.deny = true })
	vs, _ = c.Volumes(ctx)
	for _, v := range vs {
		if v.Path == sdb1 {
			if _, err := c.Mount(ctx, v); !errors.Is(err, ErrNotAuthorized) {
				t.Fatalf("refused: %v", err)
			}
		}
	}
}

func TestWatch(t *testing.T) {
	f, c := startFake(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changed := make(chan struct{}, 4)
	if err := c.Watch(ctx, func() { changed <- struct{}{} }); err != nil {
		t.Fatal(err)
	}
	for range 3 { // a burst is one change
		f.conn.Emit(root, ifObjects+".InterfacesAdded", sdb1, map[string]map[string]dbus.Variant{})
	}
	select {
	case <-changed:
	case <-time.After(2 * time.Second):
		t.Fatal("no change")
	}
	select {
	case <-changed:
		t.Fatal("a burst reported twice")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestSize(t *testing.T) {
	for n, want := range map[uint64]string{512: "512 B", 1500: "1.5 kB", 32_000_000_000: "32 GB", 2_000_000_000_000: "2 TB"} {
		if got := Size(n); got != want {
			t.Errorf("%d: %s, want %s", n, got, want)
		}
	}
}
