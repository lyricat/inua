package polkit

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/dbusexport"
	"github.com/lyricat/inua/internal/dbustest"
)

func TestParse(t *testing.T) {
	for line, want := range map[string]Event{
		"PAM_PROMPT_ECHO_OFF Password: ":                  {Kind: Prompt, Text: "Password: "},
		"PAM_PROMPT_ECHO_ON Login: ":                      {Kind: Question, Text: "Login: "},
		`PAM_TEXT_INFO Place your finger\n on the reader`: {Kind: Info, Text: "Place your finger\n on the reader"},
		`PAM_ERROR_MSG Mot de passe erron\303\251`:        {Kind: Problem, Text: "Mot de passe erroné"},
		"SUCCESS":              {Kind: Done, OK: true},
		"FAILURE":              {Kind: Done},
		"PAM_PROMPT_ECHO_OFFX": {Kind: Info, Text: "PAM_PROMPT_ECHO_OFFX"},
	} {
		if got := parse(line); got != want {
			t.Errorf("parse(%q) = %+v, want %+v", line, got, want)
		}
	}
	if got := unescape(`a\\b\"c\101\`); got != `a\b"cA\` {
		t.Errorf("unescape %q", got)
	}
}

// fakeHelper answers on a socket as polkit's helper does: it reads the
// user and cookie, asks for a password, and succeeds for "right".
func fakeHelper(t *testing.T) chan []string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "helper.socket")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	old := HelperSocket
	HelperSocket = sock
	t.Cleanup(func() { HelperSocket = old })
	got := make(chan []string, 4)
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				r := bufio.NewReader(conn)
				var lines []string
				for range 2 {
					s, _ := r.ReadString('\n')
					lines = append(lines, strings.TrimSuffix(s, "\n"))
				}
				conn.Write([]byte("PAM_TEXT_INFO Hello\nPAM_PROMPT_ECHO_OFF Password: \n"))
				s, _ := r.ReadString('\n')
				lines = append(lines, strings.TrimSuffix(s, "\n"))
				if lines[2] == "right" {
					conn.Write([]byte("SUCCESS\n"))
				} else {
					conn.Write([]byte("PAM_ERROR_MSG Authentication failure\nFAILURE\n"))
				}
				got <- lines
			}()
		}
	}()
	return got
}

// converse runs a conversation to its end, answering prompts with answer.
func converse(t *testing.T, answer string) ([]Event, error) {
	t.Helper()
	c, err := Start("someone", "cookie-1")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var evs []Event
	for {
		ev, err := c.Next()
		evs = append(evs, ev)
		if err != nil || ev.Kind == Done {
			return evs, err
		}
		if ev.Kind == Prompt {
			c.Respond(answer + "\n")
		}
	}
}

func TestConversation(t *testing.T) {
	got := fakeHelper(t)
	evs, err := converse(t, "right")
	if err != nil {
		t.Fatal(err)
	}
	want := []Event{{Kind: Info, Text: "Hello"}, {Kind: Prompt, Text: "Password: "}, {Kind: Done, OK: true}}
	if !slices.Equal(evs, want) {
		t.Errorf("events %+v", evs)
	}
	// The newline in the answer is not sent: it would end it early.
	if lines := <-got; !slices.Equal(lines, []string{"someone", "cookie-1", "right"}) {
		t.Errorf("helper read %q", lines)
	}
	evs, _ = converse(t, "wrong")
	if last := evs[len(evs)-1]; last.Kind != Done || last.OK || evs[len(evs)-2].Kind != Problem {
		t.Errorf("wrong password: %+v", evs)
	}
	<-got

	if _, err := Start("bad\nuser", "c"); err == nil {
		t.Error("a newline in the user name went through")
	}
}

