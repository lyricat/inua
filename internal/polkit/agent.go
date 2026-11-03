// Package polkit is the shell's polkit authentication agent: when a
// program asks for rights it needs a password for (pkexec, a system
// setting, mounting a disk), polkitd asks the session's agent to have the
// user prove who they are. The agent never checks the password itself: it
// passes what the user types to polkit's helper, which checks it through
// PAM and tells polkitd the answer.
//
// Agent is the D-Bus side (registering, polkitd's calls); Conversation is
// the talk with the helper. The shell draws the dialog.
package polkit

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"

	"github.com/lyricat/inua/internal/dbusexport"
)

const (
	authorityName  = "org.freedesktop.PolicyKit1"
	authorityPath  = dbus.ObjectPath("/org/freedesktop/PolicyKit1/Authority")
	authorityIface = "org.freedesktop.PolicyKit1.Authority"
	agentIface     = "org.freedesktop.PolicyKit1.AuthenticationAgent"

	// Path is where the agent's object is.
	Path = dbus.ObjectPath("/io/github/lyricat/Inua/PolkitAgent")
)

const agentXML = `<node>
 <interface name="org.freedesktop.PolicyKit1.AuthenticationAgent">
  <method name="BeginAuthentication">
   <arg type="s" name="action_id" direction="in"/>
   <arg type="s" name="message" direction="in"/>
   <arg type="s" name="icon_name" direction="in"/>
   <arg type="a{ss}" name="details" direction="in"/>
   <arg type="s" name="cookie" direction="in"/>
   <arg type="a(sa{sv})" name="identities" direction="in"/>
  </method>
  <method name="CancelAuthentication"><arg type="s" name="cookie" direction="in"/></method>
 </interface>` + introspect.IntrospectDeclarationString + `</node>`

// ErrCancelled is a request the user dismissed, or polkitd withdrew.
var ErrCancelled = errors.New("polkit: authentication cancelled")

// Request is polkitd asking for someone to authenticate.
type Request struct {
	Action  string // the action's ID, "org.freedesktop.policykit.exec"
	Message string // what it is for, in the user's language
	Icon    string
	Details map[string]string // "program" and "command_line" from pkexec
	Cookie  string            // names the request to the helper
	// Users are the users who may answer, by login name; the one running
	// the shell first when they are one.
	Users []string
}

// Command is the command line pkexec is asked to run, "" when not pkexec.
func (r Request) Command() string {
	return cmp.Or(r.Details["command_line"], r.Details["program"])
}

// Handler has the user answer requests.
type Handler interface {
	// Authenticate asks the user to authenticate as one of r.Users,
	// through Conversations. It returns nil once one succeeded, or an
	// error: the user gave up, or ctx ended because polkitd withdrew the
	// request. Calls come from D-Bus goroutines and may overlap.
	Authenticate(ctx context.Context, r Request) error
}

// Agent is the session's authentication agent.
type Agent struct {
	conn *dbus.Conn // the system bus
	h    Handler

	mu      sync.Mutex
	pending map[string]context.CancelFunc // by cookie
}

// NewAgent prepares an agent on a system bus connection; Serve runs it.
func NewAgent(conn *dbus.Conn, h Handler) *Agent {
	return &Agent{conn: conn, h: h, pending: map[string]context.CancelFunc{}}
}

// subject is polkit's (sa{sv}): a kind and its details.
type subject struct {
	Kind    string
	Details map[string]dbus.Variant
}

// Serve registers the agent for a login session and answers polkitd until
// ctx ends. Registering fails when another agent serves the session.
func (a *Agent) Serve(ctx context.Context, session string) error {
	if err := dbusexport.Export(a.conn, methods{a}, Path, agentIface); err != nil {
		return err
	}
	defer dbusexport.Export(a.conn, nil, Path, agentIface)
	if err := dbusexport.Export(a.conn, introspect.Introspectable(agentXML), Path, "org.freedesktop.DBus.Introspectable"); err != nil {
		return err
	}
	defer dbusexport.Export(a.conn, nil, Path, "org.freedesktop.DBus.Introspectable")

	subj := subject{"unix-session", map[string]dbus.Variant{"session-id": dbus.MakeVariant(session)}}
	authority := a.conn.Object(authorityName, authorityPath)
	locale := cmp.Or(os.Getenv("LC_ALL"), os.Getenv("LC_MESSAGES"), os.Getenv("LANG"), "C")
	if err := authority.CallWithContext(ctx, authorityIface+".RegisterAuthenticationAgent", 0, subj, locale, string(Path)).Err; err != nil {
		return fmt.Errorf("polkit: registering for session %s: %w", session, err)
	}
	<-ctx.Done()
	// polkitd also forgets the agent when its connection closes.
	authority.Call(authorityIface+".UnregisterAuthenticationAgent", 0, subj, string(Path))
	return nil
}

