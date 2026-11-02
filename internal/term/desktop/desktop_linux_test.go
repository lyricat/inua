package desktop

import (
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/dbusexport"
	"github.com/lyricat/inua/internal/dbustest"
)

// fakeNotes is a notification server on a private bus.
type fakeNotes struct {
	mu    sync.Mutex
	calls [][]any
}

func (f *fakeNotes) Notify(app string, replaces uint32, icon, summary, body string, actions []string, hints map[string]dbus.Variant, timeout int32) (uint32, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, []any{summary, body, actions, hints["desktop-entry"].Value()})
	return uint32(len(f.calls)), nil
}

// A notification goes to the notification server with a Show, which runs
// what the host gave for it.
func TestNotify(t *testing.T) {
	addr := dbustest.Start(t)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", addr)
	server := dbustest.Conn(t, addr)
	notes := &fakeNotes{}
	dbusexport.Export(server, notes, "/org/freedesktop/Notifications", "org.freedesktop.Notifications")
	if r, err := server.RequestName("org.freedesktop.Notifications", dbus.NameFlagDoNotQueue); err != nil || r != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("name: %v %v", r, err)
	}
	d := New()
	shown := make(chan string, 2)
	if err := d.Notify("Codex needs you", "Allow command?", func() { shown <- "first" }); err != nil {
		t.Fatal(err)
	}
	if err := d.Notify("Claude is done", "", func() { shown <- "second" }); err != nil {
		t.Fatal(err)
	}
	notes.mu.Lock()
	got := notes.calls[0]
	notes.mu.Unlock()
	if got[0] != "Codex needs you" || got[1] != "Allow command?" || got[3] != appID {
		t.Errorf("notification %q", got)
	}
	server.Emit("/org/freedesktop/Notifications", "org.freedesktop.Notifications.ActionInvoked", uint32(1), "default")
	select {
	case which := <-shown:
		if which != "first" {
			t.Errorf("Show ran the %s notification's", which)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Show did nothing")
	}
}
