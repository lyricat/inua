package portal

import (
	"cmp"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"

	"github.com/lyricat/inua/internal/dbusexport"
	"github.com/lyricat/inua/internal/proc"
)

// What an app may hold off, Inhibit's flags.
const (
	InhibitLogout     uint32 = 1
	InhibitUserSwitch uint32 = 2
	InhibitSuspend    uint32 = 4
	InhibitIdle       uint32 = 8
)

// The session's states, as monitors hear them.
const (
	SessionRunning  uint32 = 1
	SessionQueryEnd uint32 = 2 // the user asked to log out; apps may say why not
	SessionEnding   uint32 = 3
)

// Inhibitor is an app holding something off.
type Inhibitor struct {
	App    string
	Reason string
	Flags  uint32
}

// Inhibits serves org.freedesktop.impl.portal.Inhibit. Idle goes to the
// idle daemon's org.freedesktop.ScreenSaver (hypridle's), suspend to a
// logind lock; logout and switching user are held by the shell, which
// asks Inhibitors before it ends the session.
type Inhibits struct {
	conn   *dbus.Conn
	system func() (*dbus.Conn, error) // logind's bus

	mu       sync.Mutex
	held     map[dbus.ObjectPath]*held
	monitors map[dbus.ObjectPath]bool
	locked   bool   // the screen
	state    uint32 // SessionRunning…
}

type held struct {
	Inhibitor
	cookie uint32   // the idle daemon's, 0 without
	sleep  *os.File // logind's lock
}

// NewInhibits takes logind's bus from system when an app holds off
// suspend.
func NewInhibits(conn *dbus.Conn, system func() (*dbus.Conn, error)) *Inhibits {
	return &Inhibits{conn: conn, system: system, held: map[dbus.ObjectPath]*held{}, monitors: map[dbus.ObjectPath]bool{}, state: SessionRunning}
}

// Interface is what Export serves.
func (n *Inhibits) Interface() Interface {
	return Interface{Name: "Inhibit", Version: 3, Methods: inhibitObj{n}, XML: `
<method name="Inhibit"><arg type="o" name="handle" direction="in"/><arg type="s" name="app_id" direction="in"/><arg type="s" name="window" direction="in"/><arg type="u" name="flags" direction="in"/><arg type="a{sv}" name="options" direction="in"/></method>
<method name="CreateMonitor"><arg type="o" name="handle" direction="in"/><arg type="o" name="session_handle" direction="in"/><arg type="s" name="app_id" direction="in"/><arg type="s" name="window" direction="in"/><arg type="u" name="response" direction="out"/></method>
<method name="QueryEndResponse"><arg type="o" name="session_handle" direction="in"/></method>
<signal name="StateChanged"><arg type="o" name="session_handle"/><arg type="a{sv}" name="state"/></signal>`}
}

// Holding are the apps holding off what flags names, by app.
func (n *Inhibits) Holding(flags uint32) []Inhibitor {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []Inhibitor
	for _, h := range n.held {
		if h.Flags&flags != 0 {
			out = append(out, h.Inhibitor)
		}
	}
	slices.SortFunc(out, func(a, b Inhibitor) int { return cmp.Compare(a.App, b.App) })
	return out
}

// SetLocked tells monitors whether the screen is locked.
func (n *Inhibits) SetLocked(on bool) {
	n.mu.Lock()
	changed := n.locked != on
	n.locked = on
	n.mu.Unlock()
	if changed {
		n.emitState()
	}
}

// SetState tells monitors where the session is: running, asked to end,
// ending.
func (n *Inhibits) SetState(state uint32) {
	n.mu.Lock()
	changed := n.state != state
	n.state = state
	n.mu.Unlock()
	if changed {
		n.emitState()
	}
}

func (n *Inhibits) emitState() {
	n.mu.Lock()
	state := map[string]dbus.Variant{"screensaver-active": dbus.MakeVariant(n.locked), "session-state": dbus.MakeVariant(n.state)}
	var monitors []dbus.ObjectPath
	for m := range n.monitors {
		monitors = append(monitors, m)
	}
	n.mu.Unlock()
	for _, m := range monitors {
		n.conn.Emit(Path, Prefix+"Inhibit.StateChanged", m, state)
	}
}

func (n *Inhibits) release(handle dbus.ObjectPath) {
	n.mu.Lock()
	h := n.held[handle]
	delete(n.held, handle)
	n.mu.Unlock()
	if h == nil {
		return
	}
	if h.cookie != 0 {
		n.conn.Object("org.freedesktop.ScreenSaver", "/org/freedesktop/ScreenSaver").Call("org.freedesktop.ScreenSaver.UnInhibit", 0, h.cookie)
	}
	if h.sleep != nil {
		h.sleep.Close()
	}
}

