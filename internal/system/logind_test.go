package system

import (
	"strings"
	"sync"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	"github.com/lyricat/inua/internal/dbusexport"
	"github.com/lyricat/inua/internal/dbustest"
)

// logind answers CanHibernate as told; the bus calls it from its own
// goroutine.
type logind struct {
	mu     sync.Mutex
	answer string
}

func (l *logind) CanHibernate() (string, *dbus.Error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.answer, nil
}

func TestCanHibernate(t *testing.T) {
	addr := dbustest.Start(t)
	l := &logind{}
	conn := service(t, addr, "org.freedesktop.login1")
	if err := dbusexport.Export(conn, l, "/org/freedesktop/login1", "org.freedesktop.login1.Manager"); err != nil {
		t.Fatal(err)
	}
	for answer, want := range map[string]bool{"yes": true, "challenge": true, "no": false, "na": false} {
		l.mu.Lock()
		l.answer = answer
		l.mu.Unlock()
		if got, err := CanHibernate(dialer(addr)); err != nil || got != want {
			t.Errorf("%s: %v %v, want %v", answer, got, err, want)
		}
	}
	conn.ReleaseName("org.freedesktop.login1")
	if _, err := CanHibernate(dialer(addr)); err == nil {
		t.Error("no logind: no error")
	}
}

// The lid and the power key as logind has them; a mains-power lid it has
// no setting for is the battery's.
func TestReadButtons(t *testing.T) {
	addr := dbustest.Start(t)
	conn := service(t, addr, "org.freedesktop.login1")
	export(t, conn, "/org/freedesktop/login1", prop.Map{"org.freedesktop.login1.Manager": {
		"HandleLidSwitch":              {Value: "suspend"},
		"HandleLidSwitchExternalPower": {Value: ""},
		"HandlePowerKey":               {Value: "poweroff"},
	}})
	b, err := ReadButtons(dialer(addr))
	if err != nil || b != (Buttons{Lid: "suspend", LidAC: "suspend", Power: "poweroff"}) {
		t.Errorf("%+v %v", b, err)
	}
}

// The drop-in holds the three settings and nothing else: an action
// Settings does not offer is refused, so no other line can be written.
func TestButtonsDropIn(t *testing.T) {
	conf, err := Buttons{Lid: "lock", LidAC: "ignore", Power: "suspend"}.DropIn()
	if err != nil || !strings.Contains(conf, "[Login]\nHandleLidSwitch=lock\nHandleLidSwitchExternalPower=ignore\nHandlePowerKey=suspend\n") {
		t.Errorf("%q %v", conf, err)
	}
	if _, err := (Buttons{Lid: "suspend\nKillUserProcesses=yes", LidAC: "ignore", Power: "suspend"}).DropIn(); err == nil {
		t.Error("an action with a line of its own was written")
	}
}
