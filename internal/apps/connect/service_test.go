package connect

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lyricat/inua/internal/dbusexport"
	"github.com/lyricat/inua/internal/dbustest"
	"github.com/lyricat/inua/internal/kdeconnect"
)

// fakeNotes is a notification server: it keeps what it is shown.
type fakeNotes struct {
	mu    sync.Mutex
	shown []shownNote
	next  uint32
}

type shownNote struct {
	ID         uint32
	App, Title string
	Body       string
	Actions    []string
}

func (f *fakeNotes) Notify(app string, replaces uint32, icon, title, body string, actions []string,
	hints map[string]dbus.Variant, timeout int32) (uint32, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	f.shown = append(f.shown, shownNote{f.next, app, title, body, actions})
	return f.next, nil
}

func (f *fakeNotes) CloseNotification(id uint32) *dbus.Error { return nil }

// await waits for a note that ok wants.
func (f *fakeNotes) await(t *testing.T, what string, ok func(shownNote) bool) shownNote {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		for _, n := range f.shown {
			if ok(n) {
				f.mu.Unlock()
				return n
			}
		}
		f.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no note: %s", what)
	return shownNote{}
}

// phone is a device: a core of its own, and what it was sent.
type phone struct {
	*kdeconnect.Core
	packets chan kdeconnect.Packet
	changed chan kdeconnect.Device
}

func newPhone(t *testing.T, ctx context.Context) *phone {
	p := &phone{packets: make(chan kdeconnect.Packet, 64), changed: make(chan kdeconnect.Device, 64)}
	c, err := kdeconnect.New(kdeconnect.Config{
		Dir: t.TempDir(), Name: "Pixel", Type: "phone",
		Incoming: outgoing, Outgoing: incoming, Loopback: true,
		Changed: func(d kdeconnect.Device) { p.changed <- d },
		Packet:  func(_ kdeconnect.Device, pk kdeconnect.Packet) { p.packets <- pk },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	p.Core = c
	return p
}

// next is the phone's next packet of type typ.
func (p *phone) next(t *testing.T, typ string) kdeconnect.Packet {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case pk := <-p.packets:
			if pk.Type == typ {
				return pk
			}
		case <-timeout:
			t.Fatalf("the phone got no %s", typ)
		}
	}
}

// setup runs the service on a private bus with a fake notification
// server, and a phone paired with it.
func setup(t *testing.T) (*Service, *phone, *fakeNotes, *dbus.Conn, *clipLog) {
	addr := dbustest.Start(t)
	home := t.TempDir()
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", addr)
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))

	notes := &fakeNotes{}
	server := dbustest.Conn(t, addr)
	dbusexport.Export(server, notes, notesPath, notesIface)
	if _, err := server.RequestName(notesName, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s, err := newService("Desk", false, func(c *kdeconnect.Config) {
		c.Ports, c.Peers, c.Loopback = kdeconnect.Ports{}, nil, true
	})
	if err != nil {
		t.Fatal(err)
	}
	copied := &clipLog{}
	s.clip.set = copied.set
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	deadline := time.Now().Add(5 * time.Second)
	for !Running(ctx) {
		if time.Now().After(deadline) {
			t.Fatal("the service did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}

	p := newPhone(t, ctx)
	// The phone announces itself to the service, which connects to it.
	udp, _ := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: s.core.UDPPort()})
	defer udp.Close()
	info := kdeconnect.DeviceInfo{DeviceID: p.ID(), DeviceName: "Pixel", ProtocolVersion: kdeconnect.ProtocolVersion, TCPPort: tcpPort(t, p)}
	udp.Write(kdeconnect.NewPacket(kdeconnect.TypeIdentity, info).Encode())
	awaitDevice(t, p, func(d kdeconnect.Device) bool { return d.Reachable })

	// The phone asks to pair; the user pairs from the note.
	if err := p.RequestPair(s.core.ID()); err != nil {
		t.Fatal(err)
	}
	ask := notes.await(t, "the request to pair", func(n shownNote) bool { return n.Title == "Pixel wants to pair" })
	mine, _ := p.Device(s.core.ID())
	if !bytes.Contains([]byte(ask.Body), []byte(mine.Key)) {
		t.Fatalf("the note does not show the key %s: %q", mine.Key, ask.Body)
	}
	server.Emit(notesPath, notesIface+".ActionInvoked", ask.ID, "accept")
	awaitDevice(t, p, func(d kdeconnect.Device) bool { return d.Pair == kdeconnect.Paired })
	return s, p, notes, server, copied
}

// clipLog is this computer's clipboard: what was put on it.
type clipLog struct {
	mu   sync.Mutex
	list []string
}

func (c *clipLog) set(text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.list = append(c.list, text)
	return nil
}

func (c *clipLog) get() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string{}, c.list...)
}

// tcpPort is the TCP port the phone listens on, as it announces it.
func tcpPort(t *testing.T, p *phone) int {
	port := p.TCPPort()
	if port == 0 {
		t.Fatal("the phone does not listen")
	}
	return port
}

func awaitDevice(t *testing.T, p *phone, ok func(kdeconnect.Device) bool) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case d := <-p.changed:
			if ok(d) {
				return
			}
		case <-timeout:
			t.Fatal("the phone did not get there")
		}
	}
}