// sender names the program that asked for the request at handle: the
// frontend puts the app's unique bus name in it ("1_62619" for
// ":1.62619"), and the bus knows its process. "" when it has gone.
func (n *Inhibits) sender(handle dbus.ObjectPath) string {
	parts := strings.Split(string(handle), "/")
	if len(parts) < 3 || parts[len(parts)-3] != "request" {
		return ""
	}
	unique := ":" + strings.ReplaceAll(parts[len(parts)-2], "_", ".")
	var pid uint32
	if n.conn.BusObject().Call("org.freedesktop.DBus.GetConnectionUnixProcessID", 0, unique).Store(&pid) != nil {
		return ""
	}
	return proc.Name(int(pid))
}

type inhibitObj struct{ n *Inhibits }

// Inhibit holds until the frontend closes the request at handle.
func (o inhibitObj) Inhibit(handle dbus.ObjectPath, appID, window string, flags uint32, options map[string]dbus.Variant) *dbus.Error {
	n := o.n
	reason := Options(options).String("reason")
	if appID == "" {
		appID = n.sender(handle) // an app outside a sandbox comes with no ID
	}
	h := &held{Inhibitor: Inhibitor{App: appID, Reason: reason, Flags: flags}}
	who := appID
	if who == "" {
		who = "an app"
	}
	if flags&InhibitIdle != 0 {
		// Without an idle daemon, nothing goes idle to hold off.
		n.conn.Object("org.freedesktop.ScreenSaver", "/org/freedesktop/ScreenSaver").
			Call("org.freedesktop.ScreenSaver.Inhibit", 0, who, reason).Store(&h.cookie)
	}
	if flags&InhibitSuspend != 0 && n.system != nil {
		if sys, err := n.system(); err == nil {
			var fd dbus.UnixFD
			if sys.Object("org.freedesktop.login1", "/org/freedesktop/login1").
				Call("org.freedesktop.login1.Manager.Inhibit", 0, "sleep", who, reason, "block").Store(&fd) == nil {
				h.sleep = os.NewFile(uintptr(fd), "sleep-inhibitor")
			}
		}
	}
	n.mu.Lock()
	n.held[handle] = h
	n.mu.Unlock()
	if err := dbusexport.Export(n.conn, inhibitRequest{n, handle}, handle, Prefix+"Request"); err != nil {
		n.release(handle)
		return dbus.MakeFailedError(err)
	}
	dbusexport.Export(n.conn, introspect.Introspectable(requestXML), handle, "org.freedesktop.DBus.Introspectable")
	return nil
}

func (o inhibitObj) CreateMonitor(handle, session dbus.ObjectPath, appID, window string) (uint32, *dbus.Error) {
	n := o.n
	if err := dbusexport.Export(n.conn, monitorSession{n, session}, session, Prefix+"Session"); err != nil {
		return Ended, nil
	}
	dbusexport.Export(n.conn, introspect.Introspectable(sessionXML), session, "org.freedesktop.DBus.Introspectable")
	n.mu.Lock()
	n.monitors[session] = true
	n.mu.Unlock()
	n.emitState() // where the session is now
	return Success, nil
}

// QueryEndResponse is a monitor having answered SessionQueryEnd; the shell
// does not wait for the answers, it lists the apps holding.
func (o inhibitObj) QueryEndResponse(session dbus.ObjectPath) *dbus.Error { return nil }

type inhibitRequest struct {
	n      *Inhibits
	handle dbus.ObjectPath
}

func (r inhibitRequest) Close() *dbus.Error {
	r.n.release(r.handle)
	dbusexport.Export(r.n.conn, nil, r.handle, Prefix+"Request")
	dbusexport.Export(r.n.conn, nil, r.handle, "org.freedesktop.DBus.Introspectable")
	return nil
}

const sessionXML = `<node><interface name="org.freedesktop.impl.portal.Session"><method name="Close"/><signal name="Closed"/>` +
	`<property name="version" type="u" access="read"/></interface>` + introspect.IntrospectDeclarationString + `</node>`

type monitorSession struct {
	n       *Inhibits
	session dbus.ObjectPath
}

func (m monitorSession) Close() *dbus.Error {
	m.n.mu.Lock()
	delete(m.n.monitors, m.session)
	m.n.mu.Unlock()
	dbusexport.Export(m.n.conn, nil, m.session, Prefix+"Session")
	dbusexport.Export(m.n.conn, nil, m.session, "org.freedesktop.DBus.Introspectable")
	return nil
}