func TestUsers(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Skip(err)
	}
	group := filepath.Join(t.TempDir(), "group")
	os.WriteFile(group, []byte("root:x:0:\nwheel:x:4242:alice,"+me.Username+", bob\n"), 0o600)
	uid, _ := strconv.ParseUint(me.Uid, 10, 32)
	ids := []identity{
		{"unix-group", map[string]dbus.Variant{"gid": dbus.MakeVariant(uint32(4242))}},
		{"unix-user", map[string]dbus.Variant{"uid": dbus.MakeVariant(uint32(uid))}},
		{"unix-netgroup", map[string]dbus.Variant{"name": dbus.MakeVariant("x")}},
	}
	// Each once, the current user first.
	if got := users(ids, group); !slices.Equal(got, []string{me.Username, "alice", "bob"}) {
		t.Errorf("users %v", got)
	}
}

// fakeAuthority stands in for polkitd: it records registrations.
type fakeAuthority struct{ calls chan string }

func (f fakeAuthority) RegisterAuthenticationAgent(s subject, locale, path string) *dbus.Error {
	f.calls <- "register " + s.Kind + " " + s.Details["session-id"].Value().(string) + " " + path
	return nil
}

func (f fakeAuthority) UnregisterAuthenticationAgent(s subject, path string) *dbus.Error {
	f.calls <- "unregister " + path
	return nil
}

type handlerFunc func(context.Context, Request) error

func (f handlerFunc) Authenticate(ctx context.Context, r Request) error { return f(ctx, r) }

func TestAgent(t *testing.T) {
	addr := dbustest.Start(t)
	polkitd := dbustest.Conn(t, addr)
	if _, err := polkitd.RequestName(authorityName, 0); err != nil {
		t.Fatal(err)
	}
	fa := fakeAuthority{make(chan string, 4)}
	dbusexport.Export(polkitd, fa, authorityPath, authorityIface)

	shell := dbustest.Conn(t, addr)
	requests := make(chan Request, 2)
	agent := NewAgent(shell, handlerFunc(func(ctx context.Context, r Request) error {
		requests <- r
		if r.Details["answer"] == "yes" {
			return nil
		}
		<-ctx.Done()
		return ErrCancelled
	}))
	ctx, stop := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- agent.Serve(ctx, "7") }()
	if c := <-fa.calls; c != "register unix-session 7 "+string(Path) {
		t.Errorf("registered as %q", c)
	}

	me, _ := user.Current()
	uid, _ := strconv.ParseUint(me.Uid, 10, 32)
	ids := []identity{{"unix-user", map[string]dbus.Variant{"uid": dbus.MakeVariant(uint32(uid))}}}
	obj := polkitd.Object(shell.Names()[0], Path)
	begin := func(cookie, answer string) error {
		return obj.Call(agentIface+".BeginAuthentication", 0, "org.freedesktop.policykit.exec", "Run it?", "",
			map[string]string{"answer": answer, "command_line": "/usr/bin/true"}, cookie, ids).Err
	}
	if err := begin("c1", "yes"); err != nil {
		t.Errorf("begin: %v", err)
	}
	if r := <-requests; r.Cookie != "c1" || r.Command() != "/usr/bin/true" || !slices.Equal(r.Users, []string{me.Username}) {
		t.Errorf("request %+v", r)
	}

	// polkitd withdraws a request: it ends as cancelled.
	ended := make(chan error, 1)
	go func() { ended <- begin("c2", "wait") }()
	<-requests
	if err := obj.Call(agentIface+".CancelAuthentication", 0, "c2").Err; err != nil {
		t.Errorf("cancel: %v", err)
	}
	var derr dbus.Error
	if err := <-ended; !errors.As(err, &derr) || derr.Name != "org.freedesktop.PolicyKit1.Error.Cancelled" {
		t.Errorf("cancelled request ended with %v", err)
	}
	if err := obj.Call(agentIface+".CancelAuthentication", 0, "nope").Err; err == nil {
		t.Error("cancelled an unknown request")
	}

	stop()
	select {
	case err := <-served:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return")
	}
	if c := <-fa.calls; c != "unregister "+string(Path) {
		t.Errorf("then %q", c)
	}
}
