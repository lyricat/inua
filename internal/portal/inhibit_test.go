package portal

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/dbusexport"
	"github.com/lyricat/inua/internal/dbustest"
	"github.com/lyricat/inua/internal/proc"
)

// fakeScreenSaver is an idle daemon's org.freedesktop.ScreenSaver.
type fakeScreenSaver struct {
	mu     sync.Mutex
	held   map[uint32]string
	cookie uint32
}

func (f *fakeScreenSaver) Inhibit(app, reason string) (uint32, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cookie++
	f.held[f.cookie] = app + ": " + reason
	return f.cookie, nil
}

func (f *fakeScreenSaver) UnInhibit(cookie uint32) *dbus.Error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.held, cookie)
	return nil
}

func (f *fakeScreenSaver) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.held)
}

func TestInhibit(t *testing.T) {
	addr := dbustest.Start(t)
	idle := &fakeScreenSaver{held: map[uint32]string{}}
	ssConn := dbustest.Conn(t, addr)
	dbusexport.Export(ssConn, idle, "/org/freedesktop/ScreenSaver", "org.freedesktop.ScreenSaver")
	if _, err := ssConn.RequestName("org.freedesktop.ScreenSaver", 0); err != nil {
		t.Fatal(err)
	}
	conn := dbustest.Conn(t, addr)
	n := NewInhibits(conn, nil)
	client := serveShell(t, addr, conn, n.Interface())
	obj := client.Object(ShellName, Path)

	handle := dbus.ObjectPath("/org/freedesktop/portal/desktop/request/1_9/v")
	opts := map[string]dbus.Variant{"reason": dbus.MakeVariant("Playing a film")}
	if err := obj.Call(Prefix+"Inhibit.Inhibit", 0, handle, "org.example.Video", "", InhibitIdle|InhibitLogout, opts).Err; err != nil {
		t.Fatal(err)
	}
	if idle.count() != 1 {
		t.Errorf("idle daemon holds %d", idle.count())
	}
	got := n.Holding(InhibitLogout)
	if len(got) != 1 || got[0].App != "org.example.Video" || got[0].Reason != "Playing a film" {
		t.Errorf("Holding(logout) = %v", got)
	}
	if len(n.Holding(InhibitUserSwitch)) != 0 {
		t.Error("holding user switch too")
	}
	if err := client.Object(ShellName, handle).Call(Prefix+"Request.Close", 0).Err; err != nil {
		t.Fatal(err)
	}
	if idle.count() != 0 || len(n.Holding(InhibitLogout|InhibitIdle)) != 0 {
		t.Errorf("held after Close: idle %d, %v", idle.count(), n.Holding(InhibitLogout|InhibitIdle))
	}
}

// An app outside a sandbox comes with no ID: it is named by its process,
// found from the unique name the frontend puts in the handle.
func TestInhibitNamesSender(t *testing.T) {
	addr := dbustest.Start(t)
	conn := dbustest.Conn(t, addr)
	n := NewInhibits(conn, nil)
	client := serveShell(t, addr, conn, n.Interface())
	app := dbustest.Conn(t, addr) // the app, whose name the frontend passes on
	sender := strings.ReplaceAll(strings.TrimPrefix(app.Names()[0], ":"), ".", "_")
	handle := dbus.ObjectPath("/org/freedesktop/portal/desktop/request/" + sender + "/t1")
	opts := map[string]dbus.Variant{"reason": dbus.MakeVariant("Download in progress")}
	if err := client.Object(ShellName, Path).Call(Prefix+"Inhibit.Inhibit", 0, handle, "", "", InhibitLogout, opts).Err; err != nil {
		t.Fatal(err)
	}
	want := proc.Name(os.Getpid())
	if got := n.Holding(InhibitLogout); len(got) != 1 || got[0].App != want {
		t.Errorf("Holding = %v, want app %q", got, want)
	}
}

func TestInhibitMonitor(t *testing.T) {
	addr := dbustest.Start(t)
	conn := dbustest.Conn(t, addr)
	n := NewInhibits(conn, nil)
	client := serveShell(t, addr, conn, n.Interface())
	sigs := make(chan *dbus.Signal, 8)
	client.Signal(sigs)
	client.AddMatchSignal(dbus.WithMatchInterface(Prefix + "Inhibit"))
	next := func() map[string]dbus.Variant {
		t.Helper()
		for {
			select {
			case sig := <-sigs:
				if sig.Name != Prefix+"Inhibit.StateChanged" {
					continue
				}
				state, _ := sig.Body[1].(map[string]dbus.Variant)
				return state
			case <-time.After(2 * time.Second):
				t.Fatal("no StateChanged")
				return nil
			}
		}
	}

	session := dbus.ObjectPath("/org/freedesktop/portal/desktop/session/1_9/m")
	var resp uint32
	err := client.Object(ShellName, Path).Call(Prefix+"Inhibit.CreateMonitor", 0,
		dbus.ObjectPath("/org/freedesktop/portal/desktop/request/1_9/m"), session, "org.example.App", "").Store(&resp)
	if err != nil || resp != Success {
		t.Fatalf("CreateMonitor %d %v", resp, err)
	}
	if s := next(); s["session-state"].Value() != SessionRunning || s["screensaver-active"].Value() != false {
		t.Errorf("first state %v", s)
	}
	n.SetLocked(true)
	if s := next(); s["screensaver-active"].Value() != true {
		t.Errorf("locked: %v", s)
	}
	n.SetState(SessionQueryEnd)
	if s := next(); s["session-state"].Value() != SessionQueryEnd {
		t.Errorf("query end: %v", s)
	}
	if err := client.Object(ShellName, session).Call(Prefix+"Session.Close", 0).Err; err != nil {
		t.Fatal(err)
	}
	n.SetState(SessionRunning)
	for {
		select {
		case sig := <-sigs:
			if sig.Name == Prefix+"Inhibit.StateChanged" {
				t.Errorf("a closed monitor heard %v", sig.Body)
			}
			continue
		case <-time.After(150 * time.Millisecond):
		}
		break
	}
}