// methods are the agent's D-Bus methods.
type methods struct{ a *Agent }

// identity is polkit's (sa{sv}): "unix-user" with a "uid", or
// "unix-group" with a "gid".
type identity struct {
	Kind    string
	Details map[string]dbus.Variant
}

func (m methods) BeginAuthentication(action, message, icon string, details map[string]string, cookie string, ids []identity) *dbus.Error {
	a := m.a
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	if _, dup := a.pending[cookie]; dup {
		a.mu.Unlock()
		cancel()
		return dbus.NewError("org.freedesktop.PolicyKit1.Error.Failed", []any{"already authenticating for this cookie"})
	}
	a.pending[cookie] = cancel
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.pending, cookie)
		a.mu.Unlock()
		cancel()
	}()

	r := Request{Action: action, Message: message, Icon: icon, Details: details, Cookie: cookie, Users: users(ids, "/etc/group")}
	if len(r.Users) == 0 {
		return dbus.NewError("org.freedesktop.PolicyKit1.Error.Failed", []any{"no user to authenticate as"})
	}
	if err := a.h.Authenticate(ctx, r); err != nil {
		return dbus.NewError("org.freedesktop.PolicyKit1.Error.Cancelled", []any{err.Error()})
	}
	return nil
}

func (m methods) CancelAuthentication(cookie string) *dbus.Error {
	m.a.mu.Lock()
	cancel := m.a.pending[cookie]
	m.a.mu.Unlock()
	if cancel == nil {
		return dbus.NewError("org.freedesktop.PolicyKit1.Error.Failed", []any{"no such request"})
	}
	cancel()
	return nil
}

// users names the users the identities stand for, a group by its members,
// each once; the current user first if among them.
func users(ids []identity, groupFile string) []string {
	var out []string
	add := func(name string) {
		if name != "" && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	for _, id := range ids {
		switch id.Kind {
		case "unix-user":
			if uid, ok := number(id.Details["uid"]); ok {
				if u, err := user.LookupId(strconv.FormatUint(uid, 10)); err == nil {
					add(u.Username)
				}
			}
		case "unix-group":
			if gid, ok := number(id.Details["gid"]); ok {
				for _, name := range groupMembers(groupFile, gid) {
					add(name)
				}
			}
		}
	}
	if me, err := user.Current(); err == nil {
		if i := slices.Index(out, me.Username); i > 0 {
			out = slices.Insert(slices.Delete(out, i, i+1), 0, me.Username)
		}
	}
	return out
}

func number(v dbus.Variant) (uint64, bool) {
	switch n := v.Value().(type) {
	case uint32:
		return uint64(n), true
	case int32:
		return uint64(n), n >= 0
	case uint64:
		return n, true
	case int64:
		return uint64(n), n >= 0
	}
	return 0, false
}

// groupMembers lists a group's members from a group(5) file.
func groupMembers(file string, gid uint64) []string {
	f, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), ":") // name:x:gid:members
		if len(fields) < 4 || fields[2] != strconv.FormatUint(gid, 10) {
			continue
		}
		var out []string
		for m := range strings.SplitSeq(fields[3], ",") {
			if m = strings.TrimSpace(m); m != "" {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

// Session finds the login session polkitd takes the shell to be in, which
// is the only one it may register for: logind's session for this process,
// else the user's display session (a process outside any session, as from
// a user service), else XDG_SESSION_ID.
func Session(ctx context.Context, conn *dbus.Conn) (string, error) {
	logind := conn.Object("org.freedesktop.login1", "/org/freedesktop/login1")
	var path dbus.ObjectPath
	err := logind.CallWithContext(ctx, "org.freedesktop.login1.Manager.GetSessionByPID", 0, uint32(os.Getpid())).Store(&path)
	if err == nil {
		v, err := conn.Object("org.freedesktop.login1", path).GetProperty("org.freedesktop.login1.Session.Id")
		if id, _ := v.Value().(string); err == nil && id != "" {
			return id, nil
		}
	}
	var userPath dbus.ObjectPath
	if logind.CallWithContext(ctx, "org.freedesktop.login1.Manager.GetUser", 0, uint32(os.Getuid())).Store(&userPath) == nil {
		var display struct {
			ID   string
			Path dbus.ObjectPath
		}
		v, err := conn.Object("org.freedesktop.login1", userPath).GetProperty("org.freedesktop.login1.User.Display")
		if err == nil && dbus.Store([]any{v.Value()}, &display) == nil && display.ID != "" {
			return display.ID, nil
		}
	}
	if id := os.Getenv("XDG_SESSION_ID"); id != "" {
		return id, nil
	}
	return "", errors.New("polkit: not in a login session")
}