func TestPairPingAndClipboard(t *testing.T) {
	s, p, notes, _, copied := setup(t)
	// The phone hears it is paired before the service has kept it.
	kept := time.Now().Add(5 * time.Second)
	for {
		if _, ok := s.core.Store().Get(p.ID()); ok {
			break
		}
		if time.Now().After(kept) {
			t.Fatal("the service did not keep the phone as paired")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Coming, the phone is asked for its notifications.
	p.next(t, typeNoteRequest)

	p.Send(s.core.ID(), kdeconnect.NewPacket(typePing, map[string]string{"message": "where are you"}))
	notes.await(t, "the ping", func(n shownNote) bool { return n.Title == "Pixel" && n.Body == "where are you" })

	// Copied there: on the clipboard here, and not sent back when the
	// clipboard's watcher sees it.
	p.Send(s.core.ID(), kdeconnect.NewPacket(typeClipboard, map[string]string{"content": "from the phone"}))
	deadline := time.Now().Add(5 * time.Second)
	for len(copied.get()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := copied.get(); len(got) != 1 || got[0] != "from the phone" {
		t.Fatalf("copied here: %q", got)
	}
	ctx := context.Background()
	Call(ctx, "Clipboard", "from the phone")
	Call(ctx, "Clipboard", "from here")
	pk := p.next(t, typeClipboard)
	var b struct{ Content string }
	pk.Decode(&b)
	if b.Content != "from here" {
		t.Fatalf("the phone got %q", b.Content)
	}

	// The clipboard turned off for the phone: nothing goes.
	if err := Call(ctx, "SetSetting", p.ID(), SetClipboard, false).Err; err != nil {
		t.Fatal(err)
	}
	Call(ctx, "Clipboard", "private")
	select {
	case pk := <-p.packets:
		if pk.Type == typeClipboard {
			t.Fatal("the clipboard went to the phone though it is off")
		}
	case <-time.After(300 * time.Millisecond):
	}
}

func TestReceiveFile(t *testing.T) {
	s, p, notes, _, _ := setup(t)
	data := []byte("a photo, as bytes")
	body := shareBody{Filename: "../../evil.jpg", LastModified: 1700000000000}
	if err := p.SendPayload(context.Background(), s.core.ID(), kdeconnect.NewPacket(typeShare, body), bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatal(err)
	}
	notes.await(t, "the file", func(n shownNote) bool { return n.Title == "A file from Pixel" && n.Body == "evil.jpg" })
	path := filepath.Join(os.Getenv("HOME"), "Downloads", "evil.jpg")
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("the file: %q, %v", got, err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0666&^umask() {
		t.Fatalf("the file's mode: %v", fi.Mode())
	}
	// A second of the name does not replace the first.
	p.SendPayload(context.Background(), s.core.ID(), kdeconnect.NewPacket(typeShare, body), bytes.NewReader(data), int64(len(data)))
	notes.await(t, "the second file", func(n shownNote) bool { return n.Body == "evil (2).jpg" })
}

func TestPhoneNotification(t *testing.T) {
	s, p, notes, server, _ := setup(t)
	p.Send(s.core.ID(), kdeconnect.NewPacket(typeNote, noteBody{ID: "n1", AppName: "Messages", Title: "Ana", Text: "lunch? <b>now</b>",
		IsClearable: true, Actions: []string{"Mark read"}}))
	n := notes.await(t, "the phone's notification", func(n shownNote) bool { return n.Title == "Ana" })
	if n.App != "Messages" || n.Body != "lunch? &lt;b&gt;now&lt;/b&gt;" {
		t.Fatalf("shown as %+v", n)
	}
	if len(n.Actions) != 2 || n.Actions[1] != "Mark read" {
		t.Fatalf("its buttons: %q", n.Actions)
	}
	// Its button pressed here is pressed there.
	server.Emit(notesPath, notesIface+".ActionInvoked", n.ID, "a:Mark read")
	pk := p.next(t, typeNoteAction)
	var act struct{ Key, Action string }
	pk.Decode(&act)
	if act.Key != "n1" || act.Action != "Mark read" {
		t.Fatalf("the action: %+v", act)
	}
	// Closed here by the user: closed there.
	server.Emit(notesPath, notesIface+".NotificationClosed", n.ID, uint32(2))
	pk = p.next(t, typeNoteRequest)
	var req struct{ Cancel string }
	pk.Decode(&req)
	if req.Cancel != "n1" {
		t.Fatalf("the cancel: %+v", req)
	}
}

func TestCommands(t *testing.T) {
	s, p, _, _, _ := setup(t)
	ctx := context.Background()
	if err := Call(ctx, "SetCommands", `[{"name":"Lock","command":"true"}]`).Err; err != nil {
		t.Fatal(err)
	}
	// The list sent when the phone came may still be on its way: the one
	// wanted names the command.
	var b struct{ CommandList string }
	for i := 0; i < 3 && !strings.Contains(b.CommandList, `"name":"Lock"`); i++ {
		p.next(t, typeCommand).Decode(&b)
	}
	cmds := s.runner.list()
	if len(cmds) != 1 || cmds[0].Key == "" || !bytes.Contains([]byte(b.CommandList), []byte(`"name":"Lock"`)) {
		t.Fatalf("the commands: %+v, sent %s", cmds, b.CommandList)
	}
	if err := Call(ctx, "SetCommands", `[{"name":"","command":"x"}]`).Err; err == nil {
		t.Fatal("a command without a name was taken")
	}
}

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{
		"photo.jpg":        "photo.jpg",
		"../../etc/passwd": "passwd",
		`C:\Users\a\b.txt`: "b.txt",
		".bashrc":          "bashrc",
		"":                 "Shared file",
		"..":               "Shared file",
	} {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}
